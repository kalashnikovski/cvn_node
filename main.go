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

// Hardcoded Seed Node Array - Ensures all nodes worldwide know exactly who to call
var BootstrapSeeds = []string{
	"127.0.0.1:8080", // Local fallback testing address
}

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

// HandleIncomingPeer processes network messages from incoming node connections
func HandleIncomingPeer(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	
	for scanner.Scan() {
		msg := scanner.Text()
		
		// If a new node calls for the blockchain history, stream our ledger file to them
		if msg == "REQ_CHAIN_SYNC" {
			chain := LoadChain()
			data, _ := json.Marshal(chain)
			fmt.Fprintln(conn, string(data))
			fmt.Println("📡 [Network Layer] Successfully synchronized ledger history with remote peer.")
		}
	}
}

// StartTCPServer opens the communication gateway for incoming network miners
func StartTCPServer() {
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Printf("⚠️  TCP Server Error: Failed to open listening socket: %v\n", err)
		return
	}
	defer listener.Close()

	fmt.Println("📡 Global TCP Subnetwork Online. Listening for miners on Port :8080...")
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go HandleIncomingPeer(conn)
	}
}

// ConnectToNetwork attempts to connect to seed nodes to discover and sync with the active network
func ConnectToNetwork() []Block {
	fmt.Println("🔍 Attempting to connect to Bootstrap Seed Nodes...")
	for _, seed := range BootstrapSeeds {
		conn, err := net.DialTimeout("tcp", seed, 2*time.Second)
		if err != nil {
			continue
		}
		defer conn.Close()

		// Requesting the master chain from the anchor node
		fmt.Fprintln(conn, "REQ_CHAIN_SYNC")
		
		var incomingChain []Block
		decoder := json.NewDecoder(conn)
		if err := decoder.Decode(&incomingChain); err == nil {
			fmt.Printf("✅ Connected successfully! Synced chain height: %d blocks from peer [%s]\n", len(incomingChain)-1, seed)
			return incomingChain
		}
	}
	
	fmt.Println("⚠️  No active seeds found. Operating as network anchor node baseline.")
	return LoadChain()
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
	fmt.Println("💎 COVENANT STANDARD (CVN) HARDENED LEDGER RIG")
	fmt.Println("====================================================\n")

	// Launch background listener socket thread
	go StartTCPServer()
	time.Sleep(500 * time.Millisecond)

	// Attempt peer discovery and global chain state synchronization
	blockchain := ConnectToNetwork()
	currentBlock := blockchain[len(blockchain)-1]

	for {
		pendingTransactions := []Transaction{
			{
				Sender:    "COVENANT_STEWARD_ASSEMBLY",
				Recipient: "Nikola_Global_Network_Node",
				Amount:    50.0,
				Witness:   "Communal_Peer_Witness_7",
				Timestamp: time.Now(),
			},
		}

		newBlock := MineBlock(currentBlock, pendingTransactions)
		
		// Refresh local chain state before appending
		blockchain = LoadChain()
		blockchain = append(blockchain, newBlock)
		currentBlock = newBlock

		SaveChain(blockchain)
		fmt.Printf("💰 WALLET AUDIT: Current Balance: %.2f CVN\n", GetAddressBalance(blockchain, "Nikola_Global_Network_Node"))
		fmt.Println("-----------------------------------------------------")

		time.Sleep(1 * time.Second)
	}
}