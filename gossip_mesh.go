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

// 🟩 FIX 2: Explicitly name input interface parameter variable 'conn' to rectify type-scoping errors
func handleIncomingPeerStream(conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	remoteIP, _, _ := net.SplitHostPort(remoteAddr)

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

		if strings.HasPrefix(msg, "GET_BATCH:") {
			heightStr := strings.TrimPrefix(msg, "GET_BATCH:")
			startHeight, err := strconv.ParseInt(heightStr, 10, 64)
			if err != nil || startHeight < 0 {
				return
			}

			var blocks []Block
			
			StateMutex.RLock()
			currentTip := GetLatestBlockInternal().Index
			StateMutex.RUnlock()

			endHeight := startHeight + 511
			if endHeight > currentTip {
				endHeight = currentTip
			}

			for h := startHeight; h <= endHeight; h++ {
				blocks = append(blocks, GetBlockByHeightFromDB(h))
			}

			jsonBytes, err := json.Marshal(blocks)
			if err == nil {
				_, _ = conn.Write(append(jsonBytes, '\n'))
			}
			return
		}

		if strings.HasPrefix(msg, "TX_BROADCAST:") {
			txPayload := strings.TrimPrefix(msg, "TX_BROADCAST:")
			var incomingTx Transaction
			if err := json.Unmarshal([]byte(txPayload), &incomingTx); err == nil {
				if VerifyIncomingWireTransaction(incomingTx) {
					if MempoolMatrix.PushTransaction(incomingTx) {
						go BroadcastTransactionToMesh(incomingTx)
					}
				} else {
					log.Printf("🚨 [GATE FIREWALL] Dropped unauthenticated or spent transaction: %s\n", incomingTx.ID)
				}
			}
			return
		}

		if strings.HasPrefix(msg, "BLOCK_BROADCAST:") {
			blockPayload := strings.TrimPrefix(msg, "BLOCK_BROADCAST:")
			var incomingBlock Block
			if err := json.Unmarshal([]byte(blockPayload), &incomingBlock); err == nil {
				if VerifyIncomingWireBlock(incomingBlock) {
					go ProcessIncomingNetworkBlock(incomingBlock)
				} else {
					log.Printf("🚨 [GATE FIREWALL] Block #%d failed deep front-gate audit! Discarding.\n", incomingBlock.Index)
				}
			}
			return
		}

		if strings.HasPrefix(msg, "PEER_PING:") {
			PeerMapMutex.Lock()
			peerMeshKey := fmt.Sprintf("%s:8080", remoteIP)
			if _, exists := ActivePeerMap[peerMeshKey]; !exists {
				fmt.Printf("🌐 [GOSSIP MESH] Handshake verified from raw cloud seed source: %s\n", peerMeshKey)
			}
			ActivePeerMap[peerMeshKey] = time.Now()
			PeerMapMutex.Unlock()
			return
		}
	}
}

func BroadcastNewBlock(block Block) {
	jsonBytes, err := json.Marshal(block)
	if err != nil {
		return
	}
	payload := fmt.Sprintf("BLOCK_BROADCAST:%s\n", string(jsonBytes))
	transmitPayloadToNetworkBoundary(payload)
}

func BroadcastTransactionToMesh(tx Transaction) {
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
			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
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
					conn.Close()
				}
			}(seedAddr)
		}

		time.Sleep(20 * time.Second)

		PeerMapMutex.Lock()
		for peer, lastSeen := range ActivePeerMap {
			if time.Since(lastSeen) > (2 * time.Minute) {
				delete(ActivePeerMap, peer)
				fmt.Printf("❌ [GOSSIP MESH] Severed inactive channel: %s\n", peer)
			}
		}
		PeerMapMutex.Unlock()
	}
}