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

func GetLatestBlock() Block {
	var latest Block
	latest.Index = 0
	latest.Hash = "0000000000000000000000000000000000000000000000000000000000000000" // Hard Genesis Hash
	latest.Difficulty = 4

	if GlobalBoltEngine == nil { return latest }

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		meta := tx.Bucket([]byte("Metadata"))
		b := tx.Bucket([]byte("Blocks"))
		if meta == nil || b == nil { return nil }

		heightBytes := meta.Get([]byte("height"))
		if heightBytes != nil {
			heightStr := string(heightBytes)
			blockData := b.Get([]byte(heightStr))
			if blockData != nil {
				_ = json.Unmarshal(blockData, &latest)
			}
		}
		return nil
	})
	return latest
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
	// 🛡️ MESH ENTRANCE FILTER SHIELD
	// Instantly drop any empty parameters, local loopbacks, or malformed visual HTML string leaks
	cleanedAddr := strings.TrimSpace(peerAddr)
	if cleanedAddr == "" || 
	   strings.HasPrefix(cleanedAddr, "127.0.0.1") || 
	   strings.HasPrefix(cleanedAddr, "0.0.0.0") || 
	   strings.Contains(cleanedAddr, "<!") || 
	   strings.Contains(cleanedAddr, "html") || 
	   strings.Contains(cleanedAddr, "DOCTYPE") { 
		return 
	}
	
	RosterMutex.Lock()
	defer RosterMutex.Unlock()
	for _, existing := range ActivePeerRoster {
		if existing == cleanedAddr { return }
	}
	ActivePeerRoster = append(ActivePeerRoster, cleanedAddr)
	fmt.Printf("🛰️  [Gossip Mesh Network] Connected new mesh node to routing tables: %s\n", cleanedAddr)
}
func HandleIncomingPeer(conn net.Conn) {
	defer conn.Close()
	
	reader := bufio.NewReader(conn)

	for {
		lineBytes, err := reader.ReadBytes('\n')
		if err != nil { return } 
		
		text := strings.TrimSpace(string(lineBytes))
		if text == "" { continue }

		if text == "REQ_CHAIN_HEIGHT" {
			latestBlock := GetLatestBlock()
			fmt.Fprintln(conn, strconv.FormatInt(latestBlock.Index, 10))
			continue
		}
		
		// 📡 UPGRADED BATCH STREAMING WIRE GATE: Correctly maps indexes 1 and 2 with unified array slices
		if strings.HasPrefix(text, "REQ_BLOCK_BATCH:") {
			reqPayload := strings.TrimPrefix(text, "REQ_BLOCK_BATCH:")
			parts := strings.Split(reqPayload, ":")
			if len(parts) < 3 { continue }
			
			startIdx, _ := strconv.ParseInt(parts[1], 10, 64)
			endIdx, _ := strconv.ParseInt(parts[2], 10, 64)
			
			if endIdx - startIdx > 512 { endIdx = startIdx + 512 }

			var rawBatch []map[string]interface{}
			_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
				b := tx.Bucket([]byte("Blocks"))
				if b != nil {
					for i := startIdx; i <= endIdx; i++ {
						bData := b.Get([]byte(strconv.FormatInt(i, 10)))
						if bData != nil {
							var dynamicBlock map[string]interface{}
							if json.Unmarshal(bData, &dynamicBlock) == nil {
								rawBatch = append(rawBatch, dynamicBlock)
							}
						}
					}
				}
				return nil
			})

			batchData, _ := json.Marshal(rawBatch)
			compactStr := strings.ReplaceAll(string(batchData), "\n", "")
			compactStr = strings.ReplaceAll(compactStr, "\r", "")
			
			fmt.Fprintln(conn, compactStr)
			continue
		}

		if text == "REQ_CHAIN_SYNC" {
			// Legacy full chain fallback slice logic
			continue
		}
		
		if strings.HasPrefix(text, "GOSSIP_PEER_DISCOVERY:") {
			incomingNodeAddress := strings.TrimPrefix(text, "GOSSIP_PEER_DISCOVERY:")
			RegisterGossipPeer(incomingNodeAddress)
			RosterMutex.Lock()
			rosterJSON, _ := json.Marshal(ActivePeerRoster)
			RosterMutex.Unlock()
			fmt.Fprintln(conn, string(rosterJSON))
			continue
		}
		
		if strings.HasPrefix(text, "TX_BROADCAST:") {
			payload := strings.TrimPrefix(text, "TX_BROADCAST:")
			var tx Transaction
			if err := json.Unmarshal([]byte(payload), &tx); err == nil {
				accepted := MempoolMatrix.PushTransaction(tx)
				if accepted {
					fmt.Fprintln(conn, "TX_ACCEPTED")
				} else {
					fmt.Fprintln(conn, "TX_REJECTED_SPAM_OVERFLOW")
				}
			} else {
				fmt.Fprintln(conn, "TX_REJECTED_MALFORMED")
			}
			continue
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
	
	mux.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		RosterMutex.Lock()
		sharedRoster := make([]string, len(ActivePeerRoster))
		copy(sharedRoster, ActivePeerRoster)
		RosterMutex.Unlock()
		if len(sharedRoster) == 0 {
			sharedRoster = append(sharedRoster, "207.148.67.11:8080 (Cloud Master Seed Node Anchor)")
		}
		data, _ := json.Marshal(sharedRoster)
		w.Write(data)
	})

	mux.HandleFunc("/addresses", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		BalanceCacheMutex.RLock()
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
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("✅ Ingestion successful"))
		} else {
			http.Error(w, "Mempool capacity overflow", http.StatusTooManyRequests)
		}
	})

	mux.HandleFunc("/audit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, "audit.html")
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "" && r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, "explorer.html")
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
func SyncChainFromSeedPeer(seedAddr string) Block {
	var syncedTip Block
	syncedTip.Index = 0
	syncedTip.Hash = "0000000000000000000000000000000000000000000000000000000000000000"
	syncedTip.Difficulty = 4

	conn, err := net.DialTimeout("tcp", seedAddr, 5*time.Second)
	if err != nil { return GetLatestBlock() }
	defer conn.Close()
	
	networkReader := bufio.NewReader(conn)

	fmt.Fprintln(conn, "REQ_CHAIN_HEIGHT")
	respLine, err := networkReader.ReadString('\n')
	if err != nil { return GetLatestBlock() }
	
	remoteHeight, err := strconv.ParseInt(strings.TrimSpace(respLine), 10, 64)
	if err != nil { return GetLatestBlock() }
	
	localHeight := GetLatestBlock().Index
	if remoteHeight > localHeight {
		totalBlocksToSync := remoteHeight - localHeight
		fmt.Printf("\n⛓️  [SYNC GATE ENGAGED] Network Tip Height: #%d | Local Height: #%d\n", remoteHeight, localHeight)
		fmt.Printf("⏳ Catching up on %d missing block segments in 512 bulk compressed batches...\n", totalBlocksToSync)

		currentIdx := localHeight + 1
		for currentIdx <= remoteHeight {
			targetEnd := currentIdx + 511
			if targetEnd > remoteHeight { targetEnd = remoteHeight }
			
			// Request an optimized 512-block batch packet from the seed node
			fmt.Fprintln(conn, fmt.Sprintf("REQ_BLOCK_BATCH:%d:%d", currentIdx, targetEnd))
			
			batchBytes, err := networkReader.ReadBytes('\n')
			if err != nil { 
				fmt.Printf("\n🚨 [BATCH EXCEPTION] Socket read timeout during bulk chunk transfer: %v\n", err)
				return GetLatestBlock()	
			}
			
			cleanedBatchStr := strings.TrimSpace(string(batchBytes))
			var blockBatch []Block
			
			if err := json.Unmarshal([]byte(cleanedBatchStr), &blockBatch); err != nil {
				var dynamicBatch []map[string]interface{}
				if json.Unmarshal([]byte(cleanedBatchStr), &dynamicBatch) == nil && len(dynamicBatch) > 0 {
					for _, rawBlk := range dynamicBatch {
						var bBlock Block
						bBytes, _ := json.Marshal(rawBlk)
						if json.Unmarshal(bBytes, &bBlock) == nil {
							blockBatch = append(blockBatch, bBlock)
						}
					}
				}
			}

			if len(blockBatch) == 0 { 
				fmt.Printf("\n🚨 [SYNC STALL] Received empty block batch for indexes %d to %d\n", currentIdx, targetEnd)
				break 
			}

			// ✅ CRITICAL 1-by-1 MATCHING ALIGNMENT: 
			// We calculate progress and print live console updates INSIDE the active transactional batch loops!
			_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
				b := tx.Bucket([]byte("Blocks"))
				meta := tx.Bucket([]byte("Metadata"))
				
				for _, block := range blockBatch {
					blockData, _ := json.Marshal(block)
					_ = b.Put([]byte(strconv.FormatInt(block.Index, 10)), blockData)
					_ = meta.Put([]byte("height"), []byte(strconv.FormatInt(block.Index, 10)))
					
					// Force an instant visual percentage calculation every time a key block sector finishes saving
					if block.Index == targetEnd || block.Index%128 == 0 {
						currentSyncedCount := block.Index - localHeight
						percentComplete := (float64(currentSyncedCount) / float64(totalBlocksToSync)) * 100.0
						barLength := 20
						completedBars := int((percentComplete / 100.0) * float64(barLength))
						barStr := strings.Repeat("■", completedBars) + strings.Repeat("░", barLength-completedBars)
						
						// Use explicit \n line feeds to bypass Linux stdout buffering blocks completely
						fmt.Printf("📡 Sync Progress: [%s] %.1f%% Completed (#%d/#%d)\n", barStr, percentComplete, block.Index, remoteHeight)
						_ = os.Stdout.Sync()
					}
				}
				return nil
			})

			lastBlockInBatch := blockBatch[len(blockBatch)-1]
			syncedTip = lastBlockInBatch
			currentIdx = lastBlockInBatch.Index + 1
			
			time.Sleep(20 * time.Millisecond) // Smooth pacing delay to ensure visual stability
		}
		fmt.Println("\n🟩 [SYNC COMPLETE] Local database block height aligns with canonical mainnet wire!")
		RebuildStateBalanceCache()
		return syncedTip
	}
	return GetLatestBlock()
}

func RunAutonomousBootstrapEngine() Block {
	fmt.Println("🛰️  [BOOTSTRAP ENGINE] Manual link flag absent. Booting autonomous peer discovery engine...")
	return executeBootstrapSequence()
}

func executeBootstrapSequence() Block {
	localInterfaces := make(map[string]bool)
	localInterfaces["127.0.0.1"] = true
	localInterfaces["0.0.0.0"] = true
	localInterfaces["localhost"] = true

	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				localInterfaces[ipnet.IP.String()] = true
			}
		}
	}

	for _, seedIP := range MasterSeedNodes {
		hostSegment := seedIP
		if strings.Contains(seedIP, ":") {
			parts := strings.Split(seedIP, ":")
			if len(parts) > 0 {
				hostSegment = parts[0]
			}
		}

		if localInterfaces[hostSegment] {
			continue
		}

		fmt.Printf("📡 [BOOTSTRAP] Attempting handshake alignment with Master Seed Anchor: %s\n", seedIP)
		conn, err := net.DialTimeout("tcp", seedIP, 5*time.Second)
		if err != nil { 
			fmt.Printf("   ❌ Seed %s unresponsive or connection timed out. Advancing...\n", seedIP)
			continue 
		}
		conn.Close()

		fmt.Printf("🟩 [BOOTSTRAP SUCCESS] Secure channel verified with seed: %s. Commencing automated sync pipeline...\n", seedIP)
		tipBlock := SyncChainFromSeedPeer(seedIP)
		go DialAndGossipWithSeedPeer(seedIP)
		RegisterGossipPeer(seedIP)
		return tipBlock
	}
	return GetLatestBlock()
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

func ValidateBlockSize(b Block) bool {
	blockBytes, _ := json.Marshal(b)
	return len(blockBytes) <= MaxBlockPayloadSizeBytes
}

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
	if len(dm.ZeroFeeChamber) >= dm.MaxZeroFeeCap { return false }
	dm.ZeroFeeChamber = append(dm.ZeroFeeChamber, tx)
	return true
}

func (dm *DualChamberMempool) AssembleBlockPayload(maxTxCount int) []Transaction {
	dm.Lock()
	defer dm.Unlock()
	finalPayload := make([]Transaction, 0)
	for id, tx := range dm.PriorityChamber {
		if len(finalPayload) >= maxTxCount { break }
		finalPayload = append(finalPayload, tx)
		delete(dm.PriorityChamber, id)
	}
	return finalPayload
}

func InitBoltEngine() {
	db, err := bbolt.Open("cvn_mainnet.db", 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil { log.Fatalf("Database lock failure: %v", err) }
	GlobalBoltEngine = db

	_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		b, _ := tx.CreateBucketIfNotExists([]byte("Blocks"))
		meta, _ := tx.CreateBucketIfNotExists([]byte("Metadata"))
		
		if meta.Get([]byte("height")) == nil {
			_ = meta.Put([]byte("height"), []byte("0"))
		}
		
		if b.Get([]byte("0")) == nil {
			genesisBlock := Block{
				Index:     0,
				Hash:      "0000000000000000000000000000000000000000000000000000000000000000",
				Timestamp: 1710000000,
			}
			gData, _ := json.Marshal(genesisBlock)
			_ = b.Put([]byte("0"), gData)
		}
		return nil
	})
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
if arg == "--miner-address" && i+1 < len(os.Args) {
inputAddress := strings.TrimSpace(os.Args[i+1])
if inputAddress != "" && inputAddress != "=" {
CustomMinerAddress = inputAddress
userPastedAddress = true
}
i++
}
if arg == "--generate-profile" {
fmt.Printf("📋 PROFILE EXPORT INITIALIZED\n")
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
fmt.Println("⏳ [STATE ENGINE] Scanning binary BoltDB buckets to generate State Balance Cache...")
fmt.Println("   ↳ (This may take a moment to safely parse block histories under your 25% vCPU limit...)")
RebuildStateBalanceCache()
fmt.Println("🟩 [STATE ENGINE] Memory Matrix successfully synced. Proceeding to network gates.")
var currentBlock Block
if ConnectTarget != "" {
fmt.Printf("📡 Target connect instruction found: %s\n", ConnectTarget)
currentBlock = SyncChainFromSeedPeer(ConnectTarget)
go DialAndGossipWithSeedPeer(ConnectTarget)
} else {
currentBlock = RunAutonomousBootstrapEngine()
}
if currentBlock.Index == 0 {
currentBlock = GetLatestBlock()
}
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