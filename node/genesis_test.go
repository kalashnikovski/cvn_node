package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two made-up values with the shape of Bitcoin block hashes.
const (
	testAnchorA = "00000000000000000001a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f7"
	testAnchorB = "00000000000000000002b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708"
)

// asLaunchNetwork turns the current settings into a network that, like a real
// launch, names a Bitcoin block whose hash is not known yet.
func asLaunchNetwork() {
	Params.Name = "launch-test"
	Params.Anchor = GenesisAnchor{BitcoinHeight: 1000000}
}

func TestBitcoinBlockHashShape(t *testing.T) {
	if !validBitcoinBlockHash(testAnchorA) {
		t.Error("a well-formed hash was refused")
	}
	bad := map[string]string{
		"too short":        testAnchorA[:63],
		"too long":         testAnchorA + "0",
		"not hex":          testAnchorA[:63] + "g",
		"upper case":       strings.ToUpper(testAnchorA),
		"no leading zeros": "a" + testAnchorA[1:],
		"too few zeros":    "000000000000000a" + testAnchorA[16:],
		"empty":            "",
		"a CVN block hash": "50371f1749e88e38d339bdfa21bf40d8a96f4997bd7c3a68335bed6344de8369",
		"with spaces":      " " + testAnchorA[1:],
		"a sentence":       "the hash of bitcoin block one million, as soon as it is known!!",
	}
	for name, value := range bad {
		if validBitcoinBlockHash(value) {
			t.Errorf("%s was accepted as a Bitcoin block hash: %q", name, value)
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
	for _, expected := range []string{"1000000", "--genesis-anchor", "genesis_anchor_launch-test.txt"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("the refusal should tell the operator about %q: %v", expected, err)
		}
	}

	if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", "not-a-hash"}); err == nil {
		t.Error("nonsense was accepted as the anchor")
	}
	Params.Anchor.BitcoinHash = ""
	if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", strings.ToUpper(testAnchorA)}); err != nil || Params.Anchor.BitcoinHash != testAnchorA {
		t.Errorf("an anchor given on the command line was not taken: %v", err)
	}

	// The same from the anchor file, with the stray spaces and line ending a
	// person would leave in it.
	Params.Anchor.BitcoinHash = ""
	if err := os.WriteFile("genesis_anchor_launch-test.txt", []byte("  "+testAnchorB+"\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := resolveGenesisAnchor([]string{"cvn_node"}); err != nil || Params.Anchor.BitcoinHash != testAnchorB {
		t.Errorf("an anchor in the file was not taken: %v", err)
	}
	// A hash released with the software needs nothing from the operator, and
	// what the operator types cannot override it.
	Params.Anchor.BitcoinHash = testAnchorA
	if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", testAnchorB}); err != nil || Params.Anchor.BitcoinHash != testAnchorA {
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
		t.Error("testnet-1's genesis block must stay exactly as it is")
	}
	onNetwork(t, "devnet")
	if !strings.Contains(genesisStatement(), "no anchor") {
		t.Errorf("devnet's genesis block should say plainly that it has no anchor: %q", genesisStatement())
	}
}

// The anchor decides the genesis block, and the genesis block decides the
// RandomX key for the first blocks. So before the anchor exists, nobody can
// build the genesis block, mine on it, or even prepare to.
func TestAnchorDecidesTheGenesisBlockAndTheFirstKey(t *testing.T) {
	onNetwork(t, "devnet")
	asLaunchNetwork()

	Params.Anchor.BitcoinHash = testAnchorA
	genesisA := buildGenesisBlock()
	Params.Anchor.BitcoinHash = testAnchorB
	genesisB := buildGenesisBlock()

	if genesisA.Hash == genesisB.Hash {
		t.Fatal("two different anchors gave the same genesis block")
	}
	if !strings.Contains(genesisA.GuardMatrix, testAnchorA) || !strings.Contains(genesisA.GuardMatrix, "Bitcoin block 1000000") {
		t.Errorf("the anchor is not written in the genesis header: %q", genesisA.GuardMatrix)
	}
	if genesisA.Hash != CalculateHash(genesisA) {
		t.Error("the genesis identifier does not cover its header")
	}
	altered := genesisA
	altered.GuardMatrix = strings.Replace(altered.GuardMatrix, testAnchorA, testAnchorB, 1)
	if CalculateHash(altered) == genesisA.Hash {
		t.Error("the anchor can be changed without changing the genesis identifier")
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
		Params.Anchor.BitcoinHash = testAnchorA
	})
	genesis := GetBlockByHeightFromDB(0)
	if !strings.Contains(genesis.GuardMatrix, testAnchorA) || genesis.Hash != buildGenesisBlock().Hash {
		t.Fatalf("the stored genesis block does not carry the anchor: %q", genesis.GuardMatrix)
	}
	if err := checkStoredGenesis(); err != nil {
		t.Fatalf("a database was refused on its own network: %v", err)
	}

	// Nobody can date a block before the genesis block.
	early := mineTestBlock(t, Params.GenesisTimestamp, "CVN_miner", nil, 0)
	if ValidateBlockSignatures(early) {
		t.Error("a block dated no later than the genesis block was accepted")
	}
	block := extendTestChain(t, Params.GenesisTimestamp+60, "CVN_miner", nil)
	if block.PrevHash != genesis.Hash || balanceOf("CVN_miner") != cvn("50") {
		t.Error("the first block does not build on the anchored genesis block")
	}

	// Started again with a different anchor, the node must refuse the
	// database and not quietly carry on with the wrong chain.
	Params.Anchor.BitcoinHash = testAnchorB
	err := checkStoredGenesis()
	if err == nil {
		t.Fatal("a database from one anchor was accepted under another")
	}
	if !strings.Contains(err.Error(), genesis.Hash) || !strings.Contains(err.Error(), filepath.Base(BlockchainFile)) {
		t.Errorf("the refusal should name the database and both genesis blocks: %v", err)
	}
}
