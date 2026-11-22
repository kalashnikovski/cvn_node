package node

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// Tests for what the review of the round 7 fixes found (the fixes are in
// commit 3f3db0a; the review is audit/ROUND_7_RESULTS.md, "The fixes,
// reviewed").

// FINDING 1. A source that had once delivered a better chain was remembered
// for good, and every remembered source is asked for its newest block at the
// start of every pass, one after another. Eight that had gone away cost a
// pass sixteen seconds before it looked at anyone else, and the list only
// grew. Now the list is short, a source leaves it after a day, and one that
// does not answer leaves it at once.
func TestRecheckSourcesThatWentAwayAreForgotten(t *testing.T) {
	onNetwork(t, "devnet")
	t.Chdir(t.TempDir())
	Params.SeedHosts = []string{"10.9.0.1"}
	Params.ExtraSyncURLs = nil
	seed := Params.syncURL("10.9.0.1")

	PeerMapMutex.Lock()
	savedPeers := ActivePeerMap
	ActivePeerMap = map[string]time.Time{}
	PeerMapMutex.Unlock()
	peerCacheMutex.Lock()
	savedCache := peerCache
	peerCache = make(map[string]int64)
	peerCacheMutex.Unlock()
	syncDelivered.Lock()
	savedDelivered := syncDelivered.at
	syncDelivered.at = make(map[string]time.Time)
	syncDelivered.Unlock()
	savedProbe := probeChain
	t.Cleanup(func() {
		probeChain = savedProbe
		PeerMapMutex.Lock()
		ActivePeerMap = savedPeers
		PeerMapMutex.Unlock()
		peerCacheMutex.Lock()
		peerCache = savedCache
		peerCacheMutex.Unlock()
		syncDelivered.Lock()
		syncDelivered.at = savedDelivered
		syncDelivered.Unlock()
	})

	// Twenty nodes delivered, one a minute, the last a minute ago; one more
	// delivered two days ago; and the seed delivered just now.
	const delivered = 20
	now := time.Now()
	syncDelivered.Lock()
	for i := 1; i <= delivered; i++ {
		syncDelivered.at[Params.syncURL(fmt.Sprintf("10.9.1.%d", i))] = now.Add(-time.Duration(i) * time.Minute)
	}
	longAgo := Params.syncURL("10.9.2.1")
	syncDelivered.at[longAgo] = now.Add(-48 * time.Hour)
	syncDelivered.at[seed] = now
	syncDelivered.Unlock()

	// All of them have gone away: none answers.
	asked := make(map[string]int)
	probeChain = func(gateway string) (int64, *big.Int) {
		asked[gateway]++
		return 0, nil
	}
	ourWork := big.NewInt(100 * 1000)

	if chosen, _ := chooseSyncSource(100, ourWork); chosen != "" {
		t.Fatalf("%q was chosen, and no node answered", chosen)
	}
	if asked[seed] != 1 {
		t.Errorf("the seed was asked %d times, want once", asked[seed])
	}
	if asked[longAgo] != 0 {
		t.Error("a source that last delivered two days ago was still asked")
	}
	if others := len(asked) - 1; others > syncDeliveredKept {
		t.Errorf("%d remembered sources were asked in one pass; no more than %d are kept", others, syncDeliveredKept)
	}
	for i := 1; i <= syncDeliveredKept; i++ {
		if recent := Params.syncURL(fmt.Sprintf("10.9.1.%d", i)); asked[recent] != 1 {
			t.Errorf("%s, among the %d that delivered most recently, was asked %d times, want once", recent, syncDeliveredKept, asked[recent])
		}
	}

	// None answered, so none is asked again. The seed always is.
	asked = make(map[string]int)
	chooseSyncSource(100, ourWork)
	if len(asked) != 1 || asked[seed] != 1 {
		t.Errorf("on the next pass %d sources were asked (%v); only the seed should be, the others did not answer last time", len(asked), asked)
	}
	syncDelivered.Lock()
	remembered := len(syncDelivered.at)
	syncDelivered.Unlock()
	if remembered != 0 {
		t.Errorf("%d sources that did not answer are still remembered", remembered)
	}

	// One that does answer stays, though it has nothing new this time.
	stays := Params.syncURL("10.9.3.1")
	syncDelivered.Lock()
	syncDelivered.at[stays] = time.Now()
	syncDelivered.Unlock()
	probeChain = func(gateway string) (int64, *big.Int) {
		if gateway == stays {
			return 100, big.NewInt(100 * 1000)
		}
		return 0, nil
	}
	chooseSyncSource(100, ourWork)
	syncDelivered.Lock()
	_, kept := syncDelivered.at[stays]
	syncDelivered.Unlock()
	if !kept {
		t.Error("a source that answered, with nothing new, was forgotten")
	}
}

// FINDING 2. With the table of announced blocks full of addresses holding
// one place each, a new address holding none pushed one of them out: the one
// whose address sorts first, which here is the honest neighbour whose block
// is being fetched. Now an address gives way only to one that would still
// hold no more places than it does afterwards. The reviewer's test, turned
// round.
func TestRecheckAFullTableKeepsABlockBeingFetched(t *testing.T) {
	onNetwork(t, "devnet")
	freshBlockRelay(t)
	savedEngine := GlobalBoltEngine
	GlobalBoltEngine = nil // this node holds no blocks
	t.Cleanup(func() { GlobalBoltEngine = savedEngine })

	conn := &failingConn{}
	announce := func(from string, number int) {
		body, _ := json.Marshal(blockAnnouncement{Height: 5, Hash: fmt.Sprintf("%064x", number)})
		handleBlockAnnouncement(conn, from, string(body))
	}
	for i := 0; i < blockFetchesKept-1; i++ {
		announce(fmt.Sprintf("10.0.0.%d", i+1), i+1)
	}
	const honestFrom, honestNumber = "0.0.0.1", 900 // sorts before every other address
	announce(honestFrom, honestNumber)
	honestHash := fmt.Sprintf("%064x", honestNumber)

	announce("10.0.1.1", 901) // one more address, with no place yet

	blockRelay.Lock()
	_, kept := blockRelay.fetching[honestHash]
	_, taken := blockRelay.fetching[fmt.Sprintf("%064x", 901)]
	held := len(blockRelay.fetching)
	blockRelay.Unlock()
	if !kept {
		t.Error("an address with no place pushed out a block that was being fetched from an address with one")
	}
	if taken {
		t.Error("the new address was given a place in a table full of addresses holding one each")
	}
	if held != blockFetchesKept {
		t.Errorf("the table holds %d blocks, want %d", held, blockFetchesKept)
	}
	if _, asked := noteAnnouncedBlockArrived(honestHash); !asked {
		t.Error("the honest neighbour's block arrived and was no longer known as one that was asked for")
	}

	// An address holding two still gives way to one holding none.
	freshBlockRelay(t)
	for i := 0; i < blockFetchesKept/2; i++ {
		announce(fmt.Sprintf("10.0.0.%d", i+1), 2*i+1)
		announce(fmt.Sprintf("10.0.0.%d", i+1), 2*i+2)
	}
	announce("10.0.1.1", 901)
	blockRelay.Lock()
	_, taken = blockRelay.fetching[fmt.Sprintf("%064x", 901)]
	held = len(blockRelay.fetching)
	blockRelay.Unlock()
	if !taken || held != blockFetchesKept {
		t.Errorf("an address with no place was given one: %v; the table holds %d, want %d", taken, held, blockFetchesKept)
	}
}

// FINDING 3. The test named for "blocks are checked a group at a time" called
// prepareProofs itself and passed with the groups taken out. This one goes
// through a pass. The first block is properly mined; the 511 after it are
// made up, join on, and claim difficulty 1, which every hash meets. Handed
// over all at once, all 512 had their proofs worked out before the rules,
// which refuse the second block, saw any of them. Handed over a group at a
// time, one group is.
func TestRecheckADownloadWorksProofsOutAGroupAtATime(t *testing.T) {
	t.Chdir(t.TempDir())
	timestamp := startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 1}})
	Params.HeartbeatInitialDifficulty = 64
	Params.KeyEpochBlocks, Params.KeyLagBlocks = 1920, 100 // as on devnet and the CVN Testnet
	withProofThreads(t, 2)

	genesis := GetLatestBlockInternal()
	*timestamp += 45
	mined := extendTestChain(t, *timestamp, testAddress("miner"), nil)
	madeUp := madeUpBatch(t, syncBatchMaxBlocks-1)
	if demanded := CalculateNextDifficulty(mined, madeUp[0].Timestamp); demanded <= madeUp[0].Difficulty {
		t.Fatalf("the rules demand difficulty %d of block 2; the test needs them to demand more than %d", demanded, madeUp[0].Difficulty)
	}
	StateMutex.Lock()
	rollbackChainToHeightLocked(0)
	StateMutex.Unlock()
	backgroundWork.Wait()
	forgetProofs()

	peer := newStandInPeer(t, append([]Block{genesis, mined}, madeUp...))
	peer.claimedWork = "1000000000000000000"
	peer.asOnlySeed(t)
	atomic.StoreInt64(&lastPassAfterMiss, 0)

	before := atomic.LoadInt64(&proofsWorkedOutAhead)
	TriggerActiveReSyncPass()
	waitForPassesToEnd(t)
	worked := atomic.LoadInt64(&proofsWorkedOutAhead) - before

	if tip := GetLatestBlockInternal(); tip.Hash != mined.Hash {
		t.Fatalf("the node ended at block #%d; it should hold the mined block #1 and none of the made-up ones", tip.Index)
	}
	if group := int64(proofsAtOnce()); worked > group {
		t.Errorf("%d proofs were worked out for a batch the rules refuse at its second block; one group is %d", worked, group)
	}
	if worked == 0 {
		t.Error("no proof was worked out ahead; the test did not reach what it is testing")
	}
}

// ROUND 7, NEW FINDING 8 had no test: a pass that finds the newest block
// changed while it measured the source's branch asks for another pass. The
// reviewer of the fixes wrote this one, and it fails with that line taken
// out.
func TestRecheckATipChangedWhileABranchIsMeasuredIsFollowedUp(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	original := buildTestChain(t, 7)
	StateMutex.Lock()
	rollbackChainToHeightLocked(5)
	StateMutex.Unlock()
	backgroundWork.Wait()
	sourceChain := localChain()
	timestamp := sourceChain[5].Timestamp
	for i := 0; i < 5; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, testAddress("review-source"), nil)
	}
	sourceChain = localChain()
	// Make a rival of the original tip, then put the original chain back.
	StateMutex.Lock()
	rollbackChainToHeightLocked(5)
	if !SaveBlockToStorageInternal(original[6]) {
		StateMutex.Unlock()
		t.Fatal("could not put back the original block 6")
	}
	StateMutex.Unlock()
	rivalTip := extendTestChain(t, original[7].Timestamp+1, testAddress("review-rival"), nil)
	StateMutex.Lock()
	rollbackChainToHeightLocked(5)
	for _, block := range original[6:] {
		if !SaveBlockToStorageInternal(block) {
			StateMutex.Unlock()
			t.Fatalf("could not restore original block #%d", block.Index)
		}
	}
	StateMutex.Unlock()
	backgroundWork.Wait()
	peer := newStandInPeer(t, sourceChain)
	answer := peer.server.Config.Handler
	var changed int32
	peer.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocks/batch" && atomic.CompareAndSwapInt32(&changed, 0, 1) {
			StateMutex.Lock()
			rollbackChainToHeightLocked(6)
			if !SaveBlockToStorageInternal(rivalTip) {
				StateMutex.Unlock()
				t.Error("could not install the rival tip during branch measurement")
				return
			}
			StateMutex.Unlock()
		}
		answer.ServeHTTP(w, r)
	})
	peer.asOnlySeed(t)
	atomic.StoreInt32(&blockMissedDuringPass, 0)
	atomic.StoreInt64(&lastPassAfterMiss, time.Now().Add(time.Hour).UnixNano())
	t.Cleanup(func() {
		atomic.StoreInt32(&blockMissedDuringPass, 0)
		atomic.StoreInt64(&lastPassAfterMiss, 0)
		atomic.StoreInt32(&missedBlockPassWaiting, 0)
	})
	TriggerActiveReSyncPass()
	if atomic.LoadInt32(&changed) != 1 {
		t.Fatal("the branch measurement did not replace the tip")
	}
	if tip := GetLatestBlockInternal(); tip.Hash != rivalTip.Hash {
		t.Fatalf("tip ended at #%d with hash %s, want rival tip %s", tip.Index, tip.Hash, rivalTip.Hash)
	}
	if atomic.LoadInt32(&blockMissedDuringPass) != 1 {
		t.Error("the pass did not schedule a follow-up after the tip changed during branch measurement")
	}
}
