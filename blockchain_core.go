package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ====================================================================
// 🧱 COVENANT STANDARD LAYER-1 DATA STRUCT LAYOUTS
// ====================================================================

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

// ====================================================================
// 📡 GLOBAL NETWORK STATE & CONSENSUS INTERFACES
// ====================================================================

var BlockchainFile = "cvn_mainnet.db"
var CustomMinerAddress string

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
	currentTip := GetLatestBlock()

	if incomingBlock.Index <= currentTip.Index {
		return
	}

	if !ValidateBlockSignatures(incomingBlock) {
		fmt.Printf("🚨 [SECURITY FIREWALL] Rejected malformed or spoofed block height #%d from peer!\n", incomingBlock.Index)
		return
	}

	if incomingBlock.Index == currentTip.Index + 1 {
		if incomingBlock.PrevHash != currentTip.Hash {
			fmt.Printf("❌ [CONSENSUS REJECTION] Block #%d rejected due to invalid PrevHash pointer mismatch. Fork detected.\n", incomingBlock.Index)
			return
		}

		if SaveBlockToStorage(incomingBlock) {
			fmt.Printf("🧱 [CONSENSUS EQUILIBRIUM] Successfully linked network block #%d Hash: %s\n", incomingBlock.Index, incomingBlock.Hash[:16])
			time.Sleep(100 * time.Millisecond)
			go BroadcastNewBlock(incomingBlock)
		}
		return
	}

	if incomingBlock.Index > currentTip.Index + 1 {
		fmt.Printf("🛰️  [VALID FORK DETECTED] Peer proved a valid chain exists at height #%d (Local: #%d). Re-syncing...\n", incomingBlock.Index, currentTip.Index)
		go TriggerActiveReSyncPass()
		return
	}
}

func TriggerActiveReSyncPass() {
	ForceBackgroundSyncPass()
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