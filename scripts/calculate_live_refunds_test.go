package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

type BlockPayload struct {
	Index        int64         `json:"index"`
	Transactions []Transaction `json:"transactions"`
}

// TestCalculateLiveRefundLedger runs an offline analytical loop over your active ledger vault state data
func TestCalculateLiveRefundLedger(t *testing.T) {
	fmt.Println("\n====================================================")
	fmt.Println("📊 COVENANT STANDARD LIVE REFUND LEDGER TRACKER v1.1")
	fmt.Println("====================================================")

	ledgerFile := "ledger_vault.json"
	data, err := os.ReadFile(ledgerFile)
	if err != nil {
		fmt.Printf("🚨 DATABASE ACCESS ERROR: Cannot read %s: %v\n", ledgerFile, err)
		return
	}

	var chain []BlockPayload
	if err := json.Unmarshal(data, &chain); err != nil {
		fmt.Printf("🚨 PARSE ERROR: Malformed block sequence layout: %v\n", err)
		return
	}

		liveBlockTotals := make(map[string]int)

	for _, block := range chain {
		// Ensure the block actually has transactions committed to it safely
		if len(block.Transactions) > 0 {
			// Lock the scanner explicitly to Index 0 (The Coinbase Reward Transaction)
			coinbaseTx := block.Transactions[0]
			
			if coinbaseTx.Witness != "" && coinbaseTx.Witness != "LOCAL_NODE_ENGINE" {
				liveBlockTotals[coinbaseTx.Witness]++
			}
		}
	}


	fmt.Printf("%-30s %-20s %-25s\n", "Node Identity Tag", "Live Blocks Mined", "Live Reimbursement Owed")
	fmt.Println("--------------------------------------------------------------------------------")

	var grandTotalOwed float64 = 0.0
	for tag, totalBlocks := range liveBlockTotals {
		if tag == "LOCAL_NODE_ENGINE" || tag == "CVN_b10bf930e5bd41577fa162cbaef5339abf0f9af" {
			continue
		}
		
		liveReimbursement := float64(totalBlocks) * 50.0
		grandTotalOwed += liveReimbursement
		fmt.Printf("%-30s %-20d %.2f CVN\n", tag, totalBlocks, liveReimbursement)
	}

	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("📈 TOTAL LIVE LIQUIDITY ESCROW REQUIREMENT: %.2f CVN\n", grandTotalOwed)
	fmt.Println("====================================================")
}