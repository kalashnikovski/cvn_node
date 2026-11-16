package node

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"testing"
)

// Candidate difficulty rules against a miner who chooses the dates of its
// blocks (audit round 4, finding 1). The candidates are the ones in
// difficulty_candidates_sim_test.go; nothing here is a network rule.
//
//	CVN_DATES_CANDIDATES=1 go test -run DatesCandidates -v ./node

// dateStrategy gives how long after its parent the attacker dates the block
// it is working on. sinceJump counts its blocks since it last dated one six
// minutes or more after its parent; furthest is the latest date the clock
// allows, as seconds after the parent.
type dateStrategy struct {
	name string
	gap  func(sinceJump int, furthest float64) float64
}

func honestDates() dateStrategy {
	return dateStrategy{"honest", nil}
}

// fastThenJump dates `run` blocks one second after their parents and then one
// block `jump` seconds after its parent, once the clock allows it.
func fastThenJump(run int, jump float64) dateStrategy {
	return dateStrategy{fmt.Sprintf("%d fast then +%.0f s", run, jump), func(sinceJump int, furthest float64) float64 {
		if sinceJump >= run && jump <= furthest {
			return jump
		}
		return 1
	}}
}

// alwaysAhead dates every block as far ahead as the clock allows.
func alwaysAhead() dateStrategy {
	return dateStrategy{"always as far ahead as allowed", func(_ int, furthest float64) float64 { return furthest }}
}

type datesRun struct {
	secondsPerBlock float64
	coins           float64 // attacker's coins per day against steady honest mining
	stalled         bool    // the pattern drove difficulty up without end
}

func runChosenDates(rule candidateRule, blocks int, share float64, strategy dateStrategy, seed int64) datesRun {
	const totalRate = 20000.0
	random := rand.New(rand.NewSource(seed))
	attackerRate, honestRate := totalRate*share, totalRate*(1-share)
	fair := totalRate * simTarget
	c := simChain{height: 1, parentDiff: fair, parentSolve: simTarget, startDiff: fair}
	var now, parentDate float64 // the real clock, and the parent's date; both start at 0
	sinceJump, attackerWon := 0, 0
	for ; c.height <= int64(blocks); c.height++ {
		blockStart := now
		var gap, difficulty float64
		byAttacker := false
		for {
			now++
			if now-blockStart > 100000 {
				return datesRun{stalled: true}
			}
			honestGap := math.Max(1, now-parentDate)
			furthest := now + float64(HeartbeatMaxFutureDriftSeconds) - parentDate
			attackerGap := honestGap
			if strategy.gap != nil {
				attackerGap = math.Max(1, math.Min(strategy.gap(sinceJump, furthest), furthest))
			}
			attackerDifficulty := rule.at(c, attackerGap)
			pAttacker := 1 - math.Exp(-attackerRate/attackerDifficulty)
			roll := random.Float64()
			if roll < pAttacker {
				gap, difficulty, byAttacker = attackerGap, attackerDifficulty, true
				break
			}
			if honestRate > 0 {
				honestDifficulty := rule.at(c, honestGap)
				if roll < pAttacker+(1-math.Exp(-honestRate/honestDifficulty)) {
					gap, difficulty = honestGap, honestDifficulty
					break
				}
			}
		}
		if byAttacker {
			attackerWon++
			if gap >= simStallAt {
				sinceJump = 0
			} else {
				sinceJump++
			}
		}
		c.parentRelief = 0
		if rule.stepped {
			c.parentRelief = simHalvings(gap)
		}
		c.parentDiff, c.parentSolve = difficulty, gap
		c.elapsed += gap
		parentDate += gap
	}
	return datesRun{
		secondsPerBlock: now / float64(blocks),
		coins:           (float64(attackerWon) / now) / (share / simTarget),
	}
}

func TestDatesCandidates(t *testing.T) {
	if os.Getenv("CVN_DATES_CANDIDATES") == "" {
		t.Skip("set CVN_DATES_CANDIDATES=1 to print the comparison")
	}
	const blocks = 20000
	strategies := []dateStrategy{alwaysAhead()}
	for _, run := range []int{4, 6, 8, 10, 12, 16, 24} {
		for _, jump := range []float64{360, 495, 720, 1260} {
			strategies = append(strategies, fastThenJump(run, jump))
		}
	}
	wanted := map[string]bool{
		"B: relief not inherited": true, "H: B with exponential steps": true, "I: H, slow block counted to 30 min": true,
		"schedule rule, 10 min": true, "schedule rule, 15 min": true,
	}
	t.Log("For each rule and share: the best of 29 date patterns for the attacker. coins = against steady honest mining; s = seconds per block under that pattern.")
	for _, rule := range candidateRules() {
		if !wanted[rule.name] {
			continue
		}
		line := fmt.Sprintf("%-36s", rule.name)
		for _, share := range []float64{1.0, 0.94, 0.85, 0.60} {
			honest := runChosenDates(rule, blocks, share, honestDates(), 9)
			best, bestName := honest, "honest"
			for _, strategy := range strategies {
				r := runChosenDates(rule, blocks, share, strategy, 9)
				if !r.stalled && r.coins > best.coins {
					best, bestName = r, strategy.name
				}
			}
			line += fmt.Sprintf(" | %3.0f%%: %+5.1f%% at %4.1f s (%s; honest %4.1f s)", share*100, (best.coins-1)*100, best.secondsPerBlock, bestName, honest.secondsPerBlock)
		}
		t.Log(line)
	}
}
