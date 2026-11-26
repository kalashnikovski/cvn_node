package node

import (
	"net"
	"testing"
	"time"
)

// The work server (`handleStratumClient`) holds a miner that is not on this
// machine to the pool's limits, even when no pool runs: how many connections,
// and refusal for answers without the work. A miner on this machine is not
// counted. (Security audit, November 2026, finding 3.)

// connectionFrom is a connection that says it came from another address. The
// other end is a pipe, which is what the test talks to.
type connectionFrom struct {
	net.Conn
	from *net.TCPAddr
}

func (c connectionFrom) RemoteAddr() net.Addr { return c.from }

// serveAMinerFrom runs the work server on one end of a pipe that says it came
// from this address, IPv4 or IPv6. It returns the miner's end and a channel
// that is closed when the server has finished with the connection.
func serveAMinerFrom(address string) (miner net.Conn, finished chan struct{}) {
	serverEnd, minerEnd := net.Pipe()
	wrapped := connectionFrom{Conn: serverEnd, from: &net.TCPAddr{IP: net.ParseIP(address), Port: 40000}}
	finished = make(chan struct{})
	go func() {
		defer close(finished)
		handleStratumClient(wrapped)
	}()
	return minerEnd, finished
}

// noPoolAndCleanLimits makes sure no pool runs and that the limits this file
// touches are as they are on a fresh node, and puts them back afterwards.
func noPoolAndCleanLimits(t *testing.T) {
	t.Helper()
	savedPool := poolAddress
	poolAddress = ""
	if poolRunning() {
		t.Fatal("a pool is running in the test")
	}
	poolConnections.Lock()
	savedByHost, savedAll := poolConnections.byHost, poolConnections.all
	poolConnections.byHost, poolConnections.all = make(map[string]int), 0
	poolConnections.Unlock()
	poolStrikes.Lock()
	savedStrikes := poolStrikes.byHost
	poolStrikes.byHost = make(map[string]poolStrikeCount)
	poolStrikes.Unlock()
	savedAllowed := poolStrikesAllowed
	t.Cleanup(func() {
		poolAddress = savedPool
		poolStrikesAllowed = savedAllowed
		poolConnections.Lock()
		poolConnections.byHost, poolConnections.all = savedByHost, savedAll
		poolConnections.Unlock()
		poolStrikes.Lock()
		poolStrikes.byHost = savedStrikes
		poolStrikes.Unlock()
	})
}

func connectionsFrom(host string) (perHost, all int) {
	poolConnections.Lock()
	defer poolConnections.Unlock()
	return poolConnections.byHost[host], poolConnections.all
}

// wantServerToHangUp fails the test unless the server finishes with the
// connection at once, without a word to the miner.
func wantServerToHangUp(t *testing.T, what string, miner net.Conn, finished chan struct{}) {
	t.Helper()
	defer miner.Close()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: the work server went on serving the connection, want it closed at once", what)
	}
	_ = miner.SetReadDeadline(time.Now().Add(time.Second))
	if n, err := miner.Read(make([]byte, 64)); n != 0 || err == nil {
		t.Errorf("%s: the work server said %d bytes before closing the connection", what, n)
	}
}

// wantServerToServe fails the test unless the server is still holding the
// connection after a moment, then lets it go and waits for the server.
func wantServerToServe(t *testing.T, what string, miner net.Conn, finished chan struct{}) {
	t.Helper()
	select {
	case <-finished:
		miner.Close()
		t.Fatalf("%s: the work server closed the connection, want it served", what)
	case <-time.After(300 * time.Millisecond):
	}
	miner.Close()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: the work server did not end when the miner left", what)
	}
}

func TestOnlyTheLoopbackAddressesAreThisMachine(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "127.0.0.2": true,
		"203.0.113.7": false, "192.168.1.5": false, "": false, "not-an-address": false,
	} {
		if got := isThisMachine(host); got != want {
			t.Errorf("isThisMachine(%q) is %v, want %v", host, got, want)
		}
	}
}

func TestAMinerElsewhereIsHeldToTheConnectionLimitWhenNoPoolRuns(t *testing.T) {
	noPoolAndCleanLimits(t)
	const elsewhere = "203.0.113.7"
	poolConnections.Lock()
	poolConnections.byHost[elsewhere] = poolConnectionsPerHost
	poolConnections.all = poolConnectionsPerHost
	poolConnections.Unlock()

	// The address already holds every place it may: this one is closed at
	// once, and the places it holds are not taken or given away.
	miner, finished := serveAMinerFrom(elsewhere)
	go miner.Write([]byte(`{"id":1,"method":"login","params":{"login":"x"}}` + "\n"))
	wantServerToHangUp(t, "a miner elsewhere with its places all taken", miner, finished)
	if perHost, all := connectionsFrom(elsewhere); perHost != poolConnectionsPerHost || all != poolConnectionsPerHost {
		t.Errorf("after the refusal the address holds %d places and %d are taken in all; want %d and %d", perHost, all, poolConnectionsPerHost, poolConnectionsPerHost)
	}

	// The same limit means nothing to a miner on this machine, which is not
	// counted at all.
	poolConnections.Lock()
	poolConnections.byHost["127.0.0.1"] = poolConnectionsPerHost
	poolConnections.Unlock()
	miner, finished = serveAMinerFrom("127.0.0.1")
	if perHost, _ := connectionsFrom("127.0.0.1"); perHost != poolConnectionsPerHost {
		t.Errorf("a miner on this machine changed the count held for it to %d", perHost)
	}
	wantServerToServe(t, "a miner on this machine", miner, finished)
	if perHost, _ := connectionsFrom("127.0.0.1"); perHost != poolConnectionsPerHost {
		t.Errorf("after a miner on this machine left the count held for it is %d, want it untouched at %d", perHost, poolConnectionsPerHost)
	}
	poolConnections.Lock()
	delete(poolConnections.byHost, "127.0.0.1")
	poolConnections.Unlock()
	miner, finished = serveAMinerFrom("127.0.0.1")
	wantServerToServe(t, "a miner on this machine, with nothing held for it", miner, finished)
	if perHost, _ := connectionsFrom("127.0.0.1"); perHost != 0 {
		t.Errorf("a miner on this machine is counted: %d held for it", perHost)
	}
}

func TestAMinerElsewhereIsCountedWhileItIsConnectedAndReleasedWhenItLeaves(t *testing.T) {
	noPoolAndCleanLimits(t)
	const elsewhere = "203.0.113.7"
	miner, finished := serveAMinerFrom(elsewhere)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if perHost, _ := connectionsFrom(elsewhere); perHost == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if perHost, all := connectionsFrom(elsewhere); perHost != 1 || all != 1 {
		t.Errorf("while a miner elsewhere is connected %d places are held for it and %d in all; want 1 and 1", perHost, all)
	}
	wantServerToServe(t, "a miner elsewhere with places to spare", miner, finished)
	if perHost, all := connectionsFrom(elsewhere); perHost != 0 || all != 0 {
		t.Errorf("after the miner left %d places are held for it and %d in all; want 0 and 0", perHost, all)
	}
}

func TestAMinerElsewhereThatWasRefusedForBadAnswersStaysRefusedWhenNoPoolRuns(t *testing.T) {
	noPoolAndCleanLimits(t)
	const elsewhere = "203.0.113.7"
	poolStrikesAllowed = 3
	now := time.Now()
	for i := 1; i <= poolStrikesAllowed; i++ {
		refused := poolNoteBadWork(elsewhere, now)
		if refused != (i == poolStrikesAllowed) {
			t.Fatalf("after %d answers without the work the address is refused: %v", i, refused)
		}
	}

	miner, finished := serveAMinerFrom(elsewhere)
	wantServerToHangUp(t, "a miner elsewhere that was refused for a while", miner, finished)
	if perHost, all := connectionsFrom(elsewhere); perHost != 0 || all != 0 {
		t.Errorf("a refused connection holds %d places and %d are taken in all; want none", perHost, all)
	}

	// A miner on this machine is not refused for that, whatever is said of
	// another address.
	miner, finished = serveAMinerFrom("127.0.0.1")
	wantServerToServe(t, "a miner on this machine", miner, finished)
}

func TestAMinersAddressIsKeptByItsFirstSixtyFourBitsWhenItIsIPv6(t *testing.T) {
	for _, c := range []struct{ host, want string }{
		{"203.0.113.7", "203.0.113.7"},
		{"::ffff:203.0.113.7", "203.0.113.7"},
		{"2001:db8:1:2:aaaa::1", "2001:db8:1:2::/64"},
		{"2001:db8:1:2:bbbb::9", "2001:db8:1:2::/64"},
		{"2001:db8:1:3::1", "2001:db8:1:3::/64"},
		{"not-an-address", "not-an-address"},
		{"", ""},
	} {
		if got := minerAddressKey(c.host); got != c.want {
			t.Errorf("minerAddressKey(%q) is %q, want %q", c.host, got, c.want)
		}
	}
	if minerAddressKey("2001:db8:1:2:aaaa::1") != minerAddressKey("2001:db8:1:2:bbbb::9") {
		t.Error("two addresses that share their first 64 bits have different keys")
	}
	if minerAddressKey("2001:db8:1:2:aaaa::1") == minerAddressKey("2001:db8:1:3::1") {
		t.Error("two addresses that differ in their first 64 bits have the same key")
	}
}

func TestMinersOnAddressesThatShareSixtyFourBitsShareOneLimit(t *testing.T) {
	noPoolAndCleanLimits(t)
	key := minerAddressKey("2001:db8:1:2::1")
	poolConnections.Lock()
	poolConnections.byHost[key] = poolConnectionsPerHost
	poolConnections.all = poolConnectionsPerHost
	poolConnections.Unlock()

	// Another address with the same first 64 bits finds every place taken.
	miner, finished := serveAMinerFrom("2001:db8:1:2:ffff::7")
	wantServerToHangUp(t, "a miner on an address that shares its first 64 bits with one holding every place", miner, finished)
	if perHost, all := connectionsFrom(key); perHost != poolConnectionsPerHost || all != poolConnectionsPerHost {
		t.Errorf("after the refusal the shared address holds %d places and %d are taken in all; want %d and %d", perHost, all, poolConnectionsPerHost, poolConnectionsPerHost)
	}

	// An address with other first 64 bits is its own miner and is served.
	miner, finished = serveAMinerFrom("2001:db8:9:9::1")
	wantServerToServe(t, "a miner on an address with other first 64 bits", miner, finished)

	// A miner on this machine, written as IPv6, is served and not counted.
	miner, finished = serveAMinerFrom("::1")
	wantServerToServe(t, "a miner on this machine at ::1", miner, finished)
	if perHost, _ := connectionsFrom(minerAddressKey("::1")); perHost != 0 {
		t.Errorf("a miner at ::1 is counted: %d held for it", perHost)
	}
	if perHost, all := connectionsFrom(key); perHost != poolConnectionsPerHost || all != poolConnectionsPerHost {
		t.Errorf("the shared address holds %d places and %d are taken in all after the other miners; want %d and %d", perHost, all, poolConnectionsPerHost, poolConnectionsPerHost)
	}
}
