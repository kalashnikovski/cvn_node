package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

const BlockchainFile = "ledger_vault.json"
const TargetBlockTime = 10 // Target block generation window in seconds for fast simulation

var BootstrapSeeds = []string{
	"127.0.0.1:8080",
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
	Difficulty   int64         `json:"difficulty"` // The number of leading target zeros required
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
		Difficulty:   4, // Starts requiring a prefix of 4 zeros ("0000")
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

// CalculateAdaptiveDifficulty adjusts the target zeros dynamically based on actual network velocity
func CalculateAdaptiveDifficulty(chain []Block) int64 {
	if len(chain) < 2 {
		return 4 // Base difficulty threshold
	}

	latestBlock := chain[len(chain)-1]
	prevBlock := chain[len(chain)-2]
	actualTimeElapsed := latestBlock.Timestamp - prevBlock.Timestamp

	currentDiff := latestBlock.Difficulty

	// The Organic Heartbeat Protocol Logic Loop
	if actualTimeElapsed < TargetBlockTime {
		// Blocks are compiling too fast (High Network Hashrate Influx) -> Make it harder
		fmt.Printf("💓 Organic Heartbeat: Blocks solving too fast (%ds vs target %ds). Scaling difficulty UP.\n", actualTimeElapsed, TargetBlockTime)
		return currentDiff + 1
	} else if actualTimeElapsed > (TargetBlockTime * 3) && currentDiff > 3 {
		// Network hashrate dropped or miners disconnected -> Scale difficulty DOWN to protect block space
		fmt.Printf("💓 Organic Heartbeat: Blocks solving too slow (%ds vs target %ds). Scaling difficulty DOWN.\n", actualTimeElapsed, TargetBlockTime)
		return currentDiff - 1
	}

	return currentDiff
}

func HandleIncomingPeer(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if scanner.Text() == "REQ_CHAIN_SYNC" {
			chain := LoadChain()
			data, _ := json.Marshal(chain)
			fmt.Fprintln(conn, string(data))
		}
	}
}

func StartTCPServer() {
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		return
	}
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go HandleIncomingPeer(conn)
	}
}

func MineBlock(prevBlock Block, txs []Transaction, currentDifficulty int64) Block {
	var newBlock Block
	newBlock.Index = prevBlock.Index + 1
	newBlock.Timestamp = time.Now().Unix()
	newBlock.Transactions = txs
	newBlock.PrevHash = prevBlock.Hash
	newBlock.Difficulty = currentDifficulty
	newBlock.Nonce = 0

	// Dynamically build the required prefix matching string (e.g. 4 -> "0000", 5 -> "00000")
	targetPrefix := strings.Repeat("0", int(newBlock.Difficulty))

	fmt.Printf("\n⚒️  PoD Active: Mining Block %d (Target Pattern: Starting with %d Zeros)...\n", newBlock.Index, newBlock.Difficulty)
	for {
		newBlock.Hash = CalculateHash(newBlock)
		if newBlock.Hash[:int(newBlock.Difficulty)] == targetPrefix {
			fmt.Printf("🎉 BLOCK SOLVED! Nonce: %d | Hash: %s\n", newBlock.Nonce, newBlock.Hash)
			break
		}
		newBlock.Nonce++
	}
	return newBlock
}

func main() {
	fmt.Println("====================================================")
	fmt.Println("💎 COVENANT STANDARD (CVN) ADAPTIVE HEARTBEAT RIG")
	fmt.Println("====================================================\n")

// Command check: If launched with a '--wallet' flag argument, trigger the graphical wrapper instead
	if len(os.Args) > 1 && os.Args[1] == "--wallet" {
		fmt.Println("🎨 Booting Desktop Graphical user dashboard environment interface layers...")
		RunWalletGUI()
		return
	}

	go StartTCPServer()
	time.Sleep(500 * time.Millisecond)

	blockchain := LoadChain()
	currentBlock := blockchain[len(blockchain)-1]

	fmt.Printf("📂 Local Ledger Loaded. Active Block Height: %d\n", currentBlock.Index)

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

		// Calculate the next target difficulty dynamically based on the network heartbeat speed
		nextDifficulty := CalculateAdaptiveDifficulty(blockchain)

		newBlock := MineBlock(currentBlock, pendingTransactions, nextDifficulty)
		
		blockchain = LoadChain()
		blockchain = append(blockchain, newBlock)
		currentBlock = newBlock

		SaveChain(blockchain)
		fmt.Printf("💰 WALLET AUDIT: Current Balance: %.2f CVN\n", GetAddressBalance(blockchain, "Nikola_Global_Network_Node"))
		fmt.Println("-----------------------------------------------------")

		time.Sleep(1 * time.Second)
	}
}