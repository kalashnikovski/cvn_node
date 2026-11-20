//go:build multinode

package node

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// FINDING 2, with a real node. A stranger sends one PEER_PING. The node
// lists it, opens a connection to it within ten seconds (an uplink), and
// then acts on every announcement the stranger sends down that connection,
// with no allowance.
func TestMultiNodeRound6AStrangerIsCalledAndFloodsTheUplink(t *testing.T) {
	c := newLocalCluster(t)
	node := c.seed("node", 1, nil).startNode()
	c.waitForHeight(30*time.Second, 0, node)

	stranger := c.host(99)
	listener, err := net.Listen("tcp", net.JoinHostPort(stranger, strconv.Itoa(Params.GossipPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	// One ping, from the stranger's address.
	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(stranger)}, Timeout: 3 * time.Second}
	hello, err := dialer.Dial("tcp", net.JoinHostPort(node.host, strconv.Itoa(Params.GossipPort)))
	if err != nil {
		t.Fatal(err)
	}
	writeFrame(hello, "PEER_PING:DISCOVERY_REQUEST")
	_ = hello.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, _ = readFrame(hello)
	hello.Close()

	// The node calls the stranger.
	_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(60 * time.Second))
	far, err := listener.Accept()
	if err != nil {
		t.Fatalf("the node did not open a connection to the stranger: %v", err)
	}
	defer far.Close()

	const sent = 5000
	var requests int64
	go func() {
		for {
			_ = far.SetReadDeadline(time.Now().Add(30 * time.Second))
			payload, _, err := readFrame(far)
			if err != nil {
				return
			}
			if strings.HasPrefix(string(payload), "BLOCK_REQUEST:") {
				atomic.AddInt64(&requests, 1)
			}
		}
	}()
	began := time.Now()
	for i := 0; i < sent; i++ {
		announcement, _ := json.Marshal(blockAnnouncement{Height: 1, Hash: fmt.Sprintf("%064x", i+1)})
		writeFrame(far, "BLOCK_ANNOUNCE:"+string(announcement))
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt64(&requests) < sent {
		time.Sleep(50 * time.Millisecond)
	}
	elapsed := time.Since(began)
	allowed := int64(50 + 20*elapsed.Seconds())
	got := atomic.LoadInt64(&requests)
	t.Logf("%d announcements sent; the node asked for %d blocks in %s and wrote %d log lines about them", sent, got, elapsed.Round(time.Millisecond), strings.Count(node.log(), "asking for it"))
	if got > allowed {
		t.Errorf("a stranger that sent one ping was called by the node and then had %d announcements acted on in %s; the allowance on a connection it opened itself would have let through at most %d", got, elapsed.Round(time.Millisecond), allowed)
	}
}
