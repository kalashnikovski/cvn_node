package node

import (
	"testing"
	"time"
)

// Audit finding: the difficulty of the first blocks followed the dates
// written on them. The genesis block is dated by a Bitcoin miner's clock,
// and the first miner chooses the dates of the first blocks, so either could
// make the next block look hours late and drop its difficulty to the
// minimum. During the launch window dates give no relief and difficulty
// does not go below the starting value.
func TestLaunchDifficultyCannotBeTalkedDown(t *testing.T) {
	const start = 4 * HeartbeatMinDifficulty
	startTestChain(t, "devnet", func() {
		Params.HeartbeatInitialDifficulty = start
		// Dated three days back, so that the late blocks below are still in
		// the past by this machine's clock.
		Params.GenesisTimestamp = time.Now().Unix() - 3*24*3600
	})
	genesis := GetBlockByHeightFromDB(0)

	// The Bitcoin block was dated an hour before anyone could start mining.
	for _, late := range []int64{60, 20 * 60, 60 * 60, 2 * 24 * 3600} {
		if got := CalculateNextDifficulty(genesis, genesis.Timestamp+late); got != start {
			t.Errorf("a first block dated %d seconds after the anchor needs difficulty %d, want the starting %d", late, got, start)
		}
	}

	// The first miner dates block 1 a second after the anchor, long before
	// the real time, so that block 2 looks most of a day late.
	backdated := extendTestChain(t, genesis.Timestamp+1, "CVN_first", nil)
	weekLater := genesis.Timestamp + 20*3600
	if got := CalculateNextDifficulty(backdated, weekLater); got < start {
		t.Fatalf("after a backdated first block, the second needs difficulty %d: below the starting %d", got, start)
	}
	// Nor does it help on the block after that, where the long gap shows up
	// as a slow block.
	second := extendTestChain(t, weekLater, "CVN_first", nil)
	if got := CalculateNextDifficulty(second, weekLater+45); got < start {
		t.Errorf("the third block needs difficulty %d: below the starting %d", got, start)
	}

	// Difficulty can still rise during the window when blocks come fast.
	timestamp := weekLater
	for height := int64(3); height <= HeartbeatLaunchBlocks; height++ {
		timestamp += 5
		extendTestChain(t, timestamp, "CVN_first", nil)
	}
	tip := GetLatestBlockInternal()
	if tip.Index != HeartbeatLaunchBlocks || tip.Difficulty <= start {
		t.Fatalf("block %d has difficulty %d; fast blocks should have raised it above %d", tip.Index, tip.Difficulty, start)
	}

	// After the window the ordinary rule is back, including relief for a
	// block that is truly late: without it a chain that lost its miners
	// would never recover.
	afterWindow := CalculateNextDifficulty(tip, tip.Timestamp+HeartbeatStallAfterSeconds+HeartbeatStallStepSeconds)
	if afterWindow >= tip.Difficulty/2 {
		t.Errorf("after the launch window a block nine minutes late needs difficulty %d; the parent had %d and it should have halved twice", afterWindow, tip.Difficulty)
	}
}

// Audit finding: a miner sat idle for a minute whenever the newest block was
// dated more than a minute ahead. Dating a block two minutes ahead, which is
// allowed, therefore idled every honest miner and left the field to whoever
// did it. A miner now waits only when its own block would be refused.
func TestMinerDoesNotIdleForFutureDatedBlocks(t *testing.T) {
	const now = int64(1_800_000_000)
	for ahead, wait := range map[int64]bool{
		0:   false,
		61:  false, // used to wait
		100: false, // used to wait
		HeartbeatMaxFutureDriftSeconds - minerClockMargin:     false,
		HeartbeatMaxFutureDriftSeconds - minerClockMargin + 1: true,
		HeartbeatMaxFutureDriftSeconds + 1:                    true,
	} {
		if got := mustWaitForClock(now+ahead, now); got != wait {
			t.Errorf("a block dated %d seconds ahead: wait %v, want %v", ahead, got, wait)
		}
	}
	// The longest an honest miner is held up by a block dated at the limit
	// is the margin plus the one second its own block must add.
	if delay := minerClockMargin + 1; delay > 15 {
		t.Errorf("a block dated at the limit holds honest miners for %d seconds", delay)
	}
}

// Audit finding: a rival to our newest block, on the same parent, with a
// proof that does not hold up, cost a full proof check and was not held
// against the peer that sent it.
func TestBadWorkBesideOurTipIsHeldAgainstThePeer(t *testing.T) {
	startTestChain(t, "devnet")
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 3; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, "CVN_miner", nil)
	}
	// Two honest candidates for the next block; one becomes our tip.
	ours := mineTestBlock(t, timestamp+45, "CVN_miner", nil, 0)
	theirs := mineTestBlock(t, timestamp+46, "CVN_rival", nil, 0)
	if !ValidateBlockSignatures(ours) || !SaveBlockToStorageInternal(ours) {
		t.Fatal("could not extend the chain")
	}
	if blockCarriesBadWork(theirs) {
		t.Fatal("an honest rival to our tip was held against its sender")
	}
	unworked := theirs
	for unworked.Nonce = theirs.Nonce + 1; ; unworked.Nonce++ {
		unworked.Hash = CalculateHash(unworked)
		if !BlockProofValid(unworked, unworked.Difficulty) {
			break
		}
	}
	if !blockCarriesBadWork(unworked) {
		t.Error("a rival to our tip with no valid proof of work was not noticed")
	}
	easier := theirs
	easier.Difficulty = theirs.Difficulty / 2
	easier.Hash = CalculateHash(easier)
	if !blockCarriesBadWork(easier) {
		t.Error("a rival to our tip claiming an easier difficulty was not noticed")
	}
	// Our own tip, sent back to us, is nothing to hold against anyone.
	if blockCarriesBadWork(ours) {
		t.Error("our own tip was held against the peer that echoed it")
	}
}
