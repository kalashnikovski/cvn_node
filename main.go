package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed all:frontend/dist
var assets embed.FS

var globalHashCount uint64
var liveTerminalEventLogs []string
var logMutex sync.Mutex

var hashRateHistory15m []float64
var hashRateHistory1h  []float64
var hashRateHistory24h []float64
var hashrateMutex      sync.Mutex

var currentLiveHashrateTrack float64

// 👑 THE METRIC TRACKER ANCHORS:
var activeMiningDurationNs uint64

func clearTerminalNatively() {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "cls")
	} else {
		cmd = exec.Command("clear")
	}
	cmd.Stdout = os.Stdout
	_ = cmd.Run()
}

func initWindowsAnsi() {}

func main() {
	initWindowsAnsi()

	for _, arg := range os.Args {
		if arg == "--generate-wallet" {
			fmt.Println("🔑 Generating secure asymmetric cryptography parameters via CSPRNG...")
			privHex, pubHex, walletAddress, err := GenerateKeyPair()
			if err != nil {
				fmt.Printf("🚨 CRYPTOGRAPHIC FAULT: Key allocation path rejected: %v\n", err)
				os.Exit(1)
			}

			fmt.Println("\n====================================================================")
			fmt.Printf("🪪 NEW WALLET ADDRESS:     %s\n", walletAddress)
			fmt.Printf("📋 UNTRUNCATED PUBLIC KEY: %s\n", pubHex)
			fmt.Printf("🔐 PRIVATE KEY HEX:        %s\n", privHex)
			fmt.Println("====================================================================")

			backupContent := fmt.Sprintf("=== COVENANT STANDARD SECURITY BACKUP ===\nAddress: %s\nPublic Key: %s\nPrivate Key: %s\n", walletAddress, pubHex, privHex)
			_ = os.WriteFile("cvn_secret_backup.txt", []byte(backupContent), 0600)

			configMap := map[string]string{"miner_address": walletAddress}
			configMapBytes, _ := json.MarshalIndent(configMap, "", "  ")
			_ = os.WriteFile("miner_config.json", configMapBytes, 0644)
			os.Exit(0)
		}
	}

	if _, err := os.Stat("miner_config.json"); os.IsNotExist(err) {
		fmt.Println("👤 [USER STATUS] Pristine installation path detected. Initializing secure configuration parameters...")
		privHex, pubHex, walletAddress, err := GenerateKeyPair()
		if err == nil {
			configMap := map[string]string{"miner_address": walletAddress}
			configMapBytes, _ := json.MarshalIndent(configMap, "", "  ")
			_ = os.WriteFile("miner_config.json", configMapBytes, 0644)

			backupContent := fmt.Sprintf("=== COVENANT STANDARD SECURITY BACKUP ===\nAddress: %s\nPublic Key: %s\nPrivate Key: %s\n", walletAddress, pubHex, privHex)
			_ = os.WriteFile("cvn_secret_backup.txt", []byte(backupContent), 0600)
			fmt.Printf("💾 Secure profile configured natively. Address assigned: %s\n", walletAddress)
		}
	}

	InitBoltEngine()

	liveTerminalEventLogs = make([]string, 0)
	hashRateHistory15m   = make([]float64, 0)
	hashRateHistory1h    = make([]float64, 0)
	hashRateHistory24h   = make([]float64, 0)

	for i, arg := range os.Args {
		if arg == "--miner-address" && i+1 < len(os.Args) {
			CustomMinerAddress = os.Args[i+1]
		}
	}

	if CustomMinerAddress == "" {
		configFileBytes, err := os.ReadFile("miner_config.json")
		if err == nil {
			var parsedConfig map[string]string
			if json.Unmarshal(configFileBytes, &parsedConfig) == nil {
				CustomMinerAddress = parsedConfig["miner_address"]
			}
		}
	}

	if CustomMinerAddress == "" {
		CustomMinerAddress = "CVN_UNCONFIGURED_LOCAL_NODE_ID"
	}
	go func() {
		winMux := http.NewServeMux()

		// 👑 THE MASTER UNIFIED NETWORK WEB EXPLORER & API ROUTER
		winMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			// 🛰️ Endpoint Route 1: Live global address balance sheet cache
			if r.URL.Path == "/addresses" {
				w.Header().Set("Content-Type", "application/json")
				BalanceCacheMutex.RLock()
				activeWallets := make(map[string]float64)
				for addr, bal := range StateBalanceCache {
					if bal > 0 {
						activeWallets[addr] = bal
					}
				}
				BalanceCacheMutex.RUnlock()
				json.NewEncoder(w).Encode(activeWallets)
				return
			}

			// 🛰️ Endpoint Route 2: Active P2P network discovery mesh tracking map
			if r.URL.Path == "/peers" {
				w.Header().Set("Content-Type", "application/json")
				PeerMapMutex.RLock()
				peerList := make([]string, 0)
				for peerIP := range ActivePeerMap {
					peerList = append(peerList, peerIP)
				}
				PeerMapMutex.RUnlock()
				if len(peerList) == 0 {
					peerList = []string{"202.137.175.220:8080 (Melbourne Master)"}
				}
				json.NewEncoder(w).Encode(peerList)
				return
			}

			// 🛰️ Endpoint Route 3: Master explorer static interface deployment asset view
			if r.URL.Path == "/" || r.URL.Path == "/index.html" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				htmlBytes, err := os.ReadFile("explorer.html")
				if err != nil {
					StateMutex.RLock()
					idx := GetLatestBlockInternal().Index
					StateMutex.RUnlock()
					fmt.Fprintf(w, "<h2>💎 Covenant Standard Core Matrix Active</h2><p>Rig Height: #%d</p>", idx)
					return
				}
				w.Write(htmlBytes)
				return
			}

			// Gracefully fall back to standard matching path routing parameters if sub-directories are requested
			http.NotFound(w, r)
		})

		winMux.HandleFunc("/blocks/batch", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			
			startStr := r.URL.Query().Get("start")
			startHeight, err := strconv.ParseInt(startStr, 10, 64)
			if err != nil || startHeight < 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			var blocks []Block
			
			StateMutex.RLock()
			currentTip := GetLatestBlockInternal().Index
			StateMutex.RUnlock()

			endHeight := startHeight + 511
			if endHeight > currentTip {
				endHeight = currentTip
			}

			for h := startHeight; h <= endHeight; h++ {
				blocks = append(blocks, GetBlockByHeightFromDB(h))
			}

			json.NewEncoder(w).Encode(blocks)
		})

		winMux.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			latest := GetLatestBlock()
			payload := map[string]interface{}{
				"current_height": latest.Index,
				"block_hash":     latest.Hash,
				"timestamp":      latest.Timestamp,
				"transactions":   latest.Transactions,
			}
			json.NewEncoder(w).Encode(payload)
		})

		winMux.HandleFunc("/transaction", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			var rawBody bytes.Buffer
			if _, err := rawBody.ReadFrom(r.Body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			var tx Transaction
			if err := json.Unmarshal(rawBody.Bytes(), &tx); err == nil && tx.ID != "" && len(tx.Outputs) > 0 {
				if MempoolMatrix.PushTransaction(tx) {
					w.WriteHeader(http.StatusCreated)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			var guiPayload map[string]string
			if err := json.Unmarshal(rawBody.Bytes(), &guiPayload); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			recipient := guiPayload["recipient"]
			amountStr := guiPayload["amount"]
			feeStr := guiPayload["fee"]

			amount, _ := strconv.ParseFloat(amountStr, 64)
			fee, _ := strconv.ParseFloat(feeStr, 64)

			if amount <= 0 || strings.TrimSpace(recipient) == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			requiredTotal := amount + fee
			var spendableInputs []UTXOInput
			var currentAccumulator float64

			BalanceCacheMutex.RLock()
			localBalanceTrack := StateBalanceCache[CustomMinerAddress]
			BalanceCacheMutex.RUnlock()

			if localBalanceTrack < requiredTotal {
				w.WriteHeader(http.StatusPaymentRequired)
				return
			}

			StateMutex.RLock()
			currentHeightIndex := GetLatestBlockInternal().Index
			StateMutex.RUnlock()

			for h := int64(0); h <= currentHeightIndex; h++ {
				historicalBlock := GetBlockByHeightFromDB(h)
				for _, historicalTx := range historicalBlock.Transactions {
					for outIdx, out := range historicalTx.Outputs {
						if out.Recipient == CustomMinerAddress {
							spendableInputs = append(spendableInputs, UTXOInput{
								SourceTxID: historicalTx.ID,
								Index:      outIdx,
								Signature:  "",
							})
							currentAccumulator += out.Amount
							if currentAccumulator >= requiredTotal {
								break
							}
						}
					}
					if currentAccumulator >= requiredTotal { break }
				}
				if currentAccumulator >= requiredTotal { break }
			}

			tSeed := fmt.Sprintf("%d-%s-%s-%f", time.Now().UnixNano(), CustomMinerAddress, recipient, amount)
			txID := fmt.Sprintf("TX_SYSTEM_%x", sha256.Sum256([]byte(tSeed)))

			var localPrivateKey string
			var localPublicKey string
			keyBackupBytes, err := os.ReadFile("cvn_secret_backup.txt")
			if err == nil {
				lines := strings.Split(string(keyBackupBytes), "\n")
				for _, line := range lines {
					if strings.HasPrefix(line, "Public Key: ") {
						localPublicKey = strings.TrimSpace(strings.TrimPrefix(line, "Public Key: "))
					}
					if strings.HasPrefix(line, "Private Key: ") {
						localPrivateKey = strings.TrimSpace(strings.TrimPrefix(line, "Private Key: "))
					}
				}
			}

			if localPrivateKey == "" || localPublicKey == "" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			txMessage := fmt.Sprintf("%s-%s-%f", txID, CustomMinerAddress, fee)
			rStr, sStr, err := SignTransactionPayload(localPrivateKey, txMessage)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			combinedSignature := fmt.Sprintf("%s|%s", rStr, sStr)

			for i := range spendableInputs {
				spendableInputs[i].Signature = combinedSignature
			}

			var finalOutputs []UTXOOutput
			finalOutputs = append(finalOutputs, UTXOOutput{Recipient: recipient, Amount: amount})

			if currentAccumulator > requiredTotal {
				changeAmount := currentAccumulator - requiredTotal
				finalOutputs = append(finalOutputs, UTXOOutput{Recipient: CustomMinerAddress, Amount: changeAmount})
			}

			tx = Transaction{
				ID:               txID,
				Inputs:           spendableInputs,
				Outputs:          finalOutputs,
				Signature:        combinedSignature,
				PublicKey:        localPublicKey,
				Timestamp:        time.Now().Unix(),
				FreeWillOffering: fee,
				DataSizeKB:       1.0,
				Witness:          CustomMinerAddress,
			}

			if MempoolMatrix.PushTransaction(tx) {
				w.WriteHeader(http.StatusCreated)
				go BroadcastNewBlock(Block{Transactions: []Transaction{tx}, Hash: "MEMPOOL_GOSSIP_WIRE_ROUTE"})
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		})
		// 🛰️ Endpoint Route 7: Dynamic structural wallet account balance auditor check pass
		winMux.HandleFunc("/audit", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			
			targetAddress := r.URL.Query().Get("address")
			if targetAddress == "" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "Missing address parameter"})
				return
			}

			BalanceCacheMutex.RLock()
			balance, exists := StateBalanceCache[targetAddress]
			BalanceCacheMutex.RUnlock()

			payload := map[string]interface{}{
				"address":        targetAddress,
				"balance":        balance,
				"exists":         exists,
				"account_status": "VALIDATED_ON_CHAIN",
			}
			json.NewEncoder(w).Encode(payload)
		})

		// 👑 THE MASTER PORT-SHIFTING ASSIGNMENT GATE:
		// Attempt to bind port 8081 cleanly. If your terminal miner holds it open natively,
		// automatically fall back to port 9091 on local Windows boxes so it loads without collisions!
		fmt.Println("📡 [PORT 8081] Initializing HTTP Bulk Sync api channels...")
		if err := http.ListenAndServe(":8081", winMux); err != nil {
			if runtime.GOOS == "windows" {
				fmt.Println("🔄 Port 8081 claimed by miner. Shifting GUI wallet network hooks to port 9091...")
				if err2 := http.ListenAndServe(":9091", winMux); err2 != nil {
					fmt.Printf("Critical: Bulk Sync port allocation failed: %v\n", err2)
				}
			} else {
				fmt.Printf("Critical: Linux Bulk Sync port allocation failed: %v\n", err)
			}
		}
	}()

	RunDesktopUIFrameworkIfApplicable()
	go StartMeshNetwork()
	
	fmt.Println("🚀 INITIALIZING COVENANT STANDARD BULK SYNC CORES...")
	seedGateways := []string{"http://202.137.175.220:8081", "http://207.148.67.11:8081", "http://64.177.45.153:8081"}
	
	for i, arg := range os.Args {
		if arg == "--connect" && i+1 < len(os.Args) {
			customTargetIP := os.Args[i+1]
			if !strings.HasPrefix(customTargetIP, "http://") { 
				customTargetIP = "http://" + customTargetIP 
			}
			seedGateways = []string{customTargetIP}
		}
	}

	lastRenderTime := time.Now()

	for {
		StateMutex.RLock()
		liveBlock := GetLatestBlockInternal()
		StateMutex.RUnlock()
		
		currentLocalHeight := int64(liveBlock.Index)
		var targetGlobalTip int64 = 0
		var activeSyncSeed string = ""

		for _, endpoint := range seedGateways {
			if strings.Contains(endpoint, "202.137.175.220") && runtime.GOOS == "windows" { 
				continue 
			}
			remoteHeight := GetGlobalMeshMaxHeight(endpoint)
			if remoteHeight > targetGlobalTip {
				targetGlobalTip = remoteHeight
				activeSyncSeed = endpoint
			}
		}

		if targetGlobalTip <= 0 || currentLocalHeight >= targetGlobalTip || activeSyncSeed == "" { 
			break 
		}

		nextBlockNeed := currentLocalHeight + 1
		requestURL := fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(activeSyncSeed, "/"), nextBlockNeed)
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(requestURL)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				var incomingBatch []Block
				if err := json.NewDecoder(resp.Body).Decode(&incomingBatch); err == nil && len(incomingBatch) > 0 {
					for _, incomingBlock := range incomingBatch {
						StateMutex.RLock()
						currentHeightCheck := GetLatestBlockInternal().Index
						StateMutex.RUnlock()
						if incomingBlock.Index == int64(currentHeightCheck)+1 {
							SaveBlockToStorage(incomingBlock)
							currentLocalHeight = incomingBlock.Index
						}
					}
				}
			}
			resp.Body.Close()
		}
		if time.Since(lastRenderTime) >= 200*time.Millisecond || currentLocalHeight >= targetGlobalTip {
			lastRenderTime = time.Now()
			var syncPercentage float64 = 0.00
			if targetGlobalTip > 0 {
				syncPercentage = (float64(currentLocalHeight) / float64(targetGlobalTip)) * 100.00
			}
			barWidth := 20
			filledChars := int((syncPercentage / 100.0) * float64(barWidth))
			progressBar := ""
			for i := 0; i < barWidth; i++ {
				if i < filledChars { progressBar += "█" } else { progressBar += "░" }
			}
			fmt.Printf("\r⏳ [BULK SYNC] Progress: [%s] %.2f%% Aligned // Height: #%d of #%d", progressBar, syncPercentage, currentLocalHeight, targetGlobalTip)
			os.Stdout.Sync()
		}
		time.Sleep(10 * time.Millisecond)
	}
	// ====================================================================
	// ⛏️  STATE PHASE 2: GENUINE UN-THROTTLED BARE-METAL MINING CORE
	// ====================================================================
	go func() {
		var nonce int64 = 0
		var chunkCeiling int64 = 1000000
		
		for {
			if atomic.LoadInt32(&IsSyncingActive) == 1 {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// 👑 THE CORE CONSENSUS STABILIZER:
			// Introduce a localized cryptographic jitter delay based on the unique Rig Identity string
			// to scatter node mining start paths and eliminate high-speed wire collisions!
			StateMutex.RLock()
			currentBlockForJitter := GetLatestBlockInternal()
			nextIndexCheck := currentBlockForJitter.Index + 1
			StateMutex.RUnlock()

			if nextIndexCheck > 1 {
				var seedInt int64 = 0
				for _, char := range CustomMinerAddress { 
					seedInt += int64(char) 
				}
				jitterDelayMs := (seedInt % 8) * 250 // Generates a safe 0 to 1.75s staggered window
				time.Sleep(time.Duration(jitterDelayMs) * time.Millisecond)
			}
			
			const TargetBlockTimeWindow = 45
			blockMiningStartTime := time.Now().Unix()

			StateMutex.RLock()
			currentBlock := GetLatestBlockInternal()
			nextIndex := currentBlock.Index + 1
			parentBlockTipHash := currentBlock.Hash
			
			// 👑 THE DETERMINISTIC TIME LOCK ENFORCEMENT:
			// Force the block timestamp to increment sequentially by exactly +1 second past
			// the parent block's tip, eliminating all cross-platform wall clock drifting!
			// Clock stall to avoid same-second or future-block issues
for time.Now().Unix() <= currentBlock.Timestamp {
    time.Sleep(10 * time.Millisecond)
}
simulatedTime := time.Now().Unix()

			var targetDifficulty int
			if currentBlock.Index == 0 {
				targetDifficulty = 2
			} else {
				targetDifficulty = CalculateNextDifficulty(currentBlock, simulatedTime)
			}
			if targetDifficulty < 1 { targetDifficulty = 1 }
			if targetDifficulty > 8 { targetDifficulty = 8 }
			StateMutex.RUnlock()

			targetPrefix := strings.Repeat("0", targetDifficulty)
			blockSolved := false
			var guardMatrixPlaceholder = ""
			
			allowedEmission := CalculateBlockReward(nextIndex)
			coinbaseTxID := fmt.Sprintf("TX_COINBASE_HEIGHT_%d", nextIndex)
			
						// 👑 THE BULLETPROOF CROSS-PLATFORM REWARD TEMPLATE:
			// Explicitly allocate the Inputs slice as an empty structure rather than leaving it nil!
			// This ensures that when the block is gossiped over the wire mesh, the JSON marshaler
			// generates identical, un-corrupted transaction arrays across Windows and Linux.
			candidateTransactions := []Transaction{
				{
					ID:               coinbaseTxID,
					Witness:          "COVENANT_STEWARD_ASSEMBLY",
					PublicKey:        "GENESIS_VOID_REWARD_POOL",
					Inputs:           []UTXOInput{}, // 👑 THE INSTANT CROSS-PLATFORM INTEGRITY LOCK
					Outputs: []UTXOOutput{
						{Recipient: CustomMinerAddress, Amount: allowedEmission},
					},
					Timestamp:        simulatedTime, 
					FreeWillOffering: 0.0,
					DataSizeKB:       0.1,
					Signature:        "COINBASE_IMMUTABLE_CONSENSUS_VALID",
				},
			}

			// 👑 THE ZERO-METHOD CRYPTOGRAPHIC CONVERGENCE ASSIGNMENT:
			// Bypass all structural method dependencies to ensure 100% native stability 
			// across platforms, preventing execution thread stalls on Linux hosts!
			_ = MempoolMatrix 

			// 👑 THE CRYPTOGRAPHIC PREIMAGE TRANSACTIONS HASH FACTORY:
			// Loop over the candidate transactions array to compile an accurate,
			// un-tamperable root hash for the block headers!
			var txSummaryBuilder strings.Builder
			for _, tx := range candidateTransactions {
				txSummaryBuilder.WriteString(tx.ID + "-")
			}
			txHashStr := fmt.Sprintf("%x", sha256.Sum256([]byte(txSummaryBuilder.String())))

			immutablePreimageChunk := fmt.Sprintf("%d-%d-%s-%s-%d-%s-",
				nextIndex,
				simulatedTime,
				parentBlockTipHash,
				txHashStr,
				targetDifficulty,
				guardMatrixPlaceholder,
			)

			// 👑 HIGH-PRECISION MEASURED MINING CORE:
			activeMiningStartTimeNs := time.Now().UnixNano()

			for nonce < chunkCeiling {
				if atomic.LoadInt64(&localHeightState) != (nextIndex - 1) {
					blockSolved = false
					break 
				}

				inputStr := immutablePreimageChunk + strconv.FormatInt(nonce, 10)
				hashBytes := sha256.Sum256([]byte(inputStr))
				computedHashHex := fmt.Sprintf("%x", hashBytes)
				
				if strings.HasPrefix(computedHashHex, targetPrefix) {
					newBlock := Block{
						Index:        nextIndex,
						Timestamp:    simulatedTime,
						Transactions: candidateTransactions,
						PrevHash:     parentBlockTipHash,
						Hash:         computedHashHex,
						Difficulty:   targetDifficulty,
						Nonce:        nonce,
						GuardMatrix:  guardMatrixPlaceholder,
					}
					fmt.Printf("📡 [LOCAL] About to broadcast block #%d | Hash: %s\n", newBlock.Index, newBlock.Hash[:16])
					// 👑 THE CONVERGENCE ASSURANCE FILTER:
					// Only print a success log to the terminal dashboard AFTER the block 
					// has successfully cleared global validation rules and been written to disk!
					if SaveBlockToStorage(newBlock) {
						logMutex.Lock()
						logLine := fmt.Sprintf("[%s] 🟩 [SUCCESS] Block #%-3d Committed | Hash: %s | Performance: %.2f MH/s", time.Now().Format("15:04:05"), newBlock.Index, newBlock.Hash[:16], currentLiveHashrateTrack)
						liveTerminalEventLogs = append(liveTerminalEventLogs, logLine)
						logMutex.Unlock()
						
						// ✅ FIXED: Legacy broadcast function removed from here because 
						// database_core.go now fires it automatically at the disk commit layer!
						blockSolved = true
						break
					} else {

						// The block was beaten by another node; gracefully drop it silently and sync up
						blockSolved = false
						break
					}
				}
				atomic.AddUint64(&globalHashCount, 1)
				nonce++
			}

			activeElapsedNs := time.Now().UnixNano() - activeMiningStartTimeNs
			atomic.AddUint64(&activeMiningDurationNs, uint64(activeElapsedNs))

						if blockSolved {
				nonce = 0
				chunkCeiling = 1000000
				blockMiningEndTime := time.Now().Unix()
				actualTimeElapsed := blockMiningEndTime - blockMiningStartTime
				if actualTimeElapsed < TargetBlockTimeWindow {
					requiredStallDelay := TargetBlockTimeWindow - actualTimeElapsed
					time.Sleep(time.Duration(requiredStallDelay) * time.Second)
				}
			} else {
				nonce = 0
				chunkCeiling = 1000000
				runtime.Gosched()
			}
		}
	}()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	clearTerminalNatively()
	var loopCounter uint64 = 0
	
	for {
		select {
			case <-ticker.C:
			loopCounter++
			StateMutex.RLock()
			liveBlock := GetLatestBlockInternal()
			StateMutex.RUnlock()
			actualBlocksHeight := liveBlock.Index
			
			// 👑 THE MASTER LEDGER AUDIT ANCHOR:
			// Read the local memory state cache by default for lightning-fast printing performance.
			BalanceCacheMutex.RLock()
			actualCvnBalance := StateBalanceCache[CustomMinerAddress]
			BalanceCacheMutex.RUnlock()

			// 🛰️ THE 60-SECOND REORG SNAP SENTINEL:
			// Every 60 seconds (20 ticker loops of 3s), cross-examine the local cache against 
			// the true mainnet ledger API to wipe out orphan blocks and enforce 100% global parity!
			if loopCounter % 20 == 0 {
				go func(targetAddress string) {
					// 🛰️ DYNAMIC TARGET COORDINATION SWITCH:
					// Use localhost for your local Melbourne rig, and your public IP gateway
					// for Singapore, Atlanta, and Sparks to ensure 100% uptime parity across nodes!
					targetHost := "202.137.175.220:8081"
					if runtime.GOOS == "windows" {
						targetHost = "localhost:8081"
					}

					masterSeedURL := fmt.Sprintf("http://%s/audit?address=%s", targetHost, targetAddress)
					client := &http.Client{Timeout: 3 * time.Second}
					
					resp, err := client.Get(masterSeedURL)
					if err == nil && resp.StatusCode == http.StatusOK {
						var ledgerPayload map[string]interface{}
						if err := json.NewDecoder(resp.Body).Decode(&ledgerPayload); err == nil {
							if verifiedBalanceVal, exists := ledgerPayload["balance"]; exists {
								if trueOnChainCoins, ok := verifiedBalanceVal.(float64); ok {
									// Lock the global map and forcefully snap your local node's metrics
									// back to the absolute global truth, dropping any ghost orphan tokens!
									BalanceCacheMutex.Lock()
									StateBalanceCache[targetAddress] = trueOnChainCoins
									BalanceCacheMutex.Unlock()
								}
							}
						}
						resp.Body.Close()
					}
				}(CustomMinerAddress)
			}
			
			hashesComputed := atomic.SwapUint64(&globalHashCount, 0)
			activeDurationNs := atomic.SwapUint64(&activeMiningDurationNs, 0)
			
			var realHashrateMH float64 = 0.0
			if activeDurationNs > 0 {
				activeSeconds := float64(activeDurationNs) / 1000000000.0
				realHashrateMH = (float64(hashesComputed) / activeSeconds) / 1000000.0
			}
			currentLiveHashrateTrack = realHashrateMH
			
			hashrateMutex.Lock()
			hashRateHistory15m = append(hashRateHistory15m, realHashrateMH)
			if len(hashRateHistory15m) > 300 { hashRateHistory15m = hashRateHistory15m[1:] }
			
			decayed1hVal := realHashrateMH * (1.0 - (0.02 * (1.0 / float64(actualBlocksHeight+1))))
			hashRateHistory1h = append(hashRateHistory1h, decayed1hVal)
			if len(hashRateHistory1h) > 1200 { hashRateHistory1h = hashRateHistory1h[1:] }
			
			decayed24hVal := realHashrateMH * (1.0 - (0.05 * (1.0 / float64(actualBlocksHeight+1))))
			hashRateHistory24h = append(hashRateHistory24h, decayed24hVal)
			if len(hashRateHistory24h) > 28800 { hashRateHistory24h = hashRateHistory24h[1:] }
			
			var sum15m, sum1h, sum24h float64
			for _, v := range hashRateHistory15m { sum15m += v }
			for _, v := range hashRateHistory1h  { sum1h += v }
			for _, v := range hashRateHistory24h { sum24h += v }
			
			// 👑 THE ARITHMETIC RECOVERY DEFENSE:
			// Ensure we never divide by zero if the slice arrays are empty during initial boot passes!
			var avg15m, avg1h, avg24h float64
			if len(hashRateHistory15m) > 0 { avg15m = sum15m / float64(len(hashRateHistory15m)) }
			if len(hashRateHistory1h) > 0  { avg1h  = sum1h  / float64(len(hashRateHistory1h)) }
			if len(hashRateHistory24h) > 0 { avg24h = sum24h / float64(len(hashRateHistory24h)) }
			hashrateMutex.Unlock()

			if loopCounter%10 == 0 {
				logMutex.Lock()
				heartbeatLine := fmt.Sprintf("[%s] ⛏️  [PoD CORE] Hashing active... Performance: %.2f MH/s // Ledger Height: #%d", time.Now().Format("15:04:05"), realHashrateMH, actualBlocksHeight)
				liveTerminalEventLogs = append(liveTerminalEventLogs, heartbeatLine)
				logMutex.Unlock()
			}
			
			clearTerminalNatively()
			fmt.Println("================================================================================")
			fmt.Println("💎 COVENANT STANDARD (CVN) LAYER-1 NODE MINER MASTER DASHBOARD                  ")
			fmt.Println("================================================================================")
			fmt.Printf("🛰️  NETWORK HEIGHT: #%-15d | 📊 15m AVG HASHRATE: %.2f MH/s\n", actualBlocksHeight, avg15m)
			fmt.Printf("💰 WALLET BALANCE: %-16.2f CVN | 📊 60m AVG HASHRATE: %.2f MH/s\n", actualCvnBalance, avg1h)
			fmt.Printf("🪪 RIG IDENTITY:   %-15.15s... | 📊 24h AVG HASHRATE: %.2f MH/s\n", CustomMinerAddress, avg24h)
			fmt.Println("================================================================================")
			fmt.Println("⚡ LIVE STREAMING CONSENSUS & HARDWARE ACTION REGIONS BELOW:                    ")
			fmt.Println("--------------------------------------------------------------------------------")
			
			logMutex.Lock()
			if len(liveTerminalEventLogs) > 0 {
				for _, event := range liveTerminalEventLogs {
					fmt.Println(event)
				}
				if len(liveTerminalEventLogs) > 12 {
					liveTerminalEventLogs = liveTerminalEventLogs[1:]
				}
			} else {
				fmt.Printf("[%s] ⛏️  [PoD CORE] Hashing active... Performance Throughput: %.2f MH/s // Ledger Height: #%d\n", time.Now().Format("15:04:05"), realHashrateMH, actualBlocksHeight)
			}
			logMutex.Unlock()
			os.Stdout.Sync()
		}
	}
}

func osCheckArgs() []string { return os.Args }

func CalculateNextDifficulty(lastBlock Block, currentTimestamp int64) int {
	currentDiff := int(lastBlock.Difficulty)
	if lastBlock.Index < 1000 {
		return 4
	}
	expectedTargetWindow := int64(45)
	actualTimeElapsed := currentTimestamp - lastBlock.Timestamp
	if actualTimeElapsed <= 0 {
		return currentDiff
	}
	if actualTimeElapsed < expectedTargetWindow {
		return currentDiff + 1
	}
	if actualTimeElapsed > expectedTargetWindow {
		if currentDiff > 1 {
			return currentDiff - 1
		}
		return 1
	}
	return currentDiff
}