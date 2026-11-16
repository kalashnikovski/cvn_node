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
}

// simulateChosenDates: the attacker dates `run` blocks one second after their
// parents, then one block `jump` seconds after its parent (as far as the clock
// allows). run = 0 means the attacker dates honestly. A jump of zero or less
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

	difficultyAt := func(index int, date int64) int64 {
		drift := (parentDate - firstDate) - int64(index-1)*HeartbeatTargetSeconds
		return heartbeatRetargetAnchored(base, parentSolve, date-parentDate, drift, 5000)
	}
	for i := 1; i <= blocks; i++ {
		var date int64
		byAttacker := false
		for {
			now++
			// Honest miners date a block with the true time (or just after its parent).
			honestDate := now
			if honestDate <= parentDate {
				honestDate = parentDate + 1
			}
			// The attacker's date for this second.
			attackerDate := honestDate
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
