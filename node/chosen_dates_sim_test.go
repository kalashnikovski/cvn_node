package node

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"testing"
)

// Audit round 4, finding 1 (Claude): a miner with nearly all the hashrate can
// gain by choosing the dates of its blocks. It dates a run of blocks one
// second after their parents, which raises difficulty by about 5% a block
// while the chain's dates fall behind the clock; then it dates one block far
// after its parent (still within what the clock allows), which halves the
// difficulty in one step. Twelve small rises and one halving cancel, but the
// thirteen blocks' dates span far less than thirteen block intervals, so the
// rule believes blocks are on time while they come faster.
//
// This simulation runs that strategy second by second against the real
// arithmetic (heartbeatRetargetAnchored, with late-block relief not
// inherited), with honest miners who date their blocks truthfully.

type chosenDatesResult struct {
	secondsPerBlock float64
	attackerCoins   float64 // coins per day against mining steadily with honest dates
	attackerBlocks  float64 // share of all blocks
	// stalled: the pattern drove difficulty up without end, which costs the
	// miner who tries it everything. The other figures are then not filled in.
	stalled bool
}

// simHonestClaimAhead is how far ahead of the clock the honest miners of the
// simulation will date a block in order to reach a late-block halving. Zero
// is the first behaviour: they date with the true time.
var simHonestClaimAhead int64

// simulateChosenDates: the attacker dates `run` blocks one second after their
// parents, then one block `jump` seconds after its parent (as far as the clock
// allows). run = 0 means the attacker dates honestly. run = -1 is another
// pattern (round 5): every block one second after its parent, except that
// the moment the clock allows a date `jump` seconds after the parent, the
// block is dated exactly that. A jump of zero or less
// means "as far ahead as the clock allows, once that is at least -jump
// seconds after the parent".
func simulateChosenDates(blocks int, attackerShare float64, run int, jump int64, seed int64) chosenDatesResult {
	const totalRate = 20000.0
	random := rand.New(rand.NewSource(seed))
	attackerRate := totalRate * attackerShare
	honestRate := totalRate - attackerRate
	fair := int64(totalRate * float64(HeartbeatTargetSeconds))

	base := fair // what the block being mined is worked out from (rule B)
	parentSolve := HeartbeatTargetSeconds
	var now, parentDate, firstDate int64 // real clock, and the dates on the chain
	now = 1000
	parentDate = now
	firstDate = parentDate
	sinceJump := 0
	attackerWon := 0

	// The difficulty of the block being mined depends on its date only
	// through the number of late-block halvings, so the real rule is asked
	// once for each number of halvings that comes up, not once a second.
	var known map[int64]int64
	difficultyAt := func(index int, date int64) int64 {
		gap := date - parentDate
		halvings := int64(0)
		if gap >= HeartbeatStallAfterSeconds {
			halvings = (gap-HeartbeatStallAfterSeconds)/HeartbeatStallStepSeconds + 1
		}
		if value, found := known[halvings]; found {
			return value
		}
		drift := (parentDate - firstDate) - int64(index-1)*HeartbeatTargetSeconds
		value := heartbeatRetargetAnchored(base, parentSolve, gap, drift, 5000)
		known[halvings] = value
		return value
	}
	for i := 1; i <= blocks; i++ {
		known = make(map[int64]int64)
		var date int64
		byAttacker := false
		blockStarted := now
		for {
			now++
			if now-blockStarted > 200000 {
				return chosenDatesResult{stalled: true}
			}
			// Honest miners date a block with the true time (or just after its parent).
			honestDate := now
			if honestDate <= parentDate {
				honestDate = parentDate + 1
			}
			if simHonestClaimAhead > 0 {
				honestDate = dateClaimingRelief(parentDate, honestDate, now+simHonestClaimAhead)
			}
			// The attacker's date for this second.
			attackerDate := honestDate
			if run == -1 {
				attackerDate = parentDate + 1
				if parentDate+jump <= now+HeartbeatMaxFutureDriftSeconds {
					attackerDate = parentDate + jump
				}
			}
			if run > 0 {
				attackerDate = parentDate + 1
				if sinceJump >= run {
					limit := now + HeartbeatMaxFutureDriftSeconds
					if want := parentDate + jump; jump > 0 && want <= limit {
						attackerDate = want
					} else if jump <= 0 && limit-parentDate >= -jump {
						// jump <= 0: as far ahead as the clock allows, once that is
						// at least -jump seconds after the parent.
						attackerDate = limit
					}
				}
			}
			// A block dated ahead of the clock by more than the limit is refused.
			if attackerDate > now+HeartbeatMaxFutureDriftSeconds {
				attackerDate = now + HeartbeatMaxFutureDriftSeconds
			}
			pAttacker := 1 - math.Exp(-attackerRate/float64(difficultyAt(i, attackerDate)))
			pHonest := 0.0
			if honestRate > 0 {
				pHonest = 1 - math.Exp(-honestRate/float64(difficultyAt(i, honestDate)))
			}
			roll := random.Float64()
			if roll < pAttacker {
				date, byAttacker = attackerDate, true
				break
			}
			if roll < pAttacker+pHonest {
				date = honestDate
				break
			}
		}
		drift := (parentDate - firstDate) - int64(i-1)*HeartbeatTargetSeconds
		gap := date - parentDate
		base = heartbeatRetargetAnchored(base, parentSolve, 0, drift, 5000) // unrelieved
		parentSolve = gap
		parentDate = date
		if byAttacker {
			attackerWon++
			if run > 0 && gap >= HeartbeatStallAfterSeconds {
				sinceJump = 0
			} else {
				sinceJump++
			}
		}
	}
	elapsed := float64(now - 1000)
	return chosenDatesResult{
		secondsPerBlock: elapsed / float64(blocks),
		attackerCoins:   (float64(attackerWon) / elapsed) / (attackerShare / float64(HeartbeatTargetSeconds)),
		attackerBlocks:  float64(attackerWon) / float64(blocks),
	}
}

// TestChosenDatesTable prints what the strategy earns. It runs only when
// asked for: CVN_CHOSEN_DATES_TABLE=1 go test -run ChosenDatesTable -v ./node
func TestChosenDatesTable(t *testing.T) {
	if os.Getenv("CVN_CHOSEN_DATES_TABLE") == "" {
		t.Skip("set CVN_CHOSEN_DATES_TABLE=1 to print the table")
	}
	const blocks = 20000
	for _, share := range []float64{1.0, 0.94, 0.85, 0.75, 0.50} {
		honest := simulateChosenDates(blocks, share, 0, 0, 5)
		line := fmt.Sprintf("share %3.0f%%: honest dates %.2f s/block, coins %+5.1f%%", share*100, honest.secondsPerBlock, (honest.attackerCoins-1)*100)
		// Runs longer than about eleven blocks do not balance: difficulty
		// climbs without end and the simulation would not finish.
		for _, run := range []int{8, 10} {
			r := simulateChosenDates(blocks, share, run, 495, 5)
			line += fmt.Sprintf(" | %d fast then +495 s: %.2f s/block, coins %+5.1f%%", run, r.secondsPerBlock, (r.attackerCoins-1)*100)
		}
		t.Log(line)
	}
}

// Choosing block dates must not pay, whatever share of the hashrate a miner
// has, and must not move the chain off its 45 seconds. Under the first form
// of the rule it paid a 94% miner about 9%; that comparison is kept in view.
func TestChoosingDatesDoesNotPayTheLargestMiner(t *testing.T) {
	const blocks = 20000
	for _, share := range []float64{1.0, 0.94, 0.85} {
		for _, pattern := range []struct {
			run  int
			jump int64
		}{{4, 495}, {8, 495}, {12, 495}, {6, 360}, {16, 720}} {
			r := simulateChosenDates(blocks, share, pattern.run, pattern.jump, 5)
			if r.stalled {
				continue // the pattern ruins the miner who tries it
			}
			if r.attackerCoins > 1.02 {
				t.Errorf("share %.0f%%, %d fast then +%d s: %.1f%% more coins than steady mining", share*100, pattern.run, pattern.jump, (r.attackerCoins-1)*100)
			}
			if math.Abs(r.secondsPerBlock-45) > 0.5 {
				t.Errorf("share %.0f%%, %d fast then +%d s: the chain ran at %.2f s per block, want 45", share*100, pattern.run, pattern.jump, r.secondsPerBlock)
			}
		}
	}
	// Round 5: a miner with part of the hashrate, dating every block one
	// second after its parent and claiming the late-block halving the moment
	// the clock allows. Honest miners here do what the program's miner now
	// does (minerBlockDate): they claim that halving as early as they will.
	// Compared with honest dating by the same miner, on the same random
	// numbers.
	const longRun = 60000
	simHonestClaimAhead = HeartbeatMaxFutureDriftSeconds - minerClockMargin
	for _, share := range []float64{0.94, 0.75, 0.60, 0.35} {
		honest := simulateChosenDates(longRun, share, 0, 0, 5)
		for _, jump := range []int64{360, 540} {
			r := simulateChosenDates(longRun, share, -1, jump, 5)
			if r.stalled {
				continue
			}
			if gain := r.attackerCoins - honest.attackerCoins; gain > 0.01 {
				t.Errorf("share %.0f%%, blocks one second apart and +%d s claimed early: %.2f%% more coins than the same miner dating honestly", share*100, jump, gain*100)
			}
		}
	}
	// Before honest miners did so, that pattern paid about 1%.
	simHonestClaimAhead = 0
	before := simulateChosenDates(longRun, 0.60, -1, 360, 5).attackerCoins - simulateChosenDates(longRun, 0.60, 0, 0, 5).attackerCoins
	t.Logf("with honest miners dating by the clock alone, for comparison: %.2f%% more coins at 60%% of the hashrate", before*100)
	if before < 0.006 {
		t.Error("the early claim was expected to pay when honest miners do not make it; the simulation no longer shows what it was written to show")
	}

	saved := Params.ExponentialSteps
	Params.ExponentialSteps = false
	old := simulateChosenDates(blocks, 0.94, 8, 495, 5)
	Params.ExponentialSteps = saved
	t.Logf("first form of the rule, for comparison: %.1f%% more coins, %.1f s per block", (old.attackerCoins-1)*100, old.secondsPerBlock)
	if old.attackerCoins < 1.05 {
		t.Error("the first form of the rule was expected to reward chosen dates; the simulation no longer shows what it was written to show")
	}
}

// TestRound5DatePatterns prints what the date patterns of the fifth review
// earn. CVN_CHOSEN_DATES_TABLE=1 go test -run Round5DatePatterns -v ./node
func TestRound5DatePatterns(t *testing.T) {
	if os.Getenv("CVN_CHOSEN_DATES_TABLE") == "" {
		t.Skip("set CVN_CHOSEN_DATES_TABLE=1 to print the table")
	}
	const blocks = 60000
	for _, claim := range []int64{0, 60, 110} {
		simHonestClaimAhead = claim
		t.Logf("honest miners date up to %d s ahead to reach a halving:", claim)
		for _, share := range []float64{1.0, 0.94, 0.75, 0.60, 0.35} {
			honest := simulateChosenDates(blocks, share, 0, 0, 5)
			line := fmt.Sprintf("  share %3.0f%%: honest %+5.2f%% (%.2f s)", share*100, (honest.attackerCoins-1)*100, honest.secondsPerBlock)
			for _, p := range []struct {
				name string
				run  int
				jump int64
			}{{"1 s apart, +360 early", -1, 360}, {"1 s apart, +540 early", -1, 540}, {"10 fast, +650", 10, 650}, {"10 fast, +720", 10, 720}} {
				r := simulateChosenDates(blocks, share, p.run, p.jump, 5)
				if r.stalled {
					line += fmt.Sprintf(" | %s: stalls", p.name)
				} else {
					line += fmt.Sprintf(" | %s: %+5.2f%% (%.2f s)", p.name, (r.attackerCoins-1)*100, r.secondsPerBlock)
				}
			}
			t.Log(line)
		}
	}
	simHonestClaimAhead = 0
}
