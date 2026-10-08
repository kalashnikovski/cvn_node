package main

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// PeerRoster manages the synchronized list of active distributed nodes
type PeerRoster struct {
	sync.RWMutex
	ActiveAddresses map[string]time.Time
}

var GlobalRoster = &PeerRoster{
	ActiveAddresses: make(map[string]time.Time),
}

// RegisterPeer adds an IP address to the matrix and updates its vitality timestamp
func (pr *PeerRoster) RegisterPeer(ip string) bool {
	pr.Lock()
	defer pr.Unlock()

	// Normalize IP string to stripping out incoming socket ports if accidentally passed
	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		ip = host
	}

	_, exists := pr.ActiveAddresses[ip]
	pr.ActiveAddresses[ip] = time.Now()

	if !exists {
		fmt.Printf("🌐 [GOSSIP MESH] Registered 1 new node cluster entry: %s\n", ip)
		return true // New discovery
	}
	return false // Existing node tracking updated
}

// GetPeerList serializes the active network map to share with dialing nodes
func (pr *PeerRoster) GetPeerList() []string {
	pr.RLock()
	defer pr.RUnlock()

	var list []string
	// Filter out stale peers older than 30 minutes to maintain ledger velocity
	cutoff := time.Now().Add(-30 * time.Minute)

	for ip, lastSeen := range pr.ActiveAddresses {
		if lastSeen.After(cutoff) {
			list = append(list, ip)
		}
	}
	return list
}

// BroadcastNewBlock payload streams newly solved blocks straight out to the peer network array
func BroadcastNewBlock(blockData interface{}) {
	// 1. Fetch your complete, synchronized list of known active network nodes
	peers := GlobalRoster.GetPeerList()
	if len(peers) == 0 {
		return // No external nodes active yet, maintain solo loop
	}

	// 2. Serialize your block structure parameters into a compressed JSON payload string
	payload, err := json.Marshal(blockData)
	if err != nil {
		fmt.Printf("🚨 [GOSSIP MESH] Serialization Error formatting block payload: %v\n", err)
		return
	}
	msg := fmt.Sprintf("BLOCK_PROPAGATE:%s\n", string(payload))

	// 3. Systematically loop through your active subnets and stream the network broadcast
	for _, ip := range peers {
		// Secure loop scoping to completely eliminate Go-routine variable race risks
		targetIP := ip 

		go func(target string) {
			// Dial their consensus wire gateway channel handle with a strict 5-second timeout boundary
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(target, "8080"), 5*time.Second)
			if err != nil {
				return // Peer node unavailable, skip silently to protect threads
			}
			defer conn.Close()

			// Set a write deadline to protect against slow-loris hanging network pipes
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

			// Transmit the solved block nonce parameters out to their consensus handle
			fmt.Fprint(conn, msg)
		}(targetIP)
	}
}
// StartDynamicPeerPruningHeartbeat continuously audits the active GlobalRoster entries.
// It forcefully purges offline or unresponsive network coordinates every 60 seconds.
func StartDynamicPeerPruningHeartbeat() {
	fmt.Println("🛰️  [PEER HEALTH ENGINE] Dynamic network pruning heartbeat daemon successfully activated.")
	
	ticker := time.NewTicker(60 * time.Second) // Audits the global mesh network topology once every minute
	go func() {
		for range ticker.C {
			GlobalRoster.Lock()
			activeSnapshot := make([]string, 0)
			for peerIP := range GlobalRoster.ActiveAddresses {
				activeSnapshot = append(activeSnapshot, peerIP)
			}
			GlobalRoster.Unlock()

			if len(activeSnapshot) == 0 {
				continue
			}

			fmt.Printf("🔍 [PEER HEALTH AUDIT] Auditing %d active network nodes for heartbeat responses...\n", len(activeSnapshot))

			for _, peerIP := range activeSnapshot {
				// Establish a fast 3-second diagnostic TCP probe dial to their consensus port 8080
				conn, err := net.DialTimeout("tcp", net.JoinHostPort(peerIP, "8080"), 3*time.Second)
				
				if err != nil {
					GlobalRoster.Lock()
					delete(GlobalRoster.ActiveAddresses, peerIP)
					GlobalRoster.Unlock()
					fmt.Printf("   ❌ Node %s failed health check (Offline/Timeout). Pruned from roster.\n", peerIP)
				} else {
					conn.Close()
				}
				
				// ✅ FIXED: Inject a 250ms pacing delay between individual node checks 
				// to completely eliminate port exhaustion and thread choking across your VPS hubs!
				time.Sleep(250 * time.Millisecond)
			}
		}
	}()
}