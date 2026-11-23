package node

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// A pool's address as people write it, and things that are not one.
func TestAPoolsAddressIsReadStrictly(t *testing.T) {
	for given, want := range map[string]string{
		"pool.example.org":                    "pool.example.org:28083",
		" pool.example.org ":                  "pool.example.org:28083",
		"pool.example.org:3333":               "pool.example.org:3333",
		"203.0.113.5":                         "203.0.113.5:28083",
		"203.0.113.5:3333":                    "203.0.113.5:3333",
		"localhost":                           "localhost:28083",
		"stratum+tcp://pool.example.org:3333": "pool.example.org:3333",
		"STRATUM+TCP://pool.example.org":      "pool.example.org:28083",
		"2001:db8::1":                         "[2001:db8::1]:28083",
		"[2001:db8::1]":                       "[2001:db8::1]:28083",
		"[2001:db8::1]:3333":                  "[2001:db8::1]:3333",
	} {
		got, err := poolMineTarget(given, 28083)
		if err != nil || got != want {
			t.Errorf("%q was read as %q (%v), want %q", given, got, err, want)
		}
	}
	for _, given := range []string{
		"", " ", ".", "http://pool.example.org", "https://pool.example.org/pool", "pool.example.org/pool",
		"pool example.org", "pool_example.org", "a..b", "-pool.example.org", "pool-.example.org",
		"-o", "--config=evil.json", "pool.example.org:0", "pool.example.org:65536", "pool.example.org:abc",
		"pool.example.org:+80", "pool.example.org:", "pool.example.org:3333:44", "user@pool.example.org",
		"pool.example.org;rm", "pool.example.org\n--threads", "[2001:db8::1", strings.Repeat("a", 64) + ".org",
	} {
		if got, err := poolMineTarget(given, 28083); err == nil {
			t.Errorf("%q was taken for a pool's address: %q", given, got)
		}
	}
}

func withWorkPort(t *testing.T, port int) {
	t.Helper()
	saved := Params.StratumPort
	Params.StratumPort = port
	t.Cleanup(func() { Params.StratumPort = saved })
}

// Where to mine is asked on the command line or in miner_config.json, and
// the command line wins.
func TestMiningAtAPoolIsAskedForInTwoWays(t *testing.T) {
	t.Chdir(t.TempDir())
	withWorkPort(t, 28083)

	if target, from, err := poolMineRequested([]string{"cvn_node", "--threads", "4"}); target != "" || from != "" || err != nil {
		t.Fatalf("nothing was asked and the answer was %q, %q, %v", target, from, err)
	}
	for _, args := range [][]string{
		{"cvn_node", "--pool-mine", "pool.example.org"},
		{"cvn_node", "--network", "devnet", "--pool-mine=pool.example.org", "--threads", "4"},
	} {
		target, from, err := poolMineRequested(args)
		if err != nil || target != "pool.example.org:28083" || from != "--pool-mine" {
			t.Errorf("%v gave %q, %q, %v", args, target, from, err)
		}
	}
	// The option with no address after it is a mistake, not a wish to mine alone.
	for _, args := range [][]string{
		{"cvn_node", "--pool-mine"},
		{"cvn_node", "--pool-mine", "--threads", "4"},
		{"cvn_node", "--pool-mine="},
		{"cvn_node", "--pool-mine", "http://pool.example.org"},
	} {
		if target, from, err := poolMineRequested(args); err == nil || from == "" {
			t.Errorf("%v gave %q, %q with no complaint", args, target, from)
		}
	}

	if err := os.WriteFile("miner_config.json", []byte(`{"miner_address": "CVN_x", "mine_at_pool": "pool.example.org:3333"}`), 0644); err != nil {
		t.Fatal(err)
	}
	target, from, err := poolMineRequested([]string{"cvn_node"})
	if err != nil || target != "pool.example.org:3333" || !strings.Contains(from, "miner_config.json") {
		t.Errorf("the settings file gave %q, %q, %v", target, from, err)
	}
	target, from, err = poolMineRequested([]string{"cvn_node", "--pool-mine", "other.example.org"})
	if err != nil || target != "other.example.org:28083" || from != "--pool-mine" {
		t.Errorf("the command line did not win over the settings file: %q, %q, %v", target, from, err)
	}

	// A network that is not mined with RandomX has no pools.
	Params.StratumPort = 0
	if _, _, err := poolMineRequested([]string{"cvn_node", "--pool-mine", "pool.example.org"}); err == nil {
		t.Error("a pool was accepted on a network with no work port")
	}
}

// What the fast miner prints while it works for a pool, and what is made of
// it. The lines are written the way the fast miner writes them.
func TestWhatTheFastMinerSaysAtAPoolIsUnderstood(t *testing.T) {
	state := &poolMinerState{}
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	say := func(line string) (string, bool) {
		now = now.Add(time.Second)
		return state.read(line, now)
	}
	quiet := func(line string) {
		t.Helper()
		if message, shown := say(line); shown {
			t.Errorf("%q was shown as %q and should not be", line, message)
		}
	}
	shown := func(line, want string) {
		t.Helper()
		if message, ok := say(line); !ok || !strings.Contains(message, want) {
			t.Errorf("%q gave %q (shown: %v), want something with %q", line, message, ok, want)
		}
	}

	if report := poolMinerReport(state, nil, now); !strings.Contains(report, "Not connected") {
		t.Errorf("before anything was heard the report says: %s", report)
	}
	shown("[2026-11-24 10:00:00.100]  net      use pool pool.example.org:28083  203.0.113.5", "Connected to the pool")
	quiet("[2026-11-24 10:00:00.200]  net      new job from pool.example.org:28083 diff 50000 algo rx/0 height 1200")
	quiet("[2026-11-24 10:00:10.000]  miner    speed 10s/60s/15m 17500.0 n/a n/a H/s max 17500.0 H/s")
	shown("[2026-11-24 10:00:20.000]  cpu      accepted (1/0) diff 50000 (41 ms)", "first share")
	quiet("[2026-11-24 10:00:50.000]  cpu      accepted (2/0) diff 50000 (40 ms)")
	shown(`[2026-11-24 10:01:20.000]  cpu      rejected (2/1) diff 50000 "that answer was sent before" (40 ms)`, "that answer was sent before")
	// The same complaint again within two minutes is counted and not printed.
	quiet(`[2026-11-24 10:01:25.000]  cpu      rejected (2/2) diff 50000 "that answer was sent before" (40 ms)`)

	// An answer that came after the chain moved on is counted and not
	// complained of: it happens to every miner whenever a block is found.
	quiet(`[2026-11-24 10:01:30.000]  cpu      rejected (2/3) diff 50000 "stale job: the chain has moved on" (40 ms)`)

	speed, speedKnown, connected, taken, late, refused := state.figures(now)
	if speed != 17500 || !speedKnown || !connected || taken != 2 || late != 1 || refused != 2 {
		t.Errorf("figures: speed %v (known %v), connected %v, taken %d, too late %d, refused %d", speed, speedKnown, connected, taken, late, refused)
	}
	account := &poolPageAddress{OwedUnits: 125000000, PendingUnits: 50000000, PaidUnits: 300000000}
	report := poolMinerReport(state, account, now)
	for _, want := range []string{"17.50 kH/s", "shares taken 2", "too late 1", "refused 2", "owes this address 1.25 CVN", "0.5 CVN on its way", "paid 3 CVN"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report %q does not say %q", report, want)
		}
	}

	// A pool that cannot be reached is said once, not every few seconds.
	shown(`[2026-11-24 10:02:00.000]  net      pool.example.org:28083 connect error: "connection refused"`, "connect error")
	quiet(`[2026-11-24 10:02:05.000]  net      pool.example.org:28083 connect error: "connection refused"`)
	quiet(`[2026-11-24 10:02:10.000]  net      pool.example.org:28083 connect error: "connection refused"`)
	if report := poolMinerReport(state, nil, now); !strings.Contains(report, "Not connected") {
		t.Errorf("with the pool out of reach the report says: %s", report)
	}
	now = now.Add(3 * time.Minute)
	shown(`[2026-11-24 10:05:10.000]  net      pool.example.org:28083 connect error: "connection refused"`, "connect error")

	// A speed that has not been heard for a while is not shown as today's.
	shown("[2026-11-24 10:06:00.100]  net      use pool pool.example.org:28083  203.0.113.5", "Connected")
	if report := poolMinerReport(state, nil, now.Add(time.Second)); strings.Contains(report, "kH/s") || !strings.Contains(report, "Connected") {
		t.Errorf("a speed three minutes old was reported: %s", report)
	}

	// What a pool sends is printed as plain text, whatever is in it.
	message, _ := say("[2026-11-24 10:07:00.000]  cpu      rejected (2/3) diff 50000 \"gone \x1b[2J\x1b[H\a wrong\" (40 ms)")
	if strings.ContainsAny(message, "\x1b\a") || !strings.Contains(message, "gone") || !strings.Contains(message, "wrong") {
		t.Errorf("a refusal with screen commands in it was shown as %q", message)
	}
	if plain := shownPlainly(strings.Repeat("x", 500), 40); len(plain) > 43 {
		t.Errorf("a long text was not cut short: %d characters", len(plain))
	}
}

// TestPoolMinerStandIn is not a test. Started as a separate program by the
// tests below, it plays the part of the fast miner at a pool: it logs in
// with the address it was given and prints what the fast miner prints.
func TestPoolMinerStandIn(t *testing.T) {
	switch os.Getenv("CVN_TEST_POOL_STANDIN") {
	case "exit":
		os.Exit(3)
	case "mine":
		server, address := "", ""
		for i, arg := range os.Args {
			if arg == "-o" && i+1 < len(os.Args) {
				server = os.Args[i+1]
			}
			if arg == "-u" && i+1 < len(os.Args) {
				address = os.Args[i+1]
			}
		}
		conn, err := net.Dial("tcp", server)
		if err != nil {
			fmt.Printf("[2026-11-24 10:00:00.000]  net      %s connect error: \"connection refused\"\n", server)
			os.Exit(4)
		}
		fmt.Fprintln(conn, `{"id":1,"jsonrpc":"2.0","method":"login","params":{"login":"`+address+`","pass":"x"}}`)
		reply, err := bufio.NewReader(conn).ReadString(10) // 10 is the line ending
		if err != nil || !strings.Contains(reply, `"status":"OK"`) {
			os.Exit(5)
		}
		fmt.Printf("[2026-11-24 10:00:00.100]  net      use pool %s  127.0.0.1\n", server)
		fmt.Println("[2026-11-24 10:00:10.000]  miner    speed 10s/60s/15m 17500.0 n/a n/a H/s max 17500.0 H/s")
		fmt.Println("[2026-11-24 10:00:20.000]  cpu      accepted (1/0) diff 4 (1 ms)")
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}
}

// The command line this program gives the fast miner for a pool, handed to
// a stand-in that logs in at a real pool with it.
func TestTheFastMinerIsRunAtAPool(t *testing.T) {
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 64}})
	Params.HeartbeatInitialDifficulty = 64
	server, _ := startPoolForTest(t, 4)
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CVN_TEST_POOL_STANDIN", "mine")
	miner := testAddress("a miner at the pool")
	// What follows the test's own option is what the fast miner is given.
	arguments := append([]string{"-test.run=TestPoolMinerStandIn", "--"}, fastMinerArguments(server, miner, 2)...)

	state := &poolMinerState{}
	if err := runPoolMinerOnce(program, arguments, state); err != nil {
		t.Fatalf("the stand-in miner did not log in at the pool and end cleanly: %v", err)
	}
	speed, _, connected, taken, _, refused := state.figures(time.Now())
	if speed != 17500 || !connected || taken != 1 || refused != 0 {
		t.Errorf("after the run: speed %v, connected %v, taken %d, refused %d", speed, connected, taken, refused)
	}
}

// A fast miner that ends at once, time after time, is given up on, and the
// reason is handed back to be shown.
func TestAFastMinerThatWillNotStartAtAPoolIsGivenUpOn(t *testing.T) {
	savedPause := poolMinerRestartPause
	poolMinerRestartPause = 20 * time.Millisecond
	t.Cleanup(func() { poolMinerRestartPause = savedPause })
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CVN_TEST_POOL_STANDIN", "exit")

	done := make(chan string, 1)
	go func() {
		done <- supervisePoolMiner(program, []string{"-test.run=TestPoolMinerStandIn"}, &poolMinerState{})
	}()
	select {
	case reason := <-done:
		if !strings.Contains(reason, "3") {
			t.Errorf("the reason given was %q, and the miner ended with code 3", reason)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("a miner that ends at once every time was started again for ever")
	}
}

// What a pool says of itself and of one address is read from its page.
func TestAMinerReadsThePoolsPage(t *testing.T) {
	pool := startPayingPool(t)
	pages := http.NewServeMux()
	addPoolPages(pages)
	server := httptest.NewServer(pages)
	t.Cleanup(server.Close)

	var facts poolFacts
	if err := poolPageRead(server.URL+"/pool.json", &facts); err != nil {
		t.Fatal(err)
	}
	if facts.Network != Params.Name || facts.PoolAddress != pool.address || facts.FeePercent != 0 || facts.BlocksFound < 1 || facts.MinimumPayment == "" {
		t.Errorf("the pool's facts were read as %+v", facts)
	}
	if problem := poolFactsProblem(facts); problem != "" {
		t.Errorf("a pool of this network was refused: %s", problem)
	}
	other := facts
	other.Network = "some-other-network"
	if problem := poolFactsProblem(other); !strings.Contains(problem, "some-other-network") {
		t.Errorf("a pool of another network was not refused: %q", problem)
	}

	account := poolAccountAt(server.URL, pool.alice)
	if account == nil || account.OwedUnits != cvn("37.5") || account.PaidUnits != 0 {
		t.Fatalf("Alice's account at the pool was read as %+v", account)
	}
	if poolAccountAt(server.URL, "not an address") != nil {
		t.Error("an account was read for something that is not an address")
	}
	// A page that is not there gives nothing, and no complaint.
	if poolAccountAt("http://127.0.0.1:1", pool.alice) != nil {
		t.Error("an account was read from a pool that is not there")
	}
}

// Mining at a pool needs an address to be paid at and the fast miner to do
// the work. Without either the program says so and ends; it never falls back
// to mining with the built-in miner, which cannot work for a pool.
func TestMiningAtAPoolNeedsAnAddressAndTheFastMiner(t *testing.T) {
	folder := t.TempDir()
	t.Chdir(folder)
	withWorkPort(t, 28083)
	t.Setenv("CVN_MINER", "")
	saved := programFolder
	programFolder = func() string { return folder }
	t.Cleanup(func() { programFolder = saved })

	t.Setenv("CVN_MINER_ADDRESS", "")
	if code := RunPoolMiner("127.0.0.1:1", "--pool-mine", []string{"cvn_node", "--pool-mine", "127.0.0.1:1"}); code != 1 {
		t.Errorf("with no mining address the program ended with %d, want 1", code)
	}
	t.Setenv("CVN_MINER_ADDRESS", testAddress("a miner at the pool"))
	if code := RunPoolMiner("127.0.0.1:1", "--pool-mine", []string{"cvn_node", "--pool-mine", "127.0.0.1:1"}); code != 1 {
		t.Errorf("with no fast miner the program ended with %d, want 1", code)
	}
}
