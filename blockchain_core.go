package main

import (
	"bufio"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
)

var _ embed.FS

type Block struct {
	Index        int64         `json:"current_height"`
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     string        `json:"prev_hash"`
	Hash         string        `json:"block_hash"`
	Difficulty   int           `json:"difficulty"`
	Nonce        int64         `json:"nonce"`
	GuardMatrix  string        `json:"guard_matrix"`
	UncleHash    string        `json:"uncle_hash"`
	UncleMiner   string        `json:"uncle_miner"`
}

type Transaction struct {
	ID               string       `json:"tx_id"`
	Inputs           []UTXOInput  `json:"inputs"`
	Outputs          []UTXOOutput `json:"outputs"`
	Signature        string       `json:"signature"`
	PublicKey        string       `json:"public_key"`
	Timestamp        int64        `json:"timestamp"`
	FreeWillOffering float64      `json:"free_will_offering"`
	DataSizeKB       float64      `json:"data_size_kb"`
	Witness          string       `json:"witness"`
}

type UTXOInput struct {
	SourceTxID string `json:"source_tx_id"`
	Index      int    `json:"output_index"`
	Signature  string `json:"signature"`
}

type UTXOOutput struct {
	Recipient string  `json:"recipient"`
	Amount    float64 `json:"amount"`
}

type UTXORecord struct {
	Amount    float64 `json:"amount"`
	Recipient string  `json:"recipient"`
}

type MempoolType struct {
	sync.RWMutex
	Transactions map[string]Transaction
}

func (m *MempoolType) PushTransaction(tx Transaction) bool {
	m.Lock()
	defer m.Unlock()
	if m.Transactions == nil {
		m.Transactions = make(map[string]Transaction)
	}
	m.Transactions[tx.ID] = tx
	return true
}

var BlockchainFile = "cvn_mainnet.db"
var CustomMinerAddress string
var IsSyncingActive int32 = 0

var BalanceCacheMutex sync.RWMutex
var StateBalanceCache = make(map[string]float64)
var MempoolMatrix = &MempoolType{Transactions: make(map[string]Transaction)}
var StateMutex sync.RWMutex

// Peer latency witness anchor for fork-race resolution
var BlockWitnessTimestamps sync.Map

func CalculateBlockReward(blockIndex int64) float64 {
	if blockIndex < 0 {
		return 0.0
	}
	if blockIndex <= 5256000 {
		return 50.0
	} else if blockIndex <= 10512000 {
		return 25.0
	} else if blockIndex <= 15768000 {
		return 12.5
	} else if blockIndex <= 21024000 {
		return 6.25
	}
	return 5.00
}

func GetGlobalMeshMaxHeight(seedURL string) int64 {
	cleanURL := strings.TrimSuffix(seedURL, "/") + "/block"
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(cleanURL)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0
	}
	if heightVal, exists := data["current_height"]; exists {
		if h, ok := heightVal.(float64); ok {
			return int64(h)
		}
	}
	return 0
}

// Bounded memory channel queue to store real-time wire blocks during resync phases
var DeferredBlockQueue = make(chan Block, 32)

// Canonical uncle UTXO removal helper used by all reorg paths
func deleteUncleUTXO(bUTXO *bbolt.Bucket, height int64, uncleMiner string) {
	trimmed := strings.TrimSpace(uncleMiner)
	if trimmed == "" || trimmed == "NONE" {
		return
	}
	key := []byte(fmt.Sprintf("UNCLE_%d_%s", height, trimmed))
	_ = bUTXO.Delete(key)
}

func ProcessIncomingNetworkBlock(incomingBlock Block) {
	nowUnixMs := time.Now().UnixNano() / int64(time.Millisecond)
	BlockWitnessTimestamps.LoadOrStore(incomingBlock.Hash, nowUnixMs)

	StateMutex.Lock()
	currentTip := GetLatestBlockInternal()
	StateMutex.Unlock()

	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		select {
		case DeferredBlockQueue <- incomingBlock:
			log.Printf("📥 [DEFERRED QUEUE] Cached block #%d into bounded memory channel during active re-sync pass.\n", incomingBlock.Index)
		default:
			log.Printf("⚠️ [QUEUE SATURATED] Deferred block buffer capacity (32 slots) reached. Dropping block #%d.\n", incomingBlock.Index)
		}
		return
	}

	StateMutex.Lock()
	defer StateMutex.Unlock()

	if incomingBlock.Index == currentTip.Index && incomingBlock.Hash != currentTip.Hash {
		var localTipSeenTime, incomingSeenTime int64

		if val, ok := BlockWitnessTimestamps.Load(currentTip.Hash); ok {
			localTipSeenTime = val.(int64)
		}
		if val, ok := BlockWitnessTimestamps.Load(incomingBlock.Hash); ok {
			incomingSeenTime = val.(int64)
		}

		const AdaptiveForkRaceWindowFloor = int64(400)
		timeDeltaBetweenArrivals := incomingSeenTime - localTipSeenTime

		if timeDeltaBetweenArrivals <= AdaptiveForkRaceWindowFloor && localTipSeenTime > 0 {
			fmt.Printf("FORK_RACE: height=%d preferred=%s reason=earlier_seen\n", incomingBlock.Index, currentTip.Hash)
			return
		}

		timeDiff := incomingBlock.Timestamp - currentTip.Timestamp
		if timeDiff < 0 {
			timeDiff = -timeDiff
		}

		preferIncoming := false
		if incomingBlock.Timestamp < currentTip.Timestamp {
			preferIncoming = true
		} else if incomingBlock.Timestamp == currentTip.Timestamp && incomingBlock.Hash < currentTip.Hash {
			preferIncoming = true
		} else if timeDiff <= 7 && incomingBlock.Hash < currentTip.Hash {
			preferIncoming = true
		}

		if preferIncoming {
			fmt.Printf("🔄 [TIP SWITCH] Adopting competing block at height %d (7s grace)\n", incomingBlock.Index)
			_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
				bBlocks := tx.Bucket(blocksBucket)
				bMeta := tx.Bucket(metaBucket)
				bUTXO := tx.Bucket(utxoBucket)
				if bBlocks == nil || bMeta == nil || bUTXO == nil {
					return nil
				}
				paddedKey := []byte(fmt.Sprintf("%016d", currentTip.Index))
				if data := bBlocks.Get(paddedKey); data != nil {
					var localBlock Block
					if json.Unmarshal(data, &localBlock) == nil {
						for _, txPayload := range localBlock.Transactions {
							for outIdx := range txPayload.Outputs {
								_ = bUTXO.Delete([]byte(fmt.Sprintf("%s_%d", txPayload.ID, outIdx)))
							}
						}
						if localBlock.UncleHash != "" &&
							localBlock.UncleHash != "0000000000000000000000000000000000000000000000000000000000000000" &&
							strings.TrimSpace(localBlock.UncleMiner) != "NONE" &&
							strings.TrimSpace(localBlock.UncleMiner) != "" {
							deleteUncleUTXO(bUTXO, localBlock.Index, localBlock.UncleMiner)
						}
					}
				}
				_ = bBlocks.Delete(paddedKey)
				_ = bMeta.Put([]byte("height"), []byte(fmt.Sprintf("%016d", currentTip.Index-1)))
				return nil
			})

			atomic.StoreInt64(&localHeightState, currentTip.Index-1)
			RebuildStateBalanceCacheInternal()

			if ValidateBlockSignatures(incomingBlock) && incomingBlock.PrevHash == GetLatestBlockInternal().Hash {
				if SaveBlockToStorageInternal(incomingBlock) {
					atomic.StoreInt64(&localHeightState, incomingBlock.Index)
					go BroadcastNewBlock(incomingBlock)
				}
			}
		}
		return
	}

	if incomingBlock.Index < currentTip.Index {
		return
	}
	if !ValidateBlockSignatures(incomingBlock) {
		return
	}

	if incomingBlock.Index > currentTip.Index+1 {
		go TriggerActiveReSyncPass()
		return
	}

	if incomingBlock.Index == currentTip.Index+1 {
		if incomingBlock.PrevHash != currentTip.Hash {
			go TriggerActiveReSyncPass()
			return
		}
		if SaveBlockToStorageInternal(incomingBlock) {
			atomic.StoreInt64(&localHeightState, incomingBlock.Index)
			go BroadcastNewBlock(incomingBlock)
		}
	}
}

func TriggerActiveReSyncPass() {
	if !atomic.CompareAndSwapInt32(&IsSyncingActive, 0, 1) {
		return
	}

	log.Println("⏳ [SYNC GATEWAY ACTIVE] Consensus engine flag set: TRUE. Local mining loops safely PAUSED.")

	syncWatchdogTimer := time.NewTimer(90 * time.Second)
	watchdogDoneChannel := make(chan struct{})

	go func() {
		select {
		case <-syncWatchdogTimer.C:
			log.Println("🚨 [SYNC WATCHDOG TIMEOUT] Catch-up pass exceeded 90-second allocation limit! Enforcing fallback breakout.")
			atomic.StoreInt32(&IsSyncingActive, 0)
			drainAndProcessDeferredQueue()
		case <-watchdogDoneChannel:
			syncWatchdogTimer.Stop()
		}
	}()

	defer func() {
		close(watchdogDoneChannel)
		atomic.StoreInt32(&IsSyncingActive, 0)
		log.Println("🟩 [SYNC GATEWAY RELEASED] Consensus engine flag cleared: FALSE. Mining worker pool RESUMED.")
		drainAndProcessDeferredQueue()
	}()

	activeGlobalRecoveryGateways := []string{"http://202.137.175.220:8081", "http://207.148.67.11:8081", "http://64.177.45.153:8081", "http://202.137.175.220:9091"}
	client := &http.Client{Timeout: 3 * time.Second}
	var chosenTargetSeedURL string
	var selectedHighestTargetTip int64

	PeerMapMutex.RLock()
	for activePeerAddress, lastSeenTimestamp := range ActivePeerMap {
		if time.Since(lastSeenTimestamp) <= 30*time.Second {
			cleanHostIP, _, _ := net.SplitHostPort(activePeerAddress)
			peerHTTPGateway := fmt.Sprintf("http://%s:8081", cleanHostIP)

			remotePeerHeight := GetGlobalMeshMaxHeight(peerHTTPGateway)
			if remotePeerHeight > selectedHighestTargetTip {
				selectedHighestTargetTip = remotePeerHeight
				chosenTargetSeedURL = peerHTTPGateway
			}
		}
	}
	PeerMapMutex.RUnlock()

	if chosenTargetSeedURL == "" {
		for _, endpoint := range activeGlobalRecoveryGateways {
			if strings.Contains(endpoint, "202.137.175.220:8081") && runtime.GOOS == "windows" {
				continue
			}
			remotePeerHeight := GetGlobalMeshMaxHeight(endpoint)
			if remotePeerHeight > selectedHighestTargetTip {
				selectedHighestTargetTip = remotePeerHeight
				chosenTargetSeedURL = endpoint
			}
		}
	}

	if chosenTargetSeedURL == "" {
		return
	}

	StateMutex.Lock()
	localTip := GetLatestBlockInternal()
	localHashesSnapshot := make(map[int64]string)
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		if bBlocks == nil {
			return nil
		}
		for h := localTip.Index; h >= 0; h-- {
			paddedKey := []byte(fmt.Sprintf("%016d", h))
			if data := bBlocks.Get(paddedKey); data != nil {
				var blk Block
				if json.Unmarshal(data, &blk) == nil {
					localHashesSnapshot[h] = blk.Hash
				}
			}
		}
		return nil
	})
	StateMutex.Unlock()

	scanHeight := localTip.Index
	var commonAncestorHeight int64 = -1
	for scanHeight >= 0 {
		localHash := localHashesSnapshot[scanHeight]
		remoteHash, remoteErr := fetchRemoteBlockHeader(client, chosenTargetSeedURL, scanHeight)
		if remoteErr == nil && localHash != "" && localHash == remoteHash {
			commonAncestorHeight = scanHeight
			break
		}
		scanHeight--
	}
	if commonAncestorHeight == -1 {
		return
	}

	StateMutex.Lock()
	shadowTip := GetLatestBlockInternal()
	if commonAncestorHeight < shadowTip.Index {
		_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
			bBlocks := tx.Bucket(blocksBucket)
			bMeta := tx.Bucket(metaBucket)
			bUTXO := tx.Bucket(utxoBucket)
			if bBlocks == nil || bMeta == nil || bUTXO == nil {
				return nil
			}
			for h := shadowTip.Index; h > commonAncestorHeight; h-- {
				paddedKey := []byte(fmt.Sprintf("%016d", h))
				if data := bBlocks.Get(paddedKey); data != nil {
					var block Block
					if json.Unmarshal(data, &block) == nil {
						for _, txPayload := range block.Transactions {
							for outIdx := range txPayload.Outputs {
								_ = bUTXO.Delete([]byte(fmt.Sprintf("%s_%d", txPayload.ID, outIdx)))
							}
						}
						if block.UncleHash != "" &&
							block.UncleHash != "0000000000000000000000000000000000000000000000000000000000000000" &&
							strings.TrimSpace(block.UncleMiner) != "NONE" &&
							strings.TrimSpace(block.UncleMiner) != "" {
							deleteUncleUTXO(bUTXO, block.Index, block.UncleMiner)
						}
					}
				}
				_ = bBlocks.Delete(paddedKey)
			}
			_ = bMeta.Put([]byte("height"), []byte(fmt.Sprintf("%016d", commonAncestorHeight)))
			return nil
		})
		atomic.StoreInt64(&localHeightState, commonAncestorHeight)
		RebuildStateBalanceCacheInternal()
	}

	consecutiveHTTPFailures := 0
	for {
		currentLocalTip := GetLatestBlockInternal()
		nextNeededBlock := currentLocalTip.Index + 1
		if nextNeededBlock > selectedHighestTargetTip {
			break
		}
		requestURL := fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(chosenTargetSeedURL, "/"), nextNeededBlock)
		StateMutex.Unlock()
		resp, err := client.Get(requestURL)
		var incomingBatch []Block
		if err != nil {
			consecutiveHTTPFailures++
			log.Printf("🔌 [HTTP GATEWAY TIMEOUT] Port 8081 inaccessible. Executing emergency failover block payload pull via TCP Port 8080 for Block #%d\n", nextNeededBlock)
			incomingBatch = requestBatchOverTCPMeshNatively(nextNeededBlock)
			if len(incomingBatch) == 0 {
				StateMutex.Lock()
				break
			}
		} else {
			if resp.StatusCode == http.StatusOK {
				_ = json.NewDecoder(resp.Body).Decode(&incomingBatch)
			}
			resp.Body.Close()
			if len(incomingBatch) == 0 {
				StateMutex.Lock()
				break
			}
		}
		StateMutex.Lock()
		for _, block := range incomingBatch {
			if block.Index == int64(GetLatestBlockInternal().Index)+1 {
				if ValidateBlockSignatures(block) {
					if block.UncleHash == "" {
						block.UncleHash = "0000000000000000000000000000000000000000000000000000000000000000"
					}
					if block.UncleMiner == "" {
						block.UncleMiner = "NONE"
					}
					SaveBlockToStorageInternal(block)
				} else {
					log.Printf("⚠️ [SYNC EXCLUSION FILTER] Rejected invalid block layout sequence at height #%d\n", block.Index)
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	StateMutex.Unlock()
}

func drainAndProcessDeferredQueue() {
	StateMutex.Lock()
	defer StateMutex.Unlock()
	log.Println("🔄 [DRAIN PHASE INITIALIZED] Sequential block deployment pass streaming from cached memory channel...")
	queueLength := len(DeferredBlockQueue)
	for i := 0; i < queueLength; i++ {
		select {
		case cachedBlock := <-DeferredBlockQueue:
			currentTip := GetLatestBlockInternal()
			if cachedBlock.Index == currentTip.Index+1 {
				if ValidateBlockSignatures(cachedBlock) && cachedBlock.PrevHash == currentTip.Hash {
					if SaveBlockToStorageInternal(cachedBlock) {
						log.Printf("🟩 [QUEUE RECOVERY SUCCESS] Sequentially applied buffered block #%d to disk map storage.\n", cachedBlock.Index)
						atomic.StoreInt64(&localHeightState, cachedBlock.Index)
						go BroadcastNewBlock(cachedBlock)
					}
				}
			}
		default:
			return
		}
	}
}

func requestBatchOverTCPMeshNatively(height int64) []Block {
	PeerMapMutex.RLock()
	var fallbackTargets []string
	for targetIP := range ActivePeerMap {
		fallbackTargets = append(fallbackTargets, targetIP)
	}
	PeerMapMutex.RUnlock()
	for _, endpointHost := range fallbackTargets {
		conn, err := net.DialTimeout("tcp", endpointHost, 3*time.Second)
		if err == nil {
			payloadCommand := fmt.Sprintf("GET_BATCH:%d\n", height)
			_, _ = fmt.Fprint(conn, payloadCommand)
			_ = conn.SetReadDeadline(time.Now().Add(6 * time.Second))
			scanner := bufio.NewScanner(conn)
			buf := make([]byte, 0, 64*1024)
			scanner.Buffer(buf, 10*1024*1024)
			if scanner.Scan() {
				var blocks []Block
				if json.Unmarshal(scanner.Bytes(), &blocks) == nil && len(blocks) > 0 {
					conn.Close()
					return blocks
				}
			}
			conn.Close()
		}
	}
	return nil
}

func fetchRemoteBlockHeader(client *http.Client, seedURL string, height int64) (string, error) {
	requestURL := fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(seedURL, "/"), height)
	resp, err := client.Get(requestURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("invalid response code: %d", resp.StatusCode)
	}
	var batch []Block
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		return "", err
	}
	for _, block := range batch {
		if block.Index == height {
			return block.Hash, nil
		}
	}
	return "", fmt.Errorf("block missing")
}

func CalculateHash(b Block) string {
	var txSummaryBuilder strings.Builder
	for _, tx := range b.Transactions {
		txSummaryBuilder.WriteString(tx.ID + "-")
	}
	txHashStr := fmt.Sprintf("%x", sha256.Sum256([]byte(txSummaryBuilder.String())))
	uHash := b.UncleHash
	if uHash == "" {
		uHash = "0000000000000000000000000000000000000000000000000000000000000000"
	}
	uMiner := b.UncleMiner
	if uMiner == "" {
		uMiner = "NONE"
	}
	inputStr := fmt.Sprintf("%d-%d-%s-%s-%d-%s-%d-%s-%s",
		b.Index,
		b.Timestamp,
		b.PrevHash,
		txHashStr,
		int64(b.Difficulty),
		b.GuardMatrix,
		b.Nonce,
		uHash,
		uMiner,
	)
	h := sha256.New()
	h.Write([]byte(inputStr))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func ForceBackgroundSyncPass() {}

func GetAddressBalance(address string) float64 {
	BalanceCacheMutex.RLock()
	privateBalance, exists := StateBalanceCache[address]
	BalanceCacheMutex.RUnlock()
	if exists {
		return privateBalance
	}
	return 0.0
}

const MaxTotalSupplyCap = 2100000000.0

func GetCurrentCirculatingSupply() float64 {
	StateMutex.RLock()
	latestBlock := GetLatestBlockInternal()
	StateMutex.RUnlock()
	height := latestBlock.Index
	var calculatedSupply float64 = 0.0
	var processedBlocks int64 = 0
	if height > 5256000 {
		calculatedSupply += 5256000 * 50.0
		processedBlocks = 5256000
	} else {
		calculatedSupply += float64(height) * 50.0
		return calculatedSupply
	}
	if height > 10512000 {
		calculatedSupply += (10512000 - 5256000) * 25.0
		processedBlocks = 10512000
	} else {
		calculatedSupply += float64(height-processedBlocks) * 25.0
		return calculatedSupply
	}
	if height > 15768000 {
		calculatedSupply += (15768000 - 10512000) * 12.5
		processedBlocks = 15768000
	} else {
		calculatedSupply += float64(height-processedBlocks) * 12.5
		return calculatedSupply
	}
	if height > 21024000 {
		calculatedSupply += (21024000 - 15768000) * 6.25
		processedBlocks = 21024000
	} else {
		calculatedSupply += float64(height-processedBlocks) * 6.25
		return calculatedSupply
	}
	if calculatedSupply > MaxTotalSupplyCap {
		return MaxTotalSupplyCap
	}
	return calculatedSupply
}

func CalculateNextDifficulty(lastBlock Block, nextBlockTimestamp int64) int {
	if lastBlock.Index < 10 {
		return 2
	}
	currentDiff := int(lastBlock.Difficulty)
	actualTimeElapsed := nextBlockTimestamp - lastBlock.Timestamp
	if actualTimeElapsed <= 0 {
		return currentDiff + 1
	}
	if actualTimeElapsed >= 30 && actualTimeElapsed <= 90 {
		if currentDiff < 4 {
			return 4
		}
		return currentDiff
	}
	if actualTimeElapsed < 30 {
		return currentDiff + 1
	} else if actualTimeElapsed > 90 {
		decreasedDiff := currentDiff - 1
		if decreasedDiff < 4 {
			return 4
		}
		return decreasedDiff
	}
	return currentDiff
}

func ValidateBlockSignatures(b Block) bool {
	var expectedDiff int
	if b.Index < 10 {
		expectedDiff = 2
	} else {
		if b.Difficulty < 4 {
			return false
		}
		expectedDiff = b.Difficulty
	}
	targetPrefix := strings.Repeat("0", expectedDiff)
	if !strings.HasPrefix(b.Hash, targetPrefix) {
		return false
	}
	if b.Hash != CalculateHash(b) {
		return false
	}
	if len(b.Transactions) == 0 {
		return false
	}
	isBlockValid := true
	if GlobalBoltEngine != nil {
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			bUTXO := dbTx.Bucket([]byte("utxo"))
			if bUTXO == nil {
				isBlockValid = false
				return nil
			}
			for txIdx, tx := range b.Transactions {
				if txIdx == 0 {
					continue
				}
				if !VerifyTransactionIntegrity(tx, int(b.Index), txIdx, bUTXO) {
					isBlockValid = false
					return nil
				}
			}
			return nil
		})
	}
	if !isBlockValid {
		return false
	}
	coinbaseTx := b.Transactions
	var totalCoinbasePayout float64 = 0.0
	if len(coinbaseTx) > 0 {
		for _, out := range coinbaseTx[0].Outputs {
			totalCoinbasePayout += out.Amount
		}
	} else {
		return false
	}
	targetBlockHeight := int64(b.Index)
	allowedTotalEmission := CalculateBlockReward(targetBlockHeight)
	if targetBlockHeight > 21024000 {
		allowedTotalEmission += 5.0
	}
	if b.UncleHash != "" && b.UncleHash != "0000000000000000000000000000000000000000000000000000000000000000" && b.UncleMiner != "NONE" && b.UncleMiner != "" {
		if len(coinbaseTx[0].Outputs) != 2 {
			fmt.Println("🚨 CONSENSUS REJECTION: Uncle transaction layout requires exactly two coinbase outputs.")
			return false
		}
		mainMinerPayout := coinbaseTx[0].Outputs[0].Amount
		uncleMinerPayout := coinbaseTx[0].Outputs[1].Amount
		uncleMinerRecipient := coinbaseTx[0].Outputs[1].Recipient
		expectedMainSplit := allowedTotalEmission * 0.80
		expectedUncleSplit := allowedTotalEmission * 0.20
		if (mainMinerPayout-expectedMainSplit) > 0.00001 || (mainMinerPayout-expectedMainSplit) < -0.00001 {
			fmt.Printf("🚨 CONSENSUS REJECTION: Invalid Main Miner Split footprint. Got: %f, Expected: %f\n", mainMinerPayout, expectedMainSplit)
			return false
		}
		if (uncleMinerPayout-expectedUncleSplit) > 0.00001 || (uncleMinerPayout-expectedUncleSplit) < -0.00001 {
			fmt.Printf("🚨 CONSENSUS REJECTION: Invalid Uncle Miner Payout footprint. Got: %f, Expected: %f\n", uncleMinerPayout, expectedUncleSplit)
			return false
		}
		if strings.TrimSpace(uncleMinerRecipient) != strings.TrimSpace(b.UncleMiner) {
			fmt.Printf("🚨 CONSENSUS REJECTION: Coinbase index recipient %s does not match Uncle header signature metadata %s\n", uncleMinerRecipient, b.UncleMiner)
			return false
		}
	} else {
		if (totalCoinbasePayout - allowedTotalEmission) > 0.00001 {
			fmt.Printf("🚨 EMISSION BLOCKED: Claim over-allocated payout: %f // Allowed: %f\n", totalCoinbasePayout, allowedTotalEmission)
			return false
		}
	}
	return true
}
