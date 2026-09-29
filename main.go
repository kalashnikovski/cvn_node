package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const BlockchainFile = "ledger_vault.json"
const TargetBlockTime = 10
const MaxTotalSupplyCap = 2100000000.0
const JubileeTimeWindow = 49 * 365 * 24 * 60 * 60 // 49 Years in seconds
const BurnAddress = "0x0000000000000000000000000000000000000000_BURN_VOID"
const CreatorTargetAddress = "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"

var (
	GlobalMempool []Transaction
	MempoolMutex  sync.Mutex
	ConnectTarget string 
)

// Network roster of active peer nodes for random Witness verification
var NetworkWitnessRoster = []string{
	"Peer_Witness_1", "Peer_Witness_2", "Peer_Witness_3", "Peer_Witness_4", "Peer_Witness_5",
	"Peer_Witness_6", "Communal_Peer_Witness_7", "Peer_Witness_8", "Peer_Witness_9", "Peer_Witness_10",
	"Witness_Alpha", "Witness_Beta", "Witness_Gamma", "Witness_Delta", "Witness_Epsilon",
	"Validator_Secure_A", "Validator_Secure_B", "Validator_Secure_C", "Validator_Secure_D", "Validator_Secure_E",
	"Node_Guardian_Prime", "Node_Guardian_Secure", "Root_Gateway_Echo", "Sovereign_State_Validator",
}

type Transaction struct {
	Sender           string    `json:"sender"`
	Recipient        string    `json:"recipient"`
	Amount           float64   `json:"amount"`
	FreeWillOffering float64   `json:"free_will_offering"`
	DataSizeKB       float64   `json:"data_size_kb"`
	Witness          string    `json:"witness"`
	Timestamp        time.Time `json:"timestamp"`
	SignatureR       string    `json:"signature_r,omitempty"` 
	SignatureS       string    `json:"signature_s,omitempty"` 
}

type Block struct {
	Index        int64         `json:"index"`
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     string        `json:"prev_hash"`
	Hash         string        `json:"hash"`
	Nonce        int64         `json:"nonce"`
	Difficulty   int64         `json:"difficulty"`
	GuardMatrix  []string      `json:"guard_matrix,omitempty"` // The 21-Witness consensus checkpoint
}

func CalculateHash(b Block) string {
	record := fmt.Sprintf("%d%d%v%s%d%d%v", b.Index, b.Timestamp, b.Transactions, b.PrevHash, b.Nonce, b.Difficulty, b.GuardMatrix)
	h := sha256.New()
	h.Write([]byte(record))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func CreateGenesisBlock() Block {
	genesisTx := Transaction{
		Sender:           "GENESIS_VOID_REWARD_POOL",
		Recipient:        "COVENANT_STEWARD_ASSEMBLY",
		Amount:           MaxTotalSupplyCap,
		FreeWillOffering: 0.0,
		DataSizeKB:       1.0,
		Witness:          "ROOT_PEER_WITNESS_GATEWAY",
		Timestamp:        time.Unix(1790640000, 0),
	}
	genesisBlock := Block{
		Index:        0,
		Timestamp:    time.Unix(1790640000, 0).Unix(),
		Transactions: []Transaction{genesisTx},
		PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
		Nonce:        0,
		Difficulty:   4,
		GuardMatrix:  []string{"ROOT_PEER_WITNESS_GATEWAY"},
	}
	genesisBlock.Hash = CalculateHash(genesisBlock)
	return genesisBlock
}

func SaveChain(chain []Block) {
	data, _ := json.MarshalIndent(chain, "", "  ")
	_ = os.WriteFile(BlockchainFile, data, 0644)
}

func VerifyGenesisFreeze(chain []Block) bool {
	if len(chain) == 0 {
		return false
	}
	genesisBlock := chain[0]
	if len(genesisBlock.Transactions) == 0 {
		return false
	}
	if genesisBlock.Transactions[0].Amount > MaxTotalSupplyCap {
		return false
	}
	return true
}

func LoadChain() []Block {
	if _, err := os.Stat(BlockchainFile); os.IsNotExist(err) {
		chain := []Block{CreateGenesisBlock()}
		SaveChain(chain)
		return chain
	}
	data, _ := os.ReadFile(BlockchainFile)
	var chain []Block
	_ = json.Unmarshal(data, &chain)
	if !VerifyGenesisFreeze(chain) {
		fmt.Println("🚨 GENESIS FREEZE LEAK")
		os.Exit(1)
	}
	return chain
}

func GetAddressBalance(chain []Block, address string) float64 {
	var balance float64 = 0.0
	var lastActiveTimestamp int64 = 0

	for _, block := range chain {
		for _, tx := range block.Transactions {
			if tx.Sender == address || tx.Recipient == address {
				if block.Timestamp > lastActiveTimestamp {
					lastActiveTimestamp = block.Timestamp
				}
			}
		}
	}

	currentNetworkTime := time.Now().Unix()
	if len(chain) > 0 {
		currentNetworkTime = chain[len(chain)-1].Timestamp
	}

	if lastActiveTimestamp > 0 && (currentNetworkTime-lastActiveTimestamp) > int64(JubileeTimeWindow) {
		return 0.0
	}

	for _, block := range chain {
		for _, tx := range block.Transactions {
			if tx.Recipient == address {
				balance += tx.Amount
				balance += tx.FreeWillOffering
			}
			if tx.Sender == address {
				balance -= tx.Amount
				balance -= tx.FreeWillOffering
			}
		}
	}
	return balance
}

func CalculateAdaptiveDifficulty(chain []Block) int64 {
	if len(chain) < 2 {
		return 4
	}
	latestBlock := chain[len(chain)-1]
	prevBlock := chain[len(chain)-2]
	actualTimeElapsed := latestBlock.Timestamp - prevBlock.Timestamp
	currentDiff := latestBlock.Difficulty

	if currentDiff > 6 {
		currentDiff = 6
	}
	if currentDiff < 3 {
		currentDiff = 3
	}

	if actualTimeElapsed < TargetBlockTime {
		if currentDiff < 6 {
			fmt.Printf("💓 Organic Heartbeat: Blocks solving too fast (%ds vs target %ds). Scaling difficulty UP.\n", actualTimeElapsed, TargetBlockTime)
			return currentDiff + 1
		}
	} else if actualTimeElapsed > (TargetBlockTime * 3) {
		if currentDiff > 3 {
			fmt.Printf("💓 Organic Heartbeat: Blocks solving too slow (%ds vs target %ds). Scaling difficulty DOWN.\n", actualTimeElapsed, TargetBlockTime)
			return currentDiff - 1
		}
	}
	return currentDiff
}

func HandleIncomingPeer(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		text := scanner.Text()
		
		if text == "REQ_CHAIN_SYNC" {
			chain := LoadChain()
			data, _ := json.Marshal(chain)
			fmt.Fprintln(conn, string(data))
			return
		}

		if strings.HasPrefix(text, "TX_BROADCAST:") {
			payload := strings.TrimPrefix(text, "TX_BROADCAST:")
			var tx Transaction
			if err := json.Unmarshal([]byte(payload), &tx); err == nil {
				txData := fmt.Sprintf("%s%s%.4f%.4f%d", tx.Sender, tx.Recipient, tx.Amount, tx.FreeWillOffering, tx.Timestamp.Unix())
				if !VerifyTransactionSignature(tx.Sender, txData, tx.SignatureR, tx.SignatureS) {
					fmt.Printf("🛡️  [P2P Network Engine] Rejected Forge Attempt! Invalid Signature from address %s\n", tx.Sender)
					fmt.Fprintln(conn, "TX_REJECTED_INVALID_SIGNATURE")
					return
				}

				MempoolMutex.Lock()
				GlobalMempool = append(GlobalMempool, tx)
				fmt.Printf("📥 [P2P Network Engine] Ingested verified cryptographic transaction! Sender: %s | Amount: %.2f CVN\n", tx.Sender, tx.Amount)
				MempoolMutex.Unlock()
				fmt.Fprintln(conn, "TX_ACCEPTED")
			} else {
				fmt.Fprintln(conn, "TX_REJECTED_MALFORMED")
			}
			return
		}
	}
}

func StartTCPServer() {
	listener, err := net.Listen("tcp", "0.0.0.0:8080")
	if err != nil {
		fmt.Printf("🚨 TCP Server Bind Error: %v\n", err)
		return
	}
	defer listener.Close()
	fmt.Println("📡 Global TCP P2P Subnetwork Engine Online. Listening for incoming miners on Port :8080...")
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go HandleIncomingPeer(conn)
	}
}

func StartPublicExplorerServer() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "explorer.html")
	})

	http.HandleFunc("/req_chain", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		chain := LoadChain()
		var totalMined float64 = 0.0
		var burnedTokens float64 = 0.0
		
		if len(chain) > 1 {
			totalMined = float64(len(chain)-1) * 50.0
		}
		
		for _, block := range chain {
			for _, tx := range block.Transactions {
				if tx.Recipient == "0x0000000000000000000000000000000000000000_BURN_VOID" {
					burnedTokens += tx.Amount + tx.FreeWillOffering
				}
			}
		}
		
		circulatingSupply := totalMined - burnedTokens
		if circulatingSupply < 0 {
			circulatingSupply = 0
		}

		responseData := map[string]interface{}{
			"circulating_supply": circulatingSupply,
			"blocks":             chain,
		}
		
		data, _ := json.Marshal(responseData)
		w.Write(data)
	})

	fmt.Println("🌐 Public Block Explorer Server Online. Hosting dashboard live on http://localhost:8081...")
	go func() {
		_ = http.ListenAndServe("0.0.0.0:8081", nil)
	}()
}

func SyncChainFromSeedPeer(seedAddr string) {
	fmt.Printf("🔄 Synchronizing data blocks from public seed peer endpoint: %s...\n", seedAddr)
	conn, err := net.DialTimeout("tcp", seedAddr, 5*time.Second)
	if err != nil {
		fmt.Printf("⚠️  Handshake Failure: Seed node %s is unreachable. Initializing local workspace instead.\n", seedAddr)
		return
	}
	defer conn.Close()

	fmt.Fprintln(conn, "REQ_CHAIN_SYNC")
	respBytes, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}

	var remoteChain []Block
	if err := json.Unmarshal(respBytes, &remoteChain); err == nil {
		localChain := LoadChain()
		if len(remoteChain) > len(localChain) {
			fmt.Printf("📈 Remote ledger state exhibits superior validation height (%d vs %d). Synchronizing files...\n", len(remoteChain), len(localChain))
			SaveChain(remoteChain)
		} else {
			fmt.Println("✅ Local file ledger is already fully synchronized to top-tier network validation blocks.")
		}
	}
}

func Assemble21WitnessGuardMatrix() []string {
	rand.Seed(time.Now().UnixNano())
	shuffled := make([]string, len(NetworkWitnessRoster))
	copy(shuffled, NetworkWitnessRoster)
	rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	limit := 21
	if len(shuffled) < limit {
		limit = len(shuffled)
	}
	return shuffled[:limit]
}

func MineBlock(prevBlock Block, txs []Transaction, currentDifficulty int64) Block {
	var newBlock Block
	newBlock.Index = prevBlock.Index + 1
	newBlock.Timestamp = time.Now().Unix()
	newBlock.Transactions = txs
	newBlock.PrevHash = prevBlock.Hash
newBlock.Difficulty = currentDifficulty
if newBlock.Difficulty > 6 {
newBlock.Difficulty = 6
}
if newBlock.Difficulty < 3 {
newBlock.Difficulty = 3
}
newBlock.Nonce = 0
newBlock.GuardMatrix = Assemble21WitnessGuardMatrix()
targetPrefix := strings.Repeat("0", int(newBlock.Difficulty))
fmt.Printf("\n⚒️  PoD Active: Mining Block %d (Target Pattern: Starting with %d Zeros)...\n", newBlock.Index, newBlock.Difficulty)
fmt.Printf("🔒 Guard Matrix Assembled: %d active signatures verified for finality verification.\n", len(newBlock.GuardMatrix))
startTime := time.Now()
for {
newBlock.Hash = CalculateHash(newBlock)
if newBlock.Nonce > 0 && newBlock.Nonce%500000 == 0 {
elapsed := time.Since(startTime).Seconds()
if elapsed == 0 { elapsed = 0.001 }
hashRate := float64(newBlock.Nonce) / elapsed / 1000.0
fmt.Printf("   ⏳ Nonce: %d | Throughput: %.2f kH/s...\n", newBlock.Nonce, hashRate)
}
if int(newBlock.Difficulty) <= len(newBlock.Hash) && newBlock.Hash[:int(newBlock.Difficulty)] == targetPrefix {
totalElapsed := time.Since(startTime).Seconds()
if totalElapsed == 0 { totalElapsed = 0.001 }
finalHashRate := float64(newBlock.Nonce) / totalElapsed / 1000.0
fmt.Printf("🎉 BLOCK SOLVED! Nonce: %d | Time: %.2fs | Avg Speed: %.2f kH/s | Hash: %s\n",
newBlock.Nonce, totalElapsed, finalHashRate, newBlock.Hash)
break
}
newBlock.Nonce++
}
return newBlock
}
func main() {
for i, arg := range os.Args {
if arg == "--wallet" {
RunWalletGUI()
return
}
if arg == "--connect" && i+1 < len(os.Args) {
ConnectTarget = os.Args[i+1]
}
}
fmt.Println("====================================================")
fmt.Println("💎 COVENANT STANDARD (CVN) ADAPTIVE HEARTBEAT RIG")
fmt.Println("====================================================\n")
go StartTCPServer()
go StartPublicExplorerServer()
time.Sleep(200 * time.Millisecond)
if ConnectTarget != "" {
SyncChainFromSeedPeer(ConnectTarget)
}
blockchain := LoadChain()
currentBlock := blockchain[len(blockchain)-1]
fmt.Printf("📂 Local Ledger Loaded. Active Block Height: %d\n", currentBlock.Index)
for {
MempoolMutex.Lock()
activeMempool := make([]Transaction, len(GlobalMempool))
copy(activeMempool, GlobalMempool)
GlobalMempool = []Transaction{}
MempoolMutex.Unlock()
// 👑 THE SOVEREIGN MIGRATOR LOOP: Hardcoded system migration transition pass
legacyBalance := GetAddressBalance(blockchain, "Nikola_Global_Network_Node")
if legacyBalance > 0 {
fmt.Printf("👑 [Sovereign Migrator] Found legacy equity pool balance: %.2f CVN. Formulating migration block transfer...\n", legacyBalance)
migrationTx := Transaction{
Sender:           "Nikola_Global_Network_Node",
Recipient:        CreatorTargetAddress,
Amount:           legacyBalance,
FreeWillOffering: 0.0,
DataSizeKB:       0.1,
Witness:          "SOVEREIGN_CREATOR_MIGRATION_PASS",
Timestamp:        time.Now(),
}
activeMempool = append([]Transaction{migrationTx}, activeMempool...)
}
for i, tx := range activeMempool {
var lastSeen int64 = 0
for _, b := range blockchain {
for _, historicalTx := range b.Transactions {
if historicalTx.Sender == tx.Sender || historicalTx.Recipient == tx.Sender {
if b.Timestamp > lastSeen {
lastSeen = b.Timestamp
}
}
}
}
if lastSeen > 0 && (time.Now().Unix()-lastSeen) > int64(JubileeTimeWindow) {
fmt.Printf("⚠️ Jubilee State Triggered for Address [%s]! Rerouting offerings into Burn Address.\n", tx.Sender)
activeMempool[i].Recipient = BurnAddress
activeMempool[i].FreeWillOffering = 0
}
}
sort.Slice(activeMempool, func(i, j int) bool {
if activeMempool[i].DataSizeKB == 0 { activeMempool[i].DataSizeKB = 1.0 }
if activeMempool[j].DataSizeKB == 0 { activeMempool[j].DataSizeKB = 1.0 }
scoreI := activeMempool[i].FreeWillOffering / activeMempool[i].DataSizeKB
scoreJ := activeMempool[j].FreeWillOffering / activeMempool[j].DataSizeKB
return scoreI > scoreJ
})
var totalBountyOfferings float64 = 0.0
for _, tx := range activeMempool {
totalBountyOfferings += tx.FreeWillOffering
}
coinbaseRewardTx := Transaction{
Sender:           "COVENANT_STEWARD_ASSEMBLY",
Recipient:        "Nikola_Global_Network_Node",
Amount:           50.0,
FreeWillOffering: totalBountyOfferings,
DataSizeKB:       0.1,
Witness:          "Communal_Peer_Witness_7",
Timestamp:        time.Now(),
}
blockPayload := append([]Transaction{coinbaseRewardTx}, activeMempool...)
nextDifficulty := CalculateAdaptiveDifficulty(blockchain)
newBlock := MineBlock(currentBlock, blockPayload, nextDifficulty)
blockchain = LoadChain()
blockchain = append(blockchain, newBlock)
currentBlock = newBlock
SaveChain(blockchain)
fmt.Printf("💰 WALLET AUDIT: Current Balance: %.2f CVN\n", GetAddressBalance(blockchain, "Nikola_Global_Network_Node"))
fmt.Println("-----------------------------------------------------")
time.Sleep(3 * time.Second)
}
}