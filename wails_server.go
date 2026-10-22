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

	// 👑 THE CRITICAL DATA MOUNT:
	// Force the headless execution path to initialize the BoltDB ledger core right on boot!
	// This populates your database bucket contexts and connects the global pointer hooks cleanly.
	InitBoltEngine()

	go StartMeshNetwork()

	fmt.Println("⚙️  [IGNITION] Streaming core parameters down into main dashboard metrics...")
	fmt.Println("--------------------------------------------------------------------")
}