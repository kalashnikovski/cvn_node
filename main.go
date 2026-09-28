package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

type Transaction struct {
	Sender    string    `json:"sender"`
	Recipient string    `json:"recipient"`
	Amount    float64   `json:"amount"`
	Witness   string    `json:"witness"`
	Timestamp time.Time `json:"timestamp"`
}

type Block struct {
	Index        int64         `json:"index"`
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     string        `json:"prev_hash"`
	Hash         string        `json:"hash"`
	Nonce        int64         `json:"nonce"`
	Difficulty   int64         `json:"difficulty"`
}

func CalculateHash(b Block) string {
	record := fmt.Sprintf("%d%d%s%s%d%d", b.Index, b.Timestamp, fmt.Sprintf("%v", b.Transactions), b.PrevHash, b.Nonce, b.Difficulty)
	h := sha256.New()
	h.Write([]byte(record))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func CreateGenesisBlock() Block {
	genesisTx := Transaction{
		Sender:    "GENESIS_VOID_REWARD_POOL",
		Recipient: "COVENANT_STEWARD_ASSEMBLY",
		Amount:    2100000000.0,
		Witness:   "ROOT_PEER_WITNESS_GATEWAY",
		Timestamp: time.Unix(1790640000, 0),
	}
	genesisBlock := Block{
		Index:        0,
		Timestamp:    time.Unix(1790640000, 0).Unix(),
		Transactions: []Transaction{genesisTx},
		PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
		Nonce:        0,
		Difficulty:   100000,
	}
	genesisBlock.Hash = CalculateHash(genesisBlock)
	return genesisBlock
}

func MineBlock(prevBlock Block, txs []Transaction) Block {
	var newBlock Block
	newBlock.Index = prevBlock.Index + 1
	newBlock.Timestamp = time.Now().Unix()
	newBlock.Transactions = txs
	newBlock.PrevHash = prevBlock.Hash
	newBlock.Difficulty = prevBlock.Difficulty
	newBlock.Nonce = 0

	fmt.Printf("\n⚒️  Proof-of-Diligence Active: Mining Block %d... (Press Ctrl+C to stop)\n", newBlock.Index)
	
	for {
		newBlock.Hash = CalculateHash(newBlock)
		if newBlock.Hash[:4] == "0000" {
			fmt.Printf("🎉 BLOCK SOLVED! Nonce: %d | Hash: %s\n", newBlock.Nonce, newBlock.Hash)
			break
		}
		newBlock.Nonce++
	}
	return newBlock
}

func main() {
	fmt.Println("====================================================")
	fmt.Println("💎 COVENANT STANDARD (CVN) CONTINUOUS MINING RIG")
	fmt.Println("====================================================\n")

	// Initialize the ledger with the Genesis Block baseline entry
	blockchain := []Block{CreateGenesisBlock()}
	currentBlock := blockchain[0]

	// The Infinite Mining Loop: This structural layer keeps mining blocks continuously until stopped manually
	for {
		// Simulating an incoming automated transaction block packet for each new block height
		pendingTransactions := []Transaction{
			{
				Sender:    "COVENANT_STEWARD_ASSEMBLY",
				Recipient: "Nikola_Continuous_Steward_Node",
				Amount:    50.0,
				Witness:   "Communal_Peer_Witness_7",
				Timestamp: time.Now(),
			},
		}

		// Mine the next block sequentially using the prior block's cryptographic hash signature
		newBlock := MineBlock(currentBlock, pendingTransactions)
		
		// Serialize and log the newly anchored block metadata state
		blockJSON, _ := json.MarshalIndent(newBlock, "", "  ")
		fmt.Println(string(blockJSON))
		fmt.Println("-----------------------------------------------------")

		// Update the blockchain index pointers to immediately target the next block height
		blockchain = append(blockchain, newBlock)
		currentBlock = newBlock

		// Introduce a tiny 1-second pause to prevent your processor from locking your desktop UI threads
		time.Sleep(1 * time.Second)
	}
}