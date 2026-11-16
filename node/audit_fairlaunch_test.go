package node

import (
	"math/big"
	"os"
	"strings"
	"sync/atomic"
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
	// A node that had 200 blocks, downloaded up to 450, was then confirmed
	// by a node that is not a seed, and has since received 50 more.
	setInitialDownload(200, 450, false)
	t.Cleanup(func() { setInitialDownload(0, 0, false) })
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
	// Nor "testnet": someone who reads "CVN Testnet" and types that must not
	// find themselves on the first test network without a word.
	for _, name := range []string{"mainnet", "testnet"} {
		if err := SelectNetwork(name); err == nil {
			t.Errorf("%q was accepted and chose %s", name, Params.Name)
		}
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
	return saveMinerAddress(address)
}

// Second review: an older-style address typed into miner_config.json was
// completed as if the program had saved it. The guide tells people they may
// edit that file.
func TestAnAddressTypedIntoTheConfigFileIsNotCompleted(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CVN_MINER_ADDRESS", "")
	onNetwork(t, "devnet")
	_, _, full, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	older := full[:len("CVN_")+40]
	// Written by hand: no mark.
	if err := os.WriteFile("miner_config.json", []byte(`{"miner_address": "`+older+`"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveMinerAddress([]string{"cvn_node"}); err == nil {
		t.Error("an older-style address typed into miner_config.json was completed")
	}
	// Saved by the program, then changed by hand: the mark names another
	// address and no longer counts.
	if err := saveMinerAddress(older); err != nil {
		t.Fatal(err)
	}
	changed := older[:len(older)-1] + map[bool]string{true: "0", false: "1"}[older[len(older)-1] != '0']
	raw, _ := os.ReadFile("miner_config.json")
	edited := strings.Replace(string(raw), `"miner_address": "`+older+`"`, `"miner_address": "`+changed+`"`, 1)
	if edited == string(raw) {
		t.Fatal("the test could not edit the saved address")
	}
	if err := os.WriteFile("miner_config.json", []byte(edited), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveMinerAddress([]string{"cvn_node"}); err == nil {
		t.Error("an address changed by hand after the program saved it was completed")
	}
	// A full address typed into the file is fine: its own check vouches for it.
	if err := os.WriteFile("miner_config.json", []byte(`{"miner_address": "`+full+`"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if address, err := resolveMinerAddress([]string{"cvn_node"}); err != nil || address != full {
		t.Errorf("a full address typed into the file was refused: %v", err)
	}
}

// Second review: Bitcoin lets a block carry a time up to two hours ahead of
// the true time. Dated by such a block, the genesis block was in the future,
// every honest miner waited for the clock, and an altered miner mined alone.
func TestAnAnchorDatedAheadIdlesNobody(t *testing.T) {
	onNetwork(t, "devnet")
	asLaunchNetwork()
	t.Chdir(t.TempDir())
	if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", testHeaderA}); err != nil {
		t.Fatal(err)
	}
	// The worst case Bitcoin allows: the anchor's own time is two hours
	// ahead, so the true time is two hours before it.
	trueTime := testHeaderTime - bitcoinClockAllowance
	firstBlockDate := Params.GenesisTimestamp + 1
	if firstBlockDate < trueTime {
		firstBlockDate = trueTime
	}
	if mustWaitForClock(firstBlockDate, trueTime) {
		t.Errorf("with the anchor dated two hours ahead, a miner still has to wait %d seconds before block 1", firstBlockDate-trueTime)
	}
	if Params.GenesisTimestamp > trueTime {
		t.Error("the genesis block is dated ahead of the true time")
	}
}

// Second review: nobody was told if Bitcoin replaced the anchor block, and
// which first block then counts was left to an announcement. The rule is
// Bitcoin's own chain, and a node checks it for itself.
func TestANodeSaysWhenBitcoinReplacesItsAnchor(t *testing.T) {
	onNetwork(t, "devnet")
	asLaunchNetwork()
	t.Chdir(t.TempDir())
	if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", testHeaderA}); err != nil {
		t.Fatal(err)
	}
	same := func() *standInExplorer {
		explorer := newStandInExplorer(t, testHeaderHash, testHeaderA)
		atomic.StoreInt32(&explorer.mined, 1)
		return explorer
	}
	first, second := same(), same()
	useStandInExplorers(t, first, second)
	// Still the same block, but not yet deep: keep watching, say nothing.
	if settled, warning := anchorWarning(); settled || warning != "" {
		t.Errorf("one block deep: settled %v, warning %q", settled, warning)
	}
	atomic.StoreInt32(&first.laterBlocks, anchorSettledDepth)
	atomic.StoreInt32(&second.laterBlocks, anchorSettledDepth)
	if settled, warning := anchorWarning(); !settled || warning != "" {
		t.Errorf("six blocks deep: settled %v, warning %q", settled, warning)
	}
	// Bitcoin now has another block at that height.
	replaced := func() *standInExplorer {
		explorer := newStandInExplorer(t, testHeaderBHash, testHeaderB)
		atomic.StoreInt32(&explorer.mined, 1)
		return explorer
	}
	useStandInExplorers(t, replaced(), replaced())
	settled, warning := anchorWarning()
	if settled || !strings.Contains(warning, testHeaderHash) || !strings.Contains(warning, testHeaderBHash) || !strings.Contains(warning, genesisAnchorFileName()) {
		t.Errorf("after Bitcoin replaced the block: settled %v, warning %q", settled, warning)
	}
}

// Second review: the freedom from the reorganisation limit lasted only while
// the program ran and only for the blocks downloaded at start. A newcomer
// kept on a seed's chain for an hour, or restarted, was locked in again.
func TestANewcomerStaysFreeUntilSomeoneOtherThanASeedAgrees(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet", withReorgLimit(3))
	clearSyncMemory(t)
	chain := buildTestChain(t, 12)
	StateMutex.Lock()
	rollbackChainToHeightLocked(0)
	StateMutex.Unlock()
	newStandInPeer(t, chain[:9]).asOnlySeed(t)

	runInitialDownload([]string{"cvn_node"})
	if GetLatestBlockInternal().Index != 8 {
		t.Fatalf("the newcomer should be at #8, is at #%d", GetLatestBlockInternal().Index)
	}
	// Blocks that arrive afterwards do not start the limit either: nobody
	// but the seed has vouched for any of this yet.
	StateMutex.Lock()
	for _, block := range chain[9:] {
		SaveBlockToStorageInternal(block)
	}
	StateMutex.Unlock()
	if depth := countedReorgDepth(12, 1); depth != 0 {
		t.Errorf("before anyone but a seed agrees, %d blocks count against the limit", depth)
	}
	// Stopped and started again: still free, from the same height.
	setInitialDownload(0, 0, false)
	beginInitialDownload(GetLatestBlockInternal().Index)
	if depth := countedReorgDepth(12, 1); depth != 0 {
		t.Errorf("after a restart, %d blocks count against the limit", depth)
	}
	// A node that is not a seed, holding the same chain, settles it.
	other := newStandInPeer(t, chain)
	other.alsoServeOn(t, "127.0.0.2", Params.SyncPort)
	operatorSyncSources = operatorSourcesFrom("127.0.0.2")
	t.Cleanup(func() { operatorSyncSources = nil })
	trySettleInitialDownload()
	if depth := countedReorgDepth(12, 1); depth != 0 {
		t.Errorf("the blocks held when it was settled should stay uncounted, %d count", depth)
	}
	if depth := countedReorgDepth(20, 1); depth != 8 {
		t.Errorf("blocks received after it was settled should count: 8 expected, %d counted", depth)
	}
	// And that survives a restart: every block now in the database counts.
	setInitialDownload(0, 0, false)
	beginInitialDownload(12)
	if depth := countedReorgDepth(12, 1); depth != 11 {
		t.Errorf("after settling and a restart all 11 blocks should count, %d do", depth)
	}
}

// --connect takes one node or several.
func TestConnectTakesSeveralNodes(t *testing.T) {
	onNetwork(t, "devnet")
	got := operatorSourcesFrom(" 10.0.0.5, http://example.org:9000/ ,,bad host, 10.0.0.6:1234 ")
	want := []string{Params.syncURL("10.0.0.5"), "http://example.org:9000", "http://10.0.0.6:1234"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Third review: a rival branch was checked with RandomX keys read from the
// node's own chain. For a newcomer, which is not held back by the
// reorganisation limit, the two chains can part below a key block; every
// block of the real chain that depends on that key then failed, and the
// newcomer stayed on the seed's chain however much heavier the real one was.
func TestARivalBranchIsCheckedWithItsOwnRandomXKeys(t *testing.T) {
	t.Chdir(t.TempDir())
	timestamp := startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 4}})
	withReorgLimit(2)()
	clearSyncMemory(t)

	// The key changes every 4 blocks and trails by 2. Both chains share
	// blocks 1 to 3. The seed's runs to block 9, the real one to block 15,
	// so each has key blocks (4 and 8) that the other does not.
	extend := func(count int, miner string) {
		for i := 0; i < count; i++ {
			*timestamp += 45
			extendTestChain(t, *timestamp, testAddress(miner), nil)
		}
	}
	extend(3, "shared")
	forkTime := *timestamp
	extend(6, "the seed's own miner")
	seedChain := localChain()
	StateMutex.Lock()
	rollbackChainToHeightLocked(3)
	StateMutex.Unlock()
	*timestamp = forkTime
	extend(12, "everyone else")
	realChain := localChain()
	if sumBlockWork(realChain).Cmp(sumBlockWork(seedChain)) <= 0 {
		t.Fatal("the test's real chain should hold more work than the seed's")
	}
	if seedChain[4].Hash == realChain[4].Hash || powKeyHeight(7) != 4 {
		t.Fatal("the test needs the two chains to differ at a block that is a RandomX key")
	}

	StateMutex.Lock()
	rollbackChainToHeightLocked(0)
	StateMutex.Unlock()
	newStandInPeer(t, seedChain).asOnlySeed(t)
	runInitialDownload([]string{"cvn_node"})
	if tip := GetLatestBlockInternal(); tip.Hash != seedChain[len(seedChain)-1].Hash {
		t.Fatalf("the newcomer should first have what the seed gave it; it is at #%d", tip.Index)
	}

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
		t.Fatalf("the newcomer stayed on the seed's chain at #%d: the real chain's blocks were checked with the wrong RandomX keys", tip.Index)
	}

	// A branch whose blocks were mined under some other key is still refused.
	forged := append([]Block{}, realChain[4:]...)
	forged[0].Hash = strings.Repeat("ab", 32)
	if work, wins := measureCandidateBranch(serveBlocks(forged), 3, realChain[3].Hash, 15, big.NewInt(0)); wins || work.Sign() != 0 {
		t.Error("a branch that does not prove its work was measured as having some")
	}
}

// Third review: dating the first block two hours back left two hours of
// dates that a miner finding the first blocks in a row could spend at once.
// Dating blocks close together and then one far later, with no real time
// passed, read as a block hours late: difficulty fell to its floor by block
// 23 and a hundred blocks followed nearly free.
func TestDatesFromBeforeTheLaunchBuyNothing(t *testing.T) {
	const start = 300000
	startTestChain(t, "devnet", func() {
		Params.Anchor = GenesisAnchor{BitcoinHeight: 800000}
		Params.GenesisTimestamp = 0
		t.Chdir(t.TempDir())
		if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", testHeaderA}); err != nil {
			t.Fatal(err)
		}
		Params.HeartbeatInitialDifficulty = start
	})
	if difficultyClockStart() != testHeaderTime || Params.GenesisTimestamp != testHeaderTime-bitcoinClockAllowance {
		t.Fatalf("the difficulty clock should start at the Bitcoin block's own time: %d", difficultyClockStart())
	}
	// The attack as the reviewer gave it: blocks 1 to 19 dated 45 seconds
	// apart from the backdated first block, then longer and longer gaps, all
	// inside the two hours before the launch.
	date := Params.GenesisTimestamp
	gaps := []int64{}
	for i := 0; i < 19; i++ {
		gaps = append(gaps, 45)
	}
	gaps = append(gaps, 495, 539, 719, 2000, 2000)
	lowest := int64(1) << 62
	for _, gap := range gaps {
		date += gap
		if date >= testHeaderTime {
			t.Fatal("the test's dates should all fall before the launch")
		}
		block := extendTestChain(t, date, testAddress("first miner"), nil)
		if int64(block.Difficulty) < lowest {
			lowest = int64(block.Difficulty)
		}
	}
	if lowest < start {
		t.Errorf("blocks dated before the launch brought difficulty down to %d; it started at %d", lowest, start)
	}
	// Once dates are past the launch, time counts from the launch: a block
	// dated one minute after it is one minute late, not two hours.
	tip := GetLatestBlockInternal()
	if got := CalculateNextDifficulty(tip, testHeaderTime+60); got < tip.Difficulty/2 {
		t.Errorf("a block one minute after the launch was read as hours late: difficulty %d after %d", got, tip.Difficulty)
	}
	// A network with no Bitcoin anchor is untouched.
	onNetwork(t, "devnet")
	if difficultyClockStart() != 0 || difficultyClock(5) != 5 {
		t.Error("a network without an anchor should read dates as they are")
	}
}

// Third review: a Bitcoin header cannot be tied to its height, so the
// earliest date its block may carry is what keeps an older block from being
// passed off as the anchor. A release that names a height must name that
// date too.
func TestAScheduledNetworkNamesItsEarliestDate(t *testing.T) {
	for _, network := range []ChainParams{cvnTestnetParams, devnetParams} {
		if network.Anchor.BitcoinHeight > 0 && network.Anchor.NotBefore <= 0 {
			t.Errorf("%s names Bitcoin block %d as its start and no earliest date (Anchor.NotBefore)", network.Name, network.Anchor.BitcoinHeight)
		}
		if network.Anchor.BitcoinHeight > 0 && network.Anchor.Unscheduled {
			t.Errorf("%s names a Bitcoin height and is still marked unscheduled", network.Name)
		}
	}
}
