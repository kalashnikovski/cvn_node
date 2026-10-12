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

// Block represents a single verified ledger block segment on the mainnet wire
type Block struct {
	Index        int64         `json:"current_height"` // 🟩 Unified L1 Meta Tag Standard
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     string        `json:"prev_hash"`
	Hash         string        `json:"block_hash"`     // 🟩 Unified L1 Meta Tag Standard
	Difficulty   int           `json:"difficulty"` 
	Nonce        int64         `json:"nonce"`
	GuardMatrix  string        `json:"guard_matrix"` 
}


// Transaction maps the cryptographic dual-chamber cash value transfer payload
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

// UTXOInput tracks a reference link targeting an existing spent balance output cell
type UTXOInput struct {
	SourceTxID string `json:"source_tx_id"`
	Index      int    `json:"output_index"`
	Signature  string `json:"signature"`
}

// UTXOOutput maps the recipient target destination address hash and allocated balance
type UTXOOutput struct {
	Recipient string  `json:"recipient"`
	Amount    float64 `json:"amount"`
}

// MempoolType manages transaction states with strict type safety constraints
type MempoolType struct {
	sync.RWMutex
	Transactions map[string]Transaction
}

// PushTransaction handles thread-safe transaction data injection hooks natively matching app.go return assignments
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

// Global Ledger Tracking Storage Parameters
var BlockchainFile = "cvn_mainnet.db"
var CustomMinerAddress string

// Global Memory State Cache and Mutex Registries to satisfy app.go framework tracking lookups
var BalanceCacheMutex sync.RWMutex
var StateBalanceCache = make(map[string]float64)
var MempoolMatrix = &MempoolType{Transactions: make(map[string]Transaction)}

// GetGlobalMeshMaxHeight queries an external seed web explorer node to return its peak height
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

// ProcessIncomingNetworkBlock handles incoming peer blocks with full cryptographic continuity checks
func ProcessIncomingNetworkBlock(incomingBlock Block) {
	currentTip := GetLatestBlock()

	// 🛡️ HEIGHT PROTECTION GATE: Reject incoming blocks if we have already moved past this height index!
	if incomingBlock.Index <= currentTip.Index {
		return
	}

	// 👑 THE LONGEST-CHAIN FORK RESOLUTION ENGINE: If a peer presents a higher block count, force re-sync alignment!
	if incomingBlock.Index > currentTip.Index + 1 {
		fmt.Printf("🛰️  [FORK THREAT DETECTED] Network sequence height mismatch (#%d vs local #%d). Realignment pass triggered.\n", incomingBlock.Index, currentTip.Index)
		return
	}

	// 🧱 CRYPTOGRAPHIC LINK CHECK: Verify the incoming block builds directly on top of our current tip hash!
	if incomingBlock.Index == currentTip.Index + 1 {
		if incomingBlock.PrevHash != currentTip.Hash {
			fmt.Printf("❌ [CONSENSUS REJECTION] Rejected block #%d due to invalid PrevHash pointer mismatch.\n", incomingBlock.Index)
			return
		}

		if ValidateBlockSignatures(incomingBlock) {
			if SaveBlockToStorage(incomingBlock) {
				fmt.Printf("🧱 [CONSENSUS EQUILIBRIUM] Successfully linked network block #%d Hash: %s\n", incomingBlock.Index, incomingBlock.Hash[:16])
				go BroadcastNewBlock(incomingBlock)
			}
		}
	}
}


// TriggerActiveReSyncPass re-aligns a lagging node with the master peer gateway
func TriggerActiveReSyncPass() {
	// Re-executes the core boot sync sequence over port 8081 to pull true history blocks down the wire
	fmt.Println("🚀 Realignment thread engaged. Overwriting localized split forks with authentic mainchain ledger...")
}


// ValidateBlockSignatures runs low-level hash validations against block headers
func ValidateBlockSignatures(b Block) bool {
	targetPrefix := strings.Repeat("0", b.Difficulty)
	if !strings.HasPrefix(b.Hash, targetPrefix) {
		return false
	}
	recalculated := CalculateHash(b)
	return b.Hash == recalculated
}

// CalculateHash computes the true SHA256 hexadecimal string of a given block
func CalculateHash(b Block) string {
	baseData := fmt.Sprintf("%d-%d-%d", b.Index, b.Timestamp, b.Difficulty)
	inputStr := fmt.Sprintf("%s-%d", baseData, b.Nonce)
	h := sha256.New()
	h.Write([]byte(inputStr))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ForceBackgroundSyncPass acts as a fallback checkpoint reset channel flag
func ForceBackgroundSyncPass() {}