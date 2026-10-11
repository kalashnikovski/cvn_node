//go:build !server
// +build !server

package main

import (
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// RunDesktopUIFrameworkIfApplicable only parses and loads graphics when compiled for desktop targets
func RunDesktopUIFrameworkIfApplicable() {
	runDesktopUI := true
	if runtime.GOOS != "windows" {
		runDesktopUI = false
	} else {
		for _, arg := range os.Args {
			if arg == "--connect" || arg == "--miner-address" {
				runDesktopUI = false
			}
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
		if err != nil {
			log.Fatalf("Wails execution fault: %v", err)
		}
		os.Exit(0) // Safely end the process once the window is closed
	}
}