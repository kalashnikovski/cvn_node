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
	batch, _ := fetchBlockBatchChecked(source, start)
	return batch
}

// fetchBlockBatchChecked is fetchBlockBatch that also says whether the
// source answered at all, so that "no blocks from there on" can be told from
// "no answer".
func fetchBlockBatchChecked(source string, start int64) (batch []Block, answered bool) {
	client := &http.Client{Timeout: syncBatchTimeout}
	resp, err := client.Get(fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(source, "/"), start))
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	if json.NewDecoder(io.LimitReader(resp.Body, syncReplyLimitBytes)).Decode(&batch) != nil {
		return nil, false
	}
	return batch, true
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
	batch, delivered := fetchBlockBatchChecked(source, height)
	if !delivered {
		return "", false
	}
	for _, block := range batch {
		if block.Index == height {
			return block.Hash, true
		}
	}
	// It answered, and has no block at that height: a real answer, and one
	// that differs from any block of ours.
	return "none", true
}

// What a source says about its block at one height, compared with ours.
const (
	chainsAgree = iota
	chainsDiffer
	chainSilent // it did not answer; this says nothing about its chain
)

// findCommonAncestor returns the highest height in [lowest, highest] at
// which the source's block is our own. The chains agree up to the point
// where they part and differ after it, so the point is found by halving the
// range: a dozen small requests on a chain of thousands of blocks. It
// returns -1 if they already differ at `lowest`. answered is false if the
// source failed to answer along the way, in which case nothing has been
// learned and the height returned means nothing.
func findCommonAncestor(source string, lowest, highest int64) (ancestor int64, answered bool) {
	compare := func(height int64) int {
		local := GetBlockByHeightFromDB(height).Hash
		remote, ok := fetchBlockHash(source, height)
		switch {
		case !ok:
			// A second try before giving up: one lost request should not
			// end a pass.
			if remote, ok = fetchBlockHash(source, height); !ok {
				return chainSilent
			}
		}
		if local != "" && local == remote {
			return chainsAgree
		}
		return chainsDiffer
	}
	switch compare(lowest) {
	case chainSilent:
		return 0, false
	case chainsDiffer:
		return -1, true
	}
	switch compare(highest) {
	case chainSilent:
		return 0, false
	case chainsAgree:
		return highest, true
	}
	low, high := lowest, highest // agrees at low, differs at high
	for high-low > 1 {
		middle := low + (high-low)/2
		switch compare(middle) {
		case chainSilent:
			return 0, false
		case chainsAgree:
			low = middle
		default:
			high = middle
		}
	}
	return low, true
}

// probeChain asks a node for its height and total work. A variable so tests
// can stand in for the network.
var probeChain = peerChainStatus

// syncDelivered remembers the sources that have delivered a better chain
// when they said they had one.
var syncDelivered = struct {
	sync.Mutex
	at map[string]time.Time
}{at: make(map[string]time.Time)}

// syncSourceTiers lists the nodes a pass may catch up from, most trusted
// first:
//
//  1. the seeds, which are named in the software;
//  2. nodes that have delivered a better chain before, and nodes this one
//     has reached in earlier runs;
//  3. nodes that have merely connected to this one.
//
// Anyone can be in the third tier by connecting, and what a node says about
// its own chain costs it nothing to say. So a node from a later tier is only
// asked when no node in an earlier tier claims a better chain. Otherwise a
// few strangers, each claiming enormous work, could take turns being asked
// and keep this node from ever asking a seed.
func syncSourceTiers() [][]string {
	seeds := append([]string{}, operatorSyncSources...)
	for _, endpoint := range append(seedSyncURLs(), Params.ExtraSyncURLs...) {
		if skipSeedOnThisMachine(endpoint) && strings.HasSuffix(endpoint, ":"+Params.syncPortText()) {
			continue
		}
		seeds = append(seeds, endpoint)
	}
	var proven []string
	syncDelivered.Lock()
	for source := range syncDelivered.at {
		proven = append(proven, source)
	}
	syncDelivered.Unlock()
	proven = append(proven, cachedPeerHTTPGateways(seeds)...)

	var strangers []string
	PeerMapMutex.RLock()
	for address, lastSeen := range ActivePeerMap {
		if time.Since(lastSeen) <= 30*time.Second {
			if host, _, err := net.SplitHostPort(address); err == nil {
				strangers = append(strangers, Params.syncURL(host))
			}
		}
	}
	PeerMapMutex.RUnlock()
	return [][]string{seeds, proven, strangers}
}

// operatorSyncSources are nodes the operator named with --connect. They are
// asked alongside the seeds.
var operatorSyncSources []string

// trustedSourceClaimsBetter reports whether a seed, or a node the operator
// named, claims a chain better than ours.
func trustedSourceClaimsBetter() bool {
	tiers := syncSourceTiers()
	if len(tiers) == 0 {
		return false
	}
	ourHeight, ourWork := GetLatestBlockInternal().Index, TotalChainWork()
	for _, gateway := range tiers[0] {
		if syncSourceIsSetAside(gateway) {
			continue
		}
		if height, work := probeChain(gateway); peerChainIsBetter(height, work, ourHeight, ourWork) {
			return true
		}
	}
	return false
}

// runInitialDownload catches up with the network when the program starts,
// before mining begins. It is a series of ordinary passes, so everything a
// pass does to avoid being misled applies here too.
//
// It carries on for as long as a seed (or a node the operator named) claims
// a better chain and each pass brings this node closer. It does not wait on
// the claims of any other node: one of those could claim a height it will
// never deliver and keep this node from ever starting to mine. Whatever
// remains is caught up in the background once the node is running.
func runInitialDownload(args []string) {
	if host := argumentValue(args, "--connect"); host != "" {
		if !strings.HasPrefix(host, "http://") {
			host = "http://" + host
		}
		operatorSyncSources = []string{strings.TrimSuffix(host, "/")}
	}
	fmt.Println("🚀 Catching up with the network...")
	for pass := 0; pass < initialDownloadMaxPasses; pass++ {
		before := TotalChainWork()
		TriggerActiveReSyncPass()
		if TotalChainWork().Cmp(before) <= 0 {
			break // nothing gained: nobody had anything better, or it did not hold up
		}
		fmt.Printf("⏳ [SYNC] Now at block #%d.\n", GetLatestBlockInternal().Index)
		if !trustedSourceClaimsBetter() {
			break
		}
	}
	fmt.Printf("✅ [SYNC] Starting at block #%d.\n", GetLatestBlockInternal().Index)
}

// initialDownloadMaxPasses bounds the start-up download. A pass lasts at
// most syncPassLimit, so this is many hours of downloading.
const initialDownloadMaxPasses = 500

// syncPassFromBlockInterval is the least time between passes started by a
// block that arrived and could not be placed. Such a block costs its sender
// nothing to make, so it must not be able to start passes without limit.
const syncPassFromBlockInterval = 30 * time.Second

var lastPassFromBlock int64 // unix seconds, read and written atomically

// requestSyncPassForBlock starts a pass because a block arrived that does
// not fit on our chain, unless one was started that way a moment ago.
func requestSyncPassForBlock() {
	now := time.Now().Unix()
	last := atomic.LoadInt64(&lastPassFromBlock)
	if now-last < int64(syncPassFromBlockInterval/time.Second) || !atomic.CompareAndSwapInt64(&lastPassFromBlock, last, now) {
		return
	}
	go TriggerActiveReSyncPass()
}

// chooseSyncSource picks the node to catch up from: in the first tier where
// any node claims a chain better than ours, the one claiming the best.
// Sources set aside after failing to deliver are skipped.
func chooseSyncSource(ourHeight int64, ourWork *big.Int) (source string, claimedHeight int64) {
	asked := make(map[string]bool)
	for _, tier := range syncSourceTiers() {
		var bestWork *big.Int
		claimedHeight = ourHeight
		for _, gateway := range tier {
			if asked[gateway] || syncSourceIsSetAside(gateway) {
				continue
			}
			asked[gateway] = true
			peerHeight, peerWork := probeChain(gateway)
			if !peerChainIsBetter(peerHeight, peerWork, ourHeight, ourWork) {
				continue
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
		if source != "" {
			return source, claimedHeight
		}
	}
	return "", ourHeight
}

// chainChangePauses counts how often a pass has asked the node to stand
// still. Tests read it.
var chainChangePauses int64

// syncPassRunning is 1 while a pass is in progress, so that only one runs at
// a time.
var syncPassRunning int32

// pauseForChainChange tells the rest of the node that the chain is about to
// change under it: miners stop, and blocks arriving from peers are left for
// the pass to fetch. A pass sets it only once it is actually about to remove
// or add blocks. Comparing chains, which is all a pass does when the other
// node turns out to have nothing better, disturbs nothing: otherwise anyone
// could idle a node's miner, and deafen it to new blocks, just by claiming a
// better chain again and again.
func pauseForChainChange() {
	atomic.AddInt64(&chainChangePauses, 1)
	if atomic.CompareAndSwapInt32(&IsSyncingActive, 0, 1) {
		log.Println("⏳ [SYNC] Changing our chain. Mining is paused until it is done.")
	}
}

// TriggerActiveReSyncPass runs one pass, unless one is already running.
func TriggerActiveReSyncPass() {
	if !atomic.CompareAndSwapInt32(&syncPassRunning, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&syncPassRunning, 0)
	// Whatever this pass pauses, this pass resumes.
	defer atomic.StoreInt32(&IsSyncingActive, 0)
	deadline := time.Now().Add(syncPassLimit)

	startTip := GetLatestBlockInternal()
	startWork := TotalChainWork()
	source, claimedHeight := chooseSyncSource(startTip.Index, startWork)
	if source == "" {
		return
	}
	log.Printf("🔎 [SYNC] Comparing our chain with %s, which reports block #%d.\n", source, claimedHeight)

	// A source that claimed a better chain owes us one. If the pass ends
	// with our chain no better and the fault was not ours, it is set aside.
	blameless := false
	defer func() {
		switch {
		case TotalChainWork().Cmp(startWork) > 0:
			syncDelivered.Lock()
			syncDelivered.at[source] = time.Now()
			syncDelivered.Unlock()
		case !blameless:
			syncDelivered.Lock()
			delete(syncDelivered.at, source)
			syncDelivered.Unlock()
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
	ancestor, answered := findCommonAncestor(source, lowest, startTip.Index)
	if !answered {
		// A lost request proves nothing about the source's chain. The pass
		// ends without a verdict, and the source is not blamed for it.
		log.Printf("🔌 [SYNC] %s stopped answering while our chains were being compared. Trying again later.\n", source)
		blameless = true
		return
	}
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
		pauseForChainChange()
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
		if claimedHeight-next > 100 {
			log.Printf("⏳ [SYNC] Downloading: block #%d of #%d.\n", next, claimedHeight)
		}
		// Only now, with blocks in hand to add, is the rest of the node
		// asked to stand still.
		pauseForChainChange()
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
	log.Printf("🟩 [SYNC] Pass finished at block #%d.\n", GetLatestBlockInternal().Index)
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
