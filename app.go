package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// GetLocalNodeTelemetry fetches telemetry straight from your active mining terminal's local API ports
func (a *App) GetLocalNodeTelemetry() string {
	// 👑 THE CORE HTTP LINK UPGRADE:
	// Bypass sandbox disk files completely and query your active miner's local port!
	resp, err := http.Get("http://localhost:8081/block")
	if err != nil {
		// Fallback to the port-shifted channel if applicable
		resp, err = http.Get("http://localhost:9091/block")
		if err != nil {
			// Fail-safe graceful offline state placeholder if the miner is initializing
			telemetry := map[string]interface{}{
				"current_height": 0,
				"block_hash":     "0000000000000000000000000000000000000000000000000000000000000000",
				"miner_address":  "CVN_NODE_CONNECTING...",
				"difficulty":     4,
				"is_synced":      false,
			}
			data, _ := json.Marshal(telemetry)
			return string(data)
		}
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "{}"
	}

	// Capture network variables from our terminal miner's live response payload
	heightVal, _ := data["current_height"].(float64)
	hashVal, _ := data["block_hash"].(string)

	telemetry := map[string]interface{}{
		"current_height": int64(heightVal),
		"block_hash":     hashVal,
		"miner_address":  CustomMinerAddress,
		"difficulty":     4,
		"is_synced":      heightVal > 0,
	}

	finalBytes, _ := json.Marshal(telemetry)
	return string(finalBytes)
}

// FetchWalletBalanceQuery pulls verified account metrics directly out of the terminal miner's active ledger cache
func (a *App) FetchWalletBalanceQuery(address string) float64 {
	// Query the local live /audit endpoint inside your miner's active network server
	resp, err := http.Get(fmt.Sprintf("http://localhost:8081/audit?address=%s", address))
	if err != nil {
		resp, err = http.Get(fmt.Sprintf("http://localhost:9091/audit?address=%s", address))
		if err != nil {
			return 0.0
		}
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0.0
	}

	if balanceVal, exists := data["balance"]; exists {
		return balanceVal.(float64)
	}
	return 0.0
}

// BroadcastLocalTransactionSubmit injects payloads straight over local HTTP ports into your miner's active wire validation lanes
func (a *App) BroadcastLocalTransactionSubmit(recipient, amountStr, feeStr string) string {
	// Create a lightweight temporary data structural packet to bridge front-end fields to your local network server
	payload := map[string]string{
		"recipient":  recipient,
		"amount":     amountStr,
		"fee":        feeStr,
		"timestamp":  fmt.Sprintf("%d", time.Now().Unix()),
	}
	
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return "REJECTED: Failed to serialize transaction parameters."
	}

	// Post the user's transaction choice directly to your running terminal miner's API route
	targetURL := "http://localhost:8081/transaction"
	resp, err := http.Post(targetURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		targetURL = "http://localhost:9091/transaction"
		resp, err = http.Post(targetURL, "application/json", bytes.NewBuffer(jsonBytes))
		if err != nil {
			return "REJECTED: Unable to communicate with your local mining terminal network ports."
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		return "SUCCESS: Transaction payload securely routed straight to your running miner's active mempool!"
	}

	return fmt.Sprintf("REJECTED: Local network validation gates returned error code: %d", resp.StatusCode)
}