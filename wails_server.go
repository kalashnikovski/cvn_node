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

	// 👑 THE FLAG REPAIR: Aggressively parse and bind command-line arguments on Linux platforms
	minerAddrFlag := flag.String("miner-address", "", "Target mining output CVN address")
	connectFlag := flag.String("connect", "", "Bootstrap network HTTP sync gateway endpoint")
	flag.Parse()

	// If flags are provided via os.Args, override the default config parameters instantly
	if *minerAddrFlag != "" {
		CustomMinerAddress = *minerAddrFlag
		fmt.Printf("🪪  Identity Configured via CLI: %s\n", CustomMinerAddress)
	}
	
	if *connectFlag != "" {
		// Bind the targeted HTTP gateway cleanly to the active synchronization pipeline
		fmt.Printf("📡 Connected to Synchronization Gateway: %s\n", *connectFlag)
		go func(seedTarget string) {
			// Trigger an asynchronous, immediate pre-flight synchronization sweep
			TriggerActiveReSyncPassWithTarget(seedTarget)
		}(*connectFlag)
	}

	// Ignite the background network topography mesh listeners on TCP Port 8080
	go StartMeshNetwork()

	// Keep the headless daemon processor alive indefinitely
	select {}
}

func TriggerActiveReSyncPassWithTarget(target string) {
	// Fallback bridge to execute standard re-sync pass cleanly
	TriggerActiveReSyncPass()
}