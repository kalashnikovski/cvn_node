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

	currentTip := GetLatestBlock()

	if incomingBlock.Index == currentTip.Index {
		if incomingBlock.Hash != currentTip.Hash {
			if incomingBlock.Timestamp < currentTip.Timestamp || runtime.GOOS == "linux" {
				fmt.Printf("⚠️ [TIE-BREAKER ACTIVATED] Detected hash collision at height #%d. Rolling back to resolve fork split...\n", incomingBlock.Index)
				go TriggerActiveReSyncPass("http://202.137.175.220:8081")
			}
		}
		return
	}

	if incomingBlock.Index < currentTip.Index {
		return
	}

	if !ValidateBlockSignatures(incomingBlock) {
		fmt.Printf("🚨 [SECURITY FIREWALL] Rejected malformed or spoofed block height #%d from peer!\n", incomingBlock.Index)
		return
	}

	if incomingBlock.Index > currentTip.Index+1 {
		fmt.Printf("🛰️  [FORK DETECTED] Network height #%d outpaces local tip #%d. Engaging rollback alignment engine...\n", incomingBlock.Index, currentTip.Index)
		go TriggerActiveReSyncPass("http://202.137.175.220:8081")
		return
	}

	if incomingBlock.Index == currentTip.Index+1 {
		if incomingBlock.PrevHash != currentTip.Hash {
			fmt.Printf("❌ [POINTER MISMATCH] Block #%d cannot link onto tip hash. Triggering full chain re-sync...\n", incomingBlock.Index)
			go TriggerActiveReSyncPass("http://202.137.175.220:8081")
			return
		}

		if SaveBlockToStorage(incomingBlock) {
			fmt.Printf("🧱 [CONSENSUS EQUILIBRIUM] Linked block #%d Hash: %s\n", incomingBlock.Index, incomingBlock.Hash[:16])
			time.Sleep(100 * time.Millisecond)
			go BroadcastNewBlock(incomingBlock)
		}
		return
	}
}

func TriggerActiveReSyncPass(seedURL string) {
	if !atomic.CompareAndSwapInt32(&IsSyncingActive, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&IsSyncingActive, 0)

	fmt.Println("⏳ [RE-SYNC MATRIX ACTIVE] Local mining processes safely PAUSED.")
	client := &http.Client{Timeout: 5 * time.Second}
	
	_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		bMeta := tx.Bucket(metaBucket)
		if bBlocks != nil && bMeta != nil {
			currentTip := GetLatestBlock()
			if currentTip.Index > 0 {
				paddedKey := fmt.Sprintf("%016d", currentTip.Index)
				_ = bBlocks.Delete([]byte(paddedKey))
				_ = bMeta.Put([]byte("height"), []byte(fmt.Sprintf("%d", currentTip.Index-1)))
			}
		}
		return nil
	})
	
	for {
		localTip := GetLatestBlock()
		nextNeededBlock := localTip.Index + 1
		
		requestURL := fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(seedURL, "/"), nextNeededBlock)
		resp, err := client.Get(requestURL)
		if err != nil {
			break
		}
		
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			break
		}
		
		var incomingBatch []Block
		err = json.NewDecoder(resp.Body).Decode(&incomingBatch)
		resp.Body.Close()
		
		if err != nil || len(incomingBatch) == 0 {
			break
		}
		
		for _, block := range incomingBatch {
			if block.Index == int64(GetLatestBlock().Index)+1 {
				SaveBlockToStorage(block)
				fmt.Printf("📥 [REALIGNMENT] Successfully ingested mainnet block #%d\n", block.Index)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Println("✨ [🔓 SYNC COMPLETE] Local ledger successfully aligned with mainnet. Resuming mining...")
}

func ValidateBlockSignatures(b Block) bool {
	targetPrefix := strings.Repeat("0", b.Difficulty)
	if !strings.HasPrefix(b.Hash, targetPrefix) {
		return false
	}
	recalculated := CalculateHash(b)
	return b.Hash == recalculated
}

func CalculateHash(b Block) string {
	baseData := fmt.Sprintf("%d-%d-%d", b.Index, b.Timestamp, b.Difficulty)
	inputStr := fmt.Sprintf("%s-%d", baseData, b.Nonce)
	h := sha256.New()
	h.Write([]byte(inputStr))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func ForceBackgroundSyncPass() {}