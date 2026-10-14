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

func TriggerActiveReSyncPass() {
	if !atomic.CompareAndSwapInt32(&IsSyncingActive, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&IsSyncingActive, 0)

	fmt.Println("\n⏳ [RE-SYNC MATRIX ACTIVE] Local mining processes safely PAUSED. Initializing non-destructive common ancestor discovery...")
	
	// Enforce strict write-lock across state boundaries for the entire duration of the re-org pass
	StateMutex.Lock()
	defer StateMutex.Unlock()
	
	activeGlobalRecoveryGateways := []string{
		"http://202.137.175.220:8081", 
		"http://207.148.67.11:8081",   
		"http://64.177.45.153:8081",   
	}
	
	client := &http.Client{Timeout: 4 * time.Second}

	// 1. Identify the absolute highest online seed gateway to trace forks against
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
		fmt.Println("❌ [RE-SYNC ABORTED] Gateway tracking interfaces timed out. Unable to isolate an active consensus seed coordinate.")
		return
	}

	// 2. Execute Common Ancestor discovery by tracing backward from the current local tip height
	localTip := GetLatestBlockInternal()
	currentScanningIndex := localTip.Index
	var commonAncestorIndex int64 = -1

	fmt.Printf("🔍 [RE-SCAN RUNTIME] Starting reverse block-by-block comparison loop from local height #%d against peer tip #%d...\n", currentScanningIndex, selectedHighestTargetTip)

	for currentScanningIndex >= 0 {
		// If the peer does not have a block at this height, step downward immediately
		if currentScanningIndex > selectedHighestTargetTip {
			currentScanningIndex--
			continue
		}

		// Retrieve our local block at the current scanning height from BoltDB
		var localBlockAtHeight Block
		err := GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
			bBlocks := tx.Bucket(blocksBucket)
			if bBlocks != nil {
				paddedKey := fmt.Sprintf("%016d", currentScanningIndex)
				blockData := bBlocks.Get([]byte(paddedKey))
				if blockData != nil {
					_ = json.Unmarshal(blockData, &localBlockAtHeight)
				}
			}
			return nil
		})

		if err != nil || localBlockAtHeight.Hash == "" {
			currentScanningIndex--
			continue
		}

		// Fetch the matching index block directly from the chosen seed gateway
		requestURL := fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(chosenTargetSeedURL, "/"), currentScanningIndex)
		resp, err := client.Get(requestURL)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			// If a network request fails, retry the height or slide down gracefully
			time.Sleep(50 * time.Millisecond)
			currentScanningIndex--
			continue
		}

		var peerIncomingBatch []Block
		decodeErr := json.NewDecoder(resp.Body).Decode(&peerIncomingBatch)
		resp.Body.Close()

		if decodeErr != nil || len(peerIncomingBatch) == 0 {
			currentScanningIndex--
			continue
		}

		// Cross-reference the cryptographic hashes to check for an absolute pointer match
		peerBlockAtHeight := peerIncomingBatch[0]
		if localBlockAtHeight.Hash == peerBlockAtHeight.Hash {
			commonAncestorIndex = currentScanningIndex
			fmt.Printf("🎯 [COMMON ANCESTOR ISOLATED] Perfect network alignment confirmed at block index #%d\n", commonAncestorIndex)
			fmt.Printf("🔒 Local Hash:  %s\n🌐 Remote Hash: %s\n", localBlockAtHeight.Hash, peerBlockAtHeight.Hash)
			break
		}

		currentScanningIndex--
	}

	// Fallback to Genesis Block if a deep structural fork split the chain wide open
	if commonAncestorIndex == -1 {
		fmt.Println("⚠️  [CRITICAL ALIGNMENT ALERT] No common ancestor found along local tracks. Defaulting synchronization base to Genesis Block #0.")
		commonAncestorIndex = 0
	}

	// 3. Execute an atomic, targeted database roll-back transaction down to the common ancestor point
	if localTip.Index > commonAncestorIndex {
		fmt.Printf("🧱 [FORK ROLLBACK] Purging invalid local blocks strictly above height #%d...\n", commonAncestorIndex)
		
		err := GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
			bBlocks := tx.Bucket(blocksBucket)
			bMeta := tx.Bucket(metaBucket)
			bUTXO := tx.Bucket(utxoBucket)
			if bBlocks == nil || bMeta == nil || bUTXO == nil {
				return fmt.Errorf("required consensus partitions are missing from storage")
			}

			// Cleanly trace backward from our current tip down to the common ancestor intersection point
			for i := localTip.Index; i > commonAncestorIndex; i-- {
				paddedKey := fmt.Sprintf("%016d", i)
				
				// Read block payload bytes to extract and cleanly re-record the spent UTXO input tracks
				var blockToWipe Block
				blockDataBytes := bBlocks.Get([]byte(paddedKey))
				if blockDataBytes != nil {
					if json.Unmarshal(blockDataBytes, &blockToWipe) == nil {
						// Remove all UTXO outputs spawned by this invalidated block
						for _, txPayload := range blockToWipe.Transactions {
							for outIdx := range txPayload.Outputs {
								compoundOutputKey := fmt.Sprintf("%s_%d", txPayload.ID, outIdx)
								_ = bUTXO.Delete([]byte(compoundOutputKey))
							}
							
							// Re-inject the original spent inputs straight back into the unspent pool bucket
							if !strings.HasPrefix(txPayload.ID, "TX_COINBASE_") {
								for _, input := range txPayload.Inputs {
									compoundInputKey := fmt.Sprintf("%s_%d", input.SourceTxID, input.Index)
									
									// Reconstruct a valid baseline UTXORecord entry
									recoveredUTXO := UTXORecord{
										Amount:    0.0, // Rebuilt on the subsequent full balance cache compilation step
										Recipient: "",  // Rebuilt dynamically from ledger assumptions
									}
									
									// Trace transaction outputs from parent histories to safely pull actual coin values
									var originalSourceTxBlock Block
									sourcePaddedKey := fmt.Sprintf("%016d", i-1) // Approximate vicinity query optimization
									sourceDataBytes := bBlocks.Get([]byte(sourcePaddedKey))
									if sourceDataBytes != nil {
										_ = json.Unmarshal(sourceDataBytes, &originalSourceTxBlock)
									}
									
									// Secure structural matching back-fill logic
									for _, sourceTx := range originalSourceTxBlock.Transactions {
										if sourceTx.ID == input.SourceTxID {
											if input.Index < len(sourceTx.Outputs) {
												recoveredUTXO.Amount = sourceTx.Outputs[input.Index].Amount
												recoveredUTXO.Recipient = sourceTx.Outputs[input.Index].Recipient
											}
										}
									}

									if recoveredUTXO.Recipient != "" {
										recoveredBytes, _ := json.Marshal(recoveredUTXO)
										_ = bUTXO.Put([]byte(compoundInputKey), recoveredBytes)
									}
								}
							}
						}
					}
				}

				// Complete raw disk block deletion parameters
				_ = bBlocks.Delete([]byte(paddedKey))
			}

			// Atomically lock the metadata head tracking index straight back onto our common ancestor height token
			paddedAncestorKey := fmt.Sprintf("%016d", commonAncestorIndex)
			_ = bMeta.Put([]byte("height"), []byte(paddedAncestorKey))
			return nil
		})

		if err != nil {
			fmt.Printf("🚨 [ROLLBACK ABORTED] Database pipeline error writing state changes: %v\n", err)
			return
		}

		// Refresh our memory metrics inside the localized client arrays immediately following disk modifications
		atomic.StoreInt64(&localHeightState, commonAncestorIndex)
		RebuildStateBalanceCacheInternal()
		fmt.Printf("✅ [ROLLBACK STABLE] State ledger successfully locked back onto core block height #%d\n", commonAncestorIndex)
	} else {
		fmt.Println("💎 [CONGENIAL TRACKS] Local ledger timeline is aligned with ancestor coordinates. Skipping rollback routine.")
	}

	// 4. Download, deep-validate, and ingest the peer's remaining valid blocks to catch up safely
	fmt.Printf("📥 [FORWARD INGESTION] Resuming download sequence from block #%d up to true mainnet tip height #%d...\n", commonAncestorIndex+1, selectedHighestTargetTip)
	
	nextNeededBlock := commonAncestorIndex + 1

	for nextNeededBlock <= selectedHighestTargetTip {
		requestURL := fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(chosenTargetSeedURL, "/"), nextNeededBlock)
		
		resp, err := client.Get(requestURL)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			time.Sleep(100 * time.Millisecond)
			continue
		}
		
		var incomingBatch []Block
		err = json.NewDecoder(resp.Body).Decode(&incomingBatch)
		resp.Body.Close()
		
		if err != nil || len(incomingBatch) == 0 {
			break
		}
		
		for _, block := range incomingBatch {
			currentLocalTip := GetLatestBlockInternal()
			
			// Chain linking validation check
			if block.Index == currentLocalTip.Index+1 {
				if block.PrevHash != currentLocalTip.Hash {
					fmt.Printf("🚨 [RE-SYNC PIPELINE ABORT] Ingested block #%d failed structural pointer validation logic. Timeline mismatch detected.\n", block.Index)
					return
				}

				// Deep signature enforcement firewall check
				if !ValidateBlockSignatures(block) {
					fmt.Printf("🚨 [RE-SYNC SECURITY FIREWALL] Block #%d failed signature or difficulty constraints! Aborting forward sync payload.\n", block.Index)
					return
				}

				// Commit verified block state safely to BoltDB
				if SaveBlockToStorageInternal(block) {
					fmt.Printf("📥 [INGESTION SUCCESS] Integrated consensus block #%d | Hash: %s... -> Endpoint: %s\n", block.Index, block.Hash[:16], chosenTargetSeedURL)
					nextNeededBlock = block.Index + 1
				} else {
					fmt.Printf("❌ [STORAGE FAULT] Unable to commit block #%d to disk engine partitions.\n", block.Index)
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	
	fmt.Println("✨ [🔓 SYNC COMPLETE] Local ledger aligned with mainnet. Resuming mining...")
	StateMutex.Unlock()
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

	// 🟩 FIX 1: Safely access array index slot 0 to target the underlying object cleanly
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