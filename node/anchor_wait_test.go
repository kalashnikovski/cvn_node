package node

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// standInExplorer plays a public Bitcoin block explorer on this machine. It
// knows one block, at height 800,000, and only reports it once `mined` is 1.
// No test contacts a real explorer.
type standInExplorer struct {
	server *httptest.Server
	mined  int32
	hash   string
	header string
}

func newStandInExplorer(t *testing.T, hash, header string) *standInExplorer {
	t.Helper()
	explorer := &standInExplorer{hash: hash, header: header}
	explorer.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mined := atomic.LoadInt32(&explorer.mined) == 1
		switch {
		case r.URL.Path == "/blocks/tip/height":
			if mined {
				fmt.Fprint(w, "800000")
			} else {
				fmt.Fprint(w, "799998")
			}
		case r.URL.Path == "/block-height/800000" && mined:
			fmt.Fprint(w, explorer.hash)
		case r.URL.Path == "/block/"+explorer.hash+"/header" && mined:
			fmt.Fprint(w, explorer.header)
		default:
			http.Error(w, "Block height out of range", http.StatusNotFound)
		}
	}))
	t.Cleanup(explorer.server.Close)
	return explorer
}

func useStandInExplorers(t *testing.T, explorers ...*standInExplorer) {
	t.Helper()
	saved := anchorSources
	anchorSources = nil
	for _, explorer := range explorers {
		anchorSources = append(anchorSources, explorer.server.URL)
	}
	t.Cleanup(func() { anchorSources = saved })
}

func TestAnchorIsTakenOnlyWhenTwoExplorersAgree(t *testing.T) {
	first := newStandInExplorer(t, testHeaderHash, testHeaderA)
	second := newStandInExplorer(t, testHeaderHash, testHeaderA)
	useStandInExplorers(t, first, second)

	// Before the block exists there is nothing to agree on, but the
	// explorers do say how far Bitcoin has got.
	if _, tip, agreed := lookUpAnchor(800000); agreed || tip != 799998 {
		t.Fatalf("before the block is mined: agreed %v, Bitcoin height %d", agreed, tip)
	}
	// One explorer alone is not enough.
	atomic.StoreInt32(&first.mined, 1)
	if _, _, agreed := lookUpAnchor(800000); agreed {
		t.Error("a header was taken on one explorer's word")
	}
	atomic.StoreInt32(&second.mined, 1)
	header, tip, agreed := lookUpAnchor(800000)
	if !agreed || header != testHeaderA || tip != 800000 {
		t.Fatalf("with both explorers reporting the block: agreed %v, tip %d", agreed, tip)
	}

	// Two explorers that name different blocks at that height: neither is used.
	other := newStandInExplorer(t, testHeaderBHash, testHeaderB)
	atomic.StoreInt32(&other.mined, 1)
	useStandInExplorers(t, first, other)
	if _, _, agreed := lookUpAnchor(800000); agreed {
		t.Error("a header was taken although the explorers disagree")
	}
	// An explorer whose header does not match the hash it gave, or is not a
	// Bitcoin header at all, counts for nothing, even if two say the same.
	lyingA := newStandInExplorer(t, testHeaderHash, testHeaderB)
	lyingB := newStandInExplorer(t, testHeaderHash, testHeaderB)
	junkA := newStandInExplorer(t, testHeaderHash, strings.Repeat("0", 160))
	junkB := newStandInExplorer(t, testHeaderHash, strings.Repeat("0", 160))
	for _, explorer := range []*standInExplorer{lyingA, lyingB, junkA, junkB} {
		atomic.StoreInt32(&explorer.mined, 1)
	}
	useStandInExplorers(t, lyingA, lyingB)
	if _, _, agreed := lookUpAnchor(800000); agreed {
		t.Error("a header that does not match the hash at that height was taken")
	}
	useStandInExplorers(t, junkA, junkB)
	if _, _, agreed := lookUpAnchor(800000); agreed {
		t.Error("something that is not a Bitcoin header was taken")
	}
}

// A node started before the launch waits, and starts by itself when the
// Bitcoin block appears.
func TestNodeWaitsForItsAnchorAndStartsByItself(t *testing.T) {
	onNetwork(t, "devnet")
	asLaunchNetwork()
	t.Chdir(t.TempDir())
	first := newStandInExplorer(t, testHeaderHash, testHeaderA)
	second := newStandInExplorer(t, testHeaderHash, testHeaderA)
	useStandInExplorers(t, first, second)
	treatOldBlocksAsNew(t)

	// Scheduled but not yet mined: the node can wait for this.
	if err := resolveGenesisAnchor([]string{"cvn_node"}); !errors.Is(err, errAnchorNotYetKnown) {
		t.Fatalf("a scheduled network without its anchor should be waited for: %v", err)
	}
	started := make(chan struct{})
	go func() {
		waitForGenesisAnchor([]string{"cvn_node"}, 20*time.Millisecond, 20*time.Millisecond)
		close(started)
	}()
	select {
	case <-started:
		t.Fatal("the node started before the Bitcoin block existed")
	case <-time.After(300 * time.Millisecond):
	}
	atomic.StoreInt32(&first.mined, 1)
	atomic.StoreInt32(&second.mined, 1)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the node did not start after the Bitcoin block appeared")
	}
	if Params.Anchor.BitcoinHash != testHeaderHash || Params.GenesisTimestamp != testHeaderTime {
		t.Errorf("the node started from %s dated %d", Params.Anchor.BitcoinHash, Params.GenesisTimestamp)
	}
	// The anchor is kept, so the next start needs no explorer.
	saved, err := os.ReadFile(genesisAnchorFileName())
	if err != nil || strings.TrimSpace(string(saved)) != testHeaderA {
		t.Fatalf("the anchor was not kept for later starts: %v", err)
	}
	asLaunchNetwork()
	useStandInExplorers(t) // none
	if err := resolveGenesisAnchor([]string{"cvn_node"}); err != nil || Params.Anchor.BitcoinHash != testHeaderHash {
		t.Errorf("the next start did not use the kept anchor: %v", err)
	}
}

// With look-ups switched off the node waits for the operator's file alone.
func TestNodeWaitsForTheOperatorsFile(t *testing.T) {
	onNetwork(t, "devnet")
	asLaunchNetwork()
	t.Chdir(t.TempDir())
	explorerA := newStandInExplorer(t, testHeaderHash, testHeaderA)
	explorerB := newStandInExplorer(t, testHeaderHash, testHeaderA)
	atomic.StoreInt32(&explorerA.mined, 1)
	atomic.StoreInt32(&explorerB.mined, 1)
	useStandInExplorers(t, explorerA, explorerB)

	started := make(chan struct{})
	go func() {
		waitForGenesisAnchor([]string{"cvn_node", "--no-anchor-lookup"}, 20*time.Millisecond, 20*time.Millisecond)
		close(started)
	}()
	select {
	case <-started:
		t.Fatal("the node asked the explorers although look-ups were switched off")
	case <-time.After(300 * time.Millisecond):
	}
	// A file that is not a header does not start it; the right one does.
	if err := os.WriteFile(genesisAnchorFileName(), []byte("not a header"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		t.Fatal("the node started from a file that does not hold a header")
	case <-time.After(200 * time.Millisecond):
	}
	if err := os.WriteFile(genesisAnchorFileName(), []byte(testHeaderB+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the node did not start after the header was put in the file")
	}
	if Params.Anchor.BitcoinHash != testHeaderBHash {
		t.Errorf("the node started from %s, not from the header in the file", Params.Anchor.BitcoinHash)
	}
}

// An unscheduled network is not waited for: there is no height to wait for.
func TestUnscheduledNetworkIsNotWaitedFor(t *testing.T) {
	onNetwork(t, "cvn_testnet")
	err := resolveGenesisAnchor([]string{"cvn_node"})
	if err == nil || errors.Is(err, errAnchorNotYetKnown) {
		t.Errorf("an unscheduled network should refuse outright, not wait: %v", err)
	}
}

// Devnet can rehearse a launch: with a Bitcoin height given, it needs an
// anchor exactly as a launch network does.
func TestDevnetCanRehearseALaunch(t *testing.T) {
	t.Setenv("CVN_DEVNET_ANCHOR_HEIGHT", "800000")
	onNetwork(t, "devnet")
	t.Chdir(t.TempDir())
	if !Params.Anchor.required() || Params.Anchor.BitcoinHeight != 800000 {
		t.Fatal("the rehearsal height was not taken")
	}
	if err := resolveGenesisAnchor([]string{"cvn_node"}); !errors.Is(err, errAnchorNotYetKnown) {
		t.Errorf("a rehearsal without its anchor should be waited for: %v", err)
	}
	if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", testHeaderA}); err != nil || buildGenesisBlock().Timestamp != testHeaderTime {
		t.Errorf("the rehearsal did not start from its anchor: %v", err)
	}
	t.Setenv("CVN_DEVNET_ANCHOR_HEIGHT", "")
	onNetwork(t, "devnet")
	if Params.Anchor.required() {
		t.Error("devnet needs no anchor unless a rehearsal is asked for")
	}
}

// treatOldBlocksAsNew lets a test stand in a Bitcoin block from years ago for
// one that has just been mined.
func treatOldBlocksAsNew(t *testing.T) {
	t.Helper()
	saved := anchorFreshness
	anchorFreshness = 100 * 365 * 24 * time.Hour
	t.Cleanup(func() { anchorFreshness = saved })
}

// One explorer out of reach, or several, delays nobody as long as two answer.
func TestAnchorLookupSurvivesExplorersBeingDown(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "try later", http.StatusServiceUnavailable)
	}))
	t.Cleanup(down.Close)
	good := func() *standInExplorer {
		explorer := newStandInExplorer(t, testHeaderHash, testHeaderA)
		atomic.StoreInt32(&explorer.mined, 1)
		return explorer
	}
	other := func() *standInExplorer {
		explorer := newStandInExplorer(t, testHeaderBHash, testHeaderB)
		atomic.StoreInt32(&explorer.mined, 1)
		return explorer
	}
	saved := anchorSources
	t.Cleanup(func() { anchorSources = saved })

	anchorSources = []string{down.URL, good().server.URL, down.URL, good().server.URL, down.URL}
	if header, _, agreed := lookUpAnchor(800000); !agreed || header != testHeaderA {
		t.Error("two explorers agreed and three were down, and the header was not taken")
	}
	// One explorer naming another block does not stop two that agree.
	anchorSources = []string{good().server.URL, other().server.URL, good().server.URL}
	if header, _, agreed := lookUpAnchor(800000); !agreed || header != testHeaderA {
		t.Error("two explorers agreed against one, and the header was not taken")
	}
	// Two against two: Bitcoin itself is undecided, and so is the node.
	anchorSources = []string{good().server.URL, other().server.URL, good().server.URL, other().server.URL}
	if _, _, agreed := lookUpAnchor(800000); agreed {
		t.Error("a header was taken although two explorers named each of two blocks")
	}
}

// A node that has itself seen Bitcoin short of the height takes only a block
// dated about now: the explorers cannot hand it an older block and call it
// the one it was waiting for.
func TestWaitingNodeRefusesAnOldBlock(t *testing.T) {
	onNetwork(t, "devnet")
	asLaunchNetwork()
	t.Chdir(t.TempDir())
	first := newStandInExplorer(t, testHeaderHash, testHeaderA)
	second := newStandInExplorer(t, testHeaderHash, testHeaderA)
	useStandInExplorers(t, first, second)

	started := make(chan struct{})
	go func() {
		waitForGenesisAnchor([]string{"cvn_node"}, 20*time.Millisecond, 20*time.Millisecond)
		close(started)
	}()
	time.Sleep(200 * time.Millisecond) // it sees Bitcoin at 799,998
	atomic.StoreInt32(&first.mined, 1)
	atomic.StoreInt32(&second.mined, 1)
	select {
	case <-started:
		t.Fatal("a node that watched Bitcoin reach the height started from a block dated in 2023")
	case <-time.After(600 * time.Millisecond):
	}
	if _, err := os.Stat(genesisAnchorFileName()); err == nil {
		t.Error("the old block was saved as the anchor")
	}
	// The operator's file still starts it, so the test's waiting node ends.
	treatOldBlocksAsNew(t)
	if err := os.WriteFile(genesisAnchorFileName(), []byte(testHeaderA), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the node did not start from the operator's file")
	}
}

// A header must carry about as much work as a current Bitcoin block: one
// that merely meets an easy target it states itself is not a Bitcoin block.
func TestAnchorHeaderMustCarryBitcoinsWork(t *testing.T) {
	if bitcoinAnchorEasiestBits != 0x170859cc {
		t.Fatalf("the easiest accepted Bitcoin target changed: %#x", bitcoinAnchorEasiestBits)
	}
	// Both test headers are real Bitcoin blocks of July 2023, within the limit.
	if _, _, err := parseBitcoinHeader(testHeaderA); err != nil {
		t.Fatalf("a real Bitcoin header was refused: %v", err)
	}
	// With the limit set where Bitcoin stood later, the same headers state a
	// target that is too easy, exactly as a smaller chain's header would.
	saved := bitcoinAnchorEasiestBits
	bitcoinAnchorEasiestBits = 0x17021673
	t.Cleanup(func() { bitcoinAnchorEasiestBits = saved })
	if _, _, err := parseBitcoinHeader(testHeaderA); err == nil || !strings.Contains(err.Error(), "far less work") {
		t.Errorf("a header stating too easy a target was accepted: %v", err)
	}
}

// A network can name the earliest date its Bitcoin block may carry, so that
// no block mined before the announcement can be passed off as the anchor.
func TestAnchorCannotBeABlockFromBeforeTheAnnouncement(t *testing.T) {
	onNetwork(t, "devnet")
	asLaunchNetwork()
	t.Chdir(t.TempDir())
	Params.Anchor.NotBefore = testHeaderTime + 1
	err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", testHeaderA})
	if err == nil || !strings.Contains(err.Error(), "before this network's start was announced") {
		t.Errorf("a block from before the announcement was accepted: %v", err)
	}
	asLaunchNetwork()
	Params.Anchor.NotBefore = testHeaderTime
	if err := resolveGenesisAnchor([]string{"cvn_node", "--genesis-anchor", testHeaderA}); err != nil {
		t.Errorf("a block from after the announcement was refused: %v", err)
	}
}
