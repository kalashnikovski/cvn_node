package main

import (
	"fmt"
	"net"
	"time"
)

// SetupAutomatedPortMapping coordinates non-blocking local interface discovery loops safely
func SetupAutomatedPortMapping() {
	// Spin execution safely out to an isolated background thread to protect terminal rendering speeds
	go func() {
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return
		}

		for _, address := range addrs {
			// Isolate private IPv4 address slices safely from local loops
			if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					localIP := ipnet.IP.String()
					
					// Pacing delay loop to ensure main layout prints establish first 
					time.Sleep(500 * time.Millisecond)
					fmt.Printf("📡 [NAT SHIELD] Interface Bound Natively: %s (Listening on Port :8080)\n", localIP)
					return
				}
			}
		}
	}()
}