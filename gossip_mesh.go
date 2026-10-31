package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

)

var (
	ActivePeerMap         = make(map[string]time.Time)
	PeerMapMutex          sync.RWMutex
	MaxMeshConnectionPool = make(chan struct{}, 100)
)

func StartMeshNetwork() {
	fmt.Println("====================================================================")
	fmt.Println("📡 P2P GOSSIP MESH CORE ENGINE INITIALIZING ON PORT 8080...")
	fmt.Println("====================================================================")
	
	listener8080, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Printf("🚨 CRITICAL MESH FAILURE: Unable to bind raw TCP socket on Port 8080: %v\n", err)
		return
	}
	defer listener8080.Close()
	
	fmt.Println("📡 P2P MATRIX: Successfully bound to core data channels on port :8080")
	go runNetworkTopologyDiscoveryDiscoveryLoop()

	for {
		conn, err := listener8080.Accept()
		if err != nil {
			continue
		}
		
		select {
		case MaxMeshConnectionPool <- struct{}{}:
			go handleIncomingPeerStream(conn)
		default:
			log.Printf("⚠️ TRAFFIC THRESHOLD SATURATED: Dropping connection from %s\n", conn.RemoteAddr())
			conn.Close()
		}
	}
}

func handleIncomingPeerStream(conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	defer func() {
		conn.Close()
		<-MaxMeshConnectionPool
	}()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	scanner := bufio.NewScanner(conn)

	for scanner.Scan() {
		msg := scanner.Text()
		msg = strings.TrimSpace(msg)
		if msg == "" {
			continue
		}
		if !strings.HasPrefix(msg, "BLOCK_BROADCAST:") && !strings.HasPrefix(msg, "TX_BROADCAST:") && !strings.HasPrefix(msg, "PEER_PING:") && !strings.HasPrefix(msg, "PEER_LIST:") && !strings.HasPrefix(msg, "GET_BATCH:") {
			return
		}

		if atomic.LoadInt32(&IsSyncingActive) == 1 && strings.HasPrefix(msg, "BLOCK_BROADCAST:") {
			log.Println("📥 [STALL SNAPSHOT] Real-time BLOCK_BROADCAST deferred from peer during active re-sync.")
		}

		if strings.HasPrefix(msg, "PEER_PING:") {
			PeerMapMutex.Lock()
			ActivePeerMap[remoteAddr] = time.Now()
			PeerMapMutex.Unlock()
			PeerMapMutex.RLock()
			var peers []string
			for p := range ActivePeerMap {
				peers = append(peers, p)
			}
			PeerMapMutex.RUnlock()
			listBytes, _ := json.Marshal(peers)
			_, _ = fmt.Fprintf(conn, "PEER_LIST:%s\n", string(listBytes))
			continue
		}

		if strings.HasPrefix(msg, "TX_BROADCAST:") {
			txPayload := strings.TrimPrefix(msg, "TX_BROADCAST:")
			var incomingTx Transaction
			if err := json.Unmarshal([]byte(txPayload), &incomingTx); err == nil {
				if VerifyIncomingWireTransaction(incomingTx) {
					MempoolMatrix.PushTransaction(incomingTx)
					go BroadcastNewTransaction(incomingTx)
				}
			}
			continue
		}

		if strings.HasPrefix(msg, "BLOCK_BROADCAST:") {
			blockPayload := strings.TrimPrefix(msg, "BLOCK_BROADCAST:")
			var incomingBlock Block
			if err := json.Unmarshal([]byte(blockPayload), &incomingBlock); err == nil {
				if VerifyIncomingWireBlock(incomingBlock) {
					go ProcessIncomingNetworkBlock(incomingBlock)
				}
			}
			continue
		}

		// 👑 THE CRITICAL P2P DATA COMPILED DISPATCHER:
		// When a lagging peer sends a GET_BATCH command, parse the target start index,
		// load the historical blocks from disk buckets, and serialize them straight back down the wire socket!
		if strings.HasPrefix(msg, "GET_BATCH:") {
			heightStr := strings.TrimPrefix(msg, "GET_BATCH:")
			startHeight, err := strconv.ParseInt(strings.TrimSpace(heightStr), 10, 64)
			if err != nil || startHeight < 0 {
				continue
			}

			var blocks []Block
			StateMutex.RLock()
			currentTipHeight := GetLatestBlockInternal().Index
			StateMutex.RUnlock()

			endHeight := startHeight + 511
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
				_, _ = conn.Write(append(jsonBytes, '\n'))
			}
			continue
		}
	}
}

func BroadcastNewBlock(b Block) {
	jsonBytes, err := json.Marshal(b)
	if err != nil {
		return
	}
	payload := fmt.Sprintf("BLOCK_BROADCAST:%s\n", string(jsonBytes))
	transmitPayloadToNetworkBoundary(payload)
}

func BroadcastNewTransaction(tx Transaction) {
	jsonBytes, err := json.Marshal(tx)
	if err != nil {
		return
	}
	payload := fmt.Sprintf("TX_BROADCAST:%s\n", string(jsonBytes))
	transmitPayloadToNetworkBoundary(payload)
}

func transmitPayloadToNetworkBoundary(payload string) {
	PeerMapMutex.RLock()
	var targets []string
	for peer := range ActivePeerMap {
		targets = append(targets, peer)
	}
	PeerMapMutex.RUnlock()

	for _, address := range targets {
		if strings.Contains(address, "202.137.175.220") && runtime.GOOS == "windows" {
			continue
		}
		go func(addr string) {
			conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
			if err == nil {
				_, _ = fmt.Fprint(conn, payload)
				conn.Close()
			}
		}(address)
	}
}

func runNetworkTopologyDiscoveryDiscoveryLoop() {
	staticMeshSeeds := []string{
		"202.137.175.220:8080",
		"207.148.67.11:8080",
		"64.177.45.153:8080",
	}

	for {
		for _, seedAddr := range staticMeshSeeds {
			go func(addr string) {
				conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
				if err == nil {
					_, _ = fmt.Fprintf(conn, "PEER_PING:DISCOVERY_REQUEST\n")
					reader := bufio.NewReader(conn)
					responseString, readErr := reader.ReadString('\n')

					if readErr == nil {
						responseString = strings.TrimSpace(responseString)
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
												_, _ = fmt.Fprintf(dConn, "PEER_PING:DISCOVERY_REQUEST\n")
												dConn.Close()
											}
										}(cleanedIP)
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