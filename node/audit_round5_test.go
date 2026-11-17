package node

import (
	"testing"
)

// Round 5: the whole rule, not only the series inside it, is pinned at its
// limits: the smallest difficulty with the largest step up, a mid value and
// the largest difficulty with the largest step down. The multiplication, the
// shift by 64 bits, the floor and the ceiling are all in these figures.
func TestTheWholeStepIsPinned(t *testing.T) {
	onNetwork(t, "devnet")
	const farAhead, farBehind = -1000000, 1000000 // drift far beyond its cap, either way
	cases := []struct {
		difficulty, solve, drift, want int64
	}{
		{5000, 1, farAhead, 5303},                                     // step +10600/180000
		{1000000, 495, farBehind, 600496},                             // step -91800/180000
		{HeartbeatMaxDifficulty, 495, farBehind, 21635133319812410},   // the same, from the ceiling
		{HeartbeatMaxDifficulty, 1, farAhead, HeartbeatMaxDifficulty}, // a rise from the ceiling stays there
		{5000, 495, farBehind, 5000},                                  // a fall from the floor stays there
		{1000000, HeartbeatTargetSeconds, 0, 1000000},                 // no step at all
		{1000000, HeartbeatTargetSeconds - 1, 0, 1001112},             // one second fast
		{1000000, HeartbeatTargetSeconds + 1, 0, 998890},              // one second slow
	}
	// The result is rounded to the nearest whole number. At the lowest
	// difficulty a small step up is kept (it used to be cut away) and a
	// small step down is not enough to move it.
	cases = append(cases, []struct {
		difficulty, solve, drift, want int64
	}{
		{5000, HeartbeatTargetSeconds, -30, 5001}, // step +30/180000: 5000.83
		{5000, HeartbeatTargetSeconds, 1, 5000},   // step -1/180000: 4999.97
	}...)
	for _, c := range cases {
		if got := heartbeatRetargetAnchored(c.difficulty, c.solve, 10, c.drift, 5000); got != c.want {
			t.Errorf("difficulty %d, parent took %d s, drift %d: got %d, want %d", c.difficulty, c.solve, c.drift, got, c.want)
		}
	}
}

// Round 5, finding 1: a block may be dated two minutes ahead of the clock,
// and a block dated six minutes after its parent is mined at half
// difficulty, so a miner who dates ahead reaches the halving two minutes
// before one who does not. Every miner now dates ahead for that purpose
// alone, and only as far as a miner will go.
func TestMinersClaimALateBlockHalvingAsEarlyAsAllowed(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	tip := storePlainBlocks(t, 70, 10000000)
	reach := HeartbeatMaxFutureDriftSeconds - minerClockMargin // how far ahead a miner will date

	for _, c := range []struct {
		name         string
		sinceTip     int64 // seconds by the clock since the tip's date
		wantAfterTip int64 // the date chosen, as seconds after the tip's date
	}{
		{"a minute after the tip", 60, 60},
		{"just too early to reach six minutes", HeartbeatStallAfterSeconds - reach - 1, HeartbeatStallAfterSeconds - reach - 1},
		{"the first moment six minutes can be reached", HeartbeatStallAfterSeconds - reach, HeartbeatStallAfterSeconds},
		{"between the first halving and the second", HeartbeatStallAfterSeconds + 20, HeartbeatStallAfterSeconds + 20},
		{"the first moment the second halving can be reached", HeartbeatStallAfterSeconds + HeartbeatStallStepSeconds - reach, HeartbeatStallAfterSeconds + HeartbeatStallStepSeconds},
		{"the tip is dated ahead of the clock", -30, 1},
	} {
		now := tip.Timestamp + c.sinceTip
		got := minerBlockDate(tip, now)
		if got != tip.Timestamp+c.wantAfterTip {
			t.Errorf("%s: block dated %d s after the tip, want %d", c.name, got-tip.Timestamp, c.wantAfterTip)
		}
		if got > now+reach {
			t.Errorf("%s: block dated %d s ahead of the clock, further than a miner goes (%d)", c.name, got-now, reach)
		}
		if mustWaitForClock(got, now) && c.sinceTip >= 0 {
			t.Errorf("%s: the miner would have to wait to use the date it chose", c.name)
		}
	}

	// In the first blocks of a chain no relief is given, so no date is
	// moved ahead: it would gain nothing.
	early := GetBlockByHeightFromDB(5)
	if got := minerBlockDate(early, early.Timestamp+300); got != early.Timestamp+300 {
		t.Errorf("in the launch window a block was dated %d s after its parent, want the true 300", got-early.Timestamp)
	}
}

// Round 5, finding 2 (as reported): the walk back through late blocks did
// not stop at a change of mining algorithm, where difficulty restarts. The
// answer was right, since the forward pass restarts it too; but the walk
// went on into the old algorithm's blocks for nothing. It now stops there.
func TestALateFirstBlockOfANewAlgorithm(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet", func() {
		Params.PowSchedule = []PowEra{
			{FromHeight: 1, Algorithm: PowSHA256, MinDifficulty: 5000},
			{FromHeight: 80, Algorithm: PowRandomX, MinDifficulty: 5000, StartDifficulty: 400000},
		}
	})
	if powEraStartingAt(80) == nil {
		t.Fatal("the schedule of this test has no change of algorithm at block 80")
	}
	minimum := minDifficultyAt(100)
	// The old algorithm's last blocks are late ones, at a very different difficulty.
	previous := storePlainBlocks(t, 77, 90000000)
	for _, gap := range []int64{900, 900} {
		date := previous.Timestamp + gap
		previous = storePlainBlock(t, previous, date, CalculateNextDifficulty(previous, date))
	}
	if previous.Index != 79 {
		t.Fatalf("set-up ended at block %d", previous.Index)
	}

	// Block 80 starts the new algorithm, and is itself late.
	date80 := previous.Timestamp + 600
	difficulty80 := CalculateNextDifficulty(previous, date80)
	if want := heartbeatRetargetAnchored(400000, HeartbeatTargetSeconds, 600, 0, minimum); int64(difficulty80) != want {
		t.Fatalf("the first block of the new algorithm, 600 s late, is at %d, want %d", difficulty80, want)
	}
	block80 := storePlainBlock(t, previous, date80, difficulty80)

	// Block 81 starts from what block 80 would have carried without relief:
	// the new algorithm's starting difficulty, and nothing from before.
	resetUnrelievedCache()
	got := CalculateNextDifficulty(block80, date80+45)
	if want := heartbeatRetargetAnchored(400000, 600, 45, scheduleDriftSeconds(block80), minimum); int64(got) != want {
		t.Errorf("the block after it is at %d, want %d", got, want)
	}
	// Nothing was worked out, or remembered, for the old algorithm's blocks.
	unrelievedCache.Lock()
	_, rememberedOld := unrelievedCache.byHash[previous.Hash]
	unrelievedCache.Unlock()
	if rememberedOld {
		t.Error("the walk went on into the old algorithm's blocks")
	}
}
