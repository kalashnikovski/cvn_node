package node

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
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
	operatorSyncSources = operatorSourcesFrom(argumentValue(args, "--connect"))
	fmt.Println("🚀 Catching up with the network...")
	beginInitialDownload(GetLatestBlockInternal().Index)
	for pass := 0; pass < initialDownloadMaxPasses; pass++ {
		before := TotalChainWork()
		TriggerActiveReSyncPass()
		if TotalChainWork().Cmp(before) <= 0 {
			break // nothing gained: nobody had anything better, or it did not hold up
		}
		printDownloadProgress()
		if !trustedSourceClaimsBetter() {
			break
		}
	}
	// The seeds have nothing more to give. Before this node settles on what
	// they gave it, it asks the other nodes it knows of, a few times at most:
	// a seed is a convenience, not an authority, and if another node proves
	// more work, that is the chain to follow.
	for pass := 0; pass < initialDownloadOtherPasses; pass++ {
		before := TotalChainWork()
		TriggerActiveReSyncPass()
		if TotalChainWork().Cmp(before) <= 0 {
			break
		}
		printDownloadProgress()
	}
	trySettleInitialDownload()
	fmt.Printf("✅ [SYNC] Starting at block #%d.\n", GetLatestBlockInternal().Index)
}

// printDownloadProgress says how far the start-up download has got, with a
// bar when the height the other node reported is known, so that a person
// watching a long download can see it moving.
func printDownloadProgress() {
	height, target := GetLatestBlockInternal().Index, atomic.LoadInt64(&networkHeightHeard)
	if target > height {
		fmt.Printf("⏳ [SYNC] Block #%d of #%d %s %d%%\n", height, target, progressBar(height, target, 30), height*100/target)
		return
	}
	fmt.Printf("⏳ [SYNC] Now at block #%d.\n", height)
}

// operatorSourcesFrom turns what was given with --connect into the nodes to
// ask: one host or several, separated by commas. A host given without a port
// is asked on this network's download port.
func operatorSourcesFrom(list string) []string {
	var sources []string
	for _, host := range strings.Split(list, ",") {
		host = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(host), "http://"), "/")
		if host == "" || strings.ContainsAny(host, " /@") {
			continue
		}
		if _, _, err := net.SplitHostPort(host); err != nil {
			sources = append(sources, Params.syncURL(host))
		} else {
			sources = append(sources, "http://"+host)
		}
	}
	return sources
}

// initialDownloadOtherPasses is how many passes the start-up download gives
// to nodes other than the seeds once the seeds have nothing more.
const initialDownloadOtherPasses = 3

// BLOCKS TAKEN ON ONE NODE'S WORD
//
// A node does not replace more than a set number of its own blocks, however
// much work a competing branch shows, so that settled history cannot be
// rewritten. That rule is right for blocks a node watched arrive, one by
// one, with the whole network looking on. It is wrong for the blocks a node
// downloaded when it started: those came from one source, usually a seed,
// and nobody else has vouched for them yet. If the limit protected them, a
// seed could hand newcomers a chain of its own making, and they would refuse
// the real chain for ever after.
//
// So the blocks a node downloads at start-up, and every block it receives
// after them, are not counted against the limit until a node that is not a
// seed has shown that it holds the same chain. Until then the newcomer
// follows whichever chain proves the most work, however far back the two
// part above the height it started from. That stays true if the program is
// stopped and started again: the height is kept in the database.
//
// Once a node that is not a seed agrees, the node is on a chain that someone
// other than the seeds vouches for, and the limit protects it from then on.
// Blocks that were in the database before the download are counted always.
//
// This cannot tell a node run by a stranger from one run by whoever runs the
// seeds. Nothing in software can. What it ensures is that the seeds alone
// are never enough to hold a newcomer; the rest is seeds run by more than
// one person, and --connect, which lets an operator name nodes they trust.
var initialDownload struct {
	sync.Mutex
	from int64 // heights above this are not counted...
	to   int64 // ...up to and including this one, once settled
	open bool  // not yet settled: every height above `from` is uncounted
}

var unsettledDownloadKey = []byte("unsettled_download_from")

// setInitialDownload puts the bookkeeping in a given state, in memory only.
func setInitialDownload(from, to int64, open bool) {
	initialDownload.Lock()
	initialDownload.from, initialDownload.to, initialDownload.open = from, to, open
	initialDownload.Unlock()
}

// beginInitialDownload is called when the program starts catching up, with
// the height it has. If an earlier run ended before its download was
// settled, the height that run started from still stands.
func beginInitialDownload(height int64) {
	from := height
	if GlobalBoltEngine != nil {
		_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
			bMeta := tx.Bucket(metaBucket)
			if bMeta == nil {
				return nil
			}
			if stored, err := strconv.ParseInt(string(bMeta.Get(unsettledDownloadKey)), 10, 64); err == nil && stored >= 0 && stored < from {
				from = stored
			}
			return bMeta.Put(unsettledDownloadKey, []byte(strconv.FormatInt(from, 10)))
		})
	}
	setInitialDownload(from, from, true)
}

// settleInitialDownload ends the exemption for blocks received from now on.
func settleInitialDownload() {
	tip := GetLatestBlockInternal().Index
	initialDownload.Lock()
	wasOpen := initialDownload.open
	initialDownload.to, initialDownload.open = tip, false
	initialDownload.Unlock()
	if GlobalBoltEngine != nil {
		_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
			if bMeta := tx.Bucket(metaBucket); bMeta != nil {
				return bMeta.Delete(unsettledDownloadKey)
			}
			return nil
		})
	}
	if wasOpen {
		log.Printf("🤝 [SYNC] A node that is not a seed holds the same chain as this one, up to block #%d.\n", tip)
	}
}

// nodesThatAreNotSeeds lists the nodes this one knows of, other than the
// seeds: those the operator named, those that have delivered blocks, those
// remembered from earlier runs, and those connected now.
func nodesThatAreNotSeeds() []string {
	isSeed := make(map[string]bool)
	for _, endpoint := range append(seedSyncURLs(), Params.ExtraSyncURLs...) {
		isSeed[endpoint] = true
	}
	seen := make(map[string]bool)
	var others []string
	tiers := syncSourceTiers()
	for _, tier := range append([][]string{operatorSyncSources}, tiers...) {
		for _, source := range tier {
			if !isSeed[source] && !seen[source] {
				seen[source] = true
				others = append(others, source)
			}
		}
	}
	return others
}

// trySettleInitialDownload asks the nodes that are not seeds whether they
// hold this node's chain, and settles the download if one does. It asks
// about a block a little below the tip, so that a block still on its way
// round the network does not look like a disagreement.
func trySettleInitialDownload() {
	initialDownload.Lock()
	open, from := initialDownload.open, initialDownload.from
	initialDownload.Unlock()
	if !open {
		return
	}
	tip := GetLatestBlockInternal().Index
	if tip <= from {
		// Nothing was downloaded: there is nothing taken on a seed's word.
		settleInitialDownload()
		return
	}
	height := tip - 2
	if height <= from {
		height = tip
	}
	ours := GetBlockByHeightFromDB(height).Hash
	if ours == "" {
		return
	}
	for _, source := range nodesThatAreNotSeeds() {
		if theirs, ok := fetchBlockHash(source, height); ok && theirs == ours {
			settleInitialDownload()
			return
		}
	}
}

// runDownloadSettler keeps trying until the download is settled.
func runDownloadSettler() {
	for {
		time.Sleep(downloadSettleInterval)
		trySettleInitialDownload()
		initialDownload.Lock()
		open := initialDownload.open
		initialDownload.Unlock()
		if !open {
			return
		}
	}
}

const downloadSettleInterval = 2 * time.Minute

// countedReorgDepth is how many of our blocks above `ancestor` count against
// the reorganisation limit when our tip is at `tip`.
func countedReorgDepth(tip, ancestor int64) int64 {
	initialDownload.Lock()
	from, to, open := initialDownload.from, initialDownload.to, initialDownload.open
	initialDownload.Unlock()
	if open {
		to = tip
	}
	depth := tip - ancestor
	low, high := ancestor, tip
	if from > low {
		low = from
	}
	if to < high {
		high = to
	}
	if high > low {
		depth -= high - low
	}
	return depth
}

// lowestReplaceableAncestor is the lowest height at which our chain may part
// from another's and still be replaced: the furthest back a pass needs to
// look.
func lowestReplaceableAncestor(tip int64) int64 {
	if Params.Economics == nil || Params.Economics.ReorgLimitBlocks <= 0 {
		return 0
	}
	// The counted depth only grows as the ancestor moves down, so the lowest
	// allowed height is found by halving the range.
	low, high := int64(0), tip
	for low < high {
		middle := low + (high-low)/2
		if reorgDepthAllowed(countedReorgDepth(tip, middle)) {
			high = middle
		} else {
			low = middle + 1
		}
	}
	return low
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

// A BLOCK THAT ARRIVED DURING A PASS
//
// While a pass is changing the chain, a block arriving from another node
// cannot be placed and is dropped. If nothing more were done the node would
// be one block short when the pass ended, could not attach the block after
// that either, and would wait out syncPassFromBlockInterval before asking:
// on a 45-second network, a whole block interval spent behind, mining (if it
// mines) on a block that has already been built on.
//
// So the drop is remembered, and when the pass ends another starts at once to
// fetch what was missed. It is spaced by a few seconds, so that a node sent
// blocks continuously cannot be kept asking its peers without pause.

var blockMissedDuringPass int32 // 1 if a block was dropped since the last pass began
var lastPassAfterMiss int64     // unix nanoseconds, read and written atomically

const missedBlockPassInterval = 3 * time.Second

func catchUpAfterMissedBlock() {
	if !atomic.CompareAndSwapInt32(&blockMissedDuringPass, 1, 0) {
		return
	}
	now := time.Now().UnixNano()
	last := atomic.LoadInt64(&lastPassAfterMiss)
	if now-last < int64(missedBlockPassInterval) || !atomic.CompareAndSwapInt64(&lastPassAfterMiss, last, now) {
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
	defer func() {
		atomic.StoreInt32(&syncPassRunning, 0)
		catchUpAfterMissedBlock()
	}()
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
	atomic.StoreInt64(&networkHeightHeard, claimedHeight)

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
	lowest := lowestReplaceableAncestor(startTip.Index)
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
		if depth := countedReorgDepth(tip.Index, ancestor); !reorgDepthAllowed(depth) {
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
			// The bar is drawn here, as the blocks arrive. A whole chain often
			// comes in one pass, and a bar drawn only between passes would
			// then never be seen.
			log.Printf("⏳ [SYNC] Downloading: block #%d of #%d %s %d%%\n", next, claimedHeight,
				progressBar(next-1, claimedHeight, 30), (next-1)*100/claimedHeight)
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
