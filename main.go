package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

const BlockchainFile = "ledger_vault.json"

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

func SaveChain(chain []Block) {
	data, err := json.MarshalIndent(chain, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(BlockchainFile, data, 0644)
}

func LoadChain() []Block {
	if _, err := os.Stat(BlockchainFile); os.IsNotExist(err) {
		chain := []Block{CreateGenesisBlock()}
		SaveChain(chain)
		return chain
	}
	data, err := os.ReadFile(BlockchainFile)
	if err != nil {
		return []Block{CreateGenesisBlock()}
	}
	var chain []Block
	_ = json.Unmarshal(data, &chain)
	return chain
}

func GetAddressBalance(chain []Block, address string) float64 {
	var balance float64 = 0.0
	for _, block := range chain {
		for _, tx := range block.Transactions {
			if tx.Recipient == address {
				balance += tx.Amount
			}
			if tx.Sender == address {
				balance -= tx.Amount
			}
		}
	}
	return balance
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

// StartTCPServer handles concurrent incoming connections on a separate background thread
func StartTCPServer() {
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Printf("⚠️  TCP Server Error: Failed to bind to Port 8080: %v\n", err)
		return
	}
	defer listener.Close()

	fmt.Println("📡 Native TCP Subnetwork Initialized. Listening continuously on Port :8080...")

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		// Spin up a concurrent GoRoutine worker to handle each peer connection instantly
		go func(c net.Conn) {
			defer c.Close()
			scanner := bufio.NewScanner(c)
			for scanner.Scan() {
				fmt.Printf("\n🌐 [P2P Network Alert] Received data from remote node peer: %s\n", scanner.Text())
			}
		}(conn)
	}
}

func main() {
	fmt.Println("====================================================")
	fmt.Println("💎 COVENANT STANDARD (CVN) HARDENED LEDGER RIG")
	fmt.Println("====================================================\n")

	blockchain := LoadChain()
	currentBlock := blockchain[len(blockchain)-1]

	fmt.Printf("📂 Local Ledger Loaded. Active Block Height: %d\n", currentBlock.Index)
	fmt.Printf("💰 Initial Wallet Balance: %.2f CVN\n", GetAddressBalance(blockchain, "Nikola_Continuous_Steward_Node"))

	// Launching the Native TCP Server concurrently using a custom background GoRoutine thread
	go StartTCPServer()

	// Wait briefly to allow the socket to bind smoothly before starting mining logs
	time.Sleep(500 * time.Millisecond)

	for {
		pendingTransactions := []Transaction{
			{
				Sender:    "COVENANT_STEWARD_ASSEMBLY",
				Recipient: "Nikola_Continuous_Steward_Node",
				Amount:    50.0,
				Witness:   "Communal_Peer_Witness_7",
				Timestamp: time.Now(),
			},
		}

		newBlock := MineBlock(currentBlock, pendingTransactions)
		blockchain = append(blockchain, newBlock)
		currentBlock = newBlock

		SaveChain(blockchain)

		nikolaBalance := GetAddressBalance(blockchain, "Nikola_Continuous_Steward_Node")
		fmt.Printf("💰 WALLET AUDIT: [Nikola_Continuous_Steward_Node] Balance: %.2f CVN\n", nikolaBalance)
		fmt.Println("-----------------------------------------------------")

		time.Sleep(1 * time.Second)
	}
}