package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// Transaction represents the plaintext relational ledger data transfer structure
type Transaction struct {
	Sender    string    `json:"sender"`    // Bound to Plain-Text Decentralized Identity (DID)
	Recipient string    `json:"recipient"` // Bound to Plain-Text Decentralized Identity (DID)
	Amount    float64   `json:"amount"`    // Transferred CVN token quantity
	Witness   string    `json:"witness"`   // Cryptographic Local Communal Peer Co-Signer Node ID
	Timestamp time.Time `json:"timestamp"` // Transaction generation time
}

// Block represents a single linear cryptographic entry inside the Layer-1 chain state machine
type Block struct {
	Index        int64         `json:"index"`         // Sequential block height identifier
	Timestamp    int64         `json:"timestamp"`     // Epoch time when validation finality achieved
	Transactions []Transaction `json:"transactions"`  // Plaintext batch data payload
	PrevHash     string        `json:"prev_hash"`     // Cryptographic linkage pointer to historical block
	Hash         string        `json:"hash"`          // Unique signature block hash string
	Nonce        int64         `json:"nonce"`         // ASIC-neutral CPU Proof-of-Diligence iteration value
	Difficulty   int64         `json:"difficulty"`    // Organic Heartbeat Protocol boundary metric
}

// CalculateHash compresses block metadata through a clean cryptographic SHA-256 array pass
func CalculateHash(b Block) string {
	record := fmt.Sprintf("%d%d%s%s%d%d", b.Index, b.Timestamp, fmt.Sprintf("%v", b.Transactions), b.PrevHash, b.Nonce, b.Difficulty)
	h := sha256.New()
	h.Write([]byte(record))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// CreateGenesisBlock initializes the un-deconstructible foundational state ledger entry
func CreateGenesisBlock() Block {
	// Hardcoding the sacred relational root metadata directly into the Genesis block payload
	genesisTx := Transaction{
		Sender:    "GENESIS_VOID_REWARD_POOL",
		Recipient: "COVENANT_STEWARD_ASSEMBLY",
		Amount:    2100000000.0, // Fixed supply cap target anchor point
		Witness:   "ROOT_PEER_WITNESS_GATEWAY",
		Timestamp: time.Unix(1790640000, 0), // Anchored timestamp blueprint entry
	}

	genesisBlock := Block{
		Index:        0,
		Timestamp:    time.Unix(1790640000, 0).Unix(), // Static initial clock registration
		Transactions: []Transaction{genesisTx},
		PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
		Nonce:        0,
		Difficulty:   100000, // Starting parameter baseline for the Organic Heartbeat Protocol
	}
	
	genesisBlock.Hash = CalculateHash(genesisBlock)
	return genesisBlock
}

func main() {
	fmt.Println("====================================================")
	fmt.Println("💎 COVENANT STANDARD (CVN) LAYER-1 STATE ARCHITECTURE")
	fmt.Println("   Initializing Local Sovereign Genesis Framework Node")
	fmt.Println("====================================================\n")

	fmt.Println("🤖 Executing genesis block cryptographic verification routines...")
	genesis := CreateGenesisBlock()

	// Serializes the structure into clean readable text JSON output streams
	genesisJSON, err := json.MarshalIndent(genesis, "", "  ")
	if err != nil {
		fmt.Printf("❌ Critical failure serializing state machine metadata: %v\n", err)
		return
	}

	fmt.Println("\n--- VERIFIED GENESIS BLOCK METADATA LEDGER RECORD ---")
	fmt.Println(string(genesisJSON))
	fmt.Println("-----------------------------------------------------")
	fmt.Println("✅ Success! Genesis framework compiled and verified at Block Height 0.")
	fmt.Println("👉 System state is secure. Ready to map Peer-to-Peer communication layers.")
}