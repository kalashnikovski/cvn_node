package node

import (
	"reflect"
	"strings"
	"testing"
)

// The values of the public test network must never change by accident: a
// different key, port or file would cut a node off from everyone else. This
// test pins them. (Its rules are pinned in genesis_test.go.)
func TestCVNTestnetSettingsArePinned(t *testing.T) {
	onNetwork(t, "cvn_testnet")
	t.Setenv("CVN_NETWORK_AUTH_TOKEN", "")
	if got := string(getNetworkAuthSecret()); got != "CVN_TESTNET_NODE_MESSAGE_KEY_COINS_HAVE_NO_VALUE_2026" {
		t.Errorf("message key changed: %q", got)
	}
	if Params.GossipPort != 18080 || Params.SyncPort != 18081 || Params.SyncFallbackPort != 19091 || Params.StatusPort != 18082 || Params.StratumPort != 18083 {
		t.Errorf("ports changed: %d %d %d %d %d", Params.GossipPort, Params.SyncPort, Params.SyncFallbackPort, Params.StatusPort, Params.StratumPort)
	}
	if Params.DatabaseFile != "cvn_testnet.db" || Params.PeerCacheFile != "peers_testnet.json" {
		t.Errorf("file names changed: %q %q", Params.DatabaseFile, Params.PeerCacheFile)
	}
	hosts := []string{"207.148.67.11", "64.177.44.185", "149.28.69.173", "149.248.54.9", "95.179.179.191", "65.20.67.25"}
	var wantSync, wantGossip []string
	wantWallet := []string{"http://localhost:18081", "http://localhost:19091"}
	for _, host := range hosts {
		wantSync = append(wantSync, "http://"+host+":18081")
		wantGossip = append(wantGossip, host+":18080")
		wantWallet = append(wantWallet, "http://"+host+":18081")
	}
	if got := seedSyncURLs(); !reflect.DeepEqual(got, wantSync) {
		t.Errorf("seed download addresses changed: %v", got)
	}
	if got := seedGossipAddresses(); !reflect.DeepEqual(got, wantGossip) {
		t.Errorf("seed node addresses changed: %v", got)
	}
	if got := walletNodeGateways(); !reflect.DeepEqual(got, wantWallet) {
		t.Errorf("wallet node list changed: %v", got)
	}
	if localStatusURL() != "http://localhost:18082/status" {
		t.Errorf("status address changed: %s", localStatusURL())
	}
	if Params.gossipAddress("10.1.2.3") != "10.1.2.3:18080" || Params.syncURL("10.1.2.3") != "http://10.1.2.3:18081" {
		t.Errorf("address formatting changed")
	}
}

func TestDevnetIsSeparateFromTheCVNTestnet(t *testing.T) {
	if cvnTestnetParams.AuthKey == devnetParams.AuthKey {
		t.Error("devnet shares the CVN Testnet's message key")
	}
	for _, port := range []int{devnetParams.GossipPort, devnetParams.SyncPort, devnetParams.SyncFallbackPort, devnetParams.StatusPort, devnetParams.StratumPort} {
		for _, other := range []int{cvnTestnetParams.GossipPort, cvnTestnetParams.SyncPort, cvnTestnetParams.SyncFallbackPort, cvnTestnetParams.StatusPort, cvnTestnetParams.StratumPort} {
			if port == other {
				t.Errorf("devnet reuses CVN Testnet port %d", port)
			}
		}
	}
	if devnetParams.DatabaseFile == cvnTestnetParams.DatabaseFile || devnetParams.PeerCacheFile == cvnTestnetParams.PeerCacheFile {
		t.Error("devnet shares a file with the CVN Testnet")
	}
	if len(devnetParams.SeedHosts) != 0 {
		t.Error("devnet must not have public seeds")
	}

	startTestChain(t, "devnet")
	if genesis := GetBlockByHeightFromDB(0); genesis.Hash == "" {
		t.Error("devnet has no genesis block")
	}
	if !IsTxForkHeight(1) || !IsHeartbeatHeight(1) || IsTxForkHeight(0) || IsHeartbeatHeight(0) {
		t.Error("the rules apply from block 1, and the genesis block is not a mined block")
	}
}

func TestSelectNetwork(t *testing.T) {
	saved := Params
	defer func() { Params = saved; applyParams() }()

	if err := SelectNetwork("no-such-network"); err == nil {
		t.Error("an unknown network name was accepted")
	}
	// The first test network is retired, and asking for it says so.
	for _, name := range []string{"testnet-1", " Testnet-1 "} {
		if err := SelectNetwork(name); err == nil || !strings.Contains(err.Error(), "retired") {
			t.Errorf("%q should be refused as retired (got %v)", name, err)
		}
	}
	// With no network named the program runs on the CVN Testnet, the network
	// being launched.
	if err := SelectNetwork(""); err != nil || Params.Name != "cvn_testnet" || BlockchainFile != "cvn_testnet.db" {
		t.Errorf("no network named should select cvn_testnet (got %q, %q, %v)", Params.Name, BlockchainFile, err)
	}
	if err := SelectNetwork("devnet"); err != nil || Params.Name != "devnet" || BlockchainFile != "cvn_devnet.db" {
		t.Errorf("devnet not selected: %q %q %v", Params.Name, BlockchainFile, err)
	}
	if got := requestedNetworkName([]string{"cvn", "--network", "devnet"}); got != "devnet" {
		t.Errorf("--network devnet read as %q", got)
	}
	if got := requestedNetworkName([]string{"cvn", "--network=devnet", "--wallet"}); got != "devnet" {
		t.Errorf("--network=devnet read as %q", got)
	}
}

// A devnet chain runs end to end: blocks, a transfer with a fee, exact amounts.
func TestDevnetChainRuns(t *testing.T) {
	startTestChain(t, "devnet")
	privateKey, publicKey, address, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	_, timestamp := buildSharedHistory(t, address, address)
	if got := balanceOf(address); got != cvn("600") {
		t.Fatalf("after 12 blocks the miner has %s CVN, want 600", FormatUnits(got))
	}
	transfer, err := BuildSignedTransfer(privateKey, publicKey, testAddress("someone_else"), cvn("75.5"), cvn("0.25"), ListSpendableCoins(address))
	if err != nil {
		t.Fatalf("build transfer: %v", err)
	}
	block := extendTestChain(t, timestamp+45, testAddress("other_miner"), []Transaction{transfer})
	if !IsHeartbeatHeight(block.Index) || block.Difficulty < HeartbeatMinDifficulty {
		t.Errorf("block #%d is not a heartbeat block (difficulty %d)", block.Index, block.Difficulty)
	}
	if got := balanceOf(testAddress("someone_else")); got != cvn("75.5") {
		t.Errorf("recipient has %s, want 75.5", FormatUnits(got))
	}
	// A fifth of the 0.25 fee goes to the vault; the miner keeps the rest.
	if got := balanceOf(testAddress("other_miner")); got != cvn("50.2") {
		t.Errorf("miner of the block has %s, want 50.2 (reward plus four fifths of the fee)", FormatUnits(got))
	}
	if got := VaultBalanceUnits(); got != cvn("0.05") {
		t.Errorf("vault holds %s, want 0.05 (a fifth of the fee)", FormatUnits(got))
	}
	if got := balanceOf(address); got != cvn("524.25") {
		t.Errorf("sender has %s, want 524.25", FormatUnits(got))
	}
}

// The numbers agreed for the new genesis.
func TestNewGenesisEconomics(t *testing.T) {
	e := NewGenesisEconomics
	if e.EpochBlocks != 7*365*24*3600/e.BlockSeconds {
		t.Errorf("an epoch is %d blocks, want 7 years of %d-second blocks", e.EpochBlocks, e.BlockSeconds)
	}
	if e.EmissionEndHeight() != 34339200 {
		t.Errorf("emission ends at %d, want 34,339,200", e.EmissionEndHeight())
	}
	if got := e.TotalSupplyUnits(); got != 486727500*UnitsPerCVN {
		t.Errorf("total supply is %s CVN, want 486,727,500", FormatUnits(got))
	}
	wantRewards := []int64{5000000000, 2500000000, 1250000000, 625000000, 312500000, 156250000, 78125000}
	for epoch, want := range wantRewards {
		first := int64(epoch)*e.EpochBlocks + 1
		last := int64(epoch+1) * e.EpochBlocks
		if e.RewardUnitsAt(first) != want || e.RewardUnitsAt(last) != want {
			t.Errorf("epoch %d pays %d / %d units, want %d", epoch+1, e.RewardUnitsAt(first), e.RewardUnitsAt(last), want)
		}
	}
	if e.RewardUnitsAt(0) != 0 {
		t.Error("the genesis block must pay nothing")
	}
	if e.RewardUnitsAt(e.EmissionEndHeight()+1) != 0 {
		t.Error("a reward is paid after emission ends")
	}
	// Adding up every block's reward one epoch at a time must hit the cap exactly.
	var issued int64
	for epoch := int64(0); epoch < e.EmissionEpochs; epoch++ {
		issued += e.RewardUnitsAt(epoch*e.EpochBlocks+1) * e.EpochBlocks
	}
	if issued != e.TotalSupplyUnits() {
		t.Errorf("per-block rewards add up to %d, cap is %d", issued, e.TotalSupplyUnits())
	}
	if e.VaultPayoutBlocks != 2*e.EpochBlocks {
		t.Errorf("vault constant is %d blocks, want 14 years", e.VaultPayoutBlocks)
	}
	if e.SweepDivisor != 5 || e.FeeVaultDivisor != 5 || e.MinOutputUnits != 10000 || e.ReorgLimitBlocks != 100 {
		t.Error("sweep, fee split, minimum output or reorganisation limit differ from the specification")
	}
}

func TestSeedsGivenAtStart(t *testing.T) {
	t.Setenv("CVN_SEEDS", "")
	onNetwork(t, "devnet")
	applySeedOverride([]string{"cvn_node", "--network", "devnet"})
	if len(Params.SeedHosts) != 0 {
		t.Fatal("devnet gained seeds without being given any")
	}
	unreadable := applySeedOverride([]string{"cvn_node", "--seeds", "10.0.0.1, 10.0.0.2 ,,bad host,http://x,10.0.0.3:99"})
	if got := strings.Join(Params.SeedHosts, " "); got != "10.0.0.1 10.0.0.2" {
		t.Errorf("seeds read as %q", got)
	}
	// What could not be read is named, so that it can be reported.
	if got := strings.Join(unreadable, "|"); got != "bad host|http://x|10.0.0.3:99" {
		t.Errorf("the unreadable seeds were given as %q", got)
	}
	Params.SeedHosts = nil
	if left := applySeedOverride([]string{"cvn_node", "--seeds", "10.0.0.1,10.0.0.2"}); len(left) != 0 {
		t.Errorf("a well-written list had unreadable entries: %v", left)
	}
	if got := seedSyncURLs(); len(got) != 2 || got[0] != "http://10.0.0.1:28081" {
		t.Errorf("seed download addresses: %v", got)
	}
	// A network that has its own seeds keeps them.
	onNetwork(t, "cvn_testnet")
	before := strings.Join(Params.SeedHosts, " ")
	applySeedOverride([]string{"cvn_node", "--seeds", "10.9.9.9"})
	if before == "" || strings.Join(Params.SeedHosts, " ") != before {
		t.Error("the CVN Testnet's seeds were replaced from the command line")
	}
}
