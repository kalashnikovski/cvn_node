package node

import (
	"net"
	"strings"
	"testing"
)

// Seen on the development network (18 November 2026): every seed, started
// with the list of all six seeds, treated its own address as a seventh node.
// It pinged itself, sent itself blocks, and every minute or two logged that
// it had stopped hearing from itself. A node now knows its own addresses and
// leaves them out.
func TestANodeDoesNotTreatItselfAsAPeer(t *testing.T) {
	onNetwork(t, "devnet")
	const mine, other = "203.0.113.7", "203.0.113.8" // documentation addresses
	noteOwnHost(mine)
	t.Cleanup(func() {
		ownHosts.Lock()
		delete(ownHosts.set, mine)
		ownHosts.Unlock()
	})

	if !isOwnHost(mine) || isOwnHost(other) {
		t.Fatal("the machine's own address was not told apart from another's")
	}
	if !isOwnGossipAddress(Params.gossipAddress(mine)) || isOwnGossipAddress(Params.gossipAddress(other)) {
		t.Error("the node's own address for other nodes was not recognised")
	}
	// Another program on the same machine, on another port, is not this node.
	if isOwnGossipAddress(net.JoinHostPort(mine, "9")) {
		t.Error("a different port on this machine was taken for this node")
	}
	if !isOwnSyncURL(Params.syncURL(mine)) || isOwnSyncURL(Params.syncURL(other)) {
		t.Error("the node's own block-download address was not recognised")
	}
	// The loopback address is never counted: tests and other local programs use it.
	noteOwnHost("127.0.0.1")
	if isOwnHost("127.0.0.1") || isOwnSyncURL("http://127.0.0.1:"+Params.syncPortText()) {
		t.Error("the loopback address was counted as this node")
	}
	for _, nonsense := range []string{"", "not an address", "example.org:28080"} {
		if isOwnHost(nonsense) || isOwnGossipAddress(nonsense) || isOwnSyncURL(nonsense) {
			t.Errorf("%q was taken for this node", nonsense)
		}
	}

	// Given a list of seeds that includes itself, the node contacts the others only.
	Params.SeedHosts = []string{mine, other}
	contacted := strings.Join(seedsAndCachedPeers(), " ")
	if strings.Contains(contacted, mine) || !strings.Contains(contacted, other) {
		t.Errorf("nodes to contact: %q; want the other seed and not this node", contacted)
	}
	if !skipSeedOnThisMachine(Params.syncURL(mine)) || skipSeedOnThisMachine(Params.syncURL(other)) {
		t.Error("the node would download blocks from itself, or would not from another seed")
	}
	for _, tier := range syncSourceTiers() {
		for _, endpoint := range tier {
			if strings.Contains(endpoint, mine) {
				t.Errorf("the node's own address %s is among the places it downloads blocks from", endpoint)
			}
		}
	}
}

// The addresses of the machine's own network interfaces count without
// anyone having to say so: that is the case of a server with a public address.
func TestInterfaceAddressesAreOwn(t *testing.T) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Skip("this machine's interfaces could not be listed")
	}
	found := false
	for _, address := range addresses {
		ipnet, ok := address.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() || ipnet.IP.IsUnspecified() {
			continue
		}
		found = true
		if !isOwnHost(ipnet.IP.String()) {
			t.Errorf("interface address %s was not recognised as this machine's own", ipnet.IP)
		}
	}
	if !found {
		t.Skip("this machine has no address other than loopback")
	}
}
