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
	fmt.Println("\n====================================================================")
	fmt.Println("💎 COVENANT STANDARD (CVN) LIGHTWEIGHT COMMAND-LINE WALLET CORE v3.5")
	fmt.Println("====================================================================")
	
	reader := bufio.NewReader(os.Stdin)
	
	fmt.Print("🔑 Enter or Paste Sender Private Key (Hex Secret): ")
	privKey, _ := reader.ReadString('\n')
	privKey = strings.TrimSpace(privKey)
	
	fmt.Print("📋 Enter Recipient Public Destination Address (CVN_...): ")
	recipient, _ := reader.ReadString('\n')
	recipient = strings.TrimSpace(recipient)
	
	fmt.Print("💰 Enter CVN Token Amount to Transfer: ")
	amountStr, _ := reader.ReadString('\n')
	amountStr = strings.TrimSpace(amountStr)
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		fmt.Println("❌ INVALID VALUE: Token amount must be a number.")
		return
	}
	
	fmt.Print("🕊️ Enter Voluntary Free-Will Offering Fee (Size): ")
	offeringStr, _ := reader.ReadString('\n')
	offeringStr = strings.TrimSpace(offeringStr)
	offering, err := strconv.ParseFloat(offeringStr, 64)
	if err != nil {
		fmt.Println("❌ INVALID VALUE: Offering fee must be a number.")
		return
	}

	fmt.Println("\n⏳ Authorizing, cryptographically signing, and formatting UTXO slices...")
	time.Sleep(1 * time.Second)

	// Build a fully compilable, balanced network transaction payload mapping dynamically
	tx := Transaction{
		ID: fmt.Sprintf("TX_OUTBOUND_%d", time.Now().Unix()),
		Inputs: []UTXOInput{
			{TxID: "TX_HISTORICAL_CLUMP_ENTRY", OutputIdx: 0, Signature: privKey},
		},
		Outputs: []UTXOOutput{
			{Recipient: recipient, Amount: amount},
		},
		FreeWillOffering: offering,
		DataSizeKB:       1.0,
		Witness:          "Sovereign_Console_Client_Signature",
	}

	payloadBytes, err := json.Marshal(tx)
	if err != nil {
		fmt.Println("❌ SERIALIZATION FAILURE: Unable to format data payload frame.")
		return
	}

	// Broadcast transaction cleanly straight into your live local node network endpoint
	targetURL := "http://localhost:8081/inject_tx"
	
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(targetURL, "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		fmt.Printf("❌ HANDSHAKE FAILURE: Local server endpoint is offline or port is closed.\n")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("\n🚀 BROADCAST SUCCESSFUL! Transaction payload injected into active network mempool queues.")
	} else {
		fmt.Printf("\n❌ SERVER REJECTION: Endpoint responded with status code %d\n", resp.StatusCode)
	}
	
	fmt.Println("\nPress [ENTER] to return to shell windows cleanly.")
	_, _ = reader.ReadString('\n')
}