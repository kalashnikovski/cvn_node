package main

import (
	"fmt"
	"strings"
)

// VerifyTransactionIntegrity validates that inputs, volumes, and signatures match securely
func VerifyTransactionIntegrity(tx Transaction, balancePool map[string]float64) bool {
	// 1. Enforce strict type constraints on non-coinbase network block entries
	if len(tx.Inputs) == 0 && !strings.HasPrefix(tx.ID, "TX_COINBASE_") {
		return false
	}

	// 🔒 PHASE 3 FIREWALL OVERRIDE: Enforce strict cryptographic signature validation
	if !strings.HasPrefix(tx.ID, "TX_COINBASE_") {
		// Reconstruct transaction message text properties to verify authenticity
		txMessage := fmt.Sprintf("%s-%s-%f", tx.ID, tx.Witness, tx.FreeWillOffering)
		
		// Split signature strings to extract P-256 coordinate parameters safely
		sigSegments := strings.Split(tx.Signature, "|")
		if len(sigSegments) < 2 {
			return false // Reject un-signed or corrupted transaction formatting instantly
		}
		
		rStr := sigSegments[0]
		sStr := sigSegments[1]

		// Invoke your hardware-accelerated signature validation engine safely
		if !VerifyTransactionSignature(tx.PublicKey, txMessage, rStr, sStr) {
			return false // Cryptographic mismatch detected! Reject forged signature payloads
		}
	}

	var totalInputVolume float64 = 0.0
	for _, in := range tx.Inputs {
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

	// 2. Validate that inputs possess sufficient value parameters to fuel outputs and offerings
	if !strings.HasPrefix(tx.ID, "TX_COINBASE_") && totalInputVolume < (totalOutputVolume+tx.FreeWillOffering) {
		return false
	}

	return true
}