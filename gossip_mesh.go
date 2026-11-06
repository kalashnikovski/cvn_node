package main

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
	MaxPayloadFrameSize = 256 * 1024 // 256 KiB Frame Ceiling Guard
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
			refillRate: 20, // max 20 messages/second refill
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
	fmt.Println("📡 HARDENED P2P GOSSIP MESH INITIALIZING ON PORT 8080...")
	fmt.Println("====================================================================")

	listener8080, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Printf("🚨 CRITICAL MESH FAILURE: Unable to bind raw TCP socket on Port 8080: %v\n", err)
		return
	}
	defer listener8080.Close()

	go runNetworkTopologyDiscoveryDiscoveryLoop()

	for {
		conn, err := listener8080.Accept()
		if err != nil {
			continue
		}

		remoteIP, _, _ := net.SplitHostPort(conn.RemoteAddr().String())

		// Security Check: Enforce structural Firewall Blacklists
		BlacklistMutex.Lock()
		bannedUntil, isBanned := MaliciousIPBlacklist[remoteIP]
		if isBanned && time.Now().Before(bannedUntil) {
			BlacklistMutex.Unlock()
			conn.Close()
			continue
		}
		BlacklistMutex.Unlock()

		// Flow Control Check: Enforce active max connection limits per IP (Max 4)
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

	if count >= 5 {
		BlacklistMutex.Lock()
		MaliciousIPBlacklist[ip] = time.Now().Add(5 * time.Minute)
		log.Printf("🚫 [SECURITY FIREWALL] IP %s blacklisted for 5 minutes due to flood/malformed violations.\n", ip)
		BlacklistMutex.Unlock()
	}
}

func handleIncomingPeerStream(conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	remoteIP, _, _ := net.SplitHostPort(remoteAddr)
	rateLimiter := GetIPRateLimiter(remoteIP)
	
	defer func() {
		conn.Close()
		decrementIPConnection(remoteIP)
		<-MaxMeshConnectionPool
	}()

	var lastGetBatchRequest time.Time
	
	for {
		// Enforce adaptive refreshing read deadline (30 second idle timeout)
		_ = conn.SetReadDeadline(time.Now().Add(DefaultReadTimeout))

		// Length-Prefixed Framing: Read 4-byte big-endian header metadata
		var frameLength uint32
		err := binary.Read(conn, binary.BigEndian, &frameLength)
		if err != nil {
			return 
		}

		if frameLength > MaxPayloadFrameSize {
			log.Printf("🚨 [FRAME VIOLATION] Packet frame size (%d bytes) exceeds limits.\n", frameLength)
			flagMalformedIP(remoteIP)
			return
		}

		// 👑 PROTOCOL RE-ENGINEERING CHECK: EXTRACT MAC ENVELOPE PRIOR TO UNMARSHALING
		macSignatureBuffer := make([]byte, 32)
		_, err = io.ReadFull(conn, macSignatureBuffer)
		if err != nil {
			return
		}

		payloadBuffer := make([]byte, frameLength)
		_, err = io.ReadFull(conn, payloadBuffer)
		if err != nil {
			return
		}

		// 👑 THE AUTHENTICATION WALL: Discard packets instantly if HMAC checks fail
		if !VerifyPayloadHMAC(payloadBuffer, macSignatureBuffer) {
			log.Printf("🚫 [AUTHENTICATION REFUSAL] Dropping unauthenticated packet flood from IP: %s\n", remoteIP)
			flagMalformedIP(remoteIP)
			return
		}

		if !rateLimiter.Allow() {
			continue
		}
        // ... rest of the existing message prefix string selectors switch branches remain identical

		msg := string(payloadBuffer)
		msg = strings.TrimSpace(msg)
		if msg == "" {
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
			canonicalPeerKey := fmt.Sprintf("%s:8080", remoteIP)
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
			if VerifyIncomingWireTransaction(incomingTx) {
				MempoolMatrix.PushTransaction(incomingTx)
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
			// Enforce minimum inter-request interval of 200 ms per IP
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

			// Enforce a maximum of 64 blocks per request batch
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
	
	// Compute the 32-byte secure HMAC signature
	macSignature := ComputePayloadHMAC(payloadBytes)
	
	// Total packet structure: 4 bytes (payload len) + 32 bytes (HMAC) + raw payload
	lengthHeaderBuffer := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthHeaderBuffer, uint32(len(payloadBytes)))
	
	_, _ = w.Write(lengthHeaderBuffer)
	_, _ = w.Write(macSignature)
	_, _ = w.Write(payloadBytes)
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
		localBypassIP = "202.137.175.220"
	}

	for _, address := range targets {
		if strings.Contains(address, localBypassIP) && runtime.GOOS == "windows" {
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

func runNetworkTopologyDiscoveryDiscoveryLoop() {
	for {
		staticMeshSeeds := []string{
			"202.137.175.220:8080",
			"207.148.67.11:8080",
			"64.177.45.153:8080",
		}

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

		for _, seedAddr := range staticMeshSeeds {
			go func(addr string) {
				conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
				if err == nil {
writeFramedPayloadNatively(conn, "PEER_PING:DISCOVERY_REQUEST")
var frameLength uint32
_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
err = binary.Read(conn, binary.BigEndian, &frameLength)
if err == nil && frameLength <= MaxPayloadFrameSize {
payloadBuffer := make([]byte, frameLength)
_, err = io.ReadFull(conn, payloadBuffer)
if err == nil {
responseString := string(payloadBuffer)
if strings.HasPrefix(responseString, "PEER_LIST:") {
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