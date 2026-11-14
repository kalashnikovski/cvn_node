package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// WHICH MINER DOES THE WORK
//
// On a network that mines with RandomX there are two miners to choose from:
//
//	builtin  the miner inside this program. Nothing else to install. This
//	         is what runs unless you ask for the other one.
//	xmrig    the CVN edition of XMRig, a separate program that is somewhat
//	         faster. This program starts it, feeds it work through the work
//	         server, keeps showing its own dashboard, and stops it on exit.
//
// Choose with `--miner builtin` or `--miner xmrig` on the command line, or
// with a line in miner_config.json:
//
//	{ "miner_address": "CVN_...", "miner": "xmrig" }
//
// The fast miner is looked for in a folder named xmrig-cvn beside this
// program (or in the folder it is started from). `--xmrig-path FILE`, or
// "xmrig_path" in miner_config.json, points somewhere else. Only the CVN
// edition works: the standard XMRig computes a different RandomX and every
// result it sends would be refused, so it is not accepted here.
//
// If the fast miner cannot be found or keeps failing, the built-in miner
// takes over, so a machine never sits idle because of this choice.
//
// `--threads N` limits either miner to N processor threads.

const (
	MinerBuiltIn = "builtin"
	MinerXMRig   = "xmrig"

	fastMinerQuickFailure    = 60 * time.Second
	fastMinerFailuresAllowed = 3
	fastMinerSpeedFreshness  = 45 * time.Second
)

// How long to wait before starting a stopped fast miner again, and how long
// a started one has to connect to this node.
var (
	fastMinerRestartPause    = 5 * time.Second
	fastMinerConnectDeadline = 40 * time.Second
)

// fastMinerActive is 1 while the fast miner is expected to do the mining.
// The built-in miner stays idle as long as it is set.
var fastMinerActive int32

var fastMinerSpeed struct {
	sync.Mutex
	hashesPerSecond float64
	at              time.Time
}

// argumentValue reads `--name value` or `--name=value` from a command line.
func argumentValue(args []string, name string) string {
	value := ""
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			value = args[i+1]
		} else if strings.HasPrefix(arg, name+"=") {
			value = strings.TrimPrefix(arg, name+"=")
		}
	}
	return strings.TrimSpace(value)
}

// readMinerConfig returns the settings in miner_config.json (empty if there
// is no such file).
func readMinerConfig() map[string]string {
	settings := make(map[string]string)
	raw, err := os.ReadFile("miner_config.json")
	if err != nil {
		return settings
	}
	var anything map[string]interface{}
	if json.Unmarshal(raw, &anything) != nil {
		return settings
	}
	for key, value := range anything {
		if text, ok := value.(string); ok {
			settings[key] = strings.TrimSpace(text)
		}
	}
	return settings
}

// saveMinerAddress writes a new mining address to miner_config.json and
// keeps every other setting in the file as it was.
func saveMinerAddress(address string) error {
	settings := readMinerConfig()
	settings["miner_address"] = address
	configBytes, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("miner_config.json", configBytes, 0644)
}

// chosenMiner returns which miner was asked for: on the command line first,
// then in CVN_MINER, then in miner_config.json. Nothing chosen means the
// built-in miner.
func chosenMiner(args []string) (string, error) {
	choice := argumentValue(args, "--miner")
	if choice == "" {
		choice = strings.TrimSpace(os.Getenv("CVN_MINER"))
	}
	if choice == "" {
		choice = readMinerConfig()["miner"]
	}
	switch strings.ToLower(strings.ReplaceAll(choice, "-", "")) {
	case "", "builtin", "cvn":
		return MinerBuiltIn, nil
	case "xmrig", "xmrigcvn", "fast":
		return MinerXMRig, nil
	}
	return "", fmt.Errorf("%q is not a miner. Use --miner builtin (the miner inside this program) or --miner xmrig (the CVN edition of XMRig)", choice)
}

// miningThreads returns how many processor threads mining may use.
func miningThreads(args []string) int {
	all := runtime.NumCPU()
	if all < 1 {
		all = 1
	}
	if asked, err := strconv.Atoi(argumentValue(args, "--threads")); err == nil && asked >= 1 && asked < all {
		return asked
	}
	return all
}

// locateFastMiner finds the CVN edition of XMRig on this machine.
func locateFastMiner(args []string) (string, error) {
	name := "xmrig"
	if runtime.GOOS == "windows" {
		name = "xmrig.exe"
	}
	if given := argumentValue(args, "--xmrig-path"); given != "" {
		return usableFastMiner(given, name)
	}
	if given := readMinerConfig()["xmrig_path"]; given != "" {
		return usableFastMiner(given, name)
	}
	var folders []string
	if program, err := os.Executable(); err == nil {
		folders = append(folders, filepath.Dir(program))
	}
	folders = append(folders, ".")
	for _, folder := range folders {
		for _, candidate := range []string{filepath.Join(folder, "xmrig-cvn", name), filepath.Join(folder, name)} {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return usableFastMiner(candidate, name)
			}
		}
	}
	return "", fmt.Errorf("the CVN edition of XMRig was not found. Download it from https://github.com/kalashnikovski/cvn-xmrig/releases and unpack it into a folder named xmrig-cvn beside this program, or point to it with --xmrig-path")
}

// usableFastMiner checks that a path leads to a program and that the
// program is the CVN edition.
func usableFastMiner(path, programName string) (string, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, programName)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(absolute); err != nil || info.IsDir() {
		return "", fmt.Errorf("there is no miner program at %s", absolute)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, absolute, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("%s could not be started: %v", absolute, err)
	}
	if !isCVNEditionBanner(string(output)) {
		return "", fmt.Errorf("%s is not the CVN edition of XMRig. The standard XMRig cannot mine CVN; get the CVN edition from https://github.com/kalashnikovski/cvn-xmrig/releases", absolute)
	}
	return absolute, nil
}

// isCVNEditionBanner recognises the CVN edition by what it prints for
// `--version`.
func isCVNEditionBanner(versionOutput string) bool {
	firstLine, _, _ := strings.Cut(strings.TrimSpace(versionOutput), "\n")
	return strings.HasPrefix(strings.TrimSpace(firstLine), "XMRig-CVN")
}

// fastMinerArguments is the command line the fast miner is started with.
func fastMinerArguments(workServerAddress, minerAddress string, threads int) []string {
	arguments := []string{
		"-o", workServerAddress,
		"-u", minerAddress,
		"-a", "rx/0",
		"--donate-level", "0",
		"--no-color",
		"--no-title",
		"--print-time", "10",
		"--retry-pause", "3",
	}
	if threads > 0 && threads < runtime.NumCPU() {
		arguments = append(arguments, "-t", strconv.Itoa(threads))
	}
	return arguments
}

var fastMinerSpeedLine = regexp.MustCompile(`speed 10s/60s/15m\s+(\S+)\s+(\S+)\s+(\S+)\s+H/s`)

// parseFastMinerSpeed reads the mining speed, in hashes per second, from a
// line of the fast miner's output. It prefers the most recent figure.
func parseFastMinerSpeed(line string) (float64, bool) {
	match := fastMinerSpeedLine.FindStringSubmatch(line)
	if match == nil {
		return 0, false
	}
	for _, text := range match[1:] {
		if speed, err := strconv.ParseFloat(text, 64); err == nil && speed >= 0 {
			return speed, true
		}
	}
	return 0, false
}

// fastMinerMessage picks out the lines of the fast miner's output that are
// worth a place on the dashboard, and tidies them. Everything else it prints
// stays out of the way.
func fastMinerMessage(line string) (string, bool) {
	text := strings.TrimSpace(line)
	if strings.HasPrefix(text, "[") {
		if end := strings.Index(text, "]"); end > 0 {
			text = strings.TrimSpace(text[end+1:])
		}
	}
	text = strings.Join(strings.Fields(strings.TrimPrefix(text, "* ")), " ")
	lower := strings.ToLower(text)
	if strings.Contains(lower, "speed 10s/60s/15m") || strings.HasPrefix(lower, "commands") {
		return "", false
	}
	for _, marker := range []string{"huge pages", "dataset ready", "error", "fail", "rejected", "no active pools", "permission"} {
		if strings.Contains(lower, marker) {
			return text, true
		}
	}
	return "", false
}

// fastMinerHashrate returns the speed the fast miner last reported, while it
// is connected and the figure is recent.
func fastMinerHashrate() (float64, bool) {
	if atomic.LoadInt32(&fastMinerActive) == 0 || atomic.LoadInt32(&externalMinersConnected) == 0 {
		return 0, false
	}
	fastMinerSpeed.Lock()
	defer fastMinerSpeed.Unlock()
	if time.Since(fastMinerSpeed.at) > fastMinerSpeedFreshness {
		return 0, false
	}
	return fastMinerSpeed.hashesPerSecond, true
}

// StartChosenMiner starts the fast miner if that is what was asked for and
// it can run here. In every other case it does nothing, and the built-in
// miner, which is always started, does the mining.
func StartChosenMiner(workServerAddress string) {
	choice, err := chosenMiner(os.Args)
	if err != nil || choice != MinerXMRig {
		return
	}
	if workServerAddress == "" {
		announceNetworkStatus("⛏️ [MINER] The fast miner works on RandomX networks only. This network is mined with the built-in miner.")
		return
	}
	path, err := locateFastMiner(os.Args)
	if err != nil {
		announceNetworkStatus("⚠️ [MINER] " + err.Error() + ". Mining with the built-in miner.")
		return
	}
	// The work server may be open to other machines; the miner started here
	// always reaches it on this machine.
	if _, port, err := net.SplitHostPort(workServerAddress); err == nil {
		workServerAddress = net.JoinHostPort("127.0.0.1", port)
	}
	arguments := fastMinerArguments(workServerAddress, strings.TrimSpace(CustomMinerAddress), miningThreads(os.Args))
	atomic.StoreInt32(&fastMinerActive, 1)
	announceNetworkStatus("⛏️ [MINER] Starting the fast miner: " + path)
	go superviseFastMiner(path, arguments)
}

// superviseFastMiner keeps the fast miner running, and hands the mining back
// to the built-in miner if it fails again and again.
func superviseFastMiner(path string, arguments []string) {
	quickFailures := 0
	for {
		started := time.Now()
		err := runFastMinerOnce(path, arguments)
		if time.Since(started) < fastMinerQuickFailure {
			quickFailures++
		} else {
			quickFailures = 0
		}
		reason := "it stopped"
		if err != nil {
			reason = err.Error()
		}
		if quickFailures >= fastMinerFailuresAllowed {
			atomic.StoreInt32(&fastMinerActive, 0)
			announceNetworkStatus(fmt.Sprintf("⚠️ [MINER] The fast miner failed %d times in a row (%s). The built-in miner is taking over.", quickFailures, reason))
			return
		}
		announceNetworkStatus(fmt.Sprintf("⚠️ [MINER] The fast miner is not running (%s). Starting it again.", reason))
		time.Sleep(fastMinerRestartPause)
	}
}

// runFastMinerOnce runs the fast miner until it exits, reading what it
// prints. Its output never reaches the screen directly.
func runFastMinerOnce(path string, arguments []string) error {
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

	go func() {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if speed, ok := parseFastMinerSpeed(line); ok {
				fastMinerSpeed.Lock()
				fastMinerSpeed.hashesPerSecond, fastMinerSpeed.at = speed, time.Now()
				fastMinerSpeed.Unlock()
			} else if message, ok := fastMinerMessage(line); ok {
				announceNetworkStatus("⛏️ [FAST MINER] " + message)
			}
		}
		_, _ = io.Copy(io.Discard, reader)
	}()

	// A miner that runs but never connects would mine nothing for ever, so
	// it is stopped and counted as a failure. Time the node spends catching
	// up with the network does not count against it.
	exited := make(chan struct{})
	go func() {
		waited := time.Duration(0)
		for waited < fastMinerConnectDeadline {
			select {
			case <-exited:
				return
			case <-time.After(time.Second):
			}
			if atomic.LoadInt32(&externalMinersConnected) > 0 {
				return
			}
			if atomic.LoadInt32(&IsSyncingActive) == 0 && atomic.LoadInt32(&MiningHoldActive) == 0 {
				waited += time.Second
			}
		}
		_ = command.Process.Kill()
	}()

	err := command.Wait()
	close(exited)
	_ = writer.Close()
	if err == nil && atomic.LoadInt32(&externalMinersConnected) == 0 {
		err = fmt.Errorf("it never connected to this node")
	}
	return err
}
