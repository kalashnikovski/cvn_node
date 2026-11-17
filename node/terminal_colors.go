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

// dashboardHeadingLines is how many lines the dashboard's heading takes:
// three for the title, three of figures, one rule, the "recent activity"
// line and the rule under it.
const dashboardHeadingLines = 9

// dashboardLogLines is how many log lines the dashboard shows under its
// heading in a terminal of the given height: all that fit, leaving one line
// for the cursor. When the height is not known it shows twelve, and it
// never shows fewer than five.
func dashboardLogLines(terminalHeight int) int {
	if terminalHeight <= 0 {
		return 12
	}
	lines := terminalHeight - dashboardHeadingLines - 1
	if lines < 5 {
		return 5
	}
	return lines
}

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
