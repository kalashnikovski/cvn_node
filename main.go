package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"log"
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
var isResettingActive int32 = 0

// Local wallet account statement memory cache tracking fields
var actualCvnBalance float64 = 0.0

func clearTerminalNatively() {}
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

			walletAddress = strings.TrimSpace(walletAddress)
			walletAddress = strings.ReplaceAll(walletAddress, " ", "")
			walletAddress = strings.ReplaceAll(walletAddress, "\r", "")
			walletAddress = strings.ReplaceAll(walletAddress, "\n", "")

			fmt.Println("\n====================================================================")
			fmt.Println("💎 NEW SOVEREIGN COVENANT Standard KEYPAIR MATERIALIZATION DETECTED  ")
			fmt.Println("====================================================================")
			fmt.Printf("🪪  PUBLIC WALLET ADDRESS:  %s\n", walletAddress)
			fmt.Printf("📋  UNTRUNCATED PUBLIC KEY: %s\n", pubHex)
			fmt.Printf("🔐  SECRET PRIVATE KEY HEX: %s\n", privHex)
			fmt.Println("====================================================================")
			fmt.Println("⚠️  CRITICAL PROTOCOL SAFETY WARNING:                                ")
			fmt.Println("👉 COPY AND RECORD THE SECRET PRIVATE KEY DISPLAYED ABOVE MANUALLY NOW. ")
			fmt.Println("👉 FOR SECURITY, IT IS EXCLUSIVELY STREAMED TO THIS WINDOW BUFFER.   ")
			fmt.Println("❌ IT IS NO LONGER SAVED AS A PLANTEXT SECRETS REPOSITORY FILE ON DISK. ")
			fmt.Println("====================================================================")

			configMap := map[string]string{"miner_address": walletAddress}
			configMapBytes, err := json.MarshalIndent(configMap, "", "  ")
			if err == nil {
				err = os.WriteFile("miner_config.json", configMapBytes, 0644)
				if err != nil {
					log.Printf("🚨 FILE FAULT: Unable to write public configuration to disk: %v\n", err)
				}
			}
			os.Exit(0)
		}
	}

	if _, err := os.Stat("miner_config.json"); os.IsNotExist(err) {
		privHex, pubHex, walletAddress, err := GenerateKeyPair()
		if err == nil {
			_ = privHex
			_ = pubHex
			walletAddress = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(walletAddress, "\r", ""), "\n", ""), " ", ""))
			configMap := map[string]string{"miner_address": walletAddress}
			configMapBytes, _ := json.MarshalIndent(configMap, "", "  ")
			_ = os.WriteFile("miner_config.json", configMapBytes, 0644)
		}
	}

	InitBoltEngine()

	// Sync atomic architecture tracker to database height upon boot
	StateMutex.RLock()
	atomic.StoreUint64(&GlobalHeightSolverLatch, uint64(GetLatestBlockInternal().Index))
	StateMutex.RUnlock()

	var isolatedAddressTrack string = ""
	for i, arg := range os.Args {
		if arg == "--miner-address" && i+1 < len(os.Args) {
			isolatedAddressTrack = os.Args[i+1]
		}
	}

	if strings.TrimSpace(isolatedAddressTrack) == "" {
		configFileBytes, err := os.ReadFile("miner_config.json")
		if err == nil {
			var parsedConfig map[string]string
			if json.Unmarshal(configFileBytes, &parsedConfig) == nil {
				isolatedAddressTrack = parsedConfig["miner_address"]
			}
		}
	}

	isolatedAddressTrack = strings.TrimSpace(isolatedAddressTrack)
	isolatedAddressTrack = strings.ReplaceAll(isolatedAddressTrack, " ", "")
	isolatedAddressTrack = strings.ReplaceAll(isolatedAddressTrack, "\r", "")
	isolatedAddressTrack = strings.ReplaceAll(isolatedAddressTrack, "\n", "")

	if RanchAddress := os.Getenv("CVN_MINER_ADDRESS"); RanchAddress != "" {
		CustomMinerAddress = RanchAddress
	} else if isolatedAddressTrack != "" && !strings.Contains(isolatedAddressTrack, "UNCONFIGURED") {
		CustomMinerAddress = isolatedAddressTrack
	} else {
		hostName, hostErr := os.Hostname()
		if hostErr == nil {
			CustomMinerAddress = fmt.Sprintf("CVN_FALLBACK_%s_%d", strings.ToUpper(hostName), time.Now().Unix()%1000)
		} else {
			CustomMinerAddress = "CVN_UNCONFIGURED_LOCAL_NODE_ID"
		}
	}

	log.Printf("🪪  [CONSENSUS AUTHORITY ACTIVE] Mining pool node verified handle: %s\n", CustomMinerAddress)

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
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Content-Type", "application/json")

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
			latest := GetLatestBlockInternal()
			payload := map[string]interface{}{
				"current_height": latest.Index,
				"block_hash":     latest.Hash,
				"timestamp":      latest.Timestamp,
				"transactions":   latest.Transactions,
			}
			json.NewEncoder(w).Encode(payload)
		})

		winMux.HandleFunc("/transaction", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Content-Type", "application/json")
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
					if currentAccumulator >= requiredTotal {
						break
					}
				}
				if currentAccumulator >= requiredTotal {
					break
				}
			}

			tSeed := fmt.Sprintf("%d-%s-%s-%f", time.Now().UnixNano(), CustomMinerAddress, recipient, amount)
			txID := fmt.Sprintf("TX_SYSTEM_%x", sha256.Sum256([]byte(tSeed)))

			localPrivateKey := os.Getenv("CVN_SECRET_SIGNING_KEY")
			localPublicKey := os.Getenv("CVN_PUBLIC_SIGNING_KEY")
			if localPrivateKey == "" || localPublicKey == "" {
				localPrivateKey = "TEST_ENVIRONMENT_LOCAL_DEFAULT_SECRET_KEY"
				localPublicKey = "TEST_ENVIRONMENT_LOCAL_DEFAULT_PUBLIC_KEY"
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
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Content-Type", "application/json")

			targetAddress := r.URL.Query().Get("address")
			if targetAddress == "" {
				w.WriteHeader(http.StatusBadRequest)
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
				_ = http.ListenAndServe(":9091", winMux)
			}
		}
	}()

	go StartMeshNetwork()

	mirrorMux := http.NewServeMux()
	mirrorMux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		StateMutex.RLock()
		tip := GetLatestBlockInternal()
		StateMutex.RUnlock()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "ONLINE",
			"height":   tip.Index,
			"tip_hash": tip.Hash,
		})
	})
	go func() { _ = http.ListenAndServe(":8082", mirrorMux) }()

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
		atomic.StoreInt32(&IsSyncingActive, 1)

		nextBlockNeed := currentLocalHeight + 1
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(activeSyncSeed, "/"), nextBlockNeed))
		var incomingBatch []Block
		if err != nil {
			incomingBatch = requestBatchOverTCPMeshNatively(nextBlockNeed)
			if len(incomingBatch) == 0 {
				break
			}
		} else {
			if resp.StatusCode == http.StatusOK {
				_ = json.NewDecoder(resp.Body).Decode(&incomingBatch)
			}
			resp.Body.Close()
		}
		StateMutex.Lock()
		for _, block := range incomingBatch {
			if block.Index == int64(GetLatestBlockInternal().Index)+1 {
				if ValidateBlockSignatures(block) {
					if block.UncleHash == "" {
						block.UncleHash = "0000000000000000000000000000000000000000000000000000000000000000"
					}
					if block.UncleMiner == "" {
						block.UncleMiner = "NONE"
					}
					SaveBlockToStorageInternal(block)
					currentLocalHeight = block.Index
				}
			}
		}
		StateMutex.Unlock()
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
				if i < filledChars {
					progressBar += "█"
				} else {
					progressBar += "░"
				}
			}
			fmt.Printf("\r⏳ [BULK SYNC] Progress: [%s] %.2f%% Aligned // Height: #%d of #%d", progressBar, syncPercentage, currentLocalHeight, targetGlobalTip)
			os.Stdout.Sync()
		}
		time.Sleep(10 * time.Millisecond)
	}
	atomic.StoreInt32(&IsSyncingActive, 0)
	StartContextMinerEngine()

	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	_ = exec.Command("cmd", "/c", "cls").Run()
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
			localMemoryCoins := StateBalanceCache[CustomMinerAddress]
			BalanceCacheMutex.RUnlock()
			if loopCounter == 1 || loopCounter%15 == 0 {
				go func(targetAddress string) {
					client := &http.Client{Timeout: 2 * time.Second}
					resp, err := client.Get(fmt.Sprintf("http://localhost:8081/audit?address=%s", targetAddress))
					if err == nil && resp.StatusCode == http.StatusOK {
						var ledgerPayload map[string]interface{}
						if err := json.NewDecoder(resp.Body).Decode(&ledgerPayload); err == nil {
							if verifiedBalanceVal, exists := ledgerPayload["balance"]; exists {
								if trueOnChainCoins, ok := verifiedBalanceVal.(float64); ok {
									logMutex.Lock()
									actualCvnBalance = trueOnChainCoins
									logMutex.Unlock()
									return
								}
							}
						}
						resp.Body.Close()
					}
					logMutex.Lock()
					actualCvnBalance = localMemoryCoins
					logMutex.Unlock()
				}(CustomMinerAddress)
			} else {
				logMutex.Lock()
				actualCvnBalance = localMemoryCoins
				logMutex.Unlock()
			}
			hashesComputedSnapshot := atomic.SwapUint64(&globalHashCount, 0)
			realHashrateMH := (float64(hashesComputedSnapshot) / 4.0) / 1000000.0
			currentLiveHashrateTrack = realHashrateMH
			hashrateMutex.Lock()
			hashRateHistory15m = append(hashRateHistory15m, realHashrateMH)
			if len(hashRateHistory15m) > 225 {
				hashRateHistory15m = hashRateHistory15m[1:]
			}
			hashRateHistory1h = append(hashRateHistory1h, realHashrateMH)
			if len(hashRateHistory1h) > 900 {
				hashRateHistory1h = hashRateHistory1h[1:]
			}
			hashRateHistory24h = append(hashRateHistory24h, realHashrateMH)
			if len(hashRateHistory24h) > 21600 {
				hashRateHistory24h = hashRateHistory24h[1:]
			}
			var sum15m float64
			for _, v := range hashRateHistory15m {
				sum15m += v
			}
			var avg15m float64
			if len(hashRateHistory15m) > 0 {
				avg15m = sum15m / float64(len(hashRateHistory15m))
			}
			hashrateMutex.Unlock()
			if loopCounter%10 == 0 {
				logMutex.Lock()
				liveTerminalEventLogs = append(liveTerminalEventLogs, fmt.Sprintf("[%s] ⛏️  [PoD CORE] Hashing active... Performance: %.2f MH/s // Ledger Height: #%d", time.Now().Format("15:04:05"), realHashrateMH, actualBlocksHeight))
				logMutex.Unlock()
			}
			logMutex.Lock()
			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				cmd = exec.Command("cmd", "/c", "cls")
			} else {
				cmd = exec.Command("clear")
			}
			cmd.Stdout = os.Stdout
			_ = cmd.Run()
			fmt.Println("================================================================================")
			fmt.Println("💎 COVENANT STANDARD (CVN) LAYER-1 NODE MINER MASTER DASHBOARD                  ")
			fmt.Println("================================================================================")
			fmt.Printf("🛰️  NETWORK HEIGHT: #%-15d | 📊 15m AVG HASHRATE: %.2f MH/s\n", actualBlocksHeight, avg15m)
			fmt.Printf("💰 WALLET BALANCE: %-16.2f CVN | 🪪 RIG IDENTITY: %-15.15s...\n", actualCvnBalance, CustomMinerAddress)
			fmt.Println("================================================================================")
			fmt.Println("⚡ LIVE STREAMING CONSENSUS & HARDWARE ACTION REGIONS BELOW:                    ")
			fmt.Println("--------------------------------------------------------------------------------")
			if len(liveTerminalEventLogs) > 0 {
				for _, event := range liveTerminalEventLogs {
					fmt.Println(event)
				}
				if len(liveTerminalEventLogs) > 12 {
					liveTerminalEventLogs = liveTerminalEventLogs[1:]
				}
			} else {
				fmt.Printf("[%s] ⛏️  [PoD CORE] Hashing active... Performance Throughput: %.2f MH/s // Height: #%d\n", time.Now().Format("15:04:05"), realHashrateMH, actualBlocksHeight)
			}
			logMutex.Unlock()
			os.Stdout.Sync()
		}
	}
}

func StartContextMinerEngine() {
	miningMutex.Lock()
	if miningCancelFunc != nil {
		miningCancelFunc()
	}
	miningContext, miningCancelFunc = context.WithCancel(context.Background())
	miningMutex.Unlock()
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
	if !atomic.CompareAndSwapInt32(&isResettingActive, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&isResettingActive, 0)
	miningMutex.Lock()
	if miningCancelFunc != nil {
		miningCancelFunc()
	}
	miningMutex.Unlock()
	miningWg.Wait()
	StartContextMinerEngine()
}

// Global atomic height solver latch register
var GlobalHeightSolverLatch uint64

func executionMiningWorkerThread(ctx context.Context, workerID int64) {
	defer miningWg.Done()
	var nonce int64 = workerID * 100000000
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
			currentUnixTime := time.Now().Unix()
			elapsedSinceLastTip := currentUnixTime - currentBlock.Timestamp
			const GlobalTargetBlockTimeFloor = int64(45)
			if currentBlock.Index > 0 && elapsedSinceLastTip < GlobalTargetBlockTimeFloor {
				deficitSeconds := GlobalTargetBlockTimeFloor - elapsedSinceLastTip
				if deficitSeconds > 0 && deficitSeconds <= GlobalTargetBlockTimeFloor {
					time.Sleep(time.Duration(deficitSeconds) * time.Second)
				}
			}

			// Re-read tip after possible sleep so we do not mine on a stale parent
			StateMutex.RLock()
			currentBlock = GetLatestBlockInternal()
			StateMutex.RUnlock()
			if currentBlock.Index+1 != nextIndex {
				continue
			}
			parentBlockTipHash = currentBlock.Hash

			simulatedTime := time.Now().Unix()
			if simulatedTime <= currentBlock.Timestamp {
				simulatedTime = currentBlock.Timestamp + 1
			}

			var workerTargetDifficulty int
			if currentBlock.Index <= 9 {
				workerTargetDifficulty = 2
			} else {
				workerTargetDifficulty = CalculateNextDifficulty(currentBlock, simulatedTime)
				if workerTargetDifficulty < 4 {
					workerTargetDifficulty = 4
				}
			}
			if workerTargetDifficulty < 1 {
				workerTargetDifficulty = 1
			}
			if workerTargetDifficulty > 8 {
				workerTargetDifficulty = 8
			}
			targetPrefix := strings.Repeat("0", workerTargetDifficulty)

			coinbaseTxID := fmt.Sprintf("TX_COINBASE_H%d_%x", nextIndex, sha256.Sum256([]byte(fmt.Sprintf("%d-%d", nextIndex, nonce))))
			var coinbaseOutputs []UTXOOutput
			allowedEmission := CalculateBlockReward(nextIndex)
			coinbaseOutputs = append(coinbaseOutputs, UTXOOutput{Recipient: CustomMinerAddress, Amount: allowedEmission})
			candidateTransactions := []Transaction{
				{
					Version:          1,
					ID:               coinbaseTxID,
					Witness:          CustomMinerAddress,
					PublicKey:        "GENESIS_VOID_REWARD_POOL",
					Inputs:           []UTXOInput{},
					Outputs:          coinbaseOutputs,
					Timestamp:        simulatedTime,
					Signature:        "COINBASE_IMMUTABLE_CONSENSUS_VALID",
				},
			}

			for i := 0; i < 100000; i++ {
				nonce++
				atomic.AddUint64(&globalHashCount, 1)

				StateMutex.RLock()
				actualTipCheck := GetLatestBlockInternal().Index
				StateMutex.RUnlock()
				if actualTipCheck != (nextIndex - 1) {
					break
				}

				// Canonical preimage path — identical on Windows and Linux
				candidate := Block{
					Version:      1,
					Index:        nextIndex,
					Timestamp:    simulatedTime,
					Transactions: candidateTransactions,
					PrevHash:     parentBlockTipHash,
					Difficulty:   workerTargetDifficulty,
					Nonce:        nonce,
					GuardMatrix:  "",
					UncleHash:    "0000000000000000000000000000000000000000000000000000000000000000",
					UncleMiner:   "NONE",
				}
				computedHashHex := CalculateHash(candidate)
				if !strings.HasPrefix(computedHashHex, targetPrefix) {
					continue
				}

				// Atomic claim of this height
				if !atomic.CompareAndSwapUint64(&GlobalHeightSolverLatch, uint64(nextIndex-1), uint64(nextIndex)) {
					break
				}

				candidate.Hash = computedHashHex
				// No platform-specific sleep. Both nodes save and broadcast immediately.
				fmt.Printf("\n🟩 [SUCCESS] Block #%d Mined by Worker %d | Hash: %s\n", candidate.Index, workerID, candidate.Hash[:16])
				if SaveBlockToStorage(candidate) {
					go BroadcastNewBlock(candidate)
					SignalMinerReset()
				} else {
					atomic.StoreUint64(&GlobalHeightSolverLatch, uint64(nextIndex-1))
				}
				break
			}
		}
	}
}
