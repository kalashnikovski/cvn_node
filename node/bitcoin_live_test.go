package node

import (
	"os"
	"testing"
	"time"
)

// These reach the real Bitcoin network and real block explorers, so
// they run only when asked for:
//
//	CVN_LIVE_BITCOIN=1 go test -run LiveBitcoin -v ./node/
//
// Everything else in the tests uses stand-ins on this machine.

func liveBitcoin(t *testing.T) {
	t.Helper()
	if os.Getenv("CVN_LIVE_BITCOIN") == "" {
		t.Skip("set CVN_LIVE_BITCOIN=1 to reach the real Bitcoin network")
	}
}

// Block 976,283 is the Bitcoin block the development network was started
// from on 18 November 2026, when its header was taken from two explorers.
// Give that header (the contents of genesis_anchor_devnet.txt) in
// CVN_LIVE_KNOWN_HEADER and the test compares it with the node's own count.
const liveKnownHeight = 976283

func TestLiveBitcoinHeadersFromTheNetwork(t *testing.T) {
	liveBitcoin(t)
	follower := newBitcoinFollower(bitcoinStart.Height + 1)
	defer follower.done()
	tip := follower.advance()
	t.Logf("followed Bitcoin's headers from block %d to block %d, from %q, over %d connections", bitcoinStart.Height, tip, follower.source(), len(follower.peers))
	if follower.source() != "the Bitcoin network" || tip < bitcoinStart.Height+900 {
		t.Fatalf("did not get far on the Bitcoin network: block %d from %q", tip, follower.source())
	}
	header, ok := follower.chain.headerAt(liveKnownHeight)
	if !ok {
		t.Fatalf("block %d was not reached", liveKnownHeight)
	}
	hash, _, err := parseBitcoinHeader(header)
	t.Logf("block %d by this node's own count: %s (%v)", liveKnownHeight, hash, err)
	if saved := os.Getenv("CVN_LIVE_KNOWN_HEADER"); saved != "" && saved != header {
		t.Errorf("the header counted as block %d is not the one given in CVN_LIVE_KNOWN_HEADER", liveKnownHeight)
	}
}

// Started one period earlier, the node has to work out a real change of
// Bitcoin's target for itself, at block 975,744, and arrive at the very
// header fixed in this software.
func TestLiveBitcoinAcrossARealChangeOfTarget(t *testing.T) {
	liveBitcoin(t)
	saved := bitcoinStart
	t.Cleanup(func() { bitcoinStart = saved })
	bitcoinStart = bitcoinStartingBlock{
		Height: 973728,
		Header: "00e06620044f3b4ed568ef68218ced65c5f370ee81736922b732010000000000000000005c99018695acc6a1822dd070138fe75e77d0b3e17f0e3d3d5e595faeeb5ed791329be36aace501170839ffa9",
	}
	follower := newBitcoinFollower(saved.Height)
	defer follower.done()
	tip := follower.advance()
	t.Logf("followed from block %d to block %d, from %q", bitcoinStart.Height, tip, follower.source())
	header, ok := follower.chain.headerAt(saved.Height)
	if !ok {
		t.Fatalf("block %d was not reached: Bitcoin's change of target there was not followed", saved.Height)
	}
	if header != saved.Header {
		t.Fatalf("counting from block %d gives another header at block %d than the one fixed in this software", bitcoinStart.Height, saved.Height)
	}
	before, _ := follower.chain.headerAt(saved.Height - 1)
	t.Logf("target before the change: ...%s  after: ...%s", before[144:152], header[144:152])
}

// With the Bitcoin network out of reach, block explorers supply the headers,
// and they go through the same checks.
func TestLiveBitcoinHeadersFromExplorersAlone(t *testing.T) {
	liveBitcoin(t)
	saved := findBitcoinPeers
	t.Cleanup(func() { findBitcoinPeers = saved })
	findBitcoinPeers = func() []string { return nil }
	follower := newBitcoinFollower(bitcoinStart.Height + 1)
	tip := follower.advance()
	t.Logf("followed from block %d to block %d, from %q", bitcoinStart.Height, tip, follower.source())
	if follower.source() != "block explorers" || tip < bitcoinStart.Height+100 {
		t.Fatalf("explorers alone did not get far: block %d", tip)
	}
}

// The waiting mode itself, against real Bitcoin: a node whose network starts
// from a block that already exists counts up to it and starts at once.
func TestLiveBitcoinNodeStartsFromARealBlock(t *testing.T) {
	liveBitcoin(t)
	t.Setenv("CVN_DEVNET_ANCHOR_HEIGHT", "")
	onNetwork(t, "devnet")
	Params.Name = "launch-test"
	Params.Anchor = GenesisAnchor{BitcoinHeight: liveKnownHeight}
	Params.GenesisTimestamp = 0
	t.Chdir(t.TempDir())
	anchorFollowerLock.Lock()
	anchorFollower = nil
	anchorFollowerLock.Unlock()

	started := make(chan struct{})
	go func() {
		waitForGenesisAnchor([]string{"cvn_node"}, 10*time.Second, time.Minute)
		close(started)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Minute):
		t.Fatal("the node did not start from a Bitcoin block that exists")
	}
	t.Logf("started from Bitcoin block %d: %s", liveKnownHeight, Params.Anchor.BitcoinHash)
	if known := os.Getenv("CVN_LIVE_KNOWN_HEADER"); known != "" && known != Params.Anchor.BitcoinHeader {
		t.Errorf("the node started from another header than the one given in CVN_LIVE_KNOWN_HEADER")
	}
	if settled, warning := anchorWarning(); !settled || warning != "" {
		t.Errorf("a block hundreds deep should count as settled: settled %v, warning %q", settled, warning)
	}
}
