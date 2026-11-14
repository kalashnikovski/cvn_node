package node

import (
	"fmt"
	"time"
)

// WATCHING FOR A SECOND FIRST BLOCK
//
// Every node on a network must start from the same first block. On a launch
// network the first block is made from a Bitcoin block, and once in a few
// hundred times Bitcoin replaces a block moments after it appears. If that
// happened to the anchor, some nodes would have started from one header and
// some from the other: two networks with the same name, each unable to hear
// the other, each looking healthy to the people on it.
//
// A node therefore compares its first block with the seeds' from time to
// time and says so, plainly and repeatedly, if they differ. It does not stop
// mining or change anything by itself: a seed can be wrong, and which first
// block is the network's is for people to settle, not for one node to guess.

const genesisWatchInterval = 5 * time.Minute

// compareGenesisWithSeeds asks every seed for its first block's identifier.
// same and different count the seeds that answered either way.
func compareGenesisWithSeeds() (same, different int, theirs string) {
	ours := GetBlockByHeightFromDB(0).Hash
	if ours == "" {
		return 0, 0, ""
	}
	for _, seed := range reachableSeedSyncURLs() {
		hash, ok := fetchBlockHash(seed, 0)
		switch {
		case !ok || !isLowerHex(hash, 64):
			// No answer, or not an identifier: this says nothing.
		case hash == ours:
			same++
		default:
			different++
			theirs = hash
		}
	}
	return same, different, theirs
}

// genesisWarning is the line shown when seeds hold another first block, or
// empty when none does.
func genesisWarning() string {
	same, different, theirs := compareGenesisWithSeeds()
	if different == 0 {
		return ""
	}
	ours := GetBlockByHeightFromDB(0).Hash
	return fmt.Sprintf("🚨 [FIRST BLOCK] %d of the %d seeds that answered started from a different first block than this node. "+
		"This node: %s. Theirs: %s. This node and they are on separate networks and will never share a block. "+
		"Check the project page for which is right before mining further; run with --show-genesis to see what this node started from.",
		different, same+different, ours, theirs)
}

// runGenesisWatch repeats the comparison for as long as the node runs.
func runGenesisWatch() {
	for {
		if warning := genesisWarning(); warning != "" {
			announceNetworkStatus(warning)
		}
		time.Sleep(genesisWatchInterval)
	}
}
