package node

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
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

// assets holds the desktop wallet's screens. They are packed into the program
// by the start-up file in the project's main folder and handed to Run.
var assets embed.FS

// Globally scoped atomic counters and monitoring metrics
var globalHashCount uint64
var liveTerminalEventLogs []string
var logMutex sync.Mutex

// Moving window registers for tracking rolling average throughput metrics
var hashRateHistory15m []float64
var hashRateHistory1h []float64
var hashRateHistory24h []float64
var hashrateMutex sync.Mutex

var currentLiveHashrateTrack float64

// Mining Worker Pool orchestration controls
var miningWg sync.WaitGroup
var miningContext context.Context
var miningMutex sync.Mutex

// Local wallet account statement memory cache tracking fields
var actualCvnBalance float64 = 0.0

// 👑 NATIVE PARITY TRACKING REGISTERS
var GlobalHeightSolverLatch uint64

// Run is the whole program: the node and miner, the text wallet, the desktop
// wallet and the small tools, chosen by the command line. It is called by the
// start-up file in the project's main folder.
func Run(frontendAssets embed.FS) {
	assets = frontendAssets

	// Pick the network before anything opens a file or a connection.
	if err := SelectNetwork(requestedNetworkName(os.Args)); err != nil {
		fmt.Println("🚨 " + err.Error())
		os.Exit(1)
	}
	applySeedOverride(os.Args)
	if Params.Name != testnet1Params.Name {
		fmt.Printf("🌐 NETWORK: %s — %s\n", Params.Name, Params.Description)
		if len(Params.SeedHosts) == 0 {
			fmt.Println("🌐 This network has no seeds. To join other machines, start every one with --seeds HOST,HOST,...")
		}
	}

	for _, arg := range os.Args {
		if arg == "--generate-wallet" {
			privHex, pubHex, walletAddress, err := GenerateKeyPair()
			if err != nil {
				fmt.Printf("🚨 A wallet could not be created: %v\n", err)
				os.Exit(1)
			}
			walletAddress = strings.TrimSpace(walletAddress)

			fmt.Println("\n====================================================================")
			fmt.Println(" NEW WALLET")
			fmt.Println("====================================================================")
			fmt.Printf(" Address (safe to share):   %s\n", walletAddress)
			fmt.Printf(" Public key:                %s\n", pubHex)
			fmt.Printf(" PRIVATE KEY (keep secret): %s\n", privHex)
			fmt.Println("--------------------------------------------------------------------")
			fmt.Println(" Write the private key down now. It is shown this once and saved")
			fmt.Println(" nowhere. Without it the coins at this address can never be spent.")
			fmt.Println(" The address has been saved in miner_config.json as this machine's")
			fmt.Println(" mining address.")
			fmt.Println("====================================================================")

			if err := saveMinerAddress(walletAddress); err != nil {
				fmt.Printf("🚨 miner_config.json could not be written: %v\n", err)
			}
			os.Exit(0)
		}
	}

	// A launch network's genesis block needs its anchor (see genesis.go). A
	// network that is scheduled but whose Bitcoin block has not been mined
	// yet is waited for further down, once it is clear a node is wanted;
	// the wallets and tools below do not need the genesis block.
	anchorErr := resolveGenesisAnchor(os.Args)
	if anchorErr != nil && !errors.Is(anchorErr, errAnchorNotYetKnown) {
		fmt.Println("🚨 " + anchorErr.Error())
		os.Exit(1)
	}
	// A mistyped miner choice is better refused now than discovered later.
	if _, err := chosenMiner(os.Args); err != nil {
		fmt.Println("🚨 " + err.Error())
		os.Exit(1)
	}
	// `--show-genesis` prints what identifies this network's starting point.
	for _, arg := range os.Args {
		if arg == "--show-genesis" {
			if anchorErr != nil {
				fmt.Println("🚨 " + anchorErr.Error())
				os.Exit(1)
			}
			ShowGenesis()
			return
		}
	}

	// `--randomx-bench` measures this machine's RandomX speed and exits.
	for _, arg := range os.Args {
		if arg == "--randomx-bench" {
			RunRandomXBenchmark()
			return
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

	// Decide where rewards go before anything else starts. A node never
	// invents an address: coins mined to an address whose private key nobody
	// holds could never be spent.
	// A node that does not mine earns nothing and needs no address.
	seedOnly := miningSwitchedOff(os.Args)
	if !seedOnly {
		minerAddress, err := resolveMinerAddress(os.Args)
		if err != nil {
			fmt.Println("🚨 " + err.Error())
			os.Exit(1)
		}
		CustomMinerAddress = minerAddress
	}

	// Everything is in order for a node to run. If the network has not
	// started yet, wait here for its anchor (see anchor_wait.go).
	if anchorErr != nil {
		waitForGenesisAnchor(os.Args, 10*time.Second, time.Minute)
	}

	InitBoltEngine()
	if err := checkStoredGenesis(); err != nil {
		fmt.Println("🚨 " + err.Error())
		os.Exit(1)
	}

	// Sync atomic architecture tracker to database height upon boot
	StateMutex.RLock()
	atomic.StoreUint64(&GlobalHeightSolverLatch, uint64(GetLatestBlockInternal().Index))
	StateMutex.RUnlock()

	if seedOnly {
		log.Println("🌱 This node does not mine. It keeps the chain, checks every block and passes blocks on.")
	} else {
		log.Printf("🪪 Mining rewards go to %s\n", CustomMinerAddress)
	}

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

				// A full recount reads every coin. Anyone can ask for this
				// page, as often as they like, so it is recounted at most
				// every few seconds; the running totals are kept up to date
				// between recounts as blocks arrive.
				recountBalancesIfStale()

				BalanceCacheMutex.RLock()
				activeWallets := make(map[string]float64)
				for addr, bal := range StateBalanceCache {
					if bal > 0 {
						activeWallets[addr] = UnitsAmount(bal).CVN()
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
					peerList = seedGossipAddresses()
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

			StateMutex.RLock()
			currentTip := GetLatestBlockInternal().Index
			StateMutex.RUnlock()
			blocks := blocksForBatch(startHeight, currentTip)
			json.NewEncoder(w).Encode(blocks)
		})

		// Height, identifier and total work of the newest block, without its
		// transactions: what a node comparing chains needs, at a fraction of
		// the size of /block.
		winMux.HandleFunc("/tip", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Content-Type", "application/json")
			latest := GetLatestBlockInternal()
			json.NewEncoder(w).Encode(map[string]interface{}{
				"current_height": latest.Index,
				"block_hash":     latest.Hash,
				"timestamp":      latest.Timestamp,
				"total_work":     chainWorkThrough(latest.Index).String(),
			})
		})

		// The identifier of the block at one height: a small answer for a
		// node working out where its chain parts from this one.
		winMux.HandleFunc("/blockhash", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Content-Type", "application/json")
			height, err := strconv.ParseInt(r.URL.Query().Get("height"), 10, 64)
			if err != nil || height < 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			block := GetBlockByHeightFromDB(height)
			if block.Hash == "" || block.Index != height {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"height": height, "hash": block.Hash})
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
				// Units held by the vault (always 0 on a network without one).
				"vault_units": VaultBalanceUnits(),
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
				"address": targetAddress,
				// balance is in CVN, for display; balance_units is the exact figure.
				"balance":        UnitsAmount(balance).CVN(),
				"balance_units":  balance,
				"exists":         exists,
				"account_status": "VALIDATED_ON_CHAIN",
			}
			json.NewEncoder(w).Encode(payload)
		})
		fmt.Printf("📡 [PORT %d] Initializing HTTP Bulk Sync api channels...\n", Params.SyncPort)
		if err := serveHTTP(fmt.Sprintf(":%d", Params.SyncPort), winMux); err != nil {
			if runtime.GOOS == "windows" {
				_ = serveHTTP(fmt.Sprintf(":%d", Params.SyncFallbackPort), winMux)
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
	go func() { _ = serveHTTP(fmt.Sprintf(":%d", Params.StatusPort), mirrorMux) }()
	// Catch up with the network before mining starts (see sync_pass.go).
	runInitialDownload(os.Args)
	go runChainLagWatchdog()
	// Decide once before the miner starts whether this node is caught up, then
	// keep checking. See mining_readiness.go.
	evaluateMiningReadiness()
	go runMiningReadinessMonitor()
	// On a RandomX network, also offer work to external mining software.
	// If the fast miner was chosen, start it; the built-in miner below then
	// stays idle unless the fast miner cannot run (see fast_miner.go).
	if seedOnly {
		announceNetworkStatus("🌱 [SEED] Mining is switched off on this node (--no-mining). It keeps the chain, checks every block and passes blocks on.")
	} else {
		StartChosenMiner(StartMiningWorkServer())
		go runVerifyKeyWarmer()
		go runTableJanitor()
		StartContextMinerEngine()
	}
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
			localMemoryCoins := UnitsAmount(StateBalanceCache[CustomMinerAddress]).CVN()
			BalanceCacheMutex.RUnlock()
			if loopCounter == 1 || loopCounter%15 == 0 {
				go func(targetAddress string) {
					client := &http.Client{Timeout: 2 * time.Second}
					resp, err := client.Get(fmt.Sprintf("http://localhost:%d/audit?address=%s", Params.SyncPort, targetAddress))
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
			// While the fast miner does the work, show the speed it reports.
			if externalSpeed, ok := fastMinerHashrate(); ok {
				realHashrateMH = externalSpeed / 1000000.0
			}
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
				if seedOnly {
					liveTerminalEventLogs = append(liveTerminalEventLogs, fmt.Sprintf("[%s] 🌱 [SEED] Not mining. Following the network // Ledger Height: #%d", time.Now().Format("15:04:05"), actualBlocksHeight))
				} else {
					liveTerminalEventLogs = append(liveTerminalEventLogs, fmt.Sprintf("[%s] ⛏️  [PoD CORE] Hashing active... Performance: %s // Ledger Height: #%d", time.Now().Format("15:04:05"), formatHashrate(realHashrateMH), actualBlocksHeight))
				}
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
			fmt.Printf("🛰️  NETWORK HEIGHT: #%-15d | 📊 15m AVG HASHRATE: %s\n", actualBlocksHeight, formatHashrate(avg15m))
			fmt.Printf("💰 WALLET BALANCE: %-16.2f CVN | 📊 60m AVG HASHRATE: %s\n", actualCvnBalance, formatHashrate(avg1h))
			if seedOnly {
				fmt.Printf("🌱 SEED NODE: NOT MINING            | 📊 24h AVG HASHRATE: %s\n", formatHashrate(avg24h))
			} else {
				fmt.Printf("🪪 RIG IDENTITY: %-18.18s...  | 📊 24h AVG HASHRATE: %s\n", CustomMinerAddress, formatHashrate(avg24h))
			}
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
				fmt.Printf("[%s] ⛏️  [PoD CORE] Hashing active... Performance Throughput: %s // Height: #%d\n", time.Now().Format("15:04:05"), formatHashrate(realHashrateMH), actualBlocksHeight)
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
	coresToAllocate := miningThreads(os.Args)
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
	pinned := false
	var floorTipHash string
	var floorTipFirstSeen int64
	for {
		if atomic.LoadInt32(&IsSyncingActive) == 1 || atomic.LoadInt32(&MiningHoldActive) == 1 || atomic.LoadInt32(&externalMinersConnected) > 0 || atomic.LoadInt32(&fastMinerActive) == 1 {
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
		if heartbeatTemplate && mustWaitForClock(simulatedTime, time.Now().Unix()) {
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
			if workerTargetDifficulty < 1 {
				workerTargetDifficulty = 1
			}
			if workerTargetDifficulty > 8 {
				workerTargetDifficulty = 8
			}
			targetPrefix = strings.Repeat("0", workerTargetDifficulty)
		}
		var guardMatrixPlaceholder = ""
		candidateTransactions, templateUncleHash, templateUncleMiner := buildBlockContents(currentBlock, CustomMinerAddress, simulatedTime, fmt.Sprintf("%d", nonce))
		// The transactions are fixed for this template, so digest them once.
		templateTxDigest := BlockTransactionsDigest(nextIndex, candidateTransactions)
		randomXTemplate := powAlgorithmAt(nextIndex) == PowRandomX
		if randomXTemplate && !pinned {
			pinned = true
			pinMiningThread(int(workerID))
		}
		var randomXKey []byte
		randomXCommitment := ""
		if randomXTemplate {
			var keyKnown bool
			if randomXKey, keyKnown = powKeyFor(nextIndex); !keyKnown {
				time.Sleep(250 * time.Millisecond)
				continue
			}
			// Everything in the block except the nonce is fixed for this template,
			// so it is committed to once; only the nonce changes per attempt.
			randomXCommitment = CalculateHashWithTxDigest(Block{
				Version: 1, Index: nextIndex, Timestamp: simulatedTime, PrevHash: parentBlockTipHash,
				Difficulty: workerTargetDifficulty, GuardMatrix: guardMatrixPlaceholder,
				UncleHash: templateUncleHash, UncleMiner: templateUncleMiner,
			}, templateTxDigest)
		}
		templateBuilt := time.Now()
		for i := 0; i < 100000; i++ {
			// A RandomX hash takes milliseconds, so a hundred thousand of
			// them is minutes. The block's date, and the transfers in it,
			// are renewed at least this often.
			if randomXTemplate && i%64 == 0 && time.Since(templateBuilt) > minerTemplateLife {
				break
			}
			nonce++
			atomic.AddUint64(&globalHashCount, 1)
			// Abandon the template if the tip moved or was replaced by a competing
			// block at the same height; otherwise we would mine on a stale parent.
			// A RandomX hash takes milliseconds, so there the tip is looked at every
			// few hashes: often enough to react at once, without a database read
			// per hash.
			if !randomXTemplate || i%4 == 0 {
				StateMutex.RLock()
				actualTipCheck := GetLatestBlockInternal()
				StateMutex.RUnlock()
				if actualTipCheck.Index != (nextIndex-1) || actualTipCheck.Hash != parentBlockTipHash {
					break
				}
			}
			// Under RandomX the nonce is four bytes.
			templateNonce := nonce
			if randomXTemplate {
				templateNonce = int64(uint32(nonce))
			}
			mockTemplateBlock := Block{
				Version:      1,
				Index:        nextIndex,
				Timestamp:    simulatedTime,
				Transactions: candidateTransactions,
				PrevHash:     parentBlockTipHash,
				Hash:         "",
				Difficulty:   workerTargetDifficulty,
				Nonce:        templateNonce,
				GuardMatrix:  guardMatrixPlaceholder,
				UncleHash:    templateUncleHash,
				UncleMiner:   templateUncleMiner,
			}
			// 👑 ABSOLUTE CONGRUENCE PASS: Calculate hash using canonical method blueprint safely
			// Under RandomX the identifier is only needed for a nonce that has the
			// work, so it is computed then; under SHA-256 it is the work itself.
			computedHashHex := ""
			if !randomXTemplate {
				computedHashHex = CalculateHashWithTxDigest(mockTemplateBlock, templateTxDigest)
			}
			solved := false
			if randomXTemplate {
				// The work is the RandomX hash of the mining blob under this height's key.
				blob, blobOK := powBlob(randomXCommitment, uint32(templateNonce))
				if workHex, ok := randomXMine(workerID, randomXKey, blob); ok && blobOK {
					solved = workHex <= heartbeatTargetHex
					if solved {
						computedHashHex = CalculateHashWithTxDigest(mockTemplateBlock, templateTxDigest)
					}
				} else {
					time.Sleep(200 * time.Millisecond)
				}
			} else {
				solved = (heartbeatTemplate && computedHashHex <= heartbeatTargetHex) || (!heartbeatTemplate && strings.HasPrefix(computedHashHex, targetPrefix))
			}
			if solved {
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
					logLine := fmt.Sprintf("[%s] 🎉 [SUCCESS] Solved valid block #%-3d | Hash: %s | Performance: %s", time.Now().Format("15:04:05"), mockTemplateBlock.Index, mockTemplateBlock.Hash[:16], formatHashrate(currentLiveHashrateTrack))
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

// buildBlockContents assembles what goes into a new block on top of `tip`:
// the reward transaction paying minerAddress (and the uncle's miner when one
// is owed), followed by the waiting transfers. It is used by the built-in
// miner and by the work server for external miners, so both build identical
// blocks. `uniqueness` only has to differ between attempts; it keeps reward
// transaction IDs distinct.
func buildBlockContents(tip Block, minerAddress string, timestamp int64, uniqueness string) ([]Transaction, string, string) {
	nextIndex := tip.Index + 1
	coinbaseTxID := rewardTransactionID(nextIndex, uniqueness)
	var coinbaseOutputs []UTXOOutput
	// From the transaction fork on, waiting transfers that are valid on top of
	// this tip go into the block, and their fees are added to the miner's reward.
	var templateTransfers []Transaction
	var feeUnits int64
	if IsTxForkHeight(nextIndex) {
		templateTransfers, feeUnits = selectionForTip(tip.Hash, TxMaxPerBlock-1)
	}
	// If the tip won a same-height race, name the losing block as an uncle and
	// split this block's reward 80/20 with its miner. Total emission is unchanged.
	// Everything is whole units, so the amounts written into the block are
	// exact. rewardUnits is the new reward plus the vault's payout for this
	// block; feeUnits becomes the miner's share of the fees.
	accounting := accountForBlock(nextIndex, feeUnits, vaultUnitsAfter(tip))
	rewardUnits := accounting.SubsidyUnits()
	feeUnits = accounting.MinerFeeUnits
	templateUncleHash := "0000000000000000000000000000000000000000000000000000000000000000"
	templateUncleMiner := "NONE"
	if pendingUncleHash, pendingUncleMiner, hasUncle := GetPendingUncleForTip(tip); hasUncle && rewardUnits/5 > 0 && !Params.NoUncles {
		templateUncleHash = pendingUncleHash
		templateUncleMiner = pendingUncleMiner
		// Whole units: the uncle gets a fifth of the new reward, rounded down, and
		// the miner the rest plus the fees, so the two always add up exactly.
		uncleUnits := rewardUnits / 5
		coinbaseOutputs = append(coinbaseOutputs, UTXOOutput{Recipient: minerAddress, Amount: UnitsAmount(rewardUnits - uncleUnits + feeUnits)})
		coinbaseOutputs = append(coinbaseOutputs, UTXOOutput{Recipient: pendingUncleMiner, Amount: UnitsAmount(uncleUnits)})
	} else if rewardUnits+feeUnits >= minimumOutputUnits() {
		// An amount too small to be a coin is not paid; it stays in the vault.
		coinbaseOutputs = append(coinbaseOutputs, UTXOOutput{Recipient: minerAddress, Amount: UnitsAmount(rewardUnits + feeUnits)})
	}
	candidateTransactions := []Transaction{
		{
			Version:   1,
			ID:        coinbaseTxID,
			Witness:   minerAddress,
			PublicKey: "GENESIS_VOID_REWARD_POOL",
			Inputs:    []UTXOInput{},
			Outputs:   coinbaseOutputs,
			Timestamp: timestamp,
			Signature: "COINBASE_IMMUTABLE_CONSENSUS_VALID",
		},
	}
	candidateTransactions = append(candidateTransactions, templateTransfers...)
	return candidateTransactions, templateUncleHash, templateUncleMiner
}
