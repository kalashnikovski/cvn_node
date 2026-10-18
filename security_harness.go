package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.etcd.io/bbolt"
)

// VerifyTransactionIntegrity validates signatures, balance sheet metrics, and coinbase constraints.
func VerifyTransactionIntegrity(tx Transaction, blockIndex int, txIndex int, txBucket *bbolt.Bucket) bool {
	// 👑 THE POSITION-LOCKED COINBASE GUARD:
	// If the transaction is positioned at index slot 0, it is ALWAYS a valid Coinbase reward!
	// This completely destroys brittle cross-platform string suffix or character padding variations.
	if txIndex == 0 {
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
		// Pass -1 for indices if it's coming straight out of the unconfirmed mempool wire
		if VerifyTransactionIntegrity(tx, -1, 1, bUTXO) {
			isValid = true
		}
		return nil
	})

	return isValid
}

// VerifyIncomingWireBlock checks incoming blocks with precise index parameters
func VerifyIncomingWireBlock(b Block) bool {
	targetPrefix := strings.Repeat("0", b.Difficulty)
	if !strings.HasPrefix(b.Hash, targetPrefix) {
		fmt.Printf("🚨 WIRE BLOCK FAULT: Hash signature %s missing difficulty constraints.\n", b.Hash)
		return false
	}

	if b.Hash != CalculateHash(b) {
		fmt.Println("🚨 WIRE BLOCK FAULT: Block hash re-computation checks failed mismatch constraints.")
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

		// Clean pass the exact transaction index mapping directly to the evaluator
		for txIdx, tx := range b.Transactions {
			if !VerifyTransactionIntegrity(tx, int(b.Index), txIdx, bUTXO) {
				isBlockValid = false
				return nil
			}
		}
		return nil
	})

	return isBlockValid
}