package node

import (
	"strings"
	"testing"
)

// In the miner window's log only the tag is coloured: the time and the rest
// of the line stay as they are, and other kinds of line are untouched.
func TestOnlyTheTagOfALogLineIsColoured(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	core := "[22:15:21] ⛏️  [MINING] Mining at 18.71 kH/s // height #3593"
	if got, want := colourLogTags(core), "[22:15:21] ⛏️  "+ansiPurple+"[MINING]"+ansiReset+" Mining at 18.71 kH/s // height #3593"; got != want {
		t.Errorf("a [MINING] line was written as %q, want %q", got, want)
	}
	success := "[22:15:06] 🎉 [SUCCESS] External miner solved valid block #3593 | Hash: 30b63e03f963a106"
	if got, want := colourLogTags(success), "[22:15:06] 🎉 "+ansiGreen+"[SUCCESS]"+ansiReset+" External miner solved valid block #3593 | Hash: 30b63e03f963a106"; got != want {
		t.Errorf("a [SUCCESS] line was written as %q, want %q", got, want)
	}
	for _, other := range []string{
		"[22:15:00] ⛏️ [FAST MINER] randomx dataset ready (1138 ms)",
		"[22:15:00] ⛏️ [WORK SERVER] External miners can connect to 127.0.0.1:28083.",
		"[22:15:00] 🌱 [SEED] Not mining. Following the network // Ledger Height: #12",
	} {
		if got := colourLogTags(other); got != other {
			t.Errorf("a line of another kind was changed: %q", got)
		}
	}
	// With NO_COLOR set nothing is coloured at all.
	t.Setenv("NO_COLOR", "1")
	if colourLogTags(core) != core || inGreen("x") != "x" || inPurple("x") != "x" {
		t.Error("colour codes were written although NO_COLOR is set")
	}
	if strings.Contains(colourLogTags(success), "\x1b") {
		t.Error("colour codes were written although NO_COLOR is set")
	}
}
