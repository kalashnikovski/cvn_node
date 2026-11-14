package node

import (
	"reflect"
	"strings"
	"testing"
)

// The values of the running test network must never change by accident: a
// different key, port or genesis block would cut a node off from everyone
// else. This test pins them.
func TestTestnet1SettingsArePinned(t *testing.T) {
	if Params.Name != "testnet-1" {
		t.Fatalf("default network is %q, want testnet-1", Params.Name)
	}
	t.Setenv("CVN_NETWORK_AUTH_TOKEN", "")
	if got := string(getNetworkAuthSecret()); got != "COVENANT_STANDARD_MAINNET_IGNITION_AUTHENTICATION_KEY_TOKEN_2026" {
		t.Errorf("message key changed: %q", got)
	}
	if Params.GossipPort != 8080 || Params.SyncPort != 8081 || Params.SyncFallbackPort != 9091 || Params.StatusPort != 8082 {
		t.Errorf("ports changed: %d %d %d %d", Params.GossipPort, Params.SyncPort, Params.SyncFallbackPort, Params.StatusPort)
	}
	if Params.DatabaseFile != "cvn_mainnet.db" || Params.PeerCacheFile != "peers.json" {
		t.Errorf("file names changed: %q %q", Params.DatabaseFile, Params.PeerCacheFile)
	}
	if Params.HeartbeatHeight != 2500 || Params.TxForkHeight != 6100 {
		t.Errorf("rule heights changed: %d %d", Params.HeartbeatHeight, Params.TxForkHeight)
	}
	if Params.AmountsAsUnits {
		t.Error("testnet-1 writes amounts as decimal numbers; changing that breaks every existing block hash")
	}
	hosts := []string{"207.148.67.11", "64.177.44.185", "149.28.69.173", "149.248.54.9", "95.179.179.191", "65.20.67.25"}
	var wantSync, wantGossip []string
	wantWallet := []string{"http://localhost:8081", "http://localhost:9091"}
	for _, host := range hosts {
		wantSync = append(wantSync, "http://"+host+":8081")
		wantGossip = append(wantGossip, host+":8080")
		wantWallet = append(wantWallet, "http://"+host+":8081")
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
	if localStatusURL() != "http://localhost:8082/status" {
		t.Errorf("status address changed: %s", localStatusURL())
	}
	if Params.gossipAddress("10.1.2.3") != "10.1.2.3:8080" || Params.syncURL("10.1.2.3") != "http://10.1.2.3:8081" {
		t.Errorf("address formatting changed")
	}
}

// The genesis block of testnet-1 as every node on that network holds it.
const testnet1GenesisHash = "50371f1749e88e38d339bdfa21bf40d8a96f4997bd7c3a68335bed6344de8369"

func TestTestnet1GenesisBlockIsPinned(t *testing.T) {
	startTestChain(t, "testnet-1")
	genesis := GetBlockByHeightFromDB(0)
	if genesis.Hash != testnet1GenesisHash {
		t.Fatalf("testnet-1 genesis hash is %s, want %s", genesis.Hash, testnet1GenesisHash)
	}
	if balanceOf("COVENANT_STEWARD_ASSEMBLY") != 0 {
		t.Errorf("genesis block pays something; it must pay nothing")
	}
}

func TestDevnetIsSeparateFromTestnet1(t *testing.T) {
	if testnet1Params.AuthKey == devnetParams.AuthKey {
		t.Error("devnet shares testnet-1's message key")
	}
	for _, port := range []int{devnetParams.GossipPort, devnetParams.SyncPort, devnetParams.SyncFallbackPort, devnetParams.StatusPort} {
		for _, other := range []int{testnet1Params.GossipPort, testnet1Params.SyncPort, testnet1Params.SyncFallbackPort, testnet1Params.StatusPort} {
			if port == other {
				t.Errorf("devnet reuses testnet-1 port %d", port)
			}
		}
	}
	if devnetParams.DatabaseFile == testnet1Params.DatabaseFile || devnetParams.PeerCacheFile == testnet1Params.PeerCacheFile {
		t.Error("devnet shares a file with testnet-1")
	}
	if len(devnetParams.SeedHosts) != 0 {
		t.Error("devnet must not have public seeds")
	}

	startTestChain(t, "devnet")
	genesis := GetBlockByHeightFromDB(0)
	if genesis.Hash == "" || genesis.Hash == testnet1GenesisHash {
		t.Errorf("devnet genesis hash %q must exist and differ from testnet-1's", genesis.Hash)
	}
	if !IsTxForkHeight(1) || !Params.AmountsAsUnits || !IsHeartbeatHeight(1) {
		t.Error("devnet rule heights were not applied")
	}
}

func TestSelectNetwork(t *testing.T) {
	saved := Params
	defer func() { Params = saved; applyParams() }()

	if err := SelectNetwork("no-such-network"); err == nil {
		t.Error("an unknown network name was accepted")
	}
	for _, name := range []string{"", "testnet-1", "testnet", " Testnet-1 "} {
		if err := SelectNetwork(name); err != nil || Params.Name != "testnet-1" {
			t.Errorf("%q should select testnet-1 (got %q, %v)", name, Params.Name, err)
		}
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
	applySeedOverride([]string{"cvn_node", "--seeds", "10.0.0.1, 10.0.0.2 ,,bad host,http://x,10.0.0.3:99"})
	if got := strings.Join(Params.SeedHosts, " "); got != "10.0.0.1 10.0.0.2" {
		t.Errorf("seeds read as %q", got)
	}
	if got := seedSyncURLs(); len(got) != 2 || got[0] != "http://10.0.0.1:28081" {
		t.Errorf("seed download addresses: %v", got)
	}
	// A network that has its own seeds keeps them.
	onNetwork(t, "testnet-1")
	before := strings.Join(Params.SeedHosts, " ")
	applySeedOverride([]string{"cvn_node", "--seeds", "10.9.9.9"})
	if strings.Join(Params.SeedHosts, " ") != before {
		t.Error("testnet-1's seeds were replaced from the command line")
	}
}
