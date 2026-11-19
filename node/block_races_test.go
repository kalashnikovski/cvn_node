package node

import "testing"

func TestRacesAreCountedOncePerHeight(t *testing.T) {
	beginCountingRaces(100)
	if races, blocks, _ := blockRaceCount(100); races != 0 || blocks != 0 {
		t.Fatalf("a fresh count reads %d of %d", races, blocks)
	}
	// The same height heard of twice (both miners send their block) is one race.
	noteBlockRace(105, 105, "aaaa", "bbbb")
	noteBlockRace(105, 105, "bbbb", "aaaa")
	noteBlockRace(108, 108, "cccc", "dddd")
	races, blocks, since := blockRaceCount(120)
	if races != 2 || blocks != 20 || since.IsZero() {
		t.Errorf("counted %d races over %d blocks; want 2 over 20", races, blocks)
	}
	if got := racePercent(races, blocks); got != "10.0%" {
		t.Errorf("2 in 20 is shown as %s", got)
	}
	if got := racePercent(0, 0); got != "0%" {
		t.Errorf("no blocks at all is shown as %s", got)
	}
	// Starting again starts from nothing.
	beginCountingRaces(120)
	if races, _, _ := blockRaceCount(125); races != 0 {
		t.Errorf("a new start kept %d old races", races)
	}
}
