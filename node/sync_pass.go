package node

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CATCHING UP WITH THE NETWORK
//
// A re-sync pass compares this node's chain with another node's and, if the
// other chain proves more work, follows it. It is started when a block
// arrives that does not fit on our tip, and by the watchdog that notices a
// node has fallen behind.
//
// What another node tells us about itself is a claim, not a fact. A pass
// therefore:
//
//   - has a time limit, which it keeps to by itself;
//   - sets aside, for a while, a source that claimed a better chain and then
//     did not deliver one, so that one lying node cannot become the only
//     node this one ever asks;
//   - finds where the two chains part with a handful of small requests, not
//     by walking back one block at a time;
//   - never downloads more at once than it can check.

const (
	// syncPassLimit is how long one pass may take.
	syncPassLimit = 5 * time.Minute
	// syncSourceCooldown is how long a source is set aside after a pass in
	// which it did not deliver the better chain it claimed.
	syncSourceCooldown = 10 * time.Minute
	// syncProbeTimeout bounds a small request; syncBatchTimeout a download
	// of blocks.
	syncProbeTimeout = 4 * time.Second
	syncBatchTimeout = 30 * time.Second

	// A node answering a request for blocks sends at most this many, and
	// stops early once they add up to syncBatchMaxBytes. A full block is
	// hundreds of kilobytes, so a fixed count alone could mean a reply far
	// too large to send or check in time.
	syncBatchMaxBlocks = 512
	// syncReplyLimitBytes is the most a node will read of one reply.
	syncReplyLimitBytes = 64 << 20
)

// syncBatchMaxBytes is the size at which a reply of blocks stops growing. A
// variable so tests can shrink it.
var syncBatchMaxBytes = 4 << 20

var syncSetAside = struct {
	sync.Mutex
	until map[string]time.Time
}{until: make(map[string]time.Time)}

// setSyncSourceAside stops a source being chosen for syncSourceCooldown.
func setSyncSourceAside(source, reason string) {
	syncSetAside.Lock()
	syncSetAside.until[source] = time.Now().Add(syncSourceCooldown)
	syncSetAside.Unlock()
	log.Printf("⏸️ [SYNC] %s %s. Not asking it again for %d minutes.\n", source, reason, int(syncSourceCooldown/time.Minute))
}

// syncSourceIsSetAside reports whether a source is currently set aside.
func syncSourceIsSetAside(source string) bool {
	syncSetAside.Lock()
	defer syncSetAside.Unlock()
	until, found := syncSetAside.until[source]
	if !found {
		return false
	}
	if time.Now().After(until) {
		delete(syncSetAside.until, source)
		return false
	}
	return true
}

// blocksForBatch gathers the blocks a node sends in answer to a request for
// those from `start`: up to syncBatchMaxBlocks, stopping early at
// syncBatchMaxBytes. It always includes at least one if there is one.
func blocksForBatch(start, tipHeight int64) []Block {
	var blocks []Block
	size := 0
	for h := start; h <= tipHeight && len(blocks) < syncBatchMaxBlocks; h++ {
		block := GetBlockByHeightFromDB(h)
		if block.Hash == "" {
			break
		}
		blocks = append(blocks, block)
		if size += blockSizeBytes(block); size >= syncBatchMaxBytes {
			break
		}
	}
	return blocks
}

// fetchBlockBatch downloads blocks from `start` onward from a source. It
// returns nothing if the source does not answer in time or sends too much.
func fetchBlockBatch(source string, start int64) []Block {
	client := &http.Client{Timeout: syncBatchTimeout}
	resp, err := client.Get(fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(source, "/"), start))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var batch []Block
	if json.NewDecoder(io.LimitReader(resp.Body, syncReplyLimitBytes)).Decode(&batch) != nil {
		return nil
	}
	return batch
}

// fetchBlockHash asks a source for the identifier of its block at a height.
// Nodes that predate the small request for this are asked for the block
// itself.
func fetchBlockHash(source string, height int64) (string, bool) {
	client := &http.Client{Timeout: syncProbeTimeout}
	resp, err := client.Get(fmt.Sprintf("%s/blockhash?height=%d", strings.TrimSuffix(source, "/"), height))
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var answer struct {
				Hash string `json:"hash"`
			}
			if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&answer) == nil && answer.Hash != "" {
				return answer.Hash, true
			}
			return "", false
		}
		if resp.StatusCode != http.StatusNotFound {
			return "", false
		}
		// 404: either it has no block there, or it is an older node without
		// this request. Fall through and ask the older way.
	}
	for _, block := range fetchBlockBatch(source, height) {
		if block.Index == height {
			return block.Hash, true
		}
	}
	return "", false
}

// findCommonAncestor returns the highest height in [lowest, highest] at
// which the source's block is our own. The chains agree up to the point
// where they part and differ after it, so the point is found by halving the
// range: a dozen small requests on a chain of thousands of blocks. It
// returns -1 if they already differ at `lowest`.
func findCommonAncestor(source string, lowest, highest int64) int64 {
	agrees := func(height int64) bool {
		local := GetBlockByHeightFromDB(height).Hash
		remote, ok := fetchBlockHash(source, height)
		return ok && local != "" && local == remote
	}
	if !agrees(lowest) {
		return -1
	}
	if agrees(highest) {
		return highest
	}
	low, high := lowest, highest // agrees at low, differs at high
	for high-low > 1 {
		middle := low + (high-low)/2
		if agrees(middle) {
			low = middle
		} else {
			high = middle
		}
	}
	return low
}

// chooseSyncSource picks the node to catch up from: the one claiming the
// best chain, if it is better than ours. Nodes we are in touch with are
// asked first, then the seeds and remembered peers. Sources set aside after
// failing to deliver are skipped.
func chooseSyncSource(ourHeight int64, ourWork *big.Int) (source string, claimedHeight int64) {
	var bestWork *big.Int
	claimedHeight = ourHeight
	consider := func(gateway string) {
		if syncSourceIsSetAside(gateway) {
			return
		}
		peerHeight, peerWork := peerChainStatus(gateway)
		if !peerChainIsBetter(peerHeight, peerWork, ourHeight, ourWork) {
			return
		}
		better := source == ""
		if peerWork != nil {
			better = better || bestWork == nil || peerWork.Cmp(bestWork) > 0
		} else {
			better = better || (bestWork == nil && peerHeight > claimedHeight)
		}
		if better {
			source, claimedHeight, bestWork = gateway, peerHeight, peerWork
		}
	}
	PeerMapMutex.RLock()
	var freshPeers []string
	for address, lastSeen := range ActivePeerMap {
		if time.Since(lastSeen) <= 30*time.Second {
			freshPeers = append(freshPeers, address)
		}
	}
	PeerMapMutex.RUnlock()
	for _, address := range freshPeers {
		host, _, _ := net.SplitHostPort(address)
		consider(Params.syncURL(host))
	}
	if source == "" {
		gateways := append(seedSyncURLs(), Params.ExtraSyncURLs...)
		gateways = append(gateways, cachedPeerHTTPGateways(gateways)...)
		for _, endpoint := range gateways {
			if skipSeedOnThisMachine(endpoint) && strings.HasSuffix(endpoint, ":"+Params.syncPortText()) {
				continue
			}
			consider(endpoint)
		}
	}
	return source, claimedHeight
}

// TriggerActiveReSyncPass runs one pass, unless one is already running.
func TriggerActiveReSyncPass() {
	if !atomic.CompareAndSwapInt32(&IsSyncingActive, 0, 1) {
		return
	}
	// This pass alone set the flag and this pass alone clears it.
	defer atomic.StoreInt32(&IsSyncingActive, 0)
	deadline := time.Now().Add(syncPassLimit)

	startTip := GetLatestBlockInternal()
	startWork := TotalChainWork()
	source, claimedHeight := chooseSyncSource(startTip.Index, startWork)
	if source == "" {
		return
	}
	log.Printf("⏳ [SYNC] Comparing our chain with %s, which reports block #%d. Mining is paused meanwhile.\n", source, claimedHeight)

	// A source that claimed a better chain owes us one. If the pass ends
	// with our chain no better and the fault was not ours, it is set aside.
	blameless := false
	defer func() {
		if !blameless && TotalChainWork().Cmp(startWork) <= 0 {
			setSyncSourceAside(source, "claimed a better chain and did not deliver one")
		}
	}()

	// How far back to look for the point where the chains part. A node never
	// replaces more of its own blocks than the reorganisation limit, so
	// there is no reason to look further back than that.
	lowest := int64(0)
	if Params.Economics != nil && Params.Economics.ReorgLimitBlocks > 0 {
		if lowest = startTip.Index - Params.Economics.ReorgLimitBlocks; lowest < 0 {
			lowest = 0
		}
	}
	ancestor := findCommonAncestor(source, lowest, startTip.Index)
	if ancestor < 0 {
		if lowest > 0 {
			log.Printf("🛑 [REORG LIMIT] %s offers a chain that parts from ours more than %d blocks back. This node does not replace more than that many of its own blocks, so it keeps its chain. If this node was left behind on a dead branch, stop it, delete %s and start again.\n", source, Params.Economics.ReorgLimitBlocks, Params.DatabaseFile)
		} else {
			log.Printf("🚨 [SYNC] %s shares no block with our chain, not even the first. It is on another network or another chain.\n", source)
		}
		return
	}

	StateMutex.Lock()
	tip := GetLatestBlockInternal()
	var removed []Block
	if ancestor < tip.Index {
		if depth := tip.Index - ancestor; !reorgDepthAllowed(depth) {
			StateMutex.Unlock()
			log.Printf("🛑 [REORG LIMIT] %s offers a chain that parts from ours %d blocks back. This node does not replace more than %d of its own blocks, so it keeps its chain.\n", source, depth, Params.Economics.ReorgLimitBlocks)
			return
		}
		// Switching would discard our blocks above the shared one. Do it
		// only if the other branch proves more work than those blocks hold.
		ourBranchWork := new(big.Int).Sub(chainWorkThrough(tip.Index), chainWorkThrough(ancestor))
		ancestorHash := GetBlockByHeightFromDB(ancestor).Hash
		StateMutex.Unlock()
		theirWork, theirsWins := measureCandidateBranch(func(start int64) []Block { return fetchBlockBatch(source, start) }, ancestor, ancestorHash, claimedHeight, ourBranchWork)
		if !theirsWins {
			log.Printf("⚖️ [CHAIN WORK] Keeping our chain: %s offers work %s above block #%d, ours holds %s.\n", source, theirWork.String(), ancestor, ourBranchWork.String())
			return
		}
		StateMutex.Lock()
		if GetLatestBlockInternal().Hash != tip.Hash {
			// Our tip moved while the other branch was being measured. Start
			// over on the next pass; this is no fault of the source.
			StateMutex.Unlock()
			blameless = true
			return
		}
		log.Printf("⚖️ [CHAIN WORK] Switching to the branch from %s above block #%d: it proves more work than our %d block(s) there.\n", source, ancestor, tip.Index-ancestor)
		removed = rollbackChainToHeightLocked(ancestor)
	}
	StateMutex.Unlock()

	for time.Now().Before(deadline) {
		next := GetLatestBlockInternal().Index + 1
		if next > claimedHeight {
			break
		}
		batch := fetchBlockBatch(source, next)
		if len(batch) == 0 {
			break
		}
		StateMutex.Lock()
		refused := false
		for _, block := range batch {
			if block.Index != GetLatestBlockInternal().Index+1 {
				continue
			}
			if !ValidateBlockSignatures(block) {
				log.Printf("⚠️ [SYNC] Block #%d from %s is not valid. Ending this pass.\n", block.Index, source)
				refused = true
				break
			}
			if block.UncleHash == "" {
				block.UncleHash = emptyUncleHash
			}
			if block.UncleMiner == "" {
				block.UncleMiner = emptyUncleMiner
			}
			SaveBlockToStorageInternal(block)
		}
		StateMutex.Unlock()
		// Stop if the batch moved us nowhere, rather than asking for it again.
		if refused || GetLatestBlockInternal().Index < next {
			break
		}
	}

	if len(removed) > 0 && TotalChainWork().Cmp(startWork) < 0 {
		// The other branch did not hold up under full checking (or the source
		// went away), and what we have now is worse than what we gave up.
		log.Printf("↩️ [CHAIN WORK] The new branch ended with less work than our previous chain. Restoring our %d block(s) above #%d.\n", len(removed), ancestor)
		StateMutex.Lock()
		restoreChainLocked(ancestor, removed)
		StateMutex.Unlock()
	}
	log.Printf("🟩 [SYNC] Pass finished at block #%d. Mining resumes.\n", GetLatestBlockInternal().Index)
}

// serveHTTP answers requests on an address with limits on how long a caller
// may take. Without them one caller that sends a request a byte at a time,
// or never reads the answer, holds a connection open for ever.
func serveHTTP(address string, handler http.Handler) error {
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	return server.ListenAndServe()
}

// balanceRecountInterval is the least time between full recounts of every
// balance made on behalf of web requests.
const balanceRecountInterval = 10 * time.Second

var lastBalanceRecount int64 // unix seconds, read and written atomically

// recountBalancesIfStale recounts every balance from the coins unless that
// was done within balanceRecountInterval. It reports whether it recounted.
func recountBalancesIfStale() bool {
	now := time.Now().Unix()
	last := atomic.LoadInt64(&lastBalanceRecount)
	if now-last < int64(balanceRecountInterval/time.Second) || !atomic.CompareAndSwapInt64(&lastBalanceRecount, last, now) {
		return false
	}
	RebuildStateBalanceCacheInternal()
	return true
}
