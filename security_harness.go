package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.etcd.io/bbolt"
)

// VerifyTransactionIntegrity evaluates cryptographic signatures and checks live UTXO states on the disk ledger.
func VerifyTransactionIntegrity(tx Transaction, blockIndex int, txIndex int, txBucket *bbolt.Bucket) bool {
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

	if totalInputVolume < (totalOutputVolume + tx.FreeWillOffering) {
		fmt.Printf("🚨 BALANCE FAULT: Inputs (%f) insufficient for outputs (%f) + fee (%f)\n", totalInputVolume, totalOutputVolume, tx.FreeWillOffering)
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

// VerifyIncomingWireBlock performs strict validation audits on incoming P2P blocks.
func VerifyIncomingWireBlock(b Block) bool {
	if CustomMinerAddress == "" || CustomMinerAddress == "CVN_UNCONFIGURED_LOCAL_NODE_ID" {
		fmt.Println("🚨 FRONT-GATE REFUSAL: Headless node configuration missing or uninstantiated. Aborting validation path.")
		return false
	}

	// Unified difficulty rule: height < 10 → exactly 2; height >= 10 → at least 4.
	// Do not derive the bootstrap floor from the local tip.
	var expectedDifficulty int
	if b.Index < 10 {
		expectedDifficulty = 2
	} else {
		if b.Difficulty < 4 {
			fmt.Printf("🚨 REJECTION: Incoming block #%d uses invalid difficulty %d below mainnet floor (4)\n", b.Index, b.Difficulty)
			return false
		}
		expectedDifficulty = b.Difficulty
	}

	if b.Difficulty != expectedDifficulty {
		fmt.Printf("🚨 REJECTION: Incoming block #%d uses invalid difficulty %d (Expected exactly: %d)\n", b.Index, b.Difficulty, expectedDifficulty)
		return false
	}

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
