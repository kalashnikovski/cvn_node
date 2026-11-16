package node

import (
	"math"
	"math/big"
	"testing"
)

// expStepFactor is whole-number arithmetic that every node must agree on to
// the last digit. These values pin it: if a change of compiler, machine or
// code ever moved one of them, nodes would disagree about difficulty. They
// were checked against a separate implementation of the same series, and lie
// within six parts in 2^64 of the true e^x.
func TestExpStepFactorIsPinned(t *testing.T) {
	scale := HeartbeatTargetSeconds * HeartbeatWindow * HeartbeatDriftDamping // 180,000
	pinned := map[int64]string{
		0:      "18446744073709551616", // 2^64 exactly
		1:      "18446846555905744727",
		-1:     "18446641592082702457",
		8800:   "19370993578904439454", // the fastest parent, on schedule
		10600:  "19565675300958775648", // the largest step up
		-90000: "11188515852577165300", // the slowest parent, on schedule
		-91800: "11077188259743953767", // the largest step down
	}
	for numerator, want := range pinned {
		if got := expStepFactor(numerator, scale).String(); got != want {
			t.Errorf("expStepFactor(%d, %d) = %s, want %s", numerator, scale, got, want)
		}
	}
}

// It really is e^x: against floating point over the whole range the rule
// can ask for, and a little beyond.
func TestExpStepFactorIsTheExponential(t *testing.T) {
	scale := int64(180000)
	two64 := new(big.Float).SetInt(new(big.Int).Lsh(big.NewInt(1), 64))
	for numerator := int64(-200000); numerator <= 200000; numerator += 137 {
		got, _ := new(big.Float).Quo(new(big.Float).SetInt(expStepFactor(numerator, scale)), two64).Float64()
		want := math.Exp(float64(numerator) / float64(scale))
		if math.Abs(got-want) > want*1e-13 {
			t.Fatalf("expStepFactor(%d, %d) is %.16f of 2^64, e^x is %.16f", numerator, scale, got, want)
		}
	}
}

// The point of the change: a run of steps comes to what their sum says, so
// that sharing the same span of time out differently between blocks leaves
// difficulty where it would have been.
func TestStepsComeToTheirSum(t *testing.T) {
	onNetwork(t, "devnet")
	const start = int64(900000000)
	minimum := int64(5000)
	walk := func(solveTimes []int64) int64 {
		difficulty := start
		for _, solve := range solveTimes {
			difficulty = heartbeatRetargetAnchored(difficulty, solve, 10, 0, minimum)
		}
		return difficulty
	}
	// Thirteen blocks whose dates span 13 x 45 seconds, shared out three ways.
	even := walk([]int64{45, 45, 45, 45, 45, 45, 45, 45, 45, 45, 45, 45, 45})
	lumpy := walk([]int64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 573 - 0}) // 12 x 1 + 573 = 585; the last is capped at 495
	twoStep := walk([]int64{20, 70, 20, 70, 20, 70, 20, 70, 20, 70, 20, 70, 45})
	if even != start {
		t.Fatalf("thirteen on-time blocks moved difficulty from %d to %d", start, even)
	}
	if diff := math.Abs(float64(twoStep-start)) / float64(start); diff > 1e-6 {
		t.Errorf("blocks alternately 20 and 70 seconds apart left difficulty at %d, want %d", twoStep, start)
	}
	// The lumpy run is capped at 495 seconds for its slow block, so its steps
	// sum to (12 x 44 - 450)/900, above zero: difficulty must end HIGHER
	// than it began. Under the first form of the rule it ended lower, which
	// is what a miner choosing dates lived on.
	if lumpy <= start {
		t.Errorf("twelve fast blocks and one slow one left difficulty at %d, at or below the %d it began at", lumpy, start)
	}
	if want := float64(start) * math.Exp(float64(12*44-450)/900); math.Abs(float64(lumpy)-want) > want*1e-6 {
		t.Errorf("twelve fast blocks and one slow one left difficulty at %d, want about %.0f", lumpy, want)
	}

	Params.ExponentialSteps = false
	if old := walk([]int64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 573}); old >= start {
		t.Errorf("under the first form of the rule the same run was expected to end below %d, got %d; this test no longer shows the difference", start, old)
	}
}
