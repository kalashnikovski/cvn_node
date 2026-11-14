//go:build desktop && windows

package node

import (
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// RunWalletWindow opens the desktop wallet and returns true once the window
// has been closed. This build of the program is the wallet only: it never
// opens the chain database or mines, so it can run beside the miner. It talks
// to the miner (or a public seed node) over HTTP.
func RunWalletWindow() bool {
	wailsApp := NewApp()
	err := wails.Run(&options.App{
		Title:  "Covenant Standard (CVN) Wallet",
		Width:  1100,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 10, G: 10, B: 15, A: 1},
		OnStartup:        wailsApp.startup,
		Bind:             []interface{}{wailsApp},
	})
	if err != nil {
		log.Fatalf("The wallet window could not be opened: %v", err)
	}
	return true
}
