package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"

	"cvn_node/internal/randomx"
)

// PROOF OF WORK
//
// Every block has an identifier: the SHA-256 hash of its header. Blocks refer
// to each other by it, and it is cheap to compute.
//
// Whether a block carries enough work is decided by the mining algorithm in
// force at its height:
//
//   - sha256:  the identifier itself must be at or below the target.
//   - randomx: the RandomX hash of the identifier, under the key for that
//     height, must be at or below the target. RandomX is slow and
//     memory-hungry by design, which is what keeps ordinary processors
//     competitive (see internal/randomx).
//
// The algorithm is chosen by height from the network's schedule, so a future
// algorithm can take over at a chosen block without restarting the chain.
//
// The RandomX key changes on a fixed schedule: it is the identifier of an
// earlier block, far enough back that it can no longer be replaced.

const (
	PowSHA256  = "sha256"
	PowRandomX = "randomx"
)

// PowEra is one stretch of the chain mined with one algorithm.
type PowEra struct {
	FromHeight int64
	Algorithm  string
	// MinDifficulty is the lowest difficulty a block may have in this era.
	MinDifficulty int64
	// StartDifficulty is where difficulty restarts at FromHeight when the
	// algorithm changes mid-chain: hashes of different algorithms are not
	// comparable, so the previous block's difficulty means nothing here.
	StartDifficulty int64
}

// powEraAt returns the era a height belongs to, or nil when the network has no
// schedule (plain SHA-256 throughout).
func powEraAt(height int64) *PowEra {
	var current *PowEra
	for i := range Params.PowSchedule {
		if Params.PowSchedule[i].FromHeight <= height {
			current = &Params.PowSchedule[i]
		}
	}
	return current
}

// powAlgorithmAt names the mining algorithm in force at a height.
func powAlgorithmAt(height int64) string {
	if era := powEraAt(height); era != nil && era.Algorithm != "" {
		return era.Algorithm
	}
	return PowSHA256
}

// minDifficultyAt is the lowest difficulty allowed at a height.
func minDifficultyAt(height int64) int64 {
	if era := powEraAt(height); era != nil && era.MinDifficulty > 0 {
		return era.MinDifficulty
	}
	return HeartbeatMinDifficulty
}

// powEraStartingAt returns the era that begins exactly at `height` as a
// change from a different algorithm, or nil.
func powEraStartingAt(height int64) *PowEra {
	era := powEraAt(height)
	if era == nil || era.FromHeight != height || height <= 1 {
		return nil
	}
	if powAlgorithmAt(height-1) == era.Algorithm {
		return nil
	}
	return era
}

// powKeyHeight is the height of the block whose identifier is the RandomX key
// for blocks at `height`. The key changes every KeyEpochBlocks, and the key
// block is always more than KeyLagBlocks behind, beyond the reach of a
// reorganisation. The earliest blocks use the genesis block.
func powKeyHeight(height int64) int64 {
	epoch, lag := Params.KeyEpochBlocks, Params.KeyLagBlocks
	if epoch < 1 {
		epoch = 1920
	}
	if height <= lag+1 {
		return 0
	}
	return (height - lag - 1) / epoch * epoch
}

// powKeyFor returns the RandomX key for blocks at `height`: the identifier of
// the key block on this node's chain.
func powKeyFor(height int64) ([]byte, bool) {
	keyBlock := GetBlockByHeightFromDB(powKeyHeight(height))
	key, err := hex.DecodeString(keyBlock.Hash)
	if keyBlock.Hash == "" || err != nil || len(key) == 0 {
		return nil, false
	}
	return key, true
}

// BlockProofValid reports whether a block carries the work its difficulty
// demands, under the algorithm in force at its height. block.Hash must
// already be known to be the block's true identifier.
func BlockProofValid(block Block, difficulty int) bool {
	if powAlgorithmAt(block.Index) != PowRandomX {
		return HashMeetsDifficulty(block.Hash, block.Index, difficulty)
	}
	if int64(difficulty) < minDifficultyAt(block.Index) {
		return false
	}
	key, ok := powKeyFor(block.Index)
	if !ok {
		return false
	}
	work, ok := randomXVerify(key, block.Hash)
	return ok && work <= HeartbeatTargetHex(difficulty)
}

// ---------------------------------------------------------------------------
// RandomX engines
// ---------------------------------------------------------------------------

// rxLight is one low-memory engine (about 256 MB), prepared for one key.
type rxLight struct {
	key   string
	cache *randomx.Cache
	vm    *randomx.VM
}

func newRxLight(key []byte) (*rxLight, error) {
	flags := randomx.GetFlags()
	cache, err := randomx.NewCache(flags)
	if err != nil {
		return nil, err
	}
	cache.Init(key)
	vm, err := randomx.NewVM(flags, cache, nil)
	if err != nil {
		cache.Close()
		return nil, err
	}
	return &rxLight{key: string(key), cache: cache, vm: vm}, nil
}

func (e *rxLight) close() {
	if e.vm != nil {
		e.vm.Close()
	}
	if e.cache != nil {
		e.cache.Close()
	}
}

func (e *rxLight) hash(input []byte) string {
	var output [randomx.RANDOMX_HASH_SIZE]byte
	e.vm.CalculateHash(input, &output)
	return hex.EncodeToString(output[:])
}

var rxVerifier struct {
	sync.Mutex
	engines []*rxLight        // most recently used first; at most two keys
	results map[string]string // key + identifier -> work hash, for repeat checks
}

// randomXVerify returns the RandomX hash of a block identifier under a key,
// as 64 hex digits. It keeps engines for the two most recent keys, so the
// daily key change does not interrupt verification.
func randomXVerify(key []byte, blockHashHex string) (string, bool) {
	input, err := hex.DecodeString(blockHashHex)
	if err != nil || len(input) != 32 {
		return "", false
	}
	rxVerifier.Lock()
	defer rxVerifier.Unlock()
	cacheKey := string(key) + blockHashHex
	if work, known := rxVerifier.results[cacheKey]; known {
		return work, true
	}
	var engine *rxLight
	for i, candidate := range rxVerifier.engines {
		if candidate.key == string(key) {
			engine = candidate
			rxVerifier.engines[0], rxVerifier.engines[i] = rxVerifier.engines[i], rxVerifier.engines[0]
			break
		}
	}
	if engine == nil {
		created, err := newRxLight(key)
		if err != nil {
			return "", false
		}
		engine = created
		rxVerifier.engines = append([]*rxLight{engine}, rxVerifier.engines...)
		for len(rxVerifier.engines) > 2 {
			last := len(rxVerifier.engines) - 1
			rxVerifier.engines[last].close()
			rxVerifier.engines = rxVerifier.engines[:last]
		}
	}
	work := engine.hash(input)
	if rxVerifier.results == nil || len(rxVerifier.results) > 4096 {
		rxVerifier.results = make(map[string]string)
	}
	rxVerifier.results[cacheKey] = work
	return work, true
}

// rxMiner holds what the mining threads share: for the current key, either
// the full dataset (about 2.3 GB, roughly ten times faster) or, when that
// cannot be had, a low-memory cache.
var rxMiner struct {
	sync.RWMutex
	key     string
	cache   *randomx.Cache
	dataset *randomx.Dataset
	flags   randomx.Flags
	vms     map[int64]*randomx.VM
	vmLock  sync.Mutex
	mode    string
}

// randomXMiningMode reports how the miner is hashing: "full", "light" or "".
func randomXMiningMode() string {
	rxMiner.RLock()
	defer rxMiner.RUnlock()
	return rxMiner.mode
}

func prepareRxMiner(key []byte) bool {
	rxMiner.Lock()
	defer rxMiner.Unlock()
	if rxMiner.key == string(key) && rxMiner.cache != nil {
		return true
	}
	for _, vm := range rxMiner.vms {
		vm.Close()
	}
	rxMiner.vms = make(map[int64]*randomx.VM)
	if rxMiner.dataset != nil {
		rxMiner.dataset.Close()
		rxMiner.dataset = nil
	}
	if rxMiner.cache != nil {
		rxMiner.cache.Close()
		rxMiner.cache = nil
	}
	rxMiner.key, rxMiner.mode = "", ""

	flags := randomx.GetFlags()
	cache, err := randomx.NewCache(flags)
	if err != nil {
		return false
	}
	cache.Init(key)
	rxMiner.cache, rxMiner.flags, rxMiner.mode = cache, flags, "light"
	if os.Getenv("CVN_RANDOMX_LIGHT") == "" {
		fullFlags := flags | randomx.RANDOMX_FLAG_FULL_MEM
		if dataset, err := randomx.NewDataset(fullFlags); err == nil {
			dataset.InitDatasetParallel(cache, runtime.GOMAXPROCS(0))
			rxMiner.dataset, rxMiner.flags, rxMiner.mode = dataset, fullFlags, "full"
		}
	}
	rxMiner.key = string(key)
	announceNetworkStatus("⚙️ [RANDOMX] Mining engine ready (" + rxMiner.mode + " memory mode).")
	return true
}

// randomXMine returns the RandomX hash of a block identifier for a mining
// thread, preparing the shared engine for the key first if needed.
func randomXMine(workerID int64, key []byte, blockHashHex string) (string, bool) {
	input, err := hex.DecodeString(blockHashHex)
	if err != nil || len(input) != 32 {
		return "", false
	}
	rxMiner.RLock()
	ready := rxMiner.key == string(key) && rxMiner.cache != nil
	rxMiner.RUnlock()
	if !ready && !prepareRxMiner(key) {
		return "", false
	}
	rxMiner.RLock()
	defer rxMiner.RUnlock()
	if rxMiner.key != string(key) {
		return "", false // the key changed under us; the caller simply tries again
	}
	rxMiner.vmLock.Lock()
	vm := rxMiner.vms[workerID]
	if vm == nil {
		var created *randomx.VM
		if rxMiner.dataset != nil {
			created, err = randomx.NewVM(rxMiner.flags, nil, rxMiner.dataset)
		} else {
			created, err = randomx.NewVM(rxMiner.flags, rxMiner.cache, nil)
		}
		if err != nil {
			rxMiner.vmLock.Unlock()
			return "", false
		}
		vm = created
		rxMiner.vms[workerID] = vm
	}
	rxMiner.vmLock.Unlock()
	var output [randomx.RANDOMX_HASH_SIZE]byte
	vm.CalculateHash(input, &output)
	return hex.EncodeToString(output[:]), true
}

// formatHashrate writes a mining speed for the dashboard. megaHashes is in
// millions of hashes per second, the unit the dashboard has always tracked.
// RandomX is thousands of times slower per hash than SHA-256 by design, so
// under it the speed is shown in hashes per second.
func formatHashrate(megaHashes float64) string {
	if powAlgorithmAt(atomic.LoadInt64(&localHeightState)+1) != PowRandomX {
		return fmt.Sprintf("%.2f MH/s", megaHashes)
	}
	hashes := megaHashes * 1000000
	if hashes >= 10000 {
		return fmt.Sprintf("%.2f kH/s", hashes/1000)
	}
	return fmt.Sprintf("%.0f H/s", hashes)
}
