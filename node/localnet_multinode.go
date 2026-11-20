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
//   - blocks are mined with the fast hash, as in the other tests, not RandomX;
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

const localnetName = "localnet"

// localnetDebugPort is where a node of the local test network says what each
// part of it is doing (the Go runtime's own report), on its loopback address.
const localnetDebugPort = 38089

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
	difficulty := int64(HeartbeatMinDifficulty)
	if given, err := strconv.ParseInt(os.Getenv("CVN_LOCALNET_DIFFICULTY"), 10, 64); err == nil && given >= HeartbeatMinDifficulty {
		difficulty = given
	}
	economics := NewGenesisEconomics
	// The limit on replacing one's own blocks is 100. A test of it would
	// have to mine more than that; here, and only here, it can be set lower.
	if given, err := strconv.ParseInt(os.Getenv("CVN_LOCALNET_REORG_LIMIT"), 10, 64); err == nil && given > 0 {
		economics.ReorgLimitBlocks = given
	}
	return ChainParams{
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
