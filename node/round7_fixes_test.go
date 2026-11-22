package node

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Tests for what review round 7 found. Most were written by the reviewer to
// fail on the code as it was; they are kept here, turned round where needed,
// to pass on the code as it is.

// waitForPassesToEnd waits until no pass is running and none is about to
// follow for a block that was missed.
func waitForPassesToEnd(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	quiet := 0
	for quiet < 5 {
		if time.Now().After(deadline) {
			t.Fatal("passes were still running after 20 seconds")
		}
		if atomic.LoadInt32(&syncPassRunning) == 0 && atomic.LoadInt32(&blockMissedDuringPass) == 0 {
			quiet++
		} else {
			quiet = 0
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// forgetExcuses clears what is remembered of sources that sent no blocks.
func forgetExcuses(t *testing.T) {
	t.Helper()
	clear := func() {
		syncExcused.Lock()
		syncExcused.at = make(map[string]time.Time)
		syncExcused.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

// aNodeChangingBranch returns what a node's own handlers say a moment apart:
// its newest block before it takes one off, its newest block after, and its
// answer to a request for the block it took off. The node is left at block 5
// and the chain it held, with block 6, is returned.
func aNodeChangingBranch(t *testing.T) (chain []Block, tipBefore, tipDuring, batchDuring string) {
	t.Helper()
	startTestChain(t, "devnet")
	buildTestChain(t, 6)
	source := syncAPI()
	ask := func(path string) string {
		answer := httptest.NewRecorder()
		source.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, path, nil))
		return answer.Body.String()
	}
	tipBefore = ask("/tip")
	chain = localChain()
	StateMutex.Lock()
	removed := rollbackChainToHeightLocked(5) // the first step of a change of branch
	StateMutex.Unlock()
	if len(removed) != 1 {
		t.Fatalf("%d block(s) were taken off, want 1", len(removed))
	}
	tipDuring = ask("/tip")
	batchDuring = ask("/blocks/batch?start=6")
	var batch []Block
	if err := json.Unmarshal([]byte(batchDuring), &batch); err != nil || len(batch) != 0 {
		t.Fatalf("the source was expected to answer with no blocks: %v, %d block(s)", err, len(batch))
	}
	return chain, tipBefore, tipDuring, batchDuring
}

// REPORT 1. An honest node that is part-way through changing branch has taken
// its own blocks off and not yet put the others on. Asked for its newest
// block a moment before, it said #6; asked for blocks from #6 now, it answers
// with none. It used to be set aside for ten minutes. Now it is asked again,
// and by then it has the block.
func TestRound7ASourceThatIsChangingBranchIsAskedAgain(t *testing.T) {
	t.Chdir(t.TempDir())
	chain, tipBefore, tipDuring, batchDuring := aNodeChangingBranch(t)
	forgetExcuses(t)

	peer := newStandInPeer(t, chain)
	answer := peer.server.Config.Handler
	var tipAsked, batchAsked int32
	peer.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/tip" && atomic.AddInt32(&tipAsked, 1) == 2:
			fmt.Fprint(w, tipDuring) // asked again, in the middle of its change
		case r.URL.Path == "/tip":
			fmt.Fprint(w, tipBefore)
		case r.URL.Path == "/blocks/batch" && atomic.AddInt32(&batchAsked, 1) == 1:
			fmt.Fprint(w, batchDuring)
		default:
			answer.ServeHTTP(w, r) // its change is done: it holds block 6 again
		}
	})
	address := peer.asOnlySeed(t)
	atomic.StoreInt64(&lastPassAfterMiss, 0)

	TriggerActiveReSyncPass()
	waitForPassesToEnd(t)

	if syncSourceIsSetAside(address) {
		t.Error("an honest source that was changing branch when it was asked for blocks was set aside for ten minutes")
	}
	if tip := GetLatestBlockInternal(); tip.Hash != chain[6].Hash {
		t.Errorf("the node ended at block #%d; the pass that follows should have fetched #6", tip.Index)
	}
}

// The other side of report 1. A source that goes on reporting a block and
// never sends it is excused once and no more.
func TestRound7ASourceThatNeverSendsItsBlocksIsExcusedOnlyOnce(t *testing.T) {
	t.Chdir(t.TempDir())
	chain, tipBefore, tipDuring, batchDuring := aNodeChangingBranch(t)
	forgetExcuses(t)

	peer := newStandInPeer(t, chain)
	answer := peer.server.Config.Handler
	var tipAsked, batchAsked int32
	peer.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tip":
			if atomic.AddInt32(&tipAsked, 1)%2 == 0 {
				fmt.Fprint(w, tipDuring)
			} else {
				fmt.Fprint(w, tipBefore)
			}
		case "/blocks/batch":
			atomic.AddInt32(&batchAsked, 1)
			fmt.Fprint(w, batchDuring)
		default:
			answer.ServeHTTP(w, r)
		}
	})
	address := peer.asOnlySeed(t)
	atomic.StoreInt64(&lastPassAfterMiss, 0)

	TriggerActiveReSyncPass()
	waitForPassesToEnd(t)

	if !syncSourceIsSetAside(address) {
		t.Error("a source that reported a block twice and sent it neither time was not set aside")
	}
	if asked := atomic.LoadInt32(&batchAsked); asked != 2 {
		t.Errorf("the source was asked for its blocks %d times, want 2: once, and once more after being excused", asked)
	}
}

// A source that still reports the block and still sends nothing, when asked
// a second time in the same pass, is not excused at all.
func TestRound7ASourceThatStillClaimsABlockItDoesNotSendIsSetAside(t *testing.T) {
	t.Chdir(t.TempDir())
	chain, tipBefore, _, batchDuring := aNodeChangingBranch(t)
	forgetExcuses(t)

	peer := newStandInPeer(t, chain)
	answer := peer.server.Config.Handler
	var batchAsked int32
	peer.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tip":
			fmt.Fprint(w, tipBefore)
		case "/blocks/batch":
			atomic.AddInt32(&batchAsked, 1)
			fmt.Fprint(w, batchDuring)
		default:
			answer.ServeHTTP(w, r)
		}
	})
	address := peer.asOnlySeed(t)
	atomic.StoreInt64(&lastPassAfterMiss, 0)

	TriggerActiveReSyncPass()
	waitForPassesToEnd(t)

	if !syncSourceIsSetAside(address) {
		t.Error("a source that goes on reporting a block and sends none was not set aside")
	}
	if asked := atomic.LoadInt32(&batchAsked); asked != 2 {
		t.Errorf("the source was asked for its blocks %d times, want 2 in the one pass", asked)
	}
}

// madeUpBatch returns `length` blocks that follow the newest block and each
// other, carry their own identifiers and claim difficulty 1, on a chain
// whose minimum is 1. Nothing was mined: at difficulty 1 every hash is a
// proof.
func madeUpBatch(t *testing.T, length int) []Block {
	t.Helper()
	previous := GetLatestBlockInternal()
	batch := make([]Block, 0, length)
	for i := 0; i < length; i++ {
		block := Block{
			Version: 1, Index: previous.Index + 1, Timestamp: previous.Timestamp + 1, PrevHash: previous.Hash,
			Difficulty: 1, UncleHash: emptyUncleHash, UncleMiner: emptyUncleMiner,
		}
		block.Hash = CalculateHash(block)
		batch = append(batch, block)
		previous = block
	}
	return batch
}

// REPORT 2. A reply used to hold any number of blocks, and every one of them
// had its proof worked out, with the node standing still, before the first
// was looked at by the rules, which refuse it at once: it claims difficulty 1
// where they demand far more. Now a reply is cut to what a node sends, and
// no proof is worked out for blocks whose first does not carry the
// difficulty the rules require.
func TestRound7ABatchTheRulesRefuseIsNotHashed(t *testing.T) {
	t.Chdir(t.TempDir())
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 1}})
	Params.HeartbeatInitialDifficulty = 100000
	Params.KeyEpochBlocks, Params.KeyLagBlocks = 1920, 100 // as on devnet and the CVN Testnet
	withProofThreads(t, 2)                                 // a small server

	const length = 4 * syncBatchMaxBlocks
	genesis := GetLatestBlockInternal()
	batch := madeUpBatch(t, length)
	if demanded := CalculateNextDifficulty(genesis, batch[0].Timestamp); demanded <= batch[0].Difficulty {
		t.Fatalf("the rules demand difficulty %d of block 1; the test needs them to demand more than %d", demanded, batch[0].Difficulty)
	}

	peer := newStandInPeer(t, append([]Block{genesis}, batch...))
	peer.claimedWork = "1000000000000000000"
	address := peer.asOnlySeed(t)
	atomic.StoreInt64(&lastPassAfterMiss, 0)

	if got := len(fetchBlockBatch(address, 1)); got != syncBatchMaxBlocks {
		t.Errorf("a reply of %d blocks was taken as %d blocks; a node sends at most %d and no more are taken", length, got, syncBatchMaxBlocks)
	}

	ready := atomic.LoadInt64(&proofsFoundReady)
	began := time.Now()
	TriggerActiveReSyncPass()
	took := time.Since(began)
	waitForPassesToEnd(t)

	if tip := GetLatestBlockInternal(); tip.Index != 0 {
		t.Fatalf("the node took block #%d from a batch the rules refuse", tip.Index)
	}
	if !syncSourceIsSetAside(address) {
		t.Error("the source of the batch was not set aside")
	}
	if took > 5*time.Second {
		t.Errorf("the pass took %v; it used to take over half a minute, hashing blocks the rules refuse", took.Round(time.Millisecond))
	}
	if used := atomic.LoadInt64(&proofsFoundReady) - ready; used != 0 {
		t.Errorf("%d proof(s) were worked out ahead for a batch the rules refuse", used)
	}
	forgetProofs()
	if prepared := prepareProofs(batch); prepared != 0 {
		t.Errorf("%d proofs were worked out for a batch whose first block claims too little difficulty", prepared)
	}
}

// The most a sender can have worked out for nothing: one small group. Its
// first block stands on the newest block and claims the right difficulty,
// and has no proof behind it.
func TestRound7ABlockWithoutItsProofCostsOneSmallGroup(t *testing.T) {
	t.Chdir(t.TempDir())
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 1}})
	Params.HeartbeatInitialDifficulty = 1 << 40
	Params.KeyEpochBlocks, Params.KeyLagBlocks = 1920, 100
	withProofThreads(t, 2)

	genesis := GetLatestBlockInternal()
	previous := genesis
	var batch []Block
	for i := 0; i < syncBatchMaxBlocks; i++ {
		block := Block{
			Version: 1, Index: previous.Index + 1, Timestamp: previous.Timestamp + 1, PrevHash: previous.Hash,
			Difficulty: 1, UncleHash: emptyUncleHash, UncleMiner: emptyUncleMiner,
		}
		if i == 0 {
			block.Difficulty = CalculateNextDifficulty(genesis, block.Timestamp)
		}
		block.Hash = CalculateHash(block)
		batch = append(batch, block)
		previous = block
	}
	if batch[0].Difficulty < 1<<30 {
		t.Fatalf("the rules demand difficulty %d of block 1; the test needs a difficulty no made-up block meets", batch[0].Difficulty)
	}
	forgetProofs()
	if prepared, group := prepareProofs(batch), proofsAtOnce(); prepared > group {
		t.Errorf("%d proofs were worked out for a batch whose first block has no proof; one group is %d", prepared, group)
	}
}

// NEW FINDING 2. A batch could begin with a "block 0" of the sender's making.
// Block 0 is before RandomX begins, so nothing about it was checked, and its
// identifier was then taken as the RandomX key for the blocks after it: the
// node built a 256 MB RandomX cache for a key the sender chose.
func TestRound7ABatchDoesNotNameItsOwnKey(t *testing.T) {
	t.Chdir(t.TempDir())
	timestamp := startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 4}})
	withProofThreads(t, 4)
	for i := 0; i < 2; i++ {
		*timestamp += 45
		extendTestChain(t, *timestamp, testAddress("miner"), nil)
	}

	senderKey := strings.Repeat("5a", 32)
	batch := []Block{{Index: 0, Hash: senderKey}}
	previous := batch[0]
	for i := 1; i <= 3; i++ {
		block := Block{
			Version: 1, Index: int64(i), Timestamp: Params.GenesisTimestamp + int64(i), PrevHash: previous.Hash,
			Difficulty: 4, UncleHash: emptyUncleHash, UncleMiner: emptyUncleMiner,
		}
		block.Hash = CalculateHash(block)
		batch = append(batch, block)
		previous = block
	}

	prepareProofs(batch)

	chosen, _ := hex.DecodeString(senderKey)
	rxVerifier.Lock()
	built := false
	for _, engine := range rxVerifier.engines {
		built = built || engine.key == string(chosen)
	}
	rxVerifier.Unlock()
	if built {
		t.Errorf("a RandomX engine was built for a key the sender of the batch chose (%s)", senderKey)
	}
}

// REPORT 5. While the newest block is dated ahead of the clock the work
// server hands out no work, so no miner can log in. The time a started fast
// miner is given to connect used to run on regardless.
func TestRound7TheTimeToConnectWaitsWhileTheNodeWaitsForTheClock(t *testing.T) {
	t.Chdir(t.TempDir())
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 4}})
	// A block dated far enough ahead that work is refused for about five
	// seconds: longer than the time the miner is given to connect.
	extendTestChain(t, time.Now().Unix()+HeartbeatMaxFutureDriftSeconds-minerClockMargin+5, testAddress("miner"), nil)
	startWorkServerForTest(t)

	saved := fastMinerConnectDeadline
	fastMinerConnectDeadline = 2 * time.Second
	t.Cleanup(func() { fastMinerConnectDeadline = saved })
	program, arguments := standInArguments(t, "idle")
	if _, handingOut := buildStratumJob(testAddress("stand-in miner")); handingOut {
		t.Fatal("the work server is handing out work on a block dated ahead; the test needs it to refuse")
	}
	if !workWaitsForClock() {
		t.Fatal("the node does not say it is waiting for the clock")
	}

	started := time.Now()
	err := runFastMinerOnce(program, arguments)
	took := time.Since(started)
	_, handingOut := buildStratumJob(testAddress("stand-in miner"))
	t.Logf("the fast miner was stopped after %v (%v); the work server is handing out work by then: %v", took.Round(100*time.Millisecond), err, handingOut)
	if !handingOut {
		t.Errorf("the fast miner was stopped as one that never connected after %v, while the node was still refusing to hand out work", took.Round(100*time.Millisecond))
	}
	if took > 30*time.Second {
		t.Errorf("it took %v to stop a miner that never connected", took.Round(time.Second))
	}
}

// REPORT 4. A fast miner that ran for more than a minute before it failed,
// every time, was restarted for ever, and the built-in miner never took over.
func TestRound7AFastMinerThatKeepsStoppingIsGivenUpOn(t *testing.T) {
	savedPause, savedQuick := fastMinerRestartPause, fastMinerQuickFailure
	fastMinerRestartPause = 50 * time.Millisecond
	fastMinerQuickFailure = 0 // no run is short enough to count as a failure to start
	t.Cleanup(func() { fastMinerRestartPause, fastMinerQuickFailure = savedPause, savedQuick })
	program, arguments := standInArguments(t, "exit")

	done := make(chan struct{})
	go func() { superviseFastMiner(program, arguments); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("a miner that stops every time was restarted for ever")
	}
	if atomic.LoadInt32(&fastMinerActive) != 0 {
		t.Error("the built-in miner was not released after the fast miner kept stopping")
	}
}

// NEW FINDING 3. Thirty-two addresses, each announcing eight blocks that do
// not exist, filled the table of blocks being fetched. An honest neighbour
// that then announced a real block was not asked for it, and a large block
// is sent only to nodes that ask.
func TestRound7AFullTableOfAnnouncedBlocksMakesRoom(t *testing.T) {
	onNetwork(t, "devnet")
	freshBlockRelay(t)
	savedEngine := GlobalBoltEngine
	GlobalBoltEngine = nil // this node holds no blocks
	t.Cleanup(func() { GlobalBoltEngine = savedEngine })

	near, far := connectedPair(t)
	go func() {
		for {
			_ = far.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, _, err := readFrame(far); err != nil {
				return
			}
		}
	}()
	for address := 0; address < blockFetchesKept/blockFetchesPerAddress; address++ {
		for i := 0; i < blockFetchesPerAddress; i++ {
			body, _ := json.Marshal(blockAnnouncement{Height: 5, Hash: fmt.Sprintf("%064x", address*100+i+1)})
			handleBlockAnnouncement(near, fmt.Sprintf("10.0.1.%d", address+1), string(body))
		}
	}
	blockRelay.Lock()
	held := len(blockRelay.fetching)
	blockRelay.Unlock()
	if held != blockFetchesKept {
		t.Fatalf("%d places are taken, want all %d", held, blockFetchesKept)
	}

	honestNear, honestFar := connectedPair(t)
	body, _ := json.Marshal(blockAnnouncement{Height: 5, Hash: testHash("b")})
	handleBlockAnnouncement(honestNear, "10.0.0.7", string(body))
	if got, ok := readRelayFrame(t, honestFar, 2*time.Second); !ok || got != "BLOCK_REQUEST:"+string(body) {
		t.Errorf("an honest neighbour announced a block and was not asked for it (%q): the table was full of blocks that do not exist", got)
	}
	blockRelay.Lock()
	held, kept := len(blockRelay.fetching), blockRelay.fetching[testHash("b")] != nil
	blockRelay.Unlock()
	if held > blockFetchesKept || !kept {
		t.Errorf("the table holds %d blocks (limit %d); the honest neighbour's block is in it: %v", held, blockFetchesKept, kept)
	}

	// An address that holds its full share gains nothing from a full table.
	one, _ := json.Marshal(blockAnnouncement{Height: 5, Hash: testHash("c")})
	handleBlockAnnouncement(near, "10.0.1.9", string(one))
	blockRelay.Lock()
	taken := blockRelay.fetching[testHash("c")] != nil
	blockRelay.Unlock()
	if taken {
		t.Error("an address that had begun its eight blocks was given a ninth place")
	}
}

// A claim that had no test: a block is remembered as offered by sixteen
// nodes at most, and is followed for thirty seconds.
func TestRound7AnnouncersAndTimeAreLimited(t *testing.T) {
	onNetwork(t, "devnet")
	freshBlockRelay(t)
	savedEngine := GlobalBoltEngine
	GlobalBoltEngine = nil
	t.Cleanup(func() { GlobalBoltEngine = savedEngine })

	near, far := connectedPair(t)
	go func() {
		for {
			_ = far.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, _, err := readFrame(far); err != nil {
				return
			}
		}
	}()
	body, _ := json.Marshal(blockAnnouncement{Height: 5, Hash: testHash("e")})
	for address := 0; address < 3*blockAnnouncersKept; address++ {
		handleBlockAnnouncement(near, fmt.Sprintf("10.0.2.%d", address+1), string(body))
	}
	blockRelay.Lock()
	fetch := blockRelay.fetching[testHash("e")]
	waiting := -1
	if fetch != nil {
		waiting = len(fetch.waiting)
		fetch.lastAsked = time.Now().Add(-blockFetchKept - time.Second)
	}
	blockRelay.Unlock()
	if waiting < 0 || waiting > blockAnnouncersKept {
		t.Fatalf("%d nodes are remembered as offering one block; the limit is %d", waiting, blockAnnouncersKept)
	}

	// Thirty seconds on, the next new announcement clears the old block away.
	other, _ := json.Marshal(blockAnnouncement{Height: 6, Hash: testHash("f")})
	handleBlockAnnouncement(near, "10.0.3.1", string(other))
	blockRelay.Lock()
	stillThere := blockRelay.fetching[testHash("e")] != nil
	blockRelay.Unlock()
	if stillThere {
		t.Errorf("a block announced more than %v ago is still being followed", blockFetchKept)
	}
}

// failingConn is a connection whose every write fails.
type failingConn struct {
	net.Conn
	closed int32
}

func (c *failingConn) Write([]byte) (int, error)        { return 0, errors.New("the line is dead") }
func (c *failingConn) SetWriteDeadline(time.Time) error { return nil }
func (c *failingConn) Close() error {
	atomic.StoreInt32(&c.closed, 1)
	return nil
}

// A claim that had no test: a connection whose write fails is closed, so
// that nothing written after half a message is read as the rest of it.
func TestRound7AConnectionWhoseWriteFailsIsClosed(t *testing.T) {
	conn := &failingConn{}
	writeFrame(conn, "PING")
	if atomic.LoadInt32(&conn.closed) != 1 {
		t.Error("a connection was left open after a write to it failed")
	}
}

// NEW FINDING 1, and the cause of report 1. A pass reads this node's newest
// block, then compares chains with the source over the network, and only
// then looks at the newest block again. If the next block of the very same
// chain arrived from a neighbour in between, the point where the chains
// "part" (found for the older newest block) was below the newest block, and
// the pass took that for a rival branch: it took its own good block off,
// stopped the miner, and downloaded the same block again.
func TestRound7ABlockThatArrivesWhileChainsAreComparedIsKept(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	sourceChain := buildTestChain(t, 7)
	StateMutex.Lock()
	rollbackChainToHeightLocked(5)
	StateMutex.Unlock()
	backgroundWork.Wait()

	peer := newStandInPeer(t, sourceChain)
	answer := peer.server.Config.Handler
	var arrived int32
	var lowestTipSeen int64 = 1 << 30
	peer.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blockhash" && atomic.CompareAndSwapInt32(&arrived, 0, 1) {
			// Block 6 of the same chain comes in from a neighbour while the
			// chains are being compared.
			if !placeOnTip(sourceChain[6]) {
				t.Error("block 6 was not placed")
			}
		}
		if r.URL.Path == "/blocks/batch" && atomic.LoadInt32(&arrived) == 1 {
			if height := GetLatestBlockInternal().Index; height < atomic.LoadInt64(&lowestTipSeen) {
				atomic.StoreInt64(&lowestTipSeen, height)
			}
		}
		answer.ServeHTTP(w, r)
	})
	address := peer.asOnlySeed(t)
	atomic.StoreInt64(&lastPassAfterMiss, 0)
	rewrites := atomic.LoadUint64(&chainRewriteSeq)

	TriggerActiveReSyncPass()
	waitForPassesToEnd(t)

	if tip := GetLatestBlockInternal(); tip.Hash != sourceChain[7].Hash {
		t.Fatalf("the node ended at block #%d, want #7", tip.Index)
	}
	if syncSourceIsSetAside(address) {
		t.Error("the source was set aside")
	}
	if lowest := atomic.LoadInt64(&lowestTipSeen); lowest < 6 {
		t.Errorf("during the pass this node's newest block went back to #%d", lowest)
	}
	if taken := (atomic.LoadUint64(&chainRewriteSeq) - rewrites) / 2; taken != 0 {
		t.Errorf("the source holds the same chain as this node, two blocks longer, and the pass took blocks off this node's chain %d time(s)", taken)
	}
}

// Blocks that keep arriving while chains are compared end the pass without a
// verdict after a few comparisons, and the source is not blamed.
func TestRound7APassGivesUpComparingWhenBlocksKeepArriving(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	sourceChain := buildTestChain(t, 12)
	StateMutex.Lock()
	rollbackChainToHeightLocked(5)
	StateMutex.Unlock()
	backgroundWork.Wait()

	peer := newStandInPeer(t, sourceChain)
	answer := peer.server.Config.Handler
	var batches int32
	peer.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A block arrives from a neighbour during every comparison: each time
		// this node asks about its own newest block, the next one comes in.
		if r.URL.Path == "/blockhash" {
			tip := GetLatestBlockInternal().Index
			if r.URL.Query().Get("height") == fmt.Sprint(tip) && tip+1 < int64(len(sourceChain)) {
				placeOnTip(sourceChain[tip+1])
			}
		}
		if r.URL.Path == "/blocks/batch" {
			atomic.AddInt32(&batches, 1)
		}
		answer.ServeHTTP(w, r)
	})
	address := peer.asOnlySeed(t)
	// The pass that would follow at once is held off, so that this one can be
	// looked at alone.
	atomic.StoreInt64(&lastPassAfterMiss, time.Now().Add(time.Hour).UnixNano())
	t.Cleanup(func() {
		atomic.StoreInt64(&lastPassAfterMiss, 0)
		atomic.StoreInt32(&blockMissedDuringPass, 0)
	})
	rewrites := atomic.LoadUint64(&chainRewriteSeq)

	TriggerActiveReSyncPass()

	if tip := GetLatestBlockInternal(); tip.Index != 5+chainComparisonsPerPass {
		t.Errorf("the node is at block #%d, want #%d: the three blocks that arrived and nothing else", tip.Index, 5+chainComparisonsPerPass)
	}
	if got := atomic.LoadInt32(&batches); got != 0 {
		t.Errorf("the pass asked for blocks %d time(s) although it never finished comparing", got)
	}
	if syncSourceIsSetAside(address) {
		t.Error("the source was set aside for blocks that arrived from elsewhere")
	}
	if atomic.LoadInt32(&blockMissedDuringPass) != 1 {
		t.Error("no pass was asked to follow")
	}
	if taken := atomic.LoadUint64(&chainRewriteSeq) - rewrites; taken != 0 {
		t.Error("the pass took blocks off this node's chain")
	}
}

// PART TWO, 6. The largest block the rules allow, written the way it is
// sent, against the largest message a node will send or read.
func TestRound7TheLargestBlockAsSent(t *testing.T) {
	onNetwork(t, "devnet")
	id := func(n int) string { return fmt.Sprintf("TX_%064x", n) }
	shapes := map[string]func(n int) Transaction{
		"transfers of 200 coins into one": func(n int) Transaction {
			tx := Transaction{Version: 1, ID: id(n), PublicKey: strings.Repeat("a", 128), Signature: strings.Repeat("b", 128), Timestamp: 1800000000}
			for i := 0; i < TxMaxInputs; i++ {
				tx.Inputs = append(tx.Inputs, UTXOInput{SourceTxID: id(n*1000 + i), Index: 0, AmountUnits: 1})
			}
			tx.Outputs = []UTXOOutput{{Recipient: testAddress("a"), Amount: UnitsAmount(1)}}
			return tx
		},
		"transfers of one coin into 50": func(n int) Transaction {
			tx := Transaction{Version: 1, ID: id(n), PublicKey: strings.Repeat("a", 128), Signature: strings.Repeat("b", 128), Timestamp: 1800000000}
			tx.Inputs = []UTXOInput{{SourceTxID: id(n * 1000), Index: 0, AmountUnits: 1}}
			for i := 0; i < TxMaxOutputs; i++ {
				tx.Outputs = append(tx.Outputs, UTXOOutput{Recipient: testAddress("a"), Amount: UnitsAmount(1)})
			}
			return tx
		},
		"transfers of one coin into one": func(n int) Transaction {
			return Transaction{Version: 1, ID: id(n), PublicKey: strings.Repeat("a", 128), Signature: strings.Repeat("b", 128), Timestamp: 1800000000,
				Inputs:  []UTXOInput{{SourceTxID: id(n * 1000), Index: 0, AmountUnits: 1}},
				Outputs: []UTXOOutput{{Recipient: testAddress("a"), Amount: UnitsAmount(1)}}}
		},
	}
	for name, shape := range shapes {
		block := Block{Version: 1, Index: 5, Timestamp: 1800000000, PrevHash: emptyUncleHash, Hash: emptyUncleHash, UncleHash: emptyUncleHash, UncleMiner: emptyUncleMiner, Difficulty: 5000}
		for n := 1; ; n++ {
			tx := shape(n)
			if blockSizeBytes(block)+transactionSizeBytes(tx) > Params.MaxBlockBytes {
				break
			}
			block.Transactions = append(block.Transactions, tx)
		}
		sent, _ := json.Marshal(block)
		if frame := len("BLOCK_BROADCAST:") + len(sent); frame > MaxPayloadFrameSize {
			t.Errorf("a block of %s within the size rule is %d bytes as sent, and would not be sent (limit %d)", name, frame, MaxPayloadFrameSize)
		}
	}
}
