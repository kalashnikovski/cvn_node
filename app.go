//go:build !server
// +build !server

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// App struct
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved cleanly
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	fmt.Println("🛰️ [WAILS LIFECYCLE] Core context securely mounted into application interface framework.")
}

// GetLocalNodeTelemetry marshals real-time L1 telemetry straight to your Svelte frontend with absolute zero mutex deadlocks
func (a *App) GetLocalNodeTelemetry() string {
	latest := GetLatestBlock()
	
	syncStatusIndicator := true
	if latest.Index == 0 {
		syncStatusIndicator = false
	}
	
	telemetry := map[string]interface{}{
		"current_height": latest.Index,
		"block_hash":     latest.Hash,
		"miner_address":  CustomMinerAddress,
		"difficulty":     latest.Difficulty,
		"is_synced":      syncStatusIndicator, 
	}
	
	data, _ := json.Marshal(telemetry)
	return string(data)
}

// FetchWalletBalanceQuery pulls verified balances straight out of your BoltDB memory matrix cache
func (a *App) FetchWalletBalanceQuery(address string) float64 {
	BalanceCacheMutex.RLock()
	defer BalanceCacheMutex.RUnlock()
	
	if val, exists := StateBalanceCache[address]; exists {
		return val
	}
	return 0.0
}

// BroadcastLocalTransactionSubmit safely injects user payloads straight into your active mempool tracks
func (a *App) BroadcastLocalTransactionSubmit(recipient, amountStr, feeStr string) string {
	amount, _ := strconv.ParseFloat(amountStr, 64)
	fee, _ := strconv.ParseFloat(feeStr, 64)

	if amount <= 0 || strings.TrimSpace(recipient) == "" || recipient == "1" {
		return "ALERT: Transaction rejected by Phase 3 address validation firewall filters."
	}

	BalanceCacheMutex.RLock()
	availableBalance := StateBalanceCache[CustomMinerAddress]
	BalanceCacheMutex.RUnlock()

	if availableBalance < (amount + fee) {
		return "REJECTED: Insufficient unspent cryptographic utility reserves."
	}

	tSeed := fmt.Sprintf("%d-%s-%s-%f", time.Now().UnixNano(), CustomMinerAddress, recipient, amount)
	txID := fmt.Sprintf("TX_DESKTOP_%x", sha256.Sum256([]byte(tSeed)))

	tx := Transaction{
		ID: txID,
		Inputs: []UTXOInput{
			{
				SourceTxID: "TX_GENESIS_INITIAL_POOL",
				Index:      0,
			},
		},
		Outputs: []UTXOOutput{
			{Recipient: recipient, Amount: amount},
			{Recipient: CustomMinerAddress, Amount: availableBalance - (amount + fee)}, 
		},
		FreeWillOffering: fee,
		Witness:          CustomMinerAddress,
	}

	if MempoolMatrix.PushTransaction(tx) {
		go BroadcastTransactionToMesh(tx)
		return "SUCCESS: Payload securely committed to local mempool propagation queue!"
	}
	return "REJECTED: Active mempool spam threshold ceiling saturated."
}