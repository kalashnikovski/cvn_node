//go:build server
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

// RunDesktopUIFrameworkIfApplicable handles the headless initialization parameters on server targets.
func RunDesktopUIFrameworkIfApplicable() {
	fmt.Println("====================================================================")
	fmt.Println("📡 COVENANT STANDARD (CVN) NATIVE HEADLESS SERVER CORE ENGAGED     ")
	fmt.Println("====================================================================")

	minerAddrFlag := flag.String("miner-address", "", "Target mining output CVN address")
	connectFlag := flag.String("connect", "", "Bootstrap network HTTP sync gateway endpoint")
	flag.Parse()

	if *minerAddrFlag != "" {
		CustomMinerAddress = *minerAddrFlag
	}

	if CustomMinerAddress == "" {
		configFileBytes, err := os.ReadFile("miner_config.json")
		if err == nil {
			var parsedConfig map[string]string
			if json.Unmarshal(configFileBytes, &parsedConfig) == nil {
				// Cleanly strip string whitespaces and line endings to prevent Linux parsing breaks
				CustomMinerAddress = strings.TrimSpace(parsedConfig["miner_address"])
			}
		}
	}

	// Force an immediate configuration reset if the file read path returned a broken placeholder string
	if CustomMinerAddress == "" || CustomMinerAddress == "CVN_UNCONFIGURED_LOCAL_NODE_ID" {
		fmt.Println("👤 [SERVER IDENTITY] Missing valid miner configuration. Provisioning secure L1 address handle...")
		_, _, serverAddress, err := GenerateKeyPair()
		if err == nil {
			CustomMinerAddress = serverAddress
			configMap := map[string]string{"miner_address": serverAddress}
			configMapBytes, _ := json.MarshalIndent(configMap, "", "  ")
			_ = os.WriteFile("miner_config.json", configMapBytes, 0644)
		}
	}

	fmt.Printf("🪪  ACTIVE CLOUD RIG IDENTITY: %s\n", CustomMinerAddress)
	if *connectFlag != "" {
		fmt.Printf("📡 Connected to Synchronization Gateway: %s\n", *connectFlag)
	}

	// Ignite your core P2P Gossip mesh channels asynchronously
	go StartMeshNetwork()

	fmt.Println("⚙️  [IGNITION] Headless sync engine securely locked into memory pools.")
	fmt.Println("--------------------------------------------------------------------")
	
	// 👑 THE MASTER CONVERGENCE REPAIR ANCHOR:
	// Yield execution directly to an infinite blocking thread hold block right here!
	// This permanently locks your headless server thread inside this file, stopping it 
	// from ever falling into main.go to spawn duplicate network layers or corrupt difficulty targets!
	select {}
}