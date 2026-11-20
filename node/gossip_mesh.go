package node

import (
	"encoding/binary"
	"encoding/json"
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

// Global Peer Registry Configuration Constants
const (
	MaxPeerMapCapacity  = 200
	MaxPayloadFrameSize = 2048 * 1024 // 👑 UPGRADED TO 2 MB PRODUCTION CEILING WINDOW FOR NETWORK PARITY
	DefaultReadTimeout  = 30 * time.Second
)

var (
	ActivePeerMap = make(map[string]time.Time)
	PeerMapMutex  sync.RWMutex
	// MaxMeshConnectionPool is how many connections from nodes that are not
	// seeds are served at once. Most are connections that stay open to
	// receive blocks, and an open connection that only hears announcements
	// costs little.
	MaxMeshConnectionPool = make(chan struct{}, 600)

	// Security Blacklists & IP Connection Trackers
	IPConnectionRegistry = make(map[string]int)
	IPRegistryMutex      sync.Mutex
	MaliciousIPBlacklist = make(map[string]time.Time)
	BlacklistMutex       sync.Mutex
	IPMalformedTrack     = make(map[string]int)
)

// TokenBucket handles per-remote-IP network token rate limiting
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
		log.Printf("🚨 Could not listen for other nodes on port %d: %v\n", Params.GossipPort, err)
		return
	}
	defer listener8080.Close()

	StartPeerCache()
	go runNetworkTopologyDiscoveryDiscoveryLoop()
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
			go handleIncomingPeerStream(conn, false)
			continue
		}
		select {
		case MaxMeshConnectionPool <- struct{}{}:
			go handleIncomingPeerStream(conn, true)
		default:
			decrementIPConnection(remoteIP)
			conn.Close()
		}
	}
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

	if count >= 10 { // 👑 Relax structural block bounds to ensure clean cross-continental handshakes
		BlacklistMutex.Lock()
		MaliciousIPBlacklist[ip] = time.Now().Add(5 * time.Minute)
		log.Printf("🚫 [SECURITY FIREWALL] IP %s blacklisted for 5 minutes due to tracking variations.\n", ip)
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

// handleIncomingPeerStream serves one inbound connection. pooled says
// whether it holds one of the shared places, which it gives back at the end.
func handleIncomingPeerStream(conn net.Conn, pooled bool) {
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
		// Enforce adaptive refreshing read deadline (30-second idle timeout)
		_ = conn.SetReadDeadline(time.Now().Add(DefaultReadTimeout))

		// 👑 CHUNK BOUNDARY ALIGNMENT: Allocate an explicit 4-byte buffer for the frame size header
		lengthHeaderBuffer := make([]byte, 4)
		_, err := io.ReadFull(conn, lengthHeaderBuffer)
		if err != nil {
			return
		}

		frameLength := binary.BigEndian.Uint32(lengthHeaderBuffer)
		if frameLength > MaxPayloadFrameSize {
			// Name the sender and say what the first four bytes look like. Port 8080
			// is scanned constantly, and a web request or TLS hello read as a length
			// header gives an absurd size; a real peer doing this is a different matter.
			looksLike := "unknown protocol"
			switch {
			case string(lengthHeaderBuffer) == "GET " || string(lengthHeaderBuffer) == "POST" || string(lengthHeaderBuffer) == "HEAD" || string(lengthHeaderBuffer) == "PUT " || string(lengthHeaderBuffer) == "OPTI" || string(lengthHeaderBuffer) == "CONN":
				looksLike = "an HTTP request, not a CVN peer"
			case lengthHeaderBuffer[0] == 0x16 && lengthHeaderBuffer[1] == 0x03:
				looksLike = "a TLS handshake, not a CVN peer"
			case lengthHeaderBuffer[0] == 'S' && lengthHeaderBuffer[1] == 'S' && lengthHeaderBuffer[2] == 'H':
				looksLike = "an SSH client, not a CVN peer"
			}
			log.Printf("🚨 [FRAME VIOLATION DETECTED] %s sent a %d-byte frame header (%q: %s). Severing connection.\n", remoteIP, frameLength, lengthHeaderBuffer, looksLike)
			flagMalformedIP(remoteIP)
			return
		}

		// 👑 CHUNK BOUNDARY ALIGNMENT: Explicitly isolate the 32-byte HMAC signature buffer next
		macSignatureBuffer := make([]byte, 32)
		_, err = io.ReadFull(conn, macSignatureBuffer)
		if err != nil {
			return
		}

		// 👑 CHUNK BOUNDARY ALIGNMENT: Stream out exactly the designated payload bytes
		payloadBuffer := make([]byte, frameLength)
		_, err = io.ReadFull(conn, payloadBuffer)
		if err != nil {
			return
		}

		// Discard packet instantly if cross-continental cryptographic envelope checks break
		if !VerifyPayloadHMAC(payloadBuffer, macSignatureBuffer) {
			log.Printf("🚫 [AUTHENTICATION REFUSAL] Aborted unauthenticated or misaligned packet flood from IP: %s\n", remoteIP)
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
				writeFramedPayloadNatively(conn, "RELAY_FEATURES:"+relayFeatureAnnounce)
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
			}
			writeFramedPayloadNatively(conn, "RELAY_ACK:"+localNodeInstanceID)
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
			writeFramedPayloadNatively(conn, fmt.Sprintf("PEER_LIST:%s", string(listBytes)))
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

func writeFramedPayloadNatively(w io.Writer, payload string) {
	payloadBytes := []byte(payload)
	macSignature := ComputePayloadHMAC(payloadBytes)

	// Structured serialization layout protocol: [Length Header] -> [HMAC Signature] -> [Payload JSON]
	lengthHeaderBuffer := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthHeaderBuffer, uint32(len(payloadBytes)))

	// One Write call per frame. Relay connections are written to by several
	// goroutines (ping replies, block pushes), and three separate writes could
	// interleave two frames into garbage.
	frame := make([]byte, 0, 4+len(macSignature)+len(payloadBytes))
	frame = append(frame, lengthHeaderBuffer...)
	frame = append(frame, macSignature...)
	frame = append(frame, payloadBytes...)
	if conn, isConn := w.(net.Conn); isConn {
		// A peer that stops reading must not be able to hang the sender.
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	}
	_, _ = w.Write(frame)
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
	transmitPayloadToNetworkBoundary(payload, announcement)
}

func BroadcastNewTransaction(tx Transaction) {
	jsonBytes, err := json.Marshal(tx)
	if err != nil {
		return
	}
	payload := fmt.Sprintf("TX_BROADCAST:%s", string(jsonBytes))
	transmitPayloadToNetworkBoundary(payload, "")
}

// transmitPayloadToNetworkBoundary passes a block or a transfer on: down
// every connection already open, and to a few nodes with no open connection
// by calling them up. announcement, when not empty, is sent in place of the
// payload to the open connections whose other end understands it.
func transmitPayloadToNetworkBoundary(payload, announcement string) {
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
		go func(addr string) {
			conn, err := dialPeer(addr, 3*time.Second)
			if err == nil {
				writeFramedPayloadNatively(conn, payload)
				conn.Close()
			}
		}(address)
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

func runNetworkTopologyDiscoveryDiscoveryLoop() {
	for {
		staticMeshSeeds := seedsAndCachedPeers()

		for _, seedAddr := range staticMeshSeeds {
			go func(addr string) {
				conn, err := dialPeer(addr, 2*time.Second)
				if err == nil {
					writeFramedPayloadNatively(conn, "PEER_PING:DISCOVERY_REQUEST")

					var frameLength uint32
					_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))

					lengthHeaderBuffer := make([]byte, 4)
					_, err = io.ReadFull(conn, lengthHeaderBuffer)
					if err == nil {
						frameLength = binary.BigEndian.Uint32(lengthHeaderBuffer)
						if frameLength <= MaxPayloadFrameSize {
							// Discard isolated hmac padding space during network discovery handshakes
							macDiscardBuffer := make([]byte, 32)
							_, err = io.ReadFull(conn, macDiscardBuffer)
							if err == nil {
								payloadBuffer := make([]byte, frameLength)
								_, err = io.ReadFull(conn, payloadBuffer)
								if err == nil {
									responseString := string(payloadBuffer)
									if strings.HasPrefix(responseString, "PEER_LIST:") {
										// A seed that answered with a correctly signed peer list is
										// alive, so register it now. Until this was added a node only
										// learned a peer when that peer pinged it, so for up to 15
										// seconds after start its blocks were broadcast to nobody.
										if VerifyPayloadHMAC(payloadBuffer, macDiscardBuffer) {
											PeerMapMutex.Lock()
											if _, known := ActivePeerMap[addr]; known || len(ActivePeerMap) < MaxPeerMapCapacity {
												ActivePeerMap[addr] = time.Now()
											}
											PeerMapMutex.Unlock()
										}
										rawPayload := strings.TrimPrefix(responseString, "PEER_LIST:")
										var receivedAddresses []string
										if json.Unmarshal([]byte(rawPayload), &receivedAddresses) == nil {
											for _, ip := range receivedAddresses {
												cleanedIP := strings.TrimSpace(strings.ReplaceAll(ip, "\r", ""))
												if cleanedIP == "" || strings.HasPrefix(cleanedIP, "127.0.0.1") || isOwnGossipAddress(cleanedIP) {
													continue
												}
												PeerMapMutex.RLock()
												_, exists := ActivePeerMap[cleanedIP]
												PeerMapMutex.RUnlock()
												if !exists {
													go func(targetIP string) {
														dConn, dErr := dialPeer(targetIP, 2*time.Second)
														if dErr == nil {
															writeFramedPayloadNatively(dConn, "PEER_PING:DISCOVERY_REQUEST")
															dConn.Close()
														}
													}(cleanedIP)
												}
											}
										}
									}
								}
							}
						}
					}
					conn.Close()
				}
			}(seedAddr)
		}
		time.Sleep(15 * time.Second)
		PeerMapMutex.Lock()
		for peer, lastSeen := range ActivePeerMap {
			if time.Since(lastSeen) > (90 * time.Second) {
				delete(ActivePeerMap, peer)
				// Nodes do not hold connections open to each other; this is
				// the list of nodes that new blocks and transfers are sent to.
				log.Printf("🔌 [NETWORK] Nothing heard from %s for 90 seconds; no longer sending to it until it is heard from again.\n", peer)
			}
		}
		PeerMapMutex.Unlock()
	}
}
