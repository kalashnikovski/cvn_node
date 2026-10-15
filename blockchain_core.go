package main

import (
	"crypto/sha256"
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

type Block struct {
	Index        int64         `json:"current_height"`
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     string        `json:"prev_hash"`
	Hash         string        `json:"block_hash"`
	Difficulty   int           `json:"difficulty"` 
	Nonce        int64         `json:"nonce"`
	GuardMatrix  string        `json:"guard_matrix"` 
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
	}
	if blockIndex <= 10512000 {
		return 25.0
	}
	if blockIndex <= 15768000 {
		return 12.5
	}
	if blockIndex <= 21024000 {
		return 6.25
	}
	return 0.0
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
	currentTip := GetLatestBlockInternal()

	if incomingBlock.Index == currentTip.Index {
		if incomingBlock.Hash != currentTip.Hash {
			if incomingBlock.Timestamp < currentTip.Timestamp || runtime.GOOS == "linux" {
				fmt.Printf("⚠️ [HASH COLLISION] Conflict at height #%d. Resolving tie-breaker...\n", incomingBlock.Index)
				StateMutex.Unlock()
				go TriggerActiveReSyncPass()
				return
			}
		}
		StateMutex.Unlock()
		return
	}

	if incomingBlock.Index < currentTip.Index {
		StateMutex.Unlock()
		return
	}

	if !ValidateBlockSignatures(incomingBlock) {
		fmt.Printf("🚨 [SECURITY FIREWALL] Rejected malformed or spoofed block height #%d from peer!\n", incomingBlock.Index)
		StateMutex.Unlock()
		return
	}

	if incomingBlock.Index > currentTip.Index+1 {
		fmt.Printf("🛰️  [FORK DETECTED] Network height #%d outpaces local tip #%d. Rewinding states...\n", incomingBlock.Index, currentTip.Index)
		StateMutex.Unlock()
		go TriggerActiveReSyncPass()
		return
	}

	if incomingBlock.Index == currentTip.Index+1 {
		if incomingBlock.PrevHash != currentTip.Hash {
			fmt.Printf("❌ [POINTER MISMATCH] Block #%d cannot link onto tip hash. Initializing rollback...\n", incomingBlock.Index)
			StateMutex.Unlock()
			go TriggerActiveReSyncPass()
			return
		}

		if SaveBlockToStorageInternal(incomingBlock) {
			fmt.Printf("🧱 [CONSENSUS EQUILIBRIUM] Linked block #%d Hash: %s\n", incomingBlock.Index, incomingBlock.Hash[:16])
			StateMutex.Unlock()
			time.Sleep(50 * time.Millisecond)
			go BroadcastNewBlock(incomingBlock)
			return
		}
		StateMutex.Unlock()
		return
	}
	StateMutex.Unlock()
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
	return "", fmt.Errorf("block at height %d missing from remote response", height)
}

func TriggerActiveReSyncPass() {
	if !atomic.CompareAndSwapInt32(&IsSyncingActive, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&IsSyncingActive, 0)

	fmt.Println("⏳ [RE-SYNC MATRIX ACTIVE] Local mining processes safely PAUSED.")
	
	StateMutex.Lock()
	defer StateMutex.Unlock()
	
	activeGlobalRecoveryGateways := []string{
		"http://202.137.175.220:8081", 
		"http://207.148.67.11:8081",   
		"http://64.177.45.153:8081",   
	}
	
	client := &http.Client{Timeout: 4 * time.Second}
	
	var chosenTargetSeedURL string = ""
	var selectedHighestTargetTip int64 = 0

	for _, endpoint := range activeGlobalRecoveryGateways {
		if strings.Contains(endpoint, "202.137.175.220") && runtime.GOOS == "windows" {
			continue
		}
		
		remotePeerHeight := GetGlobalMeshMaxHeight(endpoint)
		if remotePeerHeight > selectedHighestTargetTip {
			selectedHighestTargetTip = remotePeerHeight
			chosenTargetSeedURL = endpoint
		}
	}

	if chosenTargetSeedURL == "" {
		fmt.Println("🚨 [RE-SYNC ALERT] Peer matrix unreachable. Aborting re-sync path.")
		return
	}

	localTip := GetLatestBlockInternal()
	scanHeight := localTip.Index
	var commonAncestorHeight int64 = -1

	fmt.Printf("🔍 [ANCESTOR SEARCH] Initiating backward cryptographic scan from local height #%d against peer %s...\n", scanHeight, chosenTargetSeedURL)

	for scanHeight >= 0 {
		var localHash string
		err := GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
			bBlocks := tx.Bucket(blocksBucket)
			if bBlocks == nil {
				return fmt.Errorf("blocks bucket missing")
			}
			paddedKey := fmt.Sprintf("%016d", scanHeight)
			data := bBlocks.Get([]byte(paddedKey))
			if data == nil {
				return fmt.Errorf("local block data missing")
			}
			var blk Block
			if err := json.Unmarshal(data, &blk); err != nil {
				return err
			}
			localHash = blk.Hash
			return nil
		})

		if err != nil {
			scanHeight--
			continue
		}

		StateMutex.Unlock()
		remoteHash, remoteErr := fetchRemoteBlockHeader(client, chosenTargetSeedURL, scanHeight)
		StateMutex.Lock()

		if remoteErr == nil && localHash == remoteHash {
			commonAncestorHeight = scanHeight
			break
		}

		scanHeight--
	}

	if commonAncestorHeight == -1 {
		fmt.Println("🚨 [CRITICAL FORK] No common ancestor found. Halting chain reorganization to protect states.")
		return
	}

	fmt.Printf("🎯 [COMMON ANCESTOR FOUND] Shared history matches at height #%d.\n", commonAncestorHeight)

	if commonAncestorHeight < localTip.Index {
		fmt.Printf("✂️ [SURGICAL ROLLBACK] Reorganizing database ledger down to height #%d...\n", commonAncestorHeight)
		
		err := GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
			bBlocks := tx.Bucket(blocksBucket)
			bMeta := tx.Bucket(metaBucket)
			bUTXO := tx.Bucket(utxoBucket)
			if bBlocks == nil || bMeta == nil || bUTXO == nil {
				return fmt.Errorf("essential database partitions are missing")
			}

			for h := localTip.Index; h > commonAncestorHeight; h-- {
				paddedKey := fmt.Sprintf("%016d", h)
				blockData := bBlocks.Get([]byte(paddedKey))
				if blockData == nil {
					continue
				}

				var block Block
				if err := json.Unmarshal(blockData, &block); err != nil {
					return err
				}

				for _, txPayload := range block.Transactions {
					for outIdx := range txPayload.Outputs {
						compoundOutputKey := fmt.Sprintf("%s_%d", txPayload.ID, outIdx)
						_ = bUTXO.Delete([]byte(compoundOutputKey))
					}

					if !strings.HasPrefix(txPayload.ID, "TX_COINBASE_") {
						for _, input := range txPayload.Inputs {
							var historicalBlock Block
							foundSourceBlock := false
							
							c := bBlocks.Cursor()
							for k, v := c.First(); k != nil; k, v = c.Next() {
								var blk Block
								if json.Unmarshal(v, &blk) == nil {
									for _, historicalTx := range blk.Transactions {
										if historicalTx.ID == input.SourceTxID {
											historicalBlock = blk
											foundSourceBlock = true
											break
										}
									}
								}
								if foundSourceBlock {
									break
								}
							}

							if foundSourceBlock && historicalBlock.Index <= commonAncestorHeight {
								for historicalOutIdx, historicalOut := range historicalBlock.Transactions[0].Outputs {
									if historicalBlock.Transactions[0].ID == input.SourceTxID && historicalOutIdx == input.Index {
										compoundInputKey := fmt.Sprintf("%s_%d", input.SourceTxID, input.Index)
										utxoRecord := UTXORecord{
											Amount:    historicalOut.Amount,
											Recipient: historicalOut.Recipient,
										}
										utxoData, _ := json.Marshal(utxoRecord)
										_ = bUTXO.Put([]byte(compoundInputKey), utxoData)
									}
								}
								
								for _, historicalTx := range historicalBlock.Transactions {
if historicalTx.ID == input.SourceTxID {
if input.Index < len(historicalTx.Outputs) {
compoundInputKey := fmt.Sprintf("%s_%d", input.SourceTxID, input.Index)
utxoRecord := UTXORecord{
Amount:    historicalTx.Outputs[input.Index].Amount,
Recipient: historicalTx.Outputs[input.Index].Recipient,
}
utxoData, _ := json.Marshal(utxoRecord)
_ = bUTXO.Put([]byte(compoundInputKey), utxoData)
}
}
}
}
}
}
}
_ = bBlocks.Delete([]byte(paddedKey))
}
paddedAncestorKey := fmt.Sprintf("%016d", commonAncestorHeight)
_ = bMeta.Put([]byte("height"), []byte(paddedAncestorKey))
return nil
})
if err != nil {
fmt.Printf("🚨 [ROLLBACK FAILURE] Database transaction rolled back safely: %v\n", err)
return
}
atomic.StoreInt64(&localHeightState, commonAncestorHeight)
RebuildStateBalanceCacheInternal()
fmt.Println("✅ [ROLLBACK MATRIX COMPLETED] Minority state entries cleanly decoupled.")
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
fmt.Printf("📥 [DECENTRALIZED RESILIENCE] Ingested block #%d straight from endpoint -> %s\n", block.Index, chosenTargetSeedURL)
} else {
fmt.Printf("🚨 [SECURITY CATCHUP PASS] Malformed peer block detected at height #%d. Halting ingest pipeline.\n", block.Index)
break
}
}
}
time.Sleep(10 * time.Millisecond)
}
fmt.Println("✨ [🔓 SYNC COMPLETE] Local ledger aligned with mainnet. Resuming mining...")
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
coinbaseTx := b.Transactions[0]
if !strings.HasPrefix(coinbaseTx.ID, "TX_COINBASE_") {
return false
}
var totalCoinbasePayout float64 = 0.0
for _, out := range coinbaseTx.Outputs {
totalCoinbasePayout += out.Amount
}
allowedEmission := CalculateBlockReward(b.Index)
if b.Index > 21024000 {
allowedEmission += 5.0
}
if totalCoinbasePayout > allowedEmission {
return false
}
return true
}
func CalculateHash(b Block) string {
txBytes, err := json.Marshal(b.Transactions)
var txHashStr string
if err != nil {
hTx := sha256.Sum256([]byte("[]"))
txHashStr = fmt.Sprintf("%x", hTx[:])
} else {
hTx := sha256.Sum256(txBytes)
txHashStr = fmt.Sprintf("%x", hTx[:])
}
inputStr := fmt.Sprintf("%d-%d-%s-%s-%d-%s-%d",
b.Index,
b.Timestamp,
b.PrevHash,
txHashStr,
b.Difficulty,
b.GuardMatrix,
b.Nonce,
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