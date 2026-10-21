package main

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
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

func ProcessIncomingNetworkBlock(incomingBlock Block) {
	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		return
	}

	StateMutex.Lock()
	defer StateMutex.Unlock()

	currentTip := GetLatestBlockInternal()

	if incomingBlock.Index == currentTip.Index && incomingBlock.Hash != currentTip.Hash {
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

func TriggerActiveReSyncPass() {
	if !atomic.CompareAndSwapInt32(&IsSyncingActive, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&IsSyncingActive, 0)
	fmt.Println(" -> [RE-SYNC MATRIX ACTIVE] Local mining processes safely PAUSED.")

	activeGlobalRecoveryGateways := []string{"http://202.137.175.220:8081", "http://207.148.67.11:8081", "http://64.177.45.153:8081", "http://202.137.175.220:9091"}
	client := &http.Client{Timeout: 4 * time.Second}
	var chosenTargetSeedURL string
	var selectedHighestTargetTip int64

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
		if err != nil {
			StateMutex.Lock()
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			StateMutex.Lock()
			time.Sleep(100 * time.Millisecond)
			continue
		}
		var incomingBatch []Block
		err = json.NewDecoder(resp.Body).Decode(&incomingBatch)
		resp.Body.Close()
		if err != nil || len(incomingBatch) == 0 {
			StateMutex.Lock()
			break
		}

		StateMutex.Lock()
		for _, block := range incomingBatch {
			if block.Index == int64(GetLatestBlockInternal().Index)+1 {
				if ValidateBlockSignatures(block) {
					SaveBlockToStorageInternal(block)
fmt.Printf(" -> [DECENTRALIZED RESILIENCE] Ingested block #%d straight from endpoint -> %s\n", block.Index, chosenTargetSeedURL)
} else {
fmt.Printf(" -> [SECURITY CATCHUP PASS] Malformed peer block detected at height #%d. Halting ingest pipeline.\n", block.Index)
break
}
}
}
time.Sleep(10 * time.Millisecond)
}
StateMutex.Unlock()
fmt.Println(" -> [SYNC COMPLETE] Local ledger aligned with mainnet. Resuming mining...")
}
func ValidateBlockSignatures(b Block) bool {
targetPrefix := strings.Repeat("0", b.Difficulty)
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
	// Isolate the primary Coinbase transaction structure cleanly from the first slice index
	coinbaseTx := b.Transactions[0]

	var totalCoinbasePayout float64 = 0.0
	for _, out := range coinbaseTx.Outputs {
		totalCoinbasePayout += out.Amount
	}

targetBlockHeight := int64(b.Index)
allowedTotalEmission := CalculateBlockReward(targetBlockHeight)
if targetBlockHeight > 21024000 {
allowedTotalEmission += 5.0
}
if (totalCoinbasePayout - allowedTotalEmission) > 0.00001 {
fmt.Printf("🚨 EMISSION BLOCKED: Claim over-allocated payout: %f // Allowed: %f\n", totalCoinbasePayout, allowedTotalEmission)
return false
}
if b.UncleHash != "" && b.UncleHash != "0000000000000000000000000000000000000000000000000000000000000000" {
if len(coinbaseTx.Outputs) < 2 {
fmt.Println("🚨 CONSENSUS REJECTION: Uncle hash requires two coinbase outputs.")
return false
}
mainMinerPayout := coinbaseTx.Outputs[0].Amount
uncleMinerPayout := coinbaseTx.Outputs[1].Amount
expectedMainSplit := allowedTotalEmission * 0.80
expectedUncleSplit := allowedTotalEmission * 0.20
if (mainMinerPayout - expectedMainSplit) > 0.00001 || (uncleMinerPayout - expectedUncleSplit) > 0.00001 {
fmt.Printf("🚨 REJECTION: Invalid Split. Main: %f, Uncle: %f\n", mainMinerPayout, uncleMinerPayout)
return false
}
}
return true
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
defer BalanceCacheMutex.RUnlock()
if val, exists := StateBalanceCache[address]; exists {
return val
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