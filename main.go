// Command cvn_node is the Covenant Standard (CVN) program: node, miner and
// wallet in one. Everything it does is in the node folder; this file only
// packs the desktop wallet's screens into the program and starts it.
//
// Build it with:
//
//	go build -o cvn_node .
package main

import (
	"embed"

	"cvn_node/node"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

func main() {
	node.Run(frontendAssets)
}
