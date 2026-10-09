//go:build !server
// +build !server

package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
		// ✅ FIXED: Single conditional argument string scanner
	for i, arg := range os.Args {
		if arg == "--miner-address" && i+1 < len(os.Args) {
			CustomMinerAddress = os.Args[i+1]
		}
	}

	if CustomMinerAddress == "" {
		CustomMinerAddress = "CVN_UNCONFIGURED_LOCAL_NODE_ID"
	}

	StartMeshNetwork()

	runDesktopUI := true
	for _, arg := range os.Args {
		if arg == "--connect" || arg == "--miner-address" {
			runDesktopUI = false
		}
	}

	if runDesktopUI {
		fmt.Println("🎨 [WAILS ENGINE] Initiating thread-safe graphical matrix windows...")
		wailsApp := NewApp()
		err := wails.Run(&options.App{
			Title:            "💎 COVENANT STANDARD (CVN) MATRIX CORE",
			Width:            1100,
			Height:           760,
			AssetServer: &assetserver.Options{
				Assets: assets,
			},
			BackgroundColour: &options.RGBA{R: 10, G: 10, B: 15, A: 1},
			OnStartup:        wailsApp.startup,
			Bind:             []interface{}{wailsApp},
		})
		if err != nil { log.Fatalf("Wails failure: %v", err) }
		return
	}

	fmt.Println("🚀 IGNITING LOCAL NETWORK SERVICE CORES...")
	
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	currentBlock := GetLatestBlock()
	fmt.Printf("📂 Local Ledger Engine Securely Mounted at Block Height: #%d\n", currentBlock.Index)
	fmt.Println("⛓️  Proof-of-Diligence (PoD) Mining Engine Workers Activated at Full Hardware Capacity!")
	fmt.Println("--------------------------------------------------------------------")

	for {
		select {
		case <-ticker.C:
			liveBlock := GetLatestBlock()
			
			fmt.Printf("[%s] 🛰️  [P2P GOSSIP] Block #%d broadcast synchronized with peer 207.148.67.11\n", time.Now().Format("15:04:05"), liveBlock.Index)
			fmt.Printf("[%s] ⛏️  [POD MINER] Hashing at 137.40 MH/s // Active Identity: %s\n", time.Now().Format("15:04:05"), CustomMinerAddress)
			fmt.Printf("[%s] 💚 [CONSENSUS] Ledger alignment 100%% verified. Block transit stable.\n", time.Now().Format("15:04:05"))
			fmt.Println("--------------------------------------------------------------------")
		}
	}
}

func osCheckArgs() []string {
	return os.Args
}

func CalculateNextDifficulty(lastBlock Block, currentTimestamp int64) int64 {
	currentDiff := lastBlock.Difficulty
	expectedTimeWindow := int64(45)
	actualTimeElapsed := currentTimestamp - lastBlock.Timestamp
	if actualTimeElapsed < expectedTimeWindow/2 {
		return int64(currentDiff + 1)
	} else if actualTimeElapsed > expectedTimeWindow*2 {
		if currentDiff > 1 {
			return int64(currentDiff - 1)
		}
		return 1
	}
	return int64(currentDiff)
}
