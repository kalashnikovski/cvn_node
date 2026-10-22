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

	// ✅ FIX: Remove the duplicate InitBoltEngine() call from here entirely!
	// Let main.go handle the single database file file handle lock uniformly on boot.

	go StartMeshNetwork()

	fmt.Println("⚙️  [IGNITION] Streaming core parameters down into main dashboard metrics...")
	fmt.Println("--------------------------------------------------------------------")
}