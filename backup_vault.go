package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

var BackupMutex sync.Mutex

// BackupLedgerManifest non-blockingly updates ONE single static backup file
func BackupLedgerManifest(chain []Block) {
	if len(chain) == 0 {
		return
	}

	// Spin the backup process completely out into a background worker thread
	go func(blocks []Block) {
		BackupMutex.Lock()
		defer BackupMutex.Unlock()

		// FIXED: Enforce a single static file name so it overwrites instead of multiplying!
		backupFileName := "ledger_vault_backup.json"

		// Serialize the blocks array cleanly into the backup data slot
		data, err := json.MarshalIndent(blocks, "", "  ")
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

		fmt.Printf("💾 [Archive Vault] Single ledger snapshot updated cleanly to: %s\n", backupFileName)
	}(chain)
}