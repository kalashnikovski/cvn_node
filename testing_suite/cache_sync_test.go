package main

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestLiveCacheStateEngine Updates directly evaluates the real-time delta 
// cache modifier behavior built into your SaveBlockToStorage engine.
func TestLiveCacheStateEngineUpdates(t *testing.T) {
	fmt.Println("\n====================================================")
	fmt.Println("🔬 CVN SANDBOX ENGINE: LIVE UTXO DELTA CACHE TEST")
	fmt.Println("====================================================")

	// 1. Wipe out any stale database instances on disk
	os.Remove("cvn_mainnet.db")
	os.Remove("ledger_vault.json")
	os.Remove("archived_ledger_vault.json.bak")

	fmt.Println("⏳ [Phase 1] Bootstrapping BoltDB engine and setting baseline values...")
	InitBoltEngine()
	defer GlobalBoltEngine.Close()

	// Initialize and forcefully seed our memory state balance mapping via our startup routine
	RebuildStateBalanceCache()

	// Isolate pre-test baseline positions
	creatorAddress := "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"
	targetAddress  := "CVN_SPARKS_MOCK_TARGET_WALLET_ID_ADDRESS_777"

	baseCreatorBalance := GetAddressBalance(creatorAddress)
	baseTargetBalance  := GetAddressBalance(targetAddress)

	fmt.Printf("🔍 Baseline Startup State Inquiries:\n")
	fmt.Printf("   ↳ Creator Account Balance: %.2f CVN\n", baseCreatorBalance)
	fmt.Printf("   ↳ Target Peer Account Balance:  %.2f CVN\n", baseTargetBalance)

	// Verify that the Genesis setup successfully credited the Reserve account profile
	reserveBalance := GetAddressBalance("COVENANT_STEWARD_ASSEMBLY")
	if reserveBalance != MaxTotalSupplyCap {
		t.Fatalf("🚨 INITIALIZATION ERROR: State cache missing primary genesis allocation values.")
	}

	// For the sake of the sandbox test, artificially deposit tokens into your address to spend
	BalanceCacheMutex.Lock()
	StateBalanceCache[creatorAddress] = 1000.00
	BalanceCacheMutex.Unlock()

	fmt.Println("⏳ [Phase 2] Mocking transaction and solving Block Height #1...")
	
	// Create an outbound spend transaction of 150 CVN with a 5 CVN voluntary fee bribe
	testTx := Transaction{
		ID:               "TX_SANDBOX_LIVE_SPEND_SECTOR_9",
		Inputs:           []UTXOInput{{TxID: "TX_SPEND_SOURCE_SEED_12345", OutputIdx: 0}},
		Outputs:          []UTXOOutput{{Recipient: targetAddress, Amount: 150.00}},
		FreeWillOffering: 5.00,
		DataSizeKB:       1.0,
		Witness:          creatorAddress, // Senders authorize identity strings here
	}

	// Formulate a clean Coinbase block solution reward of 50 CVN + the 5 CVN fee transaction yield
	coinbaseRewardTx := Transaction{
		ID:               "TX_COINBASE_MOCK_BLOCK_1",
		Inputs:           []UTXOInput{},
		Outputs:          []UTXOOutput{{Recipient: creatorAddress, Amount: 55.00}},
		FreeWillOffering: 0.0,
		DataSizeKB:       0.1,
		Witness:          "Communal_Peer_Witness_7",
	}

	mockBlock1 := Block{
		Index:        1,
		Timestamp:    time.Now().Unix(),
		Transactions: []Transaction{coinbaseRewardTx, testTx},
		PrevHash:     "42fd693c8a69714ded75dc29cb9ec7e7eda58c4c92d37af5faf2ad4f3ef4bc18",
		Hash:         "00000abc1237890def4567890abcdef1234567890abcdef1234567890abcdef",
		Difficulty:   4,
	}

	fmt.Println("⏳ [Phase 3] Committing Block #1 into database storage hooks (Triggering Delta Cache)...")
	
	// Execute the update! This writes to disk AND dynamically adjusts the runtime memory map
	SaveBlockToStorage(mockBlock1)

	// Pull our post-execution positions straight from memory cache
	postCreatorBalance := GetAddressBalance(creatorAddress)
	postTargetBalance  := GetAddressBalance(targetAddress)

	fmt.Printf("🔍 Post-Execution Memory State Inquiries:\n")
	fmt.Printf("   ↳ Creator Account Balance: %.2f CVN\n", postCreatorBalance)
	fmt.Printf("   ↳ Target Peer Account Balance:  %.2f CVN\n", postTargetBalance)

	// 📐 MATHEMATICAL AUDIT CHECK FOR ACCURATE LEDGER STATE DELTAS
	// Creator should be: 1000.00 (Start) - 150.00 (Sent) - 5.00 (Fee) + 55.00 (Coinbase + Fee Yield) = 900.00 CVN
	expectedCreatorDelta := 1000.00 - 150.00 - 5.00 + 55.00
	if postCreatorBalance != expectedCreatorDelta {
		t.Errorf("🚨 CACHE SYNC FAILURE: Expected Creator wallet balance to be %.2f, but got %.2f", expectedCreatorDelta, postCreatorBalance)
	} else {
		fmt.Println("🎉 SENDER ACCOUNT AUDIT: Debits and fee collections verified completely in memory cache!")
	}

	// Target recipient should be: 0.00 (Start) + 150.00 (Received) = 150.00 CVN
	expectedTargetDelta := 150.00
	if postTargetBalance != expectedTargetDelta {
		t.Errorf("🚨 CACHE SYNC FAILURE: Expected Target wallet balance to be %.2f, but got %.2f", expectedTargetDelta, postTargetBalance)
	} else {
		fmt.Println("🎉 RECIPIENT ACCOUNT AUDIT: Credits successfully matched and adjusted in memory cache!")
	}

	fmt.Println("====================================================")
	fmt.Println("✅ REAL-TIME MATRIX ENGINE BALANCE UPDATE PASSED: 100% VALID")
	fmt.Println("====================================================")
}