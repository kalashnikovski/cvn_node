package node

import (
	"fmt"
	"math/big"
	"sync"
)

// THE DIFFICULTY RULE
//
// Blocks are spaced by proof of work alone:
//
//   - Difficulty is a fine-grained number: the average count of hashes a
//     block needs. A hash is valid when hash <= (2^256 - 1) / Difficulty.
//   - Difficulty is re-tuned on every block from how long the parent took:
//     faster than 45 s nudges it up, slower nudges it down. It also leans
//     against how far the whole chain is from its schedule, so over any long
//     run the chain's own timestamps average 45 s per block, whatever
//     hashrate joins or leaves.
//   - A block found 3 seconds after its parent is still valid. It just cost
//     real work, and it makes the next block harder. Nobody can keep producing
//     blocks every few seconds without out-hashing an ever-rising difficulty.
//   - If hashrate suddenly leaves and no block arrives for 6 minutes, the
//     difficulty of the block being mined halves, and halves again every
//     further 3 minutes, so the chain cannot flatline.
//
// The rule applies from the first block after the genesis block. (Its name in
// the code, "heartbeat", comes from the first test network, where it replaced
// an earlier rule part-way through the chain.)

const (
	HeartbeatTargetSeconds = int64(45)
	// Each block moves difficulty by at most 1/HeartbeatWindow upward (+5%).
	HeartbeatWindow = int64(20)
	// A parent slower than this is treated as this slow, capping one step at -50%.
	HeartbeatMaxSolveSeconds = 11 * HeartbeatTargetSeconds

	// Starting difficulty for a network that names none. The rule corrects a
	// wrong guess within a few dozen blocks in either direction.
	HeartbeatInitialDifficulty = 40000000
	// The lowest difficulty where a network's mining schedule names none.
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

// IsHeartbeatHeight reports whether the block at this height is mined under
// the difficulty rule: every block except the genesis block, which the
// program builds itself.
func IsHeartbeatHeight(height int64) bool {
	return height >= 1
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

// HashMeetsDifficulty is the proof-of-work check: the hash, read as a number,
// must be at or below the target for the difficulty.
func HashMeetsDifficulty(hashHex string, height int64, difficulty int) bool {
	if !IsHeartbeatHeight(height) {
		return false // the genesis block is not mined
	}
	if len(hashHex) != 64 || int64(difficulty) < minDifficultyAt(height) {
		return false
	}
	return hashHex <= HeartbeatTargetHex(difficulty)
}

// CalculateHeartbeatDifficulty returns the difficulty required of the block that
// follows lastBlock and is dated nextBlockTimestamp.
func CalculateHeartbeatDifficulty(lastBlock Block, nextBlockTimestamp int64) int {
	gapSeconds := difficultyClock(nextBlockTimestamp) - difficultyClock(lastBlock.Timestamp)
	if !inLaunchWindow(lastBlock.Index + 1) {
		return heartbeatDifficultyWithGap(lastBlock, launchRampGap(lastBlock.Index+1, gapSeconds))
	}
	// The launch window. The dates of the first blocks cannot be trusted to
	// say how hard they were to find: the genesis block is dated by a
	// Bitcoin miner's clock, which may be well off, and whoever mines the
	// first blocks may date them early so that the next ones look late. So
	// during these blocks no relief is given for a late block, and
	// difficulty does not go below the starting value. It can still rise.
	difficulty := heartbeatDifficultyWithGap(lastBlock, 0)
	if floor := launchFloorAt(lastBlock.Index + 1); int64(difficulty) < floor {
		return int(floor)
	}
	return difficulty
}

// difficultyClock is the date the difficulty rule reads off a block.
//
// The first block of a network that starts from a Bitcoin block is dated two
// hours before that Bitcoin block's own time, so that no miner ever has to
// wait for the clock at the start (see genesis.go). That leaves two hours of
// dates, all earlier than the launch itself, that a block may carry. If the
// difficulty rule took them at face value, a miner who found several blocks
// in a row could date them close together and then date one far later,
// without any real time having passed, and the rule would read that block as
// hours late and collapse the difficulty.
//
// So for the difficulty rule, and only for it, a date earlier than the
// Bitcoin block's own time counts as that time. Blocks dated before it are
// all "at the start": no time has passed between them, so difficulty can
// only rise. Time starts counting at the Bitcoin block's time, which is the
// earliest moment the network can really have begun.
func difficultyClock(timestamp int64) int64 {
	if start := difficultyClockStart(); timestamp < start {
		return start
	}
	return timestamp
}

// difficultyClockStart is the Bitcoin anchor block's own time on a network
// that starts from one, and zero on any other.
func difficultyClockStart() int64 {
	if !Params.Anchor.required() || Params.Anchor.BitcoinHeader == "" {
		return 0
	}
	return Params.GenesisTimestamp + bitcoinClockAllowance
}

// launchFloorAt is the lowest difficulty allowed at a height inside the
// launch window: the starting difficulty of the mining algorithm in force
// there. (Only a test network changes algorithm this early.)
func launchFloorAt(height int64) int64 {
	if era := powEraAt(height); era != nil && era.FromHeight > 1 {
		return era.StartDifficulty
	}
	return launchDifficulty()
}

// HeartbeatLaunchBlocks is the length of the launch window, in blocks: about
// fifteen minutes.
const HeartbeatLaunchBlocks = 20

// HeartbeatRampBlocks is how many blocks after the launch window relief for
// a late block takes to return in full.
const HeartbeatRampBlocks = 40

// launchRampGap limits how late a block just after the launch window may be
// counted as being. Relief for a late block halves the difficulty once for
// six minutes and again for every three after that. If all of it came back
// on the first block after the window, whoever mined the window's blocks
// could date them close together, let the real clock run on, and have the
// next block read as hours late: the very burst the window prevents, one
// block later. So the halvings allowed grow by one per block: one for the
// first block after the window, two for the second, and so on, until after
// HeartbeatRampBlocks blocks the rule is the ordinary one.
func launchRampGap(height, gapSeconds int64) int64 {
	allowed := height - HeartbeatLaunchBlocks
	if allowed < 1 || allowed >= HeartbeatRampBlocks {
		return gapSeconds
	}
	if limit := HeartbeatStallAfterSeconds + allowed*HeartbeatStallStepSeconds - 1; gapSeconds > limit {
		return limit
	}
	return gapSeconds
}

// inLaunchWindow reports whether a block at this height is one of the first
// blocks of a network that starts the launch way.
func inLaunchWindow(height int64) bool {
	return height >= 1 && height <= HeartbeatLaunchBlocks
}

// launchDifficulty is the network's starting difficulty.
func launchDifficulty() int64 {
	if Params.HeartbeatInitialDifficulty >= 1 {
		return Params.HeartbeatInitialDifficulty
	}
	return HeartbeatInitialDifficulty
}

// heartbeatDifficultyWithGap is the difficulty rule proper. gapSeconds is how
// long after lastBlock the next block is dated, as counted for late-block
// relief.
func heartbeatDifficultyWithGap(lastBlock Block, gapSeconds int64) int {
	base := int64(lastBlock.Difficulty)
	if Params.ReliefNotInherited {
		base = unrelievedDifficulty(lastBlock)
	}
	return heartbeatDifficultyFromBase(lastBlock, base, gapSeconds)
}

// heartbeatDifficultyFromBase is the rule with the parent's difficulty given
// separately. `base` is what the next block's difficulty is worked out from:
// the difficulty lastBlock carries, or, on a network where late-block relief
// is not inherited, the difficulty it would have carried without relief.
func heartbeatDifficultyFromBase(lastBlock Block, base int64, gapSeconds int64) int {
	if !IsHeartbeatHeight(lastBlock.Index) {
		// lastBlock is the genesis block: start from the network's starting
		// difficulty.
		initial := Params.HeartbeatInitialDifficulty
		if initial < 1 {
			initial = HeartbeatInitialDifficulty
		}
		return int(heartbeatRetargetAnchored(initial, HeartbeatTargetSeconds, gapSeconds, 0, minDifficultyAt(lastBlock.Index+1)))
	}
	minimum := minDifficultyAt(lastBlock.Index + 1)
	if era := powEraStartingAt(lastBlock.Index + 1); era != nil && era.StartDifficulty > 0 {
		// The mining algorithm changes at this block, so the parent's
		// difficulty does not carry over: restart from the era's own value.
		return int(heartbeatRetargetAnchored(era.StartDifficulty, HeartbeatTargetSeconds, gapSeconds, 0, minimum))
	}
	solveSeconds := HeartbeatTargetSeconds
	if grandParent := GetBlockByHeightFromDB(lastBlock.Index - 1); grandParent.Hash != "" {
		solveSeconds = difficultyClock(lastBlock.Timestamp) - difficultyClock(grandParent.Timestamp)
	}
	if lastBlock.Index <= 1 {
		// Block 1 follows the genesis block, whose date says nothing about how
		// long block 1 took to find.
		solveSeconds = HeartbeatTargetSeconds
	}
	return int(heartbeatRetargetAnchored(base, solveSeconds, gapSeconds, scheduleDriftSeconds(lastBlock), minimum))
}

// LATE-BLOCK RELIEF IS NOT INHERITED (Params.ReliefNotInherited)
//
// A block dated six minutes or more after its parent is mined at a halved
// difficulty (halved again for every further three minutes). That keeps a
// chain moving when miners leave. Under the first form of the rule the next
// block then started from that halved figure, and was cut a second time for
// the same delay through the solve-time term: one 14-minute gap left the
// following blocks at a sixteenth of the difficulty, and it took some
// seventy-five blocks, each raising it by at most 5%, to climb back.
//
// Whoever holds most of the hashrate collects those cheap blocks. Measured
// in simulation, a miner with 94% of the hashrate who mined in bursts on
// purpose took about a third more coins than by mining steadily, and blocks
// came every 32 seconds instead of 45 (see difficulty_candidates_sim_test.go).
// It was also seen on devnet when the largest miner restarted.
//
// So on networks with this setting the relief belongs to the late block
// alone. The next block is worked out from the difficulty its parent would
// have had with no relief, and is then lowered in the ordinary way for the
// parent having been slow (by at most half). A chain that really has lost its
// miners still comes down by half per block, with relief again for each block
// that is late; a miner who only paused finds the difficulty close to where
// it was.

// reliefGapSeconds is how late block b counted as being when it was mined:
// the gap the rule used to decide its late-block relief.
func reliefGapSeconds(b Block, parent Block) int64 {
	if inLaunchWindow(b.Index) {
		return 0 // no relief is given inside the launch window
	}
	return launchRampGap(b.Index, difficultyClock(b.Timestamp)-difficultyClock(parent.Timestamp))
}

// unrelievedCache remembers the answer of unrelievedDifficulty by block
// identifier. The answer depends only on the block and its ancestors, which
// the identifier fixes, so it never goes stale; the table is simply emptied
// when it grows large or the network settings change.
var unrelievedCache = struct {
	sync.Mutex
	byHash map[string]int64
}{byHash: make(map[string]int64)}

const unrelievedCacheLimit = 4096

func resetUnrelievedCache() {
	unrelievedCache.Lock()
	unrelievedCache.byHash = make(map[string]int64)
	unrelievedCache.Unlock()
}

// unrelievedDifficulty returns the difficulty block b would have carried had
// it been given no late-block relief. For a block that was not late that is
// simply its difficulty. For a late block it is the rule applied to its
// parent with no gap, which in turn needs the parent's own unrelieved
// difficulty; so the walk goes back through consecutive late blocks to the
// first one that was on time, and then works forward again.
//
// The halved figure cannot simply be doubled back: the halving discards the
// low bits, and a difficulty that reached the network's minimum was stopped
// there, so the figure before relief is not recoverable from the block alone.
func unrelievedDifficulty(b Block) int64 {
	type step struct{ block, parent Block }
	var late []step // b and its late ancestors, newest first
	current := b
	exact := true // false when an ancestor needed for the answer is not in the database
	var base int64
	for {
		if current.Hash != "" {
			unrelievedCache.Lock()
			known, found := unrelievedCache.byHash[current.Hash]
			unrelievedCache.Unlock()
			if found {
				base = known
				break
			}
		}
		if current.Index <= 1 || !IsHeartbeatHeight(current.Index) || inLaunchWindow(current.Index) {
			base = int64(current.Difficulty)
			break
		}
		parent := GetBlockByHeightFromDB(current.Index - 1)
		if parent.Hash == "" || parent.Hash != current.PrevHash {
			// The parent is not the block we hold at that height (or we hold
			// none). Nothing sound can be said from here; use what the block
			// carries and remember nothing.
			base = int64(current.Difficulty)
			exact = false
			break
		}
		if reliefGapSeconds(current, parent) < HeartbeatStallAfterSeconds {
			base = int64(current.Difficulty)
			break
		}
		late = append(late, step{current, parent})
		current = parent
	}
	remember := func(block Block, value int64) {
		if !exact || block.Hash == "" {
			return
		}
		unrelievedCache.Lock()
		if len(unrelievedCache.byHash) >= unrelievedCacheLimit {
			unrelievedCache.byHash = make(map[string]int64)
		}
		unrelievedCache.byHash[block.Hash] = value
		unrelievedCache.Unlock()
	}
	remember(current, base)
	// Forward again: each late block's unrelieved difficulty is the rule
	// applied to its parent, from the parent's unrelieved difficulty, with
	// no gap.
	for i := len(late) - 1; i >= 0; i-- {
		base = int64(heartbeatDifficultyFromBase(late[i].parent, base, 0))
		remember(late[i].block, base)
	}
	return base
}

// scheduleDriftSeconds says how far the chain is from its schedule at
// lastBlock: the time actually taken since the schedule's first block, minus
// the time the blocks since then should have taken. Negative means the chain
// is ahead (blocks have come too fast); positive means it is behind.
//
// The schedule is counted from the last block of the launch window. The
// dates of the blocks before that cannot be trusted (see
// CalculateHeartbeatDifficulty), and counting from one of them would carry
// whatever was written on it into every later difficulty.
func scheduleDriftSeconds(lastBlock Block) int64 {
	const origin = int64(HeartbeatLaunchBlocks)
	if lastBlock.Index <= origin {
		return 0
	}
	firstBlock := GetBlockByHeightFromDB(origin)
	if firstBlock.Hash == "" {
		return 0
	}
	elapsed := difficultyClock(lastBlock.Timestamp) - difficultyClock(firstBlock.Timestamp)
	scheduled := (lastBlock.Index - origin) * HeartbeatTargetSeconds
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

// heartbeatRetargetAnchored is the arithmetic of the rule. Looking only at
// the parent block, blocks would come a few percent faster than the target
// over a long run (the first test network's did). So the rule also looks at
// how far the whole chain is from its schedule and leans against that, and
// the long-run average is the target exactly.
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
