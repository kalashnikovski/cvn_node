package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"go.etcd.io/bbolt"
)

// THE GENESIS BLOCK AND ITS ANCHOR
//
// A fair launch has to be able to show that nobody mined before everyone
// could. The genesis block of a launch network therefore carries a value that
// did not exist until the moment of launch: the hash of one particular
// Bitcoin block, named by its height when the launch is announced, days
// before that block is mined. Nobody can know a future Bitcoin block's hash,
// so nobody can build this genesis block, or anything on top of it, early.
// Anyone can check the anchor against the Bitcoin chain at any later time.
//
// The anchor is part of the genesis block's header, so it decides the genesis
// block's identifier, and with it the RandomX key for the first blocks: even
// preparing to mine is impossible before the anchor exists.
//
// To give no one a head start on launch day, not even whoever publishes the
// software, a network can be released with the Bitcoin height fixed and the
// hash still open. When the Bitcoin block appears, every operator supplies its
// hash themselves (it is public the instant it is mined) and starts at the
// same moment:
//
//	cvn_node --network NAME --genesis-anchor HASH
//
// or by putting the hash in the file genesis_anchor_NAME.txt. Nodes started
// with the wrong hash build a different genesis block and simply never meet
// the real network. Later releases carry the hash, and nobody has to supply
// it again.
//
// testnet-1 began before this existed. Its genesis block has no anchor and is
// left exactly as it is.

// GenesisAnchor describes how a network's genesis block is tied to a moment
// in time.
type GenesisAnchor struct {
	// BitcoinHeight is the height of the Bitcoin block whose hash is embedded.
	// It is announced before that block is mined.
	BitcoinHeight int64
	// BitcoinHash is that block's hash: 64 hex digits. Empty means the block
	// had not been mined when this software was released, and the hash must
	// be supplied at first start.
	BitcoinHash string
	// Placeholder marks a development network whose genesis block carries a
	// statement in place of a real anchor. Such a network proves nothing
	// about when it started and must never carry value.
	Placeholder string
}

// required reports whether this network needs a real anchor.
func (a GenesisAnchor) required() bool { return a.BitcoinHeight > 0 }

// validBitcoinBlockHash accepts 64 lower-case hex digits that begin with at
// least sixteen zeros. Every Bitcoin block hash for many years has begun with
// more than that, so this catches typing errors and pasted nonsense.
func validBitcoinBlockHash(hash string) bool {
	if len(hash) != 64 || !strings.HasPrefix(hash, "0000000000000000") {
		return false
	}
	for _, c := range hash {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func genesisAnchorFileName() string {
	return "genesis_anchor_" + Params.Name + ".txt"
}

// resolveGenesisAnchor makes sure the anchor hash is known before a genesis
// block is built. It looks, in order, at the hash released with the software,
// at --genesis-anchor on the command line, and at the anchor file.
func resolveGenesisAnchor(args []string) error {
	anchor := &Params.Anchor
	if !anchor.required() {
		return nil
	}
	if anchor.BitcoinHash == "" {
		supplied := ""
		for i, arg := range args {
			if arg == "--genesis-anchor" && i+1 < len(args) {
				supplied = args[i+1]
			} else if strings.HasPrefix(arg, "--genesis-anchor=") {
				supplied = strings.TrimPrefix(arg, "--genesis-anchor=")
			}
		}
		if supplied == "" {
			if raw, err := os.ReadFile(genesisAnchorFileName()); err == nil {
				supplied = string(raw)
			}
		}
		anchor.BitcoinHash = strings.ToLower(strings.TrimSpace(supplied))
	}
	if anchor.BitcoinHash == "" {
		return fmt.Errorf("the %s network starts from Bitcoin block %d, and this software was released before that block was mined.\n"+
			"   Once it exists, look up its hash on any Bitcoin block explorer and start with:\n"+
			"       --network %s --genesis-anchor HASH\n"+
			"   or put the hash in a file named %s", Params.Name, anchor.BitcoinHeight, Params.Name, genesisAnchorFileName())
	}
	if !validBitcoinBlockHash(anchor.BitcoinHash) {
		return fmt.Errorf("%q is not a Bitcoin block hash (64 hex digits beginning with zeros). Check it against Bitcoin block %d", anchor.BitcoinHash, anchor.BitcoinHeight)
	}
	return nil
}

// genesisStatement is the text written into the genesis block's header. It
// is empty for testnet-1, whose genesis block predates anchors.
func genesisStatement() string {
	anchor := Params.Anchor
	switch {
	case anchor.required():
		return fmt.Sprintf("CVN genesis | network %s | Bitcoin block %d | %s", Params.Name, anchor.BitcoinHeight, anchor.BitcoinHash)
	case anchor.Placeholder != "":
		return fmt.Sprintf("CVN genesis | network %s | %s", Params.Name, anchor.Placeholder)
	default:
		return ""
	}
}

// buildGenesisBlock constructs this network's genesis block. It pays nothing
// to anyone.
func buildGenesisBlock() Block {
	genesis := Block{
		Version:   1,
		Index:     0,
		Timestamp: Params.GenesisTimestamp,
		Transactions: []Transaction{
			{
				Version:   1,
				ID:        "TX_GENESIS_FAIR_LAUNCH_POOL",
				Witness:   "COVENANT_STEWARD_ASSEMBLY",
				PublicKey: "GENESIS_VOID_REWARD_POOL",
				Inputs:    []UTXOInput{},
				Outputs: []UTXOOutput{
					{Recipient: "COVENANT_STEWARD_ASSEMBLY", Amount: UnitsAmount(0)},
				},
				Timestamp: Params.GenesisTimestamp,
				Signature: "COINBASE_IMMUTABLE_CONSENSUS_VALID",
			},
		},
		PrevHash:    "0000000000000000000000000000000000000000000000000000000000000000",
		Difficulty:  2,
		Nonce:       0,
		GuardMatrix: genesisStatement(),
		UncleHash:   "0000000000000000000000000000000000000000000000000000000000000000",
		UncleMiner:  "NONE",
	}
	genesis.Hash = CalculateHash(genesis)
	return genesis
}

// checkStoredGenesis confirms that a database already on disk belongs to the
// network and anchor this process was started with. Mixing them would leave a
// node quietly following a chain nobody else is on.
func checkStoredGenesis() error {
	if GlobalBoltEngine == nil {
		return nil
	}
	var stored Block
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		if bBlocks := tx.Bucket(blocksBucket); bBlocks != nil {
			if data := bBlocks.Get(chainWorkKey(0)); data != nil {
				_ = json.Unmarshal(data, &stored)
			}
		}
		return nil
	})
	if expected := buildGenesisBlock(); stored.Hash != "" && stored.Hash != expected.Hash {
		return fmt.Errorf("the database %s was started from a different genesis block than this network and anchor give.\n"+
			"   In the database:  %s\n"+
			"   Expected here:    %s\n"+
			"   Use the same --network and --genesis-anchor as before, or move that file away to start afresh", BlockchainFile, stored.Hash, expected.Hash)
	}
	return nil
}

// ShowGenesis prints what identifies this network's starting point, so that
// anyone can check it for themselves.
func ShowGenesis() {
	fmt.Println("====================================================================")
	fmt.Printf(" CVN network: %s\n", Params.Name)
	fmt.Printf(" %s\n", Params.Description)
	fmt.Println("====================================================================")
	anchor := Params.Anchor
	switch {
	case anchor.required():
		fmt.Printf(" Anchored to Bitcoin block: %d\n", anchor.BitcoinHeight)
		fmt.Printf(" That block's hash:         %s\n", anchor.BitcoinHash)
		fmt.Println(" To check it yourself, look up that block on any Bitcoin block")
		fmt.Println(" explorer and compare the hash. This network's genesis block could")
		fmt.Println(" not have been built before that Bitcoin block was mined.")
	case anchor.Placeholder != "":
		fmt.Printf(" No anchor: %s\n", anchor.Placeholder)
	default:
		fmt.Println(" No anchor: this network began before anchors were introduced.")
	}
	genesis := buildGenesisBlock()
	fmt.Println("--------------------------------------------------------------------")
	fmt.Printf(" Genesis block identifier: %s\n", genesis.Hash)
	fmt.Printf(" Genesis block date:       %d (seconds since 1 January 1970, UTC)\n", genesis.Timestamp)
	fmt.Printf(" Genesis block pays:       %s CVN\n", FormatUnits(genesis.Transactions[0].Outputs[0].Amount.Units()))
	if statement := genesisStatement(); statement != "" {
		fmt.Printf(" Written in its header:    %s\n", statement)
	}
	fmt.Println("====================================================================")
}
