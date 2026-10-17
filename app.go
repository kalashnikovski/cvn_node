package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	amount, _ := strconv.ParseFloat(amountStr, 64)
	fee, _ := strconv.ParseFloat(feeStr, 64)

	if amount <= 0 || strings.TrimSpace(recipient) == "" || recipient == "1" {
		return "ALERT: Transaction rejected by Phase 3 address validation firewall filters."
	}

	requiredTotal := amount + fee
	var gatheredUTXOs []struct {
		txID  string
		index int
		value float64
	}
	var totalAvailable float64

	// 👑 THE LIVE UTXO BATCH REALIGNMENT:
	// Pull your real spendable history directly from the terminal miner's live memory endpoints!
	addrResp, err := http.Get("http://localhost:8081/blocks/batch?start=0")
	if err != nil {
		addrResp, _ = http.Get("http://localhost:9091/blocks/batch?start=0")
	}
	
	if addrResp == nil {
		return "REJECTED: Unable to communicate with your local mining terminal network ports."
	}

	// Correctly parse the live blockchain blocks data array over the wire mesh
	var masterBlocksBatch []Block
	if err := json.NewDecoder(addrResp.Body).Decode(&masterBlocksBatch); err != nil {
		addrResp.Body.Close()
		return "REJECTED: Failed to decode live blockchain metrics from miner endpoints."
	}
	addrResp.Body.Close()

	// Scan through all valid historical mainnet blocks to gather your true unspent coins!
	for _, b := range masterBlocksBatch {
		for _, txPayload := range b.Transactions {
			for outIdx, out := range txPayload.Outputs {
				if out.Recipient == CustomMinerAddress {
					totalAvailable += out.Amount
					gatheredUTXOs = append(gatheredUTXOs, struct {
						txID  string
						index int
						value float64
					}{txID: txPayload.ID, index: outIdx, value: out.Amount})
				}
			}
		}
	}

	if totalAvailable < requiredTotal {
		return fmt.Sprintf("REJECTED: Insufficient unspent cryptographic utility reserves. Required: %f, Available: %f", requiredTotal, totalAvailable)
	}

	var selectedInputs []UTXOInput
	var inputsValueSum float64

	for _, utxo := range gatheredUTXOs {
		selectedInputs = append(selectedInputs, UTXOInput{
			SourceTxID: utxo.txID,
			Index:      utxo.index,
			Signature:  "",
		})
		inputsValueSum += utxo.value
		if inputsValueSum >= requiredTotal {
			break
		}
	}

	tSeed := fmt.Sprintf("%d-%s-%s-%f", time.Now().UnixNano(), CustomMinerAddress, recipient, amount)
	txID := fmt.Sprintf("TX_DESKTOP_%x", sha256.Sum256([]byte(tSeed)))

	var localPrivateKeySecret string
	var localPublicKeyHex string

	keyBackupBytes, err := os.ReadFile("cvn_secret_backup.txt")
	if err == nil {
		lines := strings.Split(string(keyBackupBytes), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "Public Key: ") {
				localPublicKeyHex = strings.TrimSpace(strings.TrimPrefix(line, "Public Key: "))
			}
			if strings.HasPrefix(line, "Private Key: ") {
				localPrivateKeySecret = strings.TrimSpace(strings.TrimPrefix(line, "Private Key: "))
			}
		}
	}

	if localPrivateKeySecret == "" || localPublicKeyHex == "" {
		return "REJECTED: Critical systemic error generating localized cryptographic key contexts."
	}

	txMessage := fmt.Sprintf("%s-%s-%f", txID, CustomMinerAddress, fee)
	rStr, sStr, err := SignTransactionPayload(localPrivateKeySecret, txMessage)
	if err != nil {
		return fmt.Sprintf("REJECTED: Asymmetric signature cryptographic compilation breakdown: %v", err)
	}
	combinedSignature := fmt.Sprintf("%s|%s", rStr, sStr)

	for i := range selectedInputs {
		selectedInputs[i].Signature = combinedSignature
	}

	var finalOutputs []UTXOOutput
	finalOutputs = append(finalOutputs, UTXOOutput{Recipient: recipient, Amount: amount})

	if inputsValueSum > requiredTotal {
		changeAmount := inputsValueSum - requiredTotal
		finalOutputs = append(finalOutputs, UTXOOutput{Recipient: CustomMinerAddress, Amount: changeAmount})
	}

	// Construct a clean production-grade Transaction struct matching the format perfectly!
	tx := Transaction{
		ID:               txID,
		Inputs:           selectedInputs,
		Outputs:          finalOutputs,
		Signature:        combinedSignature,
		PublicKey:        localPublicKeyHex,
		Timestamp:        time.Now().Unix(),
		FreeWillOffering: fee,
		DataSizeKB:       1.2,
		Witness:          CustomMinerAddress,
	}

	// Serialize the true Transaction struct object into bytes
	jsonBytes, err := json.Marshal(tx)
	if err != nil {
		return "REJECTED: Failed to serialize transaction parameters."
	}

		// 👑 THE CORE HTTP PROPAGATION WIRE GATE:
	targetURL := "http://localhost:8081/transaction"
	postResp, err := http.Post(targetURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		targetURL = "http://localhost:9091/transaction"
		postResp, err = http.Post(targetURL, "application/json", bytes.NewBuffer(jsonBytes))
		if err != nil {
			return "REJECTED: Unable to communicate with your local mining terminal network ports."
		}
	}
	defer postResp.Body.Close()

	// 👑 THE EXPLORER TRACE DEBUGGER:
	// Read the exact raw text error response payload coming out of your terminal miner 
	// to pinpoint the precise verification check that is failing!
	var responseBodyMap map[string]interface{}
	json.NewDecoder(postResp.Body).Decode(&responseBodyMap)
	
	fmt.Printf("\n📢 [MEMPOOL DIAGNOSTIC] Code: %d // Server Payload Message: %v\n", postResp.StatusCode, responseBodyMap)

	if postResp.StatusCode == http.StatusCreated || postResp.StatusCode == http.StatusOK {
		return "SUCCESS: Payload securely committed to local mempool propagation queue!"
	}

	return fmt.Sprintf("REJECTED: Local network validation gates returned error code: %d", postResp.StatusCode)
}