package main

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
// Two networks are defined:
//
//   - testnet-1 is the chain that has been running since 29 September 2026.
//     It is the default, and every value below is exactly what was previously
//     written into the code in many places. Earlier documents call it
//     "mainnet"; it is a test network.
//
//   - devnet is where the rules of the new genesis (GENESIS_SPEC) are built
//     and tried. It has its own key, ports and files, so it can run on the
//     same machine as a testnet-1 node and can be wiped at any time. It has no
//     public seeds.
//
// Choose with `--network devnet` or the CVN_NETWORK environment variable.

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

	DatabaseFile  string
	PeerCacheFile string

	// SeedHosts are the always-on nodes a new node contacts first.
	SeedHosts []string
	// ExtraSyncURLs are further block-download addresses tried during a
	// re-sync, beyond SeedHosts on SyncPort.
	ExtraSyncURLs []string
	// WindowsSelfSkipHost is a seed that Windows nodes do not download from
	// on SyncPort (the testnet-1 seed that itself runs on Windows).
	WindowsSelfSkipHost string

	GenesisTimestamp int64

	// AmountsAsUnits says how amounts are written into blocks: as whole
	// units when set, as decimal CVN numbers when not (testnet-1 only; see
	// amount.go).
	AmountsAsUnits bool

	// Heights at which rule changes switch on.
	HeartbeatHeight int64
	TxForkHeight    int64

	// HeartbeatInitialDifficulty is the difficulty the first heartbeat block
	// starts from.
	HeartbeatInitialDifficulty int64

	// ScheduleAnchored makes the difficulty rule also steer the chain toward
	// exactly one block per target interval, counted from block 1, so that
	// block heights line up with the calendar over the long run.
	ScheduleAnchored bool

	// Economics, when set, is the launch reward schedule. When nil the
	// network uses testnet-1's original schedule.
	Economics *GenesisEconomics
}

var testnet1Params = ChainParams{
	Name:        "testnet-1",
	Description: "The test network running since 29 September 2026",

	AuthKey: "COVENANT_STANDARD_MAINNET_IGNITION_AUTHENTICATION_KEY_TOKEN_2026",

	GossipPort:       8080,
	SyncPort:         8081,
	SyncFallbackPort: 9091,
	StatusPort:       8082,

	DatabaseFile:  "cvn_mainnet.db",
	PeerCacheFile: "peers.json",

	SeedHosts:           []string{"202.137.175.220", "207.148.67.11", "64.177.45.153"},
	ExtraSyncURLs:       []string{"http://202.137.175.220:9091"},
	WindowsSelfSkipHost: "202.137.175.220",

	GenesisTimestamp: 1790640000,

	HeartbeatHeight: 2500,
	TxForkHeight:    6100,

	HeartbeatInitialDifficulty: 40000000,
}

var devnetParams = ChainParams{
	Name:        "devnet",
	Description: "Development network for the new genesis rules; may be wiped at any time",

	AuthKey: "CVN_DEVNET_NODE_MESSAGE_KEY_NOT_FOR_REAL_VALUE_2026",

	GossipPort:       18080,
	SyncPort:         18081,
	SyncFallbackPort: 19091,
	StatusPort:       18082,

	DatabaseFile:  "cvn_devnet.db",
	PeerCacheFile: "peers_devnet.json",

	GenesisTimestamp: 1794614400, // 14 November 2026 00:00 UTC

	AmountsAsUnits: true,

	// Launch rules: the heartbeat difficulty rule and transfer validation
	// apply from the first block, the chain is steered to an exact 45-second
	// average, and rewards follow the launch schedule.
	HeartbeatHeight: 1,
	TxForkHeight:    1,

	// The lowest difficulty, so a single development machine can mine.
	HeartbeatInitialDifficulty: HeartbeatMinDifficulty,
	ScheduleAnchored:           true,
	Economics:                  &NewGenesisEconomics,
}

// Params is the network this process runs on.
var Params = testnet1Params

// KnownNetworks lists the names SelectNetwork accepts.
func KnownNetworks() []string {
	return []string{testnet1Params.Name, devnetParams.Name}
}

// SelectNetwork switches this process to the named network. It must be called
// before the database is opened or any connection is made.
func SelectNetwork(name string) error {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", testnet1Params.Name, "testnet", "mainnet":
		Params = testnet1Params
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
}

// applyParams copies the chosen network's values into the variables the rest
// of the program reads.
func applyParams() {
	BlockchainFile = Params.DatabaseFile
	HeartbeatActivationHeight = Params.HeartbeatHeight
	TxForkActivationHeight = Params.TxForkHeight
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

// GenesisEconomics holds the numbers decided for the new genesis. Nothing
// reads them for consensus yet; they are written down here, with tests, so
// each rule can be built against one agreed set of values.
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
