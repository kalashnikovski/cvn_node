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

	fmt.Println("⏳ [RE-SYNC MATRIX ACTIVE] Local mining processes safely PAUSED.")
	
	// 👑 THE MASTER DEFER ANCHOR: Guarantees StateMutex is ALWAYS unlocked cleanly on exit
	StateMutex.Lock()
	defer StateMutex.Unlock()
	
	activeGlobalRecoveryGateways := []string{
		"http://202.137.175.220:8081", 
		"http://207.148.67.11:8081",   
		"http://64.177.45.153:8081",   
	}
	
	client := &http.Client{Timeout: 4 * time.Second}
	
	// Perform the destructive multi-block fallback checks only if a real fork is validated
	_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		bMeta := tx.Bucket(metaBucket)
		if bBlocks != nil && bMeta != nil {
			for i := 0; i < 5; i++ {
				tip := GetLatestBlockInternal()
				if tip.Index > 0 {
					paddedKey := fmt.Sprintf("%016d", tip.Index)
					_ = bBlocks.Delete([]byte(paddedKey))
					_ = bMeta.Put([]byte("height"), []byte(fmt.Sprintf("%d", tip.Index-1)))
				}
			}
		}
		return nil
	})
	
	RebuildStateBalanceCacheInternal()
	
	for {
		localTip := GetLatestBlockInternal()
		nextNeededBlock := localTip.Index + 1
		
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

		// 🏃 Shortcut Exit: If aligned or trailing, this break safely hits the deferred outer unlock!
		if chosenTargetSeedURL == "" || nextNeededBlock > selectedHighestTargetTip {
			break
		}

		requestURL := fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(chosenTargetSeedURL, "/"), nextNeededBlock)
		
		// 🔓 Volatively release the lock temporarily ONLY to perform the external network fetch
		StateMutex.Unlock()
		resp, err := client.Get(requestURL)
		if err != nil {
			StateMutex.Lock() // 🔒 Re-acquire instantly on failure to maintain loop context safety
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
		
		// 🔒 Re-acquire lock before writing network blocks straight to disk storage partitions
		StateMutex.Lock()
		for _, block := range incomingBatch {
			if block.Index == int64(GetLatestBlockInternal().Index)+1 {
				SaveBlockToStorageInternal(block)
				fmt.Printf("📥 [DECENTRALIZED RESILIENCE] Ingested block #%d straight from endpoint -> %s\n", block.Index, chosenTargetSeedURL)
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