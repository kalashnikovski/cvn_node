package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// OUTBOUND RELAY
//
// A node behind a home router can open connections out, but nobody can connect
// in to it unless ports are forwarded. Blocks used to be delivered only by
// dialing each peer, so such a node never heard about new blocks in time and
// kept mining on a stale tip.
//
// Every node now keeps one long-lived connection open to each seed and public
// peer it knows (an "uplink") and asks the far end to push new blocks and
// transactions back down it. Because the firewalled node opened the connection
// itself, the router lets the replies through. No port forwarding is needed to
// receive blocks. A node that IS reachable still gets dialed by peers running
// older code, and serves these pushes to others.
//
// Peers running older code ignore RELAY_SUBSCRIBE; the uplink then simply acts
// as the usual keep-alive ping.

const (
	relayMaxUplinks        = 8
	relayPingInterval      = 15 * time.Second
	relayReadTimeout       = 45 * time.Second
	relayRetryAfterFailure = 2 * time.Minute
	relayMaxSubscribers    = 64
)

// localNodeInstanceID is random per process. It lets a node notice that an
// address it dialed is in fact itself (its own public IP).
var localNodeInstanceID = newNodeInstanceID()

func newNodeInstanceID() string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("node-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}

// ---------------------------------------------------------------------------
// Serving side: peers that asked us to push to them.
// ---------------------------------------------------------------------------

var (
	relaySubscribers      = make(map[net.Conn]string) // connection -> remote IP
	relaySubscribersMutex sync.RWMutex
)

func registerRelaySubscriber(conn net.Conn, remoteIP string) {
	relaySubscribersMutex.Lock()
	_, already := relaySubscribers[conn]
	if already || len(relaySubscribers) < relayMaxSubscribers {
		relaySubscribers[conn] = remoteIP
	}
	relaySubscribersMutex.Unlock()
	if !already {
		log.Printf("📡 [RELAY] %s subscribed to live block pushes.\n", remoteIP)
	}
}

func unregisterRelaySubscriber(conn net.Conn) {
	relaySubscribersMutex.Lock()
	delete(relaySubscribers, conn)
	relaySubscribersMutex.Unlock()
}

// pushPayloadToRelaySubscribers sends one payload down every subscribed
// connection and returns the IPs it was pushed to.
func pushPayloadToRelaySubscribers(payload string) map[string]bool {
	pushedIPs := make(map[string]bool)
	relaySubscribersMutex.RLock()
	connections := make([]net.Conn, 0, len(relaySubscribers))
	for conn, ip := range relaySubscribers {
		connections = append(connections, conn)
		pushedIPs[ip] = true
	}
	relaySubscribersMutex.RUnlock()
	for _, conn := range connections {
		go writeFramedPayloadNatively(conn, payload)
	}
	return pushedIPs
}

// ---------------------------------------------------------------------------
// Dialing side: our own uplinks.
// ---------------------------------------------------------------------------

type relayUplinkState struct {
	live bool // far end confirmed it will push to us
}

var (
	relayUplinks       = make(map[string]*relayUplinkState)
	relayRetryNotUntil = make(map[string]time.Time)
	relaySelfAddresses = make(map[string]bool)
	relayCandidates    = make(map[string]time.Time) // public peers learned from peer lists
	relayUplinkMutex   sync.Mutex
)

// RelayUplinkLiveCount is how many peers are currently pushing blocks to us.
func RelayUplinkLiveCount() int {
	relayUplinkMutex.Lock()
	defer relayUplinkMutex.Unlock()
	count := 0
	for _, uplink := range relayUplinks {
		if uplink.live {
			count++
		}
	}
	return count
}

func runRelayUplinkManager() {
	for {
		wanted := seedsAndCachedPeers()
		PeerMapMutex.RLock()
		for address := range ActivePeerMap {
			wanted = append(wanted, address)
		}
		PeerMapMutex.RUnlock()

		relayUplinkMutex.Lock()
		for address, learnedAt := range relayCandidates {
			if time.Since(learnedAt) > 10*time.Minute {
				delete(relayCandidates, address)
				continue
			}
			wanted = append(wanted, address)
		}
		for _, address := range wanted {
			address = strings.TrimSpace(address)
			if address == "" || relaySelfAddresses[address] || relayUplinks[address] != nil {
				continue
			}
			if time.Now().Before(relayRetryNotUntil[address]) {
				continue
			}
			if len(relayUplinks) >= relayMaxUplinks {
				break
			}
			relayUplinks[address] = &relayUplinkState{}
			go runRelayUplink(address, localNodeInstanceID)
		}
		relayUplinkMutex.Unlock()

		time.Sleep(10 * time.Second)
	}
}

// runRelayUplink holds one uplink open until it breaks. announcedID is the
// instance ID sent to the far end (always localNodeInstanceID outside tests).
func runRelayUplink(address string, announcedID string) {
	wasLive := false
	everConnected := false
	defer func() {
		relayUplinkMutex.Lock()
		delete(relayUplinks, address)
		if !everConnected {
			// Unreachable (most likely a firewalled peer): do not hammer it.
			relayRetryNotUntil[address] = time.Now().Add(relayRetryAfterFailure)
		}
		relayUplinkMutex.Unlock()
		if wasLive {
			log.Printf("🔌 [RELAY] Live block feed from %s closed; reconnecting.\n", address)
		}
	}()

	conn, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()
	everConnected = true

	writeFramedPayloadNatively(conn, "RELAY_SUBSCRIBE:"+announcedID)
	writeFramedPayloadNatively(conn, "PEER_PING:DISCOVERY_REQUEST")

	stopPinging := make(chan struct{})
	defer close(stopPinging)
	go func() {
		ticker := time.NewTicker(relayPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopPinging:
				return
			case <-ticker.C:
				writeFramedPayloadNatively(conn, "PEER_PING:DISCOVERY_REQUEST")
			}
		}
	}()

	for {
		_ = conn.SetReadDeadline(time.Now().Add(relayReadTimeout))
		header := make([]byte, 4)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		frameLength := binary.BigEndian.Uint32(header)
		if frameLength > MaxPayloadFrameSize {
			return
		}
		mac := make([]byte, 32)
		if _, err := io.ReadFull(conn, mac); err != nil {
			return
		}
		payload := make([]byte, frameLength)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}
		if !VerifyPayloadHMAC(payload, mac) {
			return
		}
		msg := strings.TrimSpace(string(payload))

		switch {
		case strings.HasPrefix(msg, "RELAY_ACK:"):
			if strings.TrimSpace(strings.TrimPrefix(msg, "RELAY_ACK:")) == announcedID {
				// We dialed our own public address.
				relayUplinkMutex.Lock()
				relaySelfAddresses[address] = true
				relayUplinkMutex.Unlock()
				forgetCachedPeer(address)
				return
			}
			if !wasLive {
				wasLive = true
				relayUplinkMutex.Lock()
				if uplink := relayUplinks[address]; uplink != nil {
					uplink.live = true
				}
				relayUplinkMutex.Unlock()
				announceNetworkStatus(fmt.Sprintf("📡 [RELAY] Live block feed open through %s (no port forwarding needed to receive blocks).", address))
			}

		case strings.HasPrefix(msg, "PEER_LIST:"):
			// The far end answered our ping with a signed list: it is alive.
			rememberReachablePeer(address)
			PeerMapMutex.Lock()
			if _, known := ActivePeerMap[address]; known || len(ActivePeerMap) < MaxPeerMapCapacity {
				ActivePeerMap[address] = time.Now()
			}
			PeerMapMutex.Unlock()
			var listed []string
			if json.Unmarshal([]byte(strings.TrimPrefix(msg, "PEER_LIST:")), &listed) == nil {
				relayUplinkMutex.Lock()
				for _, peer := range listed {
					peer = strings.TrimSpace(strings.ReplaceAll(peer, "\r", ""))
					if peer == "" || strings.HasPrefix(peer, "127.0.0.1") || len(relayCandidates) >= MaxPeerMapCapacity {
						continue
					}
					relayCandidates[peer] = time.Now()
				}
				relayUplinkMutex.Unlock()
			}

		case strings.HasPrefix(msg, "BLOCK_BROADCAST:"):
			var incomingBlock Block
			if json.Unmarshal([]byte(strings.TrimPrefix(msg, "BLOCK_BROADCAST:")), &incomingBlock) != nil {
				continue
			}
			if VerifyIncomingWireBlock(incomingBlock) {
				go ProcessIncomingWireBlockWithDelay(incomingBlock)
			}

		case strings.HasPrefix(msg, "TX_BROADCAST:"):
			var incomingTx Transaction
			if json.Unmarshal([]byte(strings.TrimPrefix(msg, "TX_BROADCAST:")), &incomingTx) != nil {
				continue
			}
			if !VerifyIncomingWireTransaction(incomingTx) {
				continue
			}
			MempoolMatrix.Lock()
			_, alreadyKnown := MempoolMatrix.Transactions[incomingTx.ID]
			MempoolMatrix.Unlock()
			if !alreadyKnown {
				MempoolMatrix.PushTransaction(incomingTx)
				go BroadcastNewTransaction(incomingTx)
			}
		}
	}
}
