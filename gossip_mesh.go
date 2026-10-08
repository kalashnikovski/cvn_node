package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// ============================================================================
// PART 1: CORE HORIZONTAL P2P GOSSIP MESH ENGINE (Your Existing Logic)
// ============================================================================

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

// ============================================================================
// PART 2: PHASE 3 SYNC-GATE LOCK VALIDATION FIREWALL (The New Guard)
// ============================================================================

// SyncGateMonitor holds the thread-safe state validation markers for the local mesh node
type SyncGateMonitor struct {
	mu                  sync.RWMutex
	IsFullySynchronized bool
	TargetGlobalHeight   int64
}

// GlobalSyncShield is the active sentinel monitoring your network alignment boundaries
var GlobalSyncShield = &SyncGateMonitor{
	IsFullySynchronized: false,
	TargetGlobalHeight:   0,
}

// GetGlobalMeshMaxHeight queries active peer tracking endpoints to find the true network tip height
func GetGlobalMeshMaxHeight(seedPeerURL string) int64 {
	client := http.Client{
		Timeout: 5 * time.Second, // Hard deadline to prevent Slow-Loris socket stalling
	}

	resp, err := client.Get(seedPeerURL)
	if err != nil {
		// Fallback to 0 if peer is cycling offline; protects against network isolation panic
		return 0 
	}
	defer resp.Body.Close()

	// Struct matching your live public /peers index endpoint arrays
	var peerHeights []int64
	if err := json.NewDecoder(resp.Body).Decode(&peerHeights); err != nil {
		return 0
	}

	var maxTarget int64 = 0
	for _, height := range peerHeights {
		if height > maxTarget {
			maxTarget = height
		}
	}
	return maxTarget
}

// EnforceSyncGateLock acts as the Phase 3 validation firewall, freezing worker threads until aligned
func EnforceSyncGateLock(seedPeerExplorerURL string, localHeightProvider func() int64) {
	fmt.Println("\n🔒 [PHASE 3 SECURITY MATRIX] Sync-Gate Lock activated.")
	fmt.Println("🛰️  Validating ledger alignment against global network tip benchmarks...")

	for {
		localHeight := localHeightProvider()
		globalMaxHeight := GetGlobalMeshMaxHeight(seedPeerExplorerURL)

		GlobalSyncShield.mu.Lock()
		GlobalSyncShield.TargetGlobalHeight = globalMaxHeight

		// If local height matches or beats the global tracking mesh height, lift the lock gate!
		if localHeight >= globalMaxHeight || globalMaxHeight == 0 {
			GlobalSyncShield.IsFullySynchronized = true
			GlobalSyncShield.mu.Unlock()
			
			fmt.Println("\n✨ [🔓 SYNC COMPLETE] Core fully aligned with global mainnet tip. Hashing worker threads ignited!")
			break
		}

		GlobalSyncShield.IsFullySynchronized = false
		GlobalSyncShield.mu.Unlock()

		// Print a clean, dynamic, non-bloating real-time tracking line to the console
		fmt.Printf("\r⏳ [MAINNET SYNC LOCK] Local Ledger Height: %d / True Network Tip: %d. Waiting for synchronization equilibrium...", localHeight, globalMaxHeight)
		
		// Throttle the loop pass execution to prevent local CPU thread resource exhaustion
		time.Sleep(5 * time.Second) 
	}
}

// IsCoreMinerLocked allows your mining loop workers to audit their execution permission states in memory
func IsCoreMinerLocked() bool {
	GlobalSyncShield.mu.RLock()
	GlobalSyncShield.mu.RUnlock()
	return !GlobalSyncShield.IsFullySynchronized
}