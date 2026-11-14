package node

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Tests for what the fair-launch review found. See
// audit/FINDINGS_FAIR_LAUNCH.md.

// withReorgLimit gives the test chain a small reorganisation limit, so that
// a test can go past it with a handful of blocks.
func withReorgLimit(limit int64) func() {
	return func() {
		economics := *Params.Economics
		economics.ReorgLimitBlocks = limit
		Params.Economics = &economics
	}
}

// Finding: a seed could hand a newcomer a chain of its own making, and the
// rule against replacing many of one's own blocks then kept the newcomer off
// the real chain for good. Blocks downloaded at start-up came from one node
// and are not protected by that rule while the program runs.
func TestANewcomerIsNotLockedOntoASeedsChain(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet", withReorgLimit(3))
	clearSyncMemory(t)

	// Two chains that share their first four blocks. The seed's is shorter.
	buildTestChain(t, 4)
	timestamp := GetLatestBlockInternal().Timestamp
	for i := 0; i < 6; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, testAddress("the seed's own miner"), nil)
	}
	seedChain := localChain()
	StateMutex.Lock()
	rollbackChainToHeightLocked(4)
	StateMutex.Unlock()
	timestamp = GetLatestBlockInternal().Timestamp
	for i := 0; i < 12; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, testAddress("everyone else"), nil)
	}
	realChain := localChain()
	if sumBlockWork(realChain).Cmp(sumBlockWork(seedChain)) <= 0 {
		t.Fatal("the test's real chain should hold more work than the seed's")
	}

	// A newcomer with nothing, who knows only the seed.
	StateMutex.Lock()
	rollbackChainToHeightLocked(0)
	StateMutex.Unlock()
	newStandInPeer(t, seedChain).asOnlySeed(t)
	runInitialDownload([]string{"cvn_node"})
	if tip := GetLatestBlockInternal(); tip.Hash != seedChain[len(seedChain)-1].Hash {
		t.Fatalf("the newcomer should first have what the seed gave it; it is at #%d", tip.Index)
	}

	// It then meets a node on the real chain, which parts from the seed's six
	// blocks back: twice the limit.
	real := newStandInPeer(t, realChain)
	real.alsoServeOn(t, "127.0.0.2", Params.SyncPort)
	PeerMapMutex.Lock()
	savedPeers := ActivePeerMap
	ActivePeerMap = map[string]time.Time{Params.gossipAddress("127.0.0.2"): time.Now()}
	PeerMapMutex.Unlock()
	t.Cleanup(func() {
		PeerMapMutex.Lock()
		ActivePeerMap = savedPeers
		PeerMapMutex.Unlock()
	})
	TriggerActiveReSyncPass()
	if tip := GetLatestBlockInternal(); tip.Hash != realChain[len(realChain)-1].Hash {
		t.Fatalf("the newcomer stayed on the seed's chain at #%d although another node proved more work", tip.Index)
	}
}

// The limit still protects blocks that were in the database when the program
// started and blocks that arrived afterwards.
func TestTheReorgLimitStillCountsBlocksThatWereNotJustDownloaded(t *testing.T) {
	startTestChain(t, "devnet", withReorgLimit(100))

	// Nothing downloaded: every block counts.
	if depth := countedReorgDepth(500, 380); depth != 120 {
		t.Errorf("with nothing downloaded, 120 blocks counted as %d", depth)
	}
	if lowest := lowestReplaceableAncestor(500); lowest != 400 {
		t.Errorf("with nothing downloaded the furthest back is #400, got #%d", lowest)
	}
	// A node that had 200 blocks, downloaded up to 450, and has since
	// received 50 more.
	noteInitialDownload(200, 450)
	t.Cleanup(func() { noteInitialDownload(0, 0) })
	for _, check := range []struct{ ancestor, want int64 }{
		{480, 20},  // only blocks received since
		{450, 50},  // all of those
		{300, 50},  // into the downloaded blocks: they do not count
		{200, 50},  // all of the downloaded blocks
		{150, 100}, // 50 since, and 50 it already had
		{149, 101},
	} {
		if got := countedReorgDepth(500, check.ancestor); got != check.want {
			t.Errorf("parting at #%d counts %d blocks, want %d", check.ancestor, got, check.want)
		}
	}
	if lowest := lowestReplaceableAncestor(500); lowest != 150 {
		t.Errorf("the furthest back is #150, got #%d", lowest)
	}
	// Once more than the limit has arrived since, the downloaded blocks are
	// out of reach again.
	if lowest := lowestReplaceableAncestor(600); lowest != 500 {
		t.Errorf("150 blocks after the download the furthest back is #500, got #%d", lowest)
	}
}

// Finding: seeds could keep miners waiting for ten minutes by claiming to be
// ahead, and for five by staying silent, and in a slow start that favoured
// whoever the seeds answered.
func TestNobodyCanHoldMinersBackForLong(t *testing.T) {
	onNetwork(t, "devnet")
	stale := int64(miningStaleTipSeconds) + 1

	// In the first blocks of a chain there is no hold at all: difficulty
	// does not fall there, so there is nothing to guard against.
	for _, height := range []int64{0, 1, HeartbeatLaunchBlocks - 1} {
		if hold, _, _ := decideMiningHold(height, stale, height+50, true, 0); hold {
			t.Errorf("at block #%d a node claiming to be ahead held the miner back", height)
		}
		if hold, _, _ := decideMiningHold(height, stale, 0, false, 0); hold {
			t.Errorf("at block #%d silence held the miner back", height)
		}
	}
	// A claim to be ahead still starts a catch-up, which is what proves it.
	if _, resync, _ := decideMiningHold(3, stale, 9, true, 0); !resync {
		t.Error("a node claiming to be ahead in the first blocks did not start a catch-up")
	}

	// Later, a hold lasts two minutes at the most without progress.
	later := int64(HeartbeatLaunchBlocks + 100)
	if hold, _, _ := decideMiningHold(later, stale, later+50, true, 119); !hold {
		t.Error("a node behind the network should hold while it catches up")
	}
	if hold, _, _ := decideMiningHold(later, stale, later+50, true, 120); hold {
		t.Error("a claim that was never made good still held the miner after two minutes")
	}
	if hold, _, _ := decideMiningHold(later, stale, 0, false, 120); hold {
		t.Error("silence still held the miner after two minutes")
	}
}

// Finding: if two groups of nodes start from different first blocks, nobody
// was told in a way they would see.
func TestANodeSaysWhenSeedsHoldAnotherFirstBlock(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	clearSyncMemory(t)
	ours := buildTestChain(t, 3)

	seed := newStandInPeer(t, ours)
	seed.asOnlySeed(t)
	if warning := genesisWarning(); warning != "" {
		t.Errorf("a seed with the same first block raised a warning: %s", warning)
	}
	// The same seed, now on a chain that began from another first block.
	other := append([]Block{}, ours...)
	other[0].Hash = strings.Repeat("ab", 32)
	seed.blocks = other
	warning := genesisWarning()
	if !strings.Contains(warning, ours[0].Hash) || !strings.Contains(warning, other[0].Hash) {
		t.Errorf("the warning should name both first blocks: %q", warning)
	}
	// A seed that does not answer says nothing either way.
	seed.silentAboutBlocks = true
	if warning := genesisWarning(); warning != "" {
		t.Errorf("a silent seed raised a warning: %s", warning)
	}
}

// Finding: "--network mainnet" quietly chose the first test network. There
// is no mainnet yet, and the program says so.
func TestThereIsNoNetworkCalledMainnet(t *testing.T) {
	saved := Params
	t.Cleanup(func() { Params = saved; applyParams() })
	if err := SelectNetwork("mainnet"); err == nil {
		t.Errorf("\"mainnet\" was accepted and chose %s", Params.Name)
	}
}

// Finding: an older-style address typed on the command line was completed
// with a valid check, so a typing mistake in it could no longer be caught.
func TestATypedMiningAddressIsNeverCompleted(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CVN_MINER_ADDRESS", "")
	onNetwork(t, "devnet")
	_, _, full, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	older := full[:len("CVN_")+40]
	if _, err := resolveMinerAddress([]string{"cvn_node", "--miner-address", older}); err == nil || !strings.Contains(err.Error(), "check characters") {
		t.Errorf("an older-style address typed on the command line was accepted: %v", err)
	}
	t.Setenv("CVN_MINER_ADDRESS", older)
	if _, err := resolveMinerAddress([]string{"cvn_node"}); err == nil {
		t.Error("an older-style address given in the environment was accepted")
	}
	t.Setenv("CVN_MINER_ADDRESS", "")
	// One the program saved itself is completed, as before.
	if err := writeMinerConfigForTest(older); err != nil {
		t.Fatal(err)
	}
	if address, err := resolveMinerAddress([]string{"cvn_node"}); err != nil || address != full {
		t.Errorf("an address the program saved was not completed: %q %v", address, err)
	}
}

// writeMinerConfigForTest saves a mining address the way the program does.
func writeMinerConfigForTest(address string) error {
	return os.WriteFile("miner_config.json", []byte(`{"miner_address": "`+address+`"}`), 0644)
}
