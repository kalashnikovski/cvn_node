package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

// Structural Architecture Constants
const BlockchainFile = "ledger_vault.json" // Maintained as a read-only migration seed source
const BoltDBFile = "cvn_mainnet.db"
const ProfileConfigFile = "miner_config.json"
const TargetBlockTime = 10
const MaxTotalSupplyCap = 2100000000.0
const JubileeTimeWindow = 49 * 365 * 24 * 60 * 60
const BurnAddress = "0x0000000000000000000000000000000000000000_BURN_VOID"
const CreatorTargetAddress = "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"
const CityOfRefugeWindow = 72 * 60 * 60

const RequiredStakingBond = 500.00
const SlasherPenaltyRate = 1.00
const MaxMempoolZeroFeeSpamCap = 1000 // Inbound RAM buffer threshold protector
// 🔒 PROTOCOL BLOCK-SIZE BACKBONE CONSTRAINT
const MaxBlockPayloadSizeBytes = 1024 * 1024 // 1MB Absolute Hard Ceiling Cap

// Core UTXO and Block Engine Variables
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
	FreeWillOffering float64      `json:"free_will_offering"` // Backwards-compatible field mapping
	DataSizeKB       float64      `json:"data_size_kb"`
	Witness          string       `json:"witness"`
	SignatureR       string       `json:"signature_r,omitempty"`
	SignatureS       string       `json:"signature_s,omitempty"`
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

// CalculateHash processes a block structure into a unique SHA-256 string digest
func CalculateHash(b Block) string {
	record := fmt.Sprintf("%d%d%v%s%d%d%v", b.Index, b.Timestamp, b.Transactions, b.PrevHash, b.Nonce, b.Difficulty, b.GuardMatrix)
	h := sha256.New()
	h.Write([]byte(record))
	return fmt.Sprintf("%x", h.Sum(nil))
}

type MinerConfig struct {
	SavedMinerAddress string `json:"saved_miner_address"`
}

// Thread-Safe Dual-Chamber Mempool Matrix Implementation
type DualChamberMempool struct {
	sync.RWMutex
	PriorityChamber map[string]Transaction // 80% Room (Fee-Density Sorted)
	ZeroFeeChamber  []Transaction          // 20% Room (FIFO Sorted, Cap Guarded)
	MaxZeroFeeCap   int
}

func NewDualChamberMempool(maxSpamCap int) *DualChamberMempool {
	return &DualChamberMempool{
		PriorityChamber: make(map[string]Transaction),
		ZeroFeeChamber:  make([]Transaction, 0),
		MaxZeroFeeCap:   maxSpamCap,
	}
}

func (dm *DualChamberMempool) PushTransaction(tx Transaction) bool {
	// 🔒 INTEGRATED CRYPTOGRAPHIC FIREWALL: Reconstruct transaction hash and verify via crypto_auth.go
	msgRecord := fmt.Sprintf("%s%v%v%.8f", tx.ID, tx.Inputs, tx.Outputs, tx.FreeWillOffering)
	
	// Pass data fields straight into your existing four-string verification arguments in crypto_auth.go
	if !VerifyTransactionSignature(msgRecord, tx.Witness, tx.SignatureR, tx.SignatureS) {
		return false // Instantly drop forged or unsigned payloads out of memory buffers!
	}

	dm.Lock()
	defer dm.Unlock()

	if tx.FreeWillOffering > 0.0 {
		dm.PriorityChamber[tx.ID] = tx
		return true
	}

	if len(dm.ZeroFeeChamber) >= dm.MaxZeroFeeCap {
		return false // Instantly drop overflow spam arrays without allocating RAM threads
	}

	dm.ZeroFeeChamber = append(dm.ZeroFeeChamber, tx)
	return true
}


func (dm *DualChamberMempool) AssembleBlockPayload(maxTxCount int) []Transaction {
	dm.Lock()
	defer dm.Unlock()

	finalPayload := make([]Transaction, 0, maxTxCount)
	priorityLimit := int(float64(maxTxCount) * 0.80)
	zeroFeeLimit := maxTxCount - priorityLimit

	// 1. Drain up to 80% fee-priority records
	count := 0
	type feePair struct {
		ID    string
		Tx    Transaction
		Score float64
	}
	pairs := make([]feePair, 0, len(dm.PriorityChamber))
	for id, tx := range dm.PriorityChamber {
		size := tx.DataSizeKB
		if size == 0 {
			size = 1.0
		}
		pairs = append(pairs, feePair{ID: id, Tx: tx, Score: tx.FreeWillOffering / size})
	}
	// Inline Sort: Highest density rewards move to the front
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[i].Score < pairs[j].Score {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}

	for _, p := range pairs {
		if count >= priorityLimit {
			break
		}
		finalPayload = append(finalPayload, p.Tx)
		delete(dm.PriorityChamber, p.ID)
		count++
	}

	// 2. Drain remaining 20% room via pristine FIFO zero-fee queue
	zCount := 0
	for i, tx := range dm.ZeroFeeChamber {
		if zCount >= zeroFeeLimit {
			dm.ZeroFeeChamber = dm.ZeroFeeChamber[i:]
			return finalPayload
		}
		finalPayload = append(finalPayload, tx)
		zCount++
	}

		if zCount == len(dm.ZeroFeeChamber) {
		dm.ZeroFeeChamber = make([]Transaction, 0)
	}

	return finalPayload
}

// Global Application Core Handles
var (
	MempoolMatrix        *DualChamberMempool
	GlobalBoltEngine     *bbolt.DB
	ConnectTarget        string
	CustomMinerAddress   string = "Nikola_Global_Network_Node"
	ActivePeerRoster     []string
	RosterMutex          sync.Mutex
	P2PListenPort        string = "8080"
	ExplorerPort         string = "8081"
	LocalListenerIP      string = "207.148.67.11"
	ValidatorStakingPool map[string]float64
	StakingPoolMutex     sync.Mutex
	StateBalanceCache    map[string]float64
	BalanceCacheMutex    sync.RWMutex
)

// RebuildStateBalanceCache runs once on startup to fast-index all account yields from BoltDB
func RebuildStateBalanceCache() {
	BalanceCacheMutex.Lock()
	defer BalanceCacheMutex.Unlock()

	// Clear out and allocate memory to your memory matrix indexes safely inside the function body
	StateBalanceCache = make(map[string]float64)

	// 🔥 THE FIX: Change *bbolt.DB to *bbolt.Tx right here
	err := GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Blocks"))
		if b == nil { return nil }

		return b.ForEach(func(k, v []byte) error {
			var block Block
			if json.Unmarshal(v, &block) == nil {
				for _, t := range block.Transactions {
					// 1. Process spent input debits
					if t.ID != "TX_GENESIS_INITIAL_POOL" && !strings.HasPrefix(t.ID, "TX_COINBASE_") {
						// Debit the sender's account for the total output value + fee
						var totalDebit float64
						for _, out := range t.Outputs {
							totalDebit += out.Amount
						}
						totalDebit += t.FreeWillOffering
						StateBalanceCache[t.Witness] -= totalDebit
					}

					// 2. Process received output credits
					for _, out := range t.Outputs {
						StateBalanceCache[out.Recipient] += out.Amount
					}
				}
			}
			return nil
		})
	})
	if err != nil {
		fmt.Printf("⚠️  Failed to seed State Balance Cache: %v\n", err)
	} else {
		fmt.Println("✨ State Balance Cache successfully generated from compressed binary buckets!")
	}
}

var NetworkWitnessRoster = []string{
	"Peer_Witness_1", "Peer_Witness_2", "Peer_Witness_3", "Peer_Witness_4", "Peer_Witness_5",
	"Peer_Witness_6", "Communal_Peer_Witness_7", "Peer_Witness_8", "Peer_Witness_9", "Peer_Witness_10",
	"Witness_Alpha", "Witness_Beta", "Witness_Gamma", "Witness_Delta", "Witness_Epsilon",
	"Validator_Secure_A", "Validator_Secure_B", "Validator_Secure_C", "Validator_Secure_D", "Validator_Secure_E",
	"Node_Guardian_Prime", "Node_Guardian_Secure", "Root_Gateway_Echo", "Sovereign_State_Validator",
}

// ValidateBlockSize checks if the incoming block payload adheres to the strict 1MB protocol ceiling
func ValidateBlockSize(b Block) bool {
	blockBytes, err := json.Marshal(b)
	if err != nil {
		fmt.Printf("⚠️  [PROTOCOL SHIELD] Failed to serialize Block %d for size evaluation: %v\n", b.Index, err)
		return false
	}
	currentSize := len(blockBytes)
	if currentSize > MaxBlockPayloadSizeBytes {
		fmt.Printf("🚨 [CRITICAL PROTOCOL EXPLOIT] Block %d rejected! Payload size (%d bytes) exceeds the 1MB protocol limit (%d bytes).\n", b.Index, currentSize, MaxBlockPayloadSizeBytes)
		return false
	}
	return true
}
	
func CreateGenesisBlock() Block {
	genesisTx := Transaction{
		ID:     "TX_GENESIS_INITIAL_POOL",
		Inputs: []UTXOInput{},
		Outputs: []UTXOOutput{
			{Recipient: "COVENANT_STEWARD_ASSEMBLY", Amount: MaxTotalSupplyCap},
		},
		FreeWillOffering: 0.0,
		DataSizeKB:       1.0,
		Witness:          "ROOT_PEER_WITNESS_GATEWAY",
	}
	genesisBlock := Block{
		Index:        0,
		Timestamp:    1790640000,
		Transactions: []Transaction{genesisTx},
		PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
		Nonce:        0,
		Difficulty:   4,
		GuardMatrix:  []string{"ROOT_PEER_WITNESS_GATEWAY"},
	}
	genesisBlock.Hash = CalculateHash(genesisBlock)
	return genesisBlock
}

// Highly Optimized BoltDB Engine Wrapper Handlers
func InitBoltEngine() {
	db, err := bbolt.Open(BoltDBFile, 0600, nil)
	if err != nil {
		log.Fatalf("❌ Failed to initialize BoltDB binary store: %v", err)
	}
	GlobalBoltEngine = db

	err = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		_, _ = tx.CreateBucketIfNotExists([]byte("Blocks"))
		_, _ = tx.CreateBucketIfNotExists([]byte("Metadata"))
		return nil
	})
	if err != nil {
		log.Fatalf("❌ Failed to partition database buckets: %v", err)
	}

	// 📂 DYNAMIC LIVE DATA MIGRATOR INTERCEPTOR
	if _, err := os.Stat(BlockchainFile); err == nil {
		fmt.Println("⚠️  Legacy plain-text ledger file found. Triggering Pipeline Seed Migration...")
		jsonData, err := os.ReadFile(BlockchainFile)
		if err == nil {
			var legacyChain []Block
			if json.Unmarshal(jsonData, &legacyChain) == nil && len(legacyChain) > 0 {
				err = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
					b := tx.Bucket([]byte("Blocks"))
					meta := tx.Bucket([]byte("Metadata"))
					var lastValidHash string

					for _, block := range legacyChain {
						// 🩹 Heal Block #1025 pointer gaps dynamically on the fly
						if block.Index == 1025 && block.PrevHash != lastValidHash {
							fmt.Println("🩹 Cryptographic lineage gap caught at block #1025. Sequence healed dynamically.")
							block.PrevHash = lastValidHash
							block.Hash = CalculateHash(block)
						}
						blockData, _ := json.Marshal(block)
						_ = b.Put([]byte(strconv.FormatInt(block.Index, 10)), blockData)
						lastValidHash = block.Hash
					}
					_ = meta.Put([]byte("height"), []byte(strconv.FormatInt(int64(len(legacyChain)-1), 10)))
					return nil
				})
				if err == nil {
					fmt.Println("✨ Ledger successfully written to BoltDB binary buckets. Archiving JSON seed file.")
					_ = os.Rename(BlockchainFile, "archived_ledger_vault.json.bak")
				}
			}
		}
	}

	// Ensure at least a genesis block baseline exists inside the binary store
	_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Blocks"))
		meta := tx.Bucket([]byte("Metadata"))
		if b.Get([]byte("0")) == nil {
			gen := CreateGenesisBlock()
			genData, _ := json.Marshal(gen)
			_ = b.Put([]byte("0"), genData)
			_ = meta.Put([]byte("height"), []byte("0"))
		}
		return nil
	})
}

func GetLatestBlock() Block {
	var currentBlock Block
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Blocks"))
		meta := tx.Bucket([]byte("Metadata"))
		heightBytes := meta.Get([]byte("height"))
		if heightBytes != nil {
			blockData := b.Get(heightBytes)
			_ = json.Unmarshal(blockData, &currentBlock)
		}
		return nil
	})
	return currentBlock
}

func SaveBlockToStorage(block Block) {
	_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Blocks"))
		meta := tx.Bucket([]byte("Metadata"))
		blockData, _ := json.Marshal(block)
		stringIdx := strconv.FormatInt(block.Index, 10)
		_ = b.Put([]byte(stringIdx), blockData)
		_ = meta.Put([]byte("height"), []byte(stringIdx))
		return nil
	})
}

func LoadFullChainSlice() []Block {
	var fullChain []Block
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Blocks"))
		meta := tx.Bucket([]byte("Metadata"))
		heightStr := string(meta.Get([]byte("height")))
		height, _ := strconv.ParseInt(heightStr, 10, 64)

		for i := int64(0); i <= height; i++ {
			var bStruct Block
			bData := b.Get([]byte(strconv.FormatInt(i, 10)))
			if bData != nil {
				_ = json.Unmarshal(bData, &bStruct)
				fullChain = append(fullChain, bStruct)
			}
		}
		return nil
	})
	return fullChain
}

// GetAddressBalance reads directly from the State Cache Matrix in microseconds
func GetAddressBalance(targetAddress string) float64 {
	BalanceCacheMutex.RLock()
	defer BalanceCacheMutex.RUnlock()

	if balance, exists := StateBalanceCache[targetAddress]; exists {
		return balance
	}
	return 0.0
}

func CalculateAdaptiveDifficulty() int64 {
var currentDiff int64 = 4
_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
b := tx.Bucket([]byte("Blocks"))
meta := tx.Bucket([]byte("Metadata"))
heightStr := string(meta.Get([]byte("height")))
height, _ := strconv.ParseInt(heightStr, 10, 64)
if height < 2 {
return nil
}
var latestBlock, prevBlock Block
lData := b.Get([]byte(strconv.FormatInt(height, 10)))
pData := b.Get([]byte(strconv.FormatInt(height-1, 10)))
_ = json.Unmarshal(lData, &latestBlock)
_ = json.Unmarshal(pData, &prevBlock)
actualTimeElapsed := latestBlock.Timestamp - prevBlock.Timestamp
currentDiff = latestBlock.Difficulty
if currentDiff > 6 { currentDiff = 6 }
if currentDiff < 3 { currentDiff = 3 }
if actualTimeElapsed < TargetBlockTime {
if currentDiff < 6 { currentDiff++ }
} else if actualTimeElapsed > (TargetBlockTime * 3) {
if currentDiff > 3 { currentDiff-- }
}
return nil
})
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
func HandleIncomingPeer(conn net.Conn) {
defer conn.Close()
scanner := bufio.NewScanner(conn)
for scanner.Scan() {
text := scanner.Text()
if text == "REQ_CHAIN_SYNC" {
chain := LoadFullChainSlice()
data, _ := json.Marshal(chain)
fmt.Fprintln(conn, string(data))
return
}

				// ... Keep your existing GOSSIP_PEER_DISCOVERY block directly above this ...
		if strings.HasPrefix(text, "GOSSIP_PEER_DISCOVERY:") {
            // Your clean code handles peer sharing here...
			return
		}

	// 🧱 SECURE INGRESS FIREWALL: Handle Block Propagation Payload from External Nodes
		if strings.HasPrefix(text, "BLOCK_PROPAGATE:") {
			latestBlock := GetLatestBlock()
			
			// Pass raw streaming network text straight through your security_harness.go isolation filter
			validatedBlock, safe := InterceptGossipBlock(text, latestBlock)
			if safe && validatedBlock != nil {
				fmt.Printf("🧱 [P2P Network Engine] Inbound Block Height #%d passed cryptographic gauntlet! Saving...\n", validatedBlock.Index)
				SaveBlockToStorage(*validatedBlock)
				
				// Re-index your fast-path state balance matrix cache map instantly
				RebuildStateBalanceCache()
			}
			return
		}

		// ... Keep your existing TX_BROADCAST: block directly below this ...

if strings.HasPrefix(text, "TX_BROADCAST:") {
payload := strings.TrimPrefix(text, "TX_BROADCAST:")
var tx Transaction
if err := json.Unmarshal([]byte(payload), &tx); err == nil {
accepted := MempoolMatrix.PushTransaction(tx)
if accepted {
fmt.Printf("📥 [P2P Network Engine] Ingested verified cryptographic UTXO transaction! ID: %s\n", tx.ID)
fmt.Fprintln(conn, "TX_ACCEPTED")
} else {
fmt.Fprintln(conn, "TX_REJECTED_SPAM_OVERFLOW")
}
} else {
fmt.Fprintln(conn, "TX_REJECTED_MALFORMED")
}
return
}
}
}

type RateLimiter struct {
	sync.Mutex
	LastConnection map[string]time.Time
}

var NetworkLimiter = &RateLimiter{
	LastConnection: make(map[string]time.Time),
}

func StartTCPServer() {
	listener, err := net.Listen("tcp", "0.0.0.0:"+P2PListenPort)
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

		// 1. Force Type-Assertion to TCPConn for Persistent Keep-Alives
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(3 * time.Minute) // Keep channel alive for persistent mesh streaming
		}

		// 2. Extract Remote IP Address to process throttling rules
		remoteAddr := conn.RemoteAddr().String()
		ip, _, err := net.SplitHostPort(remoteAddr)
		if err != nil {
			ip = remoteAddr
		}

		// 3. Mathematical Cool-Down Backoff Throttle
		NetworkLimiter.Lock()
		lastSeen, exists := NetworkLimiter.LastConnection[ip]
		now := time.Now()

		if exists && now.Sub(lastSeen) < 4*time.Second {
			// Connection is hitting within the 3-second bot window -> Silent Drop
			NetworkLimiter.Unlock()
			conn.Close() 
			continue
		}

			// Update tracking snapshot matrix with current timestamp
		NetworkLimiter.LastConnection[ip] = now
		NetworkLimiter.Unlock()

		// 4. NEW: Register the verified connection straight into the Gossip Mesh
		GlobalRoster.RegisterPeer(ip)

		// 5. Pass clean connection over to a single main engine logic thread
		go HandleIncomingPeer(conn)
	}
}

func StartPublicExplorerServer() {
	mux := http.NewServeMux()

	// 1. Root Explorer Handle
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "" && r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, "explorer.html")
	})

	// 2. Audit Statement Handle
	mux.HandleFunc("/audit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, "audit.html")
	})

	// 3. CLEAN PEERS HANDLER SEPARATED OUT:
	mux.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		
		// Fetch our complete, synchronized list of known active network nodes
		activePeers := GlobalRoster.GetPeerList()
		
		// If the roster is empty, initialize an empty slice so it returns [] instead of null
		if activePeers == nil {
			activePeers = []string{}
		}
		
		json.NewEncoder(w).Encode(activePeers)
	})

	// 4. Transaction Injection Handle
	mux.HandleFunc("/inject_tx", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var tx Transaction
		if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
			http.Error(w, "Malformed transaction payload data", http.StatusBadRequest)
			return
		}
		accepted := MempoolMatrix.PushTransaction(tx)
		if accepted {
			fmt.Printf("📦 [MEMPOOL INGEST] Received 1 new transaction from wallet client console cleanly!\n")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("✅ Ingestion successful"))
		} else {
			http.Error(w, "Mempool capacity overflow: transaction dropped by spam shield", http.StatusTooManyRequests)
		}
	})
    // ... Keep the rest of your server setup handles underneath this block ...
	// 👥 Unique Network Addresses Roster Handle
	mux.HandleFunc("/addresses", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		BalanceCacheMutex.RLock()
		// Extract all unique account keys from your active thread-safe memory matrix cache state
		addressList := make([]string, 0, len(StateBalanceCache))
		for addr := range StateBalanceCache {
			addressList = append(addressList, addr)
		}
		BalanceCacheMutex.RUnlock()

		// Stream both the flat list and the total integer count out to the browser
		responseData := map[string]interface{}{
			"total_unique_addresses": len(addressList),
			"addresses":              addressList,
		}
		json.NewEncoder(w).Encode(responseData)
	})

	mux.HandleFunc("/req_chain", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		chain := LoadFullChainSlice()
		var totalMined float64 = 0.0
		var burnedTokens float64 = 0.0

		if len(chain) > 1 {
			totalMined = float64(len(chain)-1) * 50.0
		}
		for _, block := range chain {
			for _, tx := range block.Transactions {
				for _, out := range tx.Outputs {
					if out.Recipient == BurnAddress {
						burnedTokens += out.Amount
					}
				}
			}
		}
		circulatingSupply := totalMined - burnedTokens
		if circulatingSupply < 0 { circulatingSupply = 0 }

		var totalRealEscrow float64 = 0.0
		StakingPoolMutex.Lock()
		for _, bond := range ValidatorStakingPool {
			totalRealEscrow += bond
		}
		StakingPoolMutex.Unlock()

		responseData := map[string]interface{}{
			"circulating_supply": circulatingSupply,
			"blocks":             chain,
			"escrow_balance":     totalRealEscrow,
		}
		data, _ := json.Marshal(responseData)
		w.Write(data)
	})

	fmt.Printf("🌐 Public Block Explorer Server Online. Hosting dashboard live on http://localhost:%s...\n", ExplorerPort)
go func() { _ = http.ListenAndServe("0.0.0.0:"+ExplorerPort, mux) }()

}

func DialAndGossipWithSeedPeer(seedAddr string) {
	conn, err := net.DialTimeout("tcp", seedAddr, 5*time.Second)
	if err != nil { return }
	defer conn.Close()
	
	client := &http.Client{Timeout: 3 * time.Second}
	resp, httpErr := client.Get("https://ipify.org")
	
	var myExtIP string
	if httpErr == nil {
		defer resp.Body.Close()
		ipBytes, _ := io.ReadAll(resp.Body)
		myExtIP = strings.TrimSpace(string(ipBytes))
	}
	
	// 🔒 SECURITY BOUNDARY FIX: If API lookups fail, exit immediately instead of cloning the seed's IP
	if myExtIP == "" || strings.HasPrefix(myExtIP, "127.0.0.1") || strings.HasPrefix(myExtIP, "0.0.0.0") {
		fmt.Println("⚠️  [GOSSIP SHIELD] External IP identification trace timed out. Aborting network broadcast registration to prevent address collisions.")
		return
	}
	
	fmt.Fprintln(conn, "GOSSIP_PEER_DISCOVERY:"+myExtIP+":8080")
	respLine, err := bufio.NewReader(conn).ReadString('\n')
	if err == nil {
		var sharedRoster []string
		if json.Unmarshal([]byte(strings.TrimSpace(respLine)), &sharedRoster) == nil {
			for _, externalNode := range sharedRoster {
				RegisterGossipPeer(externalNode)
			}
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
		if len(remoteChain) == 0 { return }
		
		// 🔒 CRITICAL SECURITY AUDIT: Validate entire remote lineage before saving
		var lastValidHash string = remoteChain[0].Hash // Genesis Baseline
		
		for i := 1; i < len(remoteChain); i++ {
			block := remoteChain[i]
			
			// 1. Verify cryptographic lineage pointer link
			if block.PrevHash != lastValidHash {
				fmt.Printf("🚨 [CONSENSUS AUDIT REJECTION] Malicious chain structure detected at Block %d! Broken hash link pointer.\n", block.Index)
				return
			}
			
			// 2. Recalculate hash internally to catch spoofed content payloads
			recalculatedHash := CalculateHash(block)
			if block.Hash != recalculatedHash {
				fmt.Printf("🚨 [CONSENSUS AUDIT REJECTION] Malicious block content payload caught at Block %d! Hash validation mismatch.\n", block.Index)
				return
			}
						// Verify that the incoming block size does not break our 1MB rule
			if !ValidateBlockSize(block) {
				fmt.Printf("🚨 [CONSENSUS AUDIT REJECTION] Malicious block size caught at Block %d! Over-sized data drop.\n", block.Index)
				return
			}

			// 3. Verify Proof-of-Diligence zero-prefix rules
			targetPrefix := strings.Repeat("0", int(block.Difficulty))
			if len(block.Hash) < int(block.Difficulty) || block.Hash[:int(block.Difficulty)] != targetPrefix {
				fmt.Printf("🚨 [CONSENSUS AUDIT REJECTION] Malicious block caught at Block %d! Failed difficulty work target.\n", block.Index)
				return
			}
			
			lastValidHash = block.Hash
		}
		
		// If the entire chain passes the security gauntlet, evaluate length
		localHeight := GetLatestBlock().Index
		if int64(len(remoteChain)-1) > localHeight {
			fmt.Printf("👑 Verified valid blockchain payload received from network peer! Advancing ledger height to Block %d...\n", remoteChain[len(remoteChain)-1].Index)
			_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
				b := tx.Bucket([]byte("Blocks"))
				meta := tx.Bucket([]byte("Metadata"))
				for _, block := range remoteChain {
					blockData, _ := json.Marshal(block)
					_ = b.Put([]byte(strconv.FormatInt(block.Index, 10)), blockData)
				}
				_ = meta.Put([]byte("height"), []byte(strconv.FormatInt(int64(len(remoteChain)-1), 10)))
				return nil
			})
		}
	}
}

func Assemble21WitnessGuardMatrix() []string {
rSource := rand.NewSource(time.Now().UnixNano())
rEngine := rand.New(rSource)
shuffled := make([]string, len(NetworkWitnessRoster))
copy(shuffled, NetworkWitnessRoster)
rEngine.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
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
	
	// 🌐 HOOK: Phase 2 Gossip Mesh Automated Block Propagation Network Broadcast
	BroadcastNewBlock(newBlock)

	break
}
newBlock.Nonce++
}
return newBlock
}
func main() {
// Initialize the structural memory allocation buffers upfront
MempoolMatrix = NewDualChamberMempool(MaxMempoolZeroFeeSpamCap)
InitBoltEngine()
defer GlobalBoltEngine.Close()
ValidatorStakingPool = make(map[string]float64)
ValidatorStakingPool["CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"] = RequiredStakingBond
ValidatorStakingPool["Peer_Alpha_Stake_Rig"] = RequiredStakingBond
CustomMinerAddress = "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"
ConnectTarget = ""
userPastedAddress := false
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		
		// 🛠️ NEW: Allow overriding the P2P communication port
		if arg == "--port" && i+1 < len(os.Args) {
			P2PListenPort = os.Args[i+1]
			i++
		}
		// 🛠️ NEW: Allow overriding the HTTP block explorer dashboard port
		if arg == "--explorer-port" && i+1 < len(os.Args) {
			ExplorerPort = os.Args[i+1]
			i++
		}
		
		if arg == "--wallet" {
			RunWalletGUI()
			return
		}
        // ... leave your other existing flags (--miner-address, --connect) below this ...

if arg == "--miner-address" && i+1 < len(os.Args) {
inputAddress := strings.TrimSpace(os.Args[i+1])
if inputAddress != "" && inputAddress != "=" {
CustomMinerAddress = inputAddress
userPastedAddress = true
}
i++
}
if arg == "--generate-profile" {
privHex, walletAddress, err := GenerateKeyPair()
if err != nil {
fmt.Printf("🚨 SETUP ERROR: Cryptographic core failed: %v\n", err)
os.Exit(1)
}
newConfig := MinerConfig{SavedMinerAddress: walletAddress}
configData, _ := json.MarshalIndent(newConfig, "", "  ")
err = os.WriteFile(ProfileConfigFile, configData, 0644)
if err != nil {
fmt.Printf("🚨 FILE SYSTEM ERROR: Unable to save profile config: %v\n", err)
os.Exit(1)
}
fmt.Println("--------------------------------------------------------------------")
fmt.Printf("📋 AUTOGENERATED WALLET ADDRESS: %s\n", walletAddress)
fmt.Printf("🔑 SAVE YOUR SECRET PRIVATE KEY: %s\n", privHex)
fmt.Println("--------------------------------------------------------------------")
return
}
if arg == "--connect" && i+1 < len(os.Args) {
ConnectTarget = os.Args[i+1]
i++
}
}
if data, err := os.ReadFile(ProfileConfigFile); err == nil && !userPastedAddress {
var savedCfg MinerConfig
if json.Unmarshal(data, &savedCfg) == nil && savedCfg.SavedMinerAddress != "" {
CustomMinerAddress = savedCfg.SavedMinerAddress
}
}
	fmt.Println("====================================================")
	fmt.Println("💎 COVENANT STANDARD (CVN) GOSSIP MESH CORE ENGAGED")
	fmt.Printf("💰 BLOCK REWARDS ROUTED TO TARGET ID: %s\n", CustomMinerAddress)
	fmt.Println("====================================================")

	// 🚀 NEW PHASE 2 HOOK: Fire the automated router firewall mapping sequence
	SetupAutomatedPortMapping()

	go StartTCPServer()
	go StartPublicExplorerServer()
	time.Sleep(200 * time.Millisecond)
	if ConnectTarget != "" {
		SyncChainFromSeedPeer(ConnectTarget)
		go DialAndGossipWithSeedPeer(ConnectTarget)
		
		// 🚀 ACTIVATE THE DAEMON AUTOMATICALLY ON BOOT
		StartPeriodicPeerSync(ConnectTarget, 1*time.Minute)
	}

	currentBlock := GetLatestBlock()
	fmt.Printf("📂 Local Ledger Loaded. Active Block Height: %d\n", currentBlock.Index)
	
	// 🚀 THE FIX: Insert the cache building call right here!
	RebuildStateBalanceCache()

	for {
		// Mine up to 100 transactions per block boundary...

activeMempool := MempoolMatrix.AssembleBlockPayload(100)
if len(activeMempool) > 0 {
fmt.Printf("📦 [MINER CORE] Sweeping %d transactions from dual-chamber matrix straight into block payload...\n", len(activeMempool))
}
var totalBountyOfferings float64 = 0.0
for _, tx := range activeMempool {
totalBountyOfferings += tx.FreeWillOffering
}
coinbaseRewardTx := Transaction{
ID:     fmt.Sprintf("TX_COINBASE_%d", time.Now().Unix()),
Inputs: []UTXOInput{},
Outputs: []UTXOOutput{
{Recipient: CustomMinerAddress, Amount: 50.0 + totalBountyOfferings},
},
FreeWillOffering: 0.0,
DataSizeKB:       0.1,
Witness:          "Communal_Peer_Witness_7",
}
blockPayload := append([]Transaction{coinbaseRewardTx}, activeMempool...)
nextDifficulty := CalculateAdaptiveDifficulty()
newBlock := MineBlock(currentBlock, blockPayload, nextDifficulty)
SaveBlockToStorage(newBlock)
currentBlock = newBlock
fmt.Printf("💰 LOCAL NODE REWARD AUDIT: Current Balance of %s: %.2f CVN\n", CustomMinerAddress, GetAddressBalance(CustomMinerAddress))
fmt.Println("-----------------------------------------------------")
time.Sleep(3 * time.Second)
}
} // <--- THIS IS THE EXISTING END OF YOUR FUNC MAIN()

// Paste the background daemon right here outside the brackets:
func StartPeriodicPeerSync(seedIP string, interval time.Duration) {
	if seedIP == "" {
		return
	}
	// Normalize seed endpoint to its public block explorer HTTP dashboard path
	host, _, err := net.SplitHostPort(seedIP)
	if err != nil {
		host = seedIP
	}
	seedURL := fmt.Sprintf("http://%s:8081/peers", host)

	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		fmt.Printf("🔄 [PEER SYNC DAEMON] Automated mesh discovery active. Target: %s\n", seedURL)

		for {
			resp, err := client.Get(seedURL)
			if err != nil {
				time.Sleep(interval)
				continue
			}

			var discoveredIPs []string
			err = json.NewDecoder(resp.Body).Decode(&discoveredIPs)
			resp.Body.Close()

			if err == nil && len(discoveredIPs) > 0 {
				for _, ip := range discoveredIPs {
					// Register harvested nodes directly into global memory tables
					RegisterGossipPeer(net.JoinHostPort(ip, "8080"))
				}
			}
			time.Sleep(interval)
		}
	}()
}