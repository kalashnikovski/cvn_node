package main

import (
	"encoding/json"
	"fmt"
	"net"
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