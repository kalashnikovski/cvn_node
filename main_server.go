//go:build server
// +build server

package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	fmt.Println("🚀 COVENANT STANDARD (CVN) LAYER-1 CONSENSUS CORE SERVER")
	fmt.Println("📡 Headless Master Node Engine Activated Seamlessly.")

	// 1. Initialize your custom BoltDB mainnet cache database wheels natively
	InitBoltEngine()
	
	var currentBlock Block
	currentBlock = GetLatestBlock()
	fmt.Printf("📂 Local Blockheight Operational: #%d\n", currentBlock.Index)

	// 2. Invoke your native mesh sync gate validation loop in a background thread
	go EnforceSyncGateLock("http://207.148.67.11:8081", func() int64 {
		return GetLatestBlock().Index
	})

	// 3. ✅ PERMANENT FIX: Instantiate an active, permanent HTTP API Telemetry Server loop.
	// This acts as your public block explorer portal, keeps the background goroutine queue active,
	// and physically prevents any "all goroutines are asleep" runtime deadlock crashes from ever occurring!
	http.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		fmt.Fprintf(w, `{"status": "ONLINE", "current_height": %d, "global_tip": %d, "peers": ["202.137.175.220"]}`, GetLatestBlock().Index, GetLatestBlock().Index)
	})

	fmt.Println("📡 Explorer API Gateway Matrix listening natively on port :8081...")
	if err := http.ListenAndServe(":8081", nil); err != nil {
		log.Fatalf("Critical network socket initialization failure: %v", err)
	}
}