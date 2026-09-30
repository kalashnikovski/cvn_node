package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// AuditActiveLedger analyzes the ledger file to compute network health statistics
func AuditActiveLedger() {
	if _, err := os.Stat(BlockchainFile); os.IsNotExist(err) {
		fmt.Println("📖 [Ledger Auditor] Scan skipped: No ledger_vault.json found yet.")
		return
	}

	data, err := os.ReadFile(BlockchainFile)
	if err != nil {
		fmt.Printf("⚠️ [Ledger Auditor Error] Cannot read ledger: %v\n", err)
		return
	}

	// Use an anonymous interface array map to entirely bypass struct compilation caching bottlenecks
	var chain []map[string]interface{}
	err = json.Unmarshal(data, &chain)
	if err != nil {
		fmt.Printf("⚠️ [Ledger Auditor Error] Malformed JSON data structure: %v\n", err)
		return
	}

	var totalTransactions int = 0
	var burnedTokens float64 = 0.0

	for _, block := range chain {
		if txs, ok := block["transactions"].([]interface{}); ok {
			totalTransactions += len(txs)
			for _, txRaw := range txs {
				if tx, ok := txRaw.(map[string]interface{}); ok {
					recipient, _ := tx["recipient"].(string)
					if recipient == "0x0000000000000000000000000000000000000000_BURN_VOID" {
						amount, _ := tx["amount"].(float64)
						offering, _ := tx["free_will_offering"].(float64)
						burnedTokens += amount + offering
					}
				}
			}
		}
	}

	fmt.Println("====================================================")
	fmt.Println("📖 COVENANT STANDARD (CVN) ON-CHAIN METRICS AUDIT")
	fmt.Println("====================================================")
	fmt.Printf("📈 Total Validated Blocks:    %d\n", len(chain))
	fmt.Printf("🔄 Total Network Velocity:    %d Transactions\n", totalTransactions)
	fmt.Printf("🔥 Total Jubilee Burned Void: %.6f CVN\n", burnedTokens)
	fmt.Printf("🪙 Net Circulating Supply:     %.6f CVN\n", 2100000000.0-burnedTokens)
	fmt.Println("====================================================")
}
