package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed all:frontend/dist
var assets embed.FS

// Globally scoped atomic counters and monitoring metrics
var globalHashCount uint64
var liveTerminalEventLogs []string
var logMutex sync.Mutex

// Moving window registers for tracking rolling average throughput metrics
var hashRateHistory15m []float64
var hashRateHistory1h  []float64
var hashRateHistory24h []float64
var hashrateMutex      sync.Mutex

var currentLiveHashrateTrack float64
var activeMiningDurationNs uint64

// Mining Worker Pool orchestration controls
var miningCancelFunc context.CancelFunc
var miningWg         sync.WaitGroup
var miningContext    context.Context
var miningMutex      sync.Mutex

func clearTerminalNatively() {
	// Reverted to raw ANSI string controls inside the main dashboard loop to kill console flickering!
}

func initWindowsAnsi() {}

func main() {
	initWindowsAnsi()

	// Parse custom command-line overrides prior to launching core daemons
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

	// Verify or auto-provision standard baseline environment configuration profiles safely
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

	// Construct and launch our unified multi-port API server engine routing context
	go func() {
		winMux := http.NewServeMux()

		winMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			if r.URL.Path == "/addresses" {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Access-Control-Allow-Origin", "*")
				
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
					SignalMinerReset()
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
				SignalMinerReset()
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		})

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
			if strings.Contains(endpoint, "207.148.67.11") && os.Getenv("CVN_RELAY_ONLY") == "1" {
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
		atomic.StoreInt32(&IsSyncingActive, 1)
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
							if incomingBlock.UncleHash == "" {
								incomingBlock.UncleHash = "0000000000000000000000000000000000000000000000000000000000000000"
							}
							if incomingBlock.UncleMiner == "" {
								incomingBlock.UncleMiner = "NONE"
							}
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
	atomic.StoreInt32(&IsSyncingActive, 0)
	// ====================================================================
	// ⛏️ STATE PHASE 2: HARDENED DYNAMIC CONTEXT MINING CORE WORKER POOL
	// ====================================================================
	StartContextMinerEngine()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	
	// Clear out the whole window completely once on bootstrap ignition pass
	fmt.Print("\033[2J\033[H")
	var loopCounter uint64 = 0

	for {
		select {
		case <-ticker.C:
			loopCounter++
			StateMutex.RLock()
			liveBlock := GetLatestBlockInternal()
			StateMutex.RUnlock()
			actualBlocksHeight := liveBlock.Index

			BalanceCacheMutex.RLock()
			actualCvnBalance := StateBalanceCache[CustomMinerAddress]
			BalanceCacheMutex.RUnlock()

			// Routine validation check targeting our peer-to-peer live state matrices
			if loopCounter%20 == 0 {
				go func(targetAddress string) {
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
			if len(hashRateHistory15m) > 300 {
				hashRateHistory15m = hashRateHistory15m[1:]
			}
			decayed1hVal := realHashrateMH * (1.0 - (0.02 * (1.0 / float64(actualBlocksHeight+1))))
			hashRateHistory1h = append(hashRateHistory1h, decayed1hVal)
			if len(hashRateHistory1h) > 1200 {
				hashRateHistory1h = hashRateHistory1h[1:]
			}
			decayed24hVal := realHashrateMH * (1.0 - (0.05 * (1.0 / float64(actualBlocksHeight+1))))
			hashRateHistory24h = append(hashRateHistory24h, decayed24hVal)
			if len(hashRateHistory24h) > 28800 {
				hashRateHistory24h = hashRateHistory24h[1:]
			}

			var sum15m, sum1h, sum24h float64
			for _, v := range hashRateHistory15m { sum15m += v }
			for _, v := range hashRateHistory1h  { sum1h += v }
			for _, v := range hashRateHistory24h { sum24h += v }

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

			// 👑 RESTORE LEGACY ANSI TERMINAL ENGINE RE-RENDERING LOOPS:
			// \033[H snaps the cursor back to the top left position cleanly.
			// This completely removes background process spawning, eliminating screen flickering!
			fmt.Print("\033[H")
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
			// Clear all trailing text from the current line down to the bottom viewport boundary
			fmt.Print("\033[J")
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

func StartContextMinerEngine() {
	miningMutex.Lock()
	defer miningMutex.Unlock()

	miningContext, miningCancelFunc = context.WithCancel(context.Background())
	coresToAllocate := runtime.NumCPU()
	if coresToAllocate < 1 {
		coresToAllocate = 1
	}

	for i := 0; i < coresToAllocate; i++ {
		miningWg.Add(1)
		go executionMiningWorkerThread(miningContext, int64(i))
	}
}

func SignalMinerReset() {
	miningMutex.Lock()
	if miningCancelFunc != nil {
		miningCancelFunc()
	}
	miningWg.Wait()
	miningMutex.Unlock()

	StartContextMinerEngine()
}

func executionMiningWorkerThread(ctx context.Context, workerID int64) {
	defer miningWg.Done()

	var nonce int64 = workerID * 100000000
	var batchTracker uint64 = 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if atomic.LoadInt32(&IsSyncingActive) == 1 {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			StateMutex.RLock()
			currentBlock := GetLatestBlockInternal()
			StateMutex.RUnlock()

			nextIndex := currentBlock.Index + 1
			parentBlockTipHash := currentBlock.Hash
			simulatedTime := time.Now().Unix()
			if simulatedTime <= currentBlock.Timestamp {
				simulatedTime = currentBlock.Timestamp + 1
			}

			var targetDifficulty int
			if nextIndex <= 10 {
				targetDifficulty = 2
			} else {
				targetDifficulty = 4
			}
			
			if targetDifficulty < 1 { targetDifficulty = 1 }
			if targetDifficulty > 8 { targetDifficulty = 8 }

			targetPrefix := strings.Repeat("0", targetDifficulty)
			var guardMatrixPlaceholder = ""
			allowedTotalEmission := CalculateBlockReward(nextIndex)
			coinbaseTxID := fmt.Sprintf("TX_COINBASE_HEIGHT_%d", nextIndex)
			coinbaseOutputs := []UTXOOutput{
				{Recipient: CustomMinerAddress, Amount: allowedTotalEmission},
			}
			targetUncleHash := "0000000000000000000000000000000000000000000000000000000000000000"
			targetUncleMiner := "NONE"

			candidateTransactions := []Transaction{
				{
					ID:               coinbaseTxID,
					Witness:          CustomMinerAddress,
					PublicKey:        "GENESIS_VOID_REWARD_POOL",
					Inputs:           []UTXOInput{},
					Outputs:          coinbaseOutputs,
					Timestamp:        simulatedTime,
					FreeWillOffering: 0.0,
					DataSizeKB:       0.1,
					Signature:        "COINBASE_IMMUTABLE_CONSENSUS_VALID",
				},
			}

			var txSummaryBuilder strings.Builder
			for _, tx := range candidateTransactions {
				txSummaryBuilder.WriteString(tx.ID + "-")
			}
			txHashStr := fmt.Sprintf("%x", sha256.Sum256([]byte(txSummaryBuilder.String())))
			activeMiningStartTimeNs := time.Now().UnixNano()

			for i := 0; i < 50000; i++ {
				if atomic.LoadInt64(&localHeightState) != (nextIndex - 1) {
					break
				}

				// 👑 FIXED PREIMAGE ALIGNMENT PATTERN:
				// Guarantees that the string representation matches exactly what verification
				// layers expect, eliminating false successes on high-speed hardware!
				inputStr := fmt.Sprintf("%d-%d-%s-%s-%d-%s-%d-%s-%s",
					nextIndex,
					simulatedTime,
					parentBlockTipHash,
					txHashStr,
					int64(targetDifficulty),
					guardMatrixPlaceholder,
					nonce,
					targetUncleHash,
					targetUncleMiner,
				)

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
						UncleHash:    targetUncleHash,
						UncleMiner:   targetUncleMiner,
					}

					if SaveBlockToStorage(newBlock) {
						logMutex.Lock()
						logLine := fmt.Sprintf("[%s] 🟩 [SUCCESS] Block #%-3d Mined by Worker %d | Hash: %s", time.Now().Format("15:04:05"), newBlock.Index, workerID, newBlock.Hash[:16])
						liveTerminalEventLogs = append(liveTerminalEventLogs, logLine)
						logMutex.Unlock()
						
						go SignalMinerReset()
						return
					}
					
					// 👑 IMMUTABLE ESCAPE VALVE BREAKOUT:
					// Instantly terminate thread processing if another core beats us to disk write commitments!
					go SignalMinerReset()
					return
				}
				batchTracker++
				nonce++
			}

			activeElapsedNs := time.Now().UnixNano() - activeMiningStartTimeNs
			atomic.AddUint64(&activeMiningDurationNs, uint64(activeElapsedNs))
			atomic.AddUint64(&globalHashCount, batchTracker)
batchTracker = 0
runtime.Gosched()
}
}
}
func osCheckArgs() []string { return os.Args }