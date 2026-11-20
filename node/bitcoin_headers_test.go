package node

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The rule for a new target, against the cases Bitcoin's own software is
// tested with (pow_tests.cpp): an ordinary change, the easiest target never
// being passed, and the two limits of a factor of four.
func TestBitcoinTargetChangesAsBitcoinsDoes(t *testing.T) {
	cases := []struct {
		name            string
		lastBits        uint32
		first, last     int64
		expected        uint32
		firstHeightNote string
	}{
		{"an ordinary change", 0x1d00ffff, 1261130161, 1262152739, 0x1d00d86a, "blocks 30,240 to 32,255"},
		{"never easier than the easiest", 0x1d00ffff, 1231006505, 1233061996, 0x1d00ffff, "blocks 0 to 2,015"},
		{"at most four times harder", 0x1c05a3f4, 1279008237, 1279297671, 0x1c0168fd, "blocks 66,528 to 68,543"},
		{"at most four times easier", 0x1c387f6f, 1263163443, 1269211443, 0x1d00e1fd, "blocks 44,352 to 46,367"},
	}
	for _, c := range cases {
		if got := nextBitcoinBits(c.lastBits, c.first, c.last); got != c.expected {
			t.Errorf("%s (%s): got %08x, Bitcoin gives %08x", c.name, c.firstHeightNote, got, c.expected)
		}
	}
	// Writing a target compactly and reading it back gives the same thing.
	for _, bits := range []uint32{0x1d00ffff, 0x1701e5ac, 0x17021673, 0x1b0404cb, 0x207fffff, 0x1c05a3f4} {
		target, ok := compactTarget(bits)
		if !ok || compactFromTarget(target) != bits {
			t.Errorf("%08x does not survive being read and written", bits)
		}
	}
}

func TestBitcoinStartingBlockIsWhatItSays(t *testing.T) {
	chain, err := newBitcoinHeaderChain(bitcoinStart)
	if err != nil {
		t.Fatal(err)
	}
	if hash := chain.headers[0].hashHex(); hash != "0000000000000000000123e2681e2f82acdcb9430a15eec24cc85b400691d479" {
		t.Errorf("the starting block's header has hash %s", hash)
	}
	if bitcoinStart.Height != 975744 || bitcoinStart.Height%2016 != 0 {
		t.Errorf("the starting block must begin a period: %d", bitcoinStart.Height)
	}
	// It must also pass as an anchor would: real, recent Bitcoin work.
	if _, _, err := parseBitcoinHeader(bitcoinStart.Header); err != nil {
		t.Error(err)
	}
	if follower := newBitcoinFollower(bitcoinStart.Height); follower != nil {
		t.Error("a height that is not after the starting block cannot be counted up to")
	}
}

// A launch network must be anchored after the block fixed in the software,
// or its nodes could not count up to the height and would fall back on
// asking block explorers.
func TestScheduledNetworksStartAfterTheFixedBitcoinBlock(t *testing.T) {
	for _, name := range KnownNetworks() {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CVN_DEVNET_ANCHOR_HEIGHT", "")
			onNetwork(t, name)
			if height := Params.Anchor.BitcoinHeight; height > 0 && height <= bitcoinStart.Height {
				t.Errorf("%s starts from Bitcoin block %d, which is not after the fixed block %d: move the fixed block back or the start forward", name, height, bitcoinStart.Height)
			}
		})
	}
}

// --- a small Bitcoin for tests ---------------------------------------------

const testEasyBits = 0x207fffff

// testBitcoin is a chain of headers made for a test under Bitcoin's rules,
// with a period of 8 blocks so that targets change within a few blocks.
type testBitcoin struct {
	mu      sync.Mutex
	start   int64
	headers [][]byte
}

func mineTestHeader(prev [32]byte, when int64, bits uint32, meets bool) []byte {
	raw := make([]byte, 80)
	raw[0] = 1
	copy(raw[4:36], prev[:])
	_, _ = rand.Read(raw[36:68])
	binary.LittleEndian.PutUint32(raw[68:72], uint32(when))
	binary.LittleEndian.PutUint32(raw[72:76], bits)
	for nonce := uint32(0); ; nonce++ {
		binary.LittleEndian.PutUint32(raw[76:80], nonce)
		if header, _ := decodeBitcoinHeader(raw); header.meetsItsTarget() == meets {
			return raw
		}
	}
}

// useSmallBitcoin switches the program to a small Bitcoin whose starting
// block is at height 800, and returns a chain holding just that block.
func useSmallBitcoin(t *testing.T) *testBitcoin {
	t.Helper()
	savedBlocks, savedSeconds, savedEasiest := bitcoinPeriodBlocks, bitcoinPeriodSeconds, bitcoinEasiestBits
	savedAnchorEasiest, savedStart, savedFind, savedSources := bitcoinAnchorEasiestBits, bitcoinStart, findBitcoinPeers, anchorSources
	bitcoinPeriodBlocks, bitcoinPeriodSeconds, bitcoinEasiestBits = 8, 8*600, testEasyBits
	bitcoinAnchorEasiestBits = testEasyBits
	findBitcoinPeers = func() []string { return nil }
	anchorSources = nil
	first := mineTestHeader([32]byte{}, time.Now().Add(-20*time.Hour).Unix(), testEasyBits, true)
	bitcoinStart = bitcoinStartingBlock{Height: 800, Header: hex.EncodeToString(first)}
	anchorFollowerLock.Lock()
	anchorFollower = nil
	anchorFollowerLock.Unlock()
	t.Cleanup(func() {
		bitcoinPeriodBlocks, bitcoinPeriodSeconds, bitcoinEasiestBits = savedBlocks, savedSeconds, savedEasiest
		bitcoinAnchorEasiestBits, bitcoinStart, findBitcoinPeers, anchorSources = savedAnchorEasiest, savedStart, savedFind, savedSources
		anchorFollowerLock.Lock()
		anchorFollower = nil
		anchorFollowerLock.Unlock()
	})
	return &testBitcoin{start: 800, headers: [][]byte{first}}
}

func (b *testBitcoin) tip() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.start + int64(len(b.headers)) - 1
}

func (b *testBitcoin) at(height int64) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.headers[height-b.start]
}

func (b *testBitcoin) all() [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([][]byte(nil), b.headers...)
}

// nextBits is the target Bitcoin's rules give the next block.
func (b *testBitcoin) nextBits() uint32 {
	last, _ := decodeBitcoinHeader(b.headers[len(b.headers)-1])
	if (b.start+int64(len(b.headers)))%bitcoinPeriodBlocks != 0 {
		return last.bits
	}
	first, _ := decodeBitcoinHeader(b.headers[int64(len(b.headers))-bitcoinPeriodBlocks])
	return nextBitcoinBits(last.bits, first.time, last.time)
}

// grow mines `count` more blocks, `spacing` seconds apart.
func (b *testBitcoin) grow(count int, spacing int64) *testBitcoin {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := 0; i < count; i++ {
		last, _ := decodeBitcoinHeader(b.headers[len(b.headers)-1])
		b.headers = append(b.headers, mineTestHeader(last.hash, last.time+spacing, b.nextBits(), true))
	}
	return b
}

// branchAt is a second chain sharing this one's blocks up to `height`.
func (b *testBitcoin) branchAt(height int64) *testBitcoin {
	b.mu.Lock()
	defer b.mu.Unlock()
	return &testBitcoin{start: b.start, headers: append([][]byte(nil), b.headers[:height-b.start+1]...)}
}

func followed(t *testing.T) *bitcoinHeaderChain {
	t.Helper()
	chain, err := newBitcoinHeaderChain(bitcoinStart)
	if err != nil {
		t.Fatal(err)
	}
	return chain
}

func TestBitcoinHeadersAreFollowedAndCounted(t *testing.T) {
	bitcoin := useSmallBitcoin(t).grow(30, 60)
	chain := followed(t)
	added, err := chain.offer(bitcoin.all()[1:], time.Now())
	if err != nil || added != 30 || chain.tipHeight() != 830 {
		t.Fatalf("30 good headers: added %d, tip %d, err %v", added, chain.tipHeight(), err)
	}
	if header, ok := chain.headerAt(815); !ok || header != hex.EncodeToString(bitcoin.at(815)) {
		t.Error("the header counted as block 815 is not the fifteenth after the starting block")
	}
	if _, ok := chain.headerAt(831); ok {
		t.Error("a header was given for a height not reached")
	}
	// The blocks came a minute apart instead of ten, so the target must have
	// tightened at each change: this run really did exercise the rule.
	early, _ := decodeBitcoinHeader(bitcoin.at(801))
	late, _ := decodeBitcoinHeader(bitcoin.at(829))
	if early.bits != testEasyBits || late.bits == testEasyBits {
		t.Fatalf("the target should have changed across three periods: %08x then %08x", early.bits, late.bits)
	}
	// Shown the same headers again, nothing changes.
	if added, err := chain.offer(bitcoin.all()[1:], time.Now()); err != nil || added != 0 {
		t.Errorf("headers already held: added %d, err %v", added, err)
	}
}

func TestBitcoinHeadersThatBreakTheRulesAreRefused(t *testing.T) {
	bitcoin := useSmallBitcoin(t).grow(15, 60) // tip 815; block 816 begins a period
	last, _ := decodeBitcoinHeader(bitcoin.at(815))
	now := time.Now()

	start := func() *bitcoinHeaderChain {
		chain := followed(t)
		if _, err := chain.offer(bitcoin.all()[1:], now); err != nil {
			t.Fatal(err)
		}
		return chain
	}
	refused := func(name string, header []byte, want string) {
		t.Helper()
		chain := start()
		added, err := chain.offer([][]byte{header}, now)
		if err == nil || added != 0 || chain.tipHeight() != 815 {
			t.Errorf("%s: added %d, err %v", name, added, err)
		} else if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: refused, but for another reason: %v", name, err)
		}
	}
	right := bitcoin.nextBits()
	if right == last.bits {
		t.Fatal("this test needs a change of target at block 816")
	}
	refused("the old target kept where the rules change it",
		mineTestHeader(last.hash, last.time+60, last.bits, true), "states a target")
	refused("a hash that does not meet its target",
		mineTestHeader(last.hash, last.time+60, right, false), "does not carry the work")
	refused("a date before the blocks it follows",
		mineTestHeader(last.hash, last.time-600, right, true), "dated no later")
	refused("a date far in the future",
		mineTestHeader(last.hash, now.Unix()+3*60*60, right, true), "ahead of this computer's clock")
	refused("a header that follows nothing known",
		mineTestHeader([32]byte{1, 2, 3}, last.time+60, right, true), "do not connect")

	// The case all this exists for: a real block offered as standing at
	// another height. Blocks 817 and on, without 816, lead nowhere.
	longer := bitcoin.branchAt(815).grow(5, 60)
	chain := start()
	if added, err := chain.offer(longer.all()[17:], now); !errors.Is(err, errHeadersDoNotConnect) || added != 0 {
		t.Errorf("headers with one left out were taken: added %d, err %v", added, err)
	}
	// A good header followed by a bad one: the good one counts, the bad one
	// and everything after it does not.
	good := longer.at(816)
	goodHeader, _ := decodeBitcoinHeader(good)
	bad := mineTestHeader(goodHeader.hash, goodHeader.time+60, goodHeader.bits, false)
	if added, err := chain.offer([][]byte{good, bad, longer.at(818)}, now); err == nil || added != 1 || chain.tipHeight() != 816 {
		t.Errorf("a good header then a bad one: added %d, tip %d, err %v", added, chain.tipHeight(), err)
	}
}

// Where Bitcoin has two branches, the node keeps the one with more work.
func TestBitcoinBranchWithMoreWorkIsKept(t *testing.T) {
	bitcoin := useSmallBitcoin(t).grow(12, 600) // tip 812
	chain := followed(t)
	now := time.Now()
	if _, err := chain.offer(bitcoin.all()[1:], now); err != nil {
		t.Fatal(err)
	}
	// A branch from block 806 that is shorter than what is held: not taken.
	branch := bitcoin.branchAt(806).grow(4, 600) // tip 810
	if added, err := chain.offer(branch.all()[7:], now); err != nil || added != 0 {
		t.Fatalf("a shorter branch: added %d, err %v", added, err)
	}
	if header, _ := chain.headerAt(810); header != hex.EncodeToString(bitcoin.at(810)) {
		t.Fatal("a shorter branch replaced the chain")
	}
	// Grown past it, the branch is the chain, and block 810 is now its own.
	branch.grow(5, 600) // tip 815
	if added, err := chain.offer(branch.all()[7:], now); err != nil || added != 3 || chain.tipHeight() != 815 {
		t.Fatalf("a longer branch: added %d, tip %d, err %v", added, chain.tipHeight(), err)
	}
	if header, _ := chain.headerAt(810); header != hex.EncodeToString(branch.at(810)) {
		t.Error("the chain still holds the block Bitcoin dropped")
	}
	// The dropped blocks are forgotten: headers built on them lead nowhere.
	dropped, _ := decodeBitcoinHeader(bitcoin.at(812))
	orphan := mineTestHeader(dropped.hash, dropped.time+600, dropped.bits, true)
	if _, err := chain.offer([][]byte{orphan}, now); !errors.Is(err, errHeadersDoNotConnect) {
		t.Errorf("a header on a dropped block: %v", err)
	}
}

// --- stand-ins for the Bitcoin network and for an explorer -----------------

// serveTestBitcoin plays one computer on the Bitcoin network on this
// machine, holding whatever chain `holding` returns at the time.
func serveTestBitcoin(t *testing.T, holding func() *testBitcoin) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				peer := &bitcoinPeer{conn: conn}
				for {
					command, payload, err := peer.read()
					if err != nil {
						return
					}
					switch command {
					case "version":
						_ = peer.send("version", make([]byte, 86))
						_ = peer.send("verack", nil)
						_ = peer.send("ping", []byte{1, 2, 3, 4, 5, 6, 7, 8})
					case "getheaders":
						headers := holding().all()
						known := make(map[[32]byte]int)
						for position, raw := range headers {
							header, _ := decodeBitcoinHeader(raw)
							known[header.hash] = position
						}
						count, rest, _ := readCount(payload[4:])
						from := -1
						for i := uint64(0); i < count && from < 0; i++ {
							var hash [32]byte
							copy(hash[:], rest[i*32:])
							if position, ok := known[hash]; ok {
								from = position
							}
						}
						reply := new(bytes.Buffer)
						if from < 0 {
							writeCount(reply, 0)
						} else {
							following := headers[from+1:]
							writeCount(reply, len(following))
							for _, raw := range following {
								reply.Write(raw)
								reply.WriteByte(0)
							}
						}
						_ = peer.send("headers", reply.Bytes())
					}
				}
			}()
		}
	}()
	return listener.Addr().String()
}

// serveTestExplorer plays a block explorer holding the same kind of chain.
func serveTestExplorer(t *testing.T, holding func() *testBitcoin) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bitcoin := holding()
		if r.URL.Path == "/blocks/tip/height" {
			fmt.Fprint(w, bitcoin.tip())
			return
		}
		top, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/blocks/"), 10, 64)
		if err != nil || top > bitcoin.tip() || top < bitcoin.start {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var page []map[string]any
		for height := top; height > top-10 && height >= bitcoin.start; height-- {
			raw := bitcoin.at(height)
			var previous, merkle [32]byte
			copy(previous[:], raw[4:36])
			copy(merkle[:], raw[36:68])
			page = append(page, map[string]any{
				"height":            height,
				"version":           binary.LittleEndian.Uint32(raw[0:4]),
				"previousblockhash": reversedHex(previous),
				"merkle_root":       reversedHex(merkle),
				"timestamp":         binary.LittleEndian.Uint32(raw[68:72]),
				"bits":              binary.LittleEndian.Uint32(raw[72:76]),
				"nonce":             binary.LittleEndian.Uint32(raw[76:80]),
			})
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestHeadersComeFromTheBitcoinNetwork(t *testing.T) {
	bitcoin := useSmallBitcoin(t).grow(20, 60)
	address := serveTestBitcoin(t, func() *testBitcoin { return bitcoin })
	findBitcoinPeers = func() []string { return []string{address} }

	follower := newBitcoinFollower(900)
	if follower == nil {
		t.Fatal("no follower for a height after the starting block")
	}
	defer follower.done()
	if tip := follower.advance(); tip != 820 || follower.source() != "the Bitcoin network" {
		t.Fatalf("followed to %d from %q", tip, follower.source())
	}
	// Over the same connection, later: the new blocks and nothing else.
	bitcoin.grow(3, 60)
	if tip := follower.advance(); tip != 823 {
		t.Fatalf("after three more blocks: followed to %d", tip)
	}
	// Bitcoin drops the last two blocks for a longer branch: so does the node.
	branch := bitcoin.branchAt(821).grow(4, 60)
	bitcoin = branch
	if tip := follower.advance(); tip != 825 {
		t.Fatalf("after Bitcoin changed branch: followed to %d", tip)
	}
	if header, _ := follower.chain.headerAt(823); header != hex.EncodeToString(branch.at(823)) {
		t.Error("the node kept a block Bitcoin dropped")
	}
}

// A computer that sends headers which break the rules is not asked again,
// and block explorers fill in what is missing.
func TestExplorersFillInWhenTheBitcoinNetworkDoesNot(t *testing.T) {
	bitcoin := useSmallBitcoin(t).grow(40, 60)

	// This one holds the real blocks up to 810 and then one that does not
	// carry its work.
	lying := bitcoin.branchAt(810)
	last, _ := decodeBitcoinHeader(lying.at(810))
	lying.headers = append(lying.headers, mineTestHeader(last.hash, last.time+60, last.bits, false))
	address := serveTestBitcoin(t, func() *testBitcoin { return lying })
	findBitcoinPeers = func() []string { return []string{address} }
	anchorSources = []string{serveTestExplorer(t, func() *testBitcoin { return bitcoin })}

	follower := newBitcoinFollower(900)
	defer follower.done()
	if tip := follower.advance(); tip != 810 {
		t.Fatalf("from the lying computer: followed to %d, want 810 and no further", tip)
	}
	if len(follower.peers) != 0 {
		t.Error("a computer that sent a bad header is still being asked")
	}
	if tip := follower.advance(); tip != 840 || follower.source() != "block explorers" {
		t.Fatalf("with an explorer to ask: followed to %d from %q", tip, follower.source())
	}
	// With nobody on the Bitcoin network in reach at all, the explorers do.
	findBitcoinPeers = func() []string { return nil }
	alone := newBitcoinFollower(900)
	if tip := alone.advance(); tip != 840 {
		t.Fatalf("explorers alone: followed to %d", tip)
	}
	// And with nobody in reach of either kind, the node says so and waits.
	anchorSources = nil
	nobody := newBitcoinFollower(900)
	if tip := nobody.advance(); tip != 800 || nobody.source() != "" {
		t.Fatalf("nobody in reach: followed to %d from %q", tip, nobody.source())
	}
}

// The whole of it: a node started early counts Bitcoin's blocks up to its
// height, starts by itself when that block appears, and afterwards notices
// if Bitcoin replaces the block.
func TestNodeFollowsBitcoinToItsAnchorAndStarts(t *testing.T) {
	bitcoin := useSmallBitcoin(t).grow(10, 60) // tip 810
	var holding sync.Mutex
	current := bitcoin
	address := serveTestBitcoin(t, func() *testBitcoin {
		holding.Lock()
		defer holding.Unlock()
		return current
	})
	findBitcoinPeers = func() []string { return []string{address} }

	onNetwork(t, "devnet")
	Params.Name = "launch-test"
	Params.Anchor = GenesisAnchor{BitcoinHeight: 812}
	Params.GenesisTimestamp = 0
	t.Chdir(t.TempDir())

	started := make(chan struct{})
	go func() {
		waitForGenesisAnchor([]string{"cvn_node"}, 20*time.Millisecond, 20*time.Millisecond)
		close(started)
	}()
	select {
	case <-started:
		t.Fatal("the node started before Bitcoin reached the height")
	case <-time.After(400 * time.Millisecond):
	}
	// Block 811 is not block 812, however real it is.
	bitcoin.grow(1, 60)
	select {
	case <-started:
		t.Fatal("the node started on the block before the one it was waiting for")
	case <-time.After(400 * time.Millisecond):
	}
	bitcoin.grow(1, 60)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the node did not start when Bitcoin reached the height")
	}
	want := hex.EncodeToString(bitcoin.at(812))
	if Params.Anchor.BitcoinHeader != want {
		t.Fatal("the node started from a header that is not block 812")
	}
	if saved, err := os.ReadFile(genesisAnchorFileName()); err != nil || strings.TrimSpace(string(saved)) != want {
		t.Errorf("the header was not kept for the next start: %v", err)
	}

	// Not settled until six blocks follow.
	if settled, warning := anchorWarning(); settled || warning != "" {
		t.Fatalf("straight after the start: settled %v, warning %q", settled, warning)
	}
	bitcoin.grow(6, 60)
	if settled, warning := anchorWarning(); !settled || warning != "" {
		t.Fatalf("six blocks later: settled %v, warning %q", settled, warning)
	}
	// Bitcoin replaces block 812 with a longer branch: the node says so.
	replaced := bitcoin.branchAt(811).grow(12, 60)
	holding.Lock()
	current = replaced
	holding.Unlock()
	if settled, warning := anchorWarning(); settled || !strings.Contains(warning, "Bitcoin has replaced block 812") {
		t.Fatalf("after Bitcoin replaced the block: settled %v, warning %q", settled, warning)
	}
}
