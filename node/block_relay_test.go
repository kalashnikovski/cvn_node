package node

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// readRelayFrame takes one message off a connection, as a node would.
func readRelayFrame(t *testing.T, conn net.Conn, within time.Duration) (string, bool) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(within))
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", false
	}
	mac := make([]byte, 32)
	payload := make([]byte, binary.BigEndian.Uint32(header))
	if _, err := io.ReadFull(conn, mac); err != nil {
		return "", false
	}
	if _, err := io.ReadFull(conn, payload); err != nil {
		return "", false
	}
	if !VerifyPayloadHMAC(payload, mac) {
		t.Fatal("a message arrived that was not signed for this network")
	}
	return string(payload), true
}

// connectedPair gives two ends of a real connection on this machine.
func connectedPair(t *testing.T) (near, far net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		accepted <- conn
	}()
	near, err = net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	far = <-accepted
	t.Cleanup(func() { near.Close(); far.Close() })
	return near, far
}

func freshBlockRelay(t *testing.T) {
	t.Helper()
	reset := func() {
		blockRelay.Lock()
		blockRelay.recent, blockRelay.recentOrder = make(map[string]string), nil
		blockRelay.uplinks, blockRelay.announces = make(map[net.Conn]string), make(map[net.Conn]bool)
		blockRelay.fetching = make(map[string]*blockFetch)
		blockRelay.identities = make(map[net.Conn]string)
		blockRelay.Unlock()
		relaySubscribersMutex.Lock()
		relaySubscribers = make(map[net.Conn]string)
		relaySubscribersMutex.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func testHash(letter string) string { return strings.Repeat(letter, 64) }

func TestABlockIsPassedOnOnce(t *testing.T) {
	freshBlockRelay(t)
	if !rememberBlockPassedOn(testHash("a"), "the block") {
		t.Fatal("the first time a block is passed on was refused")
	}
	if rememberBlockPassedOn(testHash("a"), "the block") {
		t.Error("the same block was passed on a second time")
	}
	// Only the most recent are kept ready.
	for i := 0; i < recentBlocksKept; i++ {
		rememberBlockPassedOn(testHash("b")[:60]+strings.Repeat("0", 4-len(itoa(i)))+itoa(i), "another")
	}
	if _, kept := recentBlockPayload(testHash("a")); kept {
		t.Error("an old block is still kept ready after many newer ones")
	}
}

func itoa(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

// A large block is announced to a node that understands announcements, and
// sent whole to one that does not. Neither is sent it twice.
func TestLargeBlocksAreAnnouncedOnlyToNodesThatUnderstand(t *testing.T) {
	onNetwork(t, "devnet")
	freshBlockRelay(t)
	newNear, newFar := connectedPair(t)
	oldNear, oldFar := connectedPair(t)
	twiceNear, twiceFar := connectedPair(t)

	registerRelaySubscriber(newNear, "10.0.0.1")
	noteConnectionAnnounces(newNear)
	registerRelaySubscriber(oldNear, "10.0.0.2")
	// A second connection with the node at 10.0.0.2, this one opened by us.
	registerUplinkConnection(twiceNear, "10.0.0.2")
	// The node there gave the same name on both, so they are known to be
	// two connections with one node.
	noteRelayIdentity(oldNear, "the-node-at-2")
	noteRelayIdentity(twiceNear, "the-node-at-2")

	reached := sendOverOpenConnections("BLOCK_BROADCAST:the whole block", "BLOCK_ANNOUNCE:the short form")
	if len(reached) != 2 || !reached["10.0.0.1"] || !reached["10.0.0.2"] {
		t.Fatalf("addresses reached: %v", reached)
	}
	if got, ok := readRelayFrame(t, newFar, 2*time.Second); !ok || got != "BLOCK_ANNOUNCE:the short form" {
		t.Errorf("the node that understands announcements was sent %q", got)
	}
	gotOld, okOld := readRelayFrame(t, oldFar, 500*time.Millisecond)
	gotTwice, okTwice := readRelayFrame(t, twiceFar, 500*time.Millisecond)
	if okOld == okTwice {
		t.Fatalf("the node with two connections should be sent the block on exactly one: %v %q, %v %q", okOld, gotOld, okTwice, gotTwice)
	}
	if got := gotOld + gotTwice; got != "BLOCK_BROADCAST:the whole block" {
		t.Errorf("the node that does not understand announcements was sent %q", got)
	}

	// A transfer, and a block of ordinary size, have no short form: everyone
	// is sent the thing itself.
	sendOverOpenConnections("TX_BROADCAST:a transfer", "")
	if got, _ := readRelayFrame(t, newFar, 2*time.Second); got != "TX_BROADCAST:a transfer" {
		t.Errorf("a transfer reached the announcing node as %q", got)
	}
	// A closed connection is forgotten.
	forgetRelayConnection(twiceNear)
	unregisterRelaySubscriber(oldNear)
	if reached := sendOverOpenConnections("x", ""); len(reached) != 1 {
		t.Errorf("after two connections closed, addresses reached: %v", reached)
	}
}

func TestAnAnnouncedBlockIsAskedForAndHandedOver(t *testing.T) {
	onNetwork(t, "devnet")
	freshBlockRelay(t)
	savedEngine := GlobalBoltEngine
	GlobalBoltEngine = nil // this node holds no blocks
	t.Cleanup(func() { GlobalBoltEngine = savedEngine })

	near, far := connectedPair(t)
	announcement, _ := json.Marshal(blockAnnouncement{Height: 7, Hash: testHash("c")})

	// Not held: asked for, by its height and identifier.
	handleBlockAnnouncement(near, "10.0.0.1", string(announcement))
	if got, ok := readRelayFrame(t, far, 2*time.Second); !ok || got != "BLOCK_REQUEST:"+string(announcement) {
		t.Fatalf("an announced block that is not held should be asked for; sent %q", got)
	}
	// Announced again straight away by someone else: not asked for twice at
	// once, but asked for from them if it has still not come after a moment.
	second, secondFar := connectedPair(t)
	began := time.Now()
	handleBlockAnnouncement(second, "10.0.0.2", string(announcement))
	if _, ok := readRelayFrame(t, secondFar, 300*time.Millisecond); ok {
		t.Fatal("the same block was asked for from two nodes at once")
	}
	if got, ok := readRelayFrame(t, secondFar, blockFetchPatience+2*time.Second); !ok || got != "BLOCK_REQUEST:"+string(announcement) {
		t.Fatalf("a block that did not come should be asked for from the next node that announced it; sent %q", got)
	} else if waited := time.Since(began); waited < blockFetchPatience-200*time.Millisecond {
		t.Errorf("the second node was asked after only %v", waited)
	}

	// The other side of it: a node hands over a block it passed on lately,
	// and says nothing about one it does not have ready.
	rememberBlockPassedOn(testHash("c"), "BLOCK_BROADCAST:block seven")
	handleBlockRequest(near, string(announcement))
	if got, ok := readRelayFrame(t, far, 2*time.Second); !ok || got != "BLOCK_BROADCAST:block seven" {
		t.Fatalf("a request for a recent block was answered with %q", got)
	}
	unknown, _ := json.Marshal(blockAnnouncement{Height: 8, Hash: testHash("d")})
	handleBlockRequest(near, string(unknown))
	if got, ok := readRelayFrame(t, far, 300*time.Millisecond); ok {
		t.Errorf("a request for a block not kept ready was answered with %q", got)
	}
	// Once held, an announcement of it is not answered at all.
	handleBlockAnnouncement(near, "10.0.0.1", string(announcement))
	if got, ok := readRelayFrame(t, far, 300*time.Millisecond); ok {
		t.Errorf("a block already held was asked for again: %q", got)
	}
	// Nonsense is let pass.
	for _, junk := range []string{"", "{}", `{"height":3,"hash":"short"}`, `{"height":-1,"hash":"` + testHash("e") + `"}`, "not json"} {
		handleBlockAnnouncement(near, "10.0.0.1", junk)
		handleBlockRequest(near, junk)
	}
	if got, ok := readRelayFrame(t, far, 300*time.Millisecond); ok {
		t.Errorf("a malformed announcement or request was answered: %q", got)
	}
}

// A node that has just been sent a burst of transfers still hears about the
// next block. Transfers and blocks used to share one allowance per address,
// and a burst of transfers used it up.
func TestABurstOfTransfersDoesNotCrowdOutABlock(t *testing.T) {
	onNetwork(t, "devnet")
	freshBlockRelay(t)
	savedEngine := GlobalBoltEngine
	GlobalBoltEngine = nil
	t.Cleanup(func() { GlobalBoltEngine = savedEngine })

	near, far := connectedPair(t)
	go serveInboundConnection(far, false)

	// Far more transfers than one address is allowed in a burst. They are
	// not valid transfers, which costs the sender nothing here.
	for i := 0; i < 300; i++ {
		writeFrame(near, `TX_BROADCAST:{"tx_id":"none"}`)
	}
	writeFrame(near, "RELAY_FEATURES:"+relayFeatureAnnounce)
	if got, ok := readRelayFrame(t, near, 3*time.Second); !ok || got != "RELAY_FEATURES:"+relayFeatureAnnounce {
		t.Fatalf("after a burst of transfers the node no longer answered: %q", got)
	}
	announcement, _ := json.Marshal(blockAnnouncement{Height: 9, Hash: testHash("f")})
	writeFrame(near, "BLOCK_ANNOUNCE:"+string(announcement))
	if got, ok := readRelayFrame(t, near, 3*time.Second); !ok || got != "BLOCK_REQUEST:"+string(announcement) {
		t.Fatalf("after a burst of transfers a block announcement went unheard: %q", got)
	}
	// Saying it again brings no second answer.
	writeFrame(near, "RELAY_FEATURES:"+relayFeatureAnnounce)
	if got, ok := readRelayFrame(t, near, 300*time.Millisecond); ok {
		t.Errorf("the features line was answered a second time: %q", got)
	}
}
