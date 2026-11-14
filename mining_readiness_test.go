package main

import "testing"

func TestMiningHoldDecision(t *testing.T) {
	stale := miningStaleTipSeconds
	cases := []struct {
		name                          string
		tipHeight, tipAge, peerHeight int64
		peerAnswered                  bool
		heldSeconds                   int64
		wantHold, wantResync          bool
	}{
		{"fresh tip: mine, whatever peers say", 6000, 30, 6100, true, 0, false, false},
		{"hours-old tip and the network is far ahead: wait and catch up", 5134, 40000, 6137, true, 0, true, true},
		{"stale tip and a peer is one block ahead: wait and catch up", 6000, stale + 40, 6001, true, 0, true, true},
		{"whole network stalled at our height: mine", 6000, 900, 6000, true, 0, false, false},
		{"stale tip but peers are behind us: mine", 6000, 900, 5990, true, 0, false, false},
		{"stale tip and nobody answers: wait at first", 6000, 900, 0, false, 10, true, false},
		{"stale tip and nobody answers for the full limit: mine anyway", 6000, 900, 0, false, miningHoldUnreachableLimit, false, false},
		{"peer claims to be ahead but no progress for the full limit: mine, keep trying", 6000, 5000, 999999, true, miningHoldBehindLimit, false, true},
		{"just under the stale threshold: mine", 6000, stale - 1, 6100, true, 0, false, false},
		{"exactly at the stale threshold with a peer ahead: wait", 6000, stale, 6100, true, 0, true, true},
	}
	for _, c := range cases {
		hold, resync, reason := decideMiningHold(c.tipHeight, c.tipAge, c.peerHeight, c.peerAnswered, c.heldSeconds)
		if hold != c.wantHold || resync != c.wantResync {
			t.Errorf("%s: hold=%v resync=%v, want hold=%v resync=%v", c.name, hold, resync, c.wantHold, c.wantResync)
		}
		if hold && reason == "" {
			t.Errorf("%s: a hold must say why", c.name)
		}
	}
}

func TestHeartbeatRetarget(t *testing.T) {
	const difficulty = int64(90000000)
	if got := heartbeatRetarget(difficulty, HeartbeatTargetSeconds, 10); got != difficulty {
		t.Errorf("a parent solved exactly on target changed difficulty to %d", got)
	}
	if got := heartbeatRetarget(difficulty, 1, 10); got <= difficulty || got > difficulty+difficulty/HeartbeatWindow {
		t.Errorf("a fast parent should raise difficulty by at most 1/%d, got %d", HeartbeatWindow, got)
	}
	if got := heartbeatRetarget(difficulty, 100000, 10); got < difficulty/2-difficulty/100 || got >= difficulty {
		t.Errorf("a very slow parent should lower difficulty by about half at most, got %d", got)
	}
	// Stall relief: one halving at the threshold, another per further step.
	onTarget := heartbeatRetarget(difficulty, HeartbeatTargetSeconds, 10)
	if got := heartbeatRetarget(difficulty, HeartbeatTargetSeconds, HeartbeatStallAfterSeconds); got != onTarget/2 {
		t.Errorf("at the stall threshold difficulty should halve, got %d", got)
	}
	if got := heartbeatRetarget(difficulty, HeartbeatTargetSeconds, HeartbeatStallAfterSeconds+HeartbeatStallStepSeconds); got != onTarget/4 {
		t.Errorf("one step past the stall threshold difficulty should quarter, got %d", got)
	}
	if got := heartbeatRetarget(difficulty, HeartbeatTargetSeconds, 100000000); got != HeartbeatMinDifficulty {
		t.Errorf("difficulty fell below the minimum: %d", got)
	}
	if got := heartbeatRetarget(HeartbeatMaxDifficulty, 1, 10); got != HeartbeatMaxDifficulty {
		t.Errorf("difficulty rose above the maximum: %d", got)
	}
}
