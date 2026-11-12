package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PEER CACHE
//
// A node used to know only the built-in seed addresses when it started. If the
// seeds were offline it could not find the network, even though it had been
// talking to other reachable nodes an hour earlier.
//
// Every peer this node has itself dialed and received a correctly signed
// answer from is now remembered in peers.json. On the next start those peers
// are tried alongside the seeds for discovery, live block feeds and sync.
// Addresses merely mentioned by other peers are not saved until this node has
// reached them itself, so a peer cannot fill the file with junk.

const (
	peerCacheFile         = "peers.json"
	peerCacheMaxEntries   = 100
	peerCacheMaxAge       = 30 * 24 * time.Hour
	peerCacheSaveInterval = 30 * time.Second
	peerCacheSyncGateways = 5
)

type cachedPeer struct {
	Address  string `json:"address"`
	LastSeen int64  `json:"last_seen"`
}

var (
	peerCache      = make(map[string]int64) // address -> last successful contact (unix)
	peerCacheDirty bool
	peerCacheMutex sync.Mutex
	peerCacheOnce  sync.Once
)

// normalizePeerAddress returns "ip:port" for a usable peer address, or "".
func normalizePeerAddress(address string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return ""
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return ""
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return ""
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(portNumber))
}

func loadPeerCache() {
	raw, err := os.ReadFile(peerCacheFile)
	if err != nil {
		return
	}
	var stored []cachedPeer
	if json.Unmarshal(raw, &stored) != nil {
		log.Printf("⚠️ [PEER CACHE] %s is unreadable; starting with seeds only.\n", peerCacheFile)
		return
	}
	now := time.Now()
	peerCacheMutex.Lock()
	for _, entry := range stored {
		address := normalizePeerAddress(entry.Address)
		if address == "" || entry.LastSeen > now.Unix()+3600 {
			continue
		}
		if now.Sub(time.Unix(entry.LastSeen, 0)) > peerCacheMaxAge {
			continue
		}
		peerCache[address] = entry.LastSeen
	}
	trimPeerCacheLocked()
	count := len(peerCache)
	peerCacheMutex.Unlock()
	if count > 0 {
		announceNetworkStatus(fmt.Sprintf("📒 [PEER CACHE] Loaded %d known peers from %s.", count, peerCacheFile))
	}
}

// trimPeerCacheLocked drops the stalest entries beyond the size limit.
func trimPeerCacheLocked() {
	if len(peerCache) <= peerCacheMaxEntries {
		return
	}
	ordered := sortedPeerCacheLocked()
	for _, entry := range ordered[peerCacheMaxEntries:] {
		delete(peerCache, entry.Address)
	}
}

func sortedPeerCacheLocked() []cachedPeer {
	ordered := make([]cachedPeer, 0, len(peerCache))
	for address, lastSeen := range peerCache {
		ordered = append(ordered, cachedPeer{Address: address, LastSeen: lastSeen})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].LastSeen != ordered[j].LastSeen {
			return ordered[i].LastSeen > ordered[j].LastSeen
		}
		return ordered[i].Address < ordered[j].Address
	})
	return ordered
}

// StartPeerCache loads peers.json and keeps it up to date. Safe to call twice.
func StartPeerCache() {
	peerCacheOnce.Do(func() {
		loadPeerCache()
		go func() {
			for {
				time.Sleep(peerCacheSaveInterval)
				savePeerCacheIfDirty()
			}
		}()
	})
}

// rememberReachablePeer records a peer this node dialed and got a correctly
// signed answer from.
func rememberReachablePeer(address string) {
	address = normalizePeerAddress(address)
	if address == "" {
		return
	}
	now := time.Now().Unix()
	peerCacheMutex.Lock()
	// Rewriting the file for every 15-second ping would be wasteful; a peer's
	// timestamp only needs refreshing occasionally.
	if previous, known := peerCache[address]; !known || now-previous > 600 {
		peerCache[address] = now
		peerCacheDirty = true
		trimPeerCacheLocked()
	}
	peerCacheMutex.Unlock()
}

func forgetCachedPeer(address string) {
	address = normalizePeerAddress(address)
	peerCacheMutex.Lock()
	if _, known := peerCache[address]; known {
		delete(peerCache, address)
		peerCacheDirty = true
	}
	peerCacheMutex.Unlock()
}

func savePeerCacheIfDirty() {
	peerCacheMutex.Lock()
	if !peerCacheDirty {
		peerCacheMutex.Unlock()
		return
	}
	ordered := sortedPeerCacheLocked()
	peerCacheDirty = false
	peerCacheMutex.Unlock()

	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return
	}
	// Write to a temporary file first so a crash never leaves half a file.
	temporary := peerCacheFile + ".tmp"
	if err := os.WriteFile(temporary, data, 0644); err != nil {
		return
	}
	if err := os.Rename(temporary, peerCacheFile); err != nil {
		_ = os.Remove(temporary)
	}
}

// cachedPeerAddresses lists remembered peers, most recently reached first.
func cachedPeerAddresses() []string {
	peerCacheMutex.Lock()
	ordered := sortedPeerCacheLocked()
	peerCacheMutex.Unlock()
	addresses := make([]string, 0, len(ordered))
	for _, entry := range ordered {
		addresses = append(addresses, entry.Address)
	}
	return addresses
}

// cachedPeerHTTPGateways returns sync URLs (port 8081) for the most recently
// reached peers, skipping any host already in alreadyListed.
func cachedPeerHTTPGateways(alreadyListed []string) []string {
	var gateways []string
	for _, address := range cachedPeerAddresses() {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			continue
		}
		duplicate := false
		for _, existing := range alreadyListed {
			if strings.Contains(existing, "//"+host+":") {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		gateways = append(gateways, "http://"+net.JoinHostPort(host, "8081"))
		if len(gateways) >= peerCacheSyncGateways {
			break
		}
	}
	return gateways
}

// seedsAndCachedPeers is the bootstrap list: configured seeds first, then
// remembered peers, without duplicates.
func seedsAndCachedPeers() []string {
	seen := make(map[string]bool)
	var combined []string
	for _, address := range append(loadStaticMeshSeeds(), cachedPeerAddresses()...) {
		address = strings.TrimSpace(address)
		if address == "" || seen[address] {
			continue
		}
		seen[address] = true
		combined = append(combined, address)
	}
	return combined
}
