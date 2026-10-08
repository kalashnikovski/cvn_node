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
	
	// 2. Invoke your native mesh sync gate validation loop in a background thread
	go EnforceSyncGateLock("http://207.148.67.11:8081", func() int64 {
		return GetLatestBlock().Index
	})

	// 3. 🌐 MAINNET BASE ROUTE Explorer Welcome Splash Portal
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><body style="background:#0a0a0f;color:#00ff66;font-family:monospace;padding:3rem;text-align:center;">
			<h1>💎 COVENANT STANDARD MAINNET EXPLORER CORE</h1>
			<p style="color:#00ffff;">SYSTEM STATUS: ACTIVE // TIP: #%d</p>
			<p style="color:#8a8a9e;">Query /peers or /block endpoints to stream live ledger data slices.</p>
		</body></html>`, GetLatestBlock().Index)
	})

	// 4. 🛰️ PEERS GATEWAY ENDPOINT
	http.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		fmt.Fprintf(w, `{"status": "ONLINE", "current_height": %d, "global_tip": %d, "peers": ["202.137.175.220"]}`, GetLatestBlock().Index, GetLatestBlock().Index)
	})

	// 5. 📦 BLOCK METRICS ENDPOINT (Serves true dynamic block telemetry strings)
	http.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		fmt.Fprintf(w, `{"block_height": %d, "difficulty": %d, "hash": "%s"}`, 
			GetLatestBlock().Index, GetLatestBlock().Difficulty, GetLatestBlock().Hash)
	})

	fmt.Println("📡 Explorer API Gateway Matrix listening natively on port :8081...")
	if err := http.ListenAndServe(":8081", nil); err != nil {
		log.Fatalf("Critical network socket failure: %v", err)
	}
}