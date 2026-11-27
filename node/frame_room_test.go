package node

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Large messages from other nodes share a fixed amount of room while they are
// held (frameRoom, in gossip_mesh.go). These tests are of the room itself, of
// reading a message into it, and of a node serving many connections that all
// send large messages at once.

func TestARoomGivesWhatItHasAndNoMore(t *testing.T) {
	room := &frameRoom{most: 100}
	if !room.take(60) || room.held != 60 {
		t.Fatalf("taking 60 of 100 gave held %d, want 60", room.held)
	}
	if !room.take(40) || room.held != 100 {
		t.Fatalf("taking the other 40 gave held %d, want 100", room.held)
	}
	// Beyond the room: refused, and nothing changes.
	if room.take(1) || room.held != 100 {
		t.Errorf("taking 1 more than the room held gave held %d, want it refused and 100", room.held)
	}
	room.give(30)
	if room.held != 70 {
		t.Errorf("giving back 30 left %d held, want 70", room.held)
	}
	if room.take(31) || room.held != 70 {
		t.Errorf("taking 31 with 30 free gave held %d, want it refused and 70", room.held)
	}
	if !room.take(30) || room.held != 100 {
		t.Errorf("taking the 30 that were free gave held %d, want 100", room.held)
	}
	// Giving back more than is held leaves nought, not less.
	room.give(1000)
	if room.held != 0 {
		t.Errorf("giving back more than was held left %d, want 0", room.held)
	}
	// Nothing is given back for nothing, or for a negative amount.
	room.take(10)
	room.give(0)
	room.give(-5)
	if room.held != 10 {
		t.Errorf("giving 0 and -5 left %d held, want 10", room.held)
	}
	// A negative size is refused and changes nothing.
	if room.take(-1) || room.held != 10 {
		t.Errorf("taking -1 gave held %d, want it refused and 10", room.held)
	}
}

// sendFramesOn writes the messages one after the other, in a goroutine of
// their own, and closes the connection after the last unless told not to.
func sendFramesOn(conn net.Conn, closeAfter bool, messages ...string) {
	go func() {
		for _, message := range messages {
			writeFrame(conn, message)
		}
		if closeAfter {
			conn.Close()
		}
	}()
}

func TestALargeMessageIsReadWholeWhenThereIsRoomAndTheRoomIsGivenBack(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	large := strings.Repeat("a", frameCountedOverBytes+1)
	sendFramesOn(client, false, large)

	room := &frameRoom{most: 1 << 20}
	_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
	payload, signature, held, err := readFrameWithin(server, room)
	if err != nil {
		t.Fatalf("a large message with room for it gave: %v", err)
	}
	if string(payload) != large || !VerifyPayloadHMAC(payload, signature) {
		t.Error("the large message was not returned whole and signed")
	}
	if held != int64(len(large)) || room.held != int64(len(large)) {
		t.Errorf("the message took %d, the room holds %d; want both %d", held, room.held, len(large))
	}
	room.give(held)
	if room.held != 0 {
		t.Errorf("after giving back, the room holds %d, want 0", room.held)
	}
}

func TestAMessageNotOverTheCountedSizeNeedsNoRoom(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	small := strings.Repeat("b", frameCountedOverBytes) // exactly the size that is not counted
	sendFramesOn(client, false, small)

	room := &frameRoom{most: 0}
	_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
	payload, _, held, err := readFrameWithin(server, room)
	if err != nil || string(payload) != small {
		t.Fatalf("a message of %d bytes into a room of nought gave %d bytes and %v", len(small), len(payload), err)
	}
	if held != 0 || room.held != 0 {
		t.Errorf("an uncounted message took %d (the room holds %d), want 0", held, room.held)
	}
}

func TestALargeMessageWithNoRoomIsTakenOffTheWireAndTheNextOneIsRead(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	large := strings.Repeat("c", 3*frameCountedOverBytes)
	sendFramesOn(client, false, large, "PEER_PING:next")

	room := &frameRoom{most: int64(len(large)) - 1}
	room.held = 7 // something else is held already
	_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
	payload, _, held, err := readFrameWithin(server, room)
	if err != errNoRoomForFrame || payload != nil || held != 0 {
		t.Fatalf("a large message with too little room gave %d bytes, held %d, error %v; want errNoRoomForFrame", len(payload), held, err)
	}
	if room.held != 7 {
		t.Errorf("the room holds %d after a message was refused, want the 7 it held before", room.held)
	}
	// The large one was taken off the wire, so the next is read from its start.
	payload, signature, _, err := readFrameWithin(server, room)
	if err != nil || string(payload) != "PEER_PING:next" || !VerifyPayloadHMAC(payload, signature) {
		t.Fatalf("the message after the refused one gave %q and %v", payload, err)
	}
}

func TestALargeMessageCutShortGivesBackItsRoom(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	length := 4 * frameCountedOverBytes
	payload := bytes.Repeat([]byte("d"), length)
	go func() {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], uint32(length))
		frame := append(header[:], ComputePayloadHMAC(payload)...)
		frame = append(frame, payload[:length/2]...)
		_, _ = client.Write(frame)
		client.Close() // gone after half of it
	}()

	room := &frameRoom{most: 1 << 20}
	room.held = 5
	_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, _, held, err := readFrameWithin(server, room)
	if err == nil {
		t.Fatalf("a message cut short was returned: %d bytes", len(got))
	}
	if held != 0 || room.held != 5 {
		t.Errorf("after a message cut short held is %d and the room holds %d; want 0 and the 5 it held before", held, room.held)
	}
}

func TestAMessageReadWithNoRoomGivenIsReadWhateverItsSize(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	large := strings.Repeat("e", 5*frameCountedOverBytes)
	sendFramesOn(client, false, large)
	_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
	payload, signature, err := readFrame(server)
	if err != nil || string(payload) != large || !VerifyPayloadHMAC(payload, signature) {
		t.Fatalf("readFrame of a large message gave %d bytes and %v", len(payload), err)
	}
}

// Many connections, each sending a message just over a megabyte, at a node
// that has room for two of them. A message takes room as its bytes arrive. A
// message that gets all it needs is held while the node waits its turn to
// decode it (largeBlockDecodes); the rest are taken off their connections and
// not kept. At no moment may more be held than the room allows, every message
// is either kept or refused, and the room is empty again when the connections
// are gone.
func TestConnectionsSendingLargeMessagesTogetherHoldNoMoreThanTheRoom(t *testing.T) {
	startTestChain(t, "devnet")
	Params.SeedHosts = nil // this machine's address is a stranger's, whatever the seeds are

	const connections = 12
	messageBytes := 1<<20 + 100
	roomBytes := int64(3 << 20) // two messages fit, a third does not
	savedRoom := strangersFrameRoom
	room := &frameRoom{most: roomBytes}
	strangersFrameRoom = room
	// Every place to decode a large block is taken, so each message that
	// is kept stays held.
	for i := 0; i < cap(largeBlockDecodes); i++ {
		largeBlockDecodes <- struct{}{}
	}
	released := false
	releaseDecodes := func() {
		if !released {
			released = true
			// Exactly the places this test took. A waiting handler puts its own
			// in as soon as one is free and takes it out when it is done, so
			// emptying the channel until it looks empty would take theirs.
			for i := 0; i < cap(largeBlockDecodes); i++ {
				<-largeBlockDecodes
			}
		}
	}
	// The allowance and the count of malformed messages are kept for this
	// machine's address, which all the connections share.
	forgetThisMachine := func() {
		IPTokensMutex.Lock()
		delete(IPTokensMap, "127.0.0.1")
		delete(IPTokensMap, "transfers|127.0.0.1")
		IPTokensMutex.Unlock()
		IPRegistryMutex.Lock()
		delete(IPMalformedTrack, "127.0.0.1")
		IPRegistryMutex.Unlock()
		BlacklistMutex.Lock()
		delete(MaliciousIPBlacklist, "127.0.0.1")
		BlacklistMutex.Unlock()
		forgetPeersOnThisMachine()
	}
	forgetThisMachine()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var handlers sync.WaitGroup
	var clients []net.Conn
	// Run last to first: the decodes are let go, then the clients close and the
	// handlers end, and only then is everything put back.
	t.Cleanup(func() {
		strangersFrameRoom = savedRoom
		forgetThisMachine()
	})
	t.Cleanup(handlers.Wait)
	t.Cleanup(func() {
		for _, client := range clients {
			client.Close()
		}
	})
	t.Cleanup(releaseDecodes)
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				serveInboundConnection(conn, false)
			}()
		}
	}()

	message := "BLOCK_BROADCAST:" + `{"x":"` + strings.Repeat("a", messageBytes-len(`BLOCK_BROADCAST:{"x":""}`)) + `"}`
	if len(message) != messageBytes {
		t.Fatalf("set-up: the message is %d bytes, want %d", len(message), messageBytes)
	}
	if 3*int64(messageBytes) <= roomBytes {
		t.Fatal("set-up: three messages fit in the room")
	}
	notKeptBefore := atomic.LoadInt64(&framesWithoutRoom)
	var sends sync.WaitGroup
	for i := 0; i < connections; i++ {
		client, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
		sends.Add(1)
		go func() {
			defer sends.Done()
			writeFrame(client, message)
		}()
	}
	sendsDone := make(chan struct{})
	go func() {
		sends.Wait()
		close(sendsDone)
	}()

	// A message is taken in pieces, so several of them can each take part of
	// the room and then all be refused: how many are kept is not fixed. What
	// is fixed is that every message is kept or refused, that at most two
	// fit, and that what is held is what the kept ones hold. So look at the
	// room again and again, and once every send has finished and the count
	// of refusals has stayed the same over a few looks, work out how many
	// were kept.
	look := func() (held, refused int64) {
		room.Lock()
		held = room.held
		room.Unlock()
		return held, atomic.LoadInt64(&framesWithoutRoom) - notKeptBefore
	}
	deadline := time.Now().Add(15 * time.Second)
	mostSeen := int64(0)
	steady, lastRefused := 0, int64(-1)
	sendsFinished := false
	for time.Now().Before(deadline) {
		held, refused := look()
		if held > mostSeen {
			mostSeen = held
		}
		select {
		case <-sendsDone:
			sendsFinished = true
		default:
		}
		if sendsFinished && refused == lastRefused && held == (connections-refused)*int64(messageBytes) {
			steady++
		} else {
			steady = 0
		}
		lastRefused = refused
		if steady >= 10 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if mostSeen > roomBytes {
		t.Errorf("the room held %d bytes at one moment, and it has %d", mostSeen, roomBytes)
	}
	heldNow, refused := look()
	if !sendsFinished {
		t.Errorf("the %d messages were not all sent within the time allowed", connections)
	}
	kept := connections - refused
	if refused < connections-2 || refused > connections {
		t.Errorf("%d of %d messages were refused room, want at least %d (room for two)", refused, connections, connections-2)
	}
	if heldNow != kept*int64(messageBytes) {
		t.Errorf("%d messages were kept (%d of %d refused) so the room should hold %d, and while they wait to be decoded it holds %d", kept, refused, connections, kept*int64(messageBytes), heldNow)
	}

	// The decodes go ahead and the connections end: nothing stays held.
	releaseDecodes()
	for _, client := range clients {
		client.Close()
	}
	for time.Now().Before(deadline.Add(5 * time.Second)) {
		heldNow, _ = look()
		if heldNow == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if heldNow != 0 {
		t.Errorf("after every connection ended the room still holds %d bytes", heldNow)
	}
}

// A stranger can state a large size in the first four bytes and then send
// little. The room is taken for what has arrived, not for what was stated.
func TestAStrangerWhoStatesALargeSizeAndSendsLittleHoldsOnlyWhatItSent(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	room := &frameRoom{most: 4 << 20}
	type result struct {
		payload []byte
		held    int64
		err     error
	}
	done := make(chan result, 1)
	go func() {
		_ = server.SetReadDeadline(time.Now().Add(20 * time.Second))
		payload, _, held, err := readFrameWithin(server, room)
		done <- result{payload, held, err}
	}()
	heldNow := func() int64 {
		room.Lock()
		defer room.Unlock()
		return room.held
	}

	// The header says 1 MiB and the signature is there. Nothing is held yet.
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], 1<<20)
	if _, err := client.Write(append(header[:], make([]byte, 32)...)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if held := heldNow(); held != 0 {
			t.Fatalf("with a size stated and nothing sent the room holds %d, want 0", held)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Then 40,000 bytes of the payload, and no more.
	if _, err := client.Write(make([]byte, 40000)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for heldNow() != 40000 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// It stays at what was sent, and the reader is still waiting for the rest.
	for i := 0; i < 20; i++ {
		if held := heldNow(); held != 40000 {
			t.Fatalf("after 40000 bytes were sent the room holds %d, want 40000", held)
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case got := <-done:
		t.Fatalf("the reader returned (%d bytes, %v) before the rest was sent", len(got.payload), got.err)
	default:
	}

	// The sender goes: the reader fails and gives everything back.
	client.Close()
	select {
	case got := <-done:
		if got.err == nil || got.payload != nil || got.held != 0 {
			t.Errorf("a message that was never finished gave %d bytes, held %d, error %v; want an error and nothing held", len(got.payload), got.held, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the reader did not return after the sender closed")
	}
	if held := heldNow(); held != 0 {
		t.Errorf("after the sender closed the room holds %d, want 0", held)
	}
}

// forgetTheLoopbackStranger clears what a node keeps for the address that
// tests connect from.
func forgetTheLoopbackStranger() {
	IPTokensMutex.Lock()
	delete(IPTokensMap, "127.0.0.1")
	delete(IPTokensMap, "transfers|127.0.0.1")
	IPTokensMutex.Unlock()
	IPRegistryMutex.Lock()
	delete(IPMalformedTrack, "127.0.0.1")
	IPRegistryMutex.Unlock()
	BlacklistMutex.Lock()
	delete(MaliciousIPBlacklist, "127.0.0.1")
	BlacklistMutex.Unlock()
	forgetPeersOnThisMachine()
}

// forgetPeersOnThisMachine takes this machine's address off the list of
// peers. A connection that sends a ping is listed as a peer, and a peer left
// on the list would be called up by the blocks of every later test.
func forgetPeersOnThisMachine() {
	PeerMapMutex.Lock()
	for peer := range ActivePeerMap {
		if strings.HasPrefix(peer, "127.0.0.1:") {
			delete(ActivePeerMap, peer)
		}
	}
	PeerMapMutex.Unlock()
}

func TestAConnectionWhoseLargeMessagesKeepFindingNoRoomIsClosed(t *testing.T) {
	startTestChain(t, "devnet")
	Params.SeedHosts = nil // this machine's address is a stranger's, whatever the seeds are
	savedRoom := strangersFrameRoom
	strangersFrameRoom = &frameRoom{most: 20 << 10} // less than one piece of a message
	forgetTheLoopbackStranger()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var handlers sync.WaitGroup
	var clients []net.Conn
	// Run last to first: the clients close and the handlers end, and only then
	// is everything put back.
	t.Cleanup(func() {
		strangersFrameRoom = savedRoom
		forgetTheLoopbackStranger()
	})
	t.Cleanup(handlers.Wait)
	t.Cleanup(func() {
		for _, client := range clients {
			client.Close()
		}
	})
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				serveInboundConnection(conn, false)
			}()
		}
	}()

	// About 100 KiB, which is a large message and more than the room has.
	message := "BLOCK_BROADCAST:" + `{"x":"` + strings.Repeat("a", 100<<10) + `"}`
	connect := func() net.Conn {
		client, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
		return client
	}

	// One message fewer than the limit: refused room each time, and the
	// connection is still open and answers.
	before := atomic.LoadInt64(&framesWithoutRoom)
	client := connect()
	for i := 0; i < framesWithoutRoomAtMost-1; i++ {
		writeFrame(client, message)
	}
	writeFrame(client, "PEER_PING:DISCOVERY_REQUEST")
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	answer, _, err := readFrame(client)
	if err != nil || !strings.HasPrefix(string(answer), "PEER_LIST:") {
		t.Fatalf("after %d messages without room the connection gave %q and %v; want it open, answering with a peer list", framesWithoutRoomAtMost-1, answer, err)
	}
	if refused := atomic.LoadInt64(&framesWithoutRoom) - before; refused != framesWithoutRoomAtMost-1 {
		t.Errorf("%d messages were refused room, want %d", refused, framesWithoutRoomAtMost-1)
	}

	// The last one that is allowed: the server closes the connection.
	before = atomic.LoadInt64(&framesWithoutRoom)
	client = connect()
	for i := 0; i < framesWithoutRoomAtMost; i++ {
		writeFrame(client, message)
	}
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	if got, _, err := readFrame(client); err == nil {
		t.Errorf("after %d messages without room the connection gave %q, want it closed", framesWithoutRoomAtMost, got)
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Errorf("after %d messages without room the connection was still open after 5 seconds", framesWithoutRoomAtMost)
	}
	if refused := atomic.LoadInt64(&framesWithoutRoom) - before; refused != framesWithoutRoomAtMost {
		t.Errorf("%d messages were refused room, want %d", refused, framesWithoutRoomAtMost)
	}
}

func TestAnAnswerThatMustBeShortIsNotReadWhenItIsLong(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ten := "PEER_LIST:" + strings.Repeat("a", 10<<10)
	sixty := "PEER_LIST:" + strings.Repeat("b", 60<<10)
	long := "PEER_LIST:" + strings.Repeat("c", 200<<10)
	sendFramesOn(client, false, ten, sixty, long, "PEER_PING:next")
	_ = server.SetReadDeadline(time.Now().Add(10 * time.Second))

	for _, want := range []string{ten, sixty} {
		payload, signature, err := readSmallFrame(server)
		if err != nil || string(payload) != want || !VerifyPayloadHMAC(payload, signature) {
			t.Fatalf("a short answer of %d bytes gave %d bytes and %v; want it whole", len(want), len(payload), err)
		}
	}
	if payload, _, err := readSmallFrame(server); err == nil || payload != nil {
		t.Fatalf("an answer of %d bytes gave %d bytes and %v; want an error and nothing kept", len(long), len(payload), err)
	}
	payload, signature, err := readSmallFrame(server)
	if err != nil || string(payload) != "PEER_PING:next" || !VerifyPayloadHMAC(payload, signature) {
		t.Fatalf("the message after the long answer gave %q and %v; want it read from its start", payload, err)
	}
}

// A seed's large message has room of its own, so it is read even while the
// room for strangers is full, and a stranger's is turned away.
func TestASeedsLargeMessageHasRoomOfItsOwnWhileTheRoomForStrangersIsFull(t *testing.T) {
	savedParams := Params
	savedStrangers := strangersFrameRoom
	savedSeeds := seedsFrameRoom
	t.Cleanup(func() {
		Params = savedParams
		strangersFrameRoom = savedStrangers
		seedsFrameRoom = savedSeeds
	})

	Params.SeedHosts = []string{"198.51.100.10"}
	strangersFrameRoom = &frameRoom{most: 64 << 10}
	seedsFrameRoom = &frameRoom{most: 64 << 10}

	// Fill the strangers' room completely.
	if !frameRoomFor("203.0.113.5").take(64 << 10) {
		t.Fatal("failed filling strangers room")
	}

	buildFrameConn := func(payload string) net.Conn {
		var buf bytes.Buffer
		writeFrame(&buf, payload)
		c1, c2 := net.Pipe()
		go func() {
			_, _ = c1.Write(buf.Bytes())
			_ = c1.Close()
		}()
		return c2
	}

	largePayload := strings.Repeat("x", frameCountedOverBytes+1024)

	// A stranger's large frame finds no room and is discarded.
	strangerConn := buildFrameConn(largePayload)
	defer strangerConn.Close()
	if _, _, held, err := readFrameWithin(strangerConn, frameRoomFor("203.0.113.5")); err != errNoRoomForFrame || held != 0 {
		t.Fatalf("stranger large frame when strangers room full: got held=%d err=%v, want errNoRoomForFrame", held, err)
	}

	// A seed's large frame draws on seedsFrameRoom and succeeds even while strangersFrameRoom is full.
	seedConn := buildFrameConn(largePayload)
	defer seedConn.Close()
	seedRoom := frameRoomFor("198.51.100.10")
	gotPayload, _, held, err := readFrameWithin(seedConn, seedRoom)
	if err != nil || len(gotPayload) != len(largePayload) || held != int64(len(largePayload)) {
		t.Fatalf("seed large frame failed while strangers room was full: len=%d held=%d err=%v", len(gotPayload), held, err)
	}
	seedRoom.give(held)
}
