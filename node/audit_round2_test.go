package node

import (
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Round 2 finding: the height kept in memory was written after each save,
// outside the order in which the saves were made. Two saves finishing
// together could leave the node believing its newest block was one below
// the real one.
func TestTipIsRightAfterManySavesAtOnce(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	blocks := buildTestChain(t, 40)
	StateMutex.Lock()
	rollbackChainToHeightLocked(0)
	StateMutex.Unlock()
	if GetLatestBlockInternal().Index != 0 {
		t.Fatal("could not set the chain back to its start")
	}

	// Eight savers race to put the same forty blocks back, each always
	// trying the block after whatever it sees as the tip.
	var savers sync.WaitGroup
	for i := 0; i < 8; i++ {
		savers.Add(1)
		go func() {
			defer savers.Done()
			for tries := 0; tries < 4000; tries++ {
				next := GetLatestBlockInternal().Index + 1
				if next > 40 {
					return
				}
				SaveBlockToStorageInternal(blocks[next])
			}
		}()
	}
	savers.Wait()

	inMemory := atomic.LoadInt64(&localHeightState)
	tip := GetLatestBlockInternal()
	if inMemory != 40 || tip.Index != 40 || tip.Hash != blocks[40].Hash {
		t.Fatalf("after the race the node believes its tip is #%d (%.12s); the chain's tip is #40 (%.12s)", inMemory, tip.Hash, blocks[40].Hash)
	}
	if GetBlockByHeightFromDB(41).Hash != "" {
		t.Error("a block was stored beyond the end of the chain")
	}
	if got := balanceOf("CVN_miner"); got != cvn("2000") {
		t.Errorf("after the race the miner has %s CVN, want 2000", FormatUnits(got))
	}
}

// Round 2 finding: strangers who had merely connected were asked before the
// seeds. A few of them, each claiming enormous work, could take turns being
// asked and keep a node from ever asking a seed.
func TestSeedsAreAskedBeforeStrangers(t *testing.T) {
	onNetwork(t, "devnet")
	t.Chdir(t.TempDir())
	Params.SeedHosts = []string{"10.0.0.1"}
	seed := Params.syncURL("10.0.0.1")
	stranger := Params.syncURL("10.0.0.9")
	PeerMapMutex.Lock()
	savedPeers := ActivePeerMap
	ActivePeerMap = map[string]time.Time{Params.gossipAddress("10.0.0.9"): time.Now()}
	PeerMapMutex.Unlock()
	savedProbe := probeChain
	t.Cleanup(func() {
		probeChain = savedProbe
		PeerMapMutex.Lock()
		ActivePeerMap = savedPeers
		PeerMapMutex.Unlock()
		syncSetAside.Lock()
		syncSetAside.until = make(map[string]time.Time)
		syncSetAside.Unlock()
	})

	claims := map[string]int64{seed: 110, stranger: 900000}
	probeChain = func(gateway string) (int64, *big.Int) {
		height, known := claims[gateway]
		if !known {
			return 0, nil
		}
		return height, big.NewInt(height * 1000)
	}
	ourWork := big.NewInt(100 * 1000)

	if chosen, height := chooseSyncSource(100, ourWork); chosen != seed || height != 110 {
		t.Fatalf("with a seed ahead of us, the node chose %q (claiming #%d); want the seed", chosen, height)
	}
	// Only when no seed claims anything better is a stranger asked.
	claims[seed] = 100
	if chosen, _ := chooseSyncSource(100, ourWork); chosen != stranger {
		t.Errorf("with the seed no further on than us, the node chose %q; want the stranger", chosen)
	}
	// And once it has been set aside, nobody.
	setSyncSourceAside(stranger, "test")
	if chosen, _ := chooseSyncSource(100, ourWork); chosen != "" {
		t.Errorf("with the stranger set aside, the node still chose %q", chosen)
	}
}

// Round 2 finding: a pass paused mining, and made the node ignore blocks
// from its peers, from its first moment. A node that merely claimed a better
// chain could therefore idle and deafen another at will. A pass now leaves
// the node alone until it has blocks to add or remove.
func TestComparingChainsDoesNotPauseTheNode(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	ours := buildTestChain(t, 6)
	liar := newStandInPeer(t, ours)
	liar.claimedHeight = 500000
	liar.claimedWork = "99999999999999999999999999999999"
	liar.asOnlySeed(t)

	before := atomic.LoadInt64(&chainChangePauses)
	TriggerActiveReSyncPass()
	if atomic.LoadInt64(&chainChangePauses) != before {
		t.Error("a pass against a node with nothing to give paused mining")
	}
	if atomic.LoadInt32(&IsSyncingActive) != 0 || atomic.LoadInt32(&syncPassRunning) != 0 {
		t.Error("the pass ended and left the node paused or busy")
	}
}

func TestCatchingUpDoesPauseTheNode(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	full := buildTestChain(t, 8)
	StateMutex.Lock()
	rollbackChainToHeightLocked(3)
	StateMutex.Unlock()
	newStandInPeer(t, full).asOnlySeed(t)

	before := atomic.LoadInt64(&chainChangePauses)
	TriggerActiveReSyncPass()
	if GetLatestBlockInternal().Index != 8 {
		t.Fatalf("the node did not catch up: at #%d", GetLatestBlockInternal().Index)
	}
	if atomic.LoadInt64(&chainChangePauses) == before {
		t.Error("blocks were added without the node being asked to stand still")
	}
	if atomic.LoadInt32(&IsSyncingActive) != 0 {
		t.Error("the pass ended and left mining paused")
	}
}

// Round 2 finding: a miner worked out its payout from the vault as it stood
// at whatever the tip had become, not at the block it was building on.
func TestMinerReadsTheVaultOfTheBlockItBuildsOn(t *testing.T) {
	startTestChain(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 3; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, owner, nil)
	}
	parent := GetLatestBlockInternal()
	// The next block carries a fee, a fifth of which goes into the vault.
	transfer, err := BuildSignedTransfer(privateKey, publicKey, "CVN_someone", cvn("10"), cvn("5"), ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	extendTestChain(t, timestamp+45, "CVN_other", []Transaction{transfer})
	tip := GetLatestBlockInternal()

	if got := vaultUnitsAfter(tip); got != cvn("1") || got != VaultBalanceUnits() {
		t.Fatalf("the vault after the newest block holds %s CVN, want 1", FormatUnits(got))
	}
	if got := vaultUnitsAfter(parent); got != 0 {
		t.Errorf("the vault after the block before it is read as %s CVN, want 0: a miner building there would write the wrong payout", FormatUnits(got))
	}
}

// Round 2 finding: the miner and the work server took any text starting with
// CVN_ as the address to pay. Rewards paid to text that is not a wallet
// address can never be spent.
func TestMiningIsOnlyPaidToARealAddress(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CVN_MINER_ADDRESS", "")
	_, _, real, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if address, err := resolveMinerAddress([]string{"cvn_node", "--miner-address", real}); err != nil || address != real {
		t.Fatalf("a real address was refused: %v", err)
	}
	for _, bad := range []string{"CVN_miner", "CVN_my_wallet", strings.ToUpper(real), real[:len(real)-2], real + "00"} {
		if address, err := resolveMinerAddress([]string{"cvn_node", "--miner-address", bad}); err == nil {
			t.Errorf("the miner would have paid its rewards to %q", address)
		}
	}
}

func TestWorkServerPaysOnlyARealAddress(t *testing.T) {
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 32}})
	Params.HeartbeatInitialDifficulty = 32
	atomic.StoreInt32(&externalMinersConnected, 0)
	CustomMinerAddress = "CVN_" + strings.Repeat("1", 40) // this node's own address
	server := startWorkServerForTest(t)

	// A miner that mistypes its address must be told, not silently made to
	// mine for the owner of the node.
	for _, login := range []string{"CVN_my_wallet", "x", "", "CVN_" + strings.Repeat("G", 40)} {
		miner := connectFakeMiner(t, server)
		if _, errorText, _ := miner.call("login", map[string]string{"login": login, "pass": "x"}); errorText == "" {
			t.Errorf("a miner logging in as %q was given work", login)
		}
	}
	if atomic.LoadInt32(&externalMinersConnected) != 0 {
		t.Error("a refused miner was counted as connected")
	}

	// While the node is changing its chain, answers are turned away as stale.
	miner := connectFakeMiner(t, server)
	result, errorText, _ := miner.call("login", map[string]string{"login": "CVN_" + strings.Repeat("e", 40), "pass": "x"})
	if errorText != "" {
		t.Fatalf("a miner with a real address was refused: %s", errorText)
	}
	jobID := string(result)
	if at := strings.Index(jobID, `"job_id":"`); at >= 0 {
		jobID = jobID[at+len(`"job_id":"`):]
		jobID = jobID[:strings.Index(jobID, `"`)]
	}
	atomic.StoreInt32(&IsSyncingActive, 1)
	_, errorText, _ = miner.call("submit", map[string]string{"job_id": jobID, "nonce": "00000000"})
	atomic.StoreInt32(&IsSyncingActive, 0)
	if !strings.Contains(errorText, "stale job") {
		t.Errorf("an answer sent while the chain was changing got %q; want it turned away as stale", errorText)
	}
}

// Round 2 finding: under the first test network's rules a block's identifier
// was not checked to be its own before the node acted on it.
func TestAnIdentifierThatIsNotTheBlocksOwnIsRefusedOnArrival(t *testing.T) {
	startTestChain(t, "testnet-1", func() {
		Params.HeartbeatHeight = 1
		Params.HeartbeatInitialDifficulty = HeartbeatMinDifficulty // so the test blocks mine in a moment
		applyParams()
	})
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 3; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, "CVN_miner", nil)
	}
	honest := mineTestBlock(t, timestamp+45, "CVN_miner", nil, 0)
	if !VerifyIncomingWireBlock(honest) {
		t.Fatal("an honest block was refused on arrival")
	}
	forged := honest
	forged.Index += 500
	forged.Hash = strings.Repeat("0", 64) // meets any difficulty, belongs to no block
	if VerifyIncomingWireBlock(forged) {
		t.Error("a block far ahead, with an identifier of all zeros, passed the check on arriving blocks")
	}
}

// Round 2 finding: tables of addresses that had connected, been refused or
// been rate-limited were never emptied.
func TestOldEntriesAreForgotten(t *testing.T) {
	now := time.Now()
	BlacklistMutex.Lock()
	MaliciousIPBlacklist["203.0.113.1"] = now.Add(-time.Minute)
	MaliciousIPBlacklist["203.0.113.2"] = now.Add(time.Hour)
	BlacklistMutex.Unlock()
	IPRegistryMutex.Lock()
	IPMalformedTrack["203.0.113.3"] = 4
	IPRegistryMutex.Unlock()
	IPTokensMutex.Lock()
	IPTokensMap["203.0.113.4"] = &TokenBucket{lastRefill: now.Add(-2 * time.Hour)}
	IPTokensMap["203.0.113.5"] = &TokenBucket{lastRefill: now}
	IPTokensMutex.Unlock()

	pruneNetworkTables(now)

	if peerIsBanned("203.0.113.1") || !peerIsBanned("203.0.113.2") {
		t.Error("an expired refusal was kept, or a current one dropped")
	}
	BlacklistMutex.Lock()
	_, expiredKept := MaliciousIPBlacklist["203.0.113.1"]
	delete(MaliciousIPBlacklist, "203.0.113.2")
	BlacklistMutex.Unlock()
	IPRegistryMutex.Lock()
	_, countKept := IPMalformedTrack["203.0.113.3"]
	IPRegistryMutex.Unlock()
	IPTokensMutex.Lock()
	_, idleKept := IPTokensMap["203.0.113.4"]
	_, activeKept := IPTokensMap["203.0.113.5"]
	delete(IPTokensMap, "203.0.113.5")
	IPTokensMutex.Unlock()
	if expiredKept || countKept || idleKept || !activeKept {
		t.Errorf("kept: expired refusal %v, old count %v, idle limit %v; active limit kept %v", expiredKept, countKept, idleKept, activeKept)
	}
}

// Round 2 finding: the schedule was counted from block 1, whose date the
// first miner chooses. Dates written on the launch blocks then leaned on
// every later difficulty.
func TestLaunchBlockDatesDoNotLeanOnLaterDifficulty(t *testing.T) {
	scheduleAfter := func(firstBlockSecondsAfterGenesis int64) int64 {
		startTestChain(t, "devnet", func() { Params.GenesisTimestamp = time.Now().Unix() - 3*24*3600 })
		// Block 1 is dated wherever its miner likes; the rest of the launch
		// window follows at a steady pace up to a fixed moment.
		extendTestChain(t, Params.GenesisTimestamp+firstBlockSecondsAfterGenesis, "CVN_first", nil)
		end := Params.GenesisTimestamp + 24*3600
		for height := int64(2); height <= HeartbeatLaunchBlocks; height++ {
			extendTestChain(t, end-(HeartbeatLaunchBlocks-height)*45, "CVN_first", nil)
		}
		timestamp := end
		for i := 0; i < 5; i++ {
			timestamp += 45
			extendTestChain(t, timestamp, "CVN_first", nil)
		}
		drift := scheduleDriftSeconds(GetLatestBlockInternal())
		GlobalBoltEngine.Close()
		GlobalBoltEngine = nil
		return drift
	}
	honest := scheduleAfter(23*3600 + 600)
	backdated := scheduleAfter(1)
	if honest != 0 || backdated != 0 {
		t.Errorf("five on-time blocks after the launch window: the chain is counted %d seconds off schedule, or %d if block 1 was backdated; both should be 0", honest, backdated)
	}
}
