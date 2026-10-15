package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os" // <--- ADD THIS LINE HERE
	"strconv"
	"strings"
	"time"

	"go.etcd.io/bbolt" // Make sure this line is exactly here
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

	requiredTotal := amount + fee
	var gatheredUTXOs []struct {
		txID  string
		index int
		value float64
	}
	var totalAvailable float64

	err := GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("utxo"))
		if b == nil {
			return fmt.Errorf("utxo bucket partition missing from engine disk")
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var record UTXORecord
			if err := json.Unmarshal(v, &record); err != nil {
				continue
			}

			if record.Recipient == CustomMinerAddress {
				keyStr := string(k)
				parts := strings.Split(keyStr, "_")
				if len(parts) < 2 {
					continue
				}
				parsedIdx, err := strconv.Atoi(parts[len(parts)-1])
				if err != nil {
					continue
				}
				sourceTxID := strings.Join(parts[:len(parts)-1], "_")

				totalAvailable += record.Amount
				gatheredUTXOs = append(gatheredUTXOs, struct {
					txID  string
					index int
					value float64
				}{txID: sourceTxID, index: parsedIdx, value: record.Amount})
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Sprintf("REJECTED: Internal storage engine error scanning inputs: %v", err)
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

	// Dynamic Cryptographic Key Acquisition Phase
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

	// Fallback to fresh local pair generation if the local configuration matrix is pristine but unwritten
	if localPrivateKeySecret == "" || localPublicKeyHex == "" {
		var genErr error
		localPrivateKeySecret, localPublicKeyHex, _, genErr = GenerateKeyPair()
		if genErr != nil {
			return "REJECTED: Critical systemic error generating localized cryptographic key contexts."
		}
	}

	// Enforce uniform preimage template formatting matching security_harness.go rules exactly
	txMessage := fmt.Sprintf("%s-%s-%f", txID, CustomMinerAddress, fee)

	rStr, sStr, err := SignTransactionPayload(localPrivateKeySecret, txMessage)
	if err != nil {
		return fmt.Sprintf("REJECTED: Asymmetric signature cryptographic compilation breakdown: %v", err)
	}
	combinedSignature := fmt.Sprintf("%s|%s", rStr, sStr)

	// Explicit structural distribution down onto individual transaction input objects
	for i := range selectedInputs {
		selectedInputs[i].Signature = combinedSignature
	}

	var finalOutputs []UTXOOutput
	finalOutputs = append(finalOutputs, UTXOOutput{Recipient: recipient, Amount: amount})

	if inputsValueSum > requiredTotal {
		changeAmount := inputsValueSum - requiredTotal
		finalOutputs = append(finalOutputs, UTXOOutput{Recipient: CustomMinerAddress, Amount: changeAmount})
	}

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

	if MempoolMatrix.PushTransaction(tx) {
		go BroadcastTransactionToMesh(tx)
		return "SUCCESS: Payload securely committed to local mempool propagation queue!"
	}
	return "REJECTED: Active mempool spam threshold ceiling saturated."
}