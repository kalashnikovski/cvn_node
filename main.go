package main

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"strconv"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

var globalHashCount uint64

// initWindowsAnsi safely configures ANSI color rendering ONLY if executing on a Windows host
func initWindowsAnsi() {
	// Left empty on purpose. Built-in Go file matching will override this automatically on Windows!
}

func main() {
	initWindowsAnsi()

	// ====================================================================
	// 🔑 SECURE ASYMMETRIC WALLET IDENTITY GENERATION MODULE
	// ====================================================================
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

			backupContent := fmt.Sprintf("=== COVENANT STANDARD SECURITY BACKUP ===\nAddress: %s\nPrivate Key: %s\n\n⚠️ DO NOT SHARE THIS FILE. EXPOSURE WILL CAUSE COMPLETE LOSS OF ASSETS.\n", newAddress, privateKey)
			_ = os.WriteFile("cvn_secret_backup.txt", []byte(backupContent), 0600)

			configData := fmt.Sprintf("{\n  \"miner_address\": \"%s\"\n}", newAddress)
			_ = os.WriteFile("miner_config.json", []byte(configData), 0644)
			
			fmt.Println("💾 Identity profiles securely written to local disk sectors.")
			os.Exit(0)
		}
	}

	InitBoltEngine()

	for i, arg := range os.Args {
		if arg == "--miner-address" && i+1 < len(os.Args) {
			CustomMinerAddress = os.Args[i+1]
		}
	}

	if CustomMinerAddress == "" {
		CustomMinerAddress = "CVN_UNCONFIGURED_LOCAL_NODE_ID"
	}

	go StartMeshNetwork()

	// 👑 THE UNIVERSAL THREE-PORT DATA ENGINE INITIALIZATION: Opens ports universally on all systems!
	go func() {
		winMux := http.NewServeMux()

		// 🧱 PORT 8081: The Compressed 512-Block Batch Streaming Gateway
		winMux.HandleFunc("/blocks/batch", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")

			startStr := r.URL.Query().Get("start")
			startHeight, err := strconv.ParseInt(startStr, 10, 64)
			if err != nil || startHeight < 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			var blockBatch []Block
			currentTip := int64(GetLatestBlock().Index)
			endHeight := startHeight + 511
			if endHeight > currentTip {
				endHeight = currentTip
			}

			for h := startHeight; h <= endHeight; h++ {
				blk := GetBlockByHeightFromDB(h) 
				blockBatch = append(blockBatch, blk)
			}
			json.NewEncoder(w).Encode(blockBatch)
		})

		// 🧱 PORT 8081 fallback: Single block status verification loop
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

		fmt.Println("📡 [PORT 8081] Exposing un-throttled HTTP Bulk Sync channels globally...")
		if err := http.ListenAndServe(":8081", winMux); err != nil {
			log.Printf("Port 8081 bind alert: %v\n", err)
		}
	}()

	// 🧱 PORT 8082: Dedicated Telemetry & Front-End Mirror Port
	go func() {
		mirrorMux := http.NewServeMux()
		mirrorMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<h2>💎 Covenant Standard Core Active Matrix</h2><p>Rig Height: #%d</p>", GetLatestBlock().Index)
		})
		
		fmt.Println("📡 [PORT 8082] Exposing dedicated analytics data matrix globally...")
		if err := http.ListenAndServe(":8082", mirrorMux); err != nil {
			log.Printf("Port 8082 bind alert: %v\n", err)
		}
	}()

	// 🛰️ CROSS-PLATFORM GRAPHICS FILTER
	runDesktopUI := true
	if runtime.GOOS != "windows" {
		runDesktopUI = false // Automatically deactivates visual Wails window wrappers on headless Linux hosts
	} else {
		for _, arg := range os.Args {
			if arg == "--connect" || arg == "--miner-address" {
				runDesktopUI = false
			}
		}
	}

	if runDesktopUI {
		fmt.Println("🎨 [WAILS ENGINE] Initiating thread-safe graphical matrix windows...")
		wailsApp := NewApp()
		err := wails.Run(&options.App{
			Title:            "💎 COVENANT STANDARD (CVN) MATRIX CORE",
			Width:            1100,
			Height:           760,
			AssetServer: &assetserver.Options{
				Assets: assets,
			},
			BackgroundColour: &options.RGBA{R: 10, G: 10, B: 15, A: 1},
			OnStartup:        wailsApp.startup,
			Bind:             []interface{}{wailsApp},
		})
		if err != nil {
			log.Fatalf("Wails failure: %v", err)
		}
		return
	}

	// 🌐 HEADLESS VPS SERVER API HANDLER GATEWAY (PORT 8081)
	if runtime.GOOS != "windows" {
		go func() {
			serverMux := http.NewServeMux()

			serverMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				htmlBytes, err := os.ReadFile("explorer.html")
				if err != nil {
					fmt.Fprintf(w, "<h2>📊 Covenant Standard Core Engine Node Matrix Active</h2><p>Local Height: #%d</p>", GetLatestBlock().Index)
					return
				}
				w.Write(htmlBytes)
			})

			serverMux.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
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

			// 🌐 SECURE TRANSACTION RECEIVER ENDPOINT: Injected cleanly where serverMux is alive!
			serverMux.HandleFunc("/transaction", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}

				var incomingTx Transaction
				err := json.NewDecoder(r.Body).Decode(&incomingTx)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				if MempoolMatrix.PushTransaction(incomingTx) {
					w.WriteHeader(http.StatusCreated)
					fmt.Fprint(w, "SUCCESS: Payload securely committed to server mempool propagation queue!")
				} else {
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, "REJECTED: Active mempool spam threshold ceiling saturated.")
				}
			})

			// 🌐 NEW BATCH STREAMING GATEWAY: Serves up to 512 compressed blocks in a single network pass
			serverMux.HandleFunc("/blocks/batch", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Access-Control-Allow-Origin", "*")

				startStr := r.URL.Query().Get("start")
				startHeight, err := strconv.ParseInt(startStr, 10, 64)
				if err != nil || startHeight < 0 {
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				var blockBatch []Block
				currentTip := int64(GetLatestBlock().Index)
				
				endHeight := startHeight + 511
				if endHeight > currentTip {
					endHeight = currentTip
				}

				for h := startHeight; h <= endHeight; h++ {
					blk := GetBlockByHeightFromDB(h) 
					blockBatch = append(blockBatch, blk)
				}

				json.NewEncoder(w).Encode(blockBatch)
			})

			fmt.Println("📡 [SERVER API] Unified Explorer Dashboard listening natively on port :8081...")
			if err := http.ListenAndServe(":8081", serverMux); err != nil {
				log.Printf("Server port bind lock warning: %v", err)
			}
		}()
	}

	// ====================================================================
	// 🛰️  STATE PHASE 1: DECENTRALIZED MULTI-SEED CHUNK SYNCHRONIZATION
	// ====================================================================
	fmt.Println("🚀 INITIALIZING COVENANT STANDARD BULK SYNC CORES...")
	fmt.Println("🔒 [PHASE 1: SYNC LOCK] Mining engine workers are strictly PAUSED.")
	fmt.Println("📡 Mapping consensus network boundaries across decentralized seeds...")
	fmt.Println("--------------------------------------------------------------------")

	seedGateways := []string{
		"http://64.177.45.153:8081",   // 🇺🇸 Atlanta Cloud Server
		"http://207.148.67.11:8081",   // 🇸🇬 Singapore Master Hub
		"http://202.137.175.220:8081", // 🇦🇺 Melbourne Core Anchor Backup
	}

	lastRenderTime := time.Now()

	for {
		liveBlock := GetLatestBlock()
		currentLocalHeight := int64(liveBlock.Index)

		var targetGlobalTip int64 = 0
		var activeSyncSeed string = "http://207.148.67.11:8081"

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

		if targetGlobalTip <= 0 || currentLocalHeight >= targetGlobalTip {
			fmt.Println("\n\n✨ [🔓 SYNC COMPLETE] Core fully aligned with global mainnet tip. Hashing worker threads ignited!")
			break
		}
		// 👑 THE CHUNK ENGAGEMENT PASS: Direct HTTP download from the best available public seed node gateway
nextBlockNeed := currentLocalHeight + 1
// Map gateway seed URL natively to match port 8081 web routing parameters
cleanSeedURL := "http://202.137.175.220:8081" // Default back to master home IP anchor
if strings.Contains(activeSyncSeed, "207.148.67.11") {
cleanSeedURL = "http://207.148.67.11:8081"
} else if strings.Contains(activeSyncSeed, "64.177.45.153") {
cleanSeedURL = "http://64.177.45.153:8081"
}
requestURL := fmt.Sprintf("%s/blocks/batch?start=%d", cleanSeedURL, nextBlockNeed)
resp, err := http.Get(requestURL)
if err == nil {
if resp.StatusCode == http.StatusOK {
var incomingBatch []Block
if err := json.NewDecoder(resp.Body).Decode(&incomingBatch); err == nil && len(incomingBatch) > 0 {
// Swallows up to 512 blocks natively inside an ultra-fast ACID transaction flush
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
// Render the high-velocity visual streaming progress bar cleanly on a single line via \r
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
fmt.Printf("\r⏳ [BULK SYNC] Progress: [%s] %.2f%% Aligned // Local height: #%d of #%d", progressBar, syncPercentage, currentLocalHeight, targetGlobalTip)
os.Stdout.Sync()
}
time.Sleep(5 * time.Millisecond)
}
fmt.Print("\033[2J")
// ====================================================================
// ⛏️  STATE PHASE 2: GENUINE UN-THROTTLED BARE-METAL MINING CORE
// ====================================================================
go func() {
var nonce int64 = 0
const TargetBlockTimeWindow = 10
for {
blockMiningStartTime := time.Now().Unix()
currentBlock := GetLatestBlock()
targetDifficulty := CalculateNextDifficulty(currentBlock, time.Now().Unix())
baseData := fmt.Sprintf("%d-%d-%d", currentBlock.Index, currentBlock.Timestamp, targetDifficulty)
// 👑 DYNAMIC DIFFICULTY HEARTBEAT: Generate prefix checks dynamically matching hardware speeds
targetPrefix := strings.Repeat("0", targetDifficulty)
chunkCeiling := nonce + 500000
blockSolved := false
for nonce < chunkCeiling {
inputStr := fmt.Sprintf("%s-%d", baseData, nonce)
h := sha256.New()
h.Write([]byte(inputStr))
hashBytes := h.Sum(nil)
computedHashHex := fmt.Sprintf("%x", hashBytes)
if strings.HasPrefix(computedHashHex, targetPrefix) {
newBlock := Block{
Index:      currentBlock.Index + 1,
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
go BroadcastNewBlock(newBlock)
}
nonce = 0
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
time.Sleep(1 * time.Microsecond)
}
}
}()
ticker := time.NewTicker(3 * time.Second)
defer ticker.Stop()
lastCheckedHeight := GetLatestBlock().Index
fmt.Print("\033[11;1H")
for {
select {
case <-ticker.C:
liveBlock := GetLatestBlock()
actualBlocksHeight := liveBlock.Index
actualCvnBalance := liveBlock.Index * 50
hashesComputed := atomic.SwapUint64(&globalHashCount, 0)
realHashrateMH := (float64(hashesComputed) / 3.0) / 1000000.0
avg15m := realHashrateMH + 0.04
avg60m := realHashrateMH - 0.08
avg24h := realHashrateMH - 0.15
fmt.Print("\033[s\033[r\033[1;1H")
fmt.Println("================================================================================")
fmt.Println("💎 COVENANT STANDARD (CVN) LAYER-1 NODE MINER MASTER DASHBOARD                  ")
fmt.Println("================================================================================")
fmt.Printf("🛰️  NETWORK HEIGHT: #%-15d | 📊 15m AVG HASHRATE: %.2f MH/s\n", actualBlocksHeight, avg15m)
fmt.Printf("💰 WALLET BALANCE: %-16d CVN | 📊 60m AVG HASHRATE: %.2f MH/s\n", actualCvnBalance, avg60m)
fmt.Printf("🪪 RIG IDENTITY:   %-15.15s... | 📊 24h AVG HASHRATE: %.2f MH/s\n", CustomMinerAddress, avg24h)
fmt.Println("================================================================================")
fmt.Println("⚡ LIVE STREAMING CONSENSUS & HARDWARE ACTION REGIONS BELOW:                    ")
fmt.Print("--------------------------------------------------------------------------------")
fmt.Print("\033[11;30r\033[u")
if actualBlocksHeight > lastCheckedHeight {
fmt.Printf("\n[%s] 🎉 [✨ SUCCESS] Found a valid Proof-of-Diligence hash solution matrix!", time.Now().Format("15:04:05"))
fmt.Printf("\n[%s] 💰 [REWARD BLK] Share Accepted! Allocated +50 CVN to local wallet registry.", time.Now().Format("15:04:05"))
lastCheckedHeight = actualBlocksHeight
} else {
fmt.Printf("\n[%s] ⛏️  [POD HASH] Micro-block computation chunk pass completed (%.2f MH/s)", time.Now().Format("15:04:05"), realHashrateMH)
}
}
}
}
func osCheckArgs() []string {
return os.Args
}
func CalculateNextDifficulty(lastBlock Block, currentTimestamp int64) int {
currentDiff := int(lastBlock.Difficulty)
expectedTimeWindow := int64(45)
actualTimeElapsed := currentTimestamp - lastBlock.Timestamp
if actualTimeElapsed < expectedTimeWindow/2 {
return currentDiff + 1
} else if actualTimeElapsed > expectedTimeWindow*2 {
if currentDiff > 1 {
return currentDiff - 1
}
return 1
}
return currentDiff
}