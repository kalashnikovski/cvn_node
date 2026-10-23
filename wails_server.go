//go:build linux && server
// +build linux,server

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

// RunHeadlessServerUIFramework acts as the pure headless initializer for Linux cloud virtual instances.
func RunDesktopUIFrameworkIfApplicable() {
	fmt.Println("====================================================================")
	fmt.Println("📡 COVENANT STANDARD (CVN) NATIVE HEADLESS SERVER CORE ENGAGED     ")
	fmt.Println("====================================================================")

	minerAddrFlag := flag.String("miner-address", "", "Target mining output CVN address")
	connectFlag := flag.String("connect", "", "Bootstrap network HTTP sync gateway endpoint")
	flag.Parse()

	if *minerAddrFlag != "" {
		CustomMinerAddress = strings.TrimSpace(*minerAddrFlag)
	}

	if CustomMinerAddress == "" {
		configFileBytes, err := os.ReadFile("miner_config.json")
		if err == nil {
			var parsedConfig map[string]string
			if json.Unmarshal(configFileBytes, &parsedConfig) == nil {
				CustomMinerAddress = strings.TrimSpace(parsedConfig["miner_address"])
			}
		}
	}

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

	// Ignite core P2P mesh parameters
	go StartMeshNetwork()

	fmt.Println("⚙️  [IGNITION] Headless sync engine securely locked into memory pools.")
	fmt.Println("--------------------------------------------------------------------")
	
	// Infinite background daemon hold
	select {}
}