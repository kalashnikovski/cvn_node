package node

import (
	"encoding/json"
	"fmt"
	"testing"

	"go.etcd.io/bbolt"
)

// storePlainBlocks writes blocks 1..count straight into the database, one
// every 45 seconds at the given difficulty, with no proof of work. The
// difficulty rule only reads heights, dates and difficulties, so this lets a
// test set up any history it needs. It returns the last block.
func storePlainBlocks(t *testing.T, count int, difficulty int) Block {
	t.Helper()
	previous := GetBlockByHeightFromDB(0)
	for height := int64(1); height <= int64(count); height++ {
		previous = storePlainBlock(t, previous, previous.Timestamp+HeartbeatTargetSeconds, difficulty)
	}
	return previous
}

func storePlainBlock(t *testing.T, parent Block, timestamp int64, difficulty int) Block {
	t.Helper()
	block := Block{
		Version: 1, Index: parent.Index + 1, Timestamp: timestamp, PrevHash: parent.Hash, Difficulty: difficulty,
		Hash: fmt.Sprintf("%064x", parent.Index+1+int64(difficulty)*1000003+timestamp*7919),
	}
	raw, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	if err := GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(blocksBucket).Put([]byte(fmt.Sprintf("%016d", block.Index)), raw)
	}); err != nil {
		t.Fatal(err)
	}
	return block
}

// A block that arrives 14 minutes late is mined at an eighth of the
// difficulty. The block after it starts from the difficulty its parent would
// have had with no relief, lowered once for the parent's slowness; under the
// first form of the rule it started from the eighth and was lowered again.
func TestReliefForALateBlockIsNotInherited(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	if !Params.ReliefNotInherited {
		t.Fatal("devnet must not let late-block relief be inherited")
	}
	const steady = 10000000
	minimum := minDifficultyAt(100)
	parent := storePlainBlocks(t, 70, steady) // past the launch window and the ramp after it, exactly on schedule

	lateDate := parent.Timestamp + 840
	lateDifficulty := CalculateNextDifficulty(parent, lateDate)
	if want := steady >> 3; lateDifficulty != want {
		t.Fatalf("a block 14 minutes late is mined at %d, want an eighth of %d (%d)", lateDifficulty, steady, want)
	}
	late := storePlainBlock(t, parent, lateDate, lateDifficulty)

	next := CalculateNextDifficulty(late, lateDate+45)
	want := heartbeatRetargetAnchored(steady, 840, 45, scheduleDriftSeconds(late), minimum)
	if int64(next) != want {
		t.Errorf("the block after a late one is at %d, want %d", next, want)
	}
	if next < steady*55/100 || next > steady*65/100 {
		t.Errorf("the block after a late one should be at about 0.6 of %d (one slow-parent step), got %d", steady, next)
	}

	// The same question with nothing remembered gives the same answer.
	resetUnrelievedCache()
	if again := CalculateNextDifficulty(late, lateDate+45); again != next {
		t.Errorf("asked again with an empty table the answer changed: %d, then %d", next, again)
	}

	// With the setting off the old behaviour returns: an eighth, then the
	// slow-parent step on top.
	Params.ReliefNotInherited = false
	old := CalculateNextDifficulty(late, lateDate+45)
	Params.ReliefNotInherited = true
	if oldWant := heartbeatRetargetAnchored(int64(lateDifficulty), 840, 45, scheduleDriftSeconds(late), minimum); int64(old) != oldWant {
		t.Errorf("first form of the rule gave %d, want %d", old, oldWant)
	}
	if old*6 > next {
		t.Errorf("first form of the rule (%d) should leave far less than the new one (%d)", old, next)
	}
}

// Several late blocks in a row: each one's unrelieved difficulty is worked
// out from the one before, back to the last block that was on time.
func TestSeveralLateBlocksInARow(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	const steady = 10000000
	minimum := minDifficultyAt(100)
	onTime := storePlainBlocks(t, 70, steady)

	// By hand: the three figures the rule should pass along.
	unrelieved := int64(steady)
	previous, solve := onTime, HeartbeatTargetSeconds
	for i, gap := range []int64{840, 600, 2000} {
		date := previous.Timestamp + gap
		got := CalculateNextDifficulty(previous, date)
		if want := heartbeatRetargetAnchored(unrelieved, solve, gap, scheduleDriftSeconds(previous), minimum); int64(got) != want {
			t.Fatalf("late block %d: mined at %d, want %d", i+1, got, want)
		}
		// What this block would have carried without relief, for the next step.
		unrelieved = heartbeatRetargetAnchored(unrelieved, solve, 0, scheduleDriftSeconds(previous), minimum)
		previous, solve = storePlainBlock(t, previous, date, got), gap
	}
	afterRun := CalculateNextDifficulty(previous, previous.Timestamp+45)
	if want := heartbeatRetargetAnchored(unrelieved, solve, 45, scheduleDriftSeconds(previous), minimum); int64(afterRun) != want {
		t.Errorf("after three late blocks: %d, want %d", afterRun, want)
	}
	// Three slow parents lower the difficulty by three slow-parent steps,
	// no more: about 0.6 cubed of where it stood, whatever relief each
	// block had.
	if afterRun < steady*18/100 || afterRun > steady*26/100 {
		t.Errorf("after three late blocks difficulty is %d, want about 0.22 of %d", afterRun, steady)
	}
	resetUnrelievedCache()
	if again := CalculateNextDifficulty(previous, previous.Timestamp+45); again != afterRun {
		t.Errorf("asked again with an empty table the answer changed: %d, then %d", afterRun, again)
	}
}

// Near the network's minimum the relieved difficulty is stopped at the
// minimum, so it cannot be doubled back to find what it was. The rule works
// the unrelieved figure out afresh and gets it exactly.
func TestUnrelievedDifficultyNearTheMinimum(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	minimum := minDifficultyAt(100)
	steady := int(minimum) * 3 / 2
	parent := storePlainBlocks(t, 70, steady)

	lateDate := parent.Timestamp + 2000 // ten halvings: far below the minimum
	lateDifficulty := CalculateNextDifficulty(parent, lateDate)
	if int64(lateDifficulty) != minimum {
		t.Fatalf("a very late block near the minimum is mined at %d, want the minimum %d", lateDifficulty, minimum)
	}
	late := storePlainBlock(t, parent, lateDate, lateDifficulty)

	next := CalculateNextDifficulty(late, lateDate+45)
	if want := heartbeatRetargetAnchored(int64(steady), 2000, 45, scheduleDriftSeconds(late), minimum); int64(next) != want {
		t.Errorf("the block after it is at %d, want %d", next, want)
	}
	if int64(next) > int64(steady) {
		t.Errorf("difficulty rose from %d to %d after a block that was 33 minutes late", steady, next)
	}
}

// Blocks that arrive on time are judged exactly as before; so are the blocks
// of the launch window, where no relief is given at all.
func TestOnTimeBlocksAreUnchanged(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	both := func(parent Block, date int64) (withSetting, without int) {
		withSetting = CalculateNextDifficulty(parent, date)
		Params.ReliefNotInherited = false
		without = CalculateNextDifficulty(parent, date)
		Params.ReliefNotInherited = true
		return
	}
	previous := GetBlockByHeightFromDB(0)
	gaps := []int64{45, 3, 200, 359, 1, 90, 45, 300, 12, 45}
	for height := 1; height <= 90; height++ {
		gap := gaps[height%len(gaps)]
		if height == 10 {
			gap = 5000 // very late, but inside the launch window
		}
		date := previous.Timestamp + gap
		withSetting, without := both(previous, date)
		if withSetting != without {
			t.Fatalf("block %d, %d s after its parent: %d with the setting, %d without", height, gap, withSetting, without)
		}
		previous = storePlainBlock(t, previous, date, withSetting)
	}
}

// A real chain, mined and validated block by block, with a late block in it:
// every block is accepted, and each block's difficulty is what the rule
// gives when asked afresh.
func TestAChainWithALateBlockValidates(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 64; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, testAddress("miner"), nil)
	}
	timestamp += 1200 // twenty minutes without a block
	extendTestChain(t, timestamp, testAddress("miner"), nil)
	for _, gap := range []int64{45, 400, 45, 45} {
		timestamp += gap
		extendTestChain(t, timestamp, testAddress("miner"), nil)
	}
	resetUnrelievedCache()
	tip := GetLatestBlockInternal()
	for height := tip.Index; height > 60; height-- {
		block, parent := GetBlockByHeightFromDB(height), GetBlockByHeightFromDB(height-1)
		if want := CalculateNextDifficulty(parent, block.Timestamp); block.Difficulty != want {
			t.Errorf("block %d carries difficulty %d; asked afresh the rule gives %d", height, block.Difficulty, want)
		}
	}
}
