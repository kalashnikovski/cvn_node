package node

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// MINING AT A POOL
//
// A machine that mines alone finds a block rarely and is paid the whole of
// it. At a pool many machines work on the same blocks, and each is paid its
// part of every block the pool finds: the same in the long run, in smaller
// amounts that come far more often.
//
//	cvn_node --pool-mine HOST        (on Windows: LaunchMiner.bat --pool-mine HOST)
//
// or a line in miner_config.json:
//
//	{ "miner_address": "CVN_...", "mine_at_pool": "HOST" }
//
// starts the fast miner pointed at that pool, with this machine's mining
// address as its name there. That address is where the pool sends what it
// owes. Nothing else is started: no node, and no copy of the chain. The pool
// builds the blocks; this machine does work for it.
//
// Only the fast miner can mine at a pool. The built-in miner takes its work
// from the node in the same program and does not speak to pools.
//
// HOST may be followed by a port (HOST:PORT). Without one it is the port the
// pools of this network listen on: the network's work port.

const poolMineSetting = "mine_at_pool"

// poolMineRequested reports the pool this machine was asked to mine at, and
// where it was asked: on the command line or in miner_config.json. `from`
// is empty when it was not asked at all.
func poolMineRequested(args []string) (target, from string, err error) {
	given, onCommandLine := "", false
	for i, arg := range args {
		switch {
		case arg == "--pool-mine":
			onCommandLine, given = true, ""
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				given = args[i+1]
			}
		case strings.HasPrefix(arg, "--pool-mine="):
			onCommandLine, given = true, strings.TrimPrefix(arg, "--pool-mine=")
		}
	}
	from = "--pool-mine"
	if !onCommandLine {
		if given = readMinerConfig()[poolMineSetting]; given == "" {
			return "", "", nil
		}
		from = `"` + poolMineSetting + `" in miner_config.json`
	}
	if strings.TrimSpace(given) == "" {
		return "", from, fmt.Errorf("--pool-mine needs the pool's address: --pool-mine HOST")
	}
	if Params.StratumPort <= 0 {
		return "", from, fmt.Errorf("%s is not mined with RandomX, so there are no pools on it", Params.Name)
	}
	target, err = poolMineTarget(given, Params.StratumPort)
	return target, from, err
}

// poolMineTarget reads a pool's address as a person writes it, "HOST" or
// "HOST:PORT", into the form a connection is made to. It is strict about
// what a host may look like: the text ends up on another program's command
// line.
func poolMineTarget(text string, usualPort int) (string, error) {
	text = strings.TrimSpace(text)
	for _, prefix := range []string{"stratum+tcp://", "stratum://", "tcp://"} {
		if len(text) > len(prefix) && strings.EqualFold(text[:len(prefix)], prefix) {
			text = text[len(prefix):]
			break
		}
	}
	notOne := fmt.Errorf("%q is not a pool's address. Give the machine's name or number, with or without a port, and nothing else: pool.example.org or pool.example.org:%d", text, usualPort)
	host, port := text, strconv.Itoa(usualPort)
	switch {
	case strings.HasPrefix(text, "["):
		// An IPv6 number in brackets, with or without a port.
		if h, p, err := net.SplitHostPort(text); err == nil {
			host, port = h, p
		} else if strings.HasSuffix(text, "]") {
			host = text[1 : len(text)-1]
		} else {
			return "", notOne
		}
	case strings.Count(text, ":") == 1:
		h, p, err := net.SplitHostPort(text)
		if err != nil {
			return "", notOne
		}
		host, port = h, p
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
		return "", notOne
	}
	if net.ParseIP(host) == nil && !looksLikeHostName(host) {
		return "", notOne
	}
	return net.JoinHostPort(host, port), nil
}

// looksLikeHostName reports whether a text is written as the name of a
// machine: letters, digits and hyphens, in parts separated by dots.
func looksLikeHostName(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, part := range strings.Split(host, ".") {
		if part == "" || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// How long a run of the fast miner must last not to count as a failure to
// start, how many of those in a row are borne, how long to wait before
// starting it again, how often this machine's figures are printed, and how
// long the same complaint from the miner is not printed twice. Variables so
// tests need not wait.
var (
	poolMinerQuickFailure   = 30 * time.Second
	poolMinerFailuresBorne  = 3
	poolMinerRestartPause   = 5 * time.Second
	poolMinerReportEvery    = time.Minute
	poolMinerRepeatAfter    = 2 * time.Minute
	poolMinerSpeedFreshness = 45 * time.Second
)

// poolMinerState is what this program knows of the fast miner's work at the
// pool, read from what the miner prints.
type poolMinerState struct {
	sync.Mutex
	speed     float64
	speedAt   time.Time
	connected bool
	taken     int
	late      int // answers to work the chain had already moved past
	refused   int
	said      map[string]time.Time
}

var (
	poolMinerAccepted = regexp.MustCompile(`\baccepted \(\d+/\d+\)`)
	poolMinerRejected = regexp.MustCompile(`\brejected \(\d+/\d+\)(?: diff \d+)?(?: "([^"]*)")?`)
	poolMinerUsePool  = regexp.MustCompile(`\buse pool \S+`)
)

// read takes one line the fast miner printed. It returns what to show for
// it, if anything.
func (s *poolMinerState) read(line string, now time.Time) (string, bool) {
	s.Lock()
	defer s.Unlock()
	if speed, ok := parseFastMinerSpeed(line); ok {
		s.speed, s.speedAt = speed, now
		return "", false
	}
	lower := strings.ToLower(line)
	switch {
	case poolMinerAccepted.MatchString(lower):
		s.taken++
		s.connected = true
		if s.taken == 1 {
			return "The pool took this machine's first share. From here on it has a part in the blocks the pool finds.", true
		}
		return "", false
	case poolMinerRejected.MatchString(lower):
		reason := ""
		if match := poolMinerRejected.FindStringSubmatch(line); match != nil {
			reason = match[1]
		} else if match := poolMinerRejected.FindStringSubmatch(lower); match != nil {
			reason = match[1]
		}
		// An answer to work the chain has already moved past is not a
		// fault. It happens to every miner each time a block is found
		// somewhere, and it is counted without a word.
		if strings.Contains(strings.ToLower(reason), "stale") {
			s.late++
			return "", false
		}
		s.refused++
		if reason == "" {
			return s.onceInAWhile("The pool refused a share.", now)
		}
		return s.onceInAWhile("The pool refused a share: "+shownPlainly(reason, 200), now)
	case poolMinerUsePool.MatchString(lower):
		s.connected = true
		return "Connected to the pool.", true
	case strings.Contains(lower, "new job from"):
		return "", false
	}
	if strings.Contains(lower, "connect error") || strings.Contains(lower, "read error") || strings.Contains(lower, "no active pools") || strings.Contains(lower, "end of file") {
		s.connected = false
	}
	message, ok := fastMinerMessage(line)
	if !ok {
		return "", false
	}
	return s.onceInAWhile(shownPlainly(message, 300), now)
}

// onceInAWhile lets a message through unless the same one was shown a short
// while ago. A miner that cannot reach its pool says so every few seconds.
func (s *poolMinerState) onceInAWhile(message string, now time.Time) (string, bool) {
	if s.said == nil {
		s.said = make(map[string]time.Time)
	}
	if last, seen := s.said[message]; seen && now.Sub(last) < poolMinerRepeatAfter {
		return "", false
	}
	for text, at := range s.said {
		if now.Sub(at) >= poolMinerRepeatAfter {
			delete(s.said, text)
		}
	}
	s.said[message] = now
	return message, true
}

// figures are the speed while it is fresh, and the shares so far: those the
// pool took, those that came too late to count, and those it refused.
func (s *poolMinerState) figures(now time.Time) (speed float64, speedKnown, connected bool, taken, late, refused int) {
	s.Lock()
	defer s.Unlock()
	speedKnown = !s.speedAt.IsZero() && now.Sub(s.speedAt) < poolMinerSpeedFreshness
	return s.speed, speedKnown, s.connected, s.taken, s.late, s.refused
}

// shownPlainly makes a text that came from somewhere else safe to print: a
// pool, or a program started here, must not be able to move the cursor or
// rewrite the screen.
func shownPlainly(text string, atMost int) string {
	var plain strings.Builder
	for _, c := range text {
		if plain.Len() >= atMost {
			plain.WriteString("...")
			break
		}
		// Control characters, and the invisible ones that turn text
		// round or hide it (review of the pool, round 2, finding 8).
		if c < 0x20 || c == 0x7f || (c >= 0x80 && c < 0xa0) || unicode.In(c, unicode.Cf, unicode.Zl, unicode.Zp) {
			plain.WriteByte(' ')
			continue
		}
		plain.WriteRune(c)
	}
	return strings.TrimSpace(plain.String())
}

// poolFacts is what a pool says of itself on its page (pool_page.go).
type poolFacts struct {
	Network        string  `json:"network"`
	PoolAddress    string  `json:"pool_address"`
	FeePercent     int     `json:"fee_percent"`
	Miners         int     `json:"miners_connected"`
	Speed          float64 `json:"speed_hashes_per_second"`
	BlocksFound    int     `json:"blocks_found"`
	MinimumPayment string  `json:"minimum_payment"`
	ConfirmBlocks  int64   `json:"confirm_blocks"`
	// A pool of an earlier version does not say these two.
	SmallestPayment        string `json:"smallest_payment"`
	PayAnyAmountAfterHours int    `json:"pay_any_amount_after_hours"`
}

// poolFactsProblem says why this program cannot mine at a pool that
// describes itself so. It is empty when it can.
func poolFactsProblem(facts poolFacts) string {
	if facts.Network != Params.Name {
		return fmt.Sprintf("That pool is on the network %q and this program was started for %q.\n"+
			"   Start again with --network and the pool's network, and a mining address of that network.",
			shownPlainly(facts.Network, 40), Params.Name)
	}
	return ""
}

// poolPageAddressOf is where a pool's page is: on the same machine as its
// work port, on the port every node of the network answers questions on.
func poolPageAddressOf(target string) string {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		host = target
	}
	return "http://" + net.JoinHostPort(host, Params.syncPortText())
}

var poolMinerPages = &http.Client{
	Timeout: 8 * time.Second,
	// The page is asked for where the pool is and nowhere it points to.
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// poolPageRead asks a pool's page for one answer and reads it into `into`.
func poolPageRead(address string, into interface{}) error {
	response, err := poolMinerPages.Get(address)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("it answered %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(into)
}

// hashesText writes a mining speed in hashes per second.
func hashesText(speed float64) string {
	if speed >= 10000 {
		return fmt.Sprintf("%.2f kH/s", speed/1000)
	}
	return fmt.Sprintf("%.0f H/s", speed)
}

// poolMinerReport is the line printed now and then while mining at a pool:
// this machine's speed and shares, and what the pool says of its address.
func poolMinerReport(state *poolMinerState, account *poolPageAddress, now time.Time) string {
	speed, speedKnown, connected, taken, late, refused := state.figures(now)
	var parts []string
	switch {
	case !connected:
		parts = append(parts, "Not connected to the pool at the moment; the fast miner keeps trying")
	case speedKnown:
		parts = append(parts, hashesText(speed))
	default:
		parts = append(parts, "Connected")
	}
	shares := fmt.Sprintf("shares taken %d", taken)
	if late > 0 {
		shares += fmt.Sprintf(", too late %d", late)
	}
	if refused > 0 {
		shares += fmt.Sprintf(", refused %d", refused)
	}
	parts = append(parts, shares)
	if account != nil {
		// What blocks have paid is on the chain as soon as each block is.
		// The pool counts a block as sure only once it is deep enough, so
		// for the first hour or so all of it is still waiting.
		paidByBlocks := fmt.Sprintf("blocks have paid this address %s CVN", FormatUnits(account.InBlockUnits+account.InBlockWaitingUnits))
		if account.InBlockWaitingUnits > 0 {
			paidByBlocks += fmt.Sprintf(" (%s of it in blocks still waiting to be sure)", FormatUnits(account.InBlockWaitingUnits))
		}
		parts = append(parts, fmt.Sprintf("%s; the pool owes it %s CVN, has %s CVN on its way and has paid %s CVN",
			paidByBlocks, FormatUnits(account.OwedUnits), FormatUnits(account.PendingUnits), FormatUnits(account.PaidUnits)))
	}
	return strings.Join(parts, " · ")
}

// poolAccountAt asks the pool what it has recorded for one address. The
// figures are the pool's own, shown as such; nothing is done with them.
func poolAccountAt(pageAddress, minerAddress string) *poolPageAddress {
	var account poolPageAddress
	if poolPageRead(pageAddress+"/pool.json?address="+url.QueryEscape(minerAddress), &account) != nil {
		return nil
	}
	if account.Address != minerAddress || account.OwedUnits < 0 || account.PendingUnits < 0 || account.PaidUnits < 0 || account.InBlockUnits < 0 || account.InBlockWaitingUnits < 0 {
		return nil
	}
	return &account
}

// RunPoolMiner mines at a pool until the program is closed. It starts the
// fast miner, keeps it running, and prints what it is doing. It returns the
// program's exit code, and only when mining could not go on.
func RunPoolMiner(target, from string, args []string) int {
	fmt.Println("====================================================================")
	fmt.Printf(" MINING AT A POOL ON %s\n", strings.ToUpper(Params.Name))
	fmt.Println("====================================================================")
	minerAddress, err := resolveMinerAddress(args)
	if err != nil {
		fmt.Println("🚨 " + err.Error())
		return 1
	}
	path, err := locateFastMiner(args)
	if err != nil {
		fmt.Println("🚨 Mining at a pool needs the fast miner. The built-in miner takes its work from the node in")
		fmt.Println("   this program and cannot work for a pool.")
		fmt.Println("   " + err.Error())
		fmt.Println("   How to get it is in MINING_GUIDE.md, \"Using the fast miner\".")
		return 1
	}
	if choice, _ := chosenMiner(args); choice == MinerBuiltIn {
		fmt.Println(" The built-in miner cannot work for a pool, so the fast miner is used.")
	}
	fmt.Printf(" Pool:          %s   (asked for with %s)\n", target, from)
	fmt.Printf(" Your address:  %s\n", minerAddress)
	fmt.Println("                The pool counts this machine's work for that address and pays it there.")
	fmt.Printf(" Fast miner:    %s\n", path)
	fmt.Println(" No node runs on this machine and no chain is kept. The pool builds the blocks.")

	pageAddress := poolPageAddressOf(target)
	var facts poolFacts
	if err := poolPageRead(pageAddress+"/pool.json", &facts); err != nil {
		fmt.Printf(" The pool's page at %s/pool did not answer, so nothing is known of the pool yet.\n", pageAddress)
	} else {
		if problem := poolFactsProblem(facts); problem != "" {
			fmt.Println("🚨 " + problem)
			return 1
		}
		fee := "takes no fee"
		if facts.FeePercent > 0 {
			fee = fmt.Sprintf("keeps %d%% of each block", facts.FeePercent)
		}
		fmt.Printf(" The pool says: %d miner(s) connected, %s, %d block(s) found so far. It %s\n", facts.Miners, hashesText(facts.Speed), facts.BlocksFound, fee)
		fmt.Printf("                and pays every address it owes %s CVN or more.\n", shownPlainly(facts.MinimumPayment, 24))
		if facts.SmallestPayment != "" && facts.PayAnyAmountAfterHours > 0 && facts.PayAnyAmountAfterHours <= 24 {
			fmt.Printf("                An address it has not paid for %d hours is paid whatever it is owed, from %s CVN up.\n", facts.PayAnyAmountAfterHours, shownPlainly(facts.SmallestPayment, 24))
		}
		fmt.Printf(" Its page:      %s/pool\n", pageAddress)
	}
	fmt.Println("====================================================================")

	state := &poolMinerState{}
	go func() {
		for {
			time.Sleep(poolMinerReportEvery)
			log.Println("⛏️ [POOL] " + poolMinerReport(state, poolAccountAt(pageAddress, minerAddress), time.Now()))
		}
	}()
	reason := supervisePoolMiner(path, fastMinerArguments(target, minerAddress, miningThreads(args)), state)
	fmt.Printf("🚨 The fast miner stopped %d times in a row within seconds of starting (%s).\n", poolMinerFailuresBorne, reason)
	fmt.Println("   Mining at a pool cannot go on without it. Start it by hand to see what it says:")
	fmt.Printf("   %s -o %s -u %s -a rx/0\n", path, target, minerAddress)
	return 1
}

// supervisePoolMiner keeps the fast miner running at the pool. A miner that
// cannot reach its pool keeps trying by itself and is left to; one that ends
// is started again. It returns, with the reason, only when the miner ends
// again and again within seconds of starting.
func supervisePoolMiner(path string, arguments []string, state *poolMinerState) string {
	quickFailures := 0
	for {
		started := time.Now()
		err := runPoolMinerOnce(path, arguments, state)
		reason := "it stopped"
		if err != nil {
			reason = err.Error()
		}
		state.Lock()
		state.connected, state.speedAt = false, time.Time{}
		state.Unlock()
		if time.Since(started) < poolMinerQuickFailure {
			quickFailures++
		} else {
			quickFailures = 0
		}
		if quickFailures >= poolMinerFailuresBorne {
			return reason
		}
		log.Printf("⚠️ [POOL] The fast miner is not running (%s). Starting it again.\n", reason)
		time.Sleep(poolMinerRestartPause)
	}
}

// runPoolMinerOnce runs the fast miner until it ends, reading what it
// prints. Its output never reaches the screen directly.
func runPoolMinerOnce(path string, arguments []string, state *poolMinerState) error {
	command := exec.Command(path, arguments...)
	command.Dir = filepath.Dir(path)
	reader, writer := io.Pipe()
	command.Stdout = writer
	command.Stderr = writer
	prepareChildProcess(command)
	if err := command.Start(); err != nil {
		return err
	}
	bindChildProcess(command)

	read := make(chan struct{})
	go func() {
		defer close(read)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			if message, ok := state.read(scanner.Text(), time.Now()); ok {
				log.Println("⛏️ [POOL] " + message)
			}
		}
		_, _ = io.Copy(io.Discard, reader)
	}()
	err := command.Wait()
	_ = writer.Close()
	<-read
	return err
}
