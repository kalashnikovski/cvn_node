//go:build multinode

package node

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A POOL AMONG REAL NODES
//
// These start the real program as a pool, beside seeds that do not mine, on
// a local test network that mines with RandomX (see localnet_multinode.go).
// The miners are stand-ins inside the test: each connects to the pool as the
// fast miner does, is sent work, tries nonces with RandomX and hands in
// those that meet what the pool asked. Nothing else on the network mines, so
// every block is the pool's.
//
// A block counts after six more, in place of the hundred of a real network,
// so that a test sees blocks found, counted and owed within a minute or two.

// poolWorker is one stand-in miner.
type poolWorker struct {
	address string
	pause   time.Duration // rest between two tries, to make a slower machine
	conn    net.Conn
	stop    chan struct{}
	done    chan struct{}
	job     struct {
		sync.Mutex
		now minerJob
	}
	counted int64 // shares the pool said it counted
	refused int64
	fault   atomic.Value
}

// startPoolWorker connects a stand-in miner to a pool and sets it mining.
func startPoolWorker(t *testing.T, pool *localNode, address string, pause time.Duration) *poolWorker {
	t.Helper()
	server := net.JoinHostPort(pool.host, strconv.Itoa(localnetWorkPort))
	var conn net.Conn
	var err error
	for attempt := 0; attempt < 100; attempt++ {
		if conn, err = net.DialTimeout("tcp", server, 2*time.Second); err == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("%s does not take miners at %s: %v", pool.name, server, err)
	}
	worker := &poolWorker{address: address, pause: pause, conn: conn, stop: make(chan struct{}), done: make(chan struct{})}
	t.Cleanup(worker.halt)
	login, _ := json.Marshal(map[string]interface{}{"id": 1, "jsonrpc": "2.0", "method": "login",
		"params": map[string]string{"login": address, "pass": "x", "agent": "stand-in"}})
	if _, err := conn.Write(append(login, '\n')); err != nil {
		t.Fatal(err)
	}
	go worker.listen()
	go worker.mine()
	return worker
}

// listen takes in what the pool sends: new work, and its answer to each
// share handed in.
func (w *poolWorker) listen() {
	reader := bufio.NewReader(w.conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var message struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(line, &message) != nil {
			continue
		}
		var job minerJob
		switch {
		case message.Method == "job":
			_ = json.Unmarshal(message.Params, &job)
		case message.Error != nil:
			// Work on a block someone has just found is refused as stale;
			// that is the pool working as it should.
			if !strings.Contains(message.Error.Message, "stale") && !strings.Contains(message.Error.Message, "expired") {
				w.fault.Store(message.Error.Message)
			}
			atomic.AddInt64(&w.refused, 1)
			continue
		default:
			var login struct {
				Job minerJob `json:"job"`
			}
			if json.Unmarshal(message.Result, &login) == nil && login.Job.JobID != "" {
				job = login.Job
			} else if strings.Contains(string(message.Result), "OK") {
				atomic.AddInt64(&w.counted, 1)
			}
		}
		if job.JobID != "" {
			w.job.Lock()
			w.job.now = job
			w.job.Unlock()
		}
	}
}

// mine tries nonces on the newest work, as mining software does, and hands
// in each one that meets the target the pool sent with it.
func (w *poolWorker) mine() {
	defer close(w.done)
	var working minerJob
	var blob, key []byte
	var target uint64
	var nonce uint32
	submitted := 1
	for {
		select {
		case <-w.stop:
			return
		default:
		}
		w.job.Lock()
		newest := w.job.now
		w.job.Unlock()
		if newest.JobID == "" {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if newest.JobID != working.JobID {
			working, nonce = newest, 0
			blob, _ = hex.DecodeString(working.Blob)
			key, _ = hex.DecodeString(working.SeedHash)
			raw, _ := hex.DecodeString(working.Target)
			target = 0
			for i, b := range raw {
				target |= uint64(b) << (8 * uint(i))
			}
			if len(blob) != powBlobSize || len(key) == 0 || len(raw) != 8 {
				w.fault.Store(fmt.Sprintf("unusable work from the pool: %+v", working))
				return
			}
		}
		blob[39], blob[40], blob[41], blob[42] = byte(nonce), byte(nonce>>8), byte(nonce>>16), byte(nonce>>24)
		value, ok := randomXVerify(key, blob)
		if !ok {
			w.fault.Store("RandomX failed in the stand-in miner")
			return
		}
		if top, _ := strconv.ParseUint(value[:16], 16, 64); top < target {
			submitted++
			share, _ := json.Marshal(map[string]interface{}{"id": submitted, "jsonrpc": "2.0", "method": "submit",
				"params": map[string]string{"job_id": working.JobID, "nonce": hex.EncodeToString(blob[39:43])}})
			if _, err := w.conn.Write(append(share, '\n')); err != nil {
				return
			}
		}
		nonce++
		if w.pause > 0 {
			time.Sleep(w.pause)
		}
	}
}

func (w *poolWorker) halt() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	<-w.done
	w.conn.Close()
}

func (w *poolWorker) problem() string {
	if fault, ok := w.fault.Load().(string); ok {
		return fault
	}
	return ""
}

// poolCluster is a cluster whose network mines with RandomX, on which a
// block counts after six more and a pool asks its miners for difficulty 4.
// Its pool pays nothing in the block: every part goes to the pool's address
// and is owed, so that these tests see the whole of the pool's keeping of
// debts and paying them. A pool that pays in its blocks is inBlockCluster.
func poolCluster(t *testing.T) *localCluster {
	c := inBlockCluster(t)
	c.env = append(c.env, fmt.Sprintf("CVN_LOCALNET_POOL_FLOOR=%d", MaxAmountUnits))
	return c
}

// inBlockCluster is poolCluster with a pool as it really runs: every part of
// a hundredth of a coin or more is paid by the block itself.
func inBlockCluster(t *testing.T) *localCluster {
	c := newLocalCluster(t)
	c.env = append(c.env, "CVN_LOCALNET_RANDOMX=1", "CVN_LOCALNET_REORG_LIMIT=6", "CVN_LOCALNET_POOL_SHARE=4")
	c.start = 48 // a block takes about fifty hashes to begin with
	return c
}

// TestMultiNodeAPoolPaysInItsBlocks is a pool as it really runs, with three
// miners, beside two seeds. Every block the pool finds pays the miners
// itself. The pool's address holds nothing, owes nothing, and there is
// nothing for the paying command to pay; and what each miner holds, on
// every node, is what the pool's record says the blocks paid it.
func TestMultiNodeAPoolPaysInItsBlocks(t *testing.T) {
	c := inBlockCluster(t)
	const wait = 5 * time.Minute
	poolsKey, _, poolsAddress := newTestWallet(t)
	_, _, fastAddress := newTestWallet(t)
	_, _, middleAddress := newTestWallet(t)
	_, _, slowAddress := newTestWallet(t)

	seedA := c.seed("seed-a", 1, nil).startNode()
	seedB := c.seed("seed-b", 2, []int{1}).startNode()
	c.waitForHeight(wait, 0, seedA, seedB)
	pool := c.pool("pool", 5, []int{1, 2}, poolsAddress).startNode()
	c.eventually(wait, "the pool did not publish its record", func() bool {
		page, ok := poolPageOf(pool)
		return ok && page.PoolAddress == poolsAddress
	})
	workers := []*poolWorker{
		startPoolWorker(t, pool, fastAddress, 0),
		startPoolWorker(t, pool, middleAddress, 40*time.Millisecond),
		startPoolWorker(t, pool, slowAddress, 160*time.Millisecond),
	}
	c.eventually(wait, "six of the pool's blocks did not come to count", func() bool {
		for _, worker := range workers {
			if fault := worker.problem(); fault != "" {
				t.Fatalf("a miner was told: %s", fault)
			}
		}
		page, _ := poolPageOf(pool)
		return page.BlocksConfirmed >= 6
	})
	for _, worker := range workers {
		worker.halt()
	}
	c.eventually(wait, "the pool still counts miners that have left", func() bool {
		page, _ := poolPageOf(pool)
		return page.Miners == 0
	})
	height := seedA.height()
	c.waitForHeight(wait, height, seedB, pool)
	height = seedA.height()
	var page poolPage
	c.eventually(wait, "blocks six deep were left undecided", func() bool {
		page, _ = poolPageOf(pool)
		return allDeepBlocksDecided(page, height)
	})
	if page.BlocksFound != int(height) || len(page.Blocks) != page.BlocksFound {
		t.Fatalf("the chain has %d blocks and the pool found %d (%d shown)", height, page.BlocksFound, len(page.Blocks))
	}

	// Every block of the pool's is the block the seeds hold, and its reward
	// on the chain pays exactly the parts the record says it paid.
	paid := make(map[string]int64) // by every block still standing
	var keptByThePool int64
	for _, block := range page.Blocks {
		if block.State == poolBlockLost {
			continue
		}
		if seedA.hashAt(block.Height) != block.Hash || seedB.hashAt(block.Height) != block.Hash {
			t.Errorf("block %d in the pool's record is not the block the seeds hold", block.Height)
		}
		var parts int64
		for _, part := range block.Parts {
			parts += part.Units
			if part.InBlock {
				paid[part.Address] += part.Units
			} else {
				keptByThePool += part.Units
			}
		}
		if parts != block.RewardUnits {
			t.Errorf("the parts of block %d add up to %d units; its reward was %d", block.Height, parts, block.RewardUnits)
		}
	}
	// Three miners, each with a real part of every window: no part was too
	// small for the block, so the pool was paid nothing and owes nothing.
	if keptByThePool != 0 || page.OwedUnits != 0 || page.HeldUnits != 0 || page.InBlockUnits == 0 {
		t.Errorf("the pool kept %d units, owes %d, holds %d; the blocks that count paid %d", keptByThePool, page.OwedUnits, page.HeldUnits, page.InBlockUnits)
	}
	for _, seed := range []*localNode{seedA, seedB} {
		balances := seed.balances()
		if got := balances[poolsAddress]; got != 0 {
			t.Errorf("%s counts %v CVN at the pool's address; its blocks paid the miners", seed.name, got)
		}
		for _, worker := range workers {
			if got, want := balances[worker.address], UnitsAmount(paid[worker.address]).CVN(); got != want || want == 0 {
				t.Errorf("%s counts %v CVN at the miner %s; the pool's blocks paid it %v", seed.name, got, worker.address, want)
			}
		}
	}
	if paid[fastAddress] <= paid[slowAddress] {
		t.Errorf("the blocks paid the fast miner %d units and the slow one %d", paid[fastAddress], paid[slowAddress])
	}
	// In the record, for the blocks that count: earned is what the blocks paid.
	for _, address := range page.Addresses {
		if address.EarnedUnits != address.InBlockUnits || address.OwedUnits != 0 || address.PaidUnits != 0 {
			t.Errorf("the account of %s: %+v", address.Address, address)
		}
	}
	// The paying command has nothing to pay, and says so.
	keyFile := filepath.Join(c.folder, "pool-key.txt")
	if err := os.WriteFile(keyFile, []byte(poolsKey+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := pool.payOut(keyFile); err != nil || !strings.Contains(out, "Nothing to pay") {
		t.Errorf("the paying command at a pool that owes nothing: %v\n%s", err, out)
	}
	for _, node := range []*localNode{seedA, seedB, pool} {
		if log := node.log(); strings.Contains(log, "CONSENSUS REJECTION") || strings.Contains(log, "panic:") {
			t.Errorf("%s refused a block or stopped", node.name)
		}
	}
	// The record the pool leaves behind adds up.
	pool.stopNode()
	if report, err := checkPoolRecordFile(filepath.Join(pool.folder, "pool_"+localnetName+".db")); err != nil {
		t.Errorf("the record the pool left behind does not add up: %v", err)
	} else if report.InBlockUnits != page.InBlockUnits || report.CoinUnits != 0 {
		t.Errorf("the record the pool left behind says: %s", report)
	}
}

func (c *localCluster) pool(name string, number int, seeds []int, address string) *localNode {
	return c.node(name, number, seeds, "--pool", address)
}

// poolPageOf reads a pool's record as it publishes it.
func poolPageOf(pool *localNode) (poolPage, bool) {
	var page poolPage
	ok := pool.get("/pool.json", &page)
	return page, ok
}

// payOut runs the paying command beside a pool, as its operator's timer
// would, with the pool's key in a file. It returns what the command printed.
func (n *localNode) payOut(keyFile string) (string, error) {
	command := exec.Command(n.cluster.program, "--network", localnetName, "--pool-pay", "--password-file", keyFile)
	command.Dir = n.folder
	command.Env = n.env
	out, err := command.CombinedOutput()
	return string(out), err
}

// TestMultiNodeAPool is a pool with three miners of different speeds, on a
// network with two seeds. Blocks are found and counted once they are six
// deep; the paying command pays what is owed; and at the end every unit is
// accounted for, in the pool's record and on the chain.
func TestMultiNodeAPool(t *testing.T) {
	c := poolCluster(t)
	const wait = 5 * time.Minute
	poolsKey, _, poolsAddress := newTestWallet(t)
	_, _, fastAddress := newTestWallet(t)
	_, _, middleAddress := newTestWallet(t)
	_, _, slowAddress := newTestWallet(t)

	seedA := c.seed("seed-a", 1, nil).startNode()
	seedB := c.seed("seed-b", 2, []int{1}).startNode()
	c.waitForHeight(wait, 0, seedA, seedB)
	pool := c.pool("pool", 5, []int{1, 2}, poolsAddress).startNode()
	c.eventually(wait, "the pool did not publish its record", func() bool {
		page, ok := poolPageOf(pool)
		return ok && page.PoolAddress == poolsAddress
	})

	fast := startPoolWorker(t, pool, fastAddress, 0)
	middle := startPoolWorker(t, pool, middleAddress, 40*time.Millisecond)
	slow := startPoolWorker(t, pool, slowAddress, 160*time.Millisecond)
	workers := []*poolWorker{fast, middle, slow}
	minersAreWell := func() {
		for _, worker := range workers {
			if fault := worker.problem(); fault != "" {
				t.Fatalf("a miner was told: %s", fault)
			}
		}
	}

	c.eventually(wait, "the pool did not see its three miners", func() bool {
		page, _ := poolPageOf(pool)
		return page.Miners == 3
	})
	// Mine until four of the pool's blocks count.
	c.eventually(wait, "four of the pool's blocks did not come to count", func() bool {
		minersAreWell()
		page, _ := poolPageOf(pool)
		return page.BlocksConfirmed >= 4
	})

	// Pay what is owed, with the pool's key in a file as a timer would have
	// it. A key that is not the pool's signs nothing.
	strangersKey, _, _ := newTestWallet(t)
	wrongFile := filepath.Join(c.folder, "wrong-key.txt")
	keyFile := filepath.Join(c.folder, "pool-key.txt")
	if err := os.WriteFile(wrongFile, []byte(strangersKey+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte(poolsKey+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := pool.payOut(wrongFile); err == nil || !strings.Contains(out, "Nothing was signed") {
		t.Fatalf("the paying command with a stranger's key: %v\n%s", err, out)
	}
	if page, _ := poolPageOf(pool); len(page.Payments) != 0 && page.Payments[0].State != poolPaymentReturned {
		t.Fatalf("after a stranger's key the pool shows a payment that is %s", page.Payments[0].State)
	}
	out, err := pool.payOut(keyFile)
	if err != nil || !strings.Contains(out, "All sent") {
		t.Fatalf("the paying command: %v\n%s", err, out)
	}
	if strings.Contains(out, poolsKey) {
		t.Error("the paying command printed the pool's key")
	}
	// The payment is mined by the pool's own miners and, six blocks on, settled.
	c.eventually(wait, "the payment did not settle", func() bool {
		minersAreWell()
		page, _ := poolPageOf(pool)
		return page.PaymentsSettled >= 1
	})
	for _, worker := range workers {
		worker.halt()
		if atomic.LoadInt64(&worker.counted) == 0 {
			t.Errorf("the miner at %s had no share counted", worker.address)
		}
	}

	// The chain now stands still. Every node must hold the same one.
	c.eventually(wait, "the pool still counts miners that have left", func() bool {
		page, _ := poolPageOf(pool)
		return page.Miners == 0
	})
	height := seedA.height()
	c.waitForHeight(wait, height, seedB, pool)
	c.eventually(wait, "the nodes do not hold the same newest block", func() bool {
		_, a, okA := seedA.tip()
		_, b, okB := seedB.tip()
		_, p, okP := pool.tip()
		return okA && okB && okP && a == b && b == p
	})
	height = seedA.height()

	// Everything deep enough has been decided, one way or the other.
	var page poolPage
	c.eventually(wait, "blocks six deep were left undecided", func() bool {
		page, _ = poolPageOf(pool)
		for _, block := range page.Blocks {
			if block.State == poolBlockWaiting && block.Height+6 <= height {
				return false
			}
		}
		return page.Height == height
	})
	if page.BlocksFound != int(height) || len(page.Blocks) != page.BlocksFound {
		t.Fatalf("the chain has %d blocks and the pool found %d (%d shown): something else mined, or a block went unrecorded", height, page.BlocksFound, len(page.Blocks))
	}
	if page.BlocksLost != 0 {
		t.Errorf("%d of the pool's blocks were lost on a network where nothing else mines", page.BlocksLost)
	}

	// Every block the pool recorded is the block the seeds hold at that
	// height, and its reward is divided to the unit.
	var confirmedUnits, allUnits int64
	for _, block := range page.Blocks {
		if got := seedA.hashAt(block.Height); got != block.Hash {
			t.Errorf("block #%d: the pool recorded %.16s and the seed holds %.16s", block.Height, block.Hash, got)
		}
		var parts int64
		for _, part := range block.Parts {
			parts += part.Units
		}
		if parts != block.RewardUnits || block.RewardUnits <= 0 {
			t.Errorf("block #%d: its parts add up to %d units of a reward of %d", block.Height, parts, block.RewardUnits)
		}
		allUnits += block.RewardUnits
		if block.State == poolBlockConfirmed {
			confirmedUnits += block.RewardUnits
			if block.Height+6 > height {
				t.Errorf("block #%d counts with fewer than six blocks on it (the chain is at #%d)", block.Height, height)
			}
		}
	}

	// The payments: every one that was sent is in a block, and what each
	// address received on the chain is what the payments say it received.
	received := map[string]int64{}
	var debited, fees int64
	for _, payment := range page.Payments {
		if payment.State == poolPaymentReturned {
			continue // the stranger's key
		}
		if payment.State != poolPaymentSettled && payment.MinedAtHeight == 0 {
			t.Errorf("payment %d is %s and in no block, with the chain standing still", payment.Serial, payment.State)
			continue
		}
		var debits, receives int64
		for _, line := range payment.Lines {
			debits += line.Debit
			receives += line.Receives
			received[line.Address] += line.Receives
		}
		if debits != payment.Units || debits-receives != payment.FeeUnits || payment.FeeUnits != cvn("0.001") {
			t.Errorf("payment %d: debited %d, received %d, fee %d", payment.Serial, debits, receives, payment.FeeUnits)
		}
		debited += debits
		fees += payment.FeeUnits
	}
	if debited == 0 {
		t.Fatal("no payment was made")
	}

	// The pool's record adds up: what each address earned is what it has
	// been paid, what is on its way and what it is still owed; and what was
	// earned in all is the rewards of the blocks that count.
	earned := map[string]int64{}
	var earnedInAll, paidAndOnItsWay int64
	// The page lists an address while it has recent work or is owed
	// something. A miner that has been paid in full, and whose work has
	// left the window, is not listed, so each miner the page does not list
	// is asked for by its address: what it earned counts all the same.
	accounts := append([]poolPageAddress(nil), page.Addresses...)
	listed := map[string]bool{}
	for _, address := range accounts {
		listed[address.Address] = true
	}
	for _, worker := range workers {
		if listed[worker.address] {
			continue
		}
		var one poolPageAddress
		if !pool.get("/pool.json?address="+worker.address, &one) || one.Address != worker.address {
			t.Fatalf("the pool did not answer for the miner at %s, which its page does not list", worker.address)
		}
		// The page may leave out only an address that is owed nothing and
		// has nothing on its way.
		if one.OwedUnits != 0 || one.PendingUnits != 0 {
			t.Errorf("the pool's page does not list the miner at %s, which is owed %d units with %d on their way", worker.address, one.OwedUnits, one.PendingUnits)
		}
		accounts = append(accounts, one)
	}
	if page.AddressesNotShown != 0 {
		t.Errorf("the pool's page left %d address(es) out for want of room, with three miners", page.AddressesNotShown)
	}
	for _, address := range accounts {
		earned[address.Address] = address.EarnedUnits
		earnedInAll += address.EarnedUnits
		paidAndOnItsWay += address.PaidUnits + address.PendingUnits
		if address.EarnedUnits != address.PaidUnits+address.PendingUnits+address.OwedUnits {
			t.Errorf("%s: earned %d, paid %d, on its way %d, owed %d", address.Address, address.EarnedUnits, address.PaidUnits, address.PendingUnits, address.OwedUnits)
		}
	}
	if earnedInAll != confirmedUnits || page.EarnedUnits != confirmedUnits || confirmedUnits == 0 {
		t.Errorf("the blocks that count paid %d units; %d were earned (the pool says %d)", confirmedUnits, earnedInAll, page.EarnedUnits)
	}
	if paidAndOnItsWay != debited {
		t.Errorf("the payments debited %d units; the accounts show %d paid or on its way", debited, paidAndOnItsWay)
	}

	// On the chain: the pool's address holds every reward less what it paid
	// out, and each miner holds what it received, on every node.
	if page.HeldUnits != allUnits-debited {
		t.Errorf("the pool's address holds %d units by the pool's count; its blocks paid %d and it paid out %d", page.HeldUnits, allUnits, debited)
	}
	for _, seed := range []*localNode{seedA, seedB} {
		balances := seed.balances()
		if got, want := balances[poolsAddress], UnitsAmount(allUnits-debited).CVN(); got != want {
			t.Errorf("%s counts %v CVN at the pool's address, want %v", seed.name, got, want)
		}
		for _, worker := range workers {
			if got, want := balances[worker.address], UnitsAmount(received[worker.address]).CVN(); got != want {
				t.Errorf("%s counts %v CVN at the miner %s; the payments say it received %v", seed.name, got, worker.address, want)
			}
		}
	}
	// Each miner earned something, and the fast one more than the slow one.
	for _, worker := range workers {
		if earned[worker.address] <= 0 {
			t.Errorf("the miner at %s earned nothing", worker.address)
		}
	}
	if earned[fastAddress] <= earned[slowAddress] {
		t.Errorf("the fast miner earned %d units and the slow one %d", earned[fastAddress], earned[slowAddress])
	}
	if received[fastAddress] <= 0 {
		t.Error("the fast miner was paid nothing")
	}
	// One address asked for by itself gets the same answer.
	var one poolPageAddress
	if !pool.get("/pool.json?address="+fastAddress, &one) || one.EarnedUnits != earned[fastAddress] {
		t.Errorf("asked for alone, the fast miner earned %d; the list says %d", one.EarnedUnits, earned[fastAddress])
	}
	// A second run ends well, whether or not there is more to pay by now.
	if out, err := pool.payOut(keyFile); err != nil {
		t.Errorf("a second run of the paying command: %v\n%s", err, out)
	}
	for _, node := range []*localNode{seedA, seedB, pool} {
		if log := node.log(); strings.Contains(log, "CONSENSUS REJECTION") || strings.Contains(log, "panic:") {
			t.Errorf("%s refused a block or stopped", node.name)
		}
	}
	log := pool.log()
	for _, want := range []string{"counts: 6 blocks stand on it", "sent:", "is settled:"} {
		if !strings.Contains(log, want) {
			t.Errorf("the pool's log does not say %q", want)
		}
	}
	if strings.Contains(log, poolsKey) {
		t.Error("the pool's log holds the pool's key")
	}

	// The record the pool leaves behind passes the check a copy of it has
	// to pass, and the real program says the same of a copy.
	pool.stopNode()
	record := filepath.Join(pool.folder, "pool_"+localnetName+".db")
	report, err := checkPoolRecordFile(record)
	if err != nil {
		t.Fatalf("the record the pool left behind does not add up: %v", err)
	}
	if report.EarnedUnits != confirmedUnits || report.Confirmed == 0 || report.Payments == 0 {
		t.Errorf("the record the pool left behind says: %s", report)
	}
	duplicate := filepath.Join(c.folder, "a-copy-of-the-record.db")
	if err := copyFile(record, duplicate); err != nil {
		t.Fatal(err)
	}
	if out, err := pool.command("--check-pool-backup", duplicate); err != nil || !strings.Contains(out, "it adds up") {
		t.Errorf("the program asked to check a copy of the record: %v\n%s", err, out)
	}
}

// command runs the program once in a node's folder, with the node's network
// and settings and the options given, and returns what it printed.
func (n *localNode) command(options ...string) (string, error) {
	command := exec.Command(n.cluster.program, append([]string{"--network", localnetName}, options...)...)
	command.Dir = n.folder
	command.Env = n.env
	out, err := command.CombinedOutput()
	return string(out), err
}

// allDeepBlocksDecided reports whether a pool has decided every block of its
// own that has six blocks on it, with the chain at `height`.
func allDeepBlocksDecided(page poolPage, height int64) bool {
	for _, block := range page.Blocks {
		if block.State == poolBlockWaiting && block.Height+6 <= height {
			return false
		}
	}
	return page.Height == height
}

// poolCopies lists the copies a pool has made of its record, oldest first.
func poolCopies(pool *localNode) []string {
	copies, _ := filepath.Glob(filepath.Join(pool.folder, backupFolder, "pool_"+localnetName+"_*.db"))
	sort.Strings(copies)
	return copies
}

// TestMultiNodeAnOlderCopyOfThePoolsRecord loses a real pool's record and
// puts back a copy the pool made before it paid anybody. The pool, started
// again, finds its payments in the chain and takes them into the record
// before it pays, so that what was paid is not paid a second time.
func TestMultiNodeAnOlderCopyOfThePoolsRecord(t *testing.T) {
	c := poolCluster(t)
	const wait = 6 * time.Minute
	poolsKey, _, poolsAddress := newTestWallet(t)
	_, _, fastAddress := newTestWallet(t)
	_, _, slowAddress := newTestWallet(t)

	seed := c.seed("seed-a", 1, nil).startNode()
	c.waitForHeight(wait, 0, seed)
	pool := c.pool("pool", 5, []int{1}, poolsAddress)
	// This pool makes copies of its record, as a real one does.
	var options []string
	for _, option := range pool.args {
		if option != "--no-backups" {
			options = append(options, option)
		}
	}
	pool.args = options
	pool.startNode()
	c.eventually(wait, "the pool did not publish its record", func() bool {
		page, ok := poolPageOf(pool)
		return ok && page.PoolAddress == poolsAddress
	})
	workers := []*poolWorker{startPoolWorker(t, pool, fastAddress, 0), startPoolWorker(t, pool, slowAddress, 80*time.Millisecond)}

	// The first copy is made a minute after the pool starts. It is the
	// older copy of this test: it knows of no payment.
	c.eventually(wait, "the pool made no copy of its record", func() bool { return len(poolCopies(pool)) > 0 })
	older := poolCopies(pool)[0]
	if report, err := checkPoolRecordFile(older); err != nil || report.Payments != 0 {
		t.Fatalf("the pool's first copy: %d payment(s), %v", report.Payments, err)
	}
	c.eventually(wait, "four of the pool's blocks did not come to count", func() bool {
		page, _ := poolPageOf(pool)
		return page.BlocksConfirmed >= 4
	})
	keyFile := filepath.Join(c.folder, "pool-key.txt")
	if err := os.WriteFile(keyFile, []byte(poolsKey+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := pool.payOut(keyFile); err != nil || !strings.Contains(out, "All sent") {
		t.Fatalf("the paying command: %v\n%s", err, out)
	}
	c.eventually(wait, "the payment did not settle", func() bool {
		page, _ := poolPageOf(pool)
		return page.PaymentsSettled >= 1 && page.PendingUnits == 0
	})
	// Straight after payments are sent the pool makes another copy.
	c.eventually(wait, "the pool made no copy after its payments were sent", func() bool { return len(poolCopies(pool)) >= 2 })

	// The miners leave and the chain stands still. What the record says
	// now is the truth this test holds the record put back against.
	for _, worker := range workers {
		worker.halt()
	}
	c.eventually(wait, "the pool still counts miners that have left", func() bool {
		page, _ := poolPageOf(pool)
		return page.Miners == 0
	})
	height := seed.height()
	c.waitForHeight(wait, height, pool)
	height = seed.height()
	var truth poolPage
	c.eventually(wait, "blocks six deep were left undecided", func() bool {
		truth, _ = poolPageOf(pool)
		return allDeepBlocksDecided(truth, height)
	})
	paidBefore, owedBefore := make(map[string]int64), make(map[string]int64)
	for _, address := range truth.Addresses {
		paidBefore[address.Address], owedBefore[address.Address] = address.PaidUnits, address.OwedUnits
	}
	if truth.PaidUnits == 0 || truth.PendingUnits != 0 {
		t.Fatalf("before the record is lost: %d units paid, %d on their way", truth.PaidUnits, truth.PendingUnits)
	}

	// The record is lost. The older copy is put back with the program's
	// own command, which will not do it under a running pool.
	if out, err := pool.command("--restore-pool-backup", older); err == nil || !strings.Contains(out, "Stop it first") {
		t.Errorf("putting a copy back under the running pool: %v\n%s", err, out)
	}
	pool.stopNode()
	if out, err := pool.command("--restore-pool-backup", older); err != nil || !strings.Contains(out, "Restored") {
		t.Fatalf("putting the older copy back: %v\n%s", err, out)
	}
	pool.startNode()

	// Started again, the pool takes its own payments in from the chain.
	var after poolPage
	c.eventually(wait, "the pool did not take its payments in from the chain", func() bool {
		var ok bool
		after, ok = poolPageOf(pool)
		return ok && after.TransfersNotInRecord == 0 && after.PaidUnits == truth.PaidUnits && allDeepBlocksDecided(after, height)
	})
	fromChain := 0
	for _, payment := range after.Payments {
		if payment.FromChain {
			fromChain++
		}
	}
	if fromChain == 0 {
		t.Error("the page shows no payment found in the chain")
	}
	for _, address := range after.Addresses {
		if address.PaidUnits != paidBefore[address.Address] {
			t.Errorf("%s had been paid %d units; the record put back now says %d", address.Address, paidBefore[address.Address], address.PaidUnits)
		}
		// Never more than the lost record owed: the copy cannot know of
		// blocks found after it, and nothing has been mined since.
		if address.OwedUnits+address.PendingUnits > owedBefore[address.Address] {
			t.Errorf("%s was owed %d units by the lost record; the record put back owes it %d with %d on their way",
				address.Address, owedBefore[address.Address], address.OwedUnits, address.PendingUnits)
		}
		if address.EarnedUnits != address.OwedUnits+address.PendingUnits+address.PaidUnits {
			t.Errorf("the account of %s does not add up: %+v", address.Address, address)
		}
	}
	// On the chain each miner holds what it held before: the paying
	// command, run now, pays at most what is still owed and nothing twice.
	before := seed.balances()
	if out, err := pool.payOut(keyFile); err != nil {
		t.Errorf("the paying command after the copy was put back: %v\n%s", err, out)
	}
	final, _ := poolPageOf(pool)
	var owedAfterRestore int64
	for _, address := range after.Addresses {
		owedAfterRestore += address.OwedUnits
	}
	if final.PendingUnits > owedAfterRestore {
		t.Errorf("%d units were sent on their way; only %d were owed", final.PendingUnits, owedAfterRestore)
	}
	for _, worker := range workers {
		if got := seed.balances()[worker.address]; got != before[worker.address] {
			t.Errorf("the chain stands still, and %s went from %v CVN to %v", worker.address, before[worker.address], got)
		}
	}
	log := pool.log()
	if !strings.Contains(log, "has been taken into it as payment") {
		t.Error("the pool's log does not say it took a payment in from the chain")
	}
	if strings.Contains(log, "panic:") || strings.Contains(log, poolsKey) {
		t.Error("the pool stopped, or its log holds the pool's key")
	}
	// And the record it leaves behind adds up.
	pool.stopNode()
	if report, err := checkPoolRecordFile(filepath.Join(pool.folder, "pool_"+localnetName+".db")); err != nil {
		t.Errorf("the record put back and brought up to date does not add up: %v", err)
	} else if report.PaidUnits != truth.PaidUnits {
		t.Errorf("the record put back says %d units were paid; %d were", report.PaidUnits, truth.PaidUnits)
	}
}

// TestMultiNodeAPoolLosesABlock is a pool that mined two blocks cut off from
// the network. When it comes back the network's chain replaces them, and
// once that is beyond doubt the pool records them as lost and owes nothing
// for them.
func TestMultiNodeAPoolLosesABlock(t *testing.T) {
	c := poolCluster(t)
	const wait = 4 * time.Minute
	_, _, mainAddress := newTestWallet(t)
	_, _, strayAddress := newTestWallet(t)
	_, _, minerAddress := newTestWallet(t)
	_, _, strayMinerAddress := newTestWallet(t)

	seedA := c.seed("seed-a", 1, nil).startNode()
	seedB := c.seed("seed-b", 2, []int{1}).startNode()
	c.waitForHeight(wait, 0, seedA, seedB)
	mainPool := c.pool("pool", 5, []int{1, 2}, mainAddress).startNode()
	miner := startPoolWorker(t, mainPool, minerAddress, 0)

	// Cut off: this pool knows of no other node.
	stray := c.pool("stray-pool", 6, nil, strayAddress).startNode()
	strayMiner := startPoolWorker(t, stray, strayMinerAddress, 0)
	c.eventually(wait, "the cut-off pool did not find two blocks", func() bool {
		page, _ := poolPageOf(stray)
		return page.BlocksFound >= 2
	})
	strayMiner.halt()
	var own poolPage
	c.eventually(wait, "the cut-off pool did not come to rest", func() bool {
		page, ok := poolPageOf(stray)
		own = page
		return ok && page.Miners == 0 && page.Height == stray.height()
	})
	if own.BlocksFound < 2 || own.BlocksWaiting != own.BlocksFound || own.OwedUnits != 0 {
		t.Fatalf("cut off, the pool holds %d blocks, %d waiting, %d units owed", own.BlocksFound, own.BlocksWaiting, own.OwedUnits)
	}
	ownHeight := own.Height
	ownFirst := stray.hashAt(1)
	if ownHeight > 5 {
		t.Fatalf("the cut-off pool mined %d blocks before it was stopped; more than five could not be replaced", ownHeight)
	}

	// The network's chain must be the longer one before the pool returns.
	c.waitForHeight(wait, ownHeight+3, seedA, seedB)
	stray.stopNode()
	stray.args = []string{"--network", localnetName, "--no-backups", "--pool", strayAddress, "--seeds", c.host(1) + "," + c.host(2)}
	stray.startNode()
	c.eventually(wait, "the returning pool did not take the network's chain", func() bool {
		return stray.hashAt(1) != "" && stray.hashAt(1) == seedA.hashAt(1)
	})
	if ownFirst == "" || ownFirst == seedA.hashAt(1) {
		t.Fatalf("the cut-off pool's block #1 was %.16s, the same as the network's: it was never on a chain of its own", ownFirst)
	}

	// Its own blocks stay "waiting" until six blocks stand above where they
	// were. Then they are lost, and nothing is owed for them.
	c.eventually(wait, "the returning pool did not record its replaced blocks as lost", func() bool {
		if fault := miner.problem(); fault != "" {
			t.Fatalf("the network's miner was told: %s", fault)
		}
		page, ok := poolPageOf(stray)
		own = page
		return ok && page.BlocksLost == page.BlocksFound && page.BlocksFound >= 2
	})
	miner.halt()
	if own.BlocksConfirmed != 0 || own.BlocksWaiting != 0 || own.OwedUnits != 0 || own.EarnedUnits != 0 {
		t.Errorf("after its blocks were replaced the pool records %d confirmed, %d waiting, %d units owed", own.BlocksConfirmed, own.BlocksWaiting, own.OwedUnits)
	}
	for _, address := range own.Addresses {
		if address.OwedUnits != 0 {
			t.Errorf("%s is owed %d units for blocks the network never had", address.Address, address.OwedUnits)
		}
	}
	if got := seedA.balances()[strayAddress]; got != 0 {
		t.Errorf("the network counts %v CVN at the cut-off pool's address", got)
	}
	if !strings.Contains(stray.log(), "was replaced by another block") {
		t.Error("the returning pool's log does not say its blocks were replaced")
	}
	// The pool that stayed on the network lost nothing. It settles its
	// blocks every fifteen seconds, so the first may take that long to count.
	var mainPage poolPage
	c.eventually(wait, "the network's own pool counted none of its blocks", func() bool {
		mainPage, _ = poolPageOf(mainPool)
		return mainPage.BlocksConfirmed > 0
	})
	if mainPage.BlocksLost != 0 {
		t.Errorf("the network's own pool records %d lost and %d confirmed", mainPage.BlocksLost, mainPage.BlocksConfirmed)
	}
}

// TestMultiNodeTheRealFastMinerAtAPool runs the real fast miner, through
// `--pool-mine`, against a real pool beside a seed. Everything else here
// uses stand-in miners; this is the one place the program a miner really
// runs meets the pool.
//
// The fast miner is not part of this repository, so the test runs only when
// CVN_REAL_FAST_MINER names its file:
//
//	CVN_REAL_FAST_MINER=/path/to/xmrig go test -tags multinode -run TheRealFastMiner -v ./node/
//
// On Windows, expect the antivirus to step in. A program built a moment ago,
// in the temporary folder, that starts mining software pointed at a pool is
// what coin-mining malware looks like. Microsoft Defender ended this test
// after half a minute, and then refused to start the multi-node test program
// at all until its source had changed. Run it on Linux, or on a machine
// where you have decided for yourself what the antivirus should allow.
func TestMultiNodeTheRealFastMinerAtAPool(t *testing.T) {
	fastMiner := os.Getenv("CVN_REAL_FAST_MINER")
	if fastMiner == "" {
		t.Skip("CVN_REAL_FAST_MINER does not name the CVN edition of XMRig, so the real miner is not tried")
	}
	c := poolCluster(t)
	const wait = 10 * time.Minute
	_, _, poolsAddress := newTestWallet(t)
	_, _, minersAddress := newTestWallet(t)

	seed := c.seed("seed-a", 1, nil).startNode()
	c.waitForHeight(wait, 0, seed)
	pool := c.pool("pool", 5, []int{1}, poolsAddress).startNode()
	c.eventually(wait, "the pool did not publish its record", func() bool {
		page, ok := poolPageOf(pool)
		return ok && page.PoolAddress == poolsAddress
	})

	// A miner's machine: the program, the fast miner and an address. No
	// seeds are named, because it joins no network.
	miner := c.node("miner", 9, nil, "--pool-mine", pool.host, "--miner-address", minersAddress,
		"--xmrig-path", fastMiner, "--threads", "1").startNode()
	stillMining := func() {
		t.Helper()
		if !miner.running() {
			t.Fatalf("the program mining at the pool ended by itself:\n%s", miner.log())
		}
	}

	c.eventually(wait, "the pool counted no work from the real miner", func() bool {
		stillMining()
		page, _ := poolPageOf(pool)
		for _, address := range page.Addresses {
			if address.Address == minersAddress && address.Work > 0 {
				return true
			}
		}
		return false
	})
	c.eventually(wait, "no block found with the real miner came to count", func() bool {
		stillMining()
		page, _ := poolPageOf(pool)
		return page.BlocksConfirmed >= 2
	})
	// Once a minute the program says how it is doing, with what the pool
	// has recorded for its address.
	c.eventually(wait, "the program never reported its shares and what it is owed", func() bool {
		stillMining()
		said := miner.log()
		return strings.Contains(said, "shares taken") && strings.Contains(said, "the pool owes this address")
	})

	said := miner.log()
	for _, want := range []string{"MINING AT A POOL", "The pool says", "takes no fee", "Connected to the pool", "first share", "H/s"} {
		if !strings.Contains(said, want) {
			t.Errorf("the program mining at the pool never said %q", want)
		}
	}
	if strings.Contains(said, "shares taken 0") {
		t.Error("the program counted no shares while the pool counted its work: it does not understand what the fast miner prints")
	}

	// The pool's record and the seed agree on the blocks, and the one miner
	// earned all of what counts.
	page, _ := poolPageOf(pool)
	for _, block := range page.Blocks {
		if block.State == poolBlockConfirmed && seed.hashAt(block.Height) != block.Hash {
			t.Errorf("the pool counts block %d, which is not the block the seed holds at that height", block.Height)
		}
	}
	var account poolPageAddress
	if !pool.get("/pool.json?address="+minersAddress, &account) || account.EarnedUnits <= 0 {
		t.Errorf("the pool has recorded nothing earned for the real miner: %+v", account)
	}

	// Closing the program takes the fast miner with it.
	miner.stopNode()
	c.eventually(wait, "the pool still counts a miner after the program was closed: the fast miner outlived it", func() bool {
		after, _ := poolPageOf(pool)
		return after.Miners == 0
	})
	t.Logf("what the program printed while it mined at the pool:\n%s", said)
}

// TestMultiNodePoolOptionsThatContradict starts the real program with
// options that cannot both be meant. It says which, and ends, and no node
// is started.
func TestMultiNodePoolOptionsThatContradict(t *testing.T) {
	c := poolCluster(t)
	_, _, address := newTestWallet(t)
	for _, attempt := range []struct {
		options []string
		says    string
	}{
		// A pool, and a miner at somebody else's pool.
		{[]string{"--pool", address, "--pool-mine", "127.0.0.1"}, "Give one of the two"},
		// A node that does not mine, asked to mine at a pool.
		{[]string{"--no-mining", "--pool-mine", "127.0.0.1"}, "Give one of the two"},
		// The option with nothing after it, and with something that is not a machine.
		{[]string{"--miner-address", address, "--pool-mine"}, "--pool-mine needs the pool's address"},
		{[]string{"--miner-address", address, "--pool-mine", "http://127.0.0.1/pool"}, "is not a pool's address"},
		// A pool to mine at, and no fast miner to do it with.
		{[]string{"--miner-address", address, "--pool-mine", "127.0.0.1"}, "needs the fast miner"},
	} {
		n := c.node("refused", 3, nil, attempt.options...)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		command := exec.CommandContext(ctx, c.program, n.args...)
		command.Dir = n.folder
		command.Env = n.env
		out, err := command.CombinedOutput()
		ranOutOfTime := ctx.Err() != nil
		cancel()
		if err == nil || ranOutOfTime || !strings.Contains(string(out), attempt.says) {
			t.Errorf("started with %v the program should end saying %q. It ended with %v and said:\n%s", attempt.options, attempt.says, err, out)
		}
		if strings.Contains(string(out), "Listening for other nodes") {
			t.Errorf("started with %v the program began to run a node", attempt.options)
		}
	}
}
