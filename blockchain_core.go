package main

import (
	"sync"
)

// Block represents a single verified ledger block segment on the mainnet wire
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

// Transaction maps the cryptographic dual-chamber cash value transfer payload
type Transaction struct {
	ID               string       `json:"tx_id"`
	Inputs           []UTXOInput  `json:"inputs"`
	Outputs          []UTXOOutput `json:"outputs"`
	Signature        string       `json:"signature"`
	PublicKey        string       `json:"public_key"` // ✅ FIXED: Explicitly carries the sender's un-hashed public key for true validation checks
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
	Transactions map[string]Transaction // ✅ FIXED: Enforced strict Transaction type safety
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

// Global Ledger Tracking Storage Parameters
var BlockchainFile = "cvn_mainnet.db"
var CustomMinerAddress string

// Global Memory State Cache and Mutex Registries to satisfy app.go framework tracking lookups
var BalanceCacheMutex sync.RWMutex
var StateBalanceCache = make(map[string]float64)
var MempoolMatrix = &MempoolType{Transactions: make(map[string]Transaction)}