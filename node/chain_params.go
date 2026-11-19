package node

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// CHAIN SETTINGS
//
// Everything that tells one network apart from another lives here: its name,
// the key its nodes sign messages with, its ports, its files, its seed nodes,
// its genesis block and the heights at which its rule changes switch on.
//
// Two networks are defined. Both run the same rules; they differ in their
// names, keys, ports, files, seeds and starting difficulty.
//
//   - cvn_testnet is the CVN Testnet, the public test of the launch rules. It
//     is the default: a program started with no network named runs on it.
//
//   - devnet is the development network, where the rules are built, tried
//     and rehearsed. It can be wiped at any time and has no public seeds.
//
// An earlier network, testnet-1, ran from 29 September 2026 on a simpler
// rule set and has been retired; this program can no longer follow it.
//
// Choose with `--network NAME` or the CVN_NETWORK environment variable.

// ChainParams is one network's settings.
type ChainParams struct {
	Name        string
	Description string

	// AuthKey signs every message between nodes. Nodes of different networks
	// reject each other's messages because their keys differ.
	AuthKey string

	GossipPort       int // blocks and transfers between nodes
	SyncPort         int // block download, wallet and explorer requests
	SyncFallbackPort int // used on Windows when SyncPort is taken
	StatusPort       int // small status page
	// StratumPort is where external mining software fetches work (see
	// stratum.go). Zero on networks that do not mine with RandomX.
	StratumPort int

	DatabaseFile  string
	PeerCacheFile string

	// SeedHosts are the always-on nodes a new node contacts first.
	SeedHosts []string
	// ExtraSyncURLs are further block-download addresses tried during a
	// re-sync, beyond SeedHosts on SyncPort.
	ExtraSyncURLs []string
	// WindowsSelfSkipHost is a seed that Windows nodes do not download from
	// on SyncPort (a seed that itself runs on Windows). Unused at present.
	WindowsSelfSkipHost string

	GenesisTimestamp int64
	// Anchor ties the genesis block to a moment in time (see genesis.go).
	Anchor GenesisAnchor

	// MaxBlockBytes limits the size of a block (see blockSizeBytes). Zero
	// means the network has no such limit.
	MaxBlockBytes int

	// HeartbeatInitialDifficulty is the difficulty the first block starts
	// from.
	HeartbeatInitialDifficulty int64

	// ReliefNotInherited keeps the relief given to a late block (difficulty
	// halved after six minutes without a block) to that block alone: the
	// next block is worked out from the difficulty its parent would have had
	// without it. Otherwise one long gap leaves dozens of cheap blocks for
	// whoever has the most hashrate (see heartbeat_difficulty.go).
	ReliefNotInherited bool

	// ExponentialSteps applies each block's difficulty step as e^x where
	// the first form of the rule applied 1 + x, so that a run of steps
	// depends only on how much time the blocks' dates span and a miner
	// gains nothing by choosing dates (see heartbeat_difficulty.go).
	//
	// This and ReliefNotInherited are on for every network. They remain
	// settings only so that a chain mined before them can still be
	// replayed, to check that nothing else has changed.
	ExponentialSteps bool

	// Economics is the reward schedule, the vault, the sweep and the limits
	// that go with them (see GenesisEconomics).
	Economics *GenesisEconomics

	// PowSchedule says which mining algorithm applies from which height
	// (see pow.go). Empty means SHA-256 throughout, which only tests use.
	PowSchedule []PowEra
	// KeyEpochBlocks is how often the RandomX key changes, and KeyLagBlocks
	// how far behind the tip the key block must be.
	KeyEpochBlocks int64
	KeyLagBlocks   int64
}

// publicSeedHosts are the always-on nodes of the public networks. They keep
// the chain and pass blocks on, and none of them mines: a seed is the
// best-connected place on the network to mine from, and nobody should have
// that. They are spread over several regions so that no single outage takes
// them all away and so that a miner anywhere has one nearby.
var publicSeedHosts = []string{
	"207.148.67.11",  // Singapore
	"64.177.44.185",  // Atlanta, United States
	"149.28.69.173",  // Los Angeles, United States
	"149.248.54.9",   // Toronto, Canada
	"95.179.179.191", // Amsterdam, Netherlands
	"65.20.67.25",    // Mumbai, India
}

var devnetParams = ChainParams{
	Name:        "devnet",
	Description: "Development network, for building and rehearsing the launch rules; may be wiped at any time",

	AuthKey: "CVN_DEVNET_NODE_MESSAGE_KEY_NOT_FOR_REAL_VALUE_2026",

	GossipPort:       28080,
	SyncPort:         28081,
	SyncFallbackPort: 29091,
	StatusPort:       28082,
	StratumPort:      28083,

	DatabaseFile:  "cvn_devnet.db",
	PeerCacheFile: "peers_devnet.json",

	GenesisTimestamp: 1794614400, // 14 November 2026 00:00 UTC
	// A development network proves nothing about when it started.
	Anchor: GenesisAnchor{Placeholder: "development network, no anchor, no value"},

	MaxBlockBytes: 300000,

	// RandomX from the first block. The key changes once a day (1,920 blocks)
	// and the key block is always more than 100 blocks behind, matching the
	// reorganisation limit. Difficulty starts at the minimum so that a single
	// development machine can mine.
	PowSchedule:                []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 5000}},
	KeyEpochBlocks:             1920,
	KeyLagBlocks:               100,
	HeartbeatInitialDifficulty: 5000,
	ReliefNotInherited:         true,
	ExponentialSteps:           true,
	Economics:                  &NewGenesisEconomics,
}

// cvnTestnetParams is the public test network of the launch rules: the full
// rehearsal for the real launch. Its coins have no value.
//
// It starts the way the real network will: from a Bitcoin block named in
// advance (see genesis.go). Until that height is announced the network is
// marked unscheduled and cannot be started. Scheduling it is one edit here,
// made in the release that announces the start:
//
//	Anchor: GenesisAnchor{
//		BitcoinHeight: <the announced height>,
//		NotBefore:     <the time of the announcement, in seconds since 1970>,
//	},
//
// Both are needed. A Bitcoin header names its block by hash, not by height,
// so NotBefore is what stops a block mined before the announcement being
// passed off as the anchor; a test refuses a release that sets one without
// the other. In a later release, once that Bitcoin block exists, its header
// goes in Anchor.BitcoinHeader so that newcomers need not supply it.
var cvnTestnetParams = ChainParams{
	Name:        "cvn_testnet",
	Description: "CVN Testnet: the public test network of the launch rules. Its coins have no value",

	AuthKey: "CVN_TESTNET_NODE_MESSAGE_KEY_COINS_HAVE_NO_VALUE_2026",

	GossipPort:       18080,
	SyncPort:         18081,
	SyncFallbackPort: 19091,
	StatusPort:       18082,
	StratumPort:      18083,

	DatabaseFile:  "cvn_testnet.db",
	PeerCacheFile: "peers_testnet.json",

	SeedHosts: publicSeedHosts,

	// The genesis block's date is the anchor block's own timestamp less two
	// hours, set when the anchor is supplied.
	Anchor: GenesisAnchor{Unscheduled: true},

	MaxBlockBytes: 300000,

	// The minimum is what one slow processor thread can still meet in about
	// a block interval. The start is set for the handful of machines expected
	// on the first day: about 22,000 hashes per second meets it once every
	// 45 seconds. Too high corrects itself (difficulty halves after six
	// minutes without a block); too low hands the first miner a burst of
	// fast blocks, so the guess errs high.
	PowSchedule:                []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 5000}},
	KeyEpochBlocks:             1920,
	KeyLagBlocks:               100,
	HeartbeatInitialDifficulty: 1000000,
	ReliefNotInherited:         true,
	ExponentialSteps:           true,
	Economics:                  &NewGenesisEconomics,
}

// Params is the network this process runs on. The program calls
// SelectNetwork before anything else, and with no network named that chooses
// the CVN Testnet. The value here is only what holds before that call.
var Params = cvnTestnetParams

// defaultNetwork is the network a program runs on when none is named.
var defaultNetwork = &cvnTestnetParams

// KnownNetworks lists the names SelectNetwork accepts.
func KnownNetworks() []string {
	return []string{cvnTestnetParams.Name, devnetParams.Name}
}

// SelectNetwork switches this process to the named network. It must be called
// before the database is opened or any connection is made.
func SelectNetwork(name string) error {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "":
		Params = *defaultNetwork
	case "testnet-1", "testnet1":
		return fmt.Errorf("testnet-1, the first test network, has been retired and this program can no longer follow it (known networks: %s)", strings.Join(KnownNetworks(), ", "))
	case cvnTestnetParams.Name, "cvn-testnet", "cvn testnet", "cvntestnet":
		Params = cvnTestnetParams
	case devnetParams.Name:
		Params = devnetParams
		applyDevnetOverrides()
	default:
		return fmt.Errorf("unknown network %q (known: %s)", name, strings.Join(KnownNetworks(), ", "))
	}
	applyParams()
	return nil
}

// applyDevnetOverrides lets a developer shorten the sweep interval and the
// vault's payout period on devnet, so that behaviour which takes years on the
// launch schedule can be watched in minutes:
//
//	CVN_DEVNET_SWEEP_BLOCKS=30   sweep a coin after 30 blocks instead of 7 years
//	CVN_DEVNET_VAULT_BLOCKS=100  pay out 1/100 of the vault per block
//	CVN_DEVNET_ANCHOR_HEIGHT=N   rehearse a launch: start from Bitcoin block N
//
// Every node on a devnet must use the same values, and a database made with
// one setting is not valid under another. No other network reads these.
func applyDevnetOverrides() {
	economics := *devnetParams.Economics
	if blocks, err := strconv.ParseInt(os.Getenv("CVN_DEVNET_SWEEP_BLOCKS"), 10, 64); err == nil && blocks > 0 {
		economics.SweepIntervalBlocks = blocks
	}
	if blocks, err := strconv.ParseInt(os.Getenv("CVN_DEVNET_VAULT_BLOCKS"), 10, 64); err == nil && blocks > 0 {
		economics.VaultPayoutBlocks = blocks
	}
	Params.Economics = &economics

	// CVN_DEVNET_ANCHOR_HEIGHT=N turns devnet into a rehearsal of a launch:
	// it starts from Bitcoin block N exactly as a launch network does,
	// waiting for that block if it has not been mined yet.
	if height, err := strconv.ParseInt(os.Getenv("CVN_DEVNET_ANCHOR_HEIGHT"), 10, 64); err == nil && height > 0 {
		Params.Anchor = GenesisAnchor{BitcoinHeight: height}
		Params.GenesisTimestamp = 0
	}
}

// applyParams copies the chosen network's values into the variables the rest
// of the program reads.
func applyParams() {
	BlockchainFile = Params.DatabaseFile
	resetUnrelievedCache()
}

// applySeedOverride lets a network that has no built-in seeds be given some
// when the program starts: `--seeds HOST,HOST,...` or the CVN_SEEDS
// environment variable. It is how a development network is run across
// several machines. A network with built-in seeds ignores it.
//
// It returns the entries it could not read. They are not passed over: a seed
// left out without a word is how a newcomer ends up mining alone.
func applySeedOverride(args []string) (unreadable []string) {
	if len(Params.SeedHosts) > 0 {
		return nil
	}
	list := argumentValue(args, "--seeds")
	if list == "" {
		list = os.Getenv("CVN_SEEDS")
	}
	var hosts []string
	for _, host := range strings.Split(list, ",") {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if len(host) <= 253 && !strings.ContainsAny(host, " /:@") {
			hosts = append(hosts, host)
		} else {
			unreadable = append(unreadable, host)
		}
	}
	Params.SeedHosts = hosts
	return unreadable
}

// requestedNetworkName reads the network asked for on the command line
// (`--network NAME`) or, failing that, in CVN_NETWORK.
func requestedNetworkName(args []string) string {
	for i, arg := range args {
		if arg == "--network" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--network=") {
			return strings.TrimPrefix(arg, "--network=")
		}
	}
	return os.Getenv("CVN_NETWORK")
}

func (p ChainParams) gossipPortText() string { return strconv.Itoa(p.GossipPort) }
func (p ChainParams) syncPortText() string   { return strconv.Itoa(p.SyncPort) }

// syncURL is the block-download address of a host on this network.
func (p ChainParams) syncURL(host string) string {
	return "http://" + net.JoinHostPort(host, p.syncPortText())
}

// gossipAddress is the node-to-node address of a host on this network.
func (p ChainParams) gossipAddress(host string) string {
	return net.JoinHostPort(host, p.gossipPortText())
}

// seedSyncURLs lists every seed's block-download address.
func seedSyncURLs() []string {
	urls := make([]string, 0, len(Params.SeedHosts))
	for _, host := range Params.SeedHosts {
		urls = append(urls, Params.syncURL(host))
	}
	return urls
}

// skipSeedOnThisMachine reports whether a block-download address should be
// left out on this machine (see WindowsSelfSkipHost).
func skipSeedOnThisMachine(endpoint string) bool {
	// A seed is given the list of all the seeds, itself among them. It does
	// not download blocks from itself.
	if isOwnSyncURL(endpoint) {
		return true
	}
	return Params.WindowsSelfSkipHost != "" && runtime.GOOS == "windows" &&
		strings.Contains(endpoint, Params.WindowsSelfSkipHost)
}

// reachableSeedSyncURLs is seedSyncURLs without the ones this machine skips.
func reachableSeedSyncURLs() []string {
	var urls []string
	for _, url := range seedSyncURLs() {
		if !skipSeedOnThisMachine(url) {
			urls = append(urls, url)
		}
	}
	return urls
}

// seedGossipAddresses lists every seed's node-to-node address.
func seedGossipAddresses() []string {
	addresses := make([]string, 0, len(Params.SeedHosts))
	for _, host := range Params.SeedHosts {
		addresses = append(addresses, Params.gossipAddress(host))
	}
	return addresses
}

// localSyncURLs are the addresses of a node running on this same machine.
func localSyncURLs() []string {
	return []string{
		"http://localhost:" + Params.syncPortText(),
		"http://localhost:" + strconv.Itoa(Params.SyncFallbackPort),
	}
}

// localStatusURL is the status page of a node running on this same machine.
func localStatusURL() string {
	return "http://localhost:" + strconv.Itoa(Params.StatusPort) + "/status"
}

// ---------------------------------------------------------------------------
// New-genesis economics (GENESIS_SPEC)
// ---------------------------------------------------------------------------

// GenesisEconomics holds the numbers of the reward schedule, the vault, the
// sweep and the limits that go with them. These are the values every node
// enforces; they are written down in one place, with tests, so each rule is
// built against one agreed set.
type GenesisEconomics struct {
	BlockSeconds int64 // target seconds per block

	EpochBlocks        int64 // one Sabbatical epoch (7 years)
	EmissionEpochs     int64 // epochs with a block reward
	InitialRewardUnits int64 // reward in the first epoch

	SweepIntervalBlocks int64 // an output unmoved this long is swept
	SweepDivisor        int64 // swept share is 1/SweepDivisor (5 = 20%)

	VaultPayoutBlocks int64 // each block pays vault balance / this

	FeeVaultDivisor int64 // vault's share of fees is 1/this while emission lasts

	MinOutputUnits int64 // smallest output that may be created

	ReorgLimitBlocks int64 // a node will not replace more of its history than this
}

// NewGenesisEconomics are the values confirmed on 13 November 2026.
var NewGenesisEconomics = GenesisEconomics{
	BlockSeconds: 45,

	EpochBlocks:        4905600,
	EmissionEpochs:     7,
	InitialRewardUnits: 50 * UnitsPerCVN,

	SweepIntervalBlocks: 4905600,
	SweepDivisor:        5,

	VaultPayoutBlocks: 9811200,

	FeeVaultDivisor: 5,

	MinOutputUnits: 10000,

	ReorgLimitBlocks: 100,
}

// EmissionEndHeight is the last height that carries a block reward.
func (e GenesisEconomics) EmissionEndHeight() int64 {
	return e.EpochBlocks * e.EmissionEpochs
}

// RewardUnitsAt is the newly issued reward at a height: the initial reward,
// halved at the start of each epoch, and nothing after the last epoch. The
// genesis block (height 0) pays nothing.
func (e GenesisEconomics) RewardUnitsAt(height int64) int64 {
	if height < 1 || height > e.EmissionEndHeight() {
		return 0
	}
	epoch := (height - 1) / e.EpochBlocks // 0 for the first epoch
	return e.InitialRewardUnits >> uint(epoch)
}

// TotalSupplyUnits is everything that will ever be issued.
func (e GenesisEconomics) TotalSupplyUnits() int64 {
	var total int64
	for epoch := int64(0); epoch < e.EmissionEpochs; epoch++ {
		total += (e.InitialRewardUnits >> uint(epoch)) * e.EpochBlocks
	}
	return total
}
