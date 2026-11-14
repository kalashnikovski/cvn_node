package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two real Bitcoin block headers, used as examples of an anchor.
const (
	// Bitcoin block 800,000, mined 24 July 2023.
	testHeaderA    = "00601d3455bb9fbd966b3ea2dc42d0c22722e4c0c1729fad17210100000000000000000055087fab0c8f3f89f8bcfd4df26c504d81b0a88e04907161838c0c53001af09135edbd64943805175e955e06"
	testHeaderHash = "00000000000000000002a7c4c1e48d76c5a37902165a270156b7a8d72728a054"
	testHeaderTime = int64(1690168629)
	// Bitcoin block 800,001.
	testHeaderB     = "00a0012054a02827d7a8b75601275a160279a3c5768de4c1c4a702000000000000000000394ddc6a5de035874cfa22167bfe923953187b5a19fbb84e186dea3c78fd871c9bedbd6494380517f5c93c8c"
	testHeaderBHash = "00000000000000000000e26b239cf19ec7ace5edd9694d51a3f6933247720947"
	// Bitcoin's very first block: a real header, but with almost no work by
	// the standard of any recent block.
	bitcoinFirstHeader = "0100000000000000000000000000000000000000000000000000000000000000000000003ba3edfd7a7b12b27ac72c3e67768f617fc81bc3888a51323a9fb8aa4b1e5e4a29ab5f49ffff001d1dac2b7c"
)

// asLaunchNetwork turns the current settings into a network that, like a real
// launch, names a Bitcoin block whose header is not known yet.
func asLaunchNetwork() {
	Params.Name = "launch-test"
	Params.Anchor = GenesisAnchor{BitcoinHeight: 800000}
	Params.GenesisTimestamp = 0
}

func TestBitcoinHeaderIsCheckedAndRead(t *testing.T) {
	hash, timestamp, err := parseBitcoinHeader(testHeaderA)
	if err != nil || hash != testHeaderHash || timestamp != testHeaderTime {
		t.Fatalf("block 800,000 read as hash %s, time %d, err %v", hash, timestamp, err)
	}
	if hash, _, err := parseBitcoinHeader("  " + strings.ToUpper(testHeaderB) + "\r\n"); err != nil || hash != testHeaderBHash {
		t.Errorf("a header in capitals with stray spaces should be read the same: %s %v", hash, err)
	}
	oneDigitChanged := testHeaderA[:100] + "f" + testHeaderA[101:]
	if testHeaderA[100] == 'f' {
		oneDigitChanged = testHeaderA[:100] + "e" + testHeaderA[101:]
	}
	bad := map[string]string{
		"with one digit changed":  oneDigitChanged,
		"too short":               testHeaderA[:159],
		"too long":                testHeaderA + "0",
		"not hex":                 testHeaderA[:159] + "g",
		"empty":                   "",
		"a block hash":            testHeaderHash,
		"the first Bitcoin block": bitcoinFirstHeader,
		"all zeros":               strings.Repeat("0", 160),
	}
	for name, value := range bad {
		if hash, _, err := parseBitcoinHeader(value); err == nil {
			t.Errorf("a header %s was accepted (hash %s)", name, hash)
		}
	}
}

func TestLaunchNetworkWillNotStartWithoutItsAnchor(t *testing.T) {
	onNetwork(t, "devnet")
	asLaunchNetwork()
	t.Chdir(t.TempDir()) // no anchor file here

	err := resolveGenesisAnchor([]string{"cvn_node", "--network", "launch-test"})
	if err == nil {
		t.Fatal("a launch network started without its anchor")
	}
	for _, expected := range []string{"800000", "--genesis-anchor", "genesis_anchor_launch-test.txt", "header"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("the refusal should tell the operator about %q: %v", expected, err)
		}
	}
	if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", "not-a-header"}); err == nil {
		t.Error("nonsense was accepted as the anchor")
	}
	if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", testHeaderHash}); err == nil {
		t.Error("a bare block hash was accepted; the header is needed for the date")
	}
	if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", strings.ToUpper(testHeaderA)}); err != nil || Params.Anchor.BitcoinHash != testHeaderHash || Params.GenesisTimestamp != testHeaderTime {
		t.Errorf("an anchor given on the command line was not taken: %v", err)
	}

	// The same from the anchor file, with the stray spaces and line ending a
	// person would leave in it.
	asLaunchNetwork()
	if err := os.WriteFile("genesis_anchor_launch-test.txt", []byte("  "+testHeaderB+"\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := resolveGenesisAnchor([]string{"cvn_node"}); err != nil || Params.Anchor.BitcoinHash != testHeaderBHash {
		t.Errorf("an anchor in the file was not taken: %v", err)
	}
	// A header released with the software needs nothing from the operator,
	// and what the operator types cannot override it.
	asLaunchNetwork()
	Params.Anchor.BitcoinHeader = testHeaderA
	if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", testHeaderB}); err != nil || Params.Anchor.BitcoinHash != testHeaderHash {
		t.Errorf("a released anchor was overridden or refused: %v", err)
	}
}

func TestNetworksWithoutAnAnchorNeedNothing(t *testing.T) {
	for _, network := range []string{"testnet-1", "devnet"} {
		onNetwork(t, network)
		if err := resolveGenesisAnchor([]string{"cvn_node"}); err != nil {
			t.Errorf("%s should start without an anchor: %v", network, err)
		}
	}
	onNetwork(t, "testnet-1")
	if genesisStatement() != "" || buildGenesisBlock().Hash != testnet1GenesisHash {
		t.Error("the genesis block of testnet-1 must stay exactly as it is")
	}
	onNetwork(t, "devnet")
	if !strings.Contains(genesisStatement(), "no anchor") {
		t.Errorf("the genesis block of devnet should say plainly that it has no anchor: %q", genesisStatement())
	}
}

// The anchor decides the genesis block, and the genesis block decides the
// RandomX key for the first blocks. So before the anchor exists, nobody can
// build the genesis block, mine on it, or even prepare to.
func TestAnchorDecidesTheGenesisBlockAndTheFirstKey(t *testing.T) {
	onNetwork(t, "devnet")
	asLaunchNetwork()

	if err := useAnchorHeader(testHeaderA); err != nil {
		t.Fatal(err)
	}
	genesisA := buildGenesisBlock()
	if err := useAnchorHeader(testHeaderB); err != nil {
		t.Fatal(err)
	}
	genesisB := buildGenesisBlock()

	if genesisA.Hash == genesisB.Hash {
		t.Fatal("two different anchors gave the same genesis block")
	}
	if !strings.Contains(genesisA.GuardMatrix, testHeaderHash) || !strings.Contains(genesisA.GuardMatrix, "Bitcoin block 800000") {
		t.Errorf("the anchor is not written in the genesis header: %q", genesisA.GuardMatrix)
	}
	// The genesis block is dated by the Bitcoin block, not by the software.
	if genesisA.Timestamp != testHeaderTime || genesisA.Transactions[0].Timestamp != testHeaderTime {
		t.Errorf("the genesis block is dated %d; the anchor block is dated %d", genesisA.Timestamp, testHeaderTime)
	}
	if genesisA.Hash != CalculateHash(genesisA) {
		t.Error("the genesis identifier does not cover its header")
	}
	altered := genesisA
	altered.GuardMatrix = strings.Replace(altered.GuardMatrix, testHeaderHash, testHeaderBHash, 1)
	if CalculateHash(altered) == genesisA.Hash {
		t.Error("the anchor can be changed without changing the genesis identifier")
	}
	redated := genesisA
	redated.Timestamp++
	if CalculateHash(redated) == genesisA.Hash {
		t.Error("the genesis date can be changed without changing the genesis identifier")
	}
	for _, genesis := range []Block{genesisA, genesisB} {
		for _, output := range genesis.Transactions[0].Outputs {
			if output.Amount.Units() != 0 {
				t.Errorf("the genesis block pays %s; it must pay nothing", output.Amount)
			}
		}
	}
	if powKeyHeight(1) != 0 || powKeyHeight(Params.KeyLagBlocks+1) != 0 {
		t.Error("the first blocks must be keyed by the genesis block")
	}
}

func TestChainOnAnAnchoredNetwork(t *testing.T) {
	startTestChain(t, "devnet", func() {
		asLaunchNetwork()
		if err := useAnchorHeader(testHeaderA); err != nil {
			t.Fatal(err)
		}
	})
	genesis := GetBlockByHeightFromDB(0)
	if !strings.Contains(genesis.GuardMatrix, testHeaderHash) || genesis.Hash != buildGenesisBlock().Hash || genesis.Timestamp != testHeaderTime {
		t.Fatalf("the stored genesis block does not carry the anchor: %q", genesis.GuardMatrix)
	}
	if err := checkStoredGenesis(); err != nil {
		t.Fatalf("a database was refused on its own network: %v", err)
	}

	// Nobody can date a block at or before the anchor block: the chain's
	// clock starts at the launch, and its first blocks cannot be backdated.
	for _, early := range []int64{testHeaderTime, testHeaderTime - 1, testHeaderTime - 7*24*3600} {
		if ValidateBlockSignatures(mineTestBlock(t, early, "CVN_miner", nil, 0)) {
			t.Errorf("a block dated %d seconds before the anchor block was accepted", testHeaderTime-early)
		}
	}
	block := extendTestChain(t, testHeaderTime+60, "CVN_miner", nil)
	if block.PrevHash != genesis.Hash || balanceOf("CVN_miner") != cvn("50") {
		t.Error("the first block does not build on the anchored genesis block")
	}

	// Started again with a different anchor, the node must refuse the
	// database and not quietly carry on with the wrong chain.
	if err := useAnchorHeader(testHeaderB); err != nil {
		t.Fatal(err)
	}
	err := checkStoredGenesis()
	if err == nil {
		t.Fatal("a database from one anchor was accepted under another")
	}
	if !strings.Contains(err.Error(), genesis.Hash) || !strings.Contains(err.Error(), filepath.Base(BlockchainFile)) {
		t.Errorf("the refusal should name the database and both genesis blocks: %v", err)
	}
}

// The public test network of the launch rules.
func TestCVNTestnet(t *testing.T) {
	for _, name := range []string{"cvn_testnet", "CVN_Testnet", "cvn-testnet", "CVN Testnet"} {
		if err := SelectNetwork(name); err != nil || Params.Name != "cvn_testnet" {
			t.Errorf("%q should select the CVN Testnet (got %q, %v)", name, Params.Name, err)
		}
	}
	onNetwork(t, "cvn_testnet")
	if BlockchainFile != "cvn_testnet.db" || len(Params.SeedHosts) != 3 {
		t.Errorf("database %q, %d seeds", BlockchainFile, len(Params.SeedHosts))
	}
	// Every launch rule is on.
	if !Params.StrictFormat || !Params.NoUncles || !Params.AmountsAsUnits || !Params.ScheduleAnchored ||
		Params.Economics == nil || Params.MaxBlockBytes != 300000 || !networkUsesRandomX() ||
		Params.HeartbeatHeight != 1 || Params.TxForkHeight != 1 || Params.KeyEpochBlocks != 1920 || Params.KeyLagBlocks != 100 {
		t.Error("the CVN Testnet must run every launch rule")
	}
	if Params.HeartbeatInitialDifficulty != 1000000 || minDifficultyAt(1) != 5000 {
		t.Errorf("starting difficulty %d, minimum %d", Params.HeartbeatInitialDifficulty, minDifficultyAt(1))
	}
	// It shares no port, database or message key with another network, so
	// all three can run on one machine and never hear each other.
	for _, other := range []ChainParams{testnet1Params, devnetParams} {
		mine := []int{Params.GossipPort, Params.SyncPort, Params.SyncFallbackPort, Params.StatusPort, Params.StratumPort}
		theirs := map[int]bool{other.GossipPort: true, other.SyncPort: true, other.SyncFallbackPort: true, other.StatusPort: true, other.StratumPort: true}
		for _, port := range mine {
			if theirs[port] {
				t.Errorf("port %d is also used by %s", port, other.Name)
			}
		}
		if other.AuthKey == Params.AuthKey || other.DatabaseFile == Params.DatabaseFile || other.PeerCacheFile == Params.PeerCacheFile {
			t.Errorf("the CVN Testnet shares a key or a file with %s", other.Name)
		}
	}

	// Until its start is announced it cannot be started at all.
	t.Chdir(t.TempDir())
	err := resolveGenesisAnchor([]string{"cvn_node", "--network", "cvn_testnet", "--genesis-anchor", testHeaderA})
	if err == nil || !strings.Contains(err.Error(), "not been scheduled") {
		t.Fatalf("the unscheduled network should refuse to start, whatever it is given: %v", err)
	}

	// Once scheduled and anchored, block 1 is asked for the starting
	// difficulty, because the genesis block is dated at the launch itself.
	Params.Anchor = GenesisAnchor{BitcoinHeight: 800000}
	if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", testHeaderA}); err != nil {
		t.Fatal(err)
	}
	genesis := buildGenesisBlock()
	if got := CalculateNextDifficulty(genesis, testHeaderTime+60); got != 1000000 {
		t.Errorf("a first block one minute after the anchor needs difficulty %d, want 1,000,000", got)
	}
	// A Bitcoin block dated well before anyone could start does not make the
	// start any easier (see audit_launch_test.go).
	if got := CalculateNextDifficulty(genesis, testHeaderTime+3600); got != 1000000 {
		t.Errorf("a first block an hour after the anchor needs difficulty %d, want 1,000,000", got)
	}
}
