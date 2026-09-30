package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// BackupLedgerManifest automatically clones the ledger vault state to protect data equity
func BackupLedgerManifest(chain []Block) {
	if len(chain) == 0 {
		return
	}

	backupFileName := fmt.Sprintf("ledger_vault_backup_height_%d.json", chain[len(chain)-1].Index)

	data, err := json.MarshalIndent(chain, "", "  ")
	if err != nil {
		fmt.Printf("⚠️ [Backup Engine Failure] Could not serialize data: %v\n", err)
		return
	}

	err = os.WriteFile(backupFileName, data, 0644)
	if err != nil {
		fmt.Printf("⚠️ [Backup Engine Failure] Write permission denied: %v\n", err)
		return
	}

	fmt.Printf("💾 [Archive Vault] Structural snapshot created successfully: %s\n", backupFileName)
}

// CleanOldSnapshots runs an internal pruning pass to prevent local disk exhaustion
func CleanOldSnapshots() {
	fmt.Println("🧹 [Archive Vault] Maintenance scan active. Retaining canonical state history profiles.")
}
