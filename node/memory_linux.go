//go:build linux

package node

import (
	"os"
	"strconv"
	"strings"
)

// availableMemoryBytes reports how much memory programs can still use
// without the system resorting to its disk, as the system itself estimates.
func availableMemoryBytes() (uint64, bool) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, false
		}
		kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}
		return kilobytes * 1024, true
	}
	return 0, false
}
