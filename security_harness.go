package main

import (
	"fmt"
)

// RunSecurityChecks performs structural threat vector testing, tracking spent UTXO inputs
func RunSecurityChecks(chain []Block) bool {
	if len(chain) < 2 {
		return true
	}

	fmt.Println("🛡️  [Security Harness] Sabbatical Slasher monitoring loop active.")

	// Global map tracking spent transactions inside this validation pass
	type SpentKey struct {
		TxID string
		Idx  int
	}
	spentUTXOMap := make(map[SpentKey]string) // Maps a UTXO key to the block height where it was spent

	// Scan entire blockchain history chronologically
	for _, block := range chain {
		for _, tx := range block.Transactions {
			// Skip coinbase blocks because they do not consume any historical inputs
			if len(tx.Inputs) == 0 {
				continue
			}

			for _, in := range tx.Inputs {
				key := SpentKey{TxID: in.TxID, Idx: in.OutputIdx}
				
				// 🚨 CRITICAL DOUBLE-SPEND TRAP DETECTED
				if historicalBlockHeight, alreadySpent := spentUTXOMap[key]; alreadySpent {
					fmt.Println("====================================================================")
					fmt.Println("🚨🚨 SECURITY HARNESS CRITICAL THREAT WARNING: DOUBLE SPEND DETECTED 🚨🚨")
					fmt.Println("====================================================================")
					fmt.Printf("🛑 Violator attempted to consume an already spent coin allocation!\n")
					fmt.Printf("🛑 Target Output: TxID [%s] | Index %d\n", in.TxID, in.OutputIdx)
					fmt.Printf("🛑 Conflict Status: This clump was already permanently consumed in Block %s\n", historicalBlockHeight)
					fmt.Println("====================================================================")
					return false
				}

				// If pristine, register this input into our memory index tracking map
				spentUTXOMap[key] = fmt.Sprintf("#%d", block.Index)
			}
		}
	}

	// Secondary check: Validate chronological link integrity
	for i := 1; i < len(chain); i++ {
		if chain[i].PrevHash != chain[i-1].Hash {
			fmt.Printf("🚨 [SECURITY ALERT] Hash chain discontinuity detected at Block Height %d!\n", chain[i].Index)
			return false
		}
		
		recalculatedHash := CalculateHash(chain[i])
		if chain[i].Hash != recalculatedHash {
			fmt.Printf("🚨 [SECURITY ALERT] Block payload mutation detected at Block Height %d!\n", chain[i].Index)
			return false
		}
	}

	fmt.Println("✅ [Security Harness] Ledger integrity verified. Zero double-spend anomalies detected.")
	return true
}

// LogSlasherSeizure footprint logs the automated Sabbatical Slasher penalization state details
func LogSlasherSeizure(minerAddress string, bondAmount float64) {
	fmt.Printf("\n⚖️  [Sabbatical Slasher] COVERT MANIPULATION LOOP FLAGGED!\n")
	fmt.Printf("🛑 Violator Node ID: %s\n", minerAddress)
	fmt.Printf("🔥 ACTION: Seizing %.2f CVN bond and routing directly to the Burn Void.\n\n", bondAmount)
}