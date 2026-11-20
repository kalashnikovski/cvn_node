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
// Which of the two is the network is not for anyone to choose. The rule is
// fixed in advance: THE NETWORK IS THE ONE WHOSE FIRST BLOCK COMES FROM THE
// BITCOIN BLOCK THAT IS STILL IN BITCOIN'S CHAIN SIX BLOCKS LATER. Blocks
// mined on the other are void. Bitcoin decides, not the seeds and not the
// people who run them.
//
// A node therefore does two things from time to time:
//
//   - Until the anchor is six Bitcoin blocks deep, it goes on following
//     Bitcoin's headers (bitcoin_headers.go) and looks whether the block it
//     started from is still the one at that height. If Bitcoin has replaced
//     it, the node says so and says how to move over. This does not depend
//     on the seeds. (A rehearsal from an old Bitcoin block, whose height
//     cannot be counted up to, asks the block explorers.)
//   - It compares its first block with the seeds' and says so, plainly and
//     repeatedly, if they differ.
//
// It does not stop mining or change anything by itself. A warning can be
// wrong (a seed can be, and for a moment Bitcoin itself can hold two blocks
// at one height), and deleting a chain is for the operator to do.

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
		"The network is the one that begins from the Bitcoin block still in Bitcoin's chain: run with --show-genesis to see which Bitcoin block this node started from, and compare it with a Bitcoin block explorer before mining further.",
		different, same+different, ours, theirs)
}

// anchorSettledDepth is how many Bitcoin blocks must follow the anchor
// before it counts as settled.
const anchorSettledDepth = 6

// anchorWarning looks whether the block this network started from is still
// the one at its height in Bitcoin's chain. settled is true once it is and
// it is anchorSettledDepth blocks deep: there is then nothing more to watch.
// warning is the line to show if Bitcoin now has another block there.
func anchorWarning() (settled bool, warning string) {
	anchor := Params.Anchor
	if anchor.BitcoinHeight <= 0 || anchor.BitcoinHeader == "" {
		return true, ""
	}
	var (
		header string
		tip    int64
		known  bool
	)
	if follower := followerForAnchor(); follower != nil {
		tip = follower.advance()
		follower.done()
		header, known = follower.chain.headerAt(anchor.BitcoinHeight)
	} else {
		header, tip, known = lookUpAnchor(anchor.BitcoinHeight)
	}
	if !known {
		return false, ""
	}
	if header == anchor.BitcoinHeader {
		return tip >= anchor.BitcoinHeight+anchorSettledDepth, ""
	}
	hash, _, err := parseBitcoinHeader(header)
	if err != nil {
		return false, ""
	}
	return false, fmt.Sprintf("🚨 [FIRST BLOCK] Bitcoin has replaced block %d. This node started from %s; Bitcoin's chain now has %s there. "+
		"By the rule fixed before the start, the network is the one that begins from the block still in Bitcoin's chain six blocks later, and blocks mined on the other are void. "+
		"Check block %d on a Bitcoin block explorer. If it has changed for good: stop this node, delete %s and %s, and start again.",
		anchor.BitcoinHeight, anchor.BitcoinHash, hash, anchor.BitcoinHeight, genesisAnchorFileName(), Params.DatabaseFile)
}

// runGenesisWatch repeats both checks for as long as the node runs.
func runGenesisWatch(lookUpAnchors bool) {
	anchorSettled := !lookUpAnchors
	for {
		if !anchorSettled {
			settled, warning := anchorWarning()
			anchorSettled = settled
			if warning != "" {
				announceNetworkStatus(warning)
			}
		}
		if warning := genesisWarning(); warning != "" {
			announceNetworkStatus(warning)
		}
		time.Sleep(genesisWatchInterval)
	}
}
