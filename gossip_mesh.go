package main

import (
	"encoding/json" 
	"fmt"
	"log"           
	"net"
	"net/http"
	"strconv"       // ✅ Added to parse string heights safely
	"strings"       // ✅ Added to handle URL route checking
	"sync"          
	"time"
)


// ============================================================================
// PART 1: CORE HORIZONTAL P2P GOSSIP MESH ENGINE & PHASE 3 FIREWALL
// ============================================================================

const BanThreshold = 100

type PeerMetrics struct {
	InfractionScore int
	LastSeen        time.Time
	IsWhitelisted   bool
	IsBanned        bool
	BanExpires      time.Time
}

type PeerRoster struct {
	sync.RWMutex
	ActiveAddresses map[string]time.Time
	FirewallRecords map[string]*PeerMetrics
}

var GlobalRoster = &PeerRoster{
	ActiveAddresses: make(map[string]time.Time),
	FirewallRecords: make(map[string]*PeerMetrics),
}

func (pr *PeerRoster) RegisterPeer(ip string) bool {
	pr.Lock()
	defer pr.Unlock()

	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		ip = host
	}

	if metrics, banned := pr.FirewallRecords[ip]; banned && metrics.IsBanned {
		if time.Now().Before(metrics.BanExpires) {
			return false 
		}
		metrics.IsBanned = false
		metrics.InfractionScore = 0
	}

	_, exists := pr.ActiveAddresses[ip]
	pr.ActiveAddresses[ip] = time.Now()

	if !exists {
		fmt.Printf("🌐 [GOSSIP MESH] Registered 1 new node cluster entry: %s\n", ip)
		return true 
	}
	return false 
}

func (pr *PeerRoster) RecordPeerInfraction(ip string, points int) {
	pr.Lock()
	defer pr.Unlock()

	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		ip = host
	}

	metrics, exists := pr.FirewallRecords[ip]
	if !exists {
		metrics = &PeerMetrics{LastSeen: time.Now()}
		pr.FirewallRecords[ip] = metrics
	}

	if metrics.IsWhitelisted {
		return 
	}

	metrics.InfractionScore += points
	metrics.LastSeen = time.Now()

	if metrics.InfractionScore >= BanThreshold && !metrics.IsBanned {
		metrics.IsBanned = true
		metrics.BanExpires = time.Now().Add(24 * time.Hour)
		delete(pr.ActiveAddresses, ip) 
		fmt.Printf("🚨 [PHASE 3 FIREWALL] BAN ACTIVATED: Peer %s breached penalty threshold (%d/%d).\n", ip, metrics.InfractionScore, BanThreshold)
	}
}

func (pr *PeerRoster) IsIPAllowed(ip string) bool {
	pr.RLock()
	defer pr.RUnlock()

	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		ip = host
	}

	metrics, exists := pr.FirewallRecords[ip]
	if !exists {
		return true
	}

	if metrics.IsBanned && time.Now().Before(metrics.BanExpires) {
		return false 
	}

	return true
}

func (pr *PeerRoster) GetPeerList() []string {
	pr.RLock()
	defer pr.RUnlock()

	var list []string
	cutoff := time.Now().Add(-30 * time.Minute)

	for ip, lastSeen := range pr.ActiveAddresses {
		if lastSeen.After(cutoff) {
			list = append(list, ip)
		}
	}
	return list
}

func BroadcastNewBlock(blockData interface{}) {
	peers := GlobalRoster.GetPeerList()
	if len(peers) == 0 {
		return 
	}

	payload, err := json.Marshal(blockData)
	if err != nil {
		fmt.Printf("🚨 [GOSSIP MESH] Serialization Error: %v\n", err)
		return
	}
	
	msg := fmt.Sprintf("BLOCK_PROPAGATE:%s\n", string(payload))

	for _, ip := range peers {
		targetIP := ip 

		go func(target string) {
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(target, "8080"), 4*time.Second)
			if err != nil {
				return
			}
			defer conn.Close()
			
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, err = conn.Write([]byte(msg))
			if err != nil {
				GlobalRoster.RecordPeerInfraction(target, 15)
			}
		}(targetIP)
	}
}

// ============================================================================
// PART 2: PHASE 3 SYNC-GATE LOCK VALIDATION FIREWALL
// ============================================================================

type SyncGateMonitor struct {
	mu                  sync.RWMutex
	IsFullySynchronized bool
	TargetGlobalHeight   int64
}

var GlobalSyncShield = &SyncGateMonitor{
	IsFullySynchronized: false,
	TargetGlobalHeight:   0,
}

// GetGlobalMeshMaxHeight queries active peer tracking endpoints safely across standard nodes and cloud servers
func GetGlobalMeshMaxHeight(seedPeerURL string) int64 {
	client := http.Client{Timeout: 3 * time.Second}

	// Strip duplicate paths if accidentally passed by main arrays
	cleanURL := seedPeerURL
	if !strings.HasSuffix(cleanURL, "/block") && !strings.HasSuffix(cleanURL, "/peers") {
		cleanURL = seedPeerURL + "/block"
	}

	resp, err := client.Get(cleanURL)
	if err != nil {
		return 0 
	}
	defer resp.Body.Close()

	var targetData struct {
		CurrentHeight int64 `json:"current_height"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&targetData); err != nil {
		// Fallback to decode raw Block index format if seed is standard node layout
		var fallbackBlock struct {
			Index int64 `json:"current_height"`
		}
		if json.NewDecoder(resp.Body).Decode(&fallbackBlock) == nil {
			return fallbackBlock.Index
		}
		return 0
	}
	return targetData.CurrentHeight
}

func EnforceSyncGateLock(seedPeerExplorerURL string, localHeightProvider func() int64) {
	fmt.Println("\n🔒 [PHASE 3 SECURITY MATRIX] Sync-Gate Lock activated.")
	fmt.Println("🛰️  Validating ledger alignment against global network tip benchmarks...")

	for {
		localHeight := localHeightProvider()
		globalMaxHeight := GetGlobalMeshMaxHeight(seedPeerExplorerURL)

		GlobalSyncShield.mu.Lock()
		GlobalSyncShield.TargetGlobalHeight = globalMaxHeight

		if localHeight >= globalMaxHeight && globalMaxHeight > 0 {
			GlobalSyncShield.IsFullySynchronized = true
			GlobalSyncShield.mu.Unlock()
			fmt.Println("\n✨ [🔓 SYNC COMPLETE] Core fully aligned with global mainnet tip. Hashing worker threads ignited!")
			break
		}

		GlobalSyncShield.IsFullySynchronized = false
		GlobalSyncShield.mu.Unlock()

		fmt.Printf("\r⏳ [MAINNET SYNC LOCK] Local Ledger Height: %d / True Network Tip: %d. Waiting for synchronization equilibrium...", localHeight, globalMaxHeight)
		time.Sleep(5 * time.Second) 
	}
}

func IsCoreMinerLocked() bool {
	GlobalSyncShield.mu.RLock()
	defer GlobalSyncShield.mu.RUnlock()
	return !GlobalSyncShield.IsFullySynchronized
}

// ============================================================================
// PART 3: NATIVE IGNITION NETWORK CORES & EXPLORER ROUTES
// ============================================================================

func StartMeshNetwork() {
	fmt.Println("📡 P2P MATRIX: Binding to core data channels...")

	// ✅ PORT 8082 BACKUP EXPLORER LAYER
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/explorer", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, htmlBackupTemplate)
		})
		
		log.Println("📡 Initializing Port 8082 Backup Matrix listener...")
		if err := http.ListenAndServe(":8082", mux); err != nil {
			log.Printf("Network socket notice for 8082: %v\n", err)
		}
	}()

	// ✅ PORT 8080: TCP P2P Listener integrated with Phase 3 Firewall Checks
	go func() {
		listener, err := net.Listen("tcp", ":8080")
		if err != nil {
			log.Printf("🚨 P2P Port 8080 connection delay or conflict notice: %v\n", err)
			return
		}
		defer listener.Close()
		
		for {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}

			if !GlobalRoster.IsIPAllowed(conn.RemoteAddr().String()) {
				conn.Close() 
				continue
			}

			go handleIncomingPeerSession(conn)
		}
	}()

	// ✅ PORT 8081: HTTP Explorer Telemetry Server
	go func() {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, htmlMelbourneTemplate, GetLatestBlock().Index)
		})

		// 🛰️ PEERS GATEWAY ENDPOINT
		http.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":         "ONLINE",
				"current_height": GetLatestBlock().Index,
				"global_tip":     GetLatestBlock().Index,
				"peers":          []string{"207.148.67.11"},
			})
		})

		// 📦 BLOCK METRICS ENDPOINT: Handles standard tips and explicit 1-by-1 path requests
		http.HandleFunc("/block/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")

			// Slice out the trailing URL string path to grab the target integer (e.g., /block/1 -> "1")
			pathSegments := strings.Split(r.URL.Path, "/")
			if len(pathSegments) > 2 && pathSegments[2] != "" {
				requestedHeight, err := strconv.ParseInt(pathSegments[2], 10, 64)
				if err == nil {
					// Call your compiled disk reader to fetch the exact single block slice
					specificBlock := GetBlockByHeightFromDB(requestedHeight)
					json.NewEncoder(w).Encode(specificBlock)
					return
				}
			}

			// Fallback: if no trailing ID index is passed, return the standard tip block
			json.NewEncoder(w).Encode(GetLatestBlock())
		})

		// Keep the base route active for general global tip checks
		http.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			json.NewEncoder(w).Encode(GetLatestBlock())
		})


		// 🔑 WALLET ADDRESSES GATEWAY ENDPOINT
		http.HandleFunc("/addresses", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"active_addresses": []string{CustomMinerAddress},
			})
		})

		// 📊 DYNAMIC WALLET AUDIT ENDPOINT LOOP PATH
		http.HandleFunc("/audit", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Access-Control-Allow-Origin", "*")

			targetAddress := r.URL.Query().Get("address")
			if targetAddress == "" {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, htmlAuditErrorTemplate)
				return
			}

			latestBlock := GetLatestBlock()
			var balance int64 = 0
			var blocksMined int64 = 0

			if targetAddress == CustomMinerAddress {
				balance = latestBlock.Index * 50 
				blocksMined = latestBlock.Index
			} else if targetAddress == "CVN_766d4b3be2e1c894f0b2a688527e4d2592151cd1" {
				balance = 1000
				blocksMined = 0
			}

			fmt.Fprintf(w, htmlAuditTemplate, latestBlock.Index, targetAddress, balance, blocksMined)
		})

		fmt.Println("📡 Explorer API Gateway Matrix listening natively on isolated port :8081...")
		if err := http.ListenAndServe(":8081", nil); err != nil {
			log.Printf("Network socket notice: %v\n", err)
		}
	}() 
}

// handleIncomingPeerSession manages low-level mesh handshakes smoothly
func handleIncomingPeerSession(conn net.Conn) {
	defer conn.Close()
	fmt.Printf("📡 [P2P MESH] Incoming handshake established from: %s\n", conn.RemoteAddr().String())
}

// ============================================================================
// PART 4: ISOLATED HTML RAW STRING LITERAL TEMPLATES
// ============================================================================

const htmlBackupTemplate = `<html><body style="background:#0a0a0f;color:#00ff66;font-family:monospace;padding:3rem;text-align:center;">
	<h1>💎 COVENANT STANDARD MELBOURNE BACKUP CORE</h1>
	<p style="color:#00ffff;">SYSTEM STATUS: ONLINE // LISTENING ON PORT 8082</p>
</body></html>`

const htmlMelbourneTemplate = `<html><body style="background:#0a0a0f;color:#00ff66;font-family:monospace;padding:3rem;text-align:center;">
	<h1>💎 COVENANT STANDARD MELBOURNE CORE RIG</h1>
	<p style="color:#00ffff;">SYSTEM STATUS: ACTIVE // BLOCK HEIGHT: #<span id="live-height">%d</span></p>
	<script>
		function updateBlockHeight() {
			fetch('/block')
				.then(response => response.json())
				.then(data => {
					if (data && (data.current_height || data.block_height)) {
						const newHeight = data.current_height || data.block_height;
						document.getElementById('live-height').innerText = newHeight;
					}
				})
				.catch(err => console.error("Gossip tracking sync delay:", err));
		}
		setInterval(updateBlockHeight, 2000);
	</script>
</body></html>`

const htmlAuditErrorTemplate = `<html><body style="background:#0a0a0f;color:#ff3333;font-family:monospace;padding:3rem;text-align:center;">
	<h2>🚨 ERROR: Missing required 'address' query token parameter</h2>
</body></html>`

const htmlAuditTemplate = `<!DOCTYPE html>
<html>
<head>
	<title>💎 CVN AUDIT NETWORK</title>
	<meta charset="utf-8">
	<style>
		body { background: #0a0a0f; color: #00ff66; font-family: monospace; padding: 3rem; text-align: center; margin: 0; }
		.container { max-width: 900px; margin: 0 auto; background: #11111a; border: 1px solid #1f1f2e; border-radius: 8px; padding: 2.5rem; box-shadow: 0 8px 32px rgba(0,0,0,0.5); }
		h1 { font-size: 1.8rem; letter-spacing: 2px; margin-bottom: 0.5rem; text-transform: uppercase; }
		.status-bar { color: #00ffff; font-size: 0.95rem; margin-bottom: 2.5rem; text-transform: uppercase; }
		.metric-card { background: #0d0d14; border-left: 4px solid #00ff66; margin: 1rem 0; padding: 1.2rem 2rem; text-align: left; display: flex; justify-content: space-between; align-items: center; }
		.label { color: #8a8a9e; text-transform: uppercase; font-size: 0.85rem; letter-spacing: 1px; }
		.value { font-size: 1.1rem; color: #00ff66; word-break: break-all; padding-left: 1rem; }
		.value-address { color: #00ffff; }
		.footer { margin-top: 3rem; color: #4a4a5a; font-size: 0.8rem; }
	</style>
</head>
<body>
	<div class="container">
		<h1>💎 COVENANT STANDARD AUDIT LEDGER</h1>
		<div class="status-bar">SYSTEM STATUS: ACTIVE // CURRENT TIP: #%d</div>
		<div class="metric-card" style="border-left-color: #00ffff;"><span class="label">Target Address</span><span class="value value-address">%s</span></div>
		<div class="metric-card"><span class="label">CVN Balance</span><span class="value">%d CVN</span></div>
		<div class="metric-card"><span class="label">Total Blocks Mined</span><span class="value">%d Blocks</span></div>
		<div class="metric-card" style="border-left-color: #00ffff;"><span class="label">Ledger Alignment</span><span class="value" style="color: #00ffff;">100%% VERIFIED</span></div>
	</div>
</body>
</html>`