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
	if len(hashHex) != 64 || difficulty < HeartbeatMinDifficulty {
		return false
	}
	return hashHex <= HeartbeatTargetHex(difficulty)
}

// CalculateHeartbeatDifficulty returns the difficulty required of the block that
// follows lastBlock and is dated nextBlockTimestamp.
func CalculateHeartbeatDifficulty(lastBlock Block, nextBlockTimestamp int64) int {
	if !IsHeartbeatHeight(lastBlock.Index) {
		// lastBlock still carries an old leading-zeros difficulty.
		return int(heartbeatRetarget(HeartbeatInitialDifficulty, HeartbeatTargetSeconds, nextBlockTimestamp-lastBlock.Timestamp))
	}
	solveSeconds := HeartbeatTargetSeconds
	if grandParent := GetBlockByHeightFromDB(lastBlock.Index - 1); grandParent.Hash != "" {
		solveSeconds = lastBlock.Timestamp - grandParent.Timestamp
	}
	return int(heartbeatRetarget(int64(lastBlock.Difficulty), solveSeconds, nextBlockTimestamp-lastBlock.Timestamp))
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
