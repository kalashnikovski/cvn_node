package node

import (
	"math"
	"math/rand"
	"os"
	"testing"
)

// hoppingResult is what one simulated run of stop-start mining gives.
type hoppingResult struct {
	averageSeconds  float64 // seconds per block over the run
	hopperReturn    float64 // blocks per hash for the stop-start miner, 1.0 = what steady mining by everyone would pay
	loyalReturn     float64 // the same for the miners who never stop
	hopperOnShare   float64 // share of the time the stop-start miner was mining
	hopperBlockPart float64 // share of all blocks the stop-start miner won
	stalls          int     // blocks that needed late-block relief
}

// simulateHopping runs the launch difficulty rule against two groups of
// miners: loyal ones who always mine, and one "hopper" who mines only while
// the difficulty of the block being worked on is at or below
// threshold x (the difficulty at which everyone mining steadily would settle).
// The hopper sees the difficulty ease during a long wait and joins the moment
// it crosses the threshold. hopperShare is the hopper's part of all hashrate.
// reliefInherited chooses the first form of the rule (true) or the one the
// launch networks use, where late-block relief stays with the late block.
func simulateHopping(blocks int, hopperShare, threshold float64, reliefInherited bool, seed int64) hoppingResult {
	const totalRate = 20000.0 // hashes per second with everyone mining
	random := rand.New(rand.NewSource(seed))
	hopperRate := totalRate * hopperShare
	loyalRate := totalRate - hopperRate
	fairDifficulty := totalRate * float64(HeartbeatTargetSeconds)

	// base is what the block being mined is worked out from: its parent's
	// difficulty, with or without the parent's relief.
	base := int64(fairDifficulty)
	parentSolve := HeartbeatTargetSeconds
	var elapsed int64
	var hopperHashes, loyalHashes, hopperSeconds float64
	var hopperBlocks, loyalBlocks, stalls int

	for i := 1; i <= blocks; i++ {
		drift := elapsed - int64(i-1)*HeartbeatTargetSeconds
		remaining := random.ExpFloat64()
		var waited float64
		hopperWon := false
		segmentStart, segmentEnd := int64(0), HeartbeatStallAfterSeconds
		for {
			segmentDifficulty := float64(heartbeatRetargetAnchored(base, parentSolve, segmentStart, drift, 5000))
			hopperOn := segmentDifficulty <= threshold*fairDifficulty
			rate := loyalRate
			if hopperOn {
				rate += hopperRate
			}
			length := float64(segmentEnd - segmentStart)
			needed := math.Inf(1)
			if rate > 0 {
				needed = remaining * segmentDifficulty / rate
			}
			spent := math.Min(needed, length)
			loyalHashes += loyalRate * spent
			if hopperOn {
				hopperHashes += hopperRate * spent
				hopperSeconds += spent
			}
			if needed < length {
				waited = float64(segmentStart) + needed
				hopperWon = hopperOn && random.Float64() < hopperRate/rate
				break
			}
			remaining -= length * rate / segmentDifficulty
			segmentStart, segmentEnd = segmentEnd, segmentEnd+HeartbeatStallStepSeconds
		}
		gap := int64(math.Ceil(waited))
		if gap < 1 {
			gap = 1
		}
		if gap >= HeartbeatStallAfterSeconds {
			stalls++
		}
		if reliefInherited {
			base = heartbeatRetargetAnchored(base, parentSolve, gap, drift, 5000)
		} else {
			base = heartbeatRetargetAnchored(base, parentSolve, 0, drift, 5000)
		}
		parentSolve = gap
		elapsed += gap
		if hopperWon {
			hopperBlocks++
		} else {
			loyalBlocks++
		}
	}

	result := hoppingResult{
		averageSeconds:  float64(elapsed) / float64(blocks),
		hopperOnShare:   hopperSeconds / float64(elapsed),
		hopperBlockPart: float64(hopperBlocks) / float64(blocks),
		stalls:          stalls,
	}
	if hopperHashes > 0 {
		result.hopperReturn = float64(hopperBlocks) / hopperHashes * fairDifficulty
	}
	if loyalHashes > 0 {
		result.loyalReturn = float64(loyalBlocks) / loyalHashes * fairDifficulty
	}
	return result
}

// TestStopStartMiningTable prints what stop-start mining earns under the
// launch difficulty rule. It is a measurement, not a pass/fail rule, and
// runs only when asked for: CVN_HOPPING_TABLE=1 go test -run StopStartMiningTable -v ./node
// (CVN_HOPPING_TABLE=old prints it for the first form of the rule).
func TestStopStartMiningTable(t *testing.T) {
	if os.Getenv("CVN_HOPPING_TABLE") == "" {
		t.Skip("set CVN_HOPPING_TABLE=1 to print the table")
	}
	const blocks = 400000
	for _, share := range []float64{0.10, 0.25, 0.40, 0.50, 0.60, 0.75, 0.94} {
		for _, threshold := range []float64{math.Inf(1), 1.0, 0.9, 0.75, 0.5, 0.25, 0.1} {
			r := simulateHopping(blocks, share, threshold, os.Getenv("CVN_HOPPING_TABLE") == "old", 7)
			t.Logf("hopper %2.0f%% of hashrate, mines when difficulty <= %4.2f of fair: on %5.1f%% of the time, wins %5.1f%% of blocks, return hopper %.3f loyal %.3f, %.2f s/block, stalls %d",
				share*100, threshold, r.hopperOnShare*100, r.hopperBlockPart*100, r.hopperReturn, r.loyalReturn, r.averageSeconds, r.stalls)
		}
	}
}

// A miner with nearly all the hashrate must not earn more by mining in bursts
// than by mining steadily, and must not be able to pull the chain off its
// 45-second schedule. Under the first form of the rule both happened; this
// keeps the comparison in view.
func TestBurstMiningDoesNotPayTheLargestMiner(t *testing.T) {
	const blocks, share = 120000, 0.94
	coinsAgainstSteady := func(r hoppingResult) float64 {
		return (r.hopperBlockPart / r.averageSeconds) / (share / float64(HeartbeatTargetSeconds))
	}
	for _, threshold := range []float64{1.0, 0.9, 0.75, 0.5, 0.25, 0.1} {
		r := simulateHopping(blocks, share, threshold, false, 21)
		if coins := coinsAgainstSteady(r); coins > 1.03 {
			t.Errorf("mining only at or below %.2f of the fair difficulty paid %.0f%% more coins than steady mining", threshold, (coins-1)*100)
		}
		if math.Abs(r.averageSeconds-45) > 0.5 {
			t.Errorf("at threshold %.2f the chain ran at %.2f s per block, want 45", threshold, r.averageSeconds)
		}
	}
	old := simulateHopping(blocks, share, 0.5, true, 21)
	t.Logf("first form of the rule, for comparison: %.0f%% more coins, %.1f s per block", (coinsAgainstSteady(old)-1)*100, old.averageSeconds)
	if coinsAgainstSteady(old) < 1.2 {
		t.Error("the first form of the rule was expected to reward burst mining; the simulation no longer shows what it was written to show")
	}
}
