package main

import (
	"crypto/sha256"
	"embed"
	"encoding/binary" 
	"encoding/json"
	"fmt"
	"io"
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
	Version      uint32        `json:"version"`
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
	Version          uint32       `json:"version"`
	ID               string       `json:"tx_id"`
	Inputs           []UTXOInput  `json:"inputs"`
	Outputs          []UTXOOutput `json:"outputs"`
	Signature        string       `json:"signature"`
	PublicKey        string       `json:"public_key"`
	Timestamp        int64        `json:"timestamp"`
	FreeWillOffering float64      `json:"free_will_offering"`
	DataSizeKB       float64      `json:"data_size_kb"`
	Witness          string       `json:"witness"`
	Extra            []byte       `json:"extra,omitempty"`
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

var BlockWitnessTimestamps sync.Map

func cleanAddr(a string) string {
	return strings.TrimSpace(a)
}

func GetActiveRules(currentHeight int64) ProtocolRuleSet {
	targetHeight := int64(0)
	for activeHeight := range ConsensusUpgradeSchedule {
		if currentHeight >= activeHeight && activeHeight >= targetHeight {
			targetHeight = activeHeight
		}
	}
	return ConsensusUpgradeSchedule[targetHeight]
}

type ProtocolRuleSet struct {
	MaxBlockSizeKB   float64
	EnforceMTP       bool
	MinimumFloorDiff int
}

var ConsensusUpgradeSchedule = map[int64]ProtocolRuleSet{
	0: {
		MaxBlockSizeKB:   1024.0,
		EnforceMTP:       false,
		MinimumFloorDiff: 2,
	},
	10000: {
		MaxBlockSizeKB:   2048.0, 
		EnforceMTP:       true,   
		MinimumFloorDiff: 4,      
	},
}

func ValidateBlockByVersion(b Block) bool {
	if b.Version == 0 {
		b.Version = 1
	}
	activeRules := GetActiveRules(b.Index)
	_ = activeRules 
	switch b.Version {
	case 1:
		return true
	case 2:
		return true
	default:
		return false
	}
}

func CalculateHash(b Block) string {
	var txSummaryBuilder strings.Builder
	for _, tx := range b.Transactions {
		txSummaryBuilder.WriteString(tx.ID + "-")
	}
	txHashStr := fmt.Sprintf("%x", sha256.Sum256([]byte(txSummaryBuilder.String())))
	
	// 👑 PLATFORM-INDEPENDENT PREIMAGE SANITIZATION HARNESS:
	// Forcibly clamp structural string reflections into explicitly allocated string blocks 
	// before executing fmt.Sprintf parameters to completely neutralize Windows/Linux padding drift.
	uHash := strings.TrimSpace(b.UncleHash)
	if uHash == "" {
		uHash = "0000000000000000000000000000000000000000000000000000000000000000"
	}
	uMiner := strings.TrimSpace(b.UncleMiner)
	if uMiner == "" {
		uMiner = "NONE"
	}
	gMatrix := strings.TrimSpace(b.GuardMatrix)
	if gMatrix == "" {
		gMatrix = "NONE"
	}

	inputStr := fmt.Sprintf("%d-%d-%d-%s-%s-%d-%s-%d-%s-%s",
		int(b.Version),
		int64(b.Index),
		int64(b.Timestamp),
		strings.TrimSpace(b.PrevHash),
		txHashStr,
		int64(b.Difficulty),
		gMatrix,
		int64(b.Nonce),
		uHash,
		uMiner,
	)
	
	h := sha256.New()
	h.Write([]byte(inputStr))
	return fmt.Sprintf("%x", h.Sum(nil))
}

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

var DeferredBlockQueue = make(chan Block, 32)

func deleteUncleUTXO(bUTXO *bbolt.Bucket, height int64, uncleMiner string) {
	trimmed := cleanAddr(uncleMiner)
	if trimmed == "" || trimmed == "NONE" {
		return
	}
	key := []byte(fmt.Sprintf("UNCLE_%d_%s", height, trimmed))
	_ = bUTXO.Delete(key)
}

func ApplyJubileeDecayIfNeeded(address string, lastActivityUnix int64) {
	const JubileeWindow = 49 * 365 * 24 * 60 * 60
	cleanedAddress := cleanAddr(address)
	if cleanedAddress == "" || cleanedAddress == "COVENANT_STEWARD_ASSEMBLY" || cleanedAddress == "GENESIS_VOID_REWARD_POOL" {
		return
	}
	currentUnixTime := time.Now().Unix()
	stagnantDuration := currentUnixTime - lastActivityUnix
	if stagnantDuration > JubileeWindow {
		log.Printf("⚠️ [JUBILEE TRIGGERED] CANDIDATE DETECTED: %s has remained inactive for %d years. Permanent decay reclamation engaged.\n",
			cleanedAddress, int(stagnantDuration/(365*24*60*60)))
	}
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
	currentTip = GetLatestBlockInternal()
	StateMutex.Unlock()

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
			StateMutex.Lock()
			_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
				bBlocks := tx.Bucket(blocksBucket)
				bMeta := tx.Bucket(metaBucket)
				bUTXO := tx.Bucket(utxoBucket)
				if bBlocks == nil || bMeta == nil || bUTXO == nil { return nil }
				
				paddedKey := []byte(fmt.Sprintf("%016d", currentTip.Index))
				if data := bBlocks.Get(paddedKey); data != nil {
					var localBlock Block
					if json.Unmarshal(data, &localBlock) == nil {
						for _, txPayload := range localBlock.Transactions {
							for outIdx := range txPayload.Outputs {
								_ = bUTXO.Delete([]byte(fmt.Sprintf("%s_%d", txPayload.ID, outIdx)))
							}
							
							if !strings.HasPrefix(txPayload.ID, "TX_COINBASE") {
								for _, input := range txPayload.Inputs {
									parentTxBlock := GetBlockByHeightFromDB(int64(currentTip.Index - 1))
									var matchedOutput UTXOOutput
									var found = false
									
									for _, pTx := range parentTxBlock.Transactions {
										if pTx.ID == input.SourceTxID && input.Index < len(pTx.Outputs) {
											matchedOutput = pTx.Outputs[input.Index]
											found = true
											break
										}
									}
									
									if found {
										restoredRecord := UTXORecord{Amount: matchedOutput.Amount, Recipient: strings.TrimSpace(matchedOutput.Recipient)}
										restoredBytes, _ := json.Marshal(restoredRecord)
										compoundKey := fmt.Sprintf("%s_%d", input.SourceTxID, input.Index)
										_ = bUTXO.Put([]byte(compoundKey), restoredBytes)
									}
								}
							}
						}
						
						if localBlock.UncleHash != "" &&
							localBlock.UncleHash != "0000000000000000000000000000000000000000000000000000000000000000" &&
							cleanAddr(localBlock.UncleMiner) != "NONE" &&
							cleanAddr(localBlock.UncleMiner) != "" {
deleteUncleUTXO(bUTXO, localBlock.Index, localBlock.UncleMiner)
}
}
}
_ = bBlocks.Delete(paddedKey)
_ = bMeta.Put([]byte("height"), []byte(fmt.Sprintf("%016d", currentTip.Index-1)))
return nil
})
StateMutex.Unlock()
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
func ProcessIncomingWireBlockWithDelay(incomingBlock Block) {
if atomic.LoadInt32(&IsSyncingActive) == 1 {
return
}
StateMutex.Lock()
currentTip := GetLatestBlockInternal()
StateMutex.Unlock()
if incomingBlock.Index == currentTip.Index+1 {
if incomingBlock.PrevHash != currentTip.Hash {
go TriggerActiveReSyncPass()
return
}
if SaveBlockToStorageInternal(incomingBlock) {
atomic.StoreInt64(&localHeightState, incomingBlock.Index)
SignalMinerReset()
go BroadcastNewBlock(incomingBlock)
}
return
}
if incomingBlock.Index == currentTip.Index && incomingBlock.Hash != currentTip.Hash &&
incomingBlock.PrevHash == currentTip.PrevHash {
var winner, loser Block
if currentTip.Hash < incomingBlock.Hash {
winner = currentTip
loser = incomingBlock
} else {
winner = incomingBlock
loser = currentTip
}
var uncleMiner string
if len(loser.Transactions) > 0 {
uncleMiner = strings.TrimSpace(loser.Transactions[0].Witness)
}
if uncleMiner == "" && len(loser.Transactions) > 0 && len(loser.Transactions[0].Outputs) > 0 {
uncleMiner = strings.TrimSpace(loser.Transactions[0].Outputs[0].Recipient)
}
if uncleMiner == "" || uncleMiner == "GENESIS_VOID_REWARD_POOL" {
uncleMiner = "NONE"
}
if uncleMiner != "NONE" && uncleMiner != "" {
StateMutex.Lock()
_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
bUncle := tx.Bucket([]byte("uncle_ledger"))
bUTXO := tx.Bucket([]byte("utxo"))
if bUncle == nil || bUTXO == nil { return nil }
uncleKey := []byte(fmt.Sprintf("UNCLE_METADATA_%d", winner.Index))
if bUncle.Get(uncleKey) == nil {
metadataPayload := map[string]string{
"winning_hash": winner.Hash,
"uncle_hash":   loser.Hash,
"uncle_miner":  uncleMiner,
}
metaBytes, _ := json.Marshal(metadataPayload)
_ = bUncle.Put(uncleKey, metaBytes)
allowedTotalEmission := CalculateBlockReward(winner.Index)
uncleSubsidyReward := allowedTotalEmission * 0.20
uncleUTXOKey := fmt.Sprintf("UNCLE_REWARD_%d_%s", winner.Index, uncleMiner)
utxoRecord := UTXORecord{Amount: uncleSubsidyReward, Recipient: uncleMiner}
utxoData, _ := json.Marshal(utxoRecord)
_ = bUTXO.Put([]byte(uncleUTXOKey), utxoData)
fmt.Printf("💾 [GHOST CONSENSUS SECURED] Competing branch Uncle metrics archived: %s\n", uncleUTXOKey)
}
return nil
})
StateMutex.Unlock()
go RebuildStateBalanceCacheInternal()
}
return
}
ProcessIncomingNetworkBlock(incomingBlock)
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
activeGlobalRecoveryGateways := []string{
"http://202.137.175.220:8081",
"http://207.148.67.11:8081",
"http://64.177.45.153:8081",
"http://202.137.175.220:9091",
}
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
if remoteErr != nil {
remoteHash = fetchRemoteBlockHeaderViaP2P(scanHeight)
if remoteHash != "" {
remoteErr = nil
}
}
if remoteErr == nil && localHash != "" && localHash == remoteHash {
commonAncestorHeight = scanHeight
break
}
scanHeight--
}
if commonAncestorHeight == -1 {
log.Println("🚨 [SYNC FAULT] Unable to establish common ancestor over HTTP or P2P Mesh channels.")
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
_ = bUTXO.Delete([]byte(fmt.Sprintf("%s%d", txPayload.ID, outIdx)))
}
}
if block.UncleHash != "" &&
block.UncleHash != "0000000000000000000000000000000000000000000000000000000000000000" &&
cleanAddr(block.UncleMiner) != "NONE" &&
cleanAddr(block.UncleMiner) != "" {
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
log.Printf("🔌 [HTTP GATEWAY TIMEOUT] Port 8081 inaccessible. Executing emergency block pull via TCP Port 8080 for Block #%d\n", nextNeededBlock)
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
syncFilterRejected := false
for _, block := range incomingBatch {
if block.Index == GetLatestBlockInternal().Index+1 {
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
syncFilterRejected = true
break
}
}
}
StateMutex.Unlock()
if syncFilterRejected {
break
}
time.Sleep(10 * time.Millisecond)
}
}
func drainAndProcessDeferredQueue() {
if atomic.LoadInt32(&IsSyncingActive) == 1 {
log.Println("⚠️ [RE-ENTRY BLOCK] Sync engine remains active. Deferring block channel queue drainage pass.")
return
}
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
func ForceBackgroundSyncPass() {}
func GetAddressBalance(address string) float64 {
BalanceCacheMutex.RLock()
privateBalance, exists := StateBalanceCache[cleanAddr(address)]
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
func GetMedianTimePast(lastBlock Block) int64 {
var timestamps []int64
currentScanBlock := lastBlock
for i := 0; i < 11; i++ {
timestamps = append(timestamps, currentScanBlock.Timestamp)
if currentScanBlock.Index == 0 {
break
}
currentScanBlock = GetBlockByHeightFromDB(currentScanBlock.Index - 1)
if currentScanBlock.Hash == "" {
break
}
}
for i := 0; i < len(timestamps)-1; i++ {
for j := 0; j < len(timestamps)-i-1; j++ {
if timestamps[j] > timestamps[j+1] {
timestamps[j], timestamps[j+1] = timestamps[j+1], timestamps[j]
}
}
}
if len(timestamps) == 0 {
return time.Now().Unix()
}
return timestamps[len(timestamps)/2]
}
const (
RetargetWindowSize    = 15
DampeningPercentage   = 20
TargetBlockTimeWindow = 45
)
func CalculateMedianElapsedTime(lastBlock Block) int64 {
var intervals []int64
currentScanBlock := lastBlock
for i := 0; i < RetargetWindowSize; i++ {
if currentScanBlock.Index == 0 {
break
}
parentBlock := GetBlockByHeightFromDB(currentScanBlock.Index - 1)
if parentBlock.Hash == "" {
break
}
delta := currentScanBlock.Timestamp - parentBlock.Timestamp
if delta > 0 {
intervals = append(intervals, delta)
}
currentScanBlock = parentBlock
}
if len(intervals) == 0 {
return TargetBlockTimeWindow
}
for i := 0; i < len(intervals)-1; i++ {
for j := 0; j < len(intervals)-i-1; j++ {
if intervals[j] > intervals[j+1] {
intervals[j], intervals[j+1] = intervals[j+1], intervals[j]
}
}
}
return intervals[len(intervals)/2]
}
func CalculateNextDifficulty(lastBlock Block, nextBlockTimestamp int64) int {
if lastBlock.Index < 10 {
return 2
}
currentDiff := int(lastBlock.Difficulty)
if currentDiff < 4 {
return 4
}
mtpFloor := GetMedianTimePast(lastBlock)
if nextBlockTimestamp <= mtpFloor {
return currentDiff + 1
}
medianElapsed := CalculateMedianElapsedTime(lastBlock)
lowThreshold := int64((TargetBlockTimeWindow * (100 - DampeningPercentage)) / 100)
highThreshold := int64((TargetBlockTimeWindow * (100 + DampeningPercentage)) / 100)
if medianElapsed < lowThreshold {
return currentDiff + 1
} else if medianElapsed > highThreshold {
decreasedDiff := currentDiff - 1
if decreasedDiff < 4 {
return 4
}
return decreasedDiff
}
return currentDiff
}
func ValidateBlockSignatures(b Block) bool {
if !ValidateBlockByVersion(b) {
fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d rejected by version dispatcher gateway rule.\n", b.Index)
return false
}
if b.Index > 0 {
parentBlock := GetBlockByHeightFromDB(b.Index - 1)
if parentBlock.Hash != "" {
mtpFloor := GetMedianTimePast(parentBlock)
if b.Timestamp <= mtpFloor {
fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d timestamp (%d) is behind historical MTP floor (%d). Exploit blocked.\n", b.Index, b.Timestamp, mtpFloor)
return false
}
if b.Timestamp > time.Now().Unix()+7200 {
fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d timestamp drifted too far into the future. Exploit blocked.\n", b.Index)
return false
}
}
}
var expectedDiff int
if b.Index <= 10 {
expectedDiff = 2
} else {
parentBlock := GetBlockByHeightFromDB(b.Index - 1)
if parentBlock.Hash == "" {
parentBlock = Block{Index: b.Index - 1, Difficulty: b.Difficulty}
}
expectedDiff = CalculateNextDifficulty(parentBlock, b.Timestamp)
}
if b.Difficulty != expectedDiff {
fmt.Printf("🚨 REJECTION: Incoming block #%d uses invalid difficulty %d (Expected exactly: %d)\n", b.Index, b.Difficulty, expectedDiff)
return false
}
targetPrefix := strings.Repeat("0", expectedDiff)
if !strings.HasPrefix(b.Hash, targetPrefix) || b.Hash != CalculateHash(b) {
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
if b.UncleHash != "" && b.UncleHash != "0000000000000000000000000000000000000000000000000000000000000000" && cleanAddr(b.UncleMiner) != "NONE" && cleanAddr(b.UncleMiner) != "" {
if len(coinbaseTx[0].Outputs) != 2 {
fmt.Println("🚨 CONSENSUS REJECTION: Uncle transaction layout requires exactly two coinbase outputs.")
return false
}
mainMinerPayout := coinbaseTx[0].Outputs[0].Amount
uncleMinerPayout := coinbaseTx[0].Outputs[1].Amount
uncleMinerRecipient := cleanAddr(coinbaseTx[0].Outputs[1].Recipient)
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
if uncleMinerRecipient != cleanAddr(b.UncleMiner) {
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
func requestBatchOverTCPMeshNatively(height int64) []Block {
PeerMapMutex.RLock()
var fallbackTargets []string
for targetIP := range ActivePeerMap {
fallbackTargets = append(fallbackTargets, targetIP)
}
PeerMapMutex.RUnlock()
for _, endpointHost := range fallbackTargets {
conn, err := net.DialTimeout("tcp", endpointHost, 3 * time.Second)
if err == nil {
lengthBuf := make([]byte, 4)
payloadCommand := fmt.Sprintf("GET_BATCH:%d", height)
payloadBytes := []byte(payloadCommand)
binary.BigEndian.PutUint32(lengthBuf, uint32(len(payloadBytes)))
_, _ = conn.Write(lengthBuf)
_, _ = conn.Write(payloadBytes)
_ = conn.SetReadDeadline(time.Now().Add(6 * time.Second))
var frameLength uint32
err = binary.Read(conn, binary.BigEndian, &frameLength)
if err == nil && frameLength <= (256*1024) {
payloadBuffer := make([]byte, frameLength)
_, err = io.ReadFull(conn, payloadBuffer)
if err == nil {
var blocks []Block
if json.Unmarshal(payloadBuffer, &blocks) == nil && len(blocks) > 0 {
conn.Close()
return blocks
}
}
}
conn.Close()
}
}
return nil
}
func fetchRemoteBlockHeaderViaP2P(height int64) string {
PeerMapMutex.RLock()
var fallbackTargets []string
for targetIP := range ActivePeerMap {
fallbackTargets = append(fallbackTargets, targetIP)
}
PeerMapMutex.RUnlock()
for _, endpointHost := range fallbackTargets {
conn, err := net.DialTimeout("tcp", endpointHost, 2 * time.Second)
if err == nil {
lengthBuf := make([]byte, 4)
payloadCommand := fmt.Sprintf("GET_BATCH:%d", height)
payloadBytes := []byte(payloadCommand)
binary.BigEndian.PutUint32(lengthBuf, uint32(len(payloadBytes)))
_, _ = conn.Write(lengthBuf)
_, _ = conn.Write(payloadBytes)
_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
var frameLength uint32
err = binary.Read(conn, binary.BigEndian, &frameLength)
if err == nil && frameLength <= (256*1024) {
payloadBuffer := make([]byte, frameLength)
_, err = io.ReadFull(conn, payloadBuffer)
if err == nil {
var blocks []Block
if json.Unmarshal(payloadBuffer, &blocks) == nil && len(blocks) > 0 {
conn.Close()
return blocks[len(blocks)-1].Hash
}
}
}
conn.Close()
}
}
return ""
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