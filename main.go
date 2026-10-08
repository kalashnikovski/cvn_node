//go:build !server
// +build !server

package main

import (
	"embed"
	"fmt"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// ✅ INITIALIZE ADDRESS: Instantly loads your true mining wallet identifier on boot
	CustomMinerAddress = "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337"

	// Ignite your local P2P networking channels and explorer telemetry servers automatically
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
	var currentBlock Block
	currentBlock = GetLatestBlock()
	fmt.Printf("📂 Local Blockheight Operational: #%d\n", currentBlock.Index)
	
	select {}
}