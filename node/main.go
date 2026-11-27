package node

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// assets holds the desktop wallet's screens. They are packed into the program
// by the start-up file in the project's main folder and handed to Run.
var assets embed.FS

// globalHashCount counts the hashes the built-in miner has tried since the
// miner window last read it.
var globalHashCount uint64

// liveTerminalEventLogs are the lines shown under "recent activity" in the
// miner window, oldest first. logMutex guards them.
var liveTerminalEventLogs []string
var logMutex sync.Mutex

// The mining speed at each reading, one every four seconds, kept for as long
// as each average covers (see dashboard.go). hashrateMutex guards them.
var hashRateHistory15m []float64
var hashRateHistory1h []float64
var hashRateHistory24h []float64
var hashrateMutex sync.Mutex

// currentLiveHashrateTrack is the speed at the latest reading.
var currentLiveHashrateTrack float64

// The built-in miner's workers (see builtin_miner.go).
var miningWg sync.WaitGroup
var miningContext context.Context
var miningMutex sync.Mutex

// heightClaimedByMiner is the highest block height one of the built-in
// miner's workers has claimed, so that two workers that find a block at the
// same height do not both store one (see miningWorker).
var heightClaimedByMiner uint64

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
	if unreadable := applySeedOverride(os.Args); len(unreadable) > 0 {
		fmt.Printf("🚨 These addresses given with --seeds (or in CVN_SEEDS) could not be read: %s\n", strings.Join(unreadable, "   "))
		fmt.Println("   Give each machine's address alone, with commas between them and nothing else:")
		fmt.Println("   no spaces, no port number, no http://. For example:  --seeds 203.0.113.5,203.0.113.9")
		os.Exit(1)
	}
	fmt.Printf("🌐 NETWORK: %s — %s\n", Params.Name, Params.Description)
	if len(Params.SeedHosts) == 0 {
		fmt.Println("🌐 This network has no seeds. To join other machines, start every one with --seeds HOST,HOST,...")
	}

	if hasArgument(os.Args, "--generate-wallet") {
		generateWalletAndExit()
	}

	// A launch network's genesis block needs its anchor (see genesis.go). A
	// network that is scheduled but whose Bitcoin block has not been mined
	// yet is waited for further down, once it is clear a node is wanted.
	// The wallets below do not need the genesis block, so they open even on
	// a network that has not been scheduled: an address can be made, and
	// published, before the start is announced. A node cannot run there, and
	// is refused once the wallets have had their turn.
	anchorErr := resolveGenesisAnchor(os.Args)
	anchorRefused := anchorErr != nil && !errors.Is(anchorErr, errAnchorNotYetKnown)
	// A settings file that cannot be read is refused, not passed over: its
	// owner wrote it to be used.
	if err := minerConfigProblem(); err != nil {
		fmt.Println("🚨 " + err.Error())
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

	// `--check-backup FILE` and `--restore-backup FILE` (see backup.go).
	// A network whose start is refused outright is reported further down.
	if !anchorRefused && runBackupCommand(os.Args, anchorErr == nil) {
		return
	}
	// `--check-pool-backup FILE` and `--restore-pool-backup FILE`
	// (pool_backup.go).
	if runPoolBackupCommand(os.Args) {
		return
	}
	// Tools that exist only in a program built for testing (none in a release).
	if !anchorRefused && anchorErr == nil && devToolCommand != nil && devToolCommand(os.Args) {
		return
	}

	// `--pool-pay` signs the payments the pool on this machine has prepared,
	// and never starts a node (pool_pay.go).
	if paying, _ := poolPayRequested(os.Args); paying {
		os.Exit(RunPoolPay(os.Args))
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
	if anchorRefused {
		fmt.Println("🚨 " + anchorErr.Error())
		os.Exit(1)
	}

	// Decide where rewards go before anything else starts. A node never
	// invents an address: coins mined to an address whose private key nobody
	// holds could never be spent.
	// A node that does not mine earns nothing and needs no address.
	seedOnly := miningSwitchedOff(os.Args)
	// A pool mines with other people's machines and not its own: its blocks
	// pay the pool's address, which is given on the command line (pool.go).
	pool, isPool, err := poolAddressRequested(os.Args)
	if err == nil && isPool && seedOnly {
		err = fmt.Errorf("--pool and --no-mining were both given. A pool builds blocks; a node that does not mine cannot be one")
	}
	if err == nil && isPool && !networkUsesRandomX() {
		err = fmt.Errorf("a pool needs a network mined with RandomX, and %s is not one", Params.Name)
	}
	// `--pool-mine HOST`, or "mine_at_pool" in miner_config.json, mines at
	// somebody's pool with the fast miner and never starts a node
	// (pool_mine.go).
	mineAt, mineAtAskedBy, mineAtErr := poolMineRequested(os.Args)
	if err == nil && mineAtAskedBy != "" {
		switch {
		case isPool:
			err = fmt.Errorf("--pool makes this node a pool, and %s asks it to mine at another. Give one of the two", mineAtAskedBy)
		case seedOnly:
			err = fmt.Errorf("mining is switched off on this node, and %s asks it to mine at a pool. Give one of the two", mineAtAskedBy)
		default:
			err = mineAtErr
		}
	}
	if err != nil {
		fmt.Println("🚨 " + err.Error())
		os.Exit(1)
	}
	if mineAtAskedBy != "" {
		os.Exit(RunPoolMiner(mineAt, mineAtAskedBy, os.Args))
	}
	if isPool {
		poolAddress = pool
		CustomMinerAddress = pool
	} else if !seedOnly {
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

	// A chain file that cannot be trusted stops the program inside
	// InitBoltEngine. The operator is told what is wrong and what to do, and
	// is not shown where in the program it was found.
	func() {
		defer func() {
			if fault := recover(); fault != nil {
				if said, ok := fault.(string); ok && strings.Contains(said, "DATABASE DAMAGED") {
					fmt.Println(said)
					fmt.Println("   If this file is from before the network was started afresh, it is only the old chain: move it away and start the node again.")
					os.Exit(1)
				}
				panic(fault)
			}
		}()
		InitBoltEngine()
	}()
	if err := checkStoredGenesis(); err != nil {
		fmt.Println("🚨 " + err.Error())
		os.Exit(1)
	}

	// The miner's workers may claim heights from the stored chain's upward.
	StateMutex.RLock()
	atomic.StoreUint64(&heightClaimedByMiner, uint64(GetLatestBlockInternal().Index))
	StateMutex.RUnlock()

	if seedOnly {
		log.Println("🌱 This node does not mine. It keeps the chain, checks every block and passes blocks on.")
	} else if isPool {
		log.Printf("⛏️ This node is a pool. Its blocks pay %s\n", poolAddress)
	} else {
		log.Printf("🪪 Mining rewards go to %s\n", CustomMinerAddress)
	}

	go serveSyncAPI()
	StartPeerCache()
	go StartMeshNetwork()
	go func() { _ = serveHTTP(listenAddress(Params.StatusPort), statusAPI()) }()
	// Before anything else, make sure the machines this node was told of are
	// on its chain (see join_check.go).
	joinBeforeMining(os.Args, !seedOnly)
	// Catch up with the network before mining starts (see sync_pass.go).
	runInitialDownload(os.Args)
	beginCountingRaces(GetLatestBlockInternal().Index)
	go runChainLagWatchdog()
	// Decide once before the miner starts whether this node is caught up, then
	// keep checking. See mining_readiness.go.
	evaluateMiningReadiness()
	go runMiningReadinessMonitor()
	go runGenesisWatch(anchorLookupAllowed(os.Args))
	go runDownloadSettler()
	// A checked copy of the chain once a day (see backup.go).
	if every, keep, on := backupSettings(os.Args); on {
		go runBackupSchedule(every, keep)
	}
	// On a RandomX network, also offer work to external mining software.
	// If the fast miner was chosen, start it; the built-in miner below then
	// stays idle unless the fast miner cannot run (see fast_miner.go).
	if seedOnly {
		announceNetworkStatus("🌱 [SEED] Mining is switched off on this node (--no-mining). It keeps the chain, checks every block and passes blocks on.")
	} else if isPool {
		// A pool hands out work and checks it. It does not mine itself: a
		// small server's processor is better spent checking shares.
		if err := startPool(); err != nil {
			fmt.Println("🚨 " + err.Error())
			os.Exit(1)
		}
		go runVerifyKeyWarmer()
		go runTableJanitor()
	} else {
		StartChosenMiner(StartMiningWorkServer())
		go runVerifyKeyWarmer()
		go runTableJanitor()
		startBuiltInMiner()
	}
	runDashboard(seedOnly)
}

// hasArgument reports whether the program was started with one option.
func hasArgument(args []string, option string) bool {
	for _, arg := range args {
		if arg == option {
			return true
		}
	}
	return false
}

// generateWalletAndExit makes a new wallet, shows its key once, saves its
// address as this machine's mining address, and ends the program.
func generateWalletAndExit() {
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
	printRecoveryWords(privHex)
	fmt.Println("--------------------------------------------------------------------")
	fmt.Println(" Write the 24 words down now, on paper, in order. They are the")
	fmt.Println(" private key written another way, with a check that catches a wrong")
	fmt.Println(" word. They are shown this once and saved nowhere. Without the words")
	fmt.Println(" or the key the coins at this address can never be spent.")
	fmt.Println(" The address has been saved in miner_config.json as this machine's")
	fmt.Println(" mining address.")
	fmt.Println("====================================================================")

	if err := saveMinerAddress(walletAddress); err != nil {
		fmt.Printf("🚨 miner_config.json could not be written: %v\n", err)
	}
	os.Exit(0)
}
