//go:build multinode

package node

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// SEVERAL REAL NODES ON ONE MACHINE
//
// Every other test in this folder runs inside one process and stands in for
// the network. These start the real program several times over, each copy
// with its own folder and its own loopback address, and watch the copies
// find each other, pass blocks on, catch up, and settle on one chain.
//
// They are left out of an ordinary `go test`: they take a few minutes and
// open ports. Run them with:
//
//	go test -tags multinode -run MultiNode -timeout 30m ./node/
//
// The nodes run the `localnet` network (localnet_multinode.go): the launch
// rules with the fast hash, reachable only from this machine.

// localCluster is one run's set of nodes.
type localCluster struct {
	t       *testing.T
	program string
	folder  string
	subnet  int // third number of every node's address, different on each run
	genesis int64
	start   int64    // starting difficulty
	env     []string // further settings given to every node started after they are added
	nodes   []*localNode
}

// localNode is one copy of the program.
type localNode struct {
	cluster *localCluster
	name    string
	host    string
	folder  string
	args    []string
	env     []string
	program string // another version of the program to run, or empty for this one
	process *exec.Cmd
	done    chan struct{}
	exit    error
	logFile *os.File
}

var (
	buildProgramOnce sync.Once
	builtProgram     string
	buildProgramErr  error
)

// multiNodeProgram builds the program once, with the local test network in it.
func multiNodeProgram(t *testing.T) string {
	t.Helper()
	buildProgramOnce.Do(func() {
		folder, err := os.MkdirTemp("", "cvn-multinode-program-")
		if err != nil {
			buildProgramErr = err
			return
		}
		name := "cvn_localnode"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		builtProgram = filepath.Join(folder, name)
		build := exec.Command("go", "build", "-tags", "multinode", "-o", builtProgram, ".")
		build.Dir = ".." // the project's main folder
		if out, err := build.CombinedOutput(); err != nil {
			buildProgramErr = fmt.Errorf("building the program: %v\n%s", err, out)
		}
	})
	if buildProgramErr != nil {
		t.Fatal(buildProgramErr)
	}
	return builtProgram
}

// earlierVersion is the release that nodes of this version must be able to
// share a network with.
const earlierVersion = "v5.0.0-rc11"

var (
	buildEarlierOnce sync.Once
	builtEarlier     string
	buildEarlierErr  error
)

// earlierProgram builds the program as it was at the earlier release, from
// this repository's own history, with the local test network in it. The test
// is skipped where that history is not at hand.
func earlierProgram(t *testing.T) string {
	t.Helper()
	buildEarlierOnce.Do(func() {
		folder, err := os.MkdirTemp("", "cvn-multinode-earlier-")
		if err != nil {
			buildEarlierErr = err
			return
		}
		source := filepath.Join(folder, "source")
		add := exec.Command("git", "worktree", "add", "--detach", source, earlierVersion)
		add.Dir = ".."
		if out, err := add.CombinedOutput(); err != nil {
			buildEarlierErr = fmt.Errorf("skip: %s is not in this copy of the repository: %v\n%s", earlierVersion, err, out)
			return
		}
		defer func() {
			remove := exec.Command("git", "worktree", "remove", "--force", source)
			remove.Dir = ".."
			_ = remove.Run()
		}()
		name := "cvn_earlier"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		builtEarlier = filepath.Join(folder, name)
		build := exec.Command("go", "build", "-tags", "multinode", "-o", builtEarlier, ".")
		build.Dir = source
		if out, err := build.CombinedOutput(); err != nil {
			buildEarlierErr = fmt.Errorf("building %s: %v\n%s", earlierVersion, err, out)
		}
	})
	if buildEarlierErr != nil {
		if strings.HasPrefix(buildEarlierErr.Error(), "skip:") {
			t.Skip(buildEarlierErr)
		}
		t.Fatal(buildEarlierErr)
	}
	return builtEarlier
}

// earlier makes a node run the earlier release in place of this version.
func (n *localNode) earlier() *localNode {
	n.program = earlierProgram(n.cluster.t)
	n.name += " (" + earlierVersion + ")"
	return n
}

// measuredStartingDifficulty picks a starting difficulty at which one mining
// thread on this machine finds a block every second or two. The rule then
// takes over, as it does on a real network.
func measuredStartingDifficulty() int64 {
	block := Block{Version: 1, Index: 1, Timestamp: 1794614500, PrevHash: strings.Repeat("0", 64), Difficulty: 100000}
	digest := BlockTransactionsDigest(1, nil)
	hashes, began := int64(0), time.Now()
	for time.Since(began) < 250*time.Millisecond {
		block.Nonce++
		_ = CalculateHashWithTxDigest(block, digest)
		hashes++
	}
	// The miner does more than hash (it looks at the chain's tip each time),
	// so it is slower than this loop. A fifth of the loop's rate per second is
	// roughly a block a second or two.
	difficulty := hashes * 4 / 5
	if difficulty < HeartbeatMinDifficulty {
		difficulty = HeartbeatMinDifficulty
	}
	return difficulty
}

// claimTestSubnet picks the third number of the addresses one test's nodes
// use, and holds it until the test ends. Two runs of these tests at once on
// one machine used to be able to draw the same number; their nodes then
// found each other, each saw a different first block, and a test failed for
// a reason that had nothing to do with the program. Holding a port on the
// subnet open marks it as taken, to this process and to any other.
func claimTestSubnet(t *testing.T) int {
	t.Helper()
	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	for attempt := 0; attempt < 200; attempt++ {
		subnet := 1 + random.Intn(250)
		held, err := net.Listen("tcp", fmt.Sprintf("127.77.%d.250:39999", subnet))
		if err != nil {
			continue
		}
		t.Cleanup(func() { held.Close() })
		return subnet
	}
	t.Fatal("no free range of addresses was found for this test's nodes")
	return 0
}

func newLocalCluster(t *testing.T) *localCluster {
	t.Helper()
	cluster := &localCluster{
		t:       t,
		program: multiNodeProgram(t),
		folder:  t.TempDir(),
		subnet:  claimTestSubnet(t),
		genesis: time.Now().Unix() - 60,
		start:   measuredStartingDifficulty(),
	}
	// This test process signs transfers and makes addresses for the same network.
	t.Setenv("CVN_LOCALNET_HOST", "127.77.0.1")
	saved := Params
	if err := SelectNetwork(localnetName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			cluster.saveStacks()
		}
		cluster.stopAll()
		Params = saved
		applyParams()
		if t.Failed() {
			cluster.showLogs()
		}
	})
	return cluster
}

func (c *localCluster) host(number int) string {
	return fmt.Sprintf("127.77.%d.%d", c.subnet, number)
}

// node prepares a copy of the program at address number `number`. It is not
// started yet. seeds are address numbers.
func (c *localCluster) node(name string, number int, seeds []int, extra ...string) *localNode {
	c.t.Helper()
	folder := filepath.Join(c.folder, name)
	if err := os.MkdirAll(folder, 0700); err != nil {
		c.t.Fatal(err)
	}
	args := []string{"--network", localnetName, "--no-backups"}
	if len(seeds) > 0 {
		hosts := make([]string, len(seeds))
		for i, seed := range seeds {
			hosts[i] = c.host(seed)
		}
		args = append(args, "--seeds", strings.Join(hosts, ","))
	}
	n := &localNode{
		cluster: c, name: name, host: c.host(number), folder: folder,
		args: append(args, extra...),
		env: append(os.Environ(),
			"CVN_LOCALNET_HOST="+c.host(number),
			fmt.Sprintf("CVN_LOCALNET_GENESIS=%d", c.genesis),
			fmt.Sprintf("CVN_LOCALNET_DIFFICULTY=%d", c.start),
			"CVN_SEEDS=", "CVN_NETWORK=", "CVN_MINER=", "CVN_LOCALNET_DEBUG=1",
		),
	}
	n.env = append(n.env, c.env...)
	c.nodes = append(c.nodes, n)
	return n
}

// seed prepares a node that does not mine; miner one that mines to `address`
// with a single thread.
func (c *localCluster) seed(name string, number int, seeds []int) *localNode {
	return c.node(name, number, seeds, "--no-mining")
}

func (c *localCluster) miner(name string, number int, seeds []int, address string, extra ...string) *localNode {
	return c.node(name, number, seeds, append([]string{"--miner-address", address, "--threads", "1"}, extra...)...)
}

func (n *localNode) startNode() *localNode {
	t := n.cluster.t
	t.Helper()
	if n.running() {
		t.Fatalf("%s is already running", n.name)
	}
	logFile, err := os.OpenFile(filepath.Join(n.folder, "node.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(logFile, "\n===== started %s: %s =====\n", time.Now().Format("15:04:05"), strings.Join(n.args, " "))
	program := n.cluster.program
	if n.program != "" {
		program = n.program
	}
	process := exec.Command(program, n.args...)
	process.Dir = n.folder
	process.Env = n.env
	process.Stdout, process.Stderr = logFile, logFile
	if err := process.Start(); err != nil {
		t.Fatalf("starting %s: %v", n.name, err)
	}
	n.process, n.logFile, n.done, n.exit = process, logFile, make(chan struct{}), nil
	go func(done chan struct{}) {
		n.exit = process.Wait()
		close(done)
	}(n.done)
	return n
}

func (n *localNode) running() bool {
	if n.done == nil {
		return false
	}
	select {
	case <-n.done:
		return false
	default:
		return true
	}
}

// stopNode ends the program the hard way, as a power cut or a closed window
// does. The chain file has to survive that.
func (n *localNode) stopNode() {
	if n.process == nil {
		return
	}
	if n.running() {
		_ = n.process.Process.Kill()
		<-n.done
	}
	if n.logFile != nil {
		n.logFile.Close()
		n.logFile = nil
	}
}

func (c *localCluster) stopAll() {
	for _, n := range c.nodes {
		n.stopNode()
	}
}

func (n *localNode) log() string {
	data, _ := os.ReadFile(filepath.Join(n.folder, "node.log"))
	return string(data)
}

// saveStacks asks every node still running what each part of it is doing, and
// keeps the answers in a folder that outlives the test. A node that has
// stopped making progress is explained by this and by nothing else.
func (c *localCluster) saveStacks() {
	folder, err := os.MkdirTemp("", "cvn-multinode-stacks-")
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	for _, n := range c.nodes {
		// The whole log, and the node's own account of its newest block.
		_ = os.WriteFile(filepath.Join(folder, n.name+".log"), []byte(n.log()), 0600)
		if !n.running() {
			continue
		}
		if resp, err := client.Get(n.url("/block")); err == nil {
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			_ = os.WriteFile(filepath.Join(folder, n.name+".block.json"), data, 0600)
		}
		resp, err := client.Get(fmt.Sprintf("http://%s:%d/debug/pprof/goroutine?debug=2", n.host, localnetDebugPort))
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		_ = os.WriteFile(filepath.Join(folder, n.name+".txt"), data, 0600)
	}
	c.t.Logf("what each running node was doing is saved in %s", folder)
}

func (c *localCluster) showLogs() {
	for _, n := range c.nodes {
		lines := strings.Split(strings.TrimRight(n.log(), "\r\n"), "\n")
		if len(lines) > 45 {
			lines = lines[len(lines)-45:]
		}
		c.t.Logf("----- last lines of %s (%s) -----\n%s", n.name, n.host, strings.Join(lines, "\n"))
	}
}

func (n *localNode) url(path string) string {
	return fmt.Sprintf("http://%s:%d%s", n.host, Params.SyncPort, path)
}

var localClient = &http.Client{Timeout: 3 * time.Second}

func (n *localNode) get(path string, into interface{}) bool {
	resp, err := localClient.Get(n.url(path))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(into) == nil
}

// tip is the node's newest block: its height and identifier. ok is false if
// the node did not answer.
func (n *localNode) tip() (height int64, hash string, ok bool) {
	var reply struct {
		Height int64  `json:"current_height"`
		Hash   string `json:"block_hash"`
	}
	if !n.get("/block", &reply) {
		return 0, "", false
	}
	return reply.Height, reply.Hash, true
}

func (n *localNode) height() int64 {
	height, _, ok := n.tip()
	if !ok {
		return -1
	}
	return height
}

func (n *localNode) hashAt(height int64) string {
	var reply struct {
		Hash string `json:"hash"`
	}
	if !n.get(fmt.Sprintf("/blockhash?height=%d", height), &reply) {
		return ""
	}
	return reply.Hash
}

// balances is every address with coins, in CVN, as this node sees it.
func (n *localNode) balances() map[string]float64 {
	balances := map[string]float64{}
	n.get("/addresses", &balances)
	return balances
}

func (n *localNode) coins(address string) []SpendableCoin {
	var coins []SpendableCoin
	n.get("/utxos?address="+address, &coins)
	return coins
}

// eventually waits until check() is true, and fails the test if it is not
// within the limit.
func (c *localCluster) eventually(limit time.Duration, what string, check func() bool) {
	c.t.Helper()
	deadline := time.Now().Add(limit)
	for {
		if check() {
			return
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("after %s: %s", limit, what)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// waitForHeight waits until every named node is at `height` or above.
func (c *localCluster) waitForHeight(limit time.Duration, height int64, nodes ...*localNode) {
	c.t.Helper()
	c.eventually(limit, fmt.Sprintf("not every node reached block #%d (%s)", height, c.heights(nodes)), func() bool {
		for _, n := range nodes {
			if n.height() < height {
				return false
			}
		}
		return true
	})
}

func (c *localCluster) heights(nodes []*localNode) string {
	parts := make([]string, len(nodes))
	for i, n := range nodes {
		parts[i] = fmt.Sprintf("%s #%d", n.name, n.height())
	}
	return strings.Join(parts, ", ")
}

// waitForAgreement waits until the named nodes hold the same block at a
// height at or above `atLeast`. Nodes a block or two apart at the very top
// have simply not all heard of the newest block yet, so the block compared
// is two below the lowest node's tip.
func (c *localCluster) waitForAgreement(limit time.Duration, atLeast int64, nodes ...*localNode) (height int64, hash string) {
	c.t.Helper()
	c.eventually(limit, fmt.Sprintf("the nodes did not settle on one chain at or above block #%d (%s)", atLeast, c.heights(nodes)), func() bool {
		lowest := int64(-1)
		for _, n := range nodes {
			h := n.height()
			if h < 0 {
				return false
			}
			if lowest < 0 || h < lowest {
				lowest = h
			}
		}
		height = lowest - 2
		if height < atLeast {
			return false
		}
		hash = nodes[0].hashAt(height)
		if hash == "" {
			return false
		}
		for _, n := range nodes[1:] {
			if n.hashAt(height) != hash {
				return false
			}
		}
		return true
	})
	return height, hash
}

func newTestWallet(t *testing.T) (privateKey, publicKey, address string) {
	t.Helper()
	privateKey, publicKey, address, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, publicKey, strings.TrimSpace(address)
}

// TestMultiNodeNetwork is one small network taken through an ordinary life:
// seeds, a miner, a second miner arriving late, a transfer, a seed that
// loses power and comes back, and a miner that was cut off and returns with
// blocks of its own.
func TestMultiNodeNetwork(t *testing.T) {
	c := newLocalCluster(t)
	const wait = 4 * time.Minute

	key1, pub1, miner1Address := newTestWallet(t)
	_, _, miner2Address := newTestWallet(t)
	_, _, strayAddress := newTestWallet(t)
	_, _, friendAddress := newTestWallet(t)

	seedA := c.seed("seed-a", 1, []int{1, 2}).startNode()
	seedB := c.seed("seed-b", 2, []int{1, 2}).startNode()
	c.waitForHeight(30*time.Second, 0, seedA, seedB)

	miner1 := c.miner("miner-1", 11, []int{1, 2}, miner1Address).startNode()

	t.Run("blocks from a miner reach every seed", func(t *testing.T) {
		c.waitForHeight(wait, 6, miner1, seedA, seedB)
		c.waitForAgreement(wait, 4, miner1, seedA, seedB)
		if got := seedA.balances()[miner1Address]; got < 50 {
			t.Errorf("seed-a shows %v CVN for the miner; it has mined at least six blocks", got)
		}
	})
	if t.Failed() {
		return
	}

	var miner2 *localNode
	t.Run("a miner that arrives late downloads the chain, then mines on it", func(t *testing.T) {
		before := seedA.height()
		miner2 = c.miner("miner-2", 12, []int{1, 2}, miner2Address).startNode()
		c.waitForHeight(wait, before, miner2)
		if first := miner2.hashAt(1); first == "" || first != seedA.hashAt(1) {
			t.Fatalf("the late miner's block #1 is %.16s and the network's is %.16s: it did not join the chain that was there", first, seedA.hashAt(1))
		}
		if !strings.Contains(miner2.log(), "Comparing our chain with") {
			t.Error("the late miner's log does not show it comparing its chain with another node's")
		}
		// Both miners go on to win blocks, and everyone counts them the same.
		c.eventually(wait, "the two miners did not both win blocks", func() bool {
			balances := seedB.balances()
			return balances[miner2Address] >= 50 && balances[miner1Address] >= 50
		})
		c.waitForAgreement(wait, before, miner1, miner2, seedA, seedB)
	})
	if t.Failed() {
		return
	}

	t.Run("a transfer handed to one node is mined and seen by all", func(t *testing.T) {
		var coins []SpendableCoin
		c.eventually(wait, "seed-b lists no coins for the first miner", func() bool {
			coins = seedB.coins(miner1Address)
			return len(coins) > 0
		})
		transfer, err := BuildSignedTransfer(key1, pub1, friendAddress, cvn("12.5"), cvn("0.5"), coins)
		if err != nil {
			t.Fatalf("building the transfer: %v", err)
		}
		// Handed to a seed, which mines nothing: it has to pass it to the miners.
		if err := SubmitTransferOverHTTP(seedB.url(""), transfer); err != nil {
			t.Fatalf("seed-b did not take the transfer: %v", err)
		}
		for _, n := range []*localNode{seedA, seedB, miner1, miner2} {
			n := n
			c.eventually(wait, n.name+" never showed the 12.5 CVN that was sent", func() bool {
				return n.balances()[friendAddress] == 12.5
			})
		}
	})
	if t.Failed() {
		return
	}

	t.Run("a seed that loses power comes back and catches up", func(t *testing.T) {
		seedB.stopNode()
		stoppedAt := seedA.height()
		c.waitForHeight(wait, stoppedAt+4, seedA, miner1, miner2)
		seedB.startNode()
		c.waitForHeight(wait, stoppedAt+4, seedB)
		c.waitForAgreement(wait, stoppedAt+2, seedA, seedB, miner1, miner2)
		if strings.Contains(seedB.log(), "panic:") {
			t.Error("seed-b's log shows a panic")
		}
	})
	if t.Failed() {
		return
	}

	t.Run("a miner that was cut off gives up its own blocks for the network's chain", func(t *testing.T) {
		// Cut off: it knows of no other node, so it mines a chain of its own
		// from the same first block.
		stray := c.miner("stray", 13, nil, strayAddress).startNode()
		c.waitForHeight(wait, 3, stray)
		ownBlock := stray.hashAt(1)
		stray.stopNode()
		// Back in touch, not mining, so that anything it holds afterwards
		// came from the network.
		stray.args = []string{"--network", localnetName, "--no-backups", "--no-mining", "--seeds", c.host(1) + "," + c.host(2)}
		stray.startNode()
		c.eventually(30*time.Second, "the returning node did not answer", func() bool {
			return stray.height() >= 0
		})
		target := seedA.height()
		c.waitForHeight(wait, target, stray)
		c.waitForAgreement(wait, target-2, stray, seedA, miner1)
		if ownBlock == "" || ownBlock == seedA.hashAt(1) {
			t.Errorf("the cut-off miner's own block #1 was %.16s, the same as the network's: it was never on a chain of its own", ownBlock)
		}
		if !strings.Contains(stray.log(), "Switching to the branch") {
			t.Error("the returning node's log does not show it switching to the network's branch")
		}
		if got := stray.hashAt(1); got != seedA.hashAt(1) {
			t.Errorf("the returning node still holds its own block #1 (%.16s, was %.16s); the network's is %.16s", got, ownBlock, seedA.hashAt(1))
		}
		if got := stray.balances()[strayAddress]; got != 0 {
			t.Errorf("the returning node still counts %v CVN from blocks the network never had", got)
		}
		if got := seedA.balances()[strayAddress]; got != 0 {
			t.Errorf("the network counts %v CVN for blocks that were mined cut off from it", got)
		}
	})

	// With the miners stopped the chain stands still, and every node left
	// must hold exactly the same thing: the same newest block, the same total
	// work, the same vault, and the same coins at every address.
	t.Run("every node counts the same coins", func(t *testing.T) {
		miner1.stopNode()
		miner2.stopNode()
		var stray *localNode
		for _, n := range c.nodes {
			if n.name == "stray" {
				stray = n
			}
		}
		rest := []*localNode{seedA, seedB, stray}
		type state struct {
			Height int64  `json:"current_height"`
			Hash   string `json:"block_hash"`
			Work   string `json:"total_work"`
			Vault  int64  `json:"vault_units"`
		}
		var settled state
		c.eventually(wait, "with the miners stopped, the remaining nodes did not come to rest on one block", func() bool {
			var first state
			if !seedA.get("/block", &first) {
				return false
			}
			for _, n := range rest[1:] {
				var other state
				if !n.get("/block", &other) || other != first {
					return false
				}
			}
			settled = first
			return true
		})
		want := seedA.balances()
		var total float64
		for _, amount := range want {
			total += amount
		}
		for _, n := range rest[1:] {
			got := n.balances()
			if len(got) != len(want) {
				t.Errorf("%s lists %d addresses with coins and seed-a lists %d", n.name, len(got), len(want))
			}
			for address, amount := range want {
				if got[address] != amount {
					t.Errorf("%s counts %v CVN at %s and seed-a counts %v", n.name, got[address], address, amount)
				}
			}
		}
		// Nothing created and nothing lost: the coins and the vault together
		// are exactly what the blocks issued.
		issued := float64(settled.Height) * 50
		held := total + float64(settled.Vault)/float64(UnitsPerCVN)
		if diff := held - issued; diff > 0.000001 || diff < -0.000001 {
			t.Errorf("at block #%d the nodes hold %v CVN with the vault, and %v was issued", settled.Height, held, issued)
		}
		if want[friendAddress] != 12.5 {
			t.Errorf("the 12.5 CVN that was sent is now counted as %v", want[friendAddress])
		}
		t.Logf("came to rest at block #%d: %v CVN at %d addresses, %d units in the vault; starting difficulty %d",
			settled.Height, total, len(want), settled.Vault, c.start)
		for _, n := range c.nodes {
			if strings.Contains(n.log(), "panic:") {
				t.Errorf("%s's log shows a panic", n.name)
			}
		}
	})
}

// A new node whose seeds do not answer must not begin a chain of its own
// (the fault a newcomer met on 19 November 2026), and carries on by itself
// once a seed is there.
func TestMultiNodeANewNodeWaitsForItsSeeds(t *testing.T) {
	c := newLocalCluster(t)
	_, _, address := newTestWallet(t)

	alone := c.miner("newcomer", 21, []int{20}, address).startNode()
	c.eventually(30*time.Second, "the newcomer did not say it was waiting", func() bool {
		return strings.Contains(alone.log(), "NOT MINING YET")
	})
	// Long enough to have mined many blocks, had it started.
	time.Sleep(8 * time.Second)
	if h := alone.height(); h > 0 {
		t.Fatalf("with its seed silent, the newcomer mined to block #%d on its own", h)
	}
	if !alone.running() {
		t.Fatal("the newcomer stopped while waiting; it should keep asking")
	}

	// The seed comes up. The newcomer notices and starts.
	c.seed("late-seed", 20, nil).startNode()
	c.eventually(2*time.Minute, "the newcomer did not start once its seed answered", func() bool {
		return alone.height() >= 1
	})
	if !strings.Contains(alone.log(), "Another machine answered") {
		t.Error("the newcomer did not say why it carried on")
	}
}

// Seeds that answer but hold another first block: the program stops and says
// so, and mines nothing.
func TestMultiNodeSeedsOnAnotherChainAreRefused(t *testing.T) {
	c := newLocalCluster(t)
	_, _, address := newTestWallet(t)
	seed := c.seed("their-seed", 30, nil).startNode()
	c.waitForHeight(30*time.Second, 0, seed)

	// Same program, same network name, a different first block.
	mine := c.miner("ours", 31, []int{30}, address)
	for i, entry := range mine.env {
		if strings.HasPrefix(entry, "CVN_LOCALNET_GENESIS=") {
			mine.env[i] = fmt.Sprintf("CVN_LOCALNET_GENESIS=%d", c.genesis-1000)
		}
	}
	mine.startNode()
	select {
	case <-mine.done:
	case <-time.After(45 * time.Second):
		t.Fatal("the program was still running 45 seconds after finding its seed on another chain")
	}
	if mine.exit == nil {
		t.Error("the program ended as if nothing was wrong")
	}
	log := mine.log()
	for _, want := range []string{"DIFFERENT CHAIN", c.host(30), "Nothing was mined", "--start-alone"} {
		if !strings.Contains(log, want) {
			t.Errorf("the message does not mention %q", want)
		}
	}
	if strings.Contains(log, "[SUCCESS]") {
		t.Error("a block was mined before the program stopped")
	}
}

// A seed address written with a port, as a newcomer might copy it from a
// browser, stops the program with the address named.
func TestMultiNodeUnreadableSeedsStopTheProgram(t *testing.T) {
	c := newLocalCluster(t)
	_, _, address := newTestWallet(t)
	n := c.miner("typo", 40, nil, address, "--seeds", c.host(41)+":38081,"+c.host(42))
	n.startNode()
	select {
	case <-n.done:
	case <-time.After(30 * time.Second):
		t.Fatal("the program kept running with a seed address it could not read")
	}
	log := n.log()
	if n.exit == nil || !strings.Contains(log, "could not be read") || !strings.Contains(log, c.host(41)+":38081") {
		t.Errorf("the program did not name the address it could not read:\n%s", log)
	}
	if _, err := os.Stat(filepath.Join(n.folder, Params.DatabaseFile)); err == nil {
		t.Error("a chain file was created before the seed list was checked")
	}
}

// restAndCheckAccounts waits, with nobody mining, until the named nodes hold
// the same newest block, and then requires that they count the same coins at
// every address and that coins plus vault are exactly what was issued.
func (c *localCluster) restAndCheckAccounts(limit time.Duration, nodes ...*localNode) (height int64) {
	t := c.t
	t.Helper()
	type state struct {
		Height int64  `json:"current_height"`
		Hash   string `json:"block_hash"`
		Work   string `json:"total_work"`
		Vault  int64  `json:"vault_units"`
	}
	var settled state
	agreed := func() (state, bool) {
		var first state
		if !nodes[0].get("/block", &first) {
			return first, false
		}
		for _, n := range nodes[1:] {
			var other state
			if !n.get("/block", &other) || other != first {
				return first, false
			}
		}
		return first, true
	}
	c.eventually(limit, "with the miners stopped, the nodes did not come to rest on one block", func() bool {
		// At rest means still agreeing a moment later. A miner that was
		// stopped a moment after finding a block has that block still on its
		// way to the others, and one look can fall just before it lands.
		first, ok := agreed()
		if !ok {
			return false
		}
		time.Sleep(2 * time.Second)
		again, ok := agreed()
		if !ok || again != first {
			return false
		}
		settled = again
		return true
	})
	want := nodes[0].balances()
	var total float64
	for _, amount := range want {
		total += amount
	}
	for _, n := range nodes[1:] {
		got := n.balances()
		if len(got) != len(want) {
			t.Errorf("%s lists %d addresses with coins and %s lists %d", n.name, len(got), nodes[0].name, len(want))
		}
		for address, amount := range want {
			if got[address] != amount {
				t.Errorf("%s counts %v CVN at %s and %s counts %v", n.name, got[address], address, nodes[0].name, amount)
			}
		}
	}
	issued := float64(settled.Height) * 50
	held := total + float64(settled.Vault)/float64(UnitsPerCVN)
	if diff := held - issued; diff > 0.000001 || diff < -0.000001 {
		t.Errorf("at block #%d the nodes hold %v CVN with the vault, and %v was issued", settled.Height, held, issued)
	}
	return settled.Height
}

// Blocks filled to the size limit: mined, passed between real nodes, checked
// by each, and counted the same by all. The same code that fills them here
// is the tool for doing it on a real test network (fill_blocks_devtool.go).
func TestMultiNodeFullBlocks(t *testing.T) { fullBlocksBetweenRealNodes(t, false) }

// The same, on a network where some nodes run the earlier release: a seed
// and an ordinary node on the old version, a seed, an ordinary node and the
// miner on this one. Blocks at the size limit have to reach all of them, by
// announcement where both ends understand that and whole where one does not,
// and every node has to end with the same chain and the same coins.
func TestMultiNodeOldAndNewVersionsTogether(t *testing.T) { fullBlocksBetweenRealNodes(t, true) }

// A sweep with real nodes. On this network a coin's date comes twelve blocks
// after it was made or last swept (the launch figure is seven years), so in
// a few minutes coins pass their first date, where a fifth is set aside,
// and their second, where it goes to the vault. One coin's owner takes the
// fifth back by renewing. A node that was not there for any of it then
// downloads the whole chain. Every node must hold the same coins, the same
// fifths set aside and the same vault, and together they must be exactly
// what was issued.
func TestMultiNodeFifthsSetAsideAreTheSameOnEveryNode(t *testing.T) {
	const interval = 12
	t.Setenv("CVN_LOCALNET_SWEEP_BLOCKS", fmt.Sprint(interval))
	t.Setenv("CVN_LOCALNET_VAULT_BLOCKS", "50")
	c := newLocalCluster(t)
	const wait = 8 * time.Minute
	key, pub, address := newTestWallet(t)

	seedA := c.seed("seed-a", 1, []int{1, 2}).startNode()
	seedB := c.seed("seed-b", 2, []int{1, 2}).startNode()
	c.waitForHeight(30*time.Second, 0, seedA, seedB)
	miner := c.miner("miner", 11, []int{1, 2}, address).startNode()

	// Past the first date of the first coins: some have a fifth set aside.
	c.waitForHeight(wait, interval+6, miner, seedA, seedB)
	var chosen SpendableCoin
	c.eventually(wait, "no coin of the miner's has a fifth set aside after its date", func() bool {
		// Of the coins with a fifth set aside, the one whose next date is
		// furthest off, so that the renewal has time to be mined before it.
		chosen = SpendableCoin{}
		for _, coin := range miner.coins(address) {
			if coin.HeldUnits > 0 && coin.SweepAt > chosen.SweepAt {
				chosen = coin
			}
		}
		return chosen.HeldUnits > 0
	})
	if chosen.Amount.Units() != cvn("50") || chosen.HeldUnits != cvn("10") {
		t.Fatalf("a reward coin past its first date is offered at %s with %s set aside; want 50 with 10", FormatUnits(chosen.Amount.Units()), FormatUnits(chosen.HeldUnits))
	}
	// Its owner takes the fifth back: one free transfer of the coin to its
	// own address, handed to a seed, which has to pass it to the miner.
	renewal, err := BuildSignedRenewal(key, pub, 0, []SpendableCoin{chosen})
	if err != nil {
		t.Fatal(err)
	}
	if err := SubmitTransferOverHTTP(seedA.url(""), renewal); err != nil {
		t.Fatalf("the seed refused the renewal: %v", err)
	}
	c.eventually(wait, "the renewal was not mined", func() bool {
		for _, coin := range seedB.coins(address) {
			if coin.TxID == renewal.ID {
				return coin.Amount.Units() == cvn("50") && coin.HeldUnits == 0
			}
		}
		return false
	})

	// Past the second date of the first coins: their fifths are the
	// vault's, and the vault has begun to pay them out to the miner.
	c.waitForHeight(wait, 2*interval+8, miner, seedA, seedB)

	// A node that saw none of it downloads the chain from the two seeds.
	late := c.seed("late", 21, []int{1, 2}).startNode()
	c.waitForHeight(wait, 2*interval+8, late)

	miner.stopNode()
	height := c.restAndCheckAccounts(wait, seedA, seedB, late)
	type figures struct {
		Hash  string `json:"block_hash"`
		Vault int64  `json:"vault_units"`
		Held  int64  `json:"held_units"`
	}
	var first figures
	if !seedA.get("/block", &first) {
		t.Fatal("seed-a did not answer")
	}
	if first.Vault <= 0 || first.Held <= 0 {
		t.Errorf("at block #%d the vault holds %d units and %d are set aside on coins; both should be above nought by now", height, first.Vault, first.Held)
	}
	// A node asked too often answers nothing for a moment, so each list of
	// coins is asked for until it comes: an empty answer is never compared.
	listed := func(n *localNode) []SpendableCoin {
		var coins []SpendableCoin
		c.eventually(time.Minute, n.name+" did not list the miner's coins", func() bool {
			coins = n.coins(address)
			return len(coins) > 0
		})
		return coins
	}
	reference := listed(seedA)
	wantCoins, _ := json.Marshal(reference)
	for _, n := range []*localNode{seedB, late} {
		var other figures
		if !n.get("/block", &other) || other != first {
			t.Errorf("%s answers %+v and seed-a %+v", n.name, other, first)
		}
		if gotCoins, _ := json.Marshal(listed(n)); string(gotCoins) != string(wantCoins) {
			t.Errorf("%s lists the miner's coins differently from seed-a (amounts, fifths set aside or dates differ)", n.name)
		}
	}
	// Every coin on this network is the miner's, so the fifths on the coins
	// a node lists must add up, to the unit, to the figure it gives for all
	// that is set aside.
	var heldListed int64
	for _, coin := range reference {
		heldListed += coin.HeldUnits
	}
	if heldListed != first.Held {
		t.Errorf("seed-a says %d units are set aside in all; the coins it lists have %d set aside on them", first.Held, heldListed)
	}
	// A node that is stopped and started again counts its coins afresh from
	// what it has stored, and must come to the same figures and the same
	// coins.
	seedB.stopNode()
	seedB.startNode()
	c.eventually(wait, "seed-b, started again, does not give the figures it gave before", func() bool {
		var again figures
		return seedB.get("/block", &again) && again == first
	})
	if gotCoins, _ := json.Marshal(listed(seedB)); string(gotCoins) != string(wantCoins) {
		t.Errorf("seed-b, started again, lists the miner's coins differently from before")
	}
	var total float64
	for _, amount := range seedA.balances() {
		total += amount
	}
	t.Logf("came to rest at block #%d: %v CVN at addresses, of which %d units set aside; %d units in the vault", height, total, first.Held, first.Vault)
	for _, n := range c.nodes {
		log := n.log()
		if strings.Contains(log, "panic:") {
			t.Errorf("%s's log shows a panic", n.name)
		}
		if strings.Contains(log, "CONSENSUS REJECTION") || strings.Contains(log, "STORAGE REFUSAL") {
			t.Errorf("%s's log shows a refused block", n.name)
		}
	}
}

// handOverTogether gives a node many transfers, eight at a time. One at a
// time takes long enough on a busy machine for the miner to find several
// blocks meanwhile, each holding only the transfers that had arrived by then
// and none of them full.
func handOverTogether(t *testing.T, to *localNode, transfers []Transaction) {
	t.Helper()
	var handing sync.WaitGroup
	refusals := make(chan error, len(transfers))
	next := make(chan Transaction)
	for worker := 0; worker < 8; worker++ {
		handing.Add(1)
		go func() {
			defer handing.Done()
			for tx := range next {
				if err := SubmitTransferOverHTTP(to.url(""), tx); err != nil {
					refusals <- err
				}
			}
		}()
	}
	for _, tx := range transfers {
		next <- tx
	}
	close(next)
	handing.Wait()
	select {
	case err := <-refusals:
		t.Fatalf("%s refused a transfer: %v", to.name, err)
	default:
	}
}

func fullBlocksBetweenRealNodes(t *testing.T, withEarlierVersion bool) {
	c := newLocalCluster(t)
	const wait = 4 * time.Minute
	key, pub, address := newTestWallet(t)

	seedA := c.seed("seed-a", 1, []int{1, 2})
	if withEarlierVersion {
		seedA.earlier()
	}
	seedA.startNode()
	seedB := c.seed("seed-b", 2, []int{1, 2}).startNode()
	c.waitForHeight(30*time.Second, 0, seedA, seedB)
	watched := []*localNode{seedA, seedB}
	if withEarlierVersion {
		// Two nodes that are nobody's seed: they hear of blocks only through
		// the connections they keep open to the seeds.
		watched = append(watched,
			c.seed("old-node", 21, []int{1, 2}).earlier().startNode(),
			c.seed("new-node", 22, []int{1, 2}).startNode())
		c.waitForHeight(30*time.Second, 0, watched...)
	}
	miner := c.miner("miner", 11, []int{1, 2}, address).startNode()
	c.waitForHeight(wait, 6, append([]*localNode{miner}, watched...)...)

	// First cut six rewards into fifty coins each, so there are enough
	// coins to make a block's worth of transfers at once.
	var rewards []SpendableCoin
	c.eventually(wait, "the miner has fewer than six coins", func() bool {
		rewards = miner.coins(address)
		return len(rewards) >= 6
	})
	cuts, _, _ := fillTransfers(key, pub, address, rewards[:6], 1<<30, 100000)
	if len(cuts) != 6 {
		t.Fatalf("built %d transfers from six coins", len(cuts))
	}
	for _, tx := range cuts {
		if err := SubmitTransferOverHTTP(miner.url(""), tx); err != nil {
			t.Fatalf("the miner refused a transfer: %v", err)
		}
	}
	var pieces []SpendableCoin
	c.eventually(wait, "the three hundred small coins never appeared", func() bool {
		pieces = pieces[:0]
		for _, coin := range miner.coins(address) {
			if coin.Amount.Units() < cvn("2") {
				pieces = append(pieces, coin)
			}
		}
		return len(pieces) >= 300
	})

	// Now nearly four blocks' worth of transfers, all at once. The first
	// block after them may be mined while they are still being handed over
	// and hold only some; the blocks after it are full.
	budget := blockTransferBudgetBytes()
	transfers, bytes, _ := fillTransfers(key, pub, address, pieces, budget*15/4, 100000)
	if bytes < budget*3 {
		t.Fatalf("built only %d bytes of transfers; a block holds %d", bytes, budget)
	}
	from := miner.height()
	handOverTogether(t, miner, transfers)
	var seeds []string
	names := make(map[string]string)
	for _, n := range watched {
		seeds = append(seeds, n.url(""))
		names[n.url("")] = n.name
	}
	blocks := watchBlocks(miner.url(""), seeds, from, 8, 20*time.Second, wait, nil)
	if len(blocks) < 8 {
		// The miner stopped making blocks with transfers still waiting.
		c.saveStacks()
	}

	full, carried := 0, 0
	for _, b := range blocks {
		carried += b.Transfers
		t.Logf("after the transfers were sent: block #%d, %d bytes, %d transfers", b.Height, b.Bytes, b.Transfers)
		if b.Bytes > Params.MaxBlockBytes {
			t.Errorf("block #%d is %d bytes, over the limit of %d", b.Height, b.Bytes, Params.MaxBlockBytes)
		}
		if b.Bytes < Params.MaxBlockBytes*9/10 {
			continue
		}
		full++
		for _, seed := range seeds {
			delay, reached := b.Delay[seed]
			if !reached {
				t.Errorf("full block #%d (%d bytes) did not reach %s within 20 seconds", b.Height, b.Bytes, names[seed])
				continue
			}
			t.Logf("full block #%d: %d bytes, %d transfers, on %s after %.2f s", b.Height, b.Bytes, b.Transfers, names[seed], delay.Seconds())
		}
	}
	if full < 2 {
		t.Errorf("saw %d full block(s) in the %d blocks after the transfers were sent; want at least two", full, len(blocks))
	}
	if carried != len(transfers) {
		t.Errorf("%d transfers were sent and the next %d blocks carried %d", len(transfers), len(blocks), carried)
	}

	// The pieces put back together: the other kind of large transfer, each
	// spending the most coins a transfer may.
	var made []SpendableCoin
	c.eventually(wait, "the pieces of the filled blocks never appeared", func() bool {
		made = miner.coins(address)
		return len(made) >= len(transfers)*TxMaxOutputs
	})
	combines, combineBytes := combineBack(key, pub, transfers, made, 100000)
	if len(combines) < len(transfers)*TxMaxOutputs/TxMaxInputs {
		t.Fatalf("built %d combining transfers for %d pieces", len(combines), len(transfers)*TxMaxOutputs)
	}
	from = miner.height()
	handOverTogether(t, miner, combines)
	carried = 0
	for _, b := range watchBlocks(miner.url(""), seeds, from, combineBytes/budget+4, 20*time.Second, wait, nil) {
		carried += b.Transfers
		if b.Bytes > Params.MaxBlockBytes {
			t.Errorf("block #%d is %d bytes, over the limit of %d", b.Height, b.Bytes, Params.MaxBlockBytes)
		}
		if b.Transfers == 0 {
			continue
		}
		for _, seed := range seeds {
			if delay, reached := b.Delay[seed]; !reached {
				t.Errorf("block #%d (%d bytes of combining transfers) did not reach %s within 20 seconds", b.Height, b.Bytes, seed)
			} else {
				t.Logf("combining block #%d: %d bytes, %d transfers of up to %d coins, on %s after %.2f s", b.Height, b.Bytes, b.Transfers, TxMaxInputs, seed, delay.Seconds())
			}
		}
	}
	if carried != len(combines) {
		t.Errorf("%d combining transfers (%d bytes) were sent and the next blocks carried %d", len(combines), combineBytes, carried)
	}

	// How the large blocks travelled. A node of this version asks for a
	// block that was announced to it; a node of the earlier version knows
	// nothing of announcements and must never have been sent one it acted on.
	asked := make(map[string]int)
	for _, n := range c.nodes {
		asked[n.name] = strings.Count(n.log(), "asking for it")
		t.Logf("%s asked for %d announced block(s)", n.name, asked[n.name])
		if n.program != "" && asked[n.name] > 0 {
			t.Errorf("%s runs the earlier version and yet asked for an announced block", n.name)
		}
	}
	if !withEarlierVersion && (asked[seedA.name] < full || asked[seedB.name] < full) {
		// Among nodes of this version a large block is never sent unasked,
		// so each seed must have asked for every full block it holds.
		t.Errorf("%d full blocks were mined, and the seeds asked for %d and %d: a large block was sent without being asked for", full, asked[seedA.name], asked[seedB.name])
	}

	miner.stopNode()
	height := c.restAndCheckAccounts(wait, watched...)
	// The pieces are all still the sender's: the coins moved nowhere.
	if got, want := seedA.balances()[address], float64(height)*50; got >= want || got < want-1 {
		t.Errorf("the wallet holds %v CVN at block #%d; it should hold just under %v (the fees' fifth is in the vault)", got, height, want)
	}
	for _, n := range c.nodes {
		log := n.log()
		if strings.Contains(log, "panic:") {
			t.Errorf("%s's log shows a panic", n.name)
		}
		if strings.Contains(log, "is not valid") {
			if n.program == "" {
				t.Errorf("%s's log shows a refused block", n.name)
			} else {
				// The earlier release can take one block two ways at once
				// and then call it not valid; that fault is known and is
				// fixed in this version. It is noted, and not held against
				// this version.
				t.Logf("%s, on the earlier release, refused a block it was taking two ways at once (a fault known in that release)", n.name)
			}
		}
		// Blocks here come a second or two apart, large ones followed
		// closely by small ones. No node of this version may take that for
		// having fallen behind and set an honest neighbour aside.
		if n.program == "" && strings.Contains(n.log(), "did not deliver one") {
			t.Errorf("%s set a neighbour aside for not delivering a chain", n.name)
		}
	}
}
