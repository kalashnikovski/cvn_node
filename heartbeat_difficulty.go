package main

import (
	"fmt"
	"math/big"
	"strings"
)

// HEARTBEAT DIFFICULTY
//
// From HeartbeatActivationHeight onward the chain stops spacing blocks with a
// 45-second wait inside the miner (which any custom miner can simply delete)
// and spaces them with proof-of-work instead:
//
//   - Difficulty becomes a fine-grained number: the average count of hashes a
//     block needs. A hash is valid when hash <= (2^256 - 1) / Difficulty. The
//     old rule counted leading zero digits, so every step was a 16x jump.
//   - Difficulty is re-tuned on every block from how long the parent took:
//     faster than 45 s nudges it up, slower nudges it down. Over any long run
//     the chain's own timestamps therefore average 45 s per block, whatever
//     hashrate joins or leaves.
//   - A block found 3 seconds after its parent is still valid. It just cost
//     real work, and it makes the next block harder. Nobody can keep producing
//     blocks every few seconds without out-hashing an ever-rising difficulty.
//   - If hashrate suddenly leaves and no block arrives for 6 minutes, the
//     difficulty of the block being mined halves, and halves again every
//     further 3 minutes, so the chain cannot flatline.
//
// Every node must run this rule before the activation height. A node that does
// not will reject the first heartbeat block and stay behind on its own chain.
// A variable only so tests can activate the rule early; treat it as a constant.
var HeartbeatActivationHeight = int64(2500)

const (
	HeartbeatTargetSeconds = int64(45)
	// Each block moves difficulty by at most 1/HeartbeatWindow upward (+5%).
	HeartbeatWindow = int64(20)
	// A parent slower than this is treated as this slow, capping one step at -50%.
	HeartbeatMaxSolveSeconds = 11 * HeartbeatTargetSeconds

	// Difficulty of the very first heartbeat block. The retarget corrects a
	// wrong guess within a few dozen blocks in either direction.
	HeartbeatInitialDifficulty = 40000000
	// 65536 equals the old four-leading-zeros floor.
	HeartbeatMinDifficulty = 65536
	HeartbeatMaxDifficulty = 1 << 55

	// Stall relief: starts when a block is dated this long after its parent,
	// then repeats every HeartbeatStallStepSeconds.
	HeartbeatStallAfterSeconds = 8 * HeartbeatTargetSeconds
	HeartbeatStallStepSeconds  = 4 * HeartbeatTargetSeconds

	// A block may not be dated further ahead of this node's clock than this.
	HeartbeatMaxFutureDriftSeconds = int64(120)
)

var heartbeatMaxHash = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// IsHeartbeatHeight reports whether the block at this height follows the heartbeat rules.
func IsHeartbeatHeight(height int64) bool {
	return height >= HeartbeatActivationHeight
}

// HeartbeatTargetHex returns the largest valid hash for a difficulty as 64
// lowercase hex digits, so it can be compared directly against a block hash.
func HeartbeatTargetHex(difficulty int) string {
	if difficulty < 1 {
		difficulty = 1
	}
	target := new(big.Int).Div(heartbeatMaxHash, big.NewInt(int64(difficulty)))
	return fmt.Sprintf("%064x", target)
}

// HashMeetsDifficulty is the single proof-of-work check for both rule sets.
func HashMeetsDifficulty(hashHex string, height int64, difficulty int) bool {
	if !IsHeartbeatHeight(height) {
		if difficulty < 0 {
			return false
		}
		return strings.HasPrefix(hashHex, strings.Repeat("0", difficulty))
	}
	if len(hashHex) != 64 || int64(difficulty) < minDifficultyAt(height) {
		return false
	}
	return hashHex <= HeartbeatTargetHex(difficulty)
}

// CalculateHeartbeatDifficulty returns the difficulty required of the block that
// follows lastBlock and is dated nextBlockTimestamp.
func CalculateHeartbeatDifficulty(lastBlock Block, nextBlockTimestamp int64) int {
	gapSeconds := nextBlockTimestamp - lastBlock.Timestamp
	if !IsHeartbeatHeight(lastBlock.Index) {
		// lastBlock is not a heartbeat block (the genesis block, or one that
		// still carries an old leading-zeros difficulty): start from the
		// network's initial difficulty.
		initial := Params.HeartbeatInitialDifficulty
		if initial < 1 {
			initial = HeartbeatInitialDifficulty
		}
		if Params.ScheduleAnchored {
			return int(heartbeatRetargetAnchored(initial, HeartbeatTargetSeconds, gapSeconds, 0, minDifficultyAt(lastBlock.Index+1)))
		}
		return int(heartbeatRetarget(initial, HeartbeatTargetSeconds, gapSeconds))
	}
	minimum := minDifficultyAt(lastBlock.Index + 1)
	if era := powEraStartingAt(lastBlock.Index + 1); era != nil && era.StartDifficulty > 0 {
		// The mining algorithm changes at this block, so the parent's
		// difficulty does not carry over: restart from the era's own value.
		return int(heartbeatRetargetAnchored(era.StartDifficulty, HeartbeatTargetSeconds, gapSeconds, 0, minimum))
	}
	solveSeconds := HeartbeatTargetSeconds
	if grandParent := GetBlockByHeightFromDB(lastBlock.Index - 1); grandParent.Hash != "" {
		solveSeconds = lastBlock.Timestamp - grandParent.Timestamp
	}
	if !Params.ScheduleAnchored {
		return int(heartbeatRetarget(int64(lastBlock.Difficulty), solveSeconds, gapSeconds))
	}
	if lastBlock.Index <= 1 {
		// Block 1 follows the genesis block, whose date says nothing about how
		// long block 1 took to find.
		solveSeconds = HeartbeatTargetSeconds
	}
	return int(heartbeatRetargetAnchored(int64(lastBlock.Difficulty), solveSeconds, gapSeconds, scheduleDriftSeconds(lastBlock), minimum))
}

// scheduleDriftSeconds says how far the chain is from its schedule at
// lastBlock: the time actually taken since block 1, minus the time the blocks
// since block 1 should have taken. Negative means the chain is ahead (blocks
// have come too fast); positive means it is behind.
//
// The schedule is counted from block 1, not from the genesis block, so it
// starts when mining starts.
func scheduleDriftSeconds(lastBlock Block) int64 {
	if lastBlock.Index <= 1 {
		return 0
	}
	firstBlock := GetBlockByHeightFromDB(1)
	if firstBlock.Hash == "" {
		return 0
	}
	elapsed := lastBlock.Timestamp - firstBlock.Timestamp
	scheduled := (lastBlock.Index - 1) * HeartbeatTargetSeconds
	return elapsed - scheduled
}

const (
	// HeartbeatDriftDamping sets how gently the schedule correction acts: a
	// drift of one block interval moves difficulty by 1/(window*damping).
	HeartbeatDriftDamping = int64(200)
	// HeartbeatDriftLimitPercent caps the correction at this share of
	// difficulty per block, so that catching up after a long stall, or
	// slowing down after a burst, never moves block times far from target.
	HeartbeatDriftLimitPercent = int64(1)
)

// heartbeatRetargetAnchored is heartbeatRetarget with one more term. The plain
// rule looks only at the parent block, and over a long run its blocks come a
// few percent faster than the target. This form also looks at how far the
// whole chain is from its schedule and leans against that, so the long-run
// average is the target exactly.
//
//	difficulty * (1 + (target - solve)/(target*window)
//	                - drift/(target*window*damping))      drift term capped
func heartbeatRetargetAnchored(parentDifficulty, parentSolveSeconds, gapSeconds, driftSeconds, minimumDifficulty int64) int64 {
	if minimumDifficulty < 1 {
		minimumDifficulty = 1
	}
	if parentSolveSeconds < 1 {
		parentSolveSeconds = 1
	}
	if parentSolveSeconds > HeartbeatMaxSolveSeconds {
		parentSolveSeconds = HeartbeatMaxSolveSeconds
	}
	scale := HeartbeatTargetSeconds * HeartbeatWindow * HeartbeatDriftDamping
	driftLimit := scale * HeartbeatDriftLimitPercent / 100
	correction := -driftSeconds
	if correction > driftLimit {
		correction = driftLimit
	}
	if correction < -driftLimit {
		correction = -driftLimit
	}
	adjustment := (HeartbeatTargetSeconds-parentSolveSeconds)*HeartbeatDriftDamping + correction

	// Whole-number arithmetic on big numbers: exact, identical on every
	// machine, and accurate even at the lowest difficulty.
	change := new(big.Int).Mul(big.NewInt(parentDifficulty), big.NewInt(adjustment))
	change.Quo(change, big.NewInt(scale))
	result := new(big.Int).Add(big.NewInt(parentDifficulty), change)
	if !result.IsInt64() || result.Int64() > HeartbeatMaxDifficulty {
		result.SetInt64(HeartbeatMaxDifficulty)
	}
	difficulty := result.Int64()

	if gapSeconds >= HeartbeatStallAfterSeconds {
		halvings := (gapSeconds-HeartbeatStallAfterSeconds)/HeartbeatStallStepSeconds + 1
		if halvings > 40 {
			halvings = 40
		}
		difficulty >>= uint(halvings)
	}
	if difficulty < minimumDifficulty {
		difficulty = minimumDifficulty
	}
	if difficulty > HeartbeatMaxDifficulty {
		difficulty = HeartbeatMaxDifficulty
	}
	return difficulty
}

// heartbeatRetarget is the pure arithmetic of the rule. parentDifficulty and
// parentSolveSeconds describe the parent block; gapSeconds is how long after
// the parent the new block is dated.
func heartbeatRetarget(parentDifficulty int64, parentSolveSeconds int64, gapSeconds int64) int64 {
	difficulty := parentDifficulty
	if parentSolveSeconds < 1 {
		parentSolveSeconds = 1
	}
	if parentSolveSeconds > HeartbeatMaxSolveSeconds {
		parentSolveSeconds = HeartbeatMaxSolveSeconds
	}
	// difficulty * (1 + (target - solve) / (target * window)), in whole numbers.
	difficulty += difficulty / (HeartbeatTargetSeconds * HeartbeatWindow) * (HeartbeatTargetSeconds - parentSolveSeconds)

	if gapSeconds >= HeartbeatStallAfterSeconds {
		halvings := (gapSeconds-HeartbeatStallAfterSeconds)/HeartbeatStallStepSeconds + 1
		if halvings > 40 {
			halvings = 40
		}
		difficulty >>= uint(halvings)
	}

	if difficulty < HeartbeatMinDifficulty {
		difficulty = HeartbeatMinDifficulty
	}
	if difficulty > HeartbeatMaxDifficulty {
		difficulty = HeartbeatMaxDifficulty
	}
	return difficulty
}
