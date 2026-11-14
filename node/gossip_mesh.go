package node

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
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
	ActivePeerMap         = make(map[string]time.Time)
	PeerMapMutex          sync.RWMutex
	MaxMeshConnectionPool = make(chan struct{}, 100)

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
	fmt.Printf("📡 HARDENED P2P GOSSIP MESH INITIALIZING ON PORT %d...\n", Params.GossipPort)
	fmt.Println("====================================================================")

	listener8080, err := net.Listen("tcp", ":"+Params.gossipPortText())
	if err != nil {
		log.Printf("🚨 CRITICAL MESH FAILURE: Unable to bind raw TCP socket on Port %d: %v\n", Params.GossipPort, err)
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
		if currentActiveSlots >= 4 {
			IPRegistryMutex.Unlock()
			conn.Close()
			continue
		}
		IPConnectionRegistry[remoteIP] = currentActiveSlots + 1
		IPRegistryMutex.Unlock()

		select {
		case MaxMeshConnectionPool <- struct{}{}:
			go handleIncomingPeerStream(conn)
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
func handleIncomingPeerStream(conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	remoteIP, _, _ := net.SplitHostPort(remoteAddr)
	rateLimiter := GetIPRateLimiter(remoteIP)

	defer func() {
		unregisterRelaySubscriber(conn)
		conn.Close()
		decrementIPConnection(remoteIP)
		<-MaxMeshConnectionPool
	}()

	var lastGetBatchRequest time.Time

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

		if !rateLimiter.Allow() {
			continue
		}

		msg := string(payloadBuffer)
		msg = strings.TrimSpace(msg)
		if msg == "" {
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
			canonicalPeerKey := Params.gossipAddress(remoteIP)
			ActivePeerMap[canonicalPeerKey] = time.Now()

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
			if AdmitTransactionToMempool(incomingTx) == nil {
				go BroadcastNewTransaction(incomingTx)
			}
			continue
		}

		if strings.HasPrefix(msg, "BLOCK_BROADCAST:") {
			blockPayload := strings.TrimPrefix(msg, "BLOCK_BROADCAST:")
			var incomingBlock Block
			if err := json.Unmarshal([]byte(blockPayload), &incomingBlock); err != nil {
				flagMalformedIP(remoteIP)
				continue
			}
			if VerifyIncomingWireBlock(incomingBlock) {
				go ProcessIncomingWireBlockWithDelay(incomingBlock)
			}
			continue
		}

		if strings.HasPrefix(msg, "GET_BATCH:") {
			if time.Since(lastGetBatchRequest) < 200*time.Millisecond {
				continue
			}
			lastGetBatchRequest = time.Now()

			heightStr := strings.TrimPrefix(msg, "GET_BATCH:")
			startHeight, err := strconv.ParseInt(strings.TrimSpace(heightStr), 10, 64)
			if err != nil || startHeight < 0 {
				continue
			}

			var blocks []Block
			StateMutex.RLock()
			currentTipHeight := GetLatestBlockInternal().Index
			StateMutex.RUnlock()

			endHeight := startHeight + 63
			if endHeight > currentTipHeight {
				endHeight = currentTipHeight
			}

			for h := startHeight; h <= endHeight; h++ {
				loadedBlk := GetBlockByHeightFromDB(h)
				if loadedBlk.Hash != "" {
					blocks = append(blocks, loadedBlk)
				}
			}

			jsonBytes, err := json.Marshal(blocks)
			if err == nil {
				writeFramedPayloadNatively(conn, string(jsonBytes))
			}
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
	transmitPayloadToNetworkBoundary(payload)
}

func BroadcastNewTransaction(tx Transaction) {
	jsonBytes, err := json.Marshal(tx)
	if err != nil {
		return
	}
	payload := fmt.Sprintf("TX_BROADCAST:%s", string(jsonBytes))
	transmitPayloadToNetworkBoundary(payload)
}

func transmitPayloadToNetworkBoundary(payload string) {
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

	// Peers holding a relay connection to us get the payload down that
	// connection, so dialing them as well would only deliver it twice (or time
	// out against their firewall).
	subscribedIPs := pushPayloadToRelaySubscribers(payload)

	for _, address := range targets {
		if localBypassIP != "" && strings.Contains(address, localBypassIP) && runtime.GOOS == "windows" {
			continue
		}
		if host, _, splitErr := net.SplitHostPort(address); splitErr == nil && subscribedIPs[host] {
			continue
		}
		go func(addr string) {
			conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
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
				conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
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
												if cleanedIP == "" || strings.HasPrefix(cleanedIP, "127.0.0.1") {
													continue
												}
												PeerMapMutex.RLock()
												_, exists := ActivePeerMap[cleanedIP]
												PeerMapMutex.RUnlock()
												if !exists {
													go func(targetIP string) {
														dConn, dErr := net.DialTimeout("tcp", targetIP, 2*time.Second)
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
				log.Printf("🔌 [GOSSIP MESH] Severed inactive channel: %s\n", peer)
			}
		}
		PeerMapMutex.Unlock()
	}
}
