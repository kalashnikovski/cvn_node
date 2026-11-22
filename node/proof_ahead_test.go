package node

import (
	"fmt"
	"math/big"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// forgetProofs empties everything the checking engine remembers, so that a
// test can tell an answer worked out ahead from one remembered from mining.
func forgetProofs() {
	rxVerifier.Lock()
	rxVerifier.results = nil
	rxVerifier.prepared = nil
	rxVerifier.Unlock()
}

func withProofThreads(t *testing.T, threads int) {
	t.Helper()
	saved := proofCheckThreads
	proofCheckThreads = func() int { return threads }
	t.Cleanup(func() {
		proofCheckThreads = saved
		releaseProofHelp()
	})
}

// The proofs worked out ahead for a batch are the same ones the ordinary
// checking arrives at, block for block, across two changes of key, including
// the blocks whose key is another block of the same batch. The batch is then
// accepted as it was before.
func TestProofsWorkedOutAheadAreTheTrueOnes(t *testing.T) {
	timestamp := startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 4}})
	withProofThreads(t, 4)

	var batch []Block
	want := make(map[string]string)
	keysUsed := make(map[string]bool)
	for height := 1; height <= 12; height++ {
		*timestamp += 45
		block := extendTestChain(t, *timestamp, testAddress("miner"), nil)
		batch = append(batch, block)
	}
	forgetProofs()
	for _, block := range batch {
		key, ok := powKeyFor(block.Index)
		if !ok {
			t.Fatalf("block #%d: no key", block.Index)
		}
		blob, _ := powBlob(powCommitment(block), uint32(block.Nonce))
		work, ok := randomXVerify(key, blob)
		if !ok {
			t.Fatalf("block #%d: its proof could not be worked out", block.Index)
		}
		want[string(key)+string(blob)] = work
		keysUsed[string(key)] = true
	}
	if len(keysUsed) < 3 {
		t.Fatalf("the batch should cross two changes of key; keys used: %d", len(keysUsed))
	}

	// The node goes back to its first block, as a node that has yet to
	// download these would be: the keys can only come from the batch.
	StateMutex.Lock()
	rollbackChainToHeightLocked(0)
	StateMutex.Unlock()
	forgetProofs()

	if prepared := prepareProofs(batch); prepared != len(batch) {
		t.Fatalf("%d of %d proofs were worked out ahead", prepared, len(batch))
	}
	rxVerifier.Lock()
	got := make(map[string]string, len(rxVerifier.prepared))
	for entry, work := range rxVerifier.prepared {
		got[entry] = work
	}
	rxVerifier.Unlock()
	if len(got) != len(want) {
		t.Errorf("%d answers were kept, want %d", len(got), len(want))
	}
	for entry, work := range want {
		if got[entry] != work {
			t.Errorf("an answer worked out ahead is %q, and the ordinary checking gives %q", got[entry], work)
		}
	}

	for _, block := range batch {
		if !ValidateBlockSignatures(block) || !SaveBlockToStorageInternal(block) {
			t.Fatalf("block #%d was refused after its proof was worked out ahead", block.Index)
		}
	}
	if tip := GetLatestBlockInternal(); tip.Hash != batch[len(batch)-1].Hash {
		t.Errorf("the chain ends at block #%d, want #%d", tip.Index, batch[len(batch)-1].Index)
	}

	releaseProofHelp()
	rxVerifier.Lock()
	defer rxVerifier.Unlock()
	if rxVerifier.prepared != nil {
		t.Error("the answers were kept after the download was over")
	}
	for _, engine := range rxVerifier.engines {
		if len(engine.spare) != 0 {
			t.Error("the spare machines were kept after the download was over")
		}
	}
}

// A node downloading a chain from another node, the whole way a node does
// it, ends on that chain, and the proofs it checked on the way were the ones
// worked out ahead.
func TestADownloadUsesTheProofsWorkedOutAhead(t *testing.T) {
	timestamp := startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 4}})
	withProofThreads(t, 4)
	for height := 1; height <= 12; height++ {
		*timestamp += 45
		extendTestChain(t, *timestamp, testAddress("miner"), nil)
	}
	sourceChain := localChain()
	sourceTip := GetLatestBlockInternal()
	StateMutex.Lock()
	rollbackChainToHeightLocked(0)
	StateMutex.Unlock()
	forgetProofs()

	newStandInPeer(t, sourceChain).asOnlySeed(t)
	before := atomic.LoadInt64(&proofsFoundReady)
	TriggerActiveReSyncPass()
	backgroundWork.Wait()

	if tip := GetLatestBlockInternal(); tip.Hash != sourceTip.Hash {
		t.Fatalf("the download ended at block #%d, and the source is at #%d", tip.Index, sourceTip.Index)
	}
	if found := atomic.LoadInt64(&proofsFoundReady) - before; found < 12 {
		t.Errorf("the checking found %d proofs ready; every one of the 12 blocks should have had one", found)
	}
	rxVerifier.Lock()
	defer rxVerifier.Unlock()
	if rxVerifier.prepared != nil {
		t.Error("the answers were kept after the download was over")
	}
}

// A block that was not worked for is refused exactly as before, and the work
// done ahead on a batch stops there: whoever sends a batch of such blocks
// costs this node a handful of hashes, not one for every block they made up.
func TestWorkingAheadStopsAtABlockWithoutTheWork(t *testing.T) {
	timestamp := startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 64}})
	Params.HeartbeatInitialDifficulty = 64
	Params.KeyEpochBlocks = 1000 // one key throughout, so the batch is one run of work
	withProofThreads(t, 2)

	var batch []Block
	for height := 1; height <= 2; height++ {
		*timestamp += 45
		batch = append(batch, extendTestChain(t, *timestamp, testAddress("miner"), nil))
	}
	*timestamp += 45
	good := mineTestBlock(t, *timestamp, testAddress("miner"), nil, 0)
	lazy := good
	digest := BlockTransactionsDigest(lazy.Index, lazy.Transactions)
	for nonce := int64(0); ; nonce++ {
		lazy.Nonce = nonce
		lazy.Hash = CalculateHashWithTxDigest(lazy, digest)
		if !BlockProofValid(lazy, lazy.Difficulty) {
			break
		}
	}
	batch = append(batch, lazy)
	// Forty more that join on to it properly and carry their own
	// identifiers, which costs their maker nothing.
	for i := 0; i < 40; i++ {
		previous := batch[len(batch)-1]
		next := previous
		next.Index, next.PrevHash, next.Timestamp, next.Nonce = previous.Index+1, previous.Hash, previous.Timestamp+45, int64(i)
		next.Hash = CalculateHash(next)
		batch = append(batch, next)
	}
	forgetProofs()

	prepared := prepareProofs(batch)
	if most := 2 * proofsAtOncePerThread; prepared > most {
		t.Errorf("%d proofs were worked out for a batch whose third block has no work behind it; at most %d should be", prepared, most)
	}
	if prepared < 3 {
		t.Errorf("only %d proofs were worked out; the blocks before the bad one should have been", prepared)
	}
	if ValidateBlockSignatures(lazy) {
		t.Error("a block without the work passed, after its proof was worked out ahead")
	}
	if !ValidateBlockSignatures(good) || !SaveBlockToStorageInternal(good) {
		t.Error("the properly mined block was refused")
	}

	// A batch that does not join up is not worked on past the break.
	forgetProofs()
	broken := append([]Block{}, batch[:2]...)
	stray := batch[1]
	stray.Index, stray.PrevHash = 3, "not the block before"
	stray.Hash = CalculateHash(stray)
	broken = append(broken, stray)
	if prepared := prepareProofs(broken); prepared != 2 {
		t.Errorf("%d proofs were worked out for a batch that breaks after two blocks", prepared)
	}

	// With one core there is nothing to gain, and nothing is done.
	proofCheckThreads = func() int { return 1 }
	forgetProofs()
	if prepared := prepareProofs(batch); prepared != 0 {
		t.Errorf("%d proofs were worked out ahead on one core", prepared)
	}
}

// Working proofs out on several cores gives the same answers as one at a
// time, and is faster. The speeds are printed for whoever wants them.
func TestProofsOnSeveralCoresAreFaster(t *testing.T) {
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 4}})
	t.Cleanup(releaseProofHelp)
	key := []byte("a key for timing proofs")
	const count = 96
	blobs := make([][]byte, count)
	for i := range blobs {
		blobs[i], _ = powBlob(fmt.Sprintf("%064x", i+1), uint32(i))
	}
	randomXVerify(key, []byte("to prepare the engine before timing"))

	threads := runtime.NumCPU()
	forgetProofs()
	started := time.Now()
	together, ok := randomXVerifyTogether(key, blobs, threads)
	tookTogether := time.Since(started)
	if !ok || len(together) != count {
		t.Fatalf("the proofs could not be worked out together")
	}

	forgetProofs()
	started = time.Now()
	for i, blob := range blobs {
		alone, ok := randomXVerify(key, blob)
		if !ok || alone != together[i] {
			t.Fatalf("proof %d: together gave %q, alone gave %q", i, together[i], alone)
		}
	}
	tookAlone := time.Since(started)

	t.Logf("%d proofs: one at a time %v (%.0f a second); on %d cores %v (%.0f a second); %.1f times faster",
		count, tookAlone.Round(time.Millisecond), count/tookAlone.Seconds(),
		threads, tookTogether.Round(time.Millisecond), count/tookTogether.Seconds(),
		tookAlone.Seconds()/tookTogether.Seconds())
	if threads >= 4 && tookTogether*3/2 > tookAlone {
		t.Errorf("on %d cores the proofs took %v, and one at a time %v", threads, tookTogether, tookAlone)
	}
}

// From the Linux check on a server in London: every seed held the same
// chain, and the node downloaded all of it from Singapore, the first in the
// list, both times. Among seeds that claim the same work the one that
// answers soonest is chosen. A seed with more work is still chosen over a
// nearer one with less.
func TestTheSeedThatAnswersSoonestIsChosen(t *testing.T) {
	onNetwork(t, "devnet")
	t.Chdir(t.TempDir())
	Params.SeedHosts = []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	far, near, middling := Params.syncURL("10.0.0.1"), Params.syncURL("10.0.0.2"), Params.syncURL("10.0.0.3")
	PeerMapMutex.Lock()
	savedPeers := ActivePeerMap
	ActivePeerMap = map[string]time.Time{}
	PeerMapMutex.Unlock()
	savedProbe := probeChain
	t.Cleanup(func() {
		probeChain = savedProbe
		PeerMapMutex.Lock()
		ActivePeerMap = savedPeers
		PeerMapMutex.Unlock()
	})

	takes := map[string]time.Duration{far: 120 * time.Millisecond, near: 5 * time.Millisecond, middling: 50 * time.Millisecond}
	claims := map[string]int64{far: 110, near: 110, middling: 110}
	probeChain = func(gateway string) (int64, *big.Int) {
		height, known := claims[gateway]
		if !known {
			return 0, nil
		}
		time.Sleep(takes[gateway])
		return height, big.NewInt(height * 1000)
	}
	ourWork := big.NewInt(100 * 1000)

	if chosen, height := chooseSyncSource(100, ourWork); chosen != near || height != 110 {
		t.Errorf("with three seeds on the same chain the node chose %q; want the one that answered soonest, %q", chosen, near)
	}
	// The far one is a block ahead: more work wins, however slow.
	claims[far] = 111
	if chosen, height := chooseSyncSource(100, ourWork); chosen != far || height != 111 {
		t.Errorf("with the far seed ahead the node chose %q (claiming #%d); want the far seed", chosen, height)
	}
	// The near one has nothing better than ours: it is not chosen for being near.
	claims[far], claims[near] = 110, 100
	if chosen, _ := chooseSyncSource(100, ourWork); chosen != middling {
		t.Errorf("with the nearest seed no further on than us the node chose %q; want %q", chosen, middling)
	}
}
