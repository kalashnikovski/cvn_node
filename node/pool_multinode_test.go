//go:build multinode

package node

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
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
func poolCluster(t *testing.T) *localCluster {
	c := newLocalCluster(t)
	c.env = append(c.env, "CVN_LOCALNET_RANDOMX=1", "CVN_LOCALNET_REORG_LIMIT=6", "CVN_LOCALNET_POOL_SHARE=4")
	c.start = 48 // a block takes about fifty hashes to begin with
	return c
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

// TestMultiNodeAPool is a pool with three miners of different speeds, on a
// network with two seeds. Blocks are found, counted once they are six deep,
// and their rewards owed to the miners; at the end every unit is accounted
// for.
func TestMultiNodeAPool(t *testing.T) {
	c := poolCluster(t)
	const wait = 4 * time.Minute
	_, _, poolsAddress := newTestWallet(t)
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

	c.eventually(wait, "the pool did not see its three miners", func() bool {
		page, _ := poolPageOf(pool)
		return page.Miners == 3
	})
	// Mine until four of the pool's blocks count.
	c.eventually(wait, "four of the pool's blocks did not come to count", func() bool {
		for _, worker := range workers {
			if fault := worker.problem(); fault != "" {
				t.Fatalf("a miner was told: %s", fault)
			}
		}
		page, _ := poolPageOf(pool)
		return page.BlocksConfirmed >= 4
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

	// What is owed is exactly the rewards of the blocks that count. Nothing
	// has been paid, and nothing is owed for a block still waiting.
	owed := map[string]int64{}
	var owedInAll int64
	for _, address := range page.Addresses {
		owed[address.Address] = address.OwedUnits
		owedInAll += address.OwedUnits
		if address.EarnedUnits != address.OwedUnits || address.PaidUnits != 0 {
			t.Errorf("%s: earned %d, owed %d, paid %d", address.Address, address.EarnedUnits, address.OwedUnits, address.PaidUnits)
		}
	}
	if owedInAll != confirmedUnits || page.OwedUnits != confirmedUnits || confirmedUnits == 0 {
		t.Errorf("the blocks that count paid %d units; %d are owed (the pool says %d)", confirmedUnits, owedInAll, page.OwedUnits)
	}
	// The pool's address holds every reward, on every node.
	if page.HeldUnits != allUnits {
		t.Errorf("the pool's address holds %d units by the pool's count; its blocks paid %d", page.HeldUnits, allUnits)
	}
	for _, seed := range []*localNode{seedA, seedB} {
		if got := seed.balances()[poolsAddress]; got != UnitsAmount(allUnits).CVN() {
			t.Errorf("%s counts %v CVN at the pool's address; its blocks paid %v", seed.name, got, UnitsAmount(allUnits).CVN())
		}
	}
	// Each miner is owed something, and the fast one more than the slow one.
	for _, worker := range workers {
		if owed[worker.address] <= 0 {
			t.Errorf("the miner at %s is owed nothing", worker.address)
		}
	}
	if owed[fastAddress] <= owed[slowAddress] {
		t.Errorf("the fast miner is owed %d units and the slow one %d", owed[fastAddress], owed[slowAddress])
	}
	// One address asked for by itself gets the same answer.
	var one poolPageAddress
	if !pool.get("/pool.json?address="+fastAddress, &one) || one.OwedUnits != owed[fastAddress] {
		t.Errorf("asked for alone, the fast miner is owed %d; the list says %d", one.OwedUnits, owed[fastAddress])
	}
	for _, node := range []*localNode{seedA, seedB, pool} {
		if log := node.log(); strings.Contains(log, "CONSENSUS REJECTION") || strings.Contains(log, "panic:") {
			t.Errorf("%s refused a block or stopped", node.name)
		}
	}
	if !strings.Contains(pool.log(), "counts: 6 blocks stand on it") {
		t.Error("the pool's log does not say that a block came to count")
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
	// The pool that stayed on the network lost nothing.
	if page, _ := poolPageOf(mainPool); page.BlocksLost != 0 || page.BlocksConfirmed == 0 {
		t.Errorf("the network's own pool records %d lost and %d confirmed", page.BlocksLost, page.BlocksConfirmed)
	}
}
