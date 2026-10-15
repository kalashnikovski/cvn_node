package main

import (
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

		winMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			htmlBytes, err := os.ReadFile("explorer.html")
			if err != nil {
				StateMutex.RLock()
				idx := GetLatestBlockInternal().Index
				StateMutex.RUnlock()
				fmt.Fprintf(w, "<h2>📊 Covenant Standard Core Matrix Active</h2><p>Rig Height: #%d</p>", idx)
				return
			}
			w.Write(htmlBytes)
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
			var tx Transaction
			if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if MempoolMatrix.PushTransaction(tx) {
				w.WriteHeader(http.StatusCreated)
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

		winMux.HandleFunc("/addresses", func(w http.ResponseWriter, r *http.Request) {
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
		})

		winMux.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")

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
		})

		fmt.Println("📡 [PORT 8081] Exposing un-throttled HTTP Bulk Sync channels globally...")
		if err := http.ListenAndServe(":8081", winMux); err != nil {
			fmt.Printf("Port 8081 bind alert: %v\n", err)
		}
	}()

	go func() {
		mirrorMux := http.NewServeMux()
		mirrorMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			StateMutex.RLock()
			h := GetLatestBlockInternal().Index
			StateMutex.RUnlock()
			fmt.Fprintf(w, "<h2>💎 Covenant Standard Core Active Matrix</h2><p>Rig Height: #%d</p>", h)
		})
		
		fmt.Println("📡 [PORT 8082] Exposing dedicated analytics data matrix globally...")
		if err := http.ListenAndServe(":8082", mirrorMux); err != nil {
			fmt.Printf("Port 8082 bind alert: %v\n", err)
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
const TargetBlockTimeWindow = 45
blockMiningStartTime := time.Now().Unix()
StateMutex.RLock()
currentBlock := GetLatestBlockInternal()
nextIndex := currentBlock.Index + 1
parentBlockTipHash := currentBlock.Hash
simulatedTime := time.Now().Unix()
if simulatedTime <= currentBlock.Timestamp {
simulatedTime = currentBlock.Timestamp + 1
}
// 👑 HARD-LOCKED DIFFICULTY PARITY:
// Lock system to difficulty 4 natively during initial epochs to eradicate cross-VP timing forks.
targetDifficulty := 4
StateMutex.RUnlock()
targetPrefix := strings.Repeat("0", targetDifficulty)
blockSolved := false
var guardMatrixPlaceholder = ""
			allowedEmission := CalculateBlockReward(nextIndex)
			
			// 👑 THE DEFINITIVE PREFIX SYNCHRONIZATION:
			// Ensure the Coinbase Transaction ID uses standard underscore formatting 
			// to easily satisfy the strict HasPrefix validation rules in blockchain_core!
			coinbaseTxID := fmt.Sprintf("TX_COINBASE_%d", nextIndex)
			
			candidateTransactions := []Transaction{
				{
					ID:               coinbaseTxID,
					Witness:          "COVENANT_STEWARD_ASSEMBLY",
					PublicKey:        "GENESIS_VOID_REWARD_POOL",
					Outputs: []UTXOOutput{
						{Recipient: CustomMinerAddress, Amount: allowedEmission},
					},
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
immutablePreimageChunk := fmt.Sprintf("%d-%d-%s-%s-%d-%s-",
nextIndex,
simulatedTime,
parentBlockTipHash,
txHashStr,
targetDifficulty,
guardMatrixPlaceholder,
)
for nonce < chunkCeiling {
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
if SaveBlockToStorage(newBlock) {
logMutex.Lock()
logLine := fmt.Sprintf("[%s] 🟩 [SUCCESS] Solved valid block #%-3d | Hash: %s | Performance: %.2f MH/s", time.Now().Format("15:04:05"), newBlock.Index, newBlock.Hash[:16], currentLiveHashrateTrack)
liveTerminalEventLogs = append(liveTerminalEventLogs, logLine)
logMutex.Unlock()
go BroadcastNewBlock(newBlock)
}
blockSolved = true
break
}
atomic.AddUint64(&globalHashCount, 1)
nonce++
}
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
chunkCeiling = nonce + 1000000
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
StateMutex.RLock()
actualCvnBalance := GetAddressBalance(CustomMinerAddress)
StateMutex.RUnlock()
hashesComputed := atomic.SwapUint64(&globalHashCount, 0)
realHashrateMH := (float64(hashesComputed) / 3.0) / 1000000.0
currentLiveHashrateTrack = realHashrateMH
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
avg15m := sum15m / float64(len(hashRateHistory15m))
avg1h  := sum1h / float64(len(hashRateHistory1h))
avg24h := sum24h / float64(len(hashRateHistory24h))
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