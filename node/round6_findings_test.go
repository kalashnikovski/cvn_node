package node

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ROUND 6 FINDINGS
//
// These tests were written by a reviewer in the sixth review. Each failed on
// commit ed1c62f because of the fault it names, and passes now.

// FINDING 1. A pass that is downloading from an honest source sets that source
// aside for ten minutes when a rival of this node's newest block is adopted
// (the sibling rule in placeWireBlock) while the download is in flight.
//
// The node holds block 5 ("high"). The source holds the same block 5 and two
// more on top of it. While the node is downloading blocks 6 and 7, the other
// block 5 ("low", same parent, same work, lower identifier) arrives from a
// neighbour. placeWireBlock replaces the newest block with it. The pass then
// finds block 6 "not valid", the node's work has not grown, and the source,
// which did nothing wrong, is set aside.
func TestRound6HonestSourceIsSetAsideWhenARivalBlockIsAdoptedDuringTheDownload(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	buildTestChain(t, 4)

	// Two blocks at height 5 on the same parent, with the same date and so
	// the same difficulty: the lower identifier wins between them.
	date := Params.GenesisTimestamp + 100 + 5*45
	one := mineTestBlock(t, date, testAddress("miner-one"), nil, 0)
	two := mineTestBlock(t, date, testAddress("miner-two"), nil, 0)
	high, low := one, two
	if high.Hash < low.Hash {
		high, low = low, high
	}
	if !siblingWins(low, high) {
		t.Fatal("the two blocks are not rivals in the way this test needs")
	}

	// The node, and the source, hold "high". The source has two more.
	if !placeOnTip(high) {
		t.Fatal("block 5 was not placed")
	}
	extendTestChain(t, date+45, testAddress("miner"), nil)
	extendTestChain(t, date+90, testAddress("miner"), nil)
	sourceChain := localChain()
	StateMutex.Lock()
	rollbackChainToHeightLocked(5)
	StateMutex.Unlock()
	if tip := GetLatestBlockInternal(); tip.Index != 5 || tip.Hash != high.Hash {
		t.Fatal("the node is not at block 5")
	}

	peer := newStandInPeer(t, sourceChain)
	answer := peer.server.Config.Handler
	var arrived int32
	peer.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocks/batch" && atomic.CompareAndSwapInt32(&arrived, 0, 1) {
			// The rival block 5 arrives from a neighbour while the download
			// is in flight, by the path every block from a neighbour takes.
			ProcessIncomingWireBlockWithDelay(low)
		}
		answer.ServeHTTP(w, r)
	})
	source := peer.asOnlySeed(t)

	TriggerActiveReSyncPass()

	if atomic.LoadInt32(&arrived) != 1 {
		t.Fatal("the pass never asked for blocks")
	}
	tip := GetLatestBlockInternal()
	t.Logf("after the pass the node is at block #%d (%.12s); the source is at #7", tip.Index, tip.Hash)
	if syncSourceIsSetAside(source) {
		t.Errorf("the source sent only valid blocks of the chain it holds, and was set aside for %s; the node is left at block #%d with its only source refused", syncSourceCooldown, tip.Index)
	}
}

// FINDING 2. Messages that arrive on a connection this node opened (an
// uplink) have no allowance at all. On a connection opened to this node each
// address may send 50 messages at once and 20 a second. A node gets an uplink
// opened to it by sending one PEER_PING, and may then send announcements as
// fast as the line carries them: each one reads a block from the database,
// adds an entry to a table that is not pruned for a minute, writes a log
// line and sends a request.
func TestRound6AnUplinkHasNoMessageAllowance(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	buildTestChain(t, 3)
	freshBlockRelay(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	const sent = 5000
	var requests int64
	finished := make(chan time.Duration, 1)
	go func() {
		far, err := listener.Accept()
		if err != nil {
			return
		}
		defer far.Close()
		// Count what the node sends back.
		go func() {
			for {
				_ = far.SetReadDeadline(time.Now().Add(20 * time.Second))
				payload, _, err := readFrame(far)
				if err != nil {
					return
				}
				if strings.HasPrefix(string(payload), "BLOCK_REQUEST:") {
					atomic.AddInt64(&requests, 1)
				}
			}
		}()
		began := time.Now()
		for i := 0; i < sent; i++ {
			announcement, _ := json.Marshal(blockAnnouncement{Height: 3, Hash: fmt.Sprintf("%064x", i+1)})
			writeFrame(far, "BLOCK_ANNOUNCE:"+string(announcement))
		}
		finished <- time.Since(began)
		time.Sleep(15 * time.Second)
	}()

	go runRelayUplink(listener.Addr().String(), "this-node")

	var took time.Duration
	select {
	case took = <-finished:
	case <-time.After(60 * time.Second):
		t.Fatal("the announcements could not be sent")
	}
	// Give the node a moment to finish answering.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt64(&requests) < sent {
		time.Sleep(50 * time.Millisecond)
	}
	elapsed := took + 10*time.Second
	allowed := int64(50 + 20*elapsed.Seconds())
	blockRelay.Lock()
	held := len(blockRelay.fetching)
	blockRelay.Unlock()
	t.Logf("%d announcements sent in %s; the node asked for %d blocks and holds %d entries in its table of blocks asked for", sent, took.Round(time.Millisecond), atomic.LoadInt64(&requests), held)
	if got := atomic.LoadInt64(&requests); got > allowed {
		t.Errorf("the node acted on %d announcements from one address in under %s; on a connection opened to it the allowance would have let through at most %d", got, elapsed.Round(time.Second), allowed)
	}
}

// FINDING 3. A node that announced a block and never sends it can keep the
// receiver from ever asking the honest node that announced the same block.
//
// handleBlockAnnouncement asks the first announcer. Every later announcer
// waits out the two seconds and then tries to claim the fetch; exactly one
// wins and the others give up for good. The honest node announces once. A
// node that announces the same block many times (inside its allowance) holds
// nearly all the places in that draw, wins it, is asked again and again, and
// the honest node is never asked.
func TestRound6TheHonestAnnouncerIsNeverAsked(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	buildTestChain(t, 2)
	freshBlockRelay(t)

	silentNear, silentFar := connectedPair(t)
	honestNear, honestFar := connectedPair(t)
	go func() { // the silent node reads and never answers
		for {
			_ = silentFar.SetReadDeadline(time.Now().Add(30 * time.Second))
			if _, _, err := readFrame(silentFar); err != nil {
				return
			}
		}
	}()

	announcement, _ := json.Marshal(blockAnnouncement{Height: 3, Hash: testHash("c")})
	body := string(announcement)

	// The silent node is first, and is asked.
	handleBlockAnnouncement(silentNear, "10.0.0.66", body)
	// The honest node announces the same block a moment later.
	time.Sleep(100 * time.Millisecond)
	handleBlockAnnouncement(honestNear, "10.0.0.7", body)
	// The silent node goes on announcing it: 40 at once, then 5 a second, all
	// within the 50 at once and 20 a second its address is allowed.
	for i := 0; i < 40; i++ {
		handleBlockAnnouncement(silentNear, "10.0.0.66", body)
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(200 * time.Millisecond):
				handleBlockAnnouncement(silentNear, "10.0.0.66", body)
			}
		}
	}()

	// The block never comes. The honest node should be asked after two
	// seconds; it is given ten.
	if message, ok := readRelayFrame(t, honestFar, 10*time.Second); !ok || !strings.HasPrefix(message, "BLOCK_REQUEST:") {
		t.Errorf("ten seconds after the honest node announced a block this node does not hold, it has not been asked for it (received %q)", message)
	}
}

// FINDING 4. Two nodes behind one public address (one household, one office,
// one carrier's shared address) each keep a connection open to a seed, and
// the seed sends each block and transfer to only one of them.
//
// sendOverOpenConnections sends "once to each address". The earlier release
// sent down every subscribed connection.
func TestRound6TwoNodesBehindOneAddressAreBothSentABlock(t *testing.T) {
	onNetwork(t, "devnet")
	freshBlockRelay(t)
	firstNear, firstFar := connectedPair(t)
	secondNear, secondFar := connectedPair(t)
	// Two different nodes, seen by this node at the same address.
	registerRelaySubscriber(firstNear, "203.0.113.7")
	registerRelaySubscriber(secondNear, "203.0.113.7")

	BroadcastNewBlock(Block{Version: 1, Index: 9, Hash: testHash("d"), PrevHash: testHash("e"),
		Transactions: []Transaction{{ID: "reward"}}})

	got := 0
	for name, far := range map[string]net.Conn{"first": firstFar, "second": secondFar} {
		if message, ok := readRelayFrame(t, far, 3*time.Second); ok && strings.HasPrefix(message, "BLOCK_BROADCAST:") {
			got++
		} else {
			t.Logf("the %s node behind the shared address was sent nothing", name)
		}
	}
	if got != 2 {
		t.Errorf("two nodes behind one address subscribed for blocks; the block was sent to %d of them", got)
	}
}

// FINDING 5. A block dropped during a pass is forgotten when the pass that
// should fetch it would start within three seconds of the last such pass.
//
// catchUpAfterMissedBlock clears the note that a block was missed before it
// decides whether to run a pass. When it then declines (the last pass of
// this kind began under missedBlockPassInterval ago) nothing remembers the
// block: no pass runs now, and none runs when the three seconds are over.
// This is fault 1 of section 2 again, for the second block dropped in a row.
func TestRound6ASecondBlockMissedInARowIsForgotten(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	buildTestChain(t, 3)
	peer := newStandInPeer(t, localChain())
	peer.asOnlySeed(t)

	// A pass that followed a missed block began one second ago...
	atomic.StoreInt64(&lastPassAfterMiss, time.Now().Add(-time.Second).UnixNano())
	// ...and another block was dropped while it ran.
	atomic.StoreInt32(&blockMissedDuringPass, 1)
	asked := atomic.LoadInt32(&peer.requests)

	catchUpAfterMissedBlock() // what TriggerActiveReSyncPass does as it ends

	// Well past the three seconds: has anything gone to look for the block?
	time.Sleep(missedBlockPassInterval + 2*time.Second)
	if atomic.LoadInt32(&peer.requests) == asked && atomic.LoadInt32(&blockMissedDuringPass) == 0 {
		t.Errorf("a block was dropped during a pass; %s later no pass has asked any node for it, and the node no longer remembers that one was dropped", missedBlockPassInterval+2*time.Second)
	}
}

// FINDING 6. A block over the announcement size is sent whole, unasked, to
// every node that is called up afresh (up to eight for each block): the
// nodes this one knows of and has no open connection with. Section 2 of the
// changes file claims that among nodes of this version no block over 16,384
// bytes is sent without being asked for. That holds only while every node is
// connected to every other, as in the tests (six nodes, eight uplinks each).
func TestRound6ALargeBlockIsSentWholeToANodeWithNoOpenConnection(t *testing.T) {
	onNetwork(t, "devnet")
	freshBlockRelay(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	// A node this one has heard from, with no connection open either way.
	PeerMapMutex.Lock()
	saved := ActivePeerMap
	ActivePeerMap = map[string]time.Time{listener.Addr().String(): time.Now()}
	PeerMapMutex.Unlock()
	t.Cleanup(func() {
		PeerMapMutex.Lock()
		ActivePeerMap = saved
		PeerMapMutex.Unlock()
	})

	large := Block{Version: 1, Index: 9, Hash: testHash("f"), PrevHash: testHash("e"),
		Transactions: []Transaction{{ID: "reward", Witness: strings.Repeat("x", 3*announceBlocksOverBytes)}}}
	BroadcastNewBlock(large)

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		accepted <- conn
	}()
	select {
	case conn := <-accepted:
		if conn == nil {
			t.Fatal("the node was not called")
		}
		defer conn.Close()
		message, ok := readRelayFrame(t, conn, 5*time.Second)
		if !ok {
			t.Fatal("the node was called and sent nothing")
		}
		if strings.HasPrefix(message, "BLOCK_BROADCAST:") {
			t.Errorf("a block of %d bytes (announced above %d) was sent whole to a node that had not asked for it", len(message), announceBlocksOverBytes)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the node was not called")
	}
}

// FINDING 5, second path. A block that cannot be placed asks for a pass and
// uses up the thirty-second allowance for such passes even when no pass
// starts, because one is already running. If the running pass is still only
// comparing chains (so the block is not noted as missed) and ends without
// fetching that block, the next block that cannot be placed starts nothing
// either, for thirty seconds.
func TestRound6ARequestForAPassIsSpentWhenNoPassStarts(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	ours := buildTestChain(t, 3)
	// The only source is one block ahead of this node.
	extendTestChain(t, Params.GenesisTimestamp+100+4*45, testAddress("miner"), nil)
	theirs := localChain()
	StateMutex.Lock()
	rollbackChainToHeightLocked(int64(len(ours) - 1))
	StateMutex.Unlock()
	peer := newStandInPeer(t, theirs)
	peer.asOnlySeed(t)
	atomic.StoreInt64(&lastPassFromBlock, 0)

	// A pass is running and has not begun changing the chain.
	atomic.StoreInt32(&syncPassRunning, 1)
	requestSyncPassForBlock() // a block arrives that does not fit
	time.Sleep(200 * time.Millisecond)
	atomic.StoreInt32(&syncPassRunning, 0) // that pass ends, having fetched nothing
	catchUpAfterMissedBlock()              // as every pass does when it ends

	requestSyncPassForBlock() // the next block that does not fit, a moment later
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && GetLatestBlockInternal().Index < 4 {
		time.Sleep(50 * time.Millisecond)
	}
	if got := GetLatestBlockInternal().Index; got < 4 {
		t.Errorf("two blocks arrived that did not fit and no pass ran for either (%d requests reached the source); the node is still at block #%d and will not ask for %s", atomic.LoadInt32(&peer.requests), got, syncPassFromBlockInterval)
	}
}

// FINDING 7. A wrong password for the saved key is taken as a private key
// when it is made only of the characters 0-9 and a-f.
//
// signingKeyFrom, with a key saved on the computer, tries what was typed as
// the password; if that fails and what was typed "looks like hex" of up to
// 64 characters, it is used as a short private key. A password of digits
// with one digit wrong is not refused: the wallet goes on with a key whose
// value is that number. The menu entry "Show the 24 recovery words" then
// prints, with no warning, 24 words that are not those of the saved key, for
// the owner to write down as their backup; the key they stand for is one
// anybody can guess.
func TestRound6AWrongPasswordOfDigitsIsTakenAsAPrivateKey(t *testing.T) {
	oldFile, oldRounds := savedKeyFile, savedKeyRounds
	savedKeyFile, savedKeyRounds = t.TempDir()+"/wallet_key.json", 1000
	t.Cleanup(func() { savedKeyFile, savedKeyRounds = oldFile, oldRounds })

	realKey, _, realAddress, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saveKeyWithPassword(realKey, "19841206"); err != nil {
		t.Fatal(err)
	}
	// The right password opens the key.
	if opened, _, err := signingKeyFrom("19841206"); err != nil || !strings.EqualFold(strings.TrimLeft(opened, "0"), strings.TrimLeft(strings.TrimSpace(realKey), "0")) {
		t.Fatalf("the right password did not open the saved key: %v", err)
	}
	// One digit wrong.
	got, publicKey, err := signingKeyFrom("19841207")
	if err == nil {
		address, _ := DeriveAddressFromPublicKey(publicKey)
		words, _ := recoveryWordsFromKey(got)
		t.Errorf("a wrong password was not refused: the wallet goes on with private key %s (address %s, not the saved %s) and would show these as the recovery words: %.40s...",
			strings.TrimLeft(got, "0"), strings.TrimSpace(address), strings.TrimSpace(realAddress), words)
	}
}
