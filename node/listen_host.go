package node

import (
	"net"
	"strconv"
	"time"
)

// ONE ADDRESS OF THIS MACHINE
//
// A node ordinarily listens on every address its machine has, and lets the
// machine choose which address its own connections leave from. A network may
// instead name one address (ChainParams.ListenHost): the node then listens
// there only, and its connections to other nodes leave from there too, so
// that they see it under that address.
//
// No published network names one. It is what lets several nodes run on one
// machine, each on its own loopback address, for the tests in
// multinode_test.go.

// listenAddress is where this node listens for a port.
func listenAddress(port int) string {
	return net.JoinHostPort(Params.ListenHost, strconv.Itoa(port))
}

// dialPeer opens a connection to another node.
func dialPeer(address string, timeout time.Duration) (net.Conn, error) {
	dialer := net.Dialer{Timeout: timeout}
	if ip := net.ParseIP(Params.ListenHost); ip != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: ip}
	}
	return dialer.Dial("tcp", address)
}

// extraNetworks are networks that exist only in programs built for testing
// (see localnet_multinode.go). In a released program the list is empty.
var extraNetworks = map[string]func() (ChainParams, error){}
