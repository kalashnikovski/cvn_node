//go:build !server
// +build !server

package main

import (
	"context"
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

// startup is called when the app starts. The context is saved
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// GetLocalNodeTelemetry marshals real-time L1 telemetry straight to your Svelte frontend
func (a *App) GetLocalNodeTelemetry() string {
	latest := GetLatestBlock()
	
	// Create a thread-safe telemetry map matching your App.svelte state variable expectations
	telemetry := map[string]interface{}{
		"current_height": latest.Index,
		"block_hash":     latest.Hash,
		"miner_address":  CustomMinerAddress,
		"difficulty":     latest.Difficulty,
	}
	
	data, _ := json.Marshal(telemetry)
	return string(data)
}

// FetchWalletBalanceQuery pulls verified balances straight out of your BoltDB memory matrix cache
func (a *App) FetchWalletBalanceQuery(address string) float64 {
	BalanceCacheMutex.RLock()
	defer BalanceCacheMutex.RUnlock()
	
	// Dynamically query your live mainnet state allocations
	if val, exists := StateBalanceCache[address]; exists {
		return val
	}
	return 0.0
}

// BroadcastLocalTransactionSubmit safely injects user payloads straight into your active mempool tracks
func (a *App) BroadcastLocalTransactionSubmit(recipient, amountStr, feeStr string) string {
	amount, _ := strconv.ParseFloat(amountStr, 64)
	fee, _ := strconv.ParseFloat(feeStr, 64)

	// Phase 3 Firewall Protection: Prevent malformed address or zero volume injections
	if amount <= 0 || strings.TrimSpace(recipient) == "" || recipient == "1" {
		return "ALERT: Transaction rejected by Phase 3 address validation firewall filters."
	}

	tx := Transaction{
		ID:               fmt.Sprintf("TX_DESKTOP_%d", time.Now().UnixNano()),
		Inputs:           []UTXOInput{{SourceTxID: "TX_GENESIS_INITIAL_POOL", Index: 0}},
		Outputs:          []UTXOOutput{{Recipient: recipient, Amount: amount}},
		FreeWillOffering: fee,
		Witness:          CustomMinerAddress,
	}

	if MempoolMatrix.PushTransaction(tx) {
		return "SUCCESS: Payload securely committed to local mempool propagation queue!"
	}
	return "REJECTED: Active mempool spam threshold ceiling saturated."
}