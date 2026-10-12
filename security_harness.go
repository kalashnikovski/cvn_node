package main

import (
	"fmt"
	"strings"
)

// VerifyTransactionIntegrity evaluates cryptographic signatures, inputs, and coinbase rules
func VerifyTransactionIntegrity(tx Transaction, blockIndex int, txIndex int, balancePool map[string]float64) bool {
	// 1. HARDENED COINBASE CHECK: Protect against malicious counterfeit token inflation
	if strings.HasPrefix(tx.ID, "TX_COINBASE_") {
		// A coinbase transaction is strictly ONLY valid if it is the very first transaction in the block!
		if txIndex != 0 {
			fmt.Println("🚨 CRITICAL BLOCK FRAUD: Malicious Coinbase injection detected outside index 0!")
			return false
		}
		return true
	}
	
	if len(tx.Inputs) == 0 {
		fmt.Println("🚨 TRANSACTION EXPLOIT REJECTED: Input-less transactions are forbidden outside Coinbase.")
		return false // Reject any other transaction attempting to mint input-less tokens
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

		// Invoke your signature validation engine safely
		if !VerifyTransactionSignature(tx.PublicKey, txMessage, rStr, sStr) {
			return false // Cryptographic mismatch detected! Reject forged signature payloads
		}
	}

	// 📊 BALANCE SHEET ACCRUAL ACCOUNTING CHECKS
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
		fmt.Printf("🚨 BALANCE FAILURE: Insufficient balance pool inputs (%f) for required output and offering total (%f)\n", totalInputVolume, totalOutputVolume+tx.FreeWillOffering)
		return false
	}

	return true
}