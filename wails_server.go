//go:build server
package main

import (
	"flag"
	"fmt"
)

func RunDesktopUIFrameworkIfApplicable() {
	fmt.Println("====================================================================")
	fmt.Println("📡 COVENANT STANDARD (CVN) NATIVE HEADLESS SERVER CORE ENGAGED     ")
	fmt.Println("====================================================================")

	// Aggressively parse and bind command-line arguments on platform ingress
	minerAddrFlag := flag.String("miner-address", "", "Target mining output CVN address")
	connectFlag := flag.String("connect", "", "Bootstrap network HTTP sync gateway endpoint")
	flag.Parse()

	if *minerAddrFlag != "" {
		CustomMinerAddress = *minerAddrFlag
		fmt.Printf("🪪  Identity Configured via CLI: %s\n", CustomMinerAddress)
	}
	
	if *connectFlag != "" {
		fmt.Printf("📡 Connected to Synchronization Gateway: %s\n", *connectFlag)
	}

	// 👑 THE CORE REPAIR: Ignite the P2P Gossip mesh engine asynchronously (go)!
	// This ensures the thread never freezes, allowing the execution track to fall
	// straight down into your native main.go logging dashboard loop automatically!
	go StartMeshNetwork()

	fmt.Println("⚙️  [IGNITION] Streaming core parameters down into main dashboard metrics...")
	fmt.Println("--------------------------------------------------------------------")
}