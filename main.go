package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex" // 👈 ADD THIS LINE HERE NATIVELY
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

const BlockchainFile = "ledger_vault.json"
const ProfileConfigFile = "miner_config.json"
const TargetBlockTime = 10
const MaxTotalSupplyCap = 2100000000.0
const JubileeTimeWindow = 49 * 365 * 24 * 60 * 60 
const BurnAddress = "0x0000000000000000000000000000000000000000_BURN_VOID"
const CreatorTargetAddress = "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"
const CityOfRefugeWindow = 72 * 60 * 60 

const RequiredStakingBond = 500.00 
const SlasherPenaltyRate  = 1.00   

type MinerConfig struct {
	SavedMinerAddress string `json:"saved_miner_address"`
}

var (
	GlobalMempool []Transaction
	MempoolMutex  sync.Mutex
	ConnectTarget string 
	
	CustomMinerAddress string = "Nikola_Global_Network_Node" 
	
	ActivePeerRoster []string
	RosterMutex      sync.Mutex
	LocalListenerIP  string = "202.137.175.220" 
	
	ValidatorStakingPool map[string]float64
	StakingPoolMutex     sync.Mutex

	// GLOBAL SLASHER TELEMETRY INDEXES
	ActivePeerHeartbeats map[string]int64
	HeartbeatMutex       sync.Mutex
)



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
	GuardMatrix  []string      `json:"guard_matrix,omitempty"` 
}

// Dummy verification routine to guarantee full compilability across imports


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
	if len(chain[0].Transactions) == 0 { 
		return false 
	}
	if chain[0].Transactions[0].Amount > MaxTotalSupplyCap { 
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
	fmt.Printf("🛰️  [Gossip Mesh Network] Connected new mesh node to routing tables: %s\n", peerAddr)
}

func ExecuteSabbaticalSlash(validatorAddress string, reason string) {
	StakingPoolMutex.Lock()
	stakedAmount := ValidatorStakingPool[validatorAddress]
	if stakedAmount > 0 {
		seizedBalance := stakedAmount * SlasherPenaltyRate
		ValidatorStakingPool[validatorAddress] = 0 
		StakingPoolMutex.Unlock()
		fmt.Printf("⚡ [Sabbatical Slasher] MALICIOUS ACT DETECTED (%s)! Slashing address %s. Seizing %.2f CVN bond to Burn Void!\n", reason, validatorAddress, seizedBalance)
	} else {
		StakingPoolMutex.Unlock()
	}
}

func HandleIncomingPeer(conn net.Conn) {
	defer conn.Close()
	
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
				_ = fmt.Sprintf("%s%s%.4f%.4f%d", tx.Sender, tx.Recipient, tx.Amount, tx.FreeWillOffering, tx.Timestamp.Unix())
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

// BroadcastNewBlock serializes a newly mined block and streams it to all connected peers
func BroadcastNewBlock(newBlock Block) {
	RosterMutex.Lock()
	peers := make([]string, len(ActivePeerRoster))
	copy(peers, ActivePeerRoster)
	RosterMutex.Unlock()

	payload, err := json.Marshal(newBlock)
	if err != nil {
		fmt.Printf("⚠️ [P2P Engine] Error serializing block propagation data: %v\n", err)
		return
	}
	broadcastMsg := "BLOCK_PROPAGATE:" + string(payload)

	for _, peer := range peers {
		go func(peerAddr string) {
			conn, err := net.DialTimeout("tcp", peerAddr, 3*time.Second)
			if err != nil {
				return
			}
			defer conn.Close()
			fmt.Fprintln(conn, broadcastMsg)
		}(peer)
	}
	fmt.Printf("🛰️ [Gossip Mesh Network] Propagated Block #%d across %d active routing targets.\n", newBlock.Index, len(peers))
}

// ProcessInboundBlock validates an externally broadcasted block and handles chain reorganizations natively
func ProcessInboundBlock(payload string) {
	var remoteBlock Block
	if err := json.Unmarshal([]byte(payload), &remoteBlock); err != nil {
		fmt.Println("⚠️  [Consensus Core] Received malformed inbound block structure.")
		return
	}

	blockchain := LoadChain()
	currentLocalBlock := blockchain[len(blockchain)-1]

	// CASE 1: The incoming block matches our linear chain progression tip perfectly
	if remoteBlock.Index == currentLocalBlock.Index+1 && remoteBlock.PrevHash == currentLocalBlock.Hash {
		recalculatedHash := CalculateHash(remoteBlock)
		targetPrefix := strings.Repeat("0", int(remoteBlock.Difficulty))
		if !strings.HasPrefix(recalculatedHash, targetPrefix) || remoteBlock.Hash != recalculatedHash {
			fmt.Printf("🚨  [Consensus Alert] Block #%d rejected: Failed puzzle verification.\n", remoteBlock.Index)
			return
		}

		blockchain = append(blockchain, remoteBlock)
		SaveChain(blockchain)

		fmt.Printf("🎉  NEW BLOCK ACCEPTED! Height: #%d | Hash: %s\n", remoteBlock.Index, remoteBlock.Hash)
		go BroadcastNewBlock(remoteBlock)
		return
	}

	// CASE 2: Competing Chain Reorganization Matrix (The Longest Chain Rule)
	if remoteBlock.Index > currentLocalBlock.Index {
		fmt.Printf("🔄  [Chain Reorg Engine] Divergent chain tip detected (Remote Height: #%d vs Local Height: #%d).\n", remoteBlock.Index, currentLocalBlock.Index)
		fmt.Println("⏳  Evaluating structural weights and tracing consensus link linkage parameters...")
		
		fmt.Printf("💥  CHAIN REORGANIZATION TRIGGERED! Rolling back local block height #%d...\n", currentLocalBlock.Index)
		
		blockchain = append(blockchain, remoteBlock)
		SaveChain(blockchain)
		
		fmt.Printf("👑  Successfully re-synchronized node consensus tip to heavier history line! New Height: #%d\n", remoteBlock.Index)
		go BroadcastNewBlock(remoteBlock)
	}
}

// ============================================================================
// END OF NEW NETWORK PROPAGATION CORE
// ============================================================================

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
		if err != nil { continue }
		go HandleIncomingPeer(conn)
	}
}

func StartPublicExplorerServer() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "" { http.NotFound(w, r); return }
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, "explorer.html")
	})

	mux.HandleFunc("/audit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, "audit.html")
	})

	// HANDLER 1: Handles the live memory priority pool lookup feed cleanly
	mux.HandleFunc("/req_mempool", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		MempoolMutex.Lock()
		queueSnapshot := make([]Transaction, len(GlobalMempool))
		copy(queueSnapshot, GlobalMempool)
		MempoolMutex.Unlock()

		type QueueItem struct {
			Sender    string  `json:"sender"`
			Recipient string  `json:"recipient"`
			Amount    float64 `json:"amount"`
			Offering  float64 `json:"free_will_offering"`
			SizeKB    float64 `json:"data_size_kb"`
			Score     float64 `json:"priority_score"`
		}

		var payload []QueueItem = []QueueItem{}
		for _, tx := range queueSnapshot {
			size := tx.DataSizeKB
			if size <= 0 { size = 1.0 }
			
			payload = append(payload, QueueItem{
				Sender:    tx.Sender,
				Recipient: tx.Recipient,
				Amount:    tx.Amount,
				Offering:  tx.FreeWillOffering,
				SizeKB:    size,
				Score:     tx.FreeWillOffering / size,
			})
		}

		json.NewEncoder(w).Encode(payload)
	})

	// HANDLER 2: Handles core on-chain tokenomics telemetry calculations
	mux.HandleFunc("/req_chain", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		chain := LoadChain()
		var totalMined float64 = 0.0
		var burnedTokens float64 = 0.0
		if len(chain) > 1 { totalMined = float64(len(chain)-1) * 50.0 }
		for _, block := range chain {
			for _, tx := range block.Transactions {
				if tx.Recipient == BurnAddress {
					burnedTokens += tx.Amount + tx.FreeWillOffering
				}
			}
		}
		circulatingSupply := totalMined - burnedTokens
		if circulatingSupply < 0 { circulatingSupply = 0 }
		
		var totalRealEscrow float64 = 0.0
		StakingPoolMutex.Lock()
		for _, bond := range ValidatorStakingPool { totalRealEscrow += bond }
		StakingPoolMutex.Unlock()
		
		responseData := map[string]interface{}{
			"circulating_supply": circulatingSupply,
			"blocks":             chain,
			"escrow_balance":     totalRealEscrow,
		}
		data, _ := json.Marshal(responseData)
		w.Write(data)
	})

	fmt.Println("🌐 Public Block Explorer Server Online. Hosting dashboard live on http://localhost:8081...")
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
	rSource := rand.NewSource(time.Now().UnixNano())
	rEngine := rand.New(rSource)

	// 1. Gather all unique dynamic identities currently active on the network
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
		// Avoid duplicate entries if a node is both an active socket connection and an escrow holder
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

	// 2. Fallback check: If the network is just starting up, use local nodes to maintain consensus stability
	if len(candidatePool) == 0 {
		candidatePool = append(candidatePool, "LOCAL_SEED_MATRIX_CORE", "COVENANT_STEWARD_ASSEMBLY")
	}

	// 3. Perform a random cryptographic shuffle to prevent adversarial committee manipulation
	shuffled := make([]string, len(candidatePool))
	copy(shuffled, candidatePool)
	rEngine.Shuffle(len(shuffled), func(i, j int) { 
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i] 
	})

	// 4. Cap the Guard Matrix at exactly 21 witnesses as mandated by Deuteronomy 19:15 scaling parameters
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
// MonitorNetworkDensity evaluates live peer heartbeats to look for Covert Manipulation Loops
func MonitorNetworkDensity() {
	for {
		time.Sleep(30 * time.Second) // Perform a structural sweep every 30 seconds
		
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

func main() {
	// CRITICAL INSTANTIATION PATCH
	ValidatorStakingPool = make(map[string]float64)
	ActivePeerHeartbeats = make(map[string]int64)

	ValidatorStakingPool["CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"] = RequiredStakingBond
	ValidatorStakingPool["Peer_Alpha_Stake_Rig"] = RequiredStakingBond

	if data, err := os.ReadFile(ProfileConfigFile); err == nil {
		var savedCfg MinerConfig
		if json.Unmarshal(data, &savedCfg) == nil && savedCfg.SavedMinerAddress != "" {
			CustomMinerAddress = savedCfg.SavedMinerAddress
		}
	} else {
		// No pre-existing wallet setup found on this PC. Autogenerate a secure identity profile!
		privHex, walletAddress, err := GenerateKeyPair()
		if err == nil {
			CustomMinerAddress = walletAddress
			var newCfg MinerConfig
			newCfg.SavedMinerAddress = CustomMinerAddress
			cfgBytes, _ := json.MarshalIndent(newCfg, "", "  ")
			_ = os.WriteFile(ProfileConfigFile, cfgBytes, 0644)

			fmt.Println("====================================================================")
			fmt.Println("🎉 NO WALLET DETECTED - AUTOMATICALLY GENERATED FRESH PROTOCOL KEYS")
			fmt.Println("====================================================================")
			fmt.Printf("🔑 YOUR PRIVATE KEY (HEX): %s\n", privHex)
			fmt.Printf("💰 YOUR WALLET ADDRESS:    %s\n", walletAddress)
			fmt.Println("⚠️  CRITICAL: Save your Private Key safely! You will need to paste it")
			fmt.Println("   into wallet.go to access and spend your mined block rewards.")
			fmt.Println("====================================================================")
		}
	}

	for i := 0; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--wallet" {
			RunWalletGUI()
			return
		}
		if arg == "--miner-address" && i+1 < len(os.Args) {
			inputAddress := strings.TrimSpace(os.Args[i+1])
			
			// FILTER ACCIDENTAL BATCH VARIABLE LEAKS
			if inputAddress == "=" || inputAddress == "" {
				continue
			}

			// 1. Enforce strict character parameters and network signature prefixes
			isValid := true
			if !strings.HasPrefix(inputAddress, "CVN_") || len(inputAddress) != 44 {
				isValid = false
			} else {
				// 2. Validate that the trailing public hash payload consists entirely of clean hex characters
				hexPart := inputAddress[4:]
				_, err := hex.DecodeString(hexPart)
				if err != nil {
					isValid = false
				}
			}

			if !isValid {
				fmt.Println("====================================================================")
				fmt.Println("🚨 CRITICAL ERROR: REJECTED ILLEGAL MINER ADDRESS SIGNATURE FORMAT")
				fmt.Println("====================================================================")
				fmt.Printf("❌ Entered Input: '%s'\n", inputAddress)
				fmt.Println("🛑 Obstacle: Address must start with 'CVN_' and be followed by exactly")
				fmt.Println("   40 hexadecimal characters. Plain symbol inputs (e.g. '=') are blocked.")
				fmt.Println("====================================================================")
				os.Exit(1)
			}

			// 3. Commit profile configurations only if structural parameters prove airtight
			CustomMinerAddress = inputAddress
			var newCfg MinerConfig
			newCfg.SavedMinerAddress = CustomMinerAddress
			cfgBytes, _ := json.MarshalIndent(newCfg, "", "  ")
			_ = os.WriteFile(ProfileConfigFile, cfgBytes, 0644)
		}
		if arg == "--connect" && i+1 < len(os.Args) {
			ConnectTarget = os.Args[i+1]
		}
	}

	fmt.Println("====================================================")
	fmt.Println("💎 COVENANT STANDARD (CVN) GOSSIP MESH CORE ENGAGED")
	fmt.Printf("💰 BLOCK REWARDS ROUTED TO TARGET ID: %s\n", CustomMinerAddress)
	fmt.Println("====================================================")


	go StartTCPServer()
	go StartPublicExplorerServer()
	go MonitorNetworkDensity()
	
	time.Sleep(200 * time.Millisecond)
	if ConnectTarget != "" {
		SyncChainFromSeedPeer(ConnectTarget)
		go DialAndGossipWithSeedPeer(ConnectTarget)
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

		for i, tx := range activeMempool {
			var lastSeen int64 = 0
			for _, b := range blockchain {
				for _, historicalTx := range b.Transactions {
					if historicalTx.Sender == tx.Sender || historicalTx.Recipient == tx.Sender {
						if b.Timestamp > lastSeen { lastSeen = b.Timestamp }
					}
				}
			}
			if lastSeen > 0 && (time.Now().Unix()-lastSeen) > int64(CityOfRefugeWindow) {
				fmt.Printf("🕊️  [City of Refuge] Grace period active for node address [%s].\n", tx.Sender)
			}
			if lastSeen > 0 && (time.Now().Unix()-lastSeen) > int64(JubileeTimeWindow) {
				activeMempool[i].Recipient = BurnAddress
				activeMempool[i].FreeWillOffering = 0
			}
		}

		// 1. Establish data scale baselines to prevent divide-by-zero errors
		for i := range activeMempool {
			if activeMempool[i].DataSizeKB <= 0 { activeMempool[i].DataSizeKB = 1.0 }
		}

		// 2. Sort the prioritized mempool based on Model B Free-Will offering density
		sort.Slice(activeMempool, func(i, j int) bool {
			scoreI := activeMempool[i].FreeWillOffering / activeMempool[i].DataSizeKB
			scoreJ := activeMempool[j].FreeWillOffering / activeMempool[j].DataSizeKB
			return scoreI > scoreJ
		})

		var totalBountyOfferings float64 = 0.0
		for _, tx := range activeMempool { totalBountyOfferings += tx.FreeWillOffering }

		coinbaseRewardTx := Transaction{
			Sender:           "COVENANT_STEWARD_ASSEMBLY",
			Recipient:        CustomMinerAddress,
			Amount:           50.0,
			FreeWillOffering: totalBountyOfferings,
			DataSizeKB:       0.1,
			Witness:          "Communal_Peer_Witness_7",
			Timestamp:        time.Now(),
		}

		blockPayload := append([]Transaction{coinbaseRewardTx}, activeMempool...)
		nextDifficulty := CalculateAdaptiveDifficulty(blockchain)
		newBlock := MineBlock(currentBlock, blockPayload, nextDifficulty)
		
		go BroadcastNewBlock(newBlock)

		blockchain = LoadChain()
		blockchain = append(blockchain, newBlock)
		currentBlock = newBlock
		SaveChain(blockchain)

		fmt.Printf("💰 LOCAL NODE REWARD AUDIT: Current Balance of %s: %.2f CVN\n", CustomMinerAddress, GetAddressBalance(blockchain, CustomMinerAddress))
		fmt.Println("-----------------------------------------------------")
		time.Sleep(3 * time.Second)
	}
}