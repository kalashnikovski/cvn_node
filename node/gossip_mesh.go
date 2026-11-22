package node

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	mathrand "math/rand"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// MaxPeerMapCapacity is how many other nodes a node keeps track of.
	MaxPeerMapCapacity = 200
	// MaxPayloadFrameSize is the largest message a node will read: 2 MB,
	// several times the largest block.
	MaxPayloadFrameSize = 2048 * 1024
	// DefaultReadTimeout is how long an inbound connection may say nothing
	// before it is closed.
	DefaultReadTimeout = 30 * time.Second
)

var (
	ActivePeerMap = make(map[string]time.Time)
	PeerMapMutex  sync.RWMutex
	// MaxMeshConnectionPool is how many connections from nodes that are not
	// seeds are served at once. Most are connections that stay open to
	// receive blocks, and an open connection that only hears announcements
	// costs little.
	MaxMeshConnectionPool = make(chan struct{}, 600)

	// Connections open from each address, addresses refused for a while,
	// and how many malformed messages each address has sent.
	IPConnectionRegistry = make(map[string]int)
	IPRegistryMutex      sync.Mutex
	MaliciousIPBlacklist = make(map[string]time.Time)
	BlacklistMutex       sync.Mutex
	IPMalformedTrack     = make(map[string]int)
)

// TokenBucket is an allowance of messages for one address: so many at once,
// refilled at a steady rate. A message beyond the allowance is dropped.
type TokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
	sync.Mutex
}

var IPTokensMap = make(map[string]*TokenBucket)
var IPTokensMutex sync.Mutex

func GetIPRateLimiter(ip string) *TokenBucket {
	IPTokensMutex.Lock()
	defer IPTokensMutex.Unlock()
	limiter, exists := IPTokensMap[ip]
	if !exists {
		limiter = &TokenBucket{
			tokens:     50,
			maxTokens:  50,
			refillRate: 20,
			lastRefill: time.Now(),
		}
		IPTokensMap[ip] = limiter
	}
	return limiter
}

func (tb *TokenBucket) Allow() bool {
	tb.Lock()
	defer tb.Unlock()
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.lastRefill = now
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

func StartMeshNetwork() {
	fmt.Println("====================================================================")
	fmt.Printf("📡 Listening for other nodes on port %d...\n", Params.GossipPort)
	fmt.Println("====================================================================")

	listener8080, err := net.Listen("tcp", listenAddress(Params.GossipPort))
	if err != nil {
		// Without this port the node has no connections at all: it neither
		// hears of blocks nor passes its own on. It used to carry on like
		// that, showing "Running" and mining blocks nobody would ever see.
		fmt.Printf("\n🚨 This node cannot use port %d: %v\n", Params.GossipPort, err)
		fmt.Println("   Another copy of the node is probably running on this computer, or another program holds the port.")
		fmt.Println("   Close it and start again. The node has stopped; nothing was changed.")
		log.Printf("🚨 Could not listen for other nodes on port %d: %v. Stopping.\n", Params.GossipPort, err)
		stopBecausePortIsTaken()
		return
	}
	defer listener8080.Close()

	StartPeerCache()
	go runPeerDiscovery()
	go runRelayUplinkManager()
	SetupAutomatedPortMapping()

	for {
		conn, err := listener8080.Accept()
		if err != nil {
			continue
		}

		remoteIP, _, _ := net.SplitHostPort(conn.RemoteAddr().String())

		BlacklistMutex.Lock()
		bannedUntil, isBanned := MaliciousIPBlacklist[remoteIP]
		if isBanned && time.Now().Before(bannedUntil) {
			BlacklistMutex.Unlock()
			conn.Close()
			continue
		}
		BlacklistMutex.Unlock()

		IPRegistryMutex.Lock()
		currentActiveSlots := IPConnectionRegistry[remoteIP]
		if currentActiveSlots >= maxConnectionsPerAddress {
			IPRegistryMutex.Unlock()
			conn.Close()
			continue
		}
		IPConnectionRegistry[remoteIP] = currentActiveSlots + 1
		IPRegistryMutex.Unlock()

		// A seed is always let in. The other places are first come, first
		// kept, and anyone can hold them for as long as they keep talking,
		// so the seeds must not depend on finding one free.
		if isSeedHost(remoteIP) {
			go serveInboundConnection(conn, false)
			continue
		}
		select {
		case MaxMeshConnectionPool <- struct{}{}:
			go serveInboundConnection(conn, true)
		default:
			decrementIPConnection(remoteIP)
			conn.Close()
		}
	}
}

// stopBecausePortIsTaken ends the program. Tests put something else here.
var stopBecausePortIsTaken = func() { os.Exit(1) }

// backgroundWork counts the sends that were started and left to finish by
// themselves, so that tests can wait for them before they put the network
// settings back.
var backgroundWork sync.WaitGroup

// inBackground runs something without waiting for it.
func inBackground(work func()) {
	backgroundWork.Add(1)
	go func() {
		defer backgroundWork.Done()
		work()
	}()
}

func decrementIPConnection(ip string) {
	IPRegistryMutex.Lock()
	if slots := IPConnectionRegistry[ip]; slots > 1 {
		IPConnectionRegistry[ip] = slots - 1
	} else {
		delete(IPConnectionRegistry, ip)
	}
	IPRegistryMutex.Unlock()
}

func flagMalformedIP(ip string) {
	IPRegistryMutex.Lock()
	IPMalformedTrack[ip]++
	count := IPMalformedTrack[ip]
	IPRegistryMutex.Unlock()

	// One malformed message can be an accident of the network. Ten are not.
	if count >= 10 {
		BlacklistMutex.Lock()
		MaliciousIPBlacklist[ip] = time.Now().Add(5 * time.Minute)
		log.Printf("🚫 [NETWORK] %s has sent ten malformed messages. Nothing is taken from it for 5 minutes.\n", ip)
		BlacklistMutex.Unlock()
	}
}

// maxConnectionsPerAddress is how many connections one address may hold.
const maxConnectionsPerAddress = 2

// isSeedHost reports whether an address is one of this network's seeds.
func isSeedHost(host string) bool {
	for _, seed := range Params.SeedHosts {
		if seed == host {
			return true
		}
	}
	return false
}

// serveInboundConnection serves one inbound connection. pooled says
// whether it holds one of the shared places, which it gives back at the end.
func serveInboundConnection(conn net.Conn, pooled bool) {
	remoteAddr := conn.RemoteAddr().String()
	remoteIP, _, _ := net.SplitHostPort(remoteAddr)
	rateLimiter := GetIPRateLimiter(remoteIP)

	// Transfers have an allowance of their own. When they shared one with
	// everything else, a node that passed on a burst of transfers could have
	// its next block ignored by the receiver.
	transferLimiter := GetIPRateLimiter("transfers|" + remoteIP)

	defer func() {
		unregisterRelaySubscriber(conn)
		forgetRelayConnection(conn)
		conn.Close()
		decrementIPConnection(remoteIP)
		if pooled {
			<-MaxMeshConnectionPool
		}
	}()

	for {
		_ = conn.SetReadDeadline(time.Now().Add(DefaultReadTimeout))
		payloadBuffer, signature, err := readFrame(conn)
		if err != nil {
			var tooLarge frameTooLarge
			if errors.As(err, &tooLarge) {
				// Name the sender and say what the first four bytes look
				// like. This port is scanned constantly, and a web request
				// or TLS hello read as a length gives an absurd size; a real
				// node doing this is a different matter.
				log.Printf("🚨 [NETWORK] %s sent something that is not a CVN message: its first four bytes, %q, read as a length give %d bytes (%s). Closing the connection.\n", remoteIP, tooLarge.header[:], tooLarge.length, tooLarge.looksLike())
				flagMalformedIP(remoteIP)
			}
			return
		}
		// A message that is not signed for this network is dropped unread.
		if !VerifyPayloadHMAC(payloadBuffer, signature) {
			log.Printf("🚫 [NETWORK] %s sent a message that is not signed for this network. Closing the connection.\n", remoteIP)
			flagMalformedIP(remoteIP)
			return
		}

		msg := string(payloadBuffer)
		msg = strings.TrimSpace(msg)
		if msg == "" {
			continue
		}
		if strings.HasPrefix(msg, "TX_BROADCAST:") {
			if !transferLimiter.Allow() {
				continue
			}
		} else if !rateLimiter.Allow() {
			continue
		}

		if strings.HasPrefix(msg, "RELAY_FEATURES:") {
			// The other end says what it understands. It is told the same
			// about this node, once.
			if strings.Contains(msg, relayFeatureAnnounce) && noteConnectionAnnounces(conn) {
				writeFrame(conn, "RELAY_FEATURES:"+relayFeatureAnnounce)
			}
			continue
		}
		if strings.HasPrefix(msg, "BLOCK_ANNOUNCE:") {
			handleBlockAnnouncement(conn, remoteIP, strings.TrimPrefix(msg, "BLOCK_ANNOUNCE:"))
			continue
		}
		if strings.HasPrefix(msg, "BLOCK_REQUEST:") {
			handleBlockRequest(conn, strings.TrimPrefix(msg, "BLOCK_REQUEST:"))
			continue
		}

		if strings.HasPrefix(msg, "RELAY_SUBSCRIBE:") {
			// The peer asks us to push new blocks and transactions back down this
			// connection, which it opened itself. That reaches nodes whose router
			// accepts no inbound connections. Our own ID means we dialed ourselves.
			subscriberID := strings.TrimSpace(strings.TrimPrefix(msg, "RELAY_SUBSCRIBE:"))
			if subscriberID != localNodeInstanceID {
				registerRelaySubscriber(conn, remoteIP)
				noteRelayIdentity(conn, subscriberID)
			}
			writeFrame(conn, "RELAY_ACK:"+localNodeInstanceID)
			continue
		}

		if strings.HasPrefix(msg, "PEER_PING:") {
			PeerMapMutex.Lock()
			if len(ActivePeerMap) >= MaxPeerMapCapacity {
				var oldestPeer string
				oldestTime := time.Now()
				for peer, lastSeen := range ActivePeerMap {
					if lastSeen.Before(oldestTime) {
						oldestTime = lastSeen
						oldestPeer = peer
					}
				}
				if oldestPeer != "" {
					delete(ActivePeerMap, oldestPeer)
				}
			}
			// A ping from one of this machine's own addresses is this node
			// talking to itself; it is answered, and not listed as a peer.
			if !isOwnHost(remoteIP) {
				canonicalPeerKey := Params.gossipAddress(remoteIP)
				ActivePeerMap[canonicalPeerKey] = time.Now()
			}

			var peers []string
			for p := range ActivePeerMap {
				peers = append(peers, p)
			}
			PeerMapMutex.Unlock()

			listBytes, _ := json.Marshal(peers)
			writeFrame(conn, fmt.Sprintf("PEER_LIST:%s", string(listBytes)))
			continue
		}

		if strings.HasPrefix(msg, "TX_BROADCAST:") {
			txPayload := strings.TrimPrefix(msg, "TX_BROADCAST:")
			var incomingTx Transaction
			if err := json.Unmarshal([]byte(txPayload), &incomingTx); err != nil {
				flagMalformedIP(remoteIP)
				continue
			}
			// Only transfers that pass the full checks are stored and passed
			// on; a transfer we already hold is not relayed again.
			queueIncomingTransfer(incomingTx)
			continue
		}

		if strings.HasPrefix(msg, "BLOCK_BROADCAST:") {
			blockPayload := strings.TrimPrefix(msg, "BLOCK_BROADCAST:")
			var incomingBlock Block
			if err := json.Unmarshal([]byte(blockPayload), &incomingBlock); err != nil {
				flagMalformedIP(remoteIP)
				continue
			}
			if !VerifyIncomingWireBlock(incomingBlock) {
				// Under the heartbeat rule this check is the same for every
				// node (the identifier must be the block's own and its
				// difficulty at least the minimum), so a block that fails it
				// is malformed, and counts against the sender.
				if IsHeartbeatHeight(incomingBlock.Index) {
					flagMalformedIP(remoteIP)
				}
				continue
			}
			if blockCarriesBadWork(incomingBlock) {
				// Checking a proof costs real time. A peer that makes us do it
				// for nothing is cut off before it can do so again.
				banPeer(remoteIP, "sent a block with invalid proof of work")
				return
			}
			go ProcessIncomingWireBlockWithDelay(incomingBlock)
			continue
		}

	}
}

// A MESSAGE ON THE WIRE
//
// Every message between nodes is one frame: four bytes giving the length of
// the payload, 32 bytes of signature over the payload (see
// getNetworkAuthSecret), and the payload, which is a word, a colon, and what
// the word is about ("BLOCK_BROADCAST:" and a block, for example).

// frameTooLarge is the error for a frame whose stated length is over the
// limit. Whatever sent it is almost never a CVN node.
type frameTooLarge struct {
	header [4]byte
	length uint32
}

func (e frameTooLarge) Error() string {
	return fmt.Sprintf("a frame of %d bytes is over the limit", e.length)
}

// looksLike guesses what sent the four bytes that were read as a length.
func (e frameTooLarge) looksLike() string {
	switch first := string(e.header[:]); {
	case first == "GET " || first == "POST" || first == "HEAD" || first == "PUT " || first == "OPTI" || first == "CONN":
		return "an HTTP request"
	case e.header[0] == 0x16 && e.header[1] == 0x03:
		return "a TLS handshake"
	case first[:3] == "SSH":
		return "an SSH client"
	}
	return "unknown"
}

// readFrame takes one frame off a connection. It checks the length against
// the limit and nothing else: whether the signature is good is for the
// caller to decide, with VerifyPayloadHMAC.
func readFrame(conn net.Conn) (payload, signature []byte, err error) {
	var header [4]byte
	if _, err = io.ReadFull(conn, header[:]); err != nil {
		return nil, nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length > MaxPayloadFrameSize {
		return nil, nil, frameTooLarge{header: header, length: length}
	}
	signature = make([]byte, 32)
	if _, err = io.ReadFull(conn, signature); err != nil {
		return nil, nil, err
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(conn, payload); err != nil {
		return nil, nil, err
	}
	return payload, signature, nil
}

// writeFrame sends one frame.
func writeFrame(w io.Writer, payload string) {
	payloadBytes := []byte(payload)
	if len(payloadBytes) > MaxPayloadFrameSize {
		// No node would read it: readFrame refuses anything larger, and
		// counts it against the sender. Nothing this program builds is
		// near the limit, so this is a fault here, not something to send.
		log.Printf("🚨 A message of %d bytes was not sent: the largest a node reads is %d.\n", len(payloadBytes), MaxPayloadFrameSize)
		return
	}
	signature := ComputePayloadHMAC(payloadBytes)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(payloadBytes)))

	// One Write call per frame. A connection is written to from several
	// places at once (answers to pings, blocks, transfers), and three
	// separate writes could interleave two frames into nonsense.
	frame := make([]byte, 0, 4+len(signature)+len(payloadBytes))
	frame = append(frame, length[:]...)
	frame = append(frame, signature...)
	frame = append(frame, payloadBytes...)
	conn, isConn := w.(net.Conn)
	if isConn {
		// A node that stops reading must not be able to hang the sender.
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	}
	if _, err := w.Write(frame); err != nil && isConn {
		// Part of the frame may have gone. Connections stay open now, and
		// whatever was written next would be read as the rest of this frame:
		// nonsense, counted against this node as a malformed message. The
		// connection is closed, and whoever holds it opens another.
		conn.Close()
	}
}

func BroadcastNewBlock(b Block) {
	jsonBytes, err := json.Marshal(b)
	if err != nil {
		return
	}
	payload := fmt.Sprintf("BLOCK_BROADCAST:%s", string(jsonBytes))
	// A block is passed on once, however many parts of the program ask.
	if !rememberBlockPassedOn(b.Hash, payload) {
		return
	}
	// A large block is announced to the nodes that understand that, and
	// sent whole only to those that ask (see block_relay.go).
	announcement := ""
	if len(payload) > announceBlocksOverBytes {
		if short, err := json.Marshal(blockAnnouncement{Height: b.Index, Hash: b.Hash}); err == nil {
			announcement = "BLOCK_ANNOUNCE:" + string(short)
		}
	}
	sendToNetwork(payload, announcement)
}

func BroadcastNewTransaction(tx Transaction) {
	jsonBytes, err := json.Marshal(tx)
	if err != nil {
		return
	}
	payload := fmt.Sprintf("TX_BROADCAST:%s", string(jsonBytes))
	sendToNetwork(payload, "")
}

// sendToNetwork passes a block or a transfer on: down
// every connection already open, and to a few nodes with no open connection
// by calling them up. announcement, when not empty, is sent in place of the
// payload to the open connections whose other end understands it.
func sendToNetwork(payload, announcement string) {
	PeerMapMutex.RLock()
	var targets []string
	for peer := range ActivePeerMap {
		targets = append(targets, peer)
	}
	PeerMapMutex.RUnlock()

	localBypassIP := os.Getenv("CVN_BYPASS_SEED_IP")
	if localBypassIP == "" {
		localBypassIP = Params.WindowsSelfSkipHost
	}

	// Nodes with a connection open to us, or that we have one open to, get
	// it down that connection. Calling them up as well would only deliver it
	// twice (or time out against their firewall).
	reached := sendOverOpenConnections(payload, announcement)

	// Of the others, a fixed number are called up, chosen afresh each time.
	// Every node that takes the block does the same, so it still reaches
	// them all, and the work per node no longer grows with the network.
	mathrand.Shuffle(len(targets), func(a, b int) { targets[a], targets[b] = targets[b], targets[a] })
	called := 0
	for _, address := range targets {
		if localBypassIP != "" && strings.Contains(address, localBypassIP) && runtime.GOOS == "windows" {
			continue
		}
		if host, _, splitErr := net.SplitHostPort(address); splitErr == nil && reached[host] {
			continue
		}
		if called++; called > freshCallsPerMessage {
			break
		}
		go callAndSend(address, payload, announcement)
	}
}

// freshCallFeatureWait is how long a node called up afresh is given to say
// that it understands announcements, before it is sent a large block whole.
const freshCallFeatureWait = time.Second

// callAndSend calls a node up and gives it a block or a transfer.
//
// A large block is not sent whole to a node that can ask for it. The caller
// says it understands announcements; a node of this version says the same
// back, is sent the announcement, and is handed the block if it asks, which
// it does only if it has not got the block another way. A node of an older
// version says nothing back, and after a moment is sent the whole block, as
// it always was. Large blocks used to be sent whole on every one of these
// calls, up to eight times by every node.
func callAndSend(address, payload, announcement string) {
	conn, err := dialPeer(address, 3*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()
	if announcement == "" {
		writeFrame(conn, payload)
		return
	}
	writeFrame(conn, "RELAY_FEATURES:"+relayFeatureAnnounce)
	_ = conn.SetReadDeadline(time.Now().Add(freshCallFeatureWait))
	answer, signature, err := readFrame(conn)
	if err != nil || !VerifyPayloadHMAC(answer, signature) ||
		!strings.HasPrefix(string(answer), "RELAY_FEATURES:") || !strings.Contains(string(answer), relayFeatureAnnounce) {
		writeFrame(conn, payload)
		return
	}
	writeFrame(conn, announcement)
	// The other node may ask at once, or only after waiting on someone it
	// asked first. The call is kept for as long as that can take.
	_ = conn.SetReadDeadline(time.Now().Add(3 * blockFetchPatience))
	for {
		request, signature, err := readFrame(conn)
		if err != nil || !VerifyPayloadHMAC(request, signature) {
			return
		}
		if msg := string(request); strings.HasPrefix(msg, "BLOCK_REQUEST:") {
			handleBlockRequest(conn, strings.TrimPrefix(msg, "BLOCK_REQUEST:"))
			return
		}
	}
}

func loadStaticMeshSeeds() []string {
	staticMeshSeeds := seedGossipAddresses()

	configFileBytes, err := os.ReadFile("miner_config.json")
	if err == nil {
		var parsedConfig map[string]interface{}
		if json.Unmarshal(configFileBytes, &parsedConfig) == nil {
			if seedInterface, exists := parsedConfig["static_mesh_seeds"]; exists {
				if sliceArray, ok := seedInterface.([]interface{}); ok && len(sliceArray) > 0 {
					var parsedSeeds []string
					for _, val := range sliceArray {
						if strSeed, ok := val.(string); ok && strSeed != "" {
							parsedSeeds = append(parsedSeeds, strings.TrimSpace(strSeed))
						}
					}
					if len(parsedSeeds) > 0 {
						staticMeshSeeds = parsedSeeds
					}
				}
			}
		}
	}
	return staticMeshSeeds
}

// runPeerDiscovery keeps the list of other nodes fresh. Every fifteen
// seconds it asks the seeds, and the nodes remembered from earlier runs, which
// nodes they know of, and it forgets any node not heard from for a minute and
// a half.
func runPeerDiscovery() {
	for {
		for _, address := range seedsAndCachedPeers() {
			go askForPeers(address)
		}
		time.Sleep(15 * time.Second)
		PeerMapMutex.Lock()
		for peer, lastSeen := range ActivePeerMap {
			if time.Since(lastSeen) > (90 * time.Second) {
				delete(ActivePeerMap, peer)
				log.Printf("🔌 [NETWORK] Nothing heard from %s for 90 seconds; no longer sending to it until it is heard from again.\n", peer)
			}
		}
		PeerMapMutex.Unlock()
	}
}

// askForPeers asks one node which nodes it knows of, and says hello to each
// of those this node has not met.
func askForPeers(address string) {
	conn, err := dialPeer(address, 2*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()
	writeFrame(conn, "PEER_PING:DISCOVERY_REQUEST")
	_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	payload, signature, err := readFrame(conn)
	if err != nil {
		return
	}
	response := string(payload)
	if !strings.HasPrefix(response, "PEER_LIST:") {
		return
	}
	// A node that answered with a correctly signed list is alive, so it is
	// listed now. Until this was added a node only learned of another when
	// that one pinged it, and for up to fifteen seconds after starting its
	// blocks were sent to nobody. A list that is not signed for this network
	// is not read at all.
	if !VerifyPayloadHMAC(payload, signature) {
		return
	}
	PeerMapMutex.Lock()
	if _, known := ActivePeerMap[address]; known || len(ActivePeerMap) < MaxPeerMapCapacity {
		ActivePeerMap[address] = time.Now()
	}
	PeerMapMutex.Unlock()
	var listed []string
	if json.Unmarshal([]byte(strings.TrimPrefix(response, "PEER_LIST:")), &listed) != nil {
		return
	}
	for _, other := range listed {
		other = strings.TrimSpace(strings.ReplaceAll(other, "\r", ""))
		if other == "" || strings.HasPrefix(other, "127.0.0.1") || isOwnGossipAddress(other) {
			continue
		}
		PeerMapMutex.RLock()
		_, met := ActivePeerMap[other]
		PeerMapMutex.RUnlock()
		if met {
			continue
		}
		// A ping is how the other node comes to list this one.
		go func(other string) {
			if hello, err := dialPeer(other, 2*time.Second); err == nil {
				writeFrame(hello, "PEER_PING:DISCOVERY_REQUEST")
				hello.Close()
			}
		}(other)
	}
}
