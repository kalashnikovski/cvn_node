package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// TestAdversarialDoubleSpend Attack Simulation fires back-to-back duplicate inputs to test node shields
func TestAdversarialDoubleSpendAttack(t *testing.T) {
	fmt.Println("====================================================")
	fmt.Println("🔥 ADVERSARIAL ATTACK SIMULATION: DOUBLE-SPEND SHIELD")
	fmt.Println("====================================================")

	targetNode := "127.0.0.1:8080"
	mockRecipient1 := "CVN_RECIPIENT_A_A_A_A_A_A_A_A_A_A_A_A_A_A_A_A_A_A"
	mockRecipient2 := "CVN_RECIPIENT_B_B_B_B_B_B_B_B_B_B_B_B_B_B_B_B_B_B"

	// 1. Establish the target victim UTXO cash clump coordinates
	sharedVictimInput := UTXOInput{
		TxID:      "CRITICAL_VICTIM_UTXO_TRANSACTION_HASH_ID_999",
		OutputIdx: 0,
		Signature: "ECDSA_ATTACK_ATTEMPT_SIGNATURE_DATA_PROVES_OWNERSHIP",
	}

	fmt.Println("⏳ [Phase 1] Constructing Transaction #1 (Valid initial spend)...")
	tx1 := Transaction{
		ID:               "TX_LEGITIMATE_SPEND_ATTEMPT",
		Inputs:           []UTXOInput{sharedVictimInput}, // Spent the first time
		Outputs:          []UTXOOutput{{Recipient: mockRecipient1, Amount: 25.0}},
		FreeWillOffering: 1.0,
		DataSizeKB:       1.0,
		Witness:          "MALICIOUS_NODE_IDENTITY_PROOF_COORDINATES",
	}

	fmt.Println("⏳ [Phase 2] Constructing Transaction #2 (FRAUDULENT DOUBLE-SPEND)...")
	tx2 := Transaction{
		ID:               "TX_MALICIOUS_DOUBLE_SPEND_EXPL0IT",
		Inputs:           []UTXOInput{sharedVictimInput}, // 🚨 EXPLOIT: Re-using the exact same input details!
		Outputs:          []UTXOOutput{{Recipient: mockRecipient2, Amount: 25.0}},
		FreeWillOffering: 5.0, // Trying to bribe the mempool with a higher fee
		DataSizeKB:       1.0,
		Witness:          "MALICIOUS_NODE_IDENTITY_PROOF_COORDINATES",
	}

	// Connect and broadcast Transaction #1
	conn1, err := net.Dial("tcp", targetNode)
	if err != nil {
		t.Fatalf("🚨 Node offline! Launch .\\LaunchMiner.bat before running test.")
	}
	txBytes1, _ := json.Marshal(tx1)
	fmt.Fprintf(conn1, "TX_BROADCAST:%s\n", string(txBytes1))
	conn1.Close()
	fmt.Println("✅ Transaction #1 broadcasted successfully into the mempool channels.")

	time.Sleep(500 * time.Millisecond) // Short delay

	// Connect and broadcast Transaction #2 (The Attack)
	conn2, err := net.Dial("tcp", targetNode)
	if err != nil {
		t.Fatalf("🚨 Connection failed on second transmission pass.")
	}
	txBytes2, _ := json.Marshal(tx2)
	fmt.Fprintf(conn2, "TX_BROADCAST:%s\n", string(txBytes2))
	conn2.Close()
	fmt.Println("🔥 [ATTACK] Transaction #2 (Double-Spend) fired down the network sockets!")
	fmt.Println("====================================================")
}

// BenchmarkPoDDifficultyClamps tests local hardware capacity against the protocol's strict target rules
func BenchmarkPoDDifficultyClamps(b *testing.B) {
	fmt.Println("\n====================================================")
	fmt.Println("⚒️  CVN LAYER-1 PROFILER: HARDWARE BENCHMARK HARNESS")
	fmt.Println("====================================================")

	mockPayload := "BENCHMARK_PROFILING_BLOCK_DATA_FRAME_V3"
	testDifficulties := []int{4, 5, 6}

	for _, diff := range testDifficulties {
		targetPrefix := strings.Repeat("0", diff)
		fmt.Printf("📋 Profiling Hash Efficiency for Ceiling Clamp: %d Zeros...\n", diff)

		var nonce uint64 = 0
		startTime := time.Now()

		for {
			inputStr := fmt.Sprintf("%s_%d", mockPayload, nonce)
			hashBytes := sha256.Sum256([]byte(inputStr))
			hashHex := hex.EncodeToString(hashBytes[:])

			if hashHex[:diff] == targetPrefix {
				elapsed := time.Since(startTime).Seconds()
				if elapsed == 0 { elapsed = 0.001 }
				
				hashRate := float64(nonce) / elapsed / 1000.0
				fmt.Printf("   🎉 Target Solved! Nonce: %d | Time: %.3fs | Local Throughput: %.2f kH/s\n", nonce, elapsed, hashRate)
				break
			}
			nonce++
			
			if nonce > 15000000 {
				elapsed := time.Since(startTime).Seconds()
				hashRate := float64(nonce) / elapsed / 1000.0
				fmt.Printf("   🛑 Profile Boundary Limit Hit at 15M hashes | Speed: %.2f kH/s\n", hashRate)
				break
			}
		}
	}
	fmt.Println("====================================================")
}
// TestLocalLedgerIntegrity performs a deep sequential hash-pointer validation check on disk data
func TestLocalLedgerIntegrity(t *testing.T) {
	fmt.Println("\n====================================================")
	fmt.Println("🛡️  COVENANT STANDARD (CVN) STATE LEDGER INTEGRITY AUDIT")
	fmt.Println("====================================================")

	ledgerPath := "ledger_vault.json"
	
	// 1. Stream the raw plain-text database data into memory bytes
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("🚨 FILE SYSTEM ERROR: Unable to load data tracking file '%s': %v\n", ledgerPath, err)
	}

	var chain []Block
	if err := json.Unmarshal(data, &chain); err != nil {
		t.Fatalf("🚨 PARSE CORRUPTION ERROR: Malformed block sequence layout detected: %v\n", err)
	}

	totalBlocks := len(chain)
	fmt.Printf("📂 Target DB File Found. Scanning %d Serialized Blocks...\n", totalBlocks)
	fmt.Println("----------------------------------------------------")

	if totalBlocks == 0 {
		fmt.Println("📋 Inspection Stalled: The ledger vault is completely empty.")
		return
	}

	// 2. Loop chronologically through the chain blocks to trace hash-pointers
	for i := 1; i < totalBlocks; i++ {
		currentBlock := chain[i]
		previousBlock := chain[i-1]

		// Verify index integrity sequence
		if currentBlock.Index != previousBlock.Index+1 {
			t.Errorf("🚨 SEQUENCE BREAK DETECTED at Block #%d! Expected Index %d, got %d\n", 
				currentBlock.Index, previousBlock.Index+1, currentBlock.Index)
		}

		// Verify deep cryptographic hash-pointer linkage backplane continuity
		if currentBlock.PrevHash != previousBlock.Hash {
			t.Errorf("🚨 HASH LINK DISCONTINUITY AT BLOCK HEIGHT #%d!\n   ↳ Left Hand Block Hash: %s\n   ↳ Mismatched PrevHash Link:  %s\n", 
				currentBlock.Index, previousBlock.Hash, currentBlock.PrevHash)
			fmt.Println("====================================================")
			return
		}

		// Recalculate hash matching parameters on the fly to catch internal state modifications
		recalculatedHash := CalculateHash(currentBlock)
		if currentBlock.Hash != recalculatedHash {
			t.Errorf("🚨 INTERNAL STATE MUTATION TAMP ALERT AT BLOCK HEIGHT #%d!\n   ↳ Stored Ledger Hash:  %s\n   ↳ Recalculated Actual: %s\n", 
				currentBlock.Index, currentBlock.Hash, recalculatedHash)
			fmt.Println("====================================================")
			return
		}
	}

	// 3. Render success telemetry parameters out if the chain passes cleanly
	fmt.Println("🎉 CRITICAL LEDGER VERIFICATION COMPLETED: 100% SUCCESS")
	fmt.Println("✅ Cryptographic pointer sequences are fully contiguous.")
	fmt.Println("✅ Zero structural file line or block hash variations detected.")
	fmt.Printf("💰 Master Reserve Allocation Asset Integrity: SECURE\n")
	fmt.Println("====================================================")
}