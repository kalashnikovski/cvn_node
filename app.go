package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
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
	amount, err := strconv.ParseFloat(strings.TrimSpace(amountStr), 64)
	if err != nil || amount <= 0 {
		return "REJECTED: Enter an amount greater than zero."
	}
	fee := 0.0
	if strings.TrimSpace(feeStr) != "" {
		fee, err = strconv.ParseFloat(strings.TrimSpace(feeStr), 64)
		if err != nil || fee < 0 {
			return "REJECTED: The fee must be zero or a positive number."
		}
	}
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return "REJECTED: Enter a recipient address."
	}

	var privateKeyHex, publicKeyHex string
	if keyBackupBytes, readErr := os.ReadFile("cvn_secret_backup.txt"); readErr == nil {
		for _, line := range strings.Split(string(keyBackupBytes), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Public Key:") {
				publicKeyHex = strings.TrimSpace(strings.TrimPrefix(line, "Public Key:"))
			}
			if strings.HasPrefix(line, "Private Key:") {
				privateKeyHex = strings.TrimSpace(strings.TrimPrefix(line, "Private Key:"))
			}
		}
	}
	if privateKeyHex == "" || publicKeyHex == "" {
		return "REJECTED: Could not read this wallet's keys from cvn_secret_backup.txt."
	}

	// The transfer is built and signed here, on this machine. The node only
	// ever receives the finished, signed transfer.
	tx, err := SendTransferFromWallet(privateKeyHex, publicKeyHex, recipient, amount, fee)
	if err != nil {
		return "REJECTED: " + err.Error()
	}
	return "SUCCESS: Transfer " + tx.ID[:19] + "... sent. It will confirm in one of the next blocks."
}
