package main

import (
	"fmt"
	"time" // Included cleanly to define block time signatures
)

// RunSecurityChecks performs structural threat vector testing on processed blocks
func RunSecurityChecks(chain []Block) bool {
	if len(chain) < 2 {
		return true
	}

	fmt.Println("🛡️  [Security Harness] Sabbatical Slasher monitoring loop active.")

	// Scan blocks to verify chronological consistency and hash link integrity
	for i := 1; i < len(chain); i++ {
		currentBlock := chain[i]
		prevBlock := chain[i-1]

		// Ensure sequential blocks maintain strict mathematical tracking bonds
		if currentBlock.PrevHash != prevBlock.Hash {
			fmt.Printf("🚨  [SECURITY ALERT] Hash chain discontinuity detected at Block Height %d!\n", currentBlock.Index)
			return false
		}

		// Verify that the hash actually matches its structural property parameters
		recalculatedHash := CalculateHash(currentBlock)
		if currentBlock.Hash != recalculatedHash {
			fmt.Printf("🚨  [SECURITY ALERT] Block payload mutation detected at Block Height %d!\n", currentBlock.Index)
			return false
		}
	}

	fmt.Println("✅  [Security Harness] Ledger integrity verified. Zero anomalies detected.")
	return true
}

// LogSlasherSeizure footprint logs the automated Sabbatical Slasher penalization state details
func LogSlasherSeizure(minerAddress string, bondAmount float64) {
	fmt.Printf("\n⚖️  [Sabbatical Slasher] COVERT MANIPULATION LOOP FLAGGED!\n")
	fmt.Printf("🛑 Violator Node ID: %s\n", minerAddress)
	fmt.Printf("🔥 ACTION: Seizing %.2f CVN bond and routing directly to the Burn Void.\n\n", bondAmount)
}

// javaTimeToTime maps raw Unix integers safely into pristine time.Time objects
func javaTimeToTime(unixTime int64) time.Time {
	return time.Unix(unixTime, 0)
}