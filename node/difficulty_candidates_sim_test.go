package node

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"testing"
)

// A comparison of candidate difficulty rules under stop-start mining and
// under honest hashrate swings. Nothing here is a network rule: the
// candidates exist only in this file. It prints a table and runs only when
// asked for:
//
//	CVN_DIFFICULTY_CANDIDATES=1 go test -run DifficultyCandidates -v ./node

// simChain is what a rule may look at when asked for the difficulty of the
// block being mined.
type simChain struct {
	height       int64   // height of the block being mined (first is 1)
	parentDiff   float64 // difficulty the parent was mined at
	parentSolve  float64 // seconds the parent took (its own gap)
	elapsed      float64 // seconds from the schedule's start to the parent
	startDiff    float64 // difficulty the chain started at
	parentRelief float64 // number of late-block halvings the parent was given
}

// candidateRule gives the difficulty of the block being mined when it is
// dated gapSeconds after its parent.
type candidateRule struct {
	name string
	at   func(c simChain, gapSeconds float64) float64
	// stepped rules only change at the late-block thresholds, which lets the
	// simulation skip recomputing them every second.
	stepped bool
}

const (
	simTarget   = float64(HeartbeatTargetSeconds)
	simMinimum  = 5000.0
	simStallAt  = float64(HeartbeatStallAfterSeconds)
	simStallGap = float64(HeartbeatStallStepSeconds)
)

func simHalvings(gap float64) float64 {
	if gap < simStallAt {
		return 0
	}
	return math.Min(40, math.Floor((gap-simStallAt)/simStallGap)+1)
}

// heartbeatFamily is the present rule written with switches, so that single
// parts of it can be changed.
type heartbeatFamily struct {
	aheadCapPercent  float64 // limit of the schedule correction when the chain is ahead
	behindCapPercent float64 // and when it is behind
	reliefInherited  bool    // present rule: the next block starts from the relieved difficulty
	solveAfterRelief bool    // present rule: a relieved parent's lateness also counts in the solve-time term
	maxSolveSeconds  float64
	// exponentialStep applies each block's step as e^x where the present
	// rule applies 1+x. Small steps are the same; but steps then multiply
	// to something that depends only on their sum, that is, on how much
	// time the blocks' dates span, and not on how that time is shared out
	// between them.
	exponentialStep bool
}

func (f heartbeatFamily) at(c simChain, gap float64) float64 {
	base := c.parentDiff
	if !f.reliefInherited {
		base *= math.Pow(2, c.parentRelief)
	}
	solve := math.Max(1, math.Min(c.parentSolve, f.maxSolveSeconds))
	if !f.solveAfterRelief && c.parentRelief > 0 {
		solve = simTarget
	}
	scale := simTarget * float64(HeartbeatWindow) * float64(HeartbeatDriftDamping)
	drift := c.elapsed - float64(c.height-1)*simTarget
	correction := -drift
	if limit := scale * f.aheadCapPercent / 100; correction > limit {
		correction = limit
	}
	if limit := scale * f.behindCapPercent / 100; correction < -limit {
		correction = -limit
	}
	step := ((simTarget-solve)*float64(HeartbeatDriftDamping) + correction) / scale
	difficulty := base * (1 + step)
	if f.exponentialStep {
		difficulty = base * math.Exp(step)
	}
	difficulty /= math.Pow(2, simHalvings(gap))
	return math.Max(simMinimum, difficulty)
}

// scheduleRule is a different kind of rule: difficulty depends only on how far
// the chain is from its schedule at the moment the block is dated. Every
// halfLife seconds ahead of schedule doubles it; every halfLife behind halves
// it. There is no separate late-block relief and no per-block step limit.
func scheduleRule(halfLife float64) func(c simChain, gap float64) float64 {
	return func(c simChain, gap float64) float64 {
		behind := c.elapsed + gap - float64(c.height)*simTarget
		return math.Max(simMinimum, c.startDiff*math.Pow(2, -behind/halfLife))
	}
}

func candidateRules() []candidateRule {
	present := heartbeatFamily{aheadCapPercent: 1, behindCapPercent: 1, reliefInherited: true, solveAfterRelief: true, maxSolveSeconds: float64(HeartbeatMaxSolveSeconds)}
	noDouble := present
	noDouble.solveAfterRelief = false
	notInherited := present
	notInherited.reliefInherited = false
	strongAhead := present
	strongAhead.aheadCapPercent = 3
	both := notInherited
	both.aheadCapPercent = 3
	exponential := notInherited
	exponential.exponentialStep = true
	exponentialLongCap := exponential
	exponentialLongCap.maxSolveSeconds = 1800
	return []candidateRule{
		{"present rule", present.at, true},
		{"A: relief not counted twice", noDouble.at, true},
		{"B: relief not inherited", notInherited.at, true},
		{"C: 3% correction when ahead", strongAhead.at, true},
		{"D: B and C together", both.at, true},
		{"H: B with exponential steps", exponential.at, true},
		{"I: H, slow block counted to 30 min", exponentialLongCap.at, true},
		{"schedule rule, 10 min", scheduleRule(600), false},
		{"schedule rule, 15 min", scheduleRule(900), false},
		{"E: schedule rule, 30 min", scheduleRule(1800), false},
		{"F: schedule rule, 1 hour", scheduleRule(3600), false},
		{"G: schedule rule, 2 hours", scheduleRule(7200), false},
	}
}

type candidateRun struct {
	seconds      float64
	blocks       int
	hopperBlocks int
	hopperHashes float64
	loyalHashes  float64
	lateBlocks   int       // blocks dated six minutes or more after their parent
	gaps         []float64 // every block's gap, when asked for
	diffs        []float64
}

// runCandidate mines `blocks` blocks second by second. loyalRate(height) is
// the hashrate that always mines; the hopper mines only while difficulty is
// at or below hopperLimit.
func runCandidate(rule candidateRule, blocks int, startDiff float64, loyalRate func(height int64) float64, hopperRate, hopperLimit float64, seed int64, keep bool) candidateRun {
	random := rand.New(rand.NewSource(seed))
	c := simChain{height: 1, parentDiff: startDiff, parentSolve: simTarget, startDiff: startDiff}
	var run candidateRun
	for ; c.height <= int64(blocks); c.height++ {
		loyal := loyalRate(c.height)
		var gap, difficulty float64
		hopperWon := false
		for {
			gap++
			if !rule.stepped || gap == 1 || (gap >= simStallAt && math.Mod(gap-simStallAt, simStallGap) == 0) {
				difficulty = rule.at(c, gap)
			}
			rate := loyal
			hopperOn := hopperRate > 0 && difficulty <= hopperLimit
			if hopperOn {
				rate += hopperRate
				run.hopperHashes += hopperRate
			}
			run.loyalHashes += loyal
			if random.Float64() < 1-math.Exp(-rate/difficulty) {
				hopperWon = hopperOn && random.Float64() < hopperRate/rate
				break
			}
		}
		if hopperWon {
			run.hopperBlocks++
		}
		if gap >= simStallAt {
			run.lateBlocks++
		}
		if keep {
			run.gaps = append(run.gaps, gap)
			run.diffs = append(run.diffs, difficulty)
		}
		c.parentRelief = 0
		if rule.stepped {
			c.parentRelief = simHalvings(gap)
		}
		c.parentDiff, c.parentSolve = difficulty, gap
		c.elapsed += gap
		run.seconds += gap
		run.blocks++
	}
	return run
}

func TestDifficultyCandidates(t *testing.T) {
	if os.Getenv("CVN_DIFFICULTY_CANDIDATES") == "" {
		t.Skip("set CVN_DIFFICULTY_CANDIDATES=1 to print the comparison")
	}
	const totalRate = 20000.0
	fair := totalRate * simTarget
	steady := func(rate float64) func(int64) float64 { return func(int64) float64 { return rate } }

	// The float copy of the present rule must behave like the real one.
	{
		real := simulateHopping(200000, 0.94, 0.5, true, 7)
		copyRun := runCandidate(candidateRules()[0], 200000, fair, steady(totalRate*0.06), totalRate*0.94, 0.5*fair, 7, false)
		t.Logf("check: real rule %.2f s/block and hopper wins %.1f%%; float copy %.2f s/block and %.1f%%",
			real.averageSeconds, real.hopperBlockPart*100, copyRun.seconds/float64(copyRun.blocks), float64(copyRun.hopperBlocks)/float64(copyRun.blocks)*100)
	}

	t.Log("")
	t.Log("STOP-START MINING. For each share of hashrate, the stop-start miner's best result over six thresholds.")
	t.Log("coins = coins per day against mining steadily; edge = coins per hash against the loyal miners; s = seconds per block at that threshold")
	shares := []float64{0.10, 0.25, 0.50, 0.75, 0.94}
	thresholds := []float64{1.0, 0.9, 0.75, 0.5, 0.25, 0.1}
	for _, rule := range candidateRules() {
		line := fmt.Sprintf("%-30s", rule.name)
		for _, share := range shares {
			bestCoins, bestEdge, secondsAtBest := -1.0, 0.0, 0.0
			for _, threshold := range thresholds {
				run := runCandidate(rule, 150000, fair, steady(totalRate*(1-share)), totalRate*share, threshold*fair, 11, false)
				if run.hopperHashes == 0 {
					continue
				}
				coins := (float64(run.hopperBlocks) / run.seconds) / (share / simTarget)
				loyalPerHash := float64(run.blocks-run.hopperBlocks) / run.loyalHashes
				edge := (float64(run.hopperBlocks) / run.hopperHashes) / loyalPerHash
				if coins > bestCoins {
					bestCoins, secondsAtBest = coins, run.seconds/float64(run.blocks)
				}
				// The per-hash edge only means something if the miner still mines a real part of the time.
				if run.hopperHashes/(totalRate*share*run.seconds) > 0.10 && edge > bestEdge {
					bestEdge = edge
				}
			}
			line += fmt.Sprintf(" | %2.0f%%: coins %+4.0f%% edge %+4.0f%% %4.1fs", share*100, (bestCoins-1)*100, (bestEdge-1)*100, secondsAtBest)
		}
		t.Log(line)
	}

	t.Log("")
	t.Log("HONEST CONDITIONS. steady: seconds per block, spread of difficulty, blocks six minutes late per 10,000.")
	t.Log("drop: hashrate falls to a tenth; minutes for the next 30 blocks (22.5 is on schedule) and the longest single wait.")
	t.Log("rise: hashrate rises tenfold; blocks found in the first 15 minutes after (20 is on schedule).")
	for _, rule := range candidateRules() {
		run := runCandidate(rule, 200000, fair, steady(totalRate), 0, 0, 3, true)
		var sum, squares float64
		for _, d := range run.diffs[1000:] {
			sum += d
			squares += d * d
		}
		n := float64(len(run.diffs) - 1000)
		mean := sum / n
		spread := math.Sqrt(squares/n-mean*mean) / mean

		var dropMinutes, longest, riseBlocks float64
		const trials = 200
		for trial := int64(0); trial < trials; trial++ {
			drop := runCandidate(rule, 2030, fair, func(h int64) float64 {
				if h > 2000 {
					return totalRate / 10
				}
				return totalRate
			}, 0, 0, 100+trial, true)
			for _, gap := range drop.gaps[2000:] {
				dropMinutes += gap / 60
				longest = math.Max(longest, gap/60)
			}
			rise := runCandidate(rule, 2400, fair, func(h int64) float64 {
				if h > 2000 {
					return totalRate * 10
				}
				return totalRate
			}, 0, 0, 500+trial, true)
			var waited float64
			for _, gap := range rise.gaps[2000:] {
				waited += gap
				if waited > 900 {
					break
				}
				riseBlocks++
			}
		}
		t.Logf("%-30s | steady %.2f s, spread %4.1f%%, late %5.1f | drop: %5.1f min, longest %5.1f min | rise: %5.1f blocks",
			rule.name, run.seconds/float64(run.blocks), spread*100, float64(run.lateBlocks)/float64(run.blocks)*10000,
			dropMinutes/trials, longest, riseBlocks/trials)
	}
}
