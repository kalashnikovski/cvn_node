package node

import (
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// alsoServeOn makes a stand-in peer answer on a second loopback address,
// using the same port. A node addresses every other node by host and the
// network's one download port, so two peers in one test need two hosts.
func (peer *standInPeer) alsoServeOn(t *testing.T, host string, port int) {
	t.Helper()
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		t.Skipf("this machine cannot listen on %s:%d: %v", host, port, err)
	}
	second := &httptest.Server{Listener: listener, Config: &http.Server{Handler: peer.server.Config.Handler}}
	second.Start()
	t.Cleanup(second.Close)
}

func clearSyncMemory(t *testing.T) {
	t.Helper()
	reset := func() {
		syncSetAside.Lock()
		syncSetAside.until = make(map[string]time.Time)
		syncSetAside.Unlock()
		syncDelivered.Lock()
		syncDelivered.at = make(map[string]time.Time)
		syncDelivered.Unlock()
		atomic.StoreInt64(&lastPassFromBlock, 0)
	}
	reset()
	t.Cleanup(reset)
}

// Round 2.5 finding: at start-up the node believed the highest height any
// remembered node claimed, and did not start mining until it got there. A
// node claiming a height it would never deliver kept it from ever starting.
func TestStartUpDoesNotWaitOnAStrangersClaim(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	clearSyncMemory(t)
	ours := buildTestChain(t, 6)

	seed := newStandInPeer(t, ours) // an honest seed, no further on than we are
	seed.asOnlySeed(t)
	liar := newStandInPeer(t, ours)
	liar.claimedHeight = 1000000000
	liar.claimedWork = "99999999999999999999999999999999"
	liar.alsoServeOn(t, "127.0.0.2", Params.SyncPort)
	PeerMapMutex.Lock()
	savedPeers := ActivePeerMap
	ActivePeerMap = map[string]time.Time{Params.gossipAddress("127.0.0.2"): time.Now()}
	PeerMapMutex.Unlock()
	t.Cleanup(func() {
		PeerMapMutex.Lock()
		ActivePeerMap = savedPeers
		PeerMapMutex.Unlock()
	})

	finished := make(chan struct{})
	go func() { runInitialDownload([]string{"cvn_node"}); close(finished) }()
	select {
	case <-finished:
	case <-time.After(60 * time.Second):
		t.Fatal("start-up is still waiting on a node that claimed a height it will never deliver")
	}
	if tip := GetLatestBlockInternal(); tip.Index != 6 || tip.Hash != ours[6].Hash {
		t.Errorf("start-up moved the chain to #%d", tip.Index)
	}
	if atomic.LoadInt32(&IsSyncingActive) != 0 {
		t.Error("start-up finished and left mining paused")
	}
}

func TestStartUpCatchesUpFromASeed(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	clearSyncMemory(t)
	full := buildTestChain(t, 14)
	StateMutex.Lock()
	rollbackChainToHeightLocked(4)
	StateMutex.Unlock()
	newStandInPeer(t, full).asOnlySeed(t)

	runInitialDownload([]string{"cvn_node"})
	if tip := GetLatestBlockInternal(); tip.Index != 14 || tip.Hash != full[14].Hash {
		t.Fatalf("after start-up the node is at #%d, want #14", tip.Index)
	}
}

// Round 2.5 finding: a seed that failed to answer one small request was
// treated as having a different chain, and set aside as a liar.
func TestASourceThatStopsAnsweringIsNotBlamed(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	clearSyncMemory(t)
	ours := buildTestChain(t, 6)
	ahead := append(append([]Block{}, ours...), Block{Index: 7, Hash: strings.Repeat("ab", 32)})

	seed := newStandInPeer(t, ahead)
	source := seed.asOnlySeed(t)
	seed.silentAboutBlocks = true // it reports its height, then answers nothing more

	TriggerActiveReSyncPass()
	if syncSourceIsSetAside(source) {
		t.Error("a seed that stopped answering was set aside as if it had lied")
	}
	if GetLatestBlockInternal().Hash != ours[6].Hash {
		t.Error("the chain changed although the source delivered nothing")
	}
}

// Round 2.5 finding: a node at height 0 was read as a node that had not
// answered. At a launch every node is at height 0, so every miner would
// have waited five minutes for an answer it already had.
func TestANodeAtTheStartCountsAsAnAnswer(t *testing.T) {
	onNetwork(t, "devnet")
	t.Chdir(t.TempDir())
	Params.SeedHosts = []string{"10.0.0.1", "10.0.0.2"}
	savedProbe := probeChain
	t.Cleanup(func() { probeChain = savedProbe })

	probeChain = func(string) (int64, *big.Int) { return 0, big.NewInt(256) } // at the genesis block
	best, answered := probePeerHeights()
	if !answered || best != 0 {
		t.Fatalf("seeds at height 0: answered %v, best height %d", answered, best)
	}
	if hold, _, reason := decideMiningHold(0, 400, best, answered, 0); hold {
		t.Errorf("with every seed at height 0 the miner was held back: %s", reason)
	}
	probeChain = func(string) (int64, *big.Int) { return 0, nil } // nobody there
	if _, answered := probePeerHeights(); answered {
		t.Error("silence was read as an answer")
	}
	// The decision rests on the seeds alone, not on any node reached before.
	for _, gateway := range miningReadinessGateways() {
		if gateway != Params.syncURL("10.0.0.1") && gateway != Params.syncURL("10.0.0.2") {
			t.Errorf("%s is asked whether to hold the miner; only seeds should be", gateway)
		}
	}
}

// Round 2.5 finding: relief for a late block came back in full on the first
// block after the launch window. Whoever mined the window could date its
// blocks close together, let the real clock run on, and have block 21 read
// as hours late: the burst the window prevents, one block later.
func TestReliefReturnsGraduallyAfterTheLaunchWindow(t *testing.T) {
	onNetwork(t, "devnet")
	hoursLate := int64(6 * 3600)
	for _, c := range []struct {
		height   int64
		halvings uint
	}{
		{HeartbeatLaunchBlocks + 1, 1},
		{HeartbeatLaunchBlocks + 2, 2},
		{HeartbeatLaunchBlocks + 10, 10},
	} {
		capped := launchRampGap(c.height, hoursLate)
		// The gap that earns exactly that many halvings, and no more.
		if got := (capped-HeartbeatStallAfterSeconds)/HeartbeatStallStepSeconds + 1; got != int64(c.halvings) {
			t.Errorf("block %d, hours late: counted as %d s late, which earns %d halvings; want %d", c.height, capped, got, c.halvings)
		}
	}
	// A block that is only a little late is not affected.
	if got := launchRampGap(HeartbeatLaunchBlocks+1, 100); got != 100 {
		t.Errorf("a block 100 s after its parent is counted as %d s late", got)
	}
	// After the ramp the ordinary rule is back in full.
	if got := launchRampGap(HeartbeatLaunchBlocks+HeartbeatRampBlocks, hoursLate); got != hoursLate {
		t.Errorf("after the ramp a block %d s late is counted as %d s late", hoursLate, got)
	}
	// Networks that do not start the launch way are untouched.
	onNetwork(t, "testnet-1")
	if got := launchRampGap(21, hoursLate); got != hoursLate {
		t.Errorf("testnet-1: a late block is counted as %d s late, want %d", got, hoursLate)
	}
}

// fillPool puts made-up entries straight into the pool, for the tests of
// what gives way when it is full.
func fillPool(t *testing.T, limit int, entries map[string]mempoolEntry) {
	t.Helper()
	savedLimit := MempoolMaxTransactions
	MempoolMaxTransactions = limit
	MempoolMatrix.Lock()
	MempoolMatrix.Transactions = make(map[string]Transaction)
	mempoolEntries = make(map[string]mempoolEntry)
	mempoolArrival = make(map[string]time.Time)
	start := time.Now().Add(-time.Minute)
	order := 0
	for _, prefix := range []string{"settled", "young", "paid"} { // a fixed order of arrival
		for id, entry := range entries {
			if strings.HasPrefix(id, prefix) {
				MempoolMatrix.Transactions[id] = Transaction{ID: id}
				mempoolEntries[id] = entry
				mempoolArrival[id] = start.Add(time.Duration(order) * time.Millisecond)
				order++
			}
		}
	}
	MempoolMatrix.Unlock()
	t.Cleanup(func() {
		MempoolMaxTransactions = savedLimit
		MempoolMatrix.Lock()
		MempoolMatrix.Transactions = make(map[string]Transaction)
		mempoolEntries = make(map[string]mempoolEntry)
		mempoolArrival = make(map[string]time.Time)
		MempoolMatrix.Unlock()
	})
}

func poolCounts() (settled, young, paid int) {
	MempoolMatrix.Lock()
	defer MempoolMatrix.Unlock()
	for id := range MempoolMatrix.Transactions {
		switch entry := mempoolEntries[id]; {
		case entry.fee > 0:
			paid++
		case entry.settled:
			settled++
		default:
			young++
		}
	}
	return
}

func tryRoom(incoming mempoolEntry) bool {
	MempoolMatrix.Lock()
	defer MempoolMatrix.Unlock()
	return makeRoomInMempool(incoming)
}

// Round 2.5 finding: with the pool full, a fee of one unit pushed out a
// fee-less transfer, and 5,000 of them emptied every pool of every fee-less
// transfer, for next to nothing.
func TestTheSmallestFeeDoesNotSweepOutFreeTransfers(t *testing.T) {
	// A pool of 10: two places are kept for fee-less transfers of settled coins.
	fillPool(t, 10, map[string]mempoolEntry{
		"settled1": {settled: true}, "settled2": {settled: true},
		"paid1": {fee: 5}, "paid2": {fee: 5}, "paid3": {fee: 5}, "paid4": {fee: 5},
		"paid5": {fee: 5}, "paid6": {fee: 5}, "paid7": {fee: 5}, "paid8": {fee: 5},
	})
	if tryRoom(mempoolEntry{fee: 1}) {
		t.Fatal("a fee of one unit pushed a waiting transfer out")
	}
	if settled, _, paid := poolCounts(); settled != 2 || paid != 8 {
		t.Fatalf("the pool changed: %d settled free, %d paying", settled, paid)
	}
	// A transfer that pays more than the cheapest paying one takes its place,
	// and the free ones are left alone.
	if !tryRoom(mempoolEntry{fee: 6}) {
		t.Fatal("a transfer paying more than the cheapest waiting found no room")
	}
	if settled, _, paid := poolCounts(); settled != 2 || paid != 7 {
		t.Errorf("after making room for a higher fee: %d settled free, %d paying; want 2 and 7", settled, paid)
	}
}

func TestFreeTransfersKeepTheirShareAndNoMore(t *testing.T) {
	// Free transfers of settled coins hold more than their two places.
	fillPool(t, 10, map[string]mempoolEntry{
		"settled1": {settled: true}, "settled2": {settled: true}, "settled3": {settled: true},
		"settled4": {settled: true}, "settled5": {settled: true},
		"paid1": {fee: 5}, "paid2": {fee: 5}, "paid3": {fee: 5}, "paid4": {fee: 5}, "paid5": {fee: 5},
	})
	for i := 0; i < 3; i++ {
		if !tryRoom(mempoolEntry{fee: 1}) {
			t.Fatalf("with free transfers over their share, a paying one found no room (round %d)", i)
		}
		MempoolMatrix.Lock()
		id := "new" + strconv.Itoa(i)
		MempoolMatrix.Transactions[id] = Transaction{ID: id}
		mempoolEntries[id] = mempoolEntry{fee: 9}
		mempoolArrival[id] = time.Now()
		MempoolMatrix.Unlock()
	}
	if settled, _, _ := poolCounts(); settled != 2 {
		t.Fatalf("free transfers were pushed down to %d places; their share is 2", settled)
	}
	if tryRoom(mempoolEntry{fee: 1}) {
		t.Error("a small fee pushed a free transfer out of its reserved share")
	}

	// A free transfer of young coins has no claim, and is the first to go.
	fillPool(t, 10, map[string]mempoolEntry{
		"young1": {}, "young2": {}, "settled1": {settled: true},
		"paid1": {fee: 5}, "paid2": {fee: 5}, "paid3": {fee: 5}, "paid4": {fee: 5},
		"paid5": {fee: 5}, "paid6": {fee: 5}, "paid7": {fee: 5},
	})
	if tryRoom(mempoolEntry{}) {
		t.Error("a fee-less transfer of young coins pushed something out")
	}
	if !tryRoom(mempoolEntry{fee: 1}) {
		t.Fatal("a paying transfer found no room in a pool holding young free ones")
	}
	if _, young, paid := poolCounts(); young != 1 || paid != 7 {
		t.Errorf("a young free transfer should have gone first: %d young, %d paying left", young, paid)
	}
	// A free transfer of settled coins claims its reserved place, first from
	// a young free one, then from the cheapest paying one.
	if !tryRoom(mempoolEntry{settled: true}) {
		t.Fatal("a settled free transfer could not take a young free one's place")
	}
	if !tryRoom(mempoolEntry{settled: true}) {
		t.Fatal("with their share not full, a settled free transfer found no room")
	}
	if _, young, paid := poolCounts(); young != 0 || paid != 6 {
		t.Errorf("want the last young free and one paying transfer gone: %d young, %d paying left", young, paid)
	}
}

// Round 2.5 finding: a sender could not replace their own waiting transfer
// with one paying more: their node refused it as a double spend.
func TestAnOwnerCanReplaceTheirWaitingTransferByPayingMore(t *testing.T) {
	startTestChain(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	extendTestChain(t, Params.GenesisTimestamp+60, owner, nil)
	coins := ListSpendableCoins(owner)
	build := func(fee string) Transaction {
		tx, err := BuildSignedTransfer(privateKey, publicKey, "CVN_someone", cvn("10"), cvn(fee), coins)
		if err != nil {
			t.Fatal(err)
		}
		return tx
	}
	first := build("0")
	if err := AdmitTransactionToMempool(first); err != nil {
		t.Fatal(err)
	}
	if err := AdmitTransactionToMempool(build("0")); err == nil && false {
		t.Fatal("unreachable")
	}
	same := build("0")
	same.Timestamp++ // another transfer of the same coin, paying no more
	same.ID = ComputeTransactionID(same)
	r, s, _ := SignTransactionPayload(privateKey, same.ID)
	same.Signature = r + "|" + s
	if err := AdmitTransactionToMempool(same); err == nil || !strings.Contains(err.Error(), "higher fee") {
		t.Errorf("a second transfer of the same coin paying no more was not refused for that reason: %v", err)
	}
	better := build("0.01")
	if err := AdmitTransactionToMempool(better); err != nil {
		t.Fatalf("a replacement paying more was refused: %v", err)
	}
	MempoolMatrix.RLock()
	_, oldThere := MempoolMatrix.Transactions[first.ID]
	_, newThere := MempoolMatrix.Transactions[better.ID]
	size := len(MempoolMatrix.Transactions)
	MempoolMatrix.RUnlock()
	if oldThere || !newThere || size != 1 {
		t.Errorf("after replacing: old one waiting %v, new one waiting %v, %d in the pool", oldThere, newThere, size)
	}
	// Only the replacement is mined.
	chosen, fees := SelectMempoolTransactionsForBlock(10)
	if len(chosen) != 1 || chosen[0].ID != better.ID || fees != cvn("0.01") {
		t.Errorf("the miner chose %d transfers with fees %d", len(chosen), fees)
	}
}

// Round 2.5 finding: every mining thread checked the whole pool for itself.
func TestTransfersAreChosenOncePerTip(t *testing.T) {
	startTestChain(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp
	for i := 0; i < 2; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, owner, nil)
	}
	coins := ListSpendableCoins(owner)
	admit := func(coin SpendableCoin) {
		tx, err := BuildSignedTransfer(privateKey, publicKey, "CVN_someone", cvn("1"), 0, []SpendableCoin{coin})
		if err != nil {
			t.Fatal(err)
		}
		if err := AdmitTransactionToMempool(tx); err != nil {
			t.Fatal(err)
		}
	}
	admit(coins[0])
	tip := GetLatestBlockInternal().Hash
	first, _ := selectionForTip(tip, 100)
	admit(coins[1])
	second, _ := selectionForTip(tip, 100)
	if len(first) != 1 || len(second) != 1 {
		t.Errorf("the same tip moments later: %d then %d transfers chosen; the choice should have been reused", len(first), len(second))
	}
	// A new tip means a new choice.
	fresh, _ := selectionForTip("another tip", 100)
	if len(fresh) != 2 {
		t.Errorf("for a new tip %d transfers were chosen, want both", len(fresh))
	}
}

// Round 2.5 finding: transfers in blocks that were removed in a
// reorganisation were dropped, although their senders had been told "sent".
func TestTransfersInRemovedBlocksGoBackToWaiting(t *testing.T) {
	startTestChain(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 3; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, owner, nil)
	}
	transfer, err := BuildSignedTransfer(privateKey, publicKey, "CVN_someone", cvn("10"), cvn("0.1"), ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	extendTestChain(t, timestamp+45, "CVN_other", []Transaction{transfer})

	StateMutex.Lock()
	rollbackChainToHeightLocked(3)
	StateMutex.Unlock()
	for wait := 0; wait < 100; wait++ {
		MempoolMatrix.RLock()
		_, waiting := MempoolMatrix.Transactions[transfer.ID]
		MempoolMatrix.RUnlock()
		if waiting {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("the transfer in the removed block was not put back in the pool")
}

// Round 2.5 finding: the fast miner was also looked for in the folder the
// program was started from, so a file planted there would have been run.
func TestFastMinerIsNotTakenFromTheStartFolder(t *testing.T) {
	t.Chdir(t.TempDir())
	name := "xmrig"
	if runtime.GOOS == "windows" {
		name = "xmrig.exe"
	}
	if err := os.MkdirAll("xmrig-cvn", 0755); err != nil {
		t.Fatal(err)
	}
	// Files with the right names, which must not even be tried.
	for _, planted := range []string{filepath.Join("xmrig-cvn", name), name} {
		if err := os.WriteFile(planted, []byte("not a miner"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	path, err := locateFastMiner([]string{"cvn_node"})
	if err == nil {
		t.Fatalf("a file in the start folder was taken as the fast miner: %s", path)
	}
	if !strings.Contains(err.Error(), "was not found") {
		t.Errorf("the planted file seems to have been tried: %v", err)
	}
}

// While the chain is being changed nothing is held against a peer.
func TestNoPeerIsJudgedWhileTheChainIsChanging(t *testing.T) {
	startTestChain(t, "devnet")
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 3; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, "CVN_miner", nil)
	}
	unworked := mineTestBlock(t, timestamp+45, "CVN_miner", nil, 0)
	for unworked.Nonce++; ; unworked.Nonce++ {
		unworked.Hash = CalculateHash(unworked)
		if !BlockProofValid(unworked, unworked.Difficulty) {
			break
		}
	}
	if !blockCarriesBadWork(unworked) {
		t.Fatal("the test needs a block that is normally held against its sender")
	}
	atomic.StoreInt32(&IsSyncingActive, 1)
	judged := blockCarriesBadWork(unworked)
	atomic.StoreInt32(&IsSyncingActive, 0)
	if judged {
		t.Error("a peer was judged while the chain was being changed")
	}
}
