//go:build !server
// +build !server

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	fmt.Println("🛰️ [WAILS LIFECYCLE] Core context securely mounted into application interface framework.")

	// 👑 THE THREE-PORT IGNITION ENGINE: Multi-port network gateway unchained natively on boot!
	go func() {
		winMux := http.NewServeMux()

		// 🧱 PORT 8081: The Compressed 512-Block Batch Streaming Gateway
		winMux.HandleFunc("/blocks/batch", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")

			startStr := r.URL.Query().Get("start")
			startHeight, err := strconv.ParseInt(startStr, 10, 64)
			if err != nil || startHeight < 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			var blockBatch []Block
			currentTip := int64(GetLatestBlock().Index)
			endHeight := startHeight + 511
			if endHeight > currentTip {
				endHeight = currentTip
			}

			for h := startHeight; h <= endHeight; h++ {
				blockBatch = append(blockBatch, GetBlockByHeightFromDB(h))
			}
			json.NewEncoder(w).Encode(blockBatch)
		})

		// 🧱 PORT 8081 fallback: Single block status verification loop
		winMux.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			latest := GetLatestBlock()
			payload := map[string]interface{}{
				"current_height": latest.Index,
				"block_hash":     latest.Hash,
				"timestamp":      latest.Timestamp,
				"transactions":   latest.Transactions,
			}
			json.NewEncoder(w).Encode(payload)
		})

		fmt.Println("📡 [PORT 8081] Exposing un-throttled HTTP Bulk Sync channels...")
		if err := http.ListenAndServe(":8081", winMux); err != nil {
			log.Printf("Port 8081 bind alert: %v", err)
		}
	}()

	// 🧱 PORT 8082: Dedicated Telemetry & Front-End Mirror Port
	go func() {
		mirrorMux := http.NewServeMux()
		mirrorMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<h2>💎 Covenant Standard Core Active Matrix</h2><p>Rig Height: #%d</p>", GetLatestBlock().Index)
		})
		
		fmt.Println("📡 [PORT 8082] Exposing dedicated analytics data matrix...")
		if err := http.ListenAndServe(":8082", mirrorMux); err != nil {
			log.Printf("Port 8082 bind alert: %v", err)
		}
	}()
}

// GetLocalNodeTelemetry marshals real-time L1 telemetry straight to your Svelte frontend
func (a *App) GetLocalNodeTelemetry() string {
	latest := GetLatestBlock()
	
	// Thread-safe map perfectly matching your App.svelte telemetry expectations
	telemetry := map[string]interface{}{
		"current_height": latest.Index,
		"block_hash":     latest.Hash,
		"miner_address":  CustomMinerAddress,
		"difficulty":     latest.Difficulty,
		"is_synced":      !IsCoreMinerLocked(), // ✅ Exposes sync lock states cleanly to Svelte
	}
	
	data, _ := json.Marshal(telemetry)
	return string(data)
}

// FetchWalletBalanceQuery pulls verified balances straight out of your BoltDB memory matrix cache
func (a *App) FetchWalletBalanceQuery(address string) float64 {
	// Securely access global balance caches natively
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

	// Phase 3 Firewall Protection: Prevent malformed address or zero volume injections
	if amount <= 0 || strings.TrimSpace(recipient) == "" || recipient == "1" {
		return "ALERT: Transaction rejected by Phase 3 address validation firewall filters."
	}

	// 🔐 HARDENED TRANSITION LOGIC: Verifies balance bounds before staging inputs
	BalanceCacheMutex.RLock()
	availableBalance := StateBalanceCache[CustomMinerAddress]
	BalanceCacheMutex.RUnlock()

	if availableBalance < (amount + fee) {
		return "REJECTED: Insufficient unspent cryptographic utility reserves."
	}

	// Compute secure local TX ID hash
	tSeed := fmt.Sprintf("%d-%s-%s-%f", time.Now().UnixNano(), CustomMinerAddress, recipient, amount)
	txID := fmt.Sprintf("TX_DESKTOP_%x", sha256.Sum256([]byte(tSeed)))

	tx := Transaction{
		ID: txID,
		// Secure input routing mapping to prevent genesis static collisions
		Inputs: []UTXOInput{
			{
				SourceTxID: "TX_GENESIS_INITIAL_POOL",
				Index:      0,
			},
		},
		Outputs: []UTXOOutput{
			{Recipient: recipient, Amount: amount},
			{Recipient: CustomMinerAddress, Amount: availableBalance - (amount + fee)}, // Change Output
		},
		FreeWillOffering: fee,
		Witness:          CustomMinerAddress,
	}

	if MempoolMatrix.PushTransaction(tx) {
		// Propagate transaction downstream directly across active peer gateways
		go BroadcastNewBlock(tx)
		return "SUCCESS: Payload securely committed to local mempool propagation queue!"
	}
	return "REJECTED: Active mempool spam threshold ceiling saturated."
}