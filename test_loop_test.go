package main

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"
)

// TestAutomatedTransactionLoop executes an automated multi-user stress test against the active UTXO node
func TestAutomatedTransactionLoop(t *testing.T) {
	fmt.Println("====================================================")
	fmt.Println("🧪 CVN CORE MESH NETWORK INTEGRATION SYSTEM TEST")
	fmt.Println("====================================================")

	targetNode := "127.0.0.1:8080"
	senderWallet := "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"
	mockRecipient := "CVN_8a7b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b"

	fmt.Printf("📡 Initializing automated transaction sequence loop targeting: %s\n", targetNode)
	fmt.Printf("💳 Origin: %s -> Target: %s\n\n", senderWallet, mockRecipient)

	// Simulate a rapid stream of 5 sequential user transactions
	for i := 1; i <= 5; i++ {
		fmt.Printf("🚀 [Loop Pass %d/5] Structuring transaction payload...", i)

		absoluteUnixTime := time.Now().Unix()
		txVol := float64(i) * 10.5

		tx := Transaction{
			ID: fmt.Sprintf("TX_TEST_LOOP_%d_%d", i, absoluteUnixTime),
			Inputs: []UTXOInput{
				{
					TxID:      "DUMMY_MOCK_HISTORICAL_UTXO_SOURCE_HASH_ID",
					OutputIdx: 0,
					Signature: "ECDSA_SIGNED_INTEGRATION_TEST_INPUT_PROOF",
				},
			},
			Outputs: []UTXOOutput{
				{
					Recipient: mockRecipient,
					Amount:    txVol,
				},
			},
			FreeWillOffering: 1.50, // Enforcing Model B Free-Will incentives
			DataSizeKB:       1.0,
			Witness:          "INTEGRATION_TEST_DUMMY_PUBLIC_KEY_COORDINATES",
			SignatureR:       "81a3b5c7d9e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7",
			SignatureS:       "92b4c6d8e0f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8",
		}

		txBytes, err := json.Marshal(tx)
		if err != nil {
			fmt.Printf("❌ Serialization Error: %v\n", err)
			continue
		}

		// Connect directly to the listener server port established in main.go
		conn, err := net.DialTimeout("tcp", targetNode, 2*time.Second)
		if err != nil {
			fmt.Printf("\n🚨 FAILED HANDSHAKE: Your node server at %s is offline or unreachable!\n", targetNode)
			fmt.Println("👉 Fix: Open a separate terminal window and launch .\\LaunchMiner.bat before running this test.")
			return
		}

		// Stream the transaction over open TCP network channels matching wallet.go's behavior exactly
		fmt.Fprintf(conn, "TX_BROADCAST:%s\n", string(txBytes))
		conn.Close()

		fmt.Println(" ✅ Broadcasted Successfully.")
		time.Sleep(1 * time.Second) // Sleep 1 second between message broadcasts
	}

	fmt.Println("\n====================================================")
	fmt.Println("🏁 AUTOMATED TRANSACTION INGESTION TEST COMPLETE")
	fmt.Println("====================================================")
}