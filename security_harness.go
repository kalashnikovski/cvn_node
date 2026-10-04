package main

import (
	"encoding/json"
	"fmt"
	"strings"
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

// InterceptGossipBlock parses and runs rapid isolation validation checks on inbound network data strings
func InterceptGossipBlock(gossipMessage string, latestLocalBlock Block) (*Block, bool) {
	// 1. Strip the wire protocol routing header
	if !strings.HasPrefix(gossipMessage, "BLOCK_PROPAGATE:") {
		return nil, false
	}
	rawJSON := strings.TrimPrefix(gossipMessage, "BLOCK_PROPAGATE:")
	rawJSON = strings.TrimSpace(rawJSON)

	// 2. Deserialize payload parameters inside isolated memory context
	var incomingBlock Block
	if err := json.Unmarshal([]byte(rawJSON), &incomingBlock); err != nil {
		fmt.Printf("🚨 [SECURITY WARNING] Malformed json packet dropped from mesh thread.\n")
		return nil, false
	}

	// 3. Threat Vector 1: Check block lineage continuity upfront
	if incomingBlock.PrevHash != latestLocalBlock.Hash || incomingBlock.Index != latestLocalBlock.Index+1 {
		// Out of sync block structure or stale broadcast, drop silently without throwing a panic
		return nil, false
	}

	// 4. Threat Vector 2: Re-verify proof-of-diligence cryptographic integrity
	recalculatedHash := CalculateHash(incomingBlock)
	if incomingBlock.Hash != recalculatedHash {
		fmt.Printf("🚨 [SECURITY WARNING] Forged or mutated block hash dropped from mesh! Expected: %s\n", recalculatedHash)
		return nil, false
	}

	// 5. Threat Vector 3: Enforce strict Q4 2026 Protocol Size Cap (1MB ceiling defense)
	if len(rawJSON) > 1024*1024 {
		fmt.Printf("🚨 [SECURITY WARNING] Over-sized block payload (%d bytes) dropped! Thread protected.\n", len(rawJSON))
		return nil, false
	}

	// Block passed upfront checks, completely safe to handle via main state thread hooks
	return &incomingBlock, true
}

// LogSlasherSeizure footprint logs the automated Sabbatical Slasher penalization state details
func LogSlasherSeizure(minerAddress string, bondAmount float64) {
	fmt.Printf("\n⚖️  [Sabbatical Slasher] COVERT MANIPULATION LOOP FLAGGED!\n")
	fmt.Printf("🛑 Violator Node ID: %s\n", minerAddress)
	fmt.Printf("🔥 ACTION: Seizing %.2f CVN bond and routing directly to the Burn Void.\n\n", bondAmount)
}