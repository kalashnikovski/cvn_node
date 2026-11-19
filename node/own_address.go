package node

import (
	"net"
	"net/url"
	"strconv"
	"sync"
)

// THIS MACHINE'S OWN ADDRESSES
//
// A node is often given a list of nodes that includes itself: every seed is
// started with the same list of all the seeds, and peers pass on lists of
// the nodes they know. A node that does not recognise its own address treats
// itself as another node: it asks itself for peers, sends itself every
// block, lists itself for others, and from time to time drops itself for
// being silent. None of that does harm, and all of it is wasted.
//
// So a node keeps a note of the addresses that are its own: the addresses of
// its network interfaces (on a server that is its public address), and the
// public address its router reports when the node sits behind one.

var ownHosts = struct {
	sync.RWMutex
	set    map[string]bool
	loaded bool
}{set: make(map[string]bool)}

// noteOwnHost records an address as this machine's own.
func noteOwnHost(host string) {
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return
	}
	ownHosts.Lock()
	ownHosts.set[ip.String()] = true
	ownHosts.Unlock()
}

// loadInterfaceAddresses records the addresses of this machine's network
// interfaces, once.
func loadInterfaceAddresses() {
	ownHosts.Lock()
	already := ownHosts.loaded
	ownHosts.loaded = true
	ownHosts.Unlock()
	if already {
		return
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return
	}
	for _, address := range addresses {
		if ipnet, ok := address.(*net.IPNet); ok {
			noteOwnHost(ipnet.IP.String())
		}
	}
}

// isOwnHost reports whether an address is one of this machine's own. The
// loopback address is not counted: other programs on the same machine, and
// the tests, are reached through it.
func isOwnHost(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	// A node given one address to live at knows that address as its own.
	if Params.ListenHost != "" && ip.Equal(net.ParseIP(Params.ListenHost)) {
		return true
	}
	loadInterfaceAddresses()
	ownHosts.RLock()
	defer ownHosts.RUnlock()
	return ownHosts.set[ip.String()]
}

// isOwnGossipAddress reports whether "host:port" is this node itself: one of
// this machine's addresses, on the port this node listens on for other nodes.
func isOwnGossipAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	return port == strconv.Itoa(Params.GossipPort) && isOwnHost(host)
}

// isOwnSyncURL reports whether a block-download address ("http://host:port")
// is this node itself.
func isOwnSyncURL(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	return parsed.Port() == strconv.Itoa(Params.SyncPort) && isOwnHost(parsed.Hostname())
}
