package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

type AnalyticsBlock struct {
	Index        int64         `json:"index"`
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
}

// TestRunLedgerMilestoneAnalytics executes a deep file growth and token velocity projection scan
func TestRunLedgerMilestoneAnalytics(t *testing.T) {
	fmt.Println("\n====================================================")
	fmt.Println("📊 COVENANT STANDARD STANDALONE ANALYTICS SIDECAR")
	fmt.Println("====================================================")

	ledgerPath := "ledger_vault.json"
	fileInfo, err := os.Stat(ledgerPath)
	if err != nil {
		fmt.Printf("🚨 ACCESS ERROR: Unable to locate active file '%s': %v\n", ledgerPath, err)
		return
	}

	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		fmt.Printf("🚨 READ ERROR: Failed to stream ledger text from disk: %v\n", err)
		return
	}

	var chain []AnalyticsBlock
	if err := json.Unmarshal(data, &chain); err != nil {
		fmt.Printf("🚨 PARSE ERROR: Malformed block serialization payload: %v\n", err)
		return
	}

	totalBlocks := len(chain)
	if totalBlocks < 2 {
		fmt.Println("📋 Scanning Stalled: Not enough block history to calculate delta metrics yet.")
		return
	}

	// 1. Calculate Core Growth Statistics
	currentSizeBytes := fileInfo.Size()
	currentSizeKB := float64(currentSizeBytes) / 1024.0
	bytesPerBlock := float64(currentSizeBytes) / float64(totalBlocks)

	// 2. Measure Velocity Metrics over the entire file lifecycle
	firstBlock := chain[0]
	lastBlock := chain[totalBlocks-1]
	totalDurationSeconds := lastBlock.Timestamp - firstBlock.Timestamp
	if totalDurationSeconds <= 0 {
		totalDurationSeconds = 1
	}

	blocksPerDay := (float64(totalBlocks) / float64(totalDurationSeconds)) * 86400.0
	projectedBytesPerDay := blocksPerDay * bytesPerBlock
	projectedMegabytesPerMonth := (projectedBytesPerDay * 30.0) / (1024.0 * 1024.0)

	// 3. Compute Days to Critical Milestones
	daysTo10MB := (10.0*1024.0*1024.0 - float64(currentSizeBytes)) / projectedBytesPerDay
	daysTo50MB := (50.0*1024.0*1024.0 - float64(currentSizeBytes)) / projectedBytesPerDay

	if daysTo10MB < 0 { daysTo10MB = 0 }
	if daysTo50MB < 0 { daysTo50MB = 0 }

	// 4. Print Beautiful Real-World Telemetry Insights
	fmt.Printf("📈 Current Ledger File Size:     %.2f KB (%d Bytes)\n", currentSizeKB, currentSizeBytes)
	fmt.Printf("⛓️  Total Active Block Height:    %d Blocks\n", totalBlocks)
	fmt.Printf("📦 Average Storage Footprint:    %.2f Bytes per Block\n", bytesPerBlock)
	fmt.Printf("🛰️  Calculated Network Velocity:  %.1f Blocks per Day\n", blocksPerDay)
	fmt.Println("----------------------------------------------------")
	fmt.Println("🔮 30-DAY LEDGER STORAGE EXPANSION PROJECTIONS")
	fmt.Println("----------------------------------------------------")
	fmt.Printf("🚀 Estimated Growth Rate:        %.2f MB / Month\n", projectedMegabytesPerMonth)
	fmt.Printf("⏳ Estimated Days to 10 MB:      %.1f Days (Runway Safe)\n", daysTo10MB)
	fmt.Printf("🚨 Estimated Days to 50 MB:      %.1f Days (Optimize Boundary)\n", daysTo50MB)
	fmt.Println("====================================================")
}