package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
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
			log.Printf("⚠️  TRAFFIC THRESHOLD SATURATED: Dropping incoming socket connection from %s to secure memory blocks.\n", conn.RemoteAddr())
			conn.Close()
		}
	}
}

func handleIncomingPeerStream(conn net.Conn) {
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
			currentTip := GetLatestBlock().Index

			endHeight := startHeight + 511
			if endHeight > currentTip {
				endHeight = currentTip
			}

			for h := startHeight; h <= endHeight; h++ {
				blk := GetBlockByHeightFromDB(h)
				blocks = append(blocks, blk)
			}
			jsonBytes, err := json.Marshal(blocks)
			if err == nil {
				_, _ = conn.Write(append(jsonBytes, '\n'))
			}
			return
		}
		if strings.HasPrefix(msg, "TX_Broadcast:") {
			txPayload := strings.TrimPrefix(msg, "TX_Broadcast:")
			var incomingTx Transaction
			if err := json.Unmarshal([]byte(txPayload), &incomingTx); err == nil {
				if MempoolMatrix.PushTransaction(incomingTx) {
					go BroadcastTransactionToMesh(incomingTx)
				}
			}
			return
		}
		if strings.HasPrefix(msg, "BLOCK_BROADCAST:") {
			blockPayload := strings.TrimPrefix(msg, "BLOCK_BROADCAST:")
			var incomingBlock Block
			if err := json.Unmarshal([]byte(blockPayload), &incomingBlock); err == nil {
				go ProcessIncomingNetworkBlock(incomingBlock)
			}
			return
		}
		if strings.HasPrefix(msg, "PEER_PING:") {
			peerAddr := strings.TrimPrefix(msg, "PEER_PING:")
			PeerMapMutex.Lock()
			if _, exists := ActivePeerMap[peerAddr]; !exists {
				fmt.Printf("🌐 [GOSSIP MESH] Registered 1 new node cluster entry: %s\n", peerAddr)
			}
			ActivePeerMap[peerAddr] = time.Now()
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
	payload := fmt.Sprintf("TX_BROADcast:%s\n", string(jsonBytes))
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
	myPublicIdentitySignature := "UNCONFIGURED_PEER"
	if CustomMinerAddress != "" {
		myPublicIdentitySignature = CustomMinerAddress
	}
	for {
		for _, seedAddr := range staticMeshSeeds {
			go func(addr string) {
				conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
				if err == nil {
					_, _ = fmt.Fprintf(conn, "PEER_PING:%s:8080\n", myPublicIdentitySignature)
					conn.Close()
				}
			}(seedAddr)
		}
		time.Sleep(30 * time.Second)
		PeerMapMutex.Lock()
		for peer, lastSeen := range ActivePeerMap {
			if time.Since(lastSeen) > (3 * time.Minute) {
				delete(ActivePeerMap, peer)
				fmt.Printf("❌ [GOSSIP MESH] Dropped inactive peer node channel: %s\n", peer)
			}
		}
		PeerMapMutex.Unlock()
	}
}
