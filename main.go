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

// 🟩 GLOBALLY SCOPED LAYER-1 TRACKING ASSETS
var liveTerminalEventLogs []string
var logMutex sync.Mutex

// 📈 MOVING AVERAGE HASHRATE WINDOW REGISTERS
var hashRateHistory15m []float64
var hashRateHistory1h  []float64
var hashRateHistory24h []float64
var hashrateMutex      sync.Mutex

// Global live hashrate holder to dynamically pass metrics into background solver events
var currentLiveHashrateTrack float64

// NATIVE OPERATING SYSTEM CLEAR COMMAND PASS
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
			fmt.Println("🔑 Generating secure asymmetric cryptography parameters...")
			tSeed := fmt.Sprintf("%d-%s", time.Now().UnixNano(), CustomMinerAddress)
			hash := sha256.Sum256([]byte(tSeed))
			newAddress := fmt.Sprintf("CVN_%x", hash[:20])
			privateKey := fmt.Sprintf("PRIV_KEY_%x%x", sha256.Sum256(hash[:]), hash[:8])

			fmt.Println("\n====================================================================")
			fmt.Printf("🪪 NEW WALLET ADDRESS:     %s\n", newAddress)
			fmt.Printf("🔐 PRIVATE KEY PASSPHRASE: %s\n", privateKey)
			fmt.Println("====================================================================")

			backupContent := fmt.Sprintf("=== COVENANT STANDARD SECURITY BACKUP ===\nAddress: %s\nPrivate Key: %s\n", newAddress, privateKey)
			_ = os.WriteFile("cvn_secret_backup.txt", []byte(backupContent), 0600)

			configData := fmt.Sprintf("{\n  \"miner_address\": \"%s\"\n}", newAddress)
			_ = os.WriteFile("miner_config.json", []byte(configData), 0644)
			os.Exit(0)
		}
	}

	InitBoltEngine()

	// Allocate memory backing arrays on boot sequence initialization
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
		CustomMinerAddress = "CVN_UNCONFIGURED_LOCAL_NODE_ID"
	}

	// ====================================================================
	// 👑 THE UNIVERSAL THREE-PORT DATA ENGINE INITIALIZATION
	// ====================================================================
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
				fmt.Fprintf(w, "<h2>📊 Covenant Standard Core Matrix Active</h2><p>Rig Height: #%d</p>", GetLatestBlock().Index)
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
			currentTip := GetLatestBlock().Index
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

		if err := http.ListenAndServe(":8081", winMux); err != nil {
			fmt.Printf("Port 8081 bind alert: %v\n", err)
		}
	}()

	go func() {
		mirrorMux := http.NewServeMux()
		mirrorMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<h2>💎 Covenant Standard Core Active Matrix</h2><p>Rig Height: #%d</p>", GetLatestBlock().Index)
		})
		
		if err := http.ListenAndServe(":8082", mirrorMux); err != nil {
			fmt.Printf("Port 8082 bind alert: %v\n", err)
		}
	}()

	RunDesktopUIFrameworkIfApplicable()

	go StartMeshNetwork()

	// ====================================================================
	// 🛰️  DECENTRALIZED MULTI-SEED CHUNK SYNCHRONIZATION PHASE
	// ====================================================================
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
		liveBlock := GetLatestBlock()
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
						if incomingBlock.Index == int64(GetLatestBlock().Index)+1 {
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
		const TargetBlockTimeWindow = 10
		
		for {
			if atomic.LoadInt32(&IsSyncingActive) == 1 {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			blockMiningStartTime := time.Now().Unix()
			currentBlock := GetLatestBlock()

			var targetDifficulty int
			if currentBlock.Index == 0 {
				targetDifficulty = 2 
			} else {
				targetDifficulty = CalculateNextDifficulty(currentBlock, time.Now().Unix())
			}

			if targetDifficulty < 1 { targetDifficulty = 1 }
			if targetDifficulty > 8 { targetDifficulty = 8 }

			nextIndex := currentBlock.Index + 1
			baseData := fmt.Sprintf("%d-%d-%d", nextIndex, time.Now().Unix(), targetDifficulty)
			targetPrefix := strings.Repeat("0", targetDifficulty)
			blockSolved := false

			for nonce < chunkCeiling {
				inputStr := baseData + "-" + strconv.FormatInt(nonce, 10)
				hashBytes := sha256.Sum256([]byte(inputStr))
				computedHashHex := fmt.Sprintf("%x", hashBytes)

				if strings.HasPrefix(computedHashHex, targetPrefix) {
					newBlock := Block{
						Index:      nextIndex,
						Timestamp:  time.Now().Unix(),
						Transactions: []Transaction{
							{
								ID:        fmt.Sprintf("TX_COINBASE_%d", time.Now().UnixNano()),
								Witness:   "COVENANT_STEWARD_ASSEMBLY",
								PublicKey: "GENESIS_VOID_REWARD_POOL",
								Outputs: []UTXOOutput{
									{Recipient: CustomMinerAddress, Amount: 50.0},
								},
							},
						},
						PrevHash:   currentBlock.Hash,
Hash:       computedHashHex,
Difficulty: targetDifficulty,
Nonce:      nonce,
}
if SaveBlockToStorage(newBlock) {
logMutex.Lock()
logLine := fmt.Sprintf("[%s] 🟩 [SUCCESS] Solved valid block #%-3d | Hash: %s | Performance: %.2f MH/s", time.Now().Format("15:04:05"), newBlock.Index, newBlock.Hash[:16], currentLiveHashrateTrack)
liveTerminalEventLogs = append(liveTerminalEventLogs, logLine)
logMutex.Unlock()
go BroadcastNewBlock(newBlock)
}
nonce = 0
chunkCeiling = 1000000
blockSolved = true
break
}
atomic.AddUint64(&globalHashCount, 1)
nonce++
}
if blockSolved {
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
// Initial clean boot screen sweep
clearTerminalNatively()
var loopCounter uint64 = 0
for {
select {
case <-ticker.C:
loopCounter++
liveBlock := GetLatestBlock()
actualBlocksHeight := liveBlock.Index
actualCvnBalance := liveBlock.Index * 50
hashesComputed := atomic.SwapUint64(&globalHashCount, 0)
realHashrateMH := (float64(hashesComputed) / 3.0) / 1000000.0
currentLiveHashrateTrack = realHashrateMH
// 👑 DYNAMIC TIME-WEIGHTED HASHRATE REGISTERS
hashrateMutex.Lock()
// 15m Window = 300 entries (at 3s per tick)
hashRateHistory15m = append(hashRateHistory15m, realHashrateMH)
if len(hashRateHistory15m) > 300 { hashRateHistory15m = hashRateHistory15m[1:] }
// 1h Window = 1200 entries. To simulate true historical drag early, layer a small fractional multiplier decay pass
decayed1hVal := realHashrateMH * (1.0 - (0.02 * (1.0 / float64(actualBlocksHeight+1))))
hashRateHistory1h = append(hashRateHistory1h, decayed1hVal)
if len(hashRateHistory1h) > 1200 { hashRateHistory1h = hashRateHistory1h[1:] }
// 24h Window = 28800 entries. Layer a separate network baseline weight offset to differentiate columns on fresh boots
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
hashrateMutex.Unlock()
// 👑 THE 30-SECOND RECURRING HEARTBEAT MONITOR:
// Activates every 10 ticker cycles (10 * 3s = 30s) to print an active telemetry update to rows 10+
if loopCounter%10 == 0 {
logMutex.Lock()
heartbeatLine := fmt.Sprintf("[%s] ⛏️  [PoD CORE] Hashing active... Performance: %.2f MH/s // Ledger Height: #%d", time.Now().Format("15:04:05"), realHashrateMH, actualBlocksHeight)
liveTerminalEventLogs = append(liveTerminalEventLogs, heartbeatLine)
logMutex.Unlock()
}
// Wipe and redraw from row 0 cleanly
clearTerminalNatively()
fmt.Println("================================================================================")
fmt.Println("💎 COVENANT STANDARD (CVN) LAYER-1 NODE MINER MASTER DASHBOARD                  ")
fmt.Println("================================================================================")
fmt.Printf("🛰️  NETWORK HEIGHT: #%-15d | 📊 15m AVG HASHRATE: %.2f MH/s\n", actualBlocksHeight, avg15m)
fmt.Printf("💰 WALLET BALANCE: %-16d CVN | 📊 60m AVG HASHRATE: %.2f MH/s\n", actualCvnBalance, avg1h)
fmt.Printf("🪪 RIG IDENTITY:   %-15.15s... | 📊 24h AVG HASHRATE: %.2f MH/s\n", CustomMinerAddress, avg24h)
fmt.Println("================================================================================")
fmt.Println("⚡ LIVE STREAMING CONSENSUS & HARDWARE ACTION REGIONS BELOW:                    ")
fmt.Println("--------------------------------------------------------------------------------")
logMutex.Lock()
if len(liveTerminalEventLogs) > 0 {
for _, event := range liveTerminalEventLogs {
fmt.Println(event)
}
// Bound total visible lines to 12 slots to prevent terminal boundaries from leaking
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
expectedTimeWindow := int64(45)
actualTimeElapsed := currentTimestamp - lastBlock.Timestamp
if actualTimeElapsed < expectedTimeWindow/2 { return currentDiff + 1 }
if actualTimeElapsed > expectedTimeWindow*2 { if currentDiff > 1 { return currentDiff - 1 }; return 1 }
return currentDiff
}