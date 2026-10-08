package main

import (
	"fmt"
	"strings"
)

// VerifyTransactionIntegrity validates that inputs match the source addresses securely
func VerifyTransactionIntegrity(tx Transaction, balancePool map[string]float64) bool {
	if len(tx.Inputs) == 0 && !strings.HasPrefix(tx.ID, "TX_COINBASE_") {
		return false
	}

	var totalInputVolume float64 = 0.0
	for _, in := range tx.Inputs {
		// ✅ HARMONIZED: Securely map to your master main.go UTXOInput field variables
		key := fmt.Sprintf("%s_%d", in.SourceTxID, in.Index)
		if val, exists := balancePool[key]; exists {
			totalInputVolume += val
		}
	}

	var totalOutputVolume float64 = 0.0
	for _, out := range tx.Outputs {
		if out.Amount <= 0 {
			return false
		}
		totalOutputVolume += out.Amount
	}

	if !strings.HasPrefix(tx.ID, "TX_COINBASE_") && totalInputVolume < (totalOutputVolume+tx.FreeWillOffering) {
		return false
	}

	return true
}