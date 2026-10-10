//go:build !server
// +build !server

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time" // ✅ FIXED: Added to support lightweight time.RFC3339 
)

var BackupMutex sync.Mutex

// BackupLedgerManifest non-blockingly updates a lightweight structural tracking file
func BackupLedgerManifest(latestBlock Block) {
	if latestBlock.Index < 0 {
		return
	}

	// Spin the backup tracking process safely out into an isolated background worker thread
	go func(block Block) {
		BackupMutex.Lock()
		defer BackupMutex.Unlock()

		backupFileName := "ledger_vault_backup.json"

		// Create a lightweight, high-performance summary object to eliminate memory bloat entirely
		summary := map[string]interface{}{
			"last_updated": time.Now().Format(time.RFC3339),
			"block_height": block.Index,
			"block_hash":   block.Hash,
			"difficulty":   block.Difficulty,
			"prev_hash":    block.PrevHash,
		}

		data, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			fmt.Printf("⚠️ [Backup Engine Alert] Serialization failure: %v\n", err)
			return
		}

		// Overwrite the single file safely on disk
		err = os.WriteFile(backupFileName, data, 0644)
		if err != nil {
			fmt.Printf("⚠️ [Backup Engine Alert] Write permissions blocked: %v\n", err)
			return
		}

		fmt.Printf("💾 [Archive Vault] Single ledger snapshot updated cleanly to: %s (#%d)\n", backupFileName, block.Index)
	}(latestBlock)
}