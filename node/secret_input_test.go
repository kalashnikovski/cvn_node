package node

import (
	"bufio"
	"strings"
	"testing"
)

// Where there is no terminal to hide the typing on (a pipe, a test), the
// line is still read, and nothing is left switched off.
func TestASecretLineIsReadWithoutATerminal(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("  correct horse battery  \nnext line\n"))
	if got := readSecretLine(reader, ""); got != "correct horse battery" {
		t.Errorf("read %q", got)
	}
	if got := readSecretLine(reader, ""); got != "next line" {
		t.Errorf("the line after it read %q", got)
	}
}
