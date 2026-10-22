package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.etcd.io/bbolt"
)

// VerifyTransactionIntegrity evaluates cryptographic signatures and checks live UTXO states on the disk ledger.
func VerifyTransactionIntegrity(tx Transaction, blockIndex int, txIndex int, txBucket *bbolt.Bucket) bool {
	// 👑 THE BULLETPROOF COINBASE SIGNATURE BYPASS:
	// Inspect the block transaction array identity prefix at the absolute front gate.
	// This guarantees that valid block reward payouts bypass empty inputs and floating-point 
	// signature constraints uniformly across both Windows and Linux network validations!
	if strings.HasPrefix(tx.ID, "TX_COINBASE") {
		if txIndex != 0 {
			fmt.Println("🚨 CRITICAL BLOCK FRAUD: Malicious Coinbase injection detected outside index 0!")
			return false
		}
		return true
	}
	
	if len(tx.Inputs) == 0 {
		fmt.Println("🚨 TRANSACTION EXPLOIT REJECTED: Input-less transactions require valid Coinbase.")
		return false
	}

	// 2. CRYPTOGRAPHIC SIGNATURE PASS: Validates ECDSA key-pair coordinates over message payload strings
	txMessage := fmt.Sprintf("%s-%s-%.8f", tx.ID, tx.Witness, tx.FreeWillOffering)
	sigSegments := strings.Split(tx.Signature, "|")
	if len(sigSegments) < 2 {
		fmt.Println("🚨 SIGNATURE FAULT: Transaction formatting dropped or corrupted segment blocks.")
		return false
	}
	
	rStr := sigSegments[0]
	sStr := sigSegments[1]

	if !VerifyTransactionSignature(tx.PublicKey, txMessage, rStr, sStr) {
		fmt.Printf("🚨 CRYPTOGRAPHIC REJECTION: Invalid signature parameters for transaction ID: %s\n", tx.ID)
		return false
	}

	// 3. UTXO DISK INTEGRITY VERIFICATION: Ensures parental references exist and remain unspent
	var totalInputVolume float64 = 0.0
	for _, in := range tx.Inputs {
		compoundKey := fmt.Sprintf("%s_%d", in.SourceTxID, in.Index)
		utxoBytes := txBucket.Get([]byte(compoundKey))
		
		if utxoBytes == nil {
			fmt.Printf("🚨 UTXO SPEND FAILURE: Reference key '%s' missing or spent on disk ledger tracking partitions.\n", compoundKey)
			return false
		}

		var unspentRecord UTXORecord
		if err := json.Unmarshal(utxoBytes, &unspentRecord); err != nil {
			fmt.Println("🚨 DATA FAULT: Unable to parse serialized UTXO bytes.")
			return false
		}
		totalInputVolume += unspentRecord.Amount
	}

	var totalOutputVolume float64 = 0.0
	for _, out := range tx.Outputs {
		if out.Amount <= 0 {
			fmt.Println("🚨 VALUE FAULT: Output values must exceed 0 token limits.")
			return false
		}
		totalOutputVolume += out.Amount
	}

	// 4. BALANCING EQUILIBRIUM RULES: Enforces total inputs can cover outputs and network offering fee metrics
	if totalInputVolume < (totalOutputVolume + tx.FreeWillOffering) {
		fmt.Printf("🚨 ACCRUAL FAILURE: Inputs (%f) insufficient for required outputs + fee offering total (%f)\n", totalInputVolume, totalOutputVolume+tx.FreeWillOffering)
		return false
	}

	return true
}

// VerifyIncomingWireTransaction intercepts unconfirmed incoming data packets on Port 8080.
func VerifyIncomingWireTransaction(tx Transaction) bool {
	if GlobalBoltEngine == nil {
		return false
	}

	isValid := false
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		bUTXO := dbTx.Bucket([]byte("utxo"))
		if bUTXO == nil {
			return nil
		}
		if VerifyTransactionIntegrity(tx, -1, 1, bUTXO) {
			isValid = true
		}
		return nil
	})

	return isValid
}

// VerifyIncomingWireBlock performs deep validation checks on incoming blocks with strict difficulty parameters.
func VerifyIncomingWireBlock(b Block) bool {
	// 👑 THE MASTER IDENTITY SAFEGUARD:
	// Prevent headless cloud servers from running consensus rules if their local address
	// profile tracking parameters fell back to unconfigured system string literals.
	if CustomMinerAddress == "" || CustomMinerAddress == "CVN_UNCONFIGURED_LOCAL_NODE_ID" {
		fmt.Println("🚨 FRONT-GATE REFUSAL: Headless node configuration missing or uninstantiated. Aborting validation path.")
		return false
	}

	targetPrefix := strings.Repeat("0", b.Difficulty)
	if !strings.HasPrefix(b.Hash, targetPrefix) {
		fmt.Printf("🚨 WIRE BLOCK FAULT: Hash signature %s missing difficulty constraints.\n", b.Hash)
		return false
	}

	// 👑 THE CRYPTOGRAPHIC IDENTITY INVARIANT:
	// Recalculate block hashes using the exact transaction array elements embedded inside
	// the packet, preventing local wallet variable drift from corrupting historical validations.
	if b.Hash != CalculateHash(b) {
		fmt.Println("🚨 WIRE BLOCK FAULT: Block hash re-computation checks failed mismatch constraints.")
		return false
	}

	StateMutex.RLock()
	expectedDifficulty := CalculateNextDifficulty(GetLatestBlockInternal(), b.Timestamp)
	StateMutex.RUnlock()

	effectiveThreshold := expectedDifficulty - 1
	if effectiveThreshold < 4 {
		effectiveThreshold = 4 // Hard lock securely onto your baseline floor of 4 zeros
	}

	if b.Difficulty < effectiveThreshold {
		fmt.Printf("🚨 REJECTION: Incoming block #%d uses insufficient difficulty %d (Expected: %d)\n", b.Index, b.Difficulty, expectedDifficulty)
		return false
	}

	if GlobalBoltEngine == nil {
		return false
	}

	isBlockValid := true
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		bUTXO := dbTx.Bucket([]byte("utxo"))
		if bUTXO == nil {
			isBlockValid = false
			return nil
		}

		for txIdx, tx := range b.Transactions {
			if txIdx == 0 {
				continue
			}
			if !VerifyTransactionIntegrity(tx, int(b.Index), txIdx, bUTXO) {
				isBlockValid = false
				return nil
			}
		}
		return nil
	})

	return isBlockValid
}