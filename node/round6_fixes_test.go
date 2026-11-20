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

// Tests for what was changed after the sixth review, beside the reviewer's
// own tests in round6_findings_test.go.

// A node called up afresh that understands announcements is sent the short
// form of a large block and the block itself only when it asks. One that says
// nothing back, as a node of an older version does, is sent the whole block.
func TestALargeBlockOnAFreshCallIsAnnouncedToANodeThatUnderstands(t *testing.T) {
	onNetwork(t, "devnet")
	freshBlockRelay(t)
	announced, _ := json.Marshal(blockAnnouncement{Height: 9, Hash: testHash("a")})
	const whole = "BLOCK_BROADCAST:the whole block"
	announcement := "BLOCK_ANNOUNCE:" + string(announced)
	rememberBlockPassedOn(testHash("a"), whole)

	listen := func() net.Listener {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { listener.Close() })
		return listener
	}

	// A node of this version.
	current := listen()
	go callAndSend(current.Addr().String(), whole, announcement)
	conn, err := current.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got, _ := readRelayFrame(t, conn, 3*time.Second); got != "RELAY_FEATURES:"+relayFeatureAnnounce {
		t.Fatalf("the call began with %q", got)
	}
	writeFrame(conn, "RELAY_FEATURES:"+relayFeatureAnnounce)
	if got, _ := readRelayFrame(t, conn, 3*time.Second); got != announcement {
		t.Fatalf("a node that understands announcements was sent %q", got)
	}
	writeFrame(conn, "BLOCK_REQUEST:"+string(announced))
	if got, _ := readRelayFrame(t, conn, 3*time.Second); got != whole {
		t.Fatalf("the block that was asked for came as %q", got)
	}

	// A node of an older version: it reads, and says nothing back.
	older := listen()
	began := time.Now()
	go callAndSend(older.Addr().String(), whole, announcement)
	quiet, err := older.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer quiet.Close()
	if got, _ := readRelayFrame(t, quiet, 3*time.Second); got != "RELAY_FEATURES:"+relayFeatureAnnounce {
		t.Fatalf("the call began with %q", got)
	}
	if got, _ := readRelayFrame(t, quiet, freshCallFeatureWait+3*time.Second); got != whole {
		t.Fatalf("a node that does not understand announcements was sent %q", got)
	} else if waited := time.Since(began); waited < freshCallFeatureWait-100*time.Millisecond {
		t.Errorf("the whole block was sent after only %v", waited)
	}

	// A transfer, or a block of ordinary size, is simply sent.
	plain := listen()
	go callAndSend(plain.Addr().String(), "TX_BROADCAST:a transfer", "")
	third, err := plain.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	if got, _ := readRelayFrame(t, third, 3*time.Second); got != "TX_BROADCAST:a transfer" {
		t.Errorf("a transfer on a fresh call came as %q", got)
	}
}

// One address cannot fill the table of blocks being fetched by announcing
// blocks that do not exist, and what it does announce does not stop another
// address being listened to.
func TestOneAddressCannotFillTheTableOfAnnouncedBlocks(t *testing.T) {
	onNetwork(t, "devnet")
	freshBlockRelay(t)
	savedEngine := GlobalBoltEngine
	GlobalBoltEngine = nil // this node holds no blocks
	t.Cleanup(func() { GlobalBoltEngine = savedEngine })

	near, far := connectedPair(t)
	var asked int64
	go func() {
		for {
			_ = far.SetReadDeadline(time.Now().Add(5 * time.Second))
			payload, _, err := readFrame(far)
			if err != nil {
				return
			}
			if strings.HasPrefix(string(payload), "BLOCK_REQUEST:") {
				atomic.AddInt64(&asked, 1)
			}
		}
	}()
	for i := 0; i < 500; i++ {
		body, _ := json.Marshal(blockAnnouncement{Height: 5, Hash: fmt.Sprintf("%064x", i+1)})
		handleBlockAnnouncement(near, "10.0.0.66", string(body))
	}
	time.Sleep(300 * time.Millisecond)
	if got := atomic.LoadInt64(&asked); got != blockFetchesPerAddress {
		t.Errorf("one address announced 500 blocks that do not exist and was asked for %d of them; the limit is %d", got, blockFetchesPerAddress)
	}
	blockRelay.Lock()
	held := len(blockRelay.fetching)
	blockRelay.Unlock()
	if held != blockFetchesPerAddress {
		t.Errorf("the table holds %d entries for that address", held)
	}

	honestNear, honestFar := connectedPair(t)
	body, _ := json.Marshal(blockAnnouncement{Height: 5, Hash: testHash("b")})
	handleBlockAnnouncement(honestNear, "10.0.0.7", string(body))
	if got, ok := readRelayFrame(t, honestFar, 2*time.Second); !ok || got != "BLOCK_REQUEST:"+string(body) {
		t.Errorf("another address announced a block and was not asked for it: %q", got)
	}
}

// A rival of the newest block is dealt with at once. It used to be kept
// waiting, for up to four seconds, when its parent had been fetched after an
// announcement a moment before.
func TestARivalOfTheNewestBlockIsNotKeptWaiting(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	buildTestChain(t, 3)
	freshBlockRelay(t)
	tip := GetLatestBlockInternal()

	blockRelay.Lock()
	blockRelay.fetching[tip.PrevHash] = &blockFetch{lastAsked: time.Now(), first: "10.0.0.1", heard: map[string]bool{"10.0.0.1": true}}
	blockRelay.Unlock()

	rival := Block{Version: 1, Index: tip.Index, PrevHash: tip.PrevHash, Hash: testHash("d")}
	if placeAfterItsParent(rival) {
		t.Error("a rival of the newest block was kept waiting for a parent this node already holds")
	}
}

// The node's own miner finds a block while a pass is fetching blocks from a
// source that is ahead. The blocks that arrive no longer fit. The source sent
// nothing wrong: it is not set aside, and the node ends on the chain with
// more work.
func TestABlockFoundDuringADownloadDoesNotBlameTheSource(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	buildTestChain(t, 5)
	date := GetLatestBlockInternal().Timestamp

	// A block 6 of this node's own, made now and placed later.
	own := mineTestBlock(t, date+50, testAddress("this-node"), nil, 0)

	// The source holds another block 6 and two more.
	extendTestChain(t, date+45, testAddress("miner"), nil)
	extendTestChain(t, date+90, testAddress("miner"), nil)
	extendTestChain(t, date+135, testAddress("miner"), nil)
	sourceChain := localChain()
	sourceTip := GetLatestBlockInternal()
	StateMutex.Lock()
	rollbackChainToHeightLocked(5)
	StateMutex.Unlock()

	peer := newStandInPeer(t, sourceChain)
	answer := peer.server.Config.Handler
	var placed int32
	peer.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocks/batch" && atomic.CompareAndSwapInt32(&placed, 0, 1) {
			if !placeOnTip(own) {
				t.Error("this node's own block 6 was not placed")
			}
		}
		answer.ServeHTTP(w, r)
	})
	source := peer.asOnlySeed(t)
	atomic.StoreInt64(&lastPassAfterMiss, 0)

	TriggerActiveReSyncPass()

	if atomic.LoadInt32(&placed) != 1 {
		t.Fatal("the pass never asked for blocks")
	}
	if syncSourceIsSetAside(source) {
		t.Fatal("the source was set aside, though it sent only valid blocks of its own chain")
	}
	// The pass that follows compares the chains as they now are, and takes
	// the source's, which has more work.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && GetLatestBlockInternal().Hash != sourceTip.Hash {
		time.Sleep(100 * time.Millisecond)
	}
	if tip := GetLatestBlockInternal(); tip.Hash != sourceTip.Hash {
		t.Errorf("the node is at block #%d (%.12s); the chain with more work ends at #%d (%.12s)", tip.Index, tip.Hash, sourceTip.Index, sourceTip.Hash)
	}
	if syncSourceIsSetAside(source) {
		t.Error("the source was set aside by the pass that followed")
	}
}
