package node

import (
	"bytes"
	"sync/atomic"
	"testing"

	"go.etcd.io/bbolt"
)

// Round 4: the tests of late-block relief left out the forty blocks after
// the launch window, where relief returns one halving at a time. A block
// there that is very late counts as only so late, both when it is mined and
// when the block after it is worked out.
func TestALateBlockInTheRampAfterTheLaunchWindow(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	const steady = 10000000
	minimum := minDifficultyAt(100)
	parent := storePlainBlocks(t, 21, steady) // block 21 is the first after the window

	// Block 22 may be given two halvings at most, however late it is.
	lateDate := parent.Timestamp + 5000
	countedGap := launchRampGap(22, 5000)
	if countedGap != HeartbeatStallAfterSeconds+2*HeartbeatStallStepSeconds-1 {
		t.Fatalf("block 22, 5000 s late, counts as %d s late", countedGap)
	}
	lateDifficulty := CalculateNextDifficulty(parent, lateDate)
	if want := heartbeatRetargetAnchored(steady, HeartbeatTargetSeconds, countedGap, scheduleDriftSeconds(parent), minimum); int64(lateDifficulty) != want {
		t.Fatalf("block 22 is mined at %d, want %d", lateDifficulty, want)
	}
	if lateDifficulty < steady/5 || lateDifficulty > steady/3 {
		t.Errorf("block 22 should be at about a quarter of %d (two halvings), got %d", steady, lateDifficulty)
	}
	late := storePlainBlock(t, parent, lateDate, lateDifficulty)

	// Block 23 starts from what block 22 would have carried without relief.
	unrelieved := heartbeatRetargetAnchored(steady, HeartbeatTargetSeconds, 0, scheduleDriftSeconds(parent), minimum)
	next := CalculateNextDifficulty(late, lateDate+45)
	if want := heartbeatRetargetAnchored(unrelieved, 5000, 45, scheduleDriftSeconds(late), minimum); int64(next) != want {
		t.Errorf("block 23 is at %d, want %d", next, want)
	}
	resetUnrelievedCache()
	if again := CalculateNextDifficulty(late, lateDate+45); again != next {
		t.Errorf("asked again with an empty table the answer changed: %d, then %d", next, again)
	}
}

// Round 4: when a late block is replaced by a rival at the same height that
// was on time, the block after it is worked out from the rival, and nothing
// remembered about the block that was replaced is used.
func TestALateBlockReplacedByOneThatWasOnTime(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	const steady = 10000000
	minimum := minDifficultyAt(100)
	parent := storePlainBlocks(t, 70, steady)

	lateDate := parent.Timestamp + 840
	late := storePlainBlock(t, parent, lateDate, CalculateNextDifficulty(parent, lateDate))
	afterLate := CalculateNextDifficulty(late, lateDate+45) // fills the table for the late block

	onTimeDate := parent.Timestamp + 50
	onTime := storePlainBlock(t, parent, onTimeDate, CalculateNextDifficulty(parent, onTimeDate)) // overwrites height 71
	afterOnTime := CalculateNextDifficulty(onTime, onTimeDate+45)
	if want := heartbeatRetargetAnchored(int64(onTime.Difficulty), 50, 45, scheduleDriftSeconds(onTime), minimum); int64(afterOnTime) != want {
		t.Errorf("after the on-time rival the next block is at %d, want %d", afterOnTime, want)
	}
	if afterOnTime == afterLate {
		t.Error("the block after the rival was worked out as if the late block were still there")
	}
}

// Round 4, finding 2: the walk back through late blocks reads several blocks
// one after another. If stored history is rewritten meanwhile, the blocks
// read may not belong to one chain; the answer is then used once and not
// remembered, so a wrong figure cannot stay with the node.
func TestNothingIsRememberedWhileHistoryIsBeingRewritten(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	parent := storePlainBlocks(t, 70, 10000000)
	lateDate := parent.Timestamp + 840
	late := storePlainBlock(t, parent, lateDate, CalculateNextDifficulty(parent, lateDate))
	remembered := func() bool {
		unrelievedCache.Lock()
		defer unrelievedCache.Unlock()
		_, found := unrelievedCache.byHash[late.Hash]
		return found
	}

	resetUnrelievedCache()
	beginChainRewrite()
	during := unrelievedDifficulty(late)
	if remembered() {
		t.Error("an answer worked out during a rewrite was remembered")
	}
	endChainRewrite()
	if after := unrelievedDifficulty(late); after != during {
		t.Errorf("the answer changed: %d during the rewrite, %d after", during, after)
	}
	if !remembered() {
		t.Error("an answer worked out with history at rest was not remembered")
	}

	// Taking blocks off the chain counts as a rewrite, and leaves the count even.
	before := atomic.LoadUint64(&chainRewriteSeq)
	atomic.StoreInt64(&localHeightState, late.Index)
	StateMutex.Lock()
	rollbackChainToHeightLocked(late.Index - 1)
	StateMutex.Unlock()
	if now := atomic.LoadUint64(&chainRewriteSeq); now != before+2 {
		t.Errorf("a rollback moved the rewrite count from %d to %d, want two steps", before, now)
	}
}

// Round 4: the old routine that rebuilds the coin set from the blocks knows
// nothing of coin ages or sweeps. It must never run over a chain that has
// blocks, even if its "done" mark is missing.
func TestTheCoinSetIsNotRebuiltOverAChainWithBlocks(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	buildTestChain(t, 4)
	coins := func() []byte {
		var all bytes.Buffer
		_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
			return tx.Bucket(utxoBucket).ForEach(func(k, v []byte) error {
				all.Write(k)
				all.WriteByte('=')
				all.Write(v)
				all.WriteByte('\n')
				return nil
			})
		})
		return all.Bytes()
	}
	before := coins()
	if !bytes.Contains(before, []byte(`"age"`)) {
		t.Fatal("this chain's coins carry no ages; the test would show nothing")
	}
	if err := GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(metaBucket).Delete([]byte("utxo_reindex_v2"))
	}); err != nil {
		t.Fatal(err)
	}
	reindexUTXOFromChainOnce()
	if after := coins(); !bytes.Equal(before, after) {
		t.Errorf("the coin set was rebuilt over a chain with blocks:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
