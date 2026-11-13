package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json" // 👑 CORRECT STANDARD LIBRARY IMPORT RESIDENCY
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
var miningWg         sync.WaitGroup
var miningContext    context.Context
var miningMutex      sync.Mutex

// Local wallet account statement memory cache tracking fields
var actualCvnBalance float64 = 0.0

// 👑 NATIVE PARITY TRACKING REGISTERS
var GlobalHeightSolverLatch uint64
var isResettingActive int32 = 0

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

	// `--wallet` opens the text wallet in this terminal and never starts a
	// node, so it can run beside the miner.
	for _, arg := range os.Args {
		if arg == "--wallet" {
			RunTextWallet()
			return
		}
	}

	// The desktop wallet build opens its window here and never starts a node.
	if RunWalletWindow() {
		return
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
				
				RebuildStateBalanceCacheInternal()

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
				// Total mining work in this node's chain; peers compare chains by it.
				"total_work": chainWorkThrough(latest.Index).String(),
			}
			json.NewEncoder(w).Encode(payload)
		})

		// A node only ever relays transfers that arrive already signed by the
		// owner of the coins. It never builds or signs one on a caller's behalf.
		winMux.HandleFunc("/transaction", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			var tx Transaction
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&tx); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "transaction is not valid JSON"})
				return
			}
			if err := AdmitTransactionToMempool(tx); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			go BroadcastNewTransaction(tx)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "tx_id": tx.ID})
		})

		// Unspent coins of one address, for wallets building a transfer.
		winMux.HandleFunc("/utxos", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ListSpendableCoins(r.URL.Query().Get("address")))
		})
// Enforced pointer *http.Request context
winMux.HandleFunc("/audit", func(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Access-Control-Allow-Origin", "")
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
StartPeerCache()
go StartMeshNetwork()
mirrorMux := http.NewServeMux()
// 👑 FIXED HOOK PLATFORM INTERCEPT PASS: Enforced explicit *http.Request context alignment
mirrorMux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Access-Control-Allow-Origin", "")
w.Header().Set("Content-Type", "application/json")
StateMutex.RLock()
tip := GetLatestBlockInternal()
StateMutex.RUnlock()
json.NewEncoder(w).Encode(map[string]interface{}{
"status":   "ONLINE",
"height":   tip.Index,
"tip_hash": tip.Hash,
// The address this node mines to, so a wallet on the same machine can
// show the right balance by default.
"miner_address": strings.TrimSpace(CustomMinerAddress),
})
})
go func() { _ = http.ListenAndServe(":8082", mirrorMux) }()
fmt.Println("🚀 INITIALIZING COVENANT STANDARD BULK SYNC CORES...")
seedGateways := []string{"http://202.137.175.220:8081", "http://207.148.67.11:8081", "http://64.177.45.153:8081"}
// Peers remembered from earlier runs are extra sync sources, so a node can
// catch up even when every built-in seed is offline. Only those answering
// right now are kept, so dead entries do not slow each batch down.
for _, cachedGateway := range cachedPeerHTTPGateways(seedGateways) {
if GetGlobalMeshMaxHeight(cachedGateway) > 0 {
seedGateways = append(seedGateways, cachedGateway)
}
}
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
startupForkRecoveryAttempts := 0
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
heightBeforeBatch := currentLocalHeight
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
if currentLocalHeight == heightBeforeBatch {
// Nothing from this batch attached to our chain. The usual cause is that
// the database ends on a side branch the network abandoned, so the next
// network block does not fit on top of it. Asking for the same batch again
// would loop forever; instead find where we parted from the network, roll
// back to that point and catch up from there.
startupForkRecoveryAttempts++
if startupForkRecoveryAttempts > 3 {
fmt.Println("\n🚨 [BULK SYNC] Could not rejoin the network automatically. Stop the node, delete cvn_mainnet.db and start again to sync from scratch.")
break
}
fmt.Printf("\n🔀 [BULK SYNC] Local chain does not match the network after block #%d. Rolling back to the last shared block and re-syncing...\n", currentLocalHeight)
atomic.StoreInt32(&IsSyncingActive, 0)
TriggerActiveReSyncPass()
continue
}
startupForkRecoveryAttempts = 0
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
go runChainLagWatchdog()
// Decide once before the miner starts whether this node is caught up, then
// keep checking. See mining_readiness.go.
evaluateMiningReadiness()
go runMiningReadinessMonitor()
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
// The longer averages cover only the time since the miner started until
// their window has filled.
var avg1h, avg24h float64
if len(hashRateHistory1h) > 0 {
var sum1h float64
for _, v := range hashRateHistory1h {
sum1h += v
}
avg1h = sum1h / float64(len(hashRateHistory1h))
}
if len(hashRateHistory24h) > 0 {
var sum24h float64
for _, v := range hashRateHistory24h {
sum24h += v
}
avg24h = sum24h / float64(len(hashRateHistory24h))
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
fmt.Printf("💰 WALLET BALANCE: %-16.2f CVN | 📊 60m AVG HASHRATE: %.2f MH/s\n", actualCvnBalance, avg1h)
fmt.Printf("🪪 RIG IDENTITY: %-18.18s...  | 📊 24h AVG HASHRATE: %.2f MH/s\n", CustomMinerAddress, avg24h)
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
miningContext = context.Background()
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
// 👑 RESET TRIGGER FORWARD ROUTINE: Maps the missing terminal state wrapper cleanly
// Intentionally a no-op: workers re-read the chain tip on every pass, so an
// incoming block needs no restart. Spawning a fresh worker pool here leaked
// runtime.NumCPU() never-exiting goroutines per accepted wire block.
func SignalMinerReset() {}
func executionMiningWorkerThread(ctx context.Context, workerID int64) {
defer miningWg.Done()
var nonce int64 = workerID * 100000000
var floorTipHash string
var floorTipFirstSeen int64
for {
if atomic.LoadInt32(&IsSyncingActive) == 1 || atomic.LoadInt32(&MiningHoldActive) == 1 {
time.Sleep(100 * time.Millisecond)
continue
}
StateMutex.RLock()
currentBlock := GetLatestBlockInternal()
StateMutex.RUnlock()
nextIndex := currentBlock.Index + 1
parentBlockTipHash := currentBlock.Hash
currentUnixTime := time.Now().Unix()
const GlobalTargetBlockTimeFloor = int64(45)
// 45-second floor. It is measured from the tip's timestamp, or from when this
// worker first saw the tip if that timestamp is ahead of our clock. A tip dated
// even one second in the future used to skip the wait entirely; the next block
// was then dated one second later still, and the miner ran away producing a
// block per second until difficulty choked the chain.
if currentBlock.Hash != floorTipHash {
floorTipHash = currentBlock.Hash
floorTipFirstSeen = currentUnixTime
}
floorReference := currentBlock.Timestamp
if floorReference > floorTipFirstSeen {
floorReference = floorTipFirstSeen
}
// Heartbeat blocks are spaced by proof-of-work, not by waiting, so the floor
// only applies to heights before the heartbeat activates.
if !IsHeartbeatHeight(nextIndex) && currentBlock.Index > 0 && currentUnixTime-floorReference < GlobalTargetBlockTimeFloor {
// Wait in short slices and restart the pass, so the floor is always
// re-checked against whatever the tip is when this worker wakes.
time.Sleep(250 * time.Millisecond)
continue
}
// The tip that passed the floor check above is the one we mine on. It is not
// re-read here: a re-read could pick up a block saved a moment ago and mine
// on top of it without that block ever being checked against the floor.
simulatedTime := time.Now().Unix()
if simulatedTime <= currentBlock.Timestamp {
simulatedTime = currentBlock.Timestamp + 1
}
var workerTargetDifficulty int
heartbeatTemplate := IsHeartbeatHeight(nextIndex)
// Peers reject blocks dated too far ahead of their clock. If the tip is
// already dated ahead, wait for real time to catch up instead of stacking
// another future-dated block on it.
if heartbeatTemplate && simulatedTime > time.Now().Unix()+HeartbeatMaxFutureDriftSeconds/2 {
time.Sleep(250 * time.Millisecond)
continue
}
heartbeatTargetHex := ""
targetPrefix := ""
if heartbeatTemplate {
workerTargetDifficulty = CalculateNextDifficulty(currentBlock, simulatedTime)
heartbeatTargetHex = HeartbeatTargetHex(workerTargetDifficulty)
} else {
if currentBlock.Index <= 9 {
workerTargetDifficulty = 2
} else {
workerTargetDifficulty = CalculateNextDifficulty(currentBlock, simulatedTime)
if workerTargetDifficulty < 4 {
workerTargetDifficulty = 4
}
}
if workerTargetDifficulty < 1 { workerTargetDifficulty = 1 }
if workerTargetDifficulty > 8 { workerTargetDifficulty = 8 }
targetPrefix = strings.Repeat("0", workerTargetDifficulty)
}
var guardMatrixPlaceholder = ""
coinbaseTxID := fmt.Sprintf("TX_COINBASE_H%d_%x", nextIndex, sha256.Sum256([]byte(fmt.Sprintf("%d-%d", nextIndex, nonce))))
var coinbaseOutputs []UTXOOutput
// From the transaction fork on, waiting transfers that are valid on top of
// this tip go into the block, and their fees are added to the miner's reward.
var templateTransfers []Transaction
var templateFees float64
if IsTxForkHeight(nextIndex) {
templateTransfers, templateFees = SelectMempoolTransactionsForBlock(TxMaxPerBlock - 1)
}
// If the tip won a same-height race, name the losing block as an uncle and
// split this block's reward 80/20 with its miner. Total emission is unchanged.
// Reward and fees as whole units (see units.go), so the amounts written
// into the block are exact.
rewardUnits := BlockRewardUnits(nextIndex)
feeUnits, _ := AmountToUnits(templateFees)
templateUncleHash := "0000000000000000000000000000000000000000000000000000000000000000"
templateUncleMiner := "NONE"
if pendingUncleHash, pendingUncleMiner, hasUncle := GetPendingUncleForTip(currentBlock); hasUncle {
templateUncleHash = pendingUncleHash
templateUncleMiner = pendingUncleMiner
// Whole units: the uncle gets a fifth of the new reward, rounded down, and
// the miner the rest plus the fees, so the two always add up exactly.
uncleUnits := rewardUnits / 5
coinbaseOutputs = append(coinbaseOutputs, UTXOOutput{Recipient: CustomMinerAddress, Amount: UnitsToAmount(rewardUnits - uncleUnits + feeUnits)})
coinbaseOutputs = append(coinbaseOutputs, UTXOOutput{Recipient: pendingUncleMiner, Amount: UnitsToAmount(uncleUnits)})
} else {
coinbaseOutputs = append(coinbaseOutputs, UTXOOutput{Recipient: CustomMinerAddress, Amount: UnitsToAmount(rewardUnits + feeUnits)})
}
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
candidateTransactions = append(candidateTransactions, templateTransfers...)
// The transactions are fixed for this template, so digest them once.
templateTxDigest := BlockTransactionsDigest(nextIndex, candidateTransactions)
for i := 0; i < 100000; i++ {
nonce++
atomic.AddUint64(&globalHashCount, 1)
StateMutex.RLock()
actualTipCheck := GetLatestBlockInternal()
StateMutex.RUnlock()
// Abandon the template if the tip moved or was replaced by a competing
// block at the same height; otherwise we would mine on a stale parent.
if actualTipCheck.Index != (nextIndex-1) || actualTipCheck.Hash != parentBlockTipHash {
break
}
mockTemplateBlock := Block{
Version:      1,
Index:        nextIndex,
Timestamp:    simulatedTime,
Transactions: candidateTransactions,
PrevHash:     parentBlockTipHash,
Hash:         "",
Difficulty:   workerTargetDifficulty,
Nonce:        nonce,
GuardMatrix:  guardMatrixPlaceholder,
UncleHash:    templateUncleHash,
UncleMiner:   templateUncleMiner,
}
// 👑 ABSOLUTE CONGRUENCE PASS: Calculate hash using canonical method blueprint safely
computedHashHex := CalculateHashWithTxDigest(mockTemplateBlock, templateTxDigest)
if (heartbeatTemplate && computedHashHex <= heartbeatTargetHex) || (!heartbeatTemplate && strings.HasPrefix(computedHashHex, targetPrefix)) {
StateMutex.RLock()
latestOnDiskTip := GetLatestBlockInternal()
StateMutex.RUnlock()
latestOnDiskTipHeight := latestOnDiskTip.Index
if latestOnDiskTipHeight == nextIndex-1 && latestOnDiskTip.Hash != parentBlockTipHash {
break
}
// Raise the latch to the on-disk tip (never lower it) so heights reached via
// bulk sync, re-sync or gossip count; otherwise the claim CAS below can never
// succeed on a node that did not mine the previous block itself.
for {
cur := atomic.LoadUint64(&GlobalHeightSolverLatch)
if latestOnDiskTipHeight < 0 || cur >= uint64(latestOnDiskTipHeight) {
break
}
if atomic.CompareAndSwapUint64(&GlobalHeightSolverLatch, cur, uint64(latestOnDiskTipHeight)) {
break
}
}
currentLatchValue := atomic.LoadUint64(&GlobalHeightSolverLatch)
if currentLatchValue >= uint64(nextIndex) || latestOnDiskTipHeight >= nextIndex {
break
}
// Claim height milestone serially across cores
if !atomic.CompareAndSwapUint64(&GlobalHeightSolverLatch, uint64(nextIndex-1), uint64(nextIndex)) {
break
}
mockTemplateBlock.Hash = computedHashHex
fmt.Printf("\n🟩 [SUCCESS] Block #%d Mined by Worker %d | Hash: %s\n", mockTemplateBlock.Index, workerID, mockTemplateBlock.Hash[:16])
// 👑 FIXED SERIAL SHUTDOWN OVERRIDE: Saves block data safely without context teardowns
if SaveBlockToStorage(mockTemplateBlock) {
logMutex.Lock()
logLine := fmt.Sprintf("[%s] 🎉 [SUCCESS] Solved valid block #%-3d | Hash: %s | Performance: %.2f MH/s", time.Now().Format("15:04:05"), mockTemplateBlock.Index, mockTemplateBlock.Hash[:16], currentLiveHashrateTrack)
liveTerminalEventLogs = append(liveTerminalEventLogs, logLine)
logMutex.Unlock()
} else {
// Release height latch registry frame back on database dropout failure
atomic.StoreUint64(&GlobalHeightSolverLatch, uint64(nextIndex-1))
}
break
}
}
}
}
func osCheckArgs() []string {
return os.Args
}