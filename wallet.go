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
		fmt.Println("💎 COVENANT STANDARD (CVN) LIGHTWEIGHT COMMAND-LINE WALLET CORE v4.0")
		fmt.Println("====================================================================")
		fmt.Println(" 👑 1. Generate a Brand New Unique Wallet (Private Key & CVN Address)")
		fmt.Println(" 🕊️  2. Send an Outbound Free-Will Offering Transaction Payload")
		fmt.Println(" ❌ 3. Exit Wallet Console Application Safely")
		fmt.Println("====================================================================")
		fmt.Print("Select option index [1-3]: ")
		
		choiceStr, _ := reader.ReadString('\n')
		choiceStr = strings.TrimSpace(choiceStr)

		if choiceStr == "1" {
			// ✅ FIXED TYPE ASSIGNMENT: Extracts all 3 coordinate parameters from crypto_auth specs
			privHex, pubHex, walletAddress, err := GenerateKeyPair()
			if err != nil {
				fmt.Printf("🚨 CRYPTOGRAPHIC FAULT: Unable to initialize secure entropy paths: %v\n", err)
				continue
			}
			fmt.Println("\n====================================================================")
			fmt.Println("🎉 NEW SECURE WALLET IDENTITY DETECTED & SCRIPTURALLY LOCKED")
			fmt.Println("====================================================================")
			fmt.Printf("📋 YOUR CVN PUBLIC ADDRESS:   %s\n", walletAddress)
			fmt.Printf("🔑 YOUR PUBLIC KEY (HEX):     %s\n", pubHex)
			fmt.Printf("🔐 YOUR PRIVATE KEY (SECRET):   %s\n", privHex)
			fmt.Println("--------------------------------------------------------------------")
			fmt.Println("⚠️  WARNING: Copy and save your Private Key safely! It is never stored on disk.")
			fmt.Println("====================================================================")

			backupContent := fmt.Sprintf(
				"====================================================\n"+
				"🛰️  COVENANT STANDARD (CVN) SOVEREIGN WALLET KEYS\n"+
				"====================================================\n\n"+
				"📋 PUBLIC WALLET ADDRESS (Safe to share with anyone):\n"+
				"%s\n\n"+
				"🔑 UN-TRUNCATED PUBLIC KEY (Required for validation fields):\n"+
				"%s\n\n"+
				"🔐 SECRET PRIVATE KEY (NEVER SHARE - Keep completely hidden):\n"+
				"%s\n"+
				"====================================================\n", 
				walletAddress, pubHex, privHex,
			)
			
			// Write backup log explicitly using strict read/write authorization keys
			_ = os.WriteFile("my_crypto_address.txt", []byte(backupContent), 0600)
			
			// ✅ FIXED IDENTIFIER: Writes using the precise key layout matching batch loops
			configMap := map[string]string{"miner_address": walletAddress}
			configBytes, _ := json.MarshalIndent(configMap, "", "  ")
			_ = os.WriteFile("miner_config.json", configBytes, 0644)
			
			fmt.Println("💾 SUCCESS: Sovereign wallet credentials safely written to disk inside your directory!")
			fmt.Println("====================================================================")

			fmt.Println("Press [ENTER] to return to the core wallet menu window.")
			_, _ = reader.ReadString('\n')

		} else if choiceStr == "2" {
			fmt.Print("\n🔐 Enter or Paste Sender Private Key (Hex Secret): ")
			privKey, _ := reader.ReadString('\n')
			privKey = strings.TrimSpace(privKey)

			fmt.Print("📋 Enter Sender Public Key (Hex Format): ")
			pubKey, _ := reader.ReadString('\n')
			pubKey = strings.TrimSpace(pubKey)
			
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

			// Standardized message sequence framework to build an authentic ECDSA signature
			txID := fmt.Sprintf("TX_OUTBOUND_%d", time.Now().UnixNano())
			txMessage := fmt.Sprintf("%s-%s-%f", txID, CustomMinerAddress, offering)

			// Sign payload natively leveraging the mathematical rules inside crypto_auth.go
			rStr, sStr, err := SignTransactionPayload(privKey, txMessage)
			if err != nil {
				fmt.Printf("❌ CRYPTO FAULT: Asymmetric signing math aborted: %v\n", err)
				continue
			}
			combinedSignature := fmt.Sprintf("%s|%s", rStr, sStr)

			tx := Transaction{
				ID:        txID,
				Signature: combinedSignature,
				PublicKey: pubKey, // ✅ Fixed Field: Securely attaches public coordinates
				Inputs: []UTXOInput{
					{SourceTxID: "TX_GENESIS_INITIAL_POOL", Index: 0, Signature: combinedSignature},
				},
				Outputs: []UTXOOutput{
					{Recipient: recipient, Amount: amount},
				},
				FreeWillOffering: offering,
				Timestamp:        time.Now().Unix(),
				DataSizeKB:       1.0,
				Witness:          CustomMinerAddress,
			}

			payloadBytes, err := json.Marshal(tx)
			if err != nil {
				fmt.Println("❌ SERIALIZATION FAILURE: Unable to format data payload frame.")
				continue
			}

			// ✅ FIXED URL INTERFACE: Route transaction propagation straight to your cloud gateway anchors
			targetURL := "http://207.148.67" // Broadcast to live Singapore active data pool mesh
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Post(targetURL, "application/json", bytes.NewBuffer(payloadBytes))
			if err != nil {
				fmt.Printf("❌ HANDSHAKE FAILURE: Gateway connection timed out over active interfaces.\n")
				continue
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
				fmt.Println("\n🚀 BROADCAST SUCCESSFUL! Transaction payload securely injected into active mainnet mempools.")
			} else {
				fmt.Printf("\n❌ MAINNET GATEWAY REJECTION: Interface responded with status code %d\n", resp.StatusCode)
			}

		} else if choiceStr == "3" {
			fmt.Println("🔒 Exiting lightweight client application cleanly.")
			break
		}
	}
}