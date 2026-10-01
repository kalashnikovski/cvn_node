package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	GlobalMempool []Transaction
	MempoolMutex  sync.Mutex
	ConnectTarget string 
	
	CustomMinerAddress string = "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337" 
	
	ActivePeerRoster []string

	RosterMutex      sync.Mutex
	LocalListenerIP  string = "202.137.175.220" 
	
	ValidatorStakingPool map[string]float64
	StakingPoolMutex     sync.Mutex

	ActivePeerHeartbeats map[string]int64
	HeartbeatMutex       sync.Mutex
)

// Ironclad Framework Constants matching scriptural protocol definitions
const (
	BlockchainFile         = "ledger_vault.json"
	ProfileConfigFile      = "miner_config.json"
	MaxTotalSupplyCap      = 2100000000.0
	TargetBlockTime        = 10   
	SlasherPenaltyRate     = 0.30 
	JubileeTimeWindow      = 49 * 365 * 24 * 60 * 60 
	RequiredStakingBond    = 500.00
	BurnAddress            = "0x0000000000000000000000000000000000000000_BURN_VOID"
)

// ============================================================================
// 💎 UTXO STATE DATA STRUCTURES
// ============================================================================

type UTXOInput struct {
	TxID      string `json:"tx_id"`      
	OutputIdx int    `json:"output_idx"` 
	Signature string `json:"signature"`  
}

type UTXOOutput struct {
	Recipient string  `json:"recipient"` 
	Amount    float64 `json:"amount"`    
}

type Transaction struct {
	ID               string       `json:"id"`                 
	Inputs           []UTXOInput  `json:"inputs"`             
	Outputs          []UTXOOutput `json:"outputs"`            
	FreeWillOffering float64      `json:"free_will_offering"` 
	DataSizeKB       float64      `json:"data_size_kb"`       
	Witness          string       `json:"witness"`            
	SignatureR       string       `json:"signature_r"`        
	SignatureS       string       `json:"signature_s"`        
}

type Block struct {
	Index        int64         `json:"index"`
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"` 
	PrevHash     string        `json:"prev_hash"`
	Hash         string        `json:"hash"`
	Difficulty   int64         `json:"difficulty"`
	Nonce        int64         `json:"nonce"`
	GuardMatrix  []string      `json:"guard_matrix"`
}

type MinerConfig struct {
	SavedMinerAddress string `json:"saved_miner_address"`
}

func CalculateHash(b Block) string {
	record := fmt.Sprintf("%d%d%v%s%d%d%v", b.Index, b.Timestamp, b.Transactions, b.PrevHash, b.Nonce, b.Difficulty, b.GuardMatrix)
	h := sha256.New()
	h.Write([]byte(record))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func CreateGenesisBlock() Block {
	genesisTx := Transaction{
		ID:     "GENESIS_TX_INITIAL_SUPPLY",
		Inputs: []UTXOInput{}, 
		Outputs: []UTXOOutput{
			{Recipient: "RESERVE_POOL_UNALLOCATED_SUPPLY", Amount: MaxTotalSupplyCap},
		},
		FreeWillOffering: 0.0,
		DataSizeKB:       0.1,
		Witness:          "Sovereign_Genesis_Pass_777",
	}

	genesisBlock := Block{
		Index:        0,
		Timestamp:    time.Now().Unix(),
		Transactions: []Transaction{genesisTx},
		PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
		Difficulty:   1,
		Nonce:        0,
		GuardMatrix:  []string{"ROOT_NODE_GENESIS_CORE"},
	}
	genesisBlock.Hash = CalculateHash(genesisBlock)
	return genesisBlock
}

func VerifyGenesisFreeze(chain []Block) bool {
	if len(chain) == 0 { 
		return false 
	}
	return true
}

func LoadChain() []Block {
	var chain []Block
	data, err := os.ReadFile(BlockchainFile)
	if err != nil {
		chain = append(chain, CreateGenesisBlock())
		SaveChain(chain)
		return chain
	}
	if err := json.Unmarshal(data, &chain); err != nil {
		chain = append(chain, CreateGenesisBlock())
		return chain
	}
	if !VerifyGenesisFreeze(chain) {
		fmt.Println("🚨 CRITICAL CORE PANIC: GENESIS STATE VIOLATION!")
		os.Exit(1)
	}
	return chain
}

func SaveChain(chain []Block) {
	data, err := json.MarshalIndent(chain, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(BlockchainFile, data, 0644)
}

func GetAddressBalance(chain []Block, address string) float64 {
	type UTXOKey struct { TxID string; Idx int }
	unspentMap := make(map[UTXOKey]float64)

	for _, block := range chain {
		for _, tx := range block.Transactions {
			for idx, out := range tx.Outputs {
				if out.Recipient == address {
					unspentMap[UTXOKey{TxID: tx.ID, Idx: idx}] = out.Amount
				}
			}
		}
	}

	for _, block := range chain {
		for _, tx := range block.Transactions {
			for _, in := range tx.Inputs {
				delete(unspentMap, UTXOKey{TxID: in.TxID, Idx: in.OutputIdx})
			}
		}
	}

	var balance float64 = 0.0
	for _, amount := range unspentMap {
		balance += amount
	}
	return balance
}

func CalculateAdaptiveDifficulty(chain []Block) int64 {
	if len(chain) < 2 { return 4 }
	latestBlock := chain[len(chain)-1]
	prevBlock := chain[len(chain)-2]
	actualTimeElapsed := latestBlock.Timestamp - prevBlock.Timestamp
	currentDiff := latestBlock.Difficulty

	if currentDiff > 6 { currentDiff = 6 }
	if currentDiff < 3 { currentDiff = 3 }

	if actualTimeElapsed < TargetBlockTime {
		if currentDiff < 6 { return currentDiff + 1 }
	} else if actualTimeElapsed > (TargetBlockTime * 3) {
		if currentDiff > 3 { return currentDiff - 1 }
	}
	return currentDiff
}

func RegisterGossipPeer(peerAddr string) {
	if peerAddr == "" || strings.HasPrefix(peerAddr, "127.0.0.1") || strings.HasPrefix(peerAddr, "0.0.0.0") {
		return
	}
	RosterMutex.Lock()
	defer RosterMutex.Unlock()
	for _, existing := range ActivePeerRoster {
		if existing == peerAddr { 
			return 
		}
	}
	ActivePeerRoster = append(ActivePeerRoster, peerAddr)
	fmt.Printf("🛰️ Connected new node to routing tables: %s\n", peerAddr)
}

func ExecuteSabbaticalSlash(validatorAddress string, reason string) {
	StakingPoolMutex.Lock()
	defer StakingPoolMutex.Unlock()
	stakedAmount := ValidatorStakingPool[validatorAddress]
	if stakedAmount > 0 {
		seizedBalance := stakedAmount * SlasherPenaltyRate
		ValidatorStakingPool[validatorAddress] = 0 
		fmt.Printf("⚡ [Slasher] MALICIOUS ACT (%s): Slashing %s. Seizing %.2f CVN to Burn Void!\n", reason, validatorAddress, seizedBalance)
	}
}

func HandleIncomingPeer(conn net.Conn) {
	defer conn.Close()
	
	// HARD-FORK FIREWALL: Enforce a strict 15-second lifetime limit per network interaction loop
	// This physically stops slow or dead connections from hanging and exhausting your PC sockets!
	err := conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err != nil {
		return
	}
	
	remoteAddr := conn.RemoteAddr().String()
	HeartbeatMutex.Lock()
	ActivePeerHeartbeats[remoteAddr] = time.Now().Unix()
	HeartbeatMutex.Unlock()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {

		text := scanner.Text()
		if text == "REQ_CHAIN_SYNC" {
			chain := LoadChain()
			data, _ := json.Marshal(chain)
			fmt.Fprintln(conn, string(data))
			return
		}
		if strings.HasPrefix(text, "GOSSIP_PEER_DISCOVERY:") {
			incomingNodeAddress := strings.TrimPrefix(text, "GOSSIP_PEER_DISCOVERY:")
			RegisterGossipPeer(incomingNodeAddress)
			RosterMutex.Lock()
			rosterJSON, _ := json.Marshal(ActivePeerRoster)
			RosterMutex.Unlock()
			fmt.Fprintln(conn, string(rosterJSON))
			return
		}
		if strings.HasPrefix(text, "BLOCK_PROPAGATE:") {
			payload := strings.TrimPrefix(text, "BLOCK_PROPAGATE:")
			ProcessInboundBlock(payload)
			return
		}
				if strings.HasPrefix(text, "TX_BROADCAST:") {
			payload := strings.TrimPrefix(text, "TX_BROADCAST:")
			var tx Transaction
			if err := json.Unmarshal([]byte(payload), &tx); err == nil {
				
								// HARD-FORK FIX: Intercept malformed formats while validating 44-45 char key structures
				isValidAddress := true
				for _, out := range tx.Outputs {
					addressLen := len(out.Recipient)
					if !strings.HasPrefix(out.Recipient, "CVN_") || addressLen < 44 || addressLen > 45 {
						isValidAddress = false
						break
					}
				}


				if !isValidAddress {
					fmt.Println("⚠️  [Security Firewall] Blocked inbound transaction: Malformed recipient signature detected!")
					fmt.Fprintln(conn, "TX_REJECTED_INVALID_ADDRESS")
					return
				}

				MempoolMutex.Lock()
				GlobalMempool = append(GlobalMempool, tx)
				MempoolMutex.Unlock()
				fmt.Fprintln(conn, "TX_ACCEPTED")
			} else {
				fmt.Fprintln(conn, "TX_REJECTED")
			}
			return
		}

	}
}

func BroadcastNewBlock(newBlock Block) {
	RosterMutex.Lock()
	peers := make([]string, len(ActivePeerRoster))
	copy(peers, ActivePeerRoster)
	RosterMutex.Unlock()

	payload, _ := json.Marshal(newBlock)
	broadcastMsg := "BLOCK_PROPAGATE:" + string(payload)

	for _, peer := range peers {
		go func(peerAddr string) {
			conn, err := net.DialTimeout("tcp", peerAddr, 3*time.Second)
			if err != nil { return }
			defer conn.Close()
			fmt.Fprintln(conn, broadcastMsg)
		}(peer)
	}
}

func ProcessInboundBlock(payload string) {
	var remoteBlock Block
	if err := json.Unmarshal([]byte(payload), &remoteBlock); err != nil { return }

	blockchain := LoadChain()
	currentLocalBlock := blockchain[len(blockchain)-1]

	if remoteBlock.Index == currentLocalBlock.Index+1 && remoteBlock.PrevHash == currentLocalBlock.Hash {
		recalculatedHash := CalculateHash(remoteBlock)
		targetPrefix := strings.Repeat("0", int(remoteBlock.Difficulty))
		if !strings.HasPrefix(recalculatedHash, targetPrefix) || remoteBlock.Hash != recalculatedHash { return }
		
		blockchain = append(blockchain, remoteBlock)
		SaveChain(blockchain)
		fmt.Printf("🎉 NEW BLOCK ACCEPTED FROM MESH NETWORK! Height: #%d\n", remoteBlock.Index)
		go BroadcastNewBlock(remoteBlock)
		return
	}

	if remoteBlock.Index > currentLocalBlock.Index {
		blockchain = append(blockchain, remoteBlock)
		SaveChain(blockchain)
		fmt.Printf("👑 CHAIN REORG: Snapping tip to heavier history height: #%d\n", remoteBlock.Index)
		go BroadcastNewBlock(remoteBlock)
	}
}

func StartTCPServer() {
	listener, err := net.Listen("tcp", "0.0.0.0:8080")
	if err != nil { return }
	defer listener.Close()
	fmt.Println("📡 Global TCP P2P Subnetwork Engine Online on Port :8080...")
	for {
		conn, err := listener.Accept()
		if err == nil { go HandleIncomingPeer(conn) }
	}
}

func StartPublicExplorerServer() {
	mux := http.NewServeMux()

		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { // 👈 Added the missing pointer '*' here
		if r.URL.Path != "/" && r.URL.Path != "" { http.NotFound(w, r); return }
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, "explorer.html")
	})


	mux.HandleFunc("/audit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, "audit.html")
	})

	mux.HandleFunc("/req_mempool", func(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
MempoolMutex.Lock()
snap := make([]Transaction, len(GlobalMempool))
copy(snap, GlobalMempool)
MempoolMutex.Unlock()
type QueueItem struct { Sender, Recipient string; Amount, Offering, SizeKB, Score float64 }
var payload []QueueItem = []QueueItem{}
for _, tx := range snap {
var mockAmt float64 = 0.0
var mockRcpt string = "CVN_UTXO_TX"
if len(tx.Outputs) > 0 {
mockAmt = tx.Outputs[0].Amount
mockRcpt = tx.Outputs[0].Recipient
}
payload = append(payload, QueueItem{Sender: "UTXO_SRC", Recipient: mockRcpt, Amount: mockAmt, Offering: tx.FreeWillOffering, SizeKB: 1.0, Score: tx.FreeWillOffering})
}
json.NewEncoder(w).Encode(payload)
})
mux.HandleFunc("/req_chain", func(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Access-Control-Allow-Origin", "")
w.Header().Set("Content-Type", "application/json")
chain := LoadChain()
var totalMined float64 = 0.0
var burnedTokens float64 = 0.0
if len(chain) > 1 { totalMined = float64(len(chain)-1) * 50.0 }
for _, b := range chain {
for _, tx := range b.Transactions {
for _, out := range tx.Outputs {
if out.Recipient == BurnAddress { burnedTokens += out.Amount }
}
burnedTokens += tx.FreeWillOffering
}
}
circulatingSupply := totalMined - burnedTokens
if circulatingSupply < 0 { circulatingSupply = 0 }
var totalRealEscrow float64 = 0.0
StakingPoolMutex.Lock()
for _, bond := range ValidatorStakingPool { totalRealEscrow += bond }
StakingPoolMutex.Unlock()
json.NewEncoder(w).Encode(map[string]interface{}{
"circulating_supply": circulatingSupply,
"blocks":             chain,
"escrow_balance":     totalRealEscrow,
})
})
fmt.Println("🌐 Public Block Explorer Server Online on http://localhost:8081...")
go func() { _ = http.ListenAndServe("0.0.0.0:8081", mux) }()
}
func DialAndGossipWithSeedPeer(seedAddr string) {
conn, err := net.DialTimeout("tcp", seedAddr, 5*time.Second)
if err != nil { return }
defer conn.Close()
client := &http.Client{Timeout: 3 * time.Second}
resp, httpErr := client.Get("ipify.org")
myExtIP := LocalListenerIP
if httpErr == nil {
defer resp.Body.Close()
ipBytes, _ := io.ReadAll(resp.Body)
myExtIP = strings.TrimSpace(string(ipBytes))
}
fmt.Fprintln(conn, "GOSSIP_PEER_DISCOVERY:"+myExtIP+":8080")
respLine, err := bufio.NewReader(conn).ReadString('\n')
if err == nil {
var sharedRoster []string
if json.Unmarshal([]byte(strings.TrimSpace(respLine)), &sharedRoster) == nil {
for _, externalNode := range sharedRoster { RegisterGossipPeer(externalNode) }
}
}
}
func SyncChainFromSeedPeer(seedAddr string) {
conn, err := net.DialTimeout("tcp", seedAddr, 5*time.Second)
if err != nil { return }
defer conn.Close()
fmt.Fprintln(conn, "REQ_CHAIN_SYNC")
respBytes, err := bufio.NewReader(conn).ReadBytes('\n')
if err != nil { return }
var remoteChain []Block
if err := json.Unmarshal(respBytes, &remoteChain); err == nil {
localChain := LoadChain()
if len(remoteChain) > len(localChain) { SaveChain(remoteChain) }
}
}
func Assemble21WitnessGuardMatrix() []string {
rEngine := rand.New(rand.NewSource(time.Now().UnixNano()))
var candidatePool []string
RosterMutex.Lock()
for _, peer := range ActivePeerRoster {
if peer != "" {
candidatePool = append(candidatePool, peer)
}
}
RosterMutex.Unlock()
StakingPoolMutex.Lock()
for validatorAddr := range ValidatorStakingPool {
exists := false
for _, existing := range candidatePool {
if existing == validatorAddr {
exists = true
break
}
}
if !exists && validatorAddr != "" {
candidatePool = append(candidatePool, validatorAddr)
}
}
StakingPoolMutex.Unlock()
if len(candidatePool) == 0 {
candidatePool = append(candidatePool, "LOCAL_SEED_MATRIX_CORE", "COVENANT_STEWARD_ASSEMBLY")
}
shuffled := make([]string, len(candidatePool))
copy(shuffled, candidatePool)
rEngine.Shuffle(len(shuffled), func(i, j int) {
shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
})
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
if newBlock.Difficulty > 6 { newBlock.Difficulty = 6 }
if newBlock.Difficulty < 3 { newBlock.Difficulty = 3 }
newBlock.Nonce = 0
newBlock.GuardMatrix = Assemble21WitnessGuardMatrix()
targetPrefix := strings.Repeat("0", int(newBlock.Difficulty))
fmt.Printf("\n⚒️  PoD Active: Mining Block %d (Target Pattern: Starting with %d Zeros)...\n", newBlock.Index, newBlock.Difficulty)
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
fmt.Printf("🎉 BLOCK SOLVED! Nonce: %d | Time: %.2fs | Speed: %.2f kH/s | Hash: %s\n", newBlock.Nonce, totalElapsed, finalHashRate, newBlock.Hash)
break
}
newBlock.Nonce++
}
return newBlock
}
func MonitorNetworkDensity() {
for {
time.Sleep(30 * time.Second)
HeartbeatMutex.Lock()
currentNetworkTime := time.Now().Unix()
var activeCount int = 0
var deadCount int = 0
var rogueNodes []string
for peer, lastSeen := range ActivePeerHeartbeats {
if (currentNetworkTime - lastSeen) > 90 {
deadCount++
StakingPoolMutex.Lock()
if _, hasStake := ValidatorStakingPool[peer]; hasStake {
rogueNodes = append(rogueNodes, peer)
}
StakingPoolMutex.Unlock()
} else {
activeCount++
}
}
HeartbeatMutex.Unlock()
totalNodes := activeCount + deadCount
if totalNodes > 3 {
dropoutRate := (float64(deadCount) / float64(totalNodes)) * 100.0
if dropoutRate >= 30.0 && len(rogueNodes) > 0 {
fmt.Printf("⚡ [Sabbatical Slasher] CRITICAL ALERT: %.2f%% of the active hashrate dropped offline instantly!\n", dropoutRate)
for _, maliciousNode := range rogueNodes {
ExecuteSabbaticalSlash(maliciousNode, "Covert Manipulation Loop - Abrupt Offline Event")
}
}
}
}
}
func RunAutomatedPeerDiscovery() {
fmt.Println("🛰️  Automated background Peer discovery active.")
for {
time.Sleep(20 * time.Second)
RosterMutex.Lock()
if len(ActivePeerRoster) == 0 { RosterMutex.Unlock(); continue }
snap := make([]string, len(ActivePeerRoster))
copy(snap, ActivePeerRoster)
RosterMutex.Unlock()
for _, peer := range snap {
go func(peerAddr string) {
conn, err := net.DialTimeout("tcp", peerAddr, 3*time.Second)
if err != nil { return }
defer conn.Close()
fmt.Fprintln(conn, "GOSSIP_PEER_DISCOVERY:"+LocalListenerIP+":8080")
respLine, err := bufio.NewReader(conn).ReadString('\n')
if err == nil {
var sharedRoster []string
if json.Unmarshal([]byte(strings.TrimSpace(respLine)), &sharedRoster) == nil {
for _, disc := range sharedRoster { RegisterGossipPeer(disc) }
}
}
}(peer)
}
}
}
func main() {
ValidatorStakingPool = make(map[string]float64)
ActivePeerHeartbeats = make(map[string]int64)
ValidatorStakingPool["CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"] = RequiredStakingBond
ValidatorStakingPool["Peer_Alpha_Stake_Rig"] = RequiredStakingBond
if data, err := os.ReadFile(ProfileConfigFile); err == nil {
var savedCfg MinerConfig
if json.Unmarshal(data, &savedCfg) == nil && savedCfg.SavedMinerAddress != "" {
CustomMinerAddress = savedCfg.SavedMinerAddress
}
}
for i := 0; i < len(os.Args); i++ {
arg := os.Args[i]
if arg == "--wallet" { RunWalletGUI(); return }
if arg == "--miner-address" && i+1 < len(os.Args) {
inputAddress := strings.TrimSpace(os.Args[i+1])
if inputAddress == "=" || inputAddress == "" { continue }
CustomMinerAddress = inputAddress
var newCfg MinerConfig
newCfg.SavedMinerAddress = CustomMinerAddress
cfgBytes, _ := json.MarshalIndent(newCfg, "", "  ")
_ = os.WriteFile(ProfileConfigFile, cfgBytes, 0644)
}
if arg == "--connect" && i+1 < len(os.Args) { ConnectTarget = os.Args[i+1] }
}
fmt.Println("====================================================")
fmt.Println("💎 COVENANT STANDARD (CVN) GOSSIP MESH CORE ENGAGED")
fmt.Printf("💰 BLOCK REWARDS ROUTED TO ID: %s\n", CustomMinerAddress)
fmt.Println("====================================================")
go StartTCPServer()
go StartPublicExplorerServer()
go MonitorNetworkDensity()
go RunAutomatedPeerDiscovery()
time.Sleep(200 * time.Millisecond)
if ConnectTarget != "" {
SyncChainFromSeedPeer(ConnectTarget)
go DialAndGossipWithSeedPeer(ConnectTarget)
}
	// HARD-FORK FIREWALL: Reload historical peer network connections from disk cache file on startup
	LoadPeersFromDisk() 

	blockchain := LoadChain()
	currentBlock := blockchain[len(blockchain)-1]
	fmt.Printf("📂 Local Ledger Loaded. Active Block Height: %d\n", currentBlock.Index)
	for {

MempoolMutex.Lock()
activeMempool := make([]Transaction, len(GlobalMempool))
copy(activeMempool, GlobalMempool)
GlobalMempool = []Transaction{}
MempoolMutex.Unlock()
for i := range activeMempool {
if activeMempool[i].DataSizeKB <= 0 { activeMempool[i].DataSizeKB = 1.0 }
}
sort.Slice(activeMempool, func(i, j int) bool {
return activeMempool[i].FreeWillOffering > activeMempool[j].FreeWillOffering
})
var totalBountyOfferings float64 = 0.0
for _, tx := range activeMempool { totalBountyOfferings += tx.FreeWillOffering }
coinbaseRewardTx := Transaction{
ID:               fmt.Sprintf("COINBASE_REWARD_HEIGHT_%d", currentBlock.Index+1),
Inputs:           []UTXOInput{},
Outputs:          []UTXOOutput{{Recipient: CustomMinerAddress, Amount: 50.0 + totalBountyOfferings}},
FreeWillOffering: 0.0,
DataSizeKB:       0.1,
Witness:          "Communal_Witness_7",
}
blockPayload := append([]Transaction{coinbaseRewardTx}, activeMempool...)
nextDifficulty := CalculateAdaptiveDifficulty(blockchain)
newBlock := MineBlock(currentBlock, blockPayload, nextDifficulty)
go BroadcastNewBlock(newBlock)
		blockchain = LoadChain()
		blockchain = append(blockchain, newBlock)
		currentBlock = newBlock
		SaveChain(blockchain)

		fmt.Printf("💰 Block #%d Sealed successfully!\n", currentBlock.Index)
		BackupLedgerManifest(blockchain) // 👈 TRIGGER YOUR ADVANCED NON-BLOCKING BACKUP TRAP HERE

fmt.Println("-----------------------------------------------------")
time.Sleep(3 * time.Second)
}
}
// SavePeersToDisk serializes the active network routing table to a local JSON cache file
func SavePeersToDisk() {
	RosterMutex.Lock()
	defer RosterMutex.Unlock()

	data, err := json.MarshalIndent(ActivePeerRoster, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile("peers.json", data, 0644)
}

// LoadPeersFromDisk reads historical node coordinates from the local cache file on boot
func LoadPeersFromDisk() {
	if _, err := os.Stat("peers.json"); os.IsNotExist(err) {
		return // No cache file exists yet, skip gracefully
	}

	data, err := os.ReadFile("peers.json")
	if err != nil {
		return
	}

	RosterMutex.Lock()
	var cachedPeers []string
	if err := json.Unmarshal(data, &cachedPeers); err == nil {
		// Merge cached file entries back into your live active routing arrays
		for _, peer := range cachedPeers {
			exists := false
			for _, active := range ActivePeerRoster {
				if active == peer {
					exists = true
					break
				}
			}
			if !exists && peer != "" {
				ActivePeerRoster = append(ActivePeerRoster, peer)
			}
		}
	}
	RosterMutex.Unlock()
	fmt.Printf("📡 [Peer Cache] Successfully reloaded %d historical peer nodes from peers.json!\n", len(ActivePeerRoster))
}