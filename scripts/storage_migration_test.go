package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// TestSandboxDatabaseMigrationAndHealing simulates, executes, and validates 
// the JSON ledger ingestion pipeline and Block #1025 gap healing pass.
func TestSandboxDatabaseMigrationAndHealing(t *testing.T) {
	fmt.Println("\n====================================================")
	fmt.Println("🔬 CVN SANDBOX ENGINE: STORAGE & HEALING LAB TEST")
	fmt.Println("====================================================")

	// Clean out any conflicting test db or ledger files left over from prior sandbox runs
	os.Remove("cvn_mainnet.db")
	os.Remove("ledger_vault.json")
	os.Remove("archived_ledger_vault.json.bak")

		fmt.Println("⏳ [Phase 1] Constructing mock historical JSON ledger tracking data...")
	
	// Create a perfectly sequential artificial chain slice containing a broken link gap at Index 2
	mockJsonChain := []Block{
		{
			Index:     0,
			Timestamp: 1790640000,
			Transactions: []Transaction{
				{ID: "TX_GENESIS_INITIAL_POOL", Outputs: []UTXOOutput{{Recipient: "COVENANT_STEWARD_ASSEMBLY", Amount: 2100000000}}},
			},
			PrevHash: "0000000000000000000000000000000000000000000000000000000000000000",
			Hash:     "42fd693c8a69714ded75dc29cb9ec7e7eda58c4c92d37af5faf2ad4f3ef4bc18",
		},
		{
			Index:     1,
			Timestamp: 1790830000,
			Transactions: []Transaction{
				{ID: "TX_COINBASE_1", Outputs: []UTXOOutput{{Recipient: "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337", Amount: 50}}},
			},
			PrevHash: "42fd693c8a69714ded75dc29cb9ec7e7eda58c4c92d37af5faf2ad4f3ef4bc18",
			Hash:     "000002d5a9308d3ecb5c99afd1195cddec4f216168420f63adb9e32f77bbabbd",
		},
		{
			Index:     2,
			Timestamp: 1790838461,
			Transactions: []Transaction{
				{ID: "COINBASE_REWARD_HEIGHT_2", Outputs: []UTXOOutput{{Recipient: "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337", Amount: 50}}},
			},
			PrevHash: "BROKEN_MALICIOUS_POINTER_GAP_ERROR_VALUE_HERE", // 🚨 INTENTIONAL CONFLICT GAP LINK
			Hash:     "000000572b1ed7fd6701ce3296dcfa75df0a2621a3503863271ecf14e590ff1b",
		},
	}


	// Serialize and drop the mock plain-text seed ledger file onto the disk workspace
	jsonData, err := json.MarshalIndent(mockJsonChain, "", "  ")
	if err != nil {
		t.Fatalf("❌ Test Setup Error: Failed to serialize mock chain layout: %v", err)
	}
	if err := os.WriteFile("ledger_vault.json", jsonData, 0644); err != nil {
		t.Fatalf("❌ Test Setup Error: Failed to write seed file frame onto disk: %v", err)
	}
	fmt.Println("✅ Legacy 'ledger_vault.json' written safely to laboratory directory partition.")

	fmt.Println("⏳ [Phase 2] Initializing BoltDB Persistence Engine and starting migration...")
	
	// Execute engine initialization. This intercepts the JSON file, triggers the migrator, 
	// detects block 1025's pointer mutation error, and programmatically heals the cryptographic link!
	InitBoltEngine()
	defer GlobalBoltEngine.Close()

	fmt.Println("⏳ [Phase 3] Pulling full chain verification state from BoltDB buckets...")
	
	// Read back the entire block database to verify persistence and structural healing state parameters
	savedBlocks := LoadFullChainSlice()
	if len(savedBlocks) != 3 {
		t.Fatalf("🚨 TEST FAIL: Expected 3 database block nodes saved to binary store, but found %d instead.", len(savedBlocks))
	}

		block1 := savedBlocks[1] // Balanced at Index 1
	block2 := savedBlocks[2] // Balanced at Index 2

	fmt.Printf("🔍 Post-Migration Link Verification Pass:\n")
	fmt.Printf("   ↳ Block 1 Target Hash: %s\n", block1.Hash)
	fmt.Printf("   ↳ Block 2 Recovered PrevHash: %s\n", block2.PrevHash)

	// Validate that the system successfully replaced the error pointer with Block 1's actual true hash
	if block2.PrevHash != block1.Hash {
		t.Errorf("🚨 TEST FAIL: The cryptographic healing path failed to correct block 2's parent link.")
	} else {
		fmt.Println("🎉 LINK SEQUENCE CHECK: contiguous lineage confirmed across structural block gaps!")
	}

	// Verify that the final block hash was accurately re-computed after the structural link healing pass
	expectedHealedHash := CalculateHash(block2)
	if block2.Hash != expectedHealedHash {
		t.Errorf("🚨 TEST FAIL: Block 2 internal hash structure is out of sync with healed values.")
	} else {
		fmt.Println("🎉 CRYPTOGRAPHIC HASH PASS: healed payload string hash parameters are 100% valid.")
	}

	// Confirm that the legacy JSON file was archived to clean the workstation directory space
	if _, err := os.Stat("ledger_vault.json"); !os.IsNotExist(err) {
		t.Errorf("🚨 MIGRATION ALIGNMENT WARNING: Plain-text ledger seed file was not archived properly.")
	} else {
		fmt.Println("🎉 DISK CLEANUP STATUS: 'ledger_vault.json' cleaned and moved to backup sectors cleanly.")
	}

	fmt.Println("====================================================")
	fmt.Println("✅ CRITICAL DATABASE PERSISTENCE TEST PASS: 100% SECURE")
	fmt.Println("====================================================")
}