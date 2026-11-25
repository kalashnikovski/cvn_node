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
	// syncReplyLimitBytes is the most a node will read of one reply. An
	// honest reply stops growing once its blocks reach syncBatchMaxBytes by
	// the rules' measure, so it ends within one block of that. As JSON it is
	// larger: at worst about three times the rules' measure, for inputs that
	// spend coins from rewards with the shortest identifiers allowed. So an
	// honest reply is at most about 13 MB (TestAFullHonestBatchFitsTheReply
	// Limit builds one), and 24 MB leaves a margin. It was 64 MB, which a
	// stranger chosen as the source could fill with empty blocks that decoded
	// to gigabytes (found 25 November); see decodeBlockBatchFromPeer as well.
	syncReplyLimitBytes = 24 << 20
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

// A SOURCE THAT SENT NO BLOCKS
//
// A source that reports a block and then sends none from that height on has
// not delivered what it claimed. But an honest node does exactly that for a
// moment while it changes its own chain: it has taken blocks off and not yet
// put the others on, so the height it reported a second ago is one it does
// not hold now. The same goes for a node that was restarted, or a request
// lost on the way. Setting such a node aside for ten minutes punished it for
// nothing (review round 7, report 1).
//
// So a pass that ends this way is excused, once. If the same source does it
// again within syncSourceCooldown without having delivered anything in
// between, it is set aside as before: a source that never delivers cannot
// be asked for ever.
var syncExcused = struct {
	sync.Mutex
	at map[string]time.Time
}{at: make(map[string]time.Time)}

// excuseSyncSourceOnce reports whether a source that sent no blocks may be
// let off this time, and notes that it was.
func excuseSyncSourceOnce(source string) bool {
	syncExcused.Lock()
	defer syncExcused.Unlock()
	for other, at := range syncExcused.at {
		if time.Since(at) > syncSourceCooldown {
			delete(syncExcused.at, other)
		}
	}
	if _, already := syncExcused.at[source]; already {
		return false
	}
	syncExcused.at[source] = time.Now()
	return true
}

// chainComparisonsPerPass is how often one pass compares chains with its
// source before it gives up for now (see TriggerActiveReSyncPass).
const chainComparisonsPerPass = 3

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
	body, err := io.ReadAll(io.LimitReader(resp.Body, syncReplyLimitBytes+1))
	if err != nil || len(body) > syncReplyLimitBytes || decodeBlockBatchFromPeer(body, &batch) != nil {
		return nil, false
	}
	// A node sends at most syncBatchMaxBlocks in one reply. One that sends
	// more is not taken at its word: the rest is asked for in its turn, like
	// any other blocks, so that no reply decides how much work is done on it.
	if len(batch) > syncBatchMaxBlocks {
		batch = batch[:syncBatchMaxBlocks]
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
//
// It is kept short. Every source in it is asked for its newest block at the
// start of every pass, one after another, and one that has gone away takes
// two seconds to give up on. Nothing used to leave the list but a source
// that was chosen and then failed, and one that no longer answers is never
// chosen: after eight such sources a pass spent sixteen seconds on them
// before it looked at a node that was there, and the number only grew (the
// review of the round 7 fixes, finding 1). So a source is forgotten the
// first time it does not answer, a day after it last delivered, or when
// more recent ones have filled the list. A node that is still about is
// still asked, among those connected to this one.
var syncDelivered = struct {
	sync.Mutex
	at map[string]time.Time
}{at: make(map[string]time.Time)}

const (
	syncDeliveredKept = 8
	syncDeliveredFor  = 24 * time.Hour
)

// provenSyncSources lists the sources remembered for having delivered, after
// dropping those remembered too long and those there is no room for. The
// seeds are left out: they are asked first in any case.
func provenSyncSources(seeds []string) []string {
	syncDelivered.Lock()
	defer syncDelivered.Unlock()
	for _, seed := range seeds {
		delete(syncDelivered.at, seed)
	}
	for source, at := range syncDelivered.at {
		if time.Since(at) > syncDeliveredFor {
			delete(syncDelivered.at, source)
		}
	}
	for len(syncDelivered.at) > syncDeliveredKept {
		oldest := ""
		for source, at := range syncDelivered.at {
			if oldest == "" || at.Before(syncDelivered.at[oldest]) {
				oldest = source
			}
		}
		delete(syncDelivered.at, oldest)
	}
	proven := make([]string, 0, len(syncDelivered.at))
	for source := range syncDelivered.at {
		proven = append(proven, source)
	}
	return proven
}

// forgetSyncDelivery drops a source from those remembered for delivering.
func forgetSyncDelivery(source string) {
	syncDelivered.Lock()
	delete(syncDelivered.at, source)
	syncDelivered.Unlock()
}

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
	proven := append(provenSyncSources(seeds), cachedPeerHTTPGateways(seeds)...)

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
	if atomic.LoadInt32(&syncPassRunning) == 1 {
		// A pass is running already and may have chosen its source before
		// this block existed. Starting another now would do nothing, and the
		// request would be used up for the next 30 seconds. So the block is
		// noted as missed, and the pass that follows this one fetches it.
		atomic.StoreInt32(&blockMissedDuringPass, 1)
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

var blockMissedDuringPass int32  // 1 if a block was dropped since the last pass began
var lastPassAfterMiss int64      // unix nanoseconds, read and written atomically
var missedBlockPassWaiting int32 // 1 while a pass for a missed block is waiting out the spacing

const missedBlockPassInterval = 3 * time.Second

// catchUpAfterMissedBlock starts a pass for a block that was dropped, now or,
// when one was started for the same reason a moment ago, once the spacing
// has run out. The note that a block was dropped is cleared only when the
// pass for it starts. It used to be cleared first, and when the spacing then
// said "not yet" the block was forgotten.
func catchUpAfterMissedBlock() {
	if atomic.LoadInt32(&blockMissedDuringPass) == 0 || atomic.LoadInt32(&syncPassRunning) == 1 {
		// Nothing was missed, or a pass is running and will come here
		// again when it ends.
		return
	}
	last := atomic.LoadInt64(&lastPassAfterMiss)
	if wait := time.Duration(last) + missedBlockPassInterval - time.Duration(time.Now().UnixNano()); wait > 0 {
		if atomic.CompareAndSwapInt32(&missedBlockPassWaiting, 0, 1) {
			time.AfterFunc(wait, func() {
				atomic.StoreInt32(&missedBlockPassWaiting, 0)
				catchUpAfterMissedBlock()
			})
		}
		return
	}
	if !atomic.CompareAndSwapInt64(&lastPassAfterMiss, last, time.Now().UnixNano()) ||
		!atomic.CompareAndSwapInt32(&blockMissedDuringPass, 1, 0) {
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
		var bestAnswered time.Duration
		claimedHeight = ourHeight
		for _, gateway := range tier {
			if asked[gateway] || syncSourceIsSetAside(gateway) {
				continue
			}
			asked[gateway] = true
			askedAt := time.Now()
			peerHeight, peerWork := probeChain(gateway)
			answered := time.Since(askedAt)
			if peerHeight <= 0 && peerWork == nil {
				forgetSyncDelivery(gateway) // it did not answer
			}
			if !peerChainIsBetter(peerHeight, peerWork, ourHeight, ourWork) {
				continue
			}
			better := source == ""
			if peerWork != nil {
				better = better || bestWork == nil || peerWork.Cmp(bestWork) > 0
				// The seeds nearly always hold the same chain. Among those
				// that claim exactly the same work, the one that answered
				// soonest is taken: it is the nearest, and the blocks will
				// come from it fastest. It used to be whichever stood first
				// in the list, so every node in the world downloaded from
				// the same seed.
				if !better && bestWork != nil && peerWork.Cmp(bestWork) == 0 && answered < bestAnswered {
					better = true
				}
			} else {
				better = better || (bestWork == nil && peerHeight > claimedHeight)
			}
			if better {
				source, claimedHeight, bestWork, bestAnswered = gateway, peerHeight, peerWork, answered
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
	defer releaseProofHelp()
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
	// noBlocksCame is set when the pass ended because the source sent no
	// blocks from a height it had reported; noBlocksFrom is that height.
	noBlocksCame, noBlocksFrom := false, int64(0)
	defer func() {
		switch {
		case TotalChainWork().Cmp(startWork) > 0:
			syncDelivered.Lock()
			syncDelivered.at[source] = time.Now()
			syncDelivered.Unlock()
			syncExcused.Lock()
			delete(syncExcused.at, source)
			syncExcused.Unlock()
		case blameless:
		case noBlocksCame && excuseSyncSourceOnce(source):
			log.Printf("🔌 [SYNC] %s reported block #%d and sent no blocks from #%d on. It may be changing its own chain. Asking again shortly; if it does this again it is set aside.\n", source, claimedHeight, noBlocksFrom)
			atomic.StoreInt32(&blockMissedDuringPass, 1)
		default:
			forgetSyncDelivery(source)
			setSyncSourceAside(source, "claimed a better chain and did not deliver one")
		}
	}()

	// How far back to look for the point where the chains part. A node never
	// replaces more of its own blocks than the reorganisation limit, so
	// there is no reason to look further back than that.
	//
	// The comparison is made over the network, and takes a moment. The point
	// it finds is the point where the source's chain parts from our chain AS
	// IT WAS when the comparison began. If a block arrived from a neighbour
	// meanwhile, that point is now one below our newest block although the
	// two chains never parted, and the pass used to take it for a rival
	// branch: it took our own good block off, stopped the miner and
	// downloaded the same block again. While the block was off, this node
	// told anyone who asked that it had no block at the height it had just
	// reported, and was set aside for it (review round 7, new finding 1).
	//
	// So the point is only acted on if our newest block is still the one the
	// comparison was made against. If it is not, the chains are compared
	// again as they now are.
	var ancestor int64
	var tip Block
	comparedWith := startTip
	for try := 1; ; try++ {
		lowest := lowestReplaceableAncestor(comparedWith.Index)
		var answered bool
		ancestor, answered = findCommonAncestor(source, lowest, comparedWith.Index)
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
		tip = GetLatestBlockInternal()
		if tip.Hash == comparedWith.Hash {
			break // the chain lock stays held
		}
		StateMutex.Unlock()
		if try >= chainComparisonsPerPass {
			// Blocks keep arriving faster than the chains can be compared.
			// That is no fault of the source; the next pass starts afresh.
			log.Printf("🔁 [SYNC] Our newest block kept changing while our chain was compared with %s. Comparing again shortly.\n", source)
			blameless = true
			atomic.StoreInt32(&blockMissedDuringPass, 1)
			return
		}
		comparedWith = tip
	}

	// The chain lock is held from here.
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
			// over on the next pass; this is no fault of the source. The
			// next pass follows in seconds, like any other for a block that
			// came at a bad moment, and does not wait for the next trigger.
			StateMutex.Unlock()
			log.Printf("🔁 [SYNC] Our newest block changed while the branch from %s was being measured. Comparing again shortly.\n", source)
			blameless = true
			atomic.StoreInt32(&blockMissedDuringPass, 1)
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
		tipWhenAsked := GetLatestBlockInternal().Hash
		batch, answered := fetchBlockBatchChecked(source, next)
		if len(batch) == 0 && answered {
			// It answered, with no blocks from a height it had reported. If
			// it still reports that height, the answer is asked for once
			// more: a node that was changing its chain a moment ago may be
			// done by now.
			if heightNow, _ := probeChain(source); heightNow >= next {
				batch, answered = fetchBlockBatchChecked(source, next)
			} else {
				answered = false // it no longer claims the block at all
			}
		}
		if len(batch) == 0 {
			// No answer, or a source that no longer holds what it reported,
			// may be excused once (see excuseSyncSourceOnce). One that goes
			// on reporting the block and goes on sending nothing is not.
			noBlocksCame, noBlocksFrom = !answered, next
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
		// The blocks are taken a few at a time. The proofs of each few are
		// worked out on several cores (see proof_ahead.go), with the chain
		// lock free, and then those blocks are checked and stored one after
		// another exactly as before. The proofs of a whole batch used to be
		// worked out before the first block was looked at by the rules, so a
		// batch of rubbish cost a batch full of hashes with the node standing
		// still (review round 7, report 2). Now it costs one small group.
		atOnce := proofsAtOnce()
		if atOnce <= 0 {
			atOnce = len(batch)
		}
		standsOn := tipWhenAsked
		refused, moved := false, false
		for from := 0; from < len(batch) && !refused && !moved; from += atOnce {
			part := batch[from:min(from+atOnce, len(batch))]
			prepareProofs(part)
			StateMutex.Lock()
			if tipNow := GetLatestBlockInternal(); tipNow.Hash != standsOn {
				// The newest block changed while these blocks were on their
				// way: this node's miner found one, or a neighbour sent one, or
				// a rival of the newest block took its place. If the new one is
				// among the blocks just fetched, they still fit and the pass
				// goes on. If it is not, they belong on a block this node no
				// longer has on top, and checking them would find the first
				// "not valid" and blame a source that sent nothing wrong. The
				// pass ends without a verdict and another follows, which
				// compares the chains as they now are.
				expected := false
				for _, block := range batch {
					if block.Index == tipNow.Index && block.Hash == tipNow.Hash {
						expected = true
						break
					}
				}
				if !expected {
					StateMutex.Unlock()
					log.Printf("🔁 [SYNC] Our newest block changed while blocks from %s were on their way. Comparing again.\n", source)
					blameless = true
					atomic.StoreInt32(&blockMissedDuringPass, 1)
					moved = true
					break
				}
			}
			for _, block := range part {
				if block.Index != GetLatestBlockInternal().Index+1 {
					continue
				}
				if !ValidateBlockSignatures(block) {
					// A block that fails here because this node took the very
					// same block another way a moment ago (it was on its way
					// from a neighbour when the pass began) is no fault of the
					// source.
					if GetBlockByHeightFromDB(block.Index).Hash == block.Hash {
						continue
					}
					log.Printf("⚠️ [SYNC] Block #%d from %s is not valid. Ending this pass.\n", block.Index, source)
					refused = true
					break
				}
				SaveBlockToStorageInternal(block)
			}
			standsOn = GetLatestBlockInternal().Hash
			StateMutex.Unlock()
		}
		// Stop if the batch moved us nowhere, rather than asking for it again.
		if moved || refused || GetLatestBlockInternal().Index < next {
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
