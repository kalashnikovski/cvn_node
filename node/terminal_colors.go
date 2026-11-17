package node

import (
	"os"
	"strings"
)

// Colours of the miner window. They are used only where the dashboard is
// drawn on a person's screen; lines that go to a log file are never
// coloured. Setting the NO_COLOR environment variable (to anything) turns
// them off, for terminals that show the codes as text.
const (
	ansiGreen  = "\x1b[92m"
	ansiPurple = "\x1b[38;5;141m"
	ansiReset  = "\x1b[0m"
)

func terminalColoursOn() bool {
	return os.Getenv("NO_COLOR") == ""
}

func inGreen(text string) string {
	if !terminalColoursOn() {
		return text
	}
	return ansiGreen + text + ansiReset
}

func inPurple(text string) string {
	if !terminalColoursOn() {
		return text
	}
	return ansiPurple + text + ansiReset
}

// colourLogTags colours the tag of a line of the miner window's log, and
// only the tag: "[MINING]" purple, "[SUCCESS]" green. The time, the rest
// of the line and every other kind of line are left as they are.
func colourLogTags(line string) string {
	if !terminalColoursOn() {
		return line
	}
	line = strings.Replace(line, "[MINING]", inPurple("[MINING]"), 1)
	line = strings.Replace(line, "[SUCCESS]", inGreen("[SUCCESS]"), 1)
	return line
}
