package node

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"

	"go.etcd.io/bbolt"
)

// THE GENESIS BLOCK AND ITS ANCHOR
//
// A fair launch has to be able to show that nobody mined before everyone
// could. The genesis block of a launch network therefore carries something
// that did not exist until the moment of launch: one particular Bitcoin
// block, named by its height when the launch is announced, days before that
// block is mined. Nobody can know a future Bitcoin block, so nobody can build
// this genesis block, or anything on top of it, early. Anyone can check the
// anchor against the Bitcoin chain at any later time.
//
// The anchor gives the genesis block two things:
//
//   - Its identity. The Bitcoin block's hash is written into the genesis
//     header, so it decides the genesis identifier and, with it, the RandomX
//     key for the first blocks: even preparing to mine is impossible before
//     the anchor exists.
//   - Its date. The genesis block is dated with the Bitcoin block's own
//     timestamp, so the chain's clock starts at the launch and not at some
//     date written into the software weeks earlier. Without this the first
//     miner could date the first blocks far in the past, and the difficulty
//     rule, seeing days pass between blocks, would drop to its minimum.
//
// Both come from the Bitcoin block's 80-byte header, which is what an
// operator supplies. The header proves itself: its hash must meet the
// difficulty it states, which nobody can fake without Bitcoin's own mining
// power.
//
// To give no one a head start on launch day, not even whoever publishes the
// software, a network is released with the Bitcoin height fixed and the
// header still open. When the Bitcoin block appears, every operator supplies
// its header themselves (it is public the instant the block is mined) and
// starts at the same moment:
//
//	cvn_node --network NAME --genesis-anchor HEADER
//
// or by putting the header in the file genesis_anchor_NAME.txt. Nodes started
// with a different header build a different genesis block and simply never
// meet the real network. Later releases carry the header, and nobody has to
// supply it again.
//
// testnet-1 began before this existed. Its genesis block has no anchor and is
// left exactly as it is.

// GenesisAnchor describes how a network's genesis block is tied to a moment
// in time.
type GenesisAnchor struct {
	// BitcoinHeight is the height of the Bitcoin block the genesis block is
	// anchored to. It is announced before that block is mined.
	BitcoinHeight int64
	// BitcoinHeader is that block's header: 160 hex digits. Empty means the
	// block had not been mined when this software was released, and the
	// header must be supplied at first start.
	BitcoinHeader string
	// Unscheduled marks a launch network whose Bitcoin height has not been
	// announced yet. Such a network cannot be started at all.
	Unscheduled bool
	// Placeholder marks a development network whose genesis block carries a
	// statement in place of a real anchor. Such a network proves nothing
	// about when it started and must never carry value.
	Placeholder string

	// BitcoinHash is the block's hash, worked out from the header.
	BitcoinHash string
}

// required reports whether this network needs a real anchor.
func (a GenesisAnchor) required() bool { return a.BitcoinHeight > 0 || a.Unscheduled }

// bitcoinAnchorMinZeroDigits is how many zero hex digits a Bitcoin block hash
// must begin with to be accepted as an anchor. Every Bitcoin block for many
// years has had more; a header with less cannot be a current Bitcoin block.
const bitcoinAnchorMinZeroDigits = 16

// parseBitcoinHeader checks an 80-byte Bitcoin block header given as hex and
// returns the block's hash, as Bitcoin explorers show it, and its timestamp.
func parseBitcoinHeader(headerHex string) (hash string, timestamp int64, err error) {
	headerHex = strings.ToLower(strings.TrimSpace(headerHex))
	if len(headerHex) != 160 {
		return "", 0, fmt.Errorf("a Bitcoin block header is 160 hex digits; this has %d", len(headerHex))
	}
	header, decodeErr := hex.DecodeString(headerHex)
	if decodeErr != nil {
		return "", 0, fmt.Errorf("a Bitcoin block header is 160 hex digits; this contains other characters")
	}
	first := sha256.Sum256(header)
	second := sha256.Sum256(first[:])
	// Bitcoin shows a hash with its bytes in reverse order.
	reversed := make([]byte, len(second))
	for i, b := range second {
		reversed[len(second)-1-i] = b
	}
	hash = hex.EncodeToString(reversed)

	// The header states its own target in a compact form; the hash, read as
	// a number, must not exceed it.
	bits := binary.LittleEndian.Uint32(header[72:76])
	exponent := uint(bits >> 24)
	mantissa := new(big.Int).SetUint64(uint64(bits & 0x007fffff))
	if bits&0x00800000 != 0 || mantissa.Sign() == 0 || exponent < 3 || exponent > 32 {
		return "", 0, fmt.Errorf("this is not a valid Bitcoin block header")
	}
	target := mantissa.Lsh(mantissa, 8*(exponent-3))
	if new(big.Int).SetBytes(reversed).Cmp(target) > 0 {
		return "", 0, fmt.Errorf("this header does not carry the proof of work it claims, so it is not a Bitcoin block header (check for a copying error)")
	}
	if !strings.HasPrefix(hash, strings.Repeat("0", bitcoinAnchorMinZeroDigits)) {
		return "", 0, fmt.Errorf("this header carries far less work than a current Bitcoin block (its hash is %s)", hash)
	}
	return hash, int64(binary.LittleEndian.Uint32(header[68:72])), nil
}

func genesisAnchorFileName() string {
	return "genesis_anchor_" + Params.Name + ".txt"
}

// useAnchorHeader makes a checked Bitcoin header this network's anchor: it
// fixes the anchor's hash and dates the genesis block.
func useAnchorHeader(headerHex string) error {
	hash, timestamp, err := parseBitcoinHeader(headerHex)
	if err != nil {
		return err
	}
	Params.Anchor.BitcoinHeader = strings.ToLower(strings.TrimSpace(headerHex))
	Params.Anchor.BitcoinHash = hash
	Params.GenesisTimestamp = timestamp
	return nil
}

// resolveGenesisAnchor makes sure the anchor is known before a genesis block
// is built. It looks, in order, at the header released with the software, at
// --genesis-anchor on the command line, and at the anchor file.
func resolveGenesisAnchor(args []string) error {
	anchor := &Params.Anchor
	if !anchor.required() {
		return nil
	}
	if anchor.BitcoinHeight <= 0 {
		return fmt.Errorf("the %s network has not been scheduled yet.\n"+
			"   Its start will be announced as a Bitcoin block height, in a new version of this software", Params.Name)
	}
	supplied := anchor.BitcoinHeader
	if supplied == "" {
		supplied = argumentValue(args, "--genesis-anchor")
	}
	if supplied == "" {
		if raw, err := os.ReadFile(genesisAnchorFileName()); err == nil {
			supplied = string(raw)
		}
	}
	if strings.TrimSpace(supplied) == "" {
		return fmt.Errorf("the %s network starts from Bitcoin block %d, and this software was released before that block was mined.\n"+
			"   Once it exists, get its header (160 hex digits). Find the block on a Bitcoin block explorer,\n"+
			"   copy its hash, and open  https://mempool.space/api/block/HASH/header  with that hash.\n"+
			"   Then start with:\n"+
			"       --network %s --genesis-anchor HEADER\n"+
			"   or put the header in a file named %s", Params.Name, anchor.BitcoinHeight, Params.Name, genesisAnchorFileName())
	}
	if err := useAnchorHeader(supplied); err != nil {
		return fmt.Errorf("the anchor for Bitcoin block %d was not accepted: %v", anchor.BitcoinHeight, err)
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
		fmt.Printf(" That block's header:       %s\n", anchor.BitcoinHeader)
		fmt.Println(" To check it yourself, look up that block on any Bitcoin block")
		fmt.Println(" explorer and compare the hash and the time. This network's genesis")
		fmt.Println(" block could not have been built before that Bitcoin block was mined.")
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
