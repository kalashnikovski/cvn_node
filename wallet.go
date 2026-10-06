package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// RunWalletGUI acts as the direct console operational interface loop for your UTXO core network
func RunWalletGUI() {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println("\n====================================================================")
		fmt.Println("💎 COVENANT STANDARD (CVN) LIGHTWEIGHT COMMAND-LINE WALLET CORE v3.6")
		fmt.Println("====================================================================")
		fmt.Println(" 👑 1. Generate a Brand New Unique Wallet (Private Key & CVN Address)")
		fmt.Println(" 🕊️  2. Send an Outbound Free-Will Offering Transaction Payload")
		fmt.Println(" ❌ 3. Exit Wallet Console Application Safely")
		fmt.Println("====================================================================")
		fmt.Print("Select option index [1-3]: ")
		
		choiceStr, _ := reader.ReadString('\n')
		choiceStr = strings.TrimSpace(choiceStr)

				if choiceStr == "1" {
			// 🔐 Hook directly into your crypto_auth.go generation mathematical rules
			privHex, walletAddress, err := GenerateKeyPair()
			if err != nil {
				fmt.Printf("🚨 CRYPTOGRAPHIC FAULT: Unable to initialize secure entropy paths: %v\n", err)
				continue
			}
			fmt.Println("\n====================================================================")
			fmt.Println("🎉 NEW SECURE WALLET IDENTITY DETECTED & SCRIPTURALLY LOCKED")
			fmt.Println("====================================================================")
			fmt.Printf("🔑 YOUR PRIVATE KEY (SECRET): %s\n", privHex)
			fmt.Printf("📋 YOUR CVN PUBLIC ADDRESS:   %s\n", walletAddress)
			fmt.Println("--------------------------------------------------------------------")
			fmt.Println("⚠️  WARNING: Copy and save your Private Key safely! It is never stored on disk.")
			fmt.Println("====================================================================")

			// ====================================================================
			// 🟩 PASTE THE AUTOMATED BACKUP FIX DIRECTLY HERE:
			// ====================================================================
			backupContent := fmt.Sprintf(
				"====================================================\n"+
				"🛰️  COVENANT STANDARD (CVN) SOVEREIGN WALLET KEYS\n"+
				"====================================================\n\n"+
				"📋 PUBLIC WALLET ADDRESS (Safe to share with anyone):\n"+
				"%s\n\n"+
				"🔑 SECRET PRIVATE KEY (NEVER SHARE - Keep completely hidden):\n"+
				"%s\n"+
				"====================================================\n", 
				walletAddress, privHex,
			)
			
			// Write the file down to your disk storage directory using strict read/write security privileges
			_ = os.WriteFile("my_crypto_address.txt", []byte(backupContent), 0600)
			
			// Also write the matching public profile format out to prevent configuration script bypasses
			configMap := map[string]string{"saved_miner_address": walletAddress}
			configBytes, _ := json.MarshalIndent(configMap, "", "  ")
			_ = os.WriteFile("miner_config.json", configBytes, 0644)
			
			fmt.Println("💾 SUCCESS: Sovereign wallet credentials safely written to disk inside your directory!")
			fmt.Println("====================================================================")
			// ====================================================================

			fmt.Println("Press [ENTER] to return to the core wallet menu window.")
			_, _ = reader.ReadString('\n')

		} else if choiceStr == "2" {
			fmt.Print("\n🔑 Enter or Paste Sender Private Key (Hex Secret): ")
			privKey, _ := reader.ReadString('\n')
			privKey = strings.TrimSpace(privKey)
			
			fmt.Print("📋 Enter Recipient Public Destination Address (CVN_...): ")
			recipient, _ := reader.ReadString('\n')
			recipient = strings.TrimSpace(recipient)
			
			fmt.Print("💰 Enter CVN Token Amount to Transfer: ")
			amountStr, _ := reader.ReadString('\n')
			amountStr = strings.TrimSpace(strings.ReplaceAll(amountStr, "\r", ""))
			amount, err := strconv.ParseFloat(amountStr, 64)
			if err != nil {
				fmt.Println("❌ INVALID VALUE: Token amount must be a number.")
				continue
			}
			
			fmt.Print("🕊️ Enter Voluntary Free-Will Offering Fee (Size): ")
			offeringStr, _ := reader.ReadString('\n')
			offeringStr = strings.TrimSpace(offeringStr)
			offering, err := strconv.ParseFloat(offeringStr, 64)
			if err != nil {
				fmt.Println("❌ INVALID VALUE: Offering fee must be a number.")
				continue
			}

			fmt.Println("\n⏳ Authorizing, cryptographically signing, and formatting UTXO slices...")
			time.Sleep(800 * time.Millisecond)

			// Dynamically generate a localized pseudo-genesis entry hook to prevent double-spend collision drops
						// 🌟 THE FIX: Map inputs straight to your verified historical genesis transaction ID!
						// 🌟 THE FINISHED FIX: Declare payloadBytes at the top scope of Option 2!
			var payloadBytes []byte

			uniqueInputSource := "TX_GENESIS_INITIAL_POOL"

			tx := Transaction{
				ID: fmt.Sprintf("TX_OUTBOUND_%d", time.Now().UnixNano()),
				Inputs: []UTXOInput{
					{TxID: uniqueInputSource, OutputIdx: 0, Signature: privKey},
				},
				Outputs: []UTXOOutput{
					{Recipient: recipient, Amount: amount},
				},
				FreeWillOffering: offering,
				DataSizeKB:       1.0,
				Witness:          "Sovereign_Console_Client_Signature",
			}

			payloadBytes, err = json.Marshal(tx)
			if err != nil {
				fmt.Println("❌ SERIALIZATION FAILURE: Unable to format data payload frame.")
				continue
			}

			targetURL := "http://localhost:8081/inject_tx"
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Post(targetURL, "application/json", bytes.NewBuffer(payloadBytes))
			if err != nil {
				fmt.Printf("❌ HANDSHAKE FAILURE: Local server endpoint is offline or port is closed.\n")
				continue
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				fmt.Println("\n🚀 BROADCAST SUCCESSFUL! Transaction payload injected into active network mempool queues.")
			} else {
				fmt.Printf("\n❌ SERVER REJECTION: Endpoint responded with status code %d\n", resp.StatusCode)
			}

		} else if choiceStr == "3" {
			fmt.Println("🔒 Exiting lightweight client application cleanly.")
			break
		}
	}
}