//go:build multinode

package node

import (
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof" // what every part of the program is doing, for a test that finds a node stuck
	"os"
	"strconv"
)

// THE LOCAL TEST NETWORK
//
// This file is left out of every ordinary build. It is compiled only with
// `-tags multinode`, for the tests in multinode_test.go, which start several
// real nodes on one machine and watch them work together.
//
// The network it adds, `localnet`, runs the launch rules with two
// differences, both so that a test takes minutes and disturbs nothing:
//
//   - blocks are mined with the fast hash, as in the other tests, not RandomX
//     (unless a test of a pool asks for RandomX: see CVN_LOCALNET_RANDOMX);
//   - every node listens on a loopback address of its own (127.x.x.x), given
//     in CVN_LOCALNET_HOST, and on ports no other network uses. Nothing
//     outside the machine can reach it, and the program refuses any other
//     kind of address.
//
// Settings, read once at start:
//
//	CVN_LOCALNET_HOST        this node's loopback address (required)
//	CVN_LOCALNET_GENESIS     the date of the first block, in Unix seconds.
//	                         Two different values make two different chains.
//	CVN_LOCALNET_DIFFICULTY  the difficulty the first block starts from
//	CVN_LOCALNET_REORG_LIMIT how many of its own blocks a node will replace
//	                         (100 if not given, as on the launch networks)
//	CVN_LOCALNET_RANDOMX     if set, blocks are mined with RandomX after all,
//	                         from a difficulty of a few dozen hashes, and the
//	                         node offers mining work on port 38083. A pool
//	                         needs this: it hands out RandomX work. The miners
//	                         are stand-ins in the test itself.
//	CVN_LOCALNET_POOL_SHARE  the difficulty a pool on this network asks of its
//	                         miners, in place of the tens of thousands a real
//	                         pool starts from
//	CVN_LOCALNET_POOL_FLOOR  the smallest part of a reward, in units, that a
//	                         pool pays in the block itself. Set far above a
//	                         reward, nothing is paid in the block and every
//	                         part is owed: the pool as a keeper of debts

const localnetName = "localnet"

// localnetDebugPort is where a node of the local test network says what each
// part of it is doing (the Go runtime's own report), on its loopback address.
const localnetDebugPort = 38089

// With CVN_LOCALNET_RANDOMX: where a node offers mining work, and the least
// difficulty a block may have.
const (
	localnetWorkPort               = 38083
	localnetRandomXLeastDifficulty = 8
)

func init() {
	extraNetworks[localnetName] = localnetParams
	if ip := net.ParseIP(os.Getenv("CVN_LOCALNET_HOST")); ip != nil && ip.IsLoopback() && os.Getenv("CVN_LOCALNET_DEBUG") != "" {
		go func() {
			_ = http.ListenAndServe(net.JoinHostPort(ip.String(), strconv.Itoa(localnetDebugPort)), nil)
		}()
	}
}

func localnetParams() (ChainParams, error) {
	host := os.Getenv("CVN_LOCALNET_HOST")
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return ChainParams{}, fmt.Errorf("the %s network runs only on a loopback address: set CVN_LOCALNET_HOST to one, for example 127.77.1.1", localnetName)
	}
	genesis := int64(1794614400)
	if given, err := strconv.ParseInt(os.Getenv("CVN_LOCALNET_GENESIS"), 10, 64); err == nil && given > 0 {
		genesis = given
	}
	// With RandomX a hash takes a stand-in miner about a fiftieth of a
	// second, so the least difficulty is a handful of hashes.
	randomX := os.Getenv("CVN_LOCALNET_RANDOMX") != ""
	least := int64(HeartbeatMinDifficulty)
	if randomX {
		least = localnetRandomXLeastDifficulty
	}
	difficulty := least
	if given, err := strconv.ParseInt(os.Getenv("CVN_LOCALNET_DIFFICULTY"), 10, 64); err == nil && given >= least {
		difficulty = given
	}
	if given, err := strconv.Atoi(os.Getenv("CVN_LOCALNET_POOL_SHARE")); err == nil && given > 0 {
		poolStartDifficulty, poolMinShareDifficulty = given, 1
	}
	if given, err := strconv.ParseInt(os.Getenv("CVN_LOCALNET_POOL_FLOOR"), 10, 64); err == nil && given > 0 {
		poolInBlockFloorUnits = given
	}
	economics := NewGenesisEconomics
	// The limit on replacing one's own blocks is 100. A test of it would
	// have to mine more than that; here, and only here, it can be set lower.
	if given, err := strconv.ParseInt(os.Getenv("CVN_LOCALNET_REORG_LIMIT"), 10, 64); err == nil && given > 0 {
		economics.ReorgLimitBlocks = given
	}
	// A coin's date is seven years after it was made, and the vault pays
	// out over fourteen. A test of the sweep with real nodes cannot wait
	// that long; here, and only here, both can be set to a few blocks.
	if given, err := strconv.ParseInt(os.Getenv("CVN_LOCALNET_SWEEP_BLOCKS"), 10, 64); err == nil && given > 0 {
		economics.SweepIntervalBlocks = given
	}
	if given, err := strconv.ParseInt(os.Getenv("CVN_LOCALNET_VAULT_BLOCKS"), 10, 64); err == nil && given > 0 {
		economics.VaultPayoutBlocks = given
	}
	// Left empty, as before, unless RandomX was asked for; then as on devnet.
	var schedule []PowEra
	var keyEpoch, keyLag int64
	workPort := 0
	if randomX {
		schedule = []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: least}}
		keyEpoch, keyLag = 1920, 100
		workPort = localnetWorkPort
	}
	return ChainParams{
		PowSchedule:    schedule,
		KeyEpochBlocks: keyEpoch,
		KeyLagBlocks:   keyLag,
		StratumPort:    workPort,

		Name:        localnetName,
		Description: "Local test network: several nodes on one machine, for the multi-node tests",

		AuthKey: "CVN_LOCALNET_NODE_MESSAGE_KEY_NOT_FOR_REAL_VALUE",

		ListenHost:       host,
		GossipPort:       38080,
		SyncPort:         38081,
		SyncFallbackPort: 39091,
		StatusPort:       38082,

		DatabaseFile:  "cvn_localnet.db",
		PeerCacheFile: "peers_localnet.json",

		GenesisTimestamp: genesis,
		Anchor:           GenesisAnchor{Placeholder: "local test network, no anchor, no value"},

		MaxBlockBytes: 300000,

		HeartbeatInitialDifficulty: difficulty,
		ReliefNotInherited:         true,
		ExponentialSteps:           true,
		Economics:                  &economics,
	}, nil
}
