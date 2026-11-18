package node

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The miner window says what the node is doing. Seen on the development
// network (18 November 2026): a miner started after half an hour's stop
// showed "Mining at 0 H/s" and a stream of the fast miner's errors for two
// minutes, with nothing to say it was only waiting.
func TestTheMinerWindowSaysWhatTheNodeIsDoing(t *testing.T) {
	restore := func(sync, hold int32, heard int64) {
		atomic.StoreInt32(&IsSyncingActive, sync)
		atomic.StoreInt32(&MiningHoldActive, hold)
		atomic.StoreInt64(&networkHeightHeard, heard)
	}
	defer restore(atomic.LoadInt32(&IsSyncingActive), atomic.LoadInt32(&MiningHoldActive), atomic.LoadInt64(&networkHeightHeard))

	restore(0, 0, 0)
	if text, working := minerStatus(100); !working || text != "UP TO DATE" {
		t.Errorf("a node that is mining says %q (working: %v)", text, working)
	}

	// Catching up, with the network's height known: progress, with a bar.
	restore(1, 0, 400)
	text, working := minerStatus(100)
	if working || !strings.Contains(text, "block #100 of #400") || !strings.Contains(text, "25%") ||
		!strings.Contains(text, "[#####...............]") {
		t.Errorf("a node a quarter of the way there says %q (working: %v)", text, working)
	}

	// Catching up, height not known.
	restore(1, 0, 0)
	if text, working := minerStatus(100); working || !strings.Contains(text, "CATCHING UP") {
		t.Errorf("a node catching up says %q (working: %v)", text, working)
	}

	// Held back with nobody ahead: the reason is shown.
	restore(0, 0, 100)
	setMiningHold(true, "no other node could be reached to confirm this node is up to date")
	if text, working := minerStatus(100); working || !strings.Contains(text, "WAITING: no other node could be reached") {
		t.Errorf("a node waiting for its peers says %q (working: %v)", text, working)
	}
	// Held back behind the network: progress again.
	atomic.StoreInt64(&networkHeightHeard, 200)
	if text, _ := minerStatus(100); !strings.Contains(text, "block #100 of #200") || !strings.Contains(text, "50%") {
		t.Errorf("a node held back behind the network says %q", text)
	}
	setMiningHold(false, "")
	if text, working := minerStatus(200); !working || text != "UP TO DATE" {
		t.Errorf("after the hold the node says %q (working: %v)", text, working)
	}
}

func TestProgressBar(t *testing.T) {
	cases := []struct {
		done, total int64
		want        string
	}{
		{0, 10, "[..........]"},
		{5, 10, "[#####.....]"},
		{10, 10, "[##########]"},
		{15, 10, "[##########]"}, // never longer than its width
		{3, 0, "[..........]"},   // nothing known
		{-1, 10, "[..........]"},
	}
	for _, c := range cases {
		if got := progressBar(c.done, c.total, 10); got != c.want {
			t.Errorf("%d of %d draws %s, want %s", c.done, c.total, got, c.want)
		}
	}
}

// While the node is not handing out work the fast miner asks again every few
// seconds, and each refusal used to put two errors on screen.
func TestTheFastMinersRetriesAreNotShown(t *testing.T) {
	var waiting fastMinerWaiting
	start := time.Unix(1_800_000_000, 0)
	refusal := `[2026-11-18 19:53:57.101]  net      127.0.0.1:28083 error: "the node is not ready to hand out work yet", code: -1`
	closed := `[2026-11-18 19:53:57.102]  net      127.0.0.1:28083 read error: "end of file"`

	// A closed connection with no refusal before it is a real fault and is shown.
	if waiting.hides(closed, start) {
		t.Error("a lost connection was hidden although the node had refused nothing")
	}
	if !waiting.hides(refusal, start.Add(time.Second)) {
		t.Error("the node's own \"not ready\" answer was shown")
	}
	if !waiting.hides(closed, start.Add(2*time.Second)) {
		t.Error("the closed connection that follows a refusal was shown")
	}
	// Long after the last refusal, a lost connection is shown again.
	if waiting.hides(closed, start.Add(time.Minute)) {
		t.Error("a lost connection a minute after the last refusal was hidden")
	}
	if waiting.hides(`[2026-11-18 19:56:01.000]  cpu      READY threads 32/32 (32) huge pages 100% 32/32`, start) {
		t.Error("an ordinary line was hidden")
	}
}
