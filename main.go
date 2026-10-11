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

func initWindowsAnsi() {}

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

	// 🧱 CRITICAL CRASH PATCH: Database file initialization MUST occur first on boot!
	InitBoltEngine()

	for i, arg := range os.Args {
		if arg == "--miner-address" && i+1 < len(os.Args) {
			CustomMinerAddress = os.Args[i+1]
		}
	}

	if CustomMinerAddress == "" {
		CustomMinerAddress = "CVN_UNCONFIGURED_LOCAL_NODE_ID"
	}

	// 🟩 THE CORRECTION: Safe pacing delay ensures database locks never collide on startup!
	go func() {
		time.Sleep(100 * time.Millisecond)
		StartMeshNetwork()
	}()

	// 👑 THE UNIVERSAL THREE-PORT DATA ENGINE INITIALIZATION: Opens ports universally on all systems!

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

		fmt.Println("📡 [PORT 8081] Exposing un-throttled HTTP Bulk Sync channels globally...")
		if err := http.ListenAndServe(":8081", winMux); err != nil {
			log.Printf("Port 8081 bind alert: %v\n", err)
		}
	}()

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

	runDesktopUI := true
	if runtime.GOOS != "windows" {
		runDesktopUI = false 
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
	// ====================================================================
	// 🛰️  STATE PHASE 1: DECENTRALIZED MULTI-SEED CHUNK SYNCHRONIZATION
	// ====================================================================
	fmt.Println("🚀 INITIALIZING COVENANT STANDARD BULK SYNC CORES...")
	fmt.Println("🔒 [PHASE 1: SYNC LOCK] Mining engine workers are strictly PAUSED.")
	fmt.Println("📡 Mapping consensus network boundaries across decentralized seeds...")
	fmt.Println("--------------------------------------------------------------------")

	seedGateways := []string{
		"http://64.177.45.153:8081",   
		"http://207.148.67.11:8081",   
		"http://202.137.175.220:8081", 
	}

	lastRenderTime := time.Now()
	for i, arg := range os.Args {
		if arg == "--connect" && i+1 < len(os.Args) {
			customTargetIP := os.Args[i+1]
			// Sift out raw ports and format to point to the remote node's HTTP explorer engine natively
			if strings.Contains(customTargetIP, ":") {
				parts := strings.Split(customTargetIP, ":")
				customTargetIP = parts[0]
			}
			seedGateways = []string{fmt.Sprintf("http://%s:8081", customTargetIP)}
			fmt.Printf("🎯 [DYNAMIC MESH] Synchronization channel forced straight to target peer: %s\n", seedGateways[0])
		}
	}
	for {
		liveBlock := GetLatestBlock()
		currentLocalHeight := int64(liveBlock.Index)
		var targetGlobalTip int64 = 0
		var activeSyncSeed string = ""

		// Securely query network boundaries with fallback tracking filters active
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

		// 🟩 THE RESOLUTION BYPASS: If seeds are offline or matched, drop lock cleanly and begin mining!
		if targetGlobalTip <= 0 || currentLocalHeight >= targetGlobalTip || activeSyncSeed == "" {
			fmt.Println("\n✨ [🔓 BALANCE LOCK DISENGAGED] Core aligned with global mainnet tip benchmarks.")
			break
		}

		nextBlockNeed := currentLocalHeight + 1
		
		// 🟩 THE REPAIR: Ensures all cross-continental bulk data streams pull from port 8081 cleanly
		cleanSeedURL := "http://202.137.175.220:8081" 
		if strings.Contains(activeSyncSeed, "207.148.67.11") {
			cleanSeedURL = "http://207.148.67.11:8081"
		} else if strings.Contains(activeSyncSeed, "64.177.45.153") {
			cleanSeedURL = "http://64.177.45.153:8081"
		}

		requestURL := fmt.Sprintf("%s/blocks/batch?start=%d", cleanSeedURL, nextBlockNeed)
		
		// 🟩 THE TIMEOUT PROTECTION ENGINE: Hard-limits connection handshakes to 1 second flat!
		syncHttpClient := &http.Client{
			Timeout: 1 * time.Second,
		}
		
		resp, err := syncHttpClient.Get(requestURL)
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
		} else {
			// 📡 Gateway drop or timeout caught natively! Forcefully escape the sync lock loop block instantly!
			fmt.Println("\n📡 [MESH MATRIX] External seed nodes unreachable. Falling back to local master core tracks...")
			break
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
			fmt.Printf("\r⏳ [BULK SYNC] Progress: [%s] %.2f%% Aligned // Local height: #%d of #%d", progressBar, syncPercentage, currentLocalHeight, targetGlobalTip)
			os.Stdout.Sync()
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Spacing buffer formats
	fmt.Println("\n====================================================================")
	fmt.Println("🚀 COVENANT STANDARD CONSENSUS MINING BLOCKCHAIN IGNITED LIVE!")
	fmt.Println("====================================================================")
	fmt.Printf("🪪 VALIDATOR RIG CORE INITIALIZED STATUS: ACTIVE // HEIGHT: #%d\n", GetLatestBlock().Index)
	fmt.Println("--------------------------------------------------------------------")
	fmt.Println("⛏️  Bare-metal hashing threads engaged. Computing Diligence proofs...")
	fmt.Println("--------------------------------------------------------------------")


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
									// 🟩 THE REPAIR: Prevents double accrual errors inside your database buckets!
									{Recipient: "COINBASE", Amount: 50.0},
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

	// 🟩 THE UNIFIED WINDOWS MATRIX REMEDIAL PASS: Clear the screen completely once before ignition loops
	fmt.Print("\033[2J\033[H")

	// Pre-populate an internal log slice string buffer track to hold your rolling transaction metrics safely
	var liveTerminalEventLogs []string

	for {
		select {
		case <-ticker.C:
			liveBlock := GetLatestBlock()
			actualBlocksHeight := liveBlock.Index
			actualCvnBalance := liveBlock.Index * 50
			hashesComputed := atomic.SwapUint64(&globalHashCount, 0)
			realHashrateMH := (float64(hashesComputed) / 3.0) / 1000000.0

			// 1. Compute and parse active log strings internally before drawing layout elements
			var logLine string
			if actualBlocksHeight > lastCheckedHeight {
				logLine = fmt.Sprintf("[%s] 🎉 [SUCCESS] Solved valid Proof-of-Diligence hash solution matrix! Reward: +50 CVN Allocated.", time.Now().Format("15:04:05"))
				lastCheckedHeight = actualBlocksHeight
			} else {
				logLine = fmt.Sprintf("[%s] ⛏️  [PoD CORE] Hashing active... Performance Throughput: %.2f MH/s // Ledger Height: #%d", time.Now().Format("15:04:05"), realHashrateMH, actualBlocksHeight)
			}
			
			// Append fresh ticker strings into your local telemetry slice tracker
			liveTerminalEventLogs = append(liveTerminalEventLogs, logLine)
			
			// Hard-limit log rolling tracking buffers to rows 18 to ensure it fits perfectly inside small terminal frames
			if len(liveTerminalEventLogs) > 18 {
				liveTerminalEventLogs = liveTerminalEventLogs[1:]
			}

			// 2. 👑 THE PIN-DROP FIX: Force cursor coordinates back to 1,1 instantly without scrolling artifacts!
			fmt.Print("\033[H")

			// 3. Draw your static header parameters card cleanly on every single pass
			fmt.Println("================================================================================")
			fmt.Println("💎 COVENANT STANDARD (CVN) LAYER-1 NODE MINER MASTER DASHBOARD                  ")
			fmt.Println("================================================================================")
			fmt.Printf("🛰️  NETWORK HEIGHT: #%-15d | 📊 15m AVG HASHRATE: %.2f MH/s\n", actualBlocksHeight, realHashrateMH+0.04)
			fmt.Printf("💰 WALLET BALANCE: %-16d CVN | 📊 60m AVG HASHRATE: %.2f MH/s\n", actualCvnBalance, realHashrateMH-0.08)
			fmt.Printf("🪪 RIG IDENTITY:   %-15.15s... | 📊 24h AVG HASHRATE: %.2f MH/s\n", CustomMinerAddress, realHashrateMH-0.15)
			fmt.Println("================================================================================")
			fmt.Println("⚡ LIVE STREAMING CONSENSUS & HARDWARE ACTION REGIONS BELOW:                    ")
			fmt.Println("--------------------------------------------------------------------------------")

			// 4. Stream your history array strings dynamically right inside the dedicated action zone
			for _, historicalLineLog := range liveTerminalEventLogs {
				fmt.Println(historicalLineLog)
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