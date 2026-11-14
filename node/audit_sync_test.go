package node

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// standInPeer plays another node's block-download address on this machine.
type standInPeer struct {
	server        *httptest.Server
	blocks        []Block // its chain, from the genesis block
	claimedHeight int64
	claimedWork   string
	hasBlockHash  bool // false: an older node without the small request
	requests      int32
}

func newStandInPeer(t *testing.T, blocks []Block) *standInPeer {
	t.Helper()
	peer := &standInPeer{blocks: blocks, hasBlockHash: true}
	peer.claimedHeight = int64(len(blocks) - 1)
	peer.claimedWork = sumBlockWork(blocks).String()
	peer.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&peer.requests, 1)
		switch r.URL.Path {
		case "/block":
			json.NewEncoder(w).Encode(map[string]interface{}{"current_height": peer.claimedHeight, "total_work": peer.claimedWork})
		case "/blockhash":
			height, _ := strconv.ParseInt(r.URL.Query().Get("height"), 10, 64)
			if !peer.hasBlockHash || height < 0 || height >= int64(len(peer.blocks)) {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"height": height, "hash": peer.blocks[height].Hash})
		case "/blocks/batch":
			start, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
			var batch []Block
			for h := start; h >= 0 && h < int64(len(peer.blocks)); h++ {
				batch = append(batch, peer.blocks[h])
			}
			json.NewEncoder(w).Encode(batch)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(peer.server.Close)
	return peer
}

// asOnlySeed makes this stand-in the one node the test chain knows about.
func (peer *standInPeer) asOnlySeed(t *testing.T) string {
	t.Helper()
	host, portText, _ := net.SplitHostPort(peer.server.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	Params.SeedHosts = []string{host}
	Params.SyncPort = port
	Params.ExtraSyncURLs = nil
	Params.WindowsSelfSkipHost = ""
	t.Cleanup(func() {
		syncSetAside.Lock()
		syncSetAside.until = make(map[string]time.Time)
		syncSetAside.Unlock()
	})
	return Params.syncURL(host)
}

func localChain() []Block {
	var blocks []Block
	for h := int64(0); h <= GetLatestBlockInternal().Index; h++ {
		blocks = append(blocks, GetBlockByHeightFromDB(h))
	}
	return blocks
}

func buildTestChain(t *testing.T, length int) []Block {
	t.Helper()
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < length; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, "CVN_miner", nil)
	}
	return localChain()
}

// The point where two chains part is found with a few small requests, not by
// walking back one block at a time and downloading 512 blocks at each step.
func TestCommonAncestorIsFoundQuickly(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	ours := buildTestChain(t, 40)

	// Another chain that agrees with ours up to block 23.
	theirs := append([]Block{}, ours...)
	for h := 24; h < len(theirs); h++ {
		theirs[h].Hash = fmt.Sprintf("%064x", h)
	}
	peer := newStandInPeer(t, theirs)
	if got := findCommonAncestor(peer.server.URL, 0, 40); got != 23 {
		t.Errorf("the chains part after block 23; found %d", got)
	}
	if used := atomic.LoadInt32(&peer.requests); used > 12 {
		t.Errorf("finding the point took %d requests on a 40-block chain", used)
	}
	if got := findCommonAncestor(newStandInPeer(t, ours).server.URL, 0, 40); got != 40 {
		t.Errorf("identical chains: found %d, want 40", got)
	}
	if got := findCommonAncestor(peer.server.URL, 30, 40); got != -1 {
		t.Errorf("chains that already differ at the lowest height looked at: found %d, want -1", got)
	}
	// An older node, without the small request, is asked the older way.
	older := newStandInPeer(t, theirs)
	older.hasBlockHash = false
	if got := findCommonAncestor(older.server.URL, 0, 40); got != 23 {
		t.Errorf("against an older node: found %d, want 23", got)
	}
}

// Audit finding: a node believed whatever a peer said about its own chain.
// One that claimed enormous work was chosen as the only source every time,
// delivered nothing, and was chosen again, so the node never asked anyone
// else.
func TestALyingSourceIsSetAside(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	ours := buildTestChain(t, 6)
	tip := GetLatestBlockInternal()

	liar := newStandInPeer(t, ours)
	liar.claimedHeight = 500000
	liar.claimedWork = "99999999999999999999999999999999"
	source := liar.asOnlySeed(t)

	if chosen, _ := chooseSyncSource(tip.Index, TotalChainWork()); chosen != source {
		t.Fatalf("the test expects the liar to be chosen first; chosen %q", chosen)
	}
	TriggerActiveReSyncPass()
	if atomic.LoadInt32(&IsSyncingActive) != 0 {
		t.Fatal("the pass ended and left mining paused")
	}
	if now := GetLatestBlockInternal(); now.Hash != tip.Hash {
		t.Fatalf("the liar moved our chain from #%d to #%d", tip.Index, now.Index)
	}
	if !syncSourceIsSetAside(source) {
		t.Fatal("a source that claimed a better chain and delivered nothing was not set aside")
	}
	if chosen, _ := chooseSyncSource(tip.Index, TotalChainWork()); chosen != "" {
		t.Errorf("the liar was chosen again straight away")
	}
}

// An honest source with a longer chain is followed, and is not set aside.
func TestAnHonestSourceIsFollowed(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	full := buildTestChain(t, 12)
	StateMutex.Lock()
	rollbackChainToHeightLocked(5)
	StateMutex.Unlock()
	if GetLatestBlockInternal().Index != 5 {
		t.Fatal("could not set the node five blocks back")
	}
	peer := newStandInPeer(t, full)
	source := peer.asOnlySeed(t)

	TriggerActiveReSyncPass()
	if now := GetLatestBlockInternal(); now.Index != 12 || now.Hash != full[12].Hash {
		t.Fatalf("after the pass the node is at #%d, want #12", now.Index)
	}
	if syncSourceIsSetAside(source) {
		t.Error("a source that delivered the chain it claimed was set aside")
	}
	if balanceOf("CVN_miner") != cvn("600") {
		t.Errorf("after catching up the miner has %s CVN, want 600", FormatUnits(balanceOf("CVN_miner")))
	}
}

// Audit finding: a node answered a request for blocks with 512 of them
// whatever their size. With full blocks that is over 100 MB, more than can
// be sent or checked in the time allowed, so a node could never catch up.
func TestBlockBatchesAreLimitedBySize(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	buildTestChain(t, 30)
	all := blocksForBatch(1, 30)
	if len(all) != 30 {
		t.Fatalf("thirty small blocks should travel together; got %d", len(all))
	}
	saved := syncBatchMaxBytes
	syncBatchMaxBytes = 5 * blockSizeBytes(all[0])
	t.Cleanup(func() { syncBatchMaxBytes = saved })
	limited := blocksForBatch(1, 30)
	if len(limited) < 1 || len(limited) > 6 {
		t.Errorf("with room for about five blocks the batch holds %d", len(limited))
	}
	if next := blocksForBatch(limited[len(limited)-1].Index+1, 30); len(next) == 0 || next[0].Index != limited[len(limited)-1].Index+1 {
		t.Error("the next request does not carry on where the first batch stopped")
	}
	// However small the limit, a request is never answered with nothing.
	syncBatchMaxBytes = 1
	if one := blocksForBatch(7, 30); len(one) != 1 || one[0].Index != 7 {
		t.Errorf("a batch under a tiny limit should hold exactly the block asked for: %d blocks", len(one))
	}
}
