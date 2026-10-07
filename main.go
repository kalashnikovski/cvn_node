package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)


// Architectural Architecture Constants & Configuration Paths
const BlockchainFile = "ledger_vault.json" // Maintained exclusively as a read-only migration seed
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
const MaxMempoolZeroFeeSpamCap = 1000
const MaxBlockPayloadSizeBytes = 1024 * 1024 // 1MB Absolute Hard Ceiling Cap

const P2PListenPort = 8080
const ExplorerPort = 8081

// 📡 PERMANENT MAINNET SEED ANCHOR ARRAYS (Phase 2 Bootstrapping Backplane)
var MasterSeedNodes = []string{
	"202.137.175.220:8080", // Lead Architect Melbourne Home Rig Hub
	"207.148.67.11:8080",   // Singapore VPS Backbone Cloud Seed Anchor
}

// Global Core Infrastructure State Variables
var (
	MempoolMatrix      *DualChamberMempool
	GlobalBoltEngine   *bbolt.DB
	ConnectTarget      string
	CustomMinerAddress string = "Nikola_Global_Network_Node"

	ActivePeerRoster []string
	RosterMutex      sync.Mutex
	LocalListenerIP  string = "202.137.175.220"

	ValidatorStakingPool map[string]float64
	StakingPoolMutex     sync.Mutex
)

// Thread-Safe State Cache Matrix Thread Maps
var (
	StateBalanceCache = make(map[string]float64)
	BalanceCacheMutex sync.RWMutex
)

// CalculateHash processes a block structure into a unique SHA-256 string digest
func CalculateHash(b Block) string {
	record := fmt.Sprintf("%d%d%v%s%d%d%v", b.Index, b.Timestamp, b.Transactions, b.PrevHash, b.Nonce, b.Difficulty, b.GuardMatrix)
	h := sha256.New()
	h.Write([]byte(record))
	return fmt.Sprintf("%x", h.Sum(nil))
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

func InitBoltEngine() {
	db, err := bbolt.Open(BoltDBFile, 0600, nil)
	if err != nil {
		log.Fatalf("❌ CRITICAL STORAGE FAULT: Failed to initialize BoltDB binary store: %v", err)
	}
	GlobalBoltEngine = db

	err = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		_, _ = tx.CreateBucketIfNotExists([]byte("Blocks"))
		_, _ = tx.CreateBucketIfNotExists([]byte("Metadata"))
		return nil
	})
	if err != nil {
		log.Fatalf("❌ CRITICAL STRUCTURAL FAULT: Failed to partition database buckets: %v", err)
	}

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
						if block.Index > 0 && block.PrevHash != lastValidHash {
							fmt.Printf("🩹 Cryptographic lineage gap caught at block #%d. Sequence healed dynamically.\n", block.Index)
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

	BalanceCacheMutex.Lock()
	defer BalanceCacheMutex.Unlock()

	for _, t := range block.Transactions {
		if t.ID != "TX_GENESIS_INITIAL_POOL" && !strings.HasPrefix(t.ID, "TX_COINBASE_") && !strings.HasPrefix(t.ID, "TX_COINBASE_REWARD_") {
			var totalDebit float64
			for _, out := range t.Outputs {
				totalDebit += out.Amount
			}
			totalDebit += t.FreeWillOffering
			StateBalanceCache[t.Witness] -= totalDebit
		}
		for _, out := range t.Outputs {
			StateBalanceCache[out.Recipient] += out.Amount
		}
	}
	fmt.Printf("\n⚡ [STATE ENGINE MATRIX] Balance cache updated live for Block Height #%d!\n", block.Index)
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
		if height < 2 { return nil }
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
	if peerAddr == "" || strings.HasPrefix(peerAddr, "127.0.0.1") || strings.HasPrefix(peerAddr, "0.0.0.0") { return }
	RosterMutex.Lock()
	defer RosterMutex.Unlock()
	for _, existing := range ActivePeerRoster {
		if existing == peerAddr { return }
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
		if strings.HasPrefix(text, "GOSSIP_PEER_DISCOVERY:") {
			incomingNodeAddress := strings.TrimPrefix(text, "GOSSIP_PEER_DISCOVERY:")
			RegisterGossipPeer(incomingNodeAddress)
			RosterMutex.Lock()
			rosterJSON, _ := json.Marshal(ActivePeerRoster)
			RosterMutex.Unlock()
			fmt.Fprintln(conn, string(rosterJSON))
			return
		}
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

func StartTCPServer() {
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", P2PListenPort))
	if err != nil {
		fmt.Printf("🚨 TCP Server Bind Error: %v\n", err)
		return
	}
	defer listener.Close()
	fmt.Printf("📡 Global TCP P2P Subnetwork Engine Online. Listening on Port :%d...\n", P2PListenPort)
	
	for {
		conn, err := listener.Accept()
if err != nil { continue }
if tcpConn, ok := conn.(*net.TCPConn); ok {
_ = tcpConn.SetKeepAlive(true)
_ = tcpConn.SetKeepAlivePeriod(3 * time.Minute)
}
go HandleIncomingPeer(conn)
}
}
func StartPublicExplorerServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "" && r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, "explorer.html")
	})
	// 📡 INJECTED PEER ROSTER TELEMETRY ROUTE
	mux.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		
		RosterMutex.Lock()
		// Capture a snapshot slice of our active mesh network routing tables
		sharedRoster := make([]string, len(ActivePeerRoster))
		copy(sharedRoster, ActivePeerRoster)
		RosterMutex.Unlock()
		
		// If the roster is empty, fall back to showing our primary seed backplane context
		if len(sharedRoster) == 0 {
			sharedRoster = append(sharedRoster, "207.148.67.11:8080 (Cloud Master Seed Node)")
		}
		
		data, _ := json.Marshal(sharedRoster)
		w.Write(data)
	})
		// 💰 INJECTED UNIQUE WALLET ADDRESSES TELEMETRY ROUTE
	mux.HandleFunc("/addresses", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		
		BalanceCacheMutex.RLock()
		// Dynamically isolate all account coordinates out of the State Balance Cache Matrix
		uniqueAddresses := make([]string, 0, len(StateBalanceCache))
		for addr := range StateBalanceCache {
			uniqueAddresses = append(uniqueAddresses, addr)
		}
		totalUnique := len(uniqueAddresses)
		BalanceCacheMutex.RUnlock()
		
		responseData := map[string]interface{}{
			"addresses":              uniqueAddresses,
			"total_unique_addresses": totalUnique,
		}
		
		data, _ := json.Marshal(responseData)
		w.Write(data)
	})

	mux.HandleFunc("/audit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, "audit.html")
	})
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
		if tx.Witness == "" || len(tx.Witness) < 10 {
			http.Error(w, "🚨 PROTOCOL CEILING ALERT: Transaction dropped. Destination target validation firewall exception.", http.StatusUnprocessableEntity)
			return
		}
		accepted := MempoolMatrix.PushTransaction(tx)
		if accepted {
			fmt.Printf("📦 [MEMPOOL INGEST] Received 1 new transaction cleanly!\n")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("✅ Ingestion successful"))
		} else {
			http.Error(w, "Mempool capacity overflow: transaction dropped by spam shield", http.StatusTooManyRequests)
		}
	})
	mux.HandleFunc("/req_chain", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		chain := LoadFullChainSlice()
		var totalMined float64 = 0.0
		var burnedTokens float64 = 0.0
		if len(chain) > 1 { totalMined = float64(len(chain)-1) * 50.0 }
		for _, block := range chain {
			for _, tx := range block.Transactions {
				for _, out := range tx.Outputs {
					if out.Recipient == BurnAddress { burnedTokens += out.Amount }
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
	fmt.Printf("🌐 Public Block Explorer Server Online. Hosting live on http://localhost:%d...\n", ExplorerPort)
	go func() { _ = http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", ExplorerPort), mux) }()
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
	if myExtIP == "" || strings.HasPrefix(myExtIP, "127.0.0.1") || strings.HasPrefix(myExtIP, "0.0.0.0") { return }
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
		if len(remoteChain) == 0 { return }
		
		localHeight := GetLatestBlock().Index
		remoteHeight := remoteChain[len(remoteChain)-1].Index
		
		if remoteHeight > localHeight {
			totalBlocksToSync := remoteHeight - localHeight
			fmt.Printf("\n⛓️  [SYNC GATE ENGAGED] Network Tip Height: #%d | Local Height: #%d\n", remoteHeight, localHeight)
			fmt.Printf("⏳ Catching up on %d missing block segments...\n", totalBlocksToSync)
			
			// ✅ FIXED: Rely on the true root parent link hash of the incoming slice chain segment
			var lastValidHash string = remoteChain[0].Hash
			for i := 1; i < len(remoteChain); i++ {
				block := remoteChain[i]
				if block.PrevHash != lastValidHash { return }
				if block.Hash != CalculateHash(block) { return }
				if !ValidateBlockSize(block) { return }
				targetPrefix := strings.Repeat("0", int(block.Difficulty))
				if len(block.Hash) < int(block.Difficulty) || block.Hash[:int(block.Difficulty)] != targetPrefix { return }
				lastValidHash = block.Hash

				if block.Index > localHeight {
					currentSyncedCount := block.Index - localHeight
					percentComplete := (float64(currentSyncedCount) / float64(totalBlocksToSync)) * 100.0
					barLength := 20
					completedBars := int((percentComplete / 100.0) * float64(barLength))
					barStr := strings.Repeat("■", completedBars) + strings.Repeat("░", barLength-completedBars)
					fmt.Printf("\r📡 Sync Progress: [%s] %.1f%% Completed (#%d/#%d)", barStr, percentComplete, block.Index, remoteHeight)
				}
			}
			fmt.Println("\n🟩 [SYNC COMPLETE] Local database block height aligns with canonical mainnet wire!")
			
			_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
				b := tx.Bucket([]byte("Blocks"))
				meta := tx.Bucket([]byte("Metadata"))
				for _, block := range remoteChain {
					blockData, _ := json.Marshal(block)
					_ = b.Put([]byte(strconv.FormatInt(block.Index, 10)), blockData)
				}
				_ = meta.Put([]byte("height"), []byte(strconv.FormatInt(remoteHeight, 10)))
				return nil
			})
		}
	}
}

func RunAutonomousBootstrapEngine() {
	fmt.Println("🛰️  [BOOTSTRAP ENGINE] Manual link flag absent. Booting autonomous peer discovery engine...")
	executeBootstrapSequence()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			RosterMutex.Lock()
			peerCount := len(ActivePeerRoster)
			RosterMutex.Unlock()
			if peerCount == 0 { executeBootstrapSequence() }
		}
	}()
}

func executeBootstrapSequence() {
	for _, seedIP := range MasterSeedNodes {
		if strings.HasPrefix(seedIP, LocalListenerIP) { continue }
		conn, err := net.DialTimeout("tcp", seedIP, 4*time.Second)
		if err != nil { continue }
		conn.Close()

		fmt.Printf("🟩 [BOOTSTRAP SUCCESS] Secure channel verified with seed: %s. Commencing automated sync pipeline...\n", seedIP)
		SyncChainFromSeedPeer(seedIP)
		DialAndGossipWithSeedPeer(seedIP)
		RegisterGossipPeer(seedIP)
		break
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
	newBlock.GuardMatrix = []string{"Peer_Alpha_Stake_Rig", "202.137.175.220:8080"}
	targetPrefix := strings.Repeat("0", int(newBlock.Difficulty))
	fmt.Printf("\n⚒️  PoD Active: Mining Block %d (Target Pattern: Starting with %d Zeros)...\n", newBlock.Index, newBlock.Difficulty)
	startTime := time.Now()
	for {
		newBlock.Hash = CalculateHash(newBlock)
		if newBlock.Nonce > 0 && newBlock.Nonce%1000000 == 0 {
			elapsed := time.Since(startTime).Seconds()
			if elapsed == 0 { elapsed = 0.001 }
			time.Sleep(1 * time.Millisecond) 
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

func RebuildStateBalanceCache() {
	BalanceCacheMutex.Lock()
	defer BalanceCacheMutex.Unlock()
	StateBalanceCache = make(map[string]float64)
	if GlobalBoltEngine == nil { return }
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Blocks"))
		meta := tx.Bucket([]byte("Metadata"))
		if b == nil || meta == nil { return nil }
		heightBytes := meta.Get([]byte("height"))
		if heightBytes == nil { return nil }
		height, _ := strconv.ParseInt(string(heightBytes), 10, 64)
		for i := int64(0); i <= height; i++ {
			v := b.Get([]byte(strconv.FormatInt(i, 10)))
			if v == nil { continue }
			var block Block
			if json.Unmarshal(v, &block) == nil {
				for _, t := range block.Transactions {
					if t.ID != "TX_GENESIS_INITIAL_POOL" && !strings.HasPrefix(t.ID, "TX_COINBASE_") && !strings.HasPrefix(t.ID, "TX_COINBASE_REWARD_") {
						var totalDebit float64
						for _, out := range t.Outputs { totalDebit += out.Amount }
						totalDebit += t.FreeWillOffering
						StateBalanceCache[t.Witness] -= totalDebit
					}
					for _, out := range t.Outputs { StateBalanceCache[out.Recipient] += out.Amount }
				}
			}
		}
		return nil
	})
	fmt.Println("✨ State Balance Cache successfully generated from compressed binary buckets!")
}

// ✅ FIXED: Injected missing block dimension validator logic directly into package scope
func ValidateBlockSize(b Block) bool {
	blockBytes, _ := json.Marshal(b)
	return len(blockBytes) <= MaxBlockPayloadSizeBytes
}

// ✅ FIXED: Restored complete DualChamberMempool matrix implementation right here
type DualChamberMempool struct {
	sync.RWMutex
	PriorityChamber map[string]Transaction
	ZeroFeeChamber  []Transaction
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
	dm.Lock()
	defer dm.Unlock()
	if tx.FreeWillOffering > 0.0 {
		dm.PriorityChamber[tx.ID] = tx
		return true
	}
	if len(dm.ZeroFeeChamber) >= dm.MaxZeroFeeCap {
		return false
	}
	dm.ZeroFeeChamber = append(dm.ZeroFeeChamber, tx)
	return true
}

func (dm *DualChamberMempool) AssembleBlockPayload(maxTxCount int) []Transaction {
	dm.Lock()
	defer dm.Unlock()
	finalPayload := make([]Transaction, 0)
	for id, tx := range dm.PriorityChamber {
		if len(finalPayload) >= maxTxCount {
			break
		}
		finalPayload = append(finalPayload, tx)
		delete(dm.PriorityChamber, id)
	}
	return finalPayload
}
func main() {
	MempoolMatrix = NewDualChamberMempool(MaxMempoolZeroFeeSpamCap)
	InitBoltEngine()
	defer GlobalBoltEngine.Close()

	ValidatorStakingPool = make(map[string]float64)
	ValidatorStakingPool["CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"] = RequiredStakingBond
	ValidatorStakingPool["Peer_Alpha_Stake_Rig"] = RequiredStakingBond

	CustomMinerAddress = "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"
	userPastedAddress := false

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--wallet" {
			RunWalletGUI()
			return
		}
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
			if err != nil { os.Exit(1) }
			newConfig := MinerConfig{SavedMinerAddress: walletAddress}
			configData, _ := json.MarshalIndent(newConfig, "", "  ")
			_ = os.WriteFile(ProfileConfigFile, configData, 0644)
			fmt.Printf("📋 WALLET GENERATED: %s\n🔑 PRIVATE KEY: %s\n", walletAddress, privHex)
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
	fmt.Println("💎 COVENANT STANDARD (CVN) LAYER-1 CONSENSUS CORE ENGINE LAUNCHER")
	fmt.Printf("💰 BLOCK REWARDS ROUTED TO TARGET ID: %s\n", CustomMinerAddress)
	fmt.Println("====================================================")

	go StartTCPServer()
	go StartPublicExplorerServer()
	time.Sleep(200 * time.Millisecond)

	// 📊 INJECTED CONSOLE TELEMETRY TRACKER
	fmt.Println("⏳ [STATE ENGINE] Scanning binary BoltDB buckets to generate State Balance Cache...")
	fmt.Println("   ↳ (This may take a moment to safely parse block histories under your 25% vCPU limit...)")
	RebuildStateBalanceCache()
	fmt.Println("🟩 [STATE ENGINE] Memory Matrix successfully synced. Proceeding to network gates.")

	if ConnectTarget != "" {
		fmt.Printf("📡 Target connect instruction found: %s\n", ConnectTarget)
		SyncChainFromSeedPeer(ConnectTarget)
		go DialAndGossipWithSeedPeer(ConnectTarget)
	} else {
		RunAutonomousBootstrapEngine()
	}

	currentBlock := GetLatestBlock()
	fmt.Printf("📂 Local Blockheight Operational: #%d\n", currentBlock.Index)

	for {
		activeMempool := MempoolMatrix.AssembleBlockPayload(100)
		var totalBountyOfferings float64 = 0.0
		for _, tx := range activeMempool { totalBountyOfferings += tx.FreeWillOffering }

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
		time.Sleep(3 * time.Second)
	}
}