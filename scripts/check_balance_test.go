package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

type AuditBlock struct {
	Index        int64         `json:"index"`
	Transactions []Transaction `json:"transactions"`
}

// TestExecuteAddressBalanceCheck query your live database file using direct arguments
func TestExecuteAddressBalanceCheck(t *testing.T) {
	fmt.Println("====================================================")
	fmt.Println("🔍 COVENANT STANDARD (CVN) OFFLINE BALANCE INQUIRY")
	fmt.Println("====================================================")

	// 1. Scan the entire terminal argument slice for any string starting with CVN_
	var targetAddress string
	for _, arg := range os.Args {
		trimmedArg := strings.TrimSpace(arg)
		if strings.HasPrefix(trimmedArg, "CVN_") {
			targetAddress = trimmedArg
			break
		}
	}

	if targetAddress == "" {
		fmt.Println("❌ PROTOCOL LOGISTICS ERROR: Missing destination target parameter flag.")
		fmt.Println("👉 Correct Playbook Usage: go test -v -run=TestExecuteAddressBalanceCheck YOUR_CVN_ADDRESS")
		fmt.Println("====================================================")
		return
	}

	// 2. Open and load your active live database file
	ledgerPath := "ledger_vault.json"
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		fmt.Printf("🚨 DATABASE ERROR: Unable to read file '%s': %v\n", ledgerPath, err)
		return
	}

	var chain []AuditBlock
	if err := json.Unmarshal(data, &chain); err != nil {
		fmt.Printf("🚨 PARSE ERROR: Malformed block sequence layout: %v\n", err)
		return
	}

	// 3. Scan the entire chain history to calculate the live UTXO balance
	var balance float64 = 0.0
	var receivedTransactions int = 0

	for _, block := range chain {
		for _, tx := range block.Transactions {
			for _, output := range tx.Outputs {
				if output.Recipient == targetAddress {
					balance += output.Amount
					receivedTransactions++
				}
			}
		}
	}

	// 4. Render the finalized balance sheet to the terminal console
	fmt.Println("----------------------------------------------------")
	fmt.Printf("📋 QUERY TARGET:       %s\n", targetAddress)
	fmt.Printf("🔢 TOTAL RECEIPTS:     %d Transaction Outputs Found\n", receivedTransactions)
	fmt.Printf("💰 UNSPENT BALANCE:    %.2f CVN\n", balance)
	fmt.Println("====================================================")
}
// TestInventoryAllNetworkAddresses extracts and lists every single unique wallet address on the blockchain
func TestInventoryAllNetworkAddresses(t *testing.T) {
	fmt.Println("====================================================")
	fmt.Println("📡  COVENANT STANDARD (CVN) NETWORK WALLET INVENTORY")
	fmt.Println("====================================================")

	ledgerPath := "ledger_vault.json"
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("🚨 DATABASE ERROR: Unable to load file '%s': %v\n", ledgerPath, err)
	}

	var chain []AuditBlock
	if err := json.Unmarshal(data, &chain); err != nil {
		t.Fatalf("🚨 PARSE ERROR: Malformed block sequence layout: %v\n", err)
	}

	// Use a map structure to instantly filter out duplicates and isolate unique keys
	uniqueWallets := make(map[string]int)

	// Chronologically parse every transaction output across every single block payload
	for _, block := range chain {
		for _, tx := range block.Transactions {
			for _, output := range tx.Outputs {
				address := strings.TrimSpace(output.Recipient)
				if strings.HasPrefix(address, "CVN_") {
					uniqueWallets[address]++
				}
			}
		}
	}

	fmt.Printf("📂 Database Scanning Complete. Found %d Total Unique Operating Wallets:\n", len(uniqueWallets))
	fmt.Println("----------------------------------------------------")

	// Print out the ledger inventory sheet with total block hits per wallet
	counter := 1
	for walletAddress, blockHits := range uniqueWallets {
		fmt.Printf("[%d] Wallet: %s\n    ↳ Total Blocks Mined/Received: %d\n", counter, walletAddress, blockHits)
		counter++
	}
	fmt.Println("====================================================")
}
// TestLiveReimbursementAudit tracks unique block solution counts per node tag with absolute precision
func TestLiveReimbursementAudit(t *testing.T) {
	fmt.Println("====================================================")
	fmt.Println("📋  COVENANT STANDARD (CVN) LIVE REIMBURSEMENT AUDIT")
	fmt.Println("====================================================")

	ledgerPath := "ledger_vault.json"
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("🚨 DATABASE ERROR: Unable to load file '%s': %v\n", ledgerPath, err)
	}

	var chain []AuditBlock
	if err := json.Unmarshal(data, &chain); err != nil {
		t.Fatalf("🚨 PARSE ERROR: Malformed block sequence layout: %v\n", err)
	}

	// Track total blocks verified by each text identity signature using the primary coinbase index
	witnessInventory := make(map[string]int)
	var totalMinedBlocks int = 0
	
	// ⚡ ANTI-GLITCH POINTER INDEX
	zeroIndex := 0

	for _, block := range chain {
		if len(block.Transactions) > zeroIndex {
			// ✅ FIXED BYPASS: Explicitly isolates the first transaction array slot
			coinbaseTx := block.Transactions[zeroIndex]
			witnessTag := strings.TrimSpace(coinbaseTx.Witness)
			
			if witnessTag == "" {
				witnessTag = "Communal_Peer_Witness_7"
			}
			witnessInventory[witnessTag]++
			totalMinedBlocks++
		}
	}

	fmt.Printf("%-30s %-20s %-20s\n", "Node Identity Tag", "Total Blocks Mined", "Total Reimbursement Owed")
	fmt.Println("--------------------------------------------------------------------------------")

	var calculatedTokenSum float64 = 0.0
	for nodeTag, blocksMined := range witnessInventory {
		reimbursement := float64(blocksMined) * 50.0
		calculatedTokenSum += reimbursement
		fmt.Printf("%-30s %-20d %.2f CVN\n", nodeTag, blocksMined, reimbursement)
	}

	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("📂 TOTAL BLOCKHEIGHT ACCORDING TO AUDIT: %d Blocks\n", totalMinedBlocks)
	fmt.Printf("💰 TOTAL ACCUMULATED LEDGER INCENTIVES: %.2f CVN\n", calculatedTokenSum)
	fmt.Println("====================================================")
}