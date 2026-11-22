package node

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// A node that cannot open the port other nodes reach it on stops. It used
// to carry on with no connections at all.
func TestANodeThatCannotOpenItsPortStops(t *testing.T) {
	onNetwork(t, "devnet")
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	_, port, _ := net.SplitHostPort(holder.Addr().String())
	Params.GossipPort, _ = strconv.Atoi(port)
	Params.ListenHost = "127.0.0.1"

	stopped := false
	saved := stopBecausePortIsTaken
	stopBecausePortIsTaken = func() { stopped = true }
	t.Cleanup(func() { stopBecausePortIsTaken = saved })

	if _, err := net.Listen("tcp", listenAddress(Params.GossipPort)); err == nil {
		t.Skip("this machine lets two programs listen on one port")
	}
	StartMeshNetwork() // returns at once: the port is taken
	if !stopped {
		t.Error("the node did not stop when its port was already in use")
	}
}

// The list of an address's coins reads every coin, and anyone can ask for
// it. One caller is answered a few times and then asked to wait.
func TestTheCoinListHasAnAllowance(t *testing.T) {
	t.Chdir(t.TempDir())
	startTestChain(t, "devnet")
	buildTestChain(t, 2)
	pages := syncAPI()

	ask := func(from string) int {
		request := httptest.NewRequest(http.MethodGet, "/utxos?address="+testAddress("miner"), nil)
		request.RemoteAddr = from
		answer := httptest.NewRecorder()
		pages.ServeHTTP(answer, request)
		return answer.Code
	}
	answered, refused := 0, 0
	for i := 0; i < 40; i++ {
		switch ask("203.0.113.9:5000") {
		case http.StatusOK:
			answered++
		case http.StatusTooManyRequests:
			refused++
		}
	}
	if answered < 5 || answered > 12 || refused != 40-answered {
		t.Errorf("of 40 requests at once from one address, %d were answered and %d asked to wait", answered, refused)
	}
	// Somebody else is not held up by it.
	if code := ask("203.0.113.10:5000"); code != http.StatusOK {
		t.Errorf("another address was answered with %d", code)
	}

	// Only a few lists are made at once, whoever asks.
	for i := 0; i < cap(coinListsInProgress); i++ {
		coinListsInProgress <- struct{}{}
	}
	if code := ask("203.0.113.11:5000"); code != http.StatusServiceUnavailable {
		t.Errorf("with every place taken, a request was answered with %d", code)
	}
	for i := 0; i < cap(coinListsInProgress); i++ {
		<-coinListsInProgress
	}
	if code := ask("203.0.113.11:5000"); code != http.StatusOK {
		t.Errorf("with places free again, a request was answered with %d", code)
	}
}

// From GitHub's code scanner: the frame writer added up the size of what it
// was about to send without a limit. Nothing this program builds is near
// the limit, but a frame over it would be refused by whoever received it
// and counted against this node, so it is not sent at all.
func TestAFrameOverTheLimitIsNotSent(t *testing.T) {
	onNetwork(t, "devnet")
	var sent bytes.Buffer
	writeFrame(&sent, strings.Repeat("x", MaxPayloadFrameSize+1))
	if sent.Len() != 0 {
		t.Errorf("%d bytes were sent for a message over the limit", sent.Len())
	}
	writeFrame(&sent, strings.Repeat("x", MaxPayloadFrameSize))
	if want := 4 + 32 + MaxPayloadFrameSize; sent.Len() != want {
		t.Errorf("a message exactly at the limit sent %d bytes, want %d", sent.Len(), want)
	}
}
