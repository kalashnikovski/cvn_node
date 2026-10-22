//go:build server
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
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
	}

	// 👑 THE WINDOWS-TO-LINUX PLATFORM BRIDGE:
	// If no explicit address was passed via CLI flags, automatically check for or 
	// generate a unique server identity profile right on disk to prevent loopback collisions!
	if CustomMinerAddress == "" {
		configFileBytes, err := os.ReadFile("miner_config.json")
		if err == nil {
			var parsedConfig map[string]string
			if json.Unmarshal(configFileBytes, &parsedConfig) == nil {
				CustomMinerAddress = parsedConfig["miner_address"]
			}
		}
	}

	if CustomMinerAddress == "" || CustomMinerAddress == "CVN_UNCONFIGURED_LOCAL_NODE_ID" {
		fmt.Println("👤 [SERVER IDENTITY] Generating a unique headless cloud mining token profile...")
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

	go StartMeshNetwork()

	fmt.Println("⚙️  [IGNITION] Streaming core parameters down into main dashboard metrics...")
	fmt.Println("--------------------------------------------------------------------")
}