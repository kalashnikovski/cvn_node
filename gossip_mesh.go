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

	"go.etcd.io/bbolt"
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

		// 👑 THE MASTER FIREWALL NETWORK SHIELD:
		// Forcefully change 'break' to 'return' right here!
		// This guarantees that any non-protocol scanner spam or HTTP proxy probe drops out of
		// the ENTIRE function call instantly, closing the TCP socket, freeing up system memory,
		// and keeping your cloud servers 100% unblocked to mine nonces independently!
		if !strings.HasPrefix(msg, "BLOCK_BROADCAST:") &&
			!strings.HasPrefix(msg, "TX_BROADCAST:") &&
			!strings.HasPrefix(msg, "PEER_PING:") &&
			!strings.HasPrefix(msg, "PEER_LIST:") &&
			!strings.HasPrefix(msg, "GET_BATCH:") {
			return // 👑 Terminates and drops the dead socket immediately!
		}

		preview := msg
		if len(preview) > 120 {
			preview = preview[:120]
		}
		fmt.Printf("📡 [GOSSIP] Received: %s\n", preview)

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
					go ProcessIncomingWireBlockWithDelay(incomingBlock)
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
			
			var activePeersList []string
			for currentPeer := range ActivePeerMap {
				activePeersList = append(activePeersList, currentPeer)
			}
			PeerMapMutex.Unlock()

			jsonPeers, err := json.Marshal(activePeersList)
			if err == nil {
				payloadResponse := fmt.Sprintf("PEER_LIST:%s\n", string(jsonPeers))
				_, _ = fmt.Fprint(conn, payloadResponse)
			}
			return
		}

		if strings.HasPrefix(msg, "PEER_LIST:") {
			rawPayload := strings.TrimPrefix(msg, "PEER_LIST:")
			var receivedAddresses []string
			if err := json.Unmarshal([]byte(rawPayload), &receivedAddresses); err != nil {
				return
			}

			for _, potentialPeer := range receivedAddresses {
				cleanedPeer := strings.TrimSpace(strings.ReplaceAll(potentialPeer, "\r", ""))
				if cleanedPeer == "" {
					continue
				}

				if strings.HasPrefix(cleanedPeer, "127.0.0.1") || strings.HasPrefix(cleanedPeer, "localhost") {
					continue
				}

				PeerMapMutex.RLock()
				_, alreadyConnected := ActivePeerMap[cleanedPeer]
				PeerMapMutex.RUnlock()

				if !alreadyConnected {
					go func(targetIP string) {
						localInterfaces, err := net.InterfaceAddrs()
						if err == nil {
							for _, interfaceAddr := range localInterfaces {
								if ipnet, ok := interfaceAddr.(*net.IPNet); ok {
									if strings.Contains(targetIP, ipnet.IP.String()) {
										return
									}
								}
							}
						}

						dialerConnection, dialErr := net.DialTimeout("tcp", targetIP, 2*time.Second)
						if dialErr != nil {
							return
						}
						defer dialerConnection.Close()

						_, _ = fmt.Fprintf(dialerConnection, "PEER_PING:DISCOVERY_REQUEST\n")
					}(cleanedPeer)
				}
			}
			return
		}
	}
}

// ProcessIncomingWireBlockWithDelay handles block integration deterministically without async race conditions!
func ProcessIncomingWireBlockWithDelay(incomingBlock Block) {
	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		return
	}

	StateMutex.Lock()
	currentTip := GetLatestBlockInternal()
	StateMutex.Unlock()

	// 👑 THE UN-FORKABLE SEQUENTIAL GATEWAY:
	if incomingBlock.Index == currentTip.Index+1 {
		if incomingBlock.PrevHash != currentTip.Hash {
			go TriggerActiveReSyncPass()
			return
		}

		if SaveBlockToStorage(incomingBlock) {
			atomic.StoreInt64(&localHeightState, incomingBlock.Index)
			go BroadcastNewBlock(incomingBlock)
		}
		return
	}

	// 👑 THE GHOST UNCLE INTEGRATION GATEWAY:
	if incomingBlock.Index == currentTip.Index && incomingBlock.Hash != currentTip.Hash {
		if incomingBlock.PrevHash == currentTip.PrevHash {
			fmt.Printf("🛰️  [UNCLE RECOGNIZED] Capturing late competing block hash %s for height %d\n", incomingBlock.Hash[:16], incomingBlock.Index)
			
			_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
				bBlocks := tx.Bucket(blocksBucket)
				if bBlocks == nil {
					return nil
				}
				
				paddedKey := fmt.Sprintf("%016d", currentTip.Index)
				if currentTip.UncleHash == "" || currentTip.UncleHash == "0000000000000000000000000000000000000000000000000000000000000000" {
					currentTip.UncleHash = incomingBlock.Hash
					
					// 👑 THE CRITICAL SUBSCRIPT REPAIR:
					// Securely check boundaries and isolate index slot 0 (The Coinbase Transaction)
					// to extract the Witness profile string identity cleanly without thread panics!
					if len(incomingBlock.Transactions) > 0 {
						currentTip.UncleMiner = incomingBlock.Transactions[0].Witness
					} else {
						currentTip.UncleMiner = "NONE"
					}
					
					updatedData, err := json.Marshal(currentTip)
					if err == nil {
						_ = bBlocks.Put([]byte(paddedKey), updatedData)
						fmt.Println("👑 [GHOST CONSENSUS SECURED] Uncle block metrics permanently written to disk bucket!")
					}
				}
				return nil
			})
		}
		return
	}

	ProcessIncomingNetworkBlock(incomingBlock)
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
time.Sleep(250 * time.Second)
PeerMapMutex.Lock()
for peer, lastSeen := range ActivePeerMap {
if time.Since(lastSeen) > (2 * time.Minute) {
delete(ActivePeerMap, peer)
log.Printf("❌ [GOSSIP MESH] Severed inactive channel: %s\n", peer)
}
}
PeerMapMutex.Unlock()
}
}