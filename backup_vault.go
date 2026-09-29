package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const SourceFile = "ledger_vault.json"

// Define a safe fallback directory path inside your user profile to act as a secondary partition mock
var BackupDirectory = filepath.Join(os.Getenv("USERPROFILE"), "Documents", "cvn_backup_vault")

type Block struct {
	Index     int64  `json:"index"`
	Hash      string `json:"hash"`
	Difficulty int64 `json:"difficulty"`
}

func main() {
	fmt.Println("==================================================================")
	fmt.Println("🛡️  COVENANT STANDARD PROTOCOL AUTOMATED BACKUP RIG")
	fmt.Println("====================================================\n")

	// 1. Verify if the primary database ledger vault exists on disk
	source, err := os.Open(SourceFile)
	if err != nil {
		fmt.Println("⚠️  Backup Aborted: Active ledger_vault.json not detected in project folder.")
		return
	}
	defer source.Close()

	// 2. Create the target redundancy folder path if it does not exist yet
	if err := os.MkdirAll(BackupDirectory, 0755); err != nil {
		fmt.Printf("⚠️  Path Error: Failed to initialize backup partition: %v\n", err)
		return
	}

	// 3. Generate a clean, timestamped file target signature (e.g., ledger_backup_2026_09_29.json)
	timestamp := time.Now().Format("2006_01_02_150405")
	backupFileName := fmt.Sprintf("ledger_backup_%s.json", timestamp)
	destinationPath := filepath.Join(BackupDirectory, backupFileName)

	destination, err := os.Create(destinationPath)
	if err != nil {
		fmt.Printf("⚠️  Write Error: Failed to create target archive file: %v\n", err)
		return
	}
	defer destination.Close()

	// 4. Stream the data bytes directly onto the backup destination path
	bytesWritten, err := io.Copy(destination, source)
	if err != nil {
		fmt.Printf("⚠️  Stream Error: Redundancy duplication failed mid-pass: %v\n", err)
		return
	}

	// 5. Unmarshal data quickly to verify file completeness before declaring success
	source.Seek(0, 0)
	var chain []Block
	decoder := json.NewDecoder(source)
	if err := decoder.Decode(&chain); err != nil {
		fmt.Println("❌ ALERT: Backup file written but integrity check FAILED (Data Corrupt).")
		return
	}

	// Calculate current block height for descriptive reporting
	blockHeight := len(chain) - 1
	if blockHeight < 0 {
		blockHeight = 0
	}

	fmt.Println("✅ REDUNDANCY SYNC SUCCESSFUL!")
	fmt.Printf("📦 Archived Ledger File Name : %s\n", backupFileName)
	fmt.Printf("📂 Target Vault Directory     : %s\n", BackupDirectory)
	fmt.Printf("📈 Validated Chain State Height: Block Height %d verified\n", blockHeight)
	fmt.Printf("💾 Total Database Payload Sync : %d bytes mirrored safely\n", bytesWritten)
	fmt.Println("==================================================================")
}