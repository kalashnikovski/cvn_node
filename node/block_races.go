package node

import (
	"fmt"
	"sync"
	"time"
)

// BLOCKS THAT LOSE A RACE
//
// Now and then two miners find a block at the same height within a moment of
// each other. Every node keeps one of the two by the same rule (more work,
// then the lower identifier) and the other is dropped; its miner is paid
// nothing for it. How often that happens says how well blocks travel: the
// longer a block takes to reach the other miners, the more of their work is
// spent on a block that is already beaten.
//
// A node counts the heights at which it has seen it happen, so that the
// figure can be read off a running network instead of guessed:
//
//   - under about 3 in 100 blocks is ordinary for 45-second blocks;
//   - more than that for long is a reason to make blocks travel faster, and
//     far more is the case for paying the loser a share (an "uncle" share),
//     which this network does not do.
//
// The count is of what this node heard. A losing block is not passed on by
// the nodes that dropped it, so a node hears of a race only from the two
// miners themselves; the seeds, which every miner sends to, hear the most.
// It starts from zero each time the node starts, and it changes nothing
// about which block is kept.

var blockRaces = struct {
	sync.Mutex
	heights     map[int64]bool
	count       int64
	since       time.Time
	startHeight int64
	started     bool
}{heights: make(map[int64]bool)}

// raceHeightsRemembered bounds the memory used to tell a new race from a
// repeat of one already counted.
const raceHeightsRemembered = 4096

// beginCountingRaces marks where the count starts: the node's height once it
// has caught up with the network.
func beginCountingRaces(height int64) {
	blockRaces.Lock()
	defer blockRaces.Unlock()
	blockRaces.started, blockRaces.startHeight, blockRaces.since = true, height, time.Now()
	blockRaces.count, blockRaces.heights = 0, make(map[int64]bool)
}

// noteBlockRace records that two valid blocks stood at `height` on the same
// parent, and says so once. tipHeight is this node's height now.
func noteBlockRace(height, tipHeight int64, kept, lost string) {
	blockRaces.Lock()
	if !blockRaces.started {
		blockRaces.started, blockRaces.startHeight, blockRaces.since = true, tipHeight, time.Now()
	}
	if blockRaces.heights[height] {
		blockRaces.Unlock()
		return
	}
	if len(blockRaces.heights) >= raceHeightsRemembered {
		blockRaces.heights = make(map[int64]bool)
	}
	blockRaces.heights[height] = true
	blockRaces.count++
	races, blocks := blockRaces.count, tipHeight-blockRaces.startHeight
	blockRaces.Unlock()
	if blocks < races {
		blocks = races
	}
	fmt.Printf("⚔️ [RACE] Height %d: two blocks competed. %.16s was kept and %.16s dropped. That is %d of the %d blocks since this node started (%s).\n",
		height, kept, lost, races, blocks, racePercent(races, blocks))
}

func racePercent(races, blocks int64) string {
	if blocks <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", float64(races)*100/float64(blocks))
}

// blockRaceCount is how many heights saw a race, out of how many blocks, and
// since when.
func blockRaceCount(tipHeight int64) (races, blocks int64, since time.Time) {
	blockRaces.Lock()
	defer blockRaces.Unlock()
	if !blockRaces.started {
		return 0, 0, time.Time{}
	}
	blocks = tipHeight - blockRaces.startHeight
	if blocks < blockRaces.count {
		blocks = blockRaces.count
	}
	return blockRaces.count, blocks, blockRaces.since
}
