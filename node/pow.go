package node

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

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
//   - randomx: the RandomX hash of the block's mining blob, under the key
//     for that height, must be at or below the target. RandomX is slow and
//     memory-hungry by design, which is what keeps ordinary processors
//     competitive (see internal/randomx).
//
// The mining blob is 43 bytes: a version byte, six zero bytes, the 32-byte
// commitment to everything in the block except the nonce, and the nonce as
// four bytes, lowest first. The commitment is the block's identifier computed
// with the nonce set to zero. The RandomX result is read as a number with its
// last byte the most significant.
//
// That layout, the 4-byte nonce at byte 39 and the way the result is read are
// what widely used RandomX mining software (XMRig) expects, so such software
// can mine CVN through the work server in stratum.go without CVN having to
// trust it: every block it finds is checked here like any other.
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

const (
	powBlobSize        = 43
	powBlobNonceOffset = 39
	powNonceLimit      = int64(1) << 32
)

// powCommitment commits to everything in a block except its nonce: it is the
// block's identifier computed with the nonce set to zero, as 64 hex digits.
func powCommitment(block Block) string {
	block.Nonce = 0
	return CalculateHash(block)
}

// powBlob builds the 43 bytes that are hashed with RandomX for a block with
// this commitment and nonce.
func powBlob(commitmentHex string, nonce uint32) ([]byte, bool) {
	commitment, err := hex.DecodeString(commitmentHex)
	if err != nil || len(commitment) != 32 {
		return nil, false
	}
	blob := make([]byte, powBlobSize)
	blob[0] = 1
	copy(blob[7:powBlobNonceOffset], commitment)
	blob[powBlobNonceOffset] = byte(nonce)
	blob[powBlobNonceOffset+1] = byte(nonce >> 8)
	blob[powBlobNonceOffset+2] = byte(nonce >> 16)
	blob[powBlobNonceOffset+3] = byte(nonce >> 24)
	return blob, true
}

// powWorkValue turns a RandomX result into the number it stands for, written
// as 64 hex digits with the most significant first, so that it can be
// compared with a target directly.
func powWorkValue(hash [randomx.RANDOMX_HASH_SIZE]byte) string {
	var reversed [randomx.RANDOMX_HASH_SIZE]byte
	for i := range hash {
		reversed[len(hash)-1-i] = hash[i]
	}
	return hex.EncodeToString(reversed[:])
}

// BlockProofValid reports whether a block carries the work its difficulty
// demands, under the algorithm in force at its height. block.Hash must
// already be known to be the block's true identifier.
func BlockProofValid(block Block, difficulty int) bool {
	if powAlgorithmAt(block.Index) != PowRandomX {
		return HashMeetsDifficulty(block.Hash, block.Index, difficulty)
	}
	if int64(difficulty) < minDifficultyAt(block.Index) || block.Nonce < 0 || block.Nonce >= powNonceLimit {
		return false
	}
	key, ok := powKeyFor(block.Index)
	if !ok {
		return false
	}
	blob, ok := powBlob(powCommitment(block), uint32(block.Nonce))
	if !ok {
		return false
	}
	work, ok := randomXVerify(key, blob)
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
	return powWorkValue(output)
}

var rxVerifier struct {
	sync.Mutex
	engines []*rxLight        // most recently used first; at most two keys
	results map[string]string // key + blob -> work value, for repeat checks
}

// randomXVerify returns the work value of a mining blob under a key (see
// powWorkValue). It keeps engines for the two most recent keys, so the daily
// key change does not interrupt verification.
func randomXVerify(key []byte, input []byte) (string, bool) {
	if len(input) == 0 || len(key) == 0 {
		return "", false
	}
	rxVerifier.Lock()
	defer rxVerifier.Unlock()
	cacheKey := string(key) + string(input)
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

// warmNextVerifyKey prepares the checking engine for the RandomX key that
// comes into use within the next KeyLagBlocks blocks, if it is not ready
// yet. The key block is that far behind the tip, so the next key is always
// known well before the first block that uses it. Preparing it ahead means
// the first block after the daily change is checked as fast as any other.
// It reports whether an engine was prepared.
//
// The mining table is not prepared ahead: a second one would need another
// 2 GB of memory, and on most machines would not get large pages. It is
// rebuilt at the change, a pause of about a second on a desktop processor.
func warmNextVerifyKey() bool {
	if !networkUsesRandomX() || Params.KeyLagBlocks <= 0 {
		return false
	}
	ahead := GetLatestBlockInternal().Index + Params.KeyLagBlocks
	if powAlgorithmAt(ahead) != PowRandomX {
		return false
	}
	key, ok := powKeyFor(ahead)
	if !ok || len(key) == 0 {
		return false
	}
	rxVerifier.Lock()
	for _, engine := range rxVerifier.engines {
		if engine.key == string(key) {
			rxVerifier.Unlock()
			return false
		}
	}
	rxVerifier.Unlock()

	// Built without holding the lock, so blocks go on being checked meanwhile.
	created, err := newRxLight(key)
	if err != nil {
		return false
	}
	rxVerifier.Lock()
	defer rxVerifier.Unlock()
	for _, engine := range rxVerifier.engines {
		if engine.key == string(key) {
			created.close()
			return false
		}
	}
	// It goes behind the engine in use, so the one in use is not the one
	// dropped to make room.
	if len(rxVerifier.engines) == 0 {
		rxVerifier.engines = []*rxLight{created}
	} else {
		rest := append([]*rxLight{created}, rxVerifier.engines[1:]...)
		rxVerifier.engines = append(rxVerifier.engines[:1:1], rest...)
	}
	for len(rxVerifier.engines) > 2 {
		last := len(rxVerifier.engines) - 1
		rxVerifier.engines[last].close()
		rxVerifier.engines = rxVerifier.engines[:last]
	}
	return true
}

// runVerifyKeyWarmer keeps the next key's checking engine ready.
func runVerifyKeyWarmer() {
	for {
		time.Sleep(30 * time.Second)
		if atomic.LoadInt32(&IsSyncingActive) == 0 && warmNextVerifyKey() {
			noteNetworkEvent("⚙️ [RANDOMX] The checking engine for the next daily key is ready.")
		}
	}
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
	rxMiner.cache, rxMiner.flags, rxMiner.mode = cache, flags, "low memory"
	if os.Getenv("CVN_RANDOMX_LIGHT") == "" {
		if dataset, fullFlags, largePages, err := newMiningDataset(flags); err == nil {
			dataset.InitDatasetParallel(cache, runtime.GOMAXPROCS(0))
			rxMiner.dataset, rxMiner.flags, rxMiner.mode = dataset, fullFlags, "full memory"
			if largePages {
				rxMiner.mode = "full memory, large pages"
			}
		} else if err == errTooLittleMemory {
			available, _ := availableMemoryBytes()
			announceNetworkStatus(fmt.Sprintf("⚙️ [RANDOMX] This machine has about %d MB of memory free. Full-speed mining needs about %d MB, so the miner is using low memory mode, which is many times slower. The node itself is not affected.", available>>20, rxFullMemoryNeededBytes>>20))
		}
	}
	rxMiner.key = string(key)
	announceNetworkStatus("⚙️ [RANDOMX] Mining engine ready (" + rxMiner.mode + " mode).")
	return true
}

// newMiningDataset allocates the full RandomX dataset for mining. It asks for
// large memory pages first, which make RandomX markedly faster, and falls
// back to ordinary memory when the system does not grant them (on Windows
// the account needs the "Lock pages in memory" right; on Linux huge pages
// must be reserved). CVN_NO_LARGE_PAGES=1 skips the attempt.
//
// The returned flags are the ones to create mining threads' engines with:
// when large pages were granted for the dataset, each thread's working
// memory is requested in large pages too.
func newMiningDataset(flags randomx.Flags) (*randomx.Dataset, randomx.Flags, bool, error) {
	fullFlags := flags | randomx.RANDOMX_FLAG_FULL_MEM
	if os.Getenv("CVN_NO_LARGE_PAGES") == "" {
		largeFlags := fullFlags | randomx.RANDOMX_FLAG_LARGE_PAGES
		if dataset, err := randomx.NewDataset(largeFlags); err == nil {
			return dataset, largeFlags, true, nil
		}
	}
	// Ordinary memory is handed out whether or not the machine really has
	// it; a machine without enough would push the table onto its disk and
	// mine at almost nothing. Better to say so and use the small mode.
	if available, known := availableMemoryBytes(); !enoughMemoryForFullMining(available, known) {
		return nil, flags, false, errTooLittleMemory
	}
	dataset, err := randomx.NewDataset(fullFlags)
	return dataset, fullFlags, false, err
}

// rxFullMemoryNeededBytes is the free memory full-speed mining needs: the
// 2,080 MB mining table plus working room.
const rxFullMemoryNeededBytes = uint64(2300) << 20

var errTooLittleMemory = errors.New("not enough free memory for the full mining table")

// enoughMemoryForFullMining decides whether to build the full mining table
// in ordinary memory. When the amount free is not known the miner tries, as
// it always did. CVN_RANDOMX_FULL=1 forces the attempt.
func enoughMemoryForFullMining(availableBytes uint64, known bool) bool {
	if !known || os.Getenv("CVN_RANDOMX_FULL") != "" {
		return true
	}
	return availableBytes >= rxFullMemoryNeededBytes
}

// pinMiningThread ties the calling goroutine to its own operating-system
// thread and that thread to one processor thread, so the 2 MiB of working
// memory each mining thread uses stays in one core's cache instead of
// following the thread around. CVN_NO_PINNING=1 turns it off.
func pinMiningThread(worker int) bool {
	if os.Getenv("CVN_NO_PINNING") != "" {
		return false
	}
	runtime.LockOSThread()
	return setCurrentThreadCPU(worker % runtime.NumCPU())
}

// randomXMine returns the work value of a mining blob for a mining thread,
// preparing the shared engine for the key first if needed.
func randomXMine(workerID int64, key []byte, input []byte) (string, bool) {
	if len(input) == 0 {
		return "", false
	}
	var err error
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
	return powWorkValue(output), true
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
