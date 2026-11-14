package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChosenMiner(t *testing.T) {
	t.Chdir(t.TempDir()) // no miner_config.json here
	t.Setenv("CVN_MINER", "")

	if choice, err := chosenMiner([]string{"cvn_node"}); err != nil || choice != MinerBuiltIn {
		t.Errorf("with nothing chosen the built-in miner must run: %q %v", choice, err)
	}
	accepted := map[string]string{
		"builtin": MinerBuiltIn, "built-in": MinerBuiltIn, "Builtin": MinerBuiltIn,
		"xmrig": MinerXMRig, "XMRig": MinerXMRig, "fast": MinerXMRig, "xmrig-cvn": MinerXMRig,
	}
	for text, want := range accepted {
		if choice, err := chosenMiner([]string{"cvn_node", "--miner", text}); err != nil || choice != want {
			t.Errorf("--miner %s gave %q, %v", text, choice, err)
		}
		if choice, err := chosenMiner([]string{"cvn_node", "--miner=" + text}); err != nil || choice != want {
			t.Errorf("--miner=%s gave %q, %v", text, choice, err)
		}
	}
	if _, err := chosenMiner([]string{"cvn_node", "--miner", "gpu"}); err == nil {
		t.Error("an unknown miner was accepted")
	}

	// The settings file is used when the command line says nothing, and the
	// command line wins when it does.
	if err := os.WriteFile("miner_config.json", []byte(`{"miner_address": "CVN_abc", "miner": "xmrig"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if choice, _ := chosenMiner([]string{"cvn_node"}); choice != MinerXMRig {
		t.Errorf("the choice in miner_config.json was not used: %q", choice)
	}
	if choice, _ := chosenMiner([]string{"cvn_node", "--miner", "builtin"}); choice != MinerBuiltIn {
		t.Errorf("the command line did not win over miner_config.json: %q", choice)
	}

	// A new wallet address must not wipe the miner choice from the file.
	if err := saveMinerAddress("CVN_new"); err != nil {
		t.Fatal(err)
	}
	if settings := readMinerConfig(); settings["miner_address"] != "CVN_new" || settings["miner"] != "xmrig" {
		t.Errorf("saving an address lost other settings: %v", settings)
	}
}

func TestFastMinerOutputIsUnderstood(t *testing.T) {
	speeds := map[string]float64{
		"[2026-11-14 17:38:28.569]  miner    speed 10s/60s/15m 17859.8 n/a n/a H/s max 17859.8 H/s":     17859.8,
		"[2026-11-14 17:40:28.569]  miner    speed 10s/60s/15m 18132.9 18001.5 n/a H/s max 18147.0 H/s": 18132.9,
		"[2026-11-14 17:40:28.569]  miner    speed 10s/60s/15m n/a 17500.0 n/a H/s max 18147.0 H/s":     17500.0,
	}
	for line, want := range speeds {
		if got, ok := parseFastMinerSpeed(line); !ok || got != want {
			t.Errorf("speed read as %v (%v), want %v: %s", got, ok, want, line)
		}
	}
	for _, line := range []string{
		"[2026-11-14 17:38:08.394]  randomx  dataset ready (1126 ms)",
		"[2026-11-14 17:38:28.569]  miner    speed 10s/60s/15m n/a n/a n/a H/s max n/a H/s",
		"",
	} {
		if _, ok := parseFastMinerSpeed(line); ok {
			t.Errorf("a speed was read from a line without one: %q", line)
		}
	}

	shown := map[string]string{
		" * HUGE PAGES   permission granted": "HUGE PAGES permission granted",
		"[2026-11-14 17:38:07.269]  randomx  allocated 2336 MB (2080+256) huge pages 100% 1168/1168 +JIT (0 ms)": "randomx allocated 2336 MB (2080+256) huge pages 100% 1168/1168 +JIT (0 ms)",
		"[2026-11-14 17:38:07.269]  msr      FAILED TO APPLY MSR MOD, HASHRATE WILL BE LOW":                      "msr FAILED TO APPLY MSR MOD, HASHRATE WILL BE LOW",
		"[2026-11-14 17:38:07.269]  net      127.0.0.1:18083 connect error: \"connection refused\"":              "net 127.0.0.1:18083 connect error: \"connection refused\"",
	}
	for line, want := range shown {
		if got, ok := fastMinerMessage(line); !ok || got != want {
			t.Errorf("%q was shown as %q (%v), want %q", line, got, ok, want)
		}
	}
	for _, line := range []string{
		" * CPU          AMD Ryzen 9 7950X3D 16-Core Processor (1) 64-bit AES VM",
		" * COMMANDS     'h' hashrate, 'p' pause, 'r' resume, 's' results, 'c' connection",
		"[2026-11-14 17:38:28.569]  miner    speed 10s/60s/15m 17859.8 n/a n/a H/s max 17859.8 H/s",
		"[2026-11-14 17:38:07.268]  cpu      use argon2 implementation AVX-512F",
	} {
		if message, ok := fastMinerMessage(line); ok {
			t.Errorf("a line of no interest reached the dashboard: %q", message)
		}
	}
}

func TestOnlyTheCVNEditionIsAccepted(t *testing.T) {
	if !isCVNEditionBanner("XMRig-CVN 6.26.0\n built on Nov 14 2026 with MSVC 2022\n") {
		t.Error("the CVN edition was not recognised")
	}
	for _, banner := range []string{"XMRig 6.26.0\n built on Mar 1 2026 with GCC 13.2.0\n", "", "something else entirely"} {
		if isCVNEditionBanner(banner) {
			t.Errorf("a program that is not the CVN edition was accepted: %q", banner)
		}
	}
	t.Chdir(t.TempDir())
	if _, err := locateFastMiner([]string{"cvn_node"}); err == nil || !strings.Contains(err.Error(), "cvn-xmrig/releases") {
		t.Errorf("a missing miner should be reported with where to get it: %v", err)
	}
	if _, err := locateFastMiner([]string{"cvn_node", "--xmrig-path", "nowhere"}); err == nil {
		t.Error("a path with no program at it was accepted")
	}
}

func TestFastMinerCommandLine(t *testing.T) {
	arguments := strings.Join(fastMinerArguments("127.0.0.1:18083", "CVN_abc", runtime.NumCPU()), " ")
	for _, expected := range []string{"-o 127.0.0.1:18083", "-u CVN_abc", "-a rx/0", "--donate-level 0", "--no-color"} {
		if !strings.Contains(arguments, expected) {
			t.Errorf("the fast miner's command line lacks %q: %s", expected, arguments)
		}
	}
	if strings.Contains(arguments, "-t ") {
		t.Errorf("with every thread in use no thread count should be passed: %s", arguments)
	}
	if runtime.NumCPU() > 1 {
		if limited := strings.Join(fastMinerArguments("127.0.0.1:18083", "CVN_abc", 1), " "); !strings.HasSuffix(limited, "-t 1") {
			t.Errorf("a thread limit was not passed on: %s", limited)
		}
		if got := miningThreads([]string{"cvn_node", "--threads", "1"}); got != 1 {
			t.Errorf("--threads 1 gave %d", got)
		}
	}
	for _, text := range []string{"0", "-3", "many", "999999"} {
		if got := miningThreads([]string{"cvn_node", "--threads", text}); got != runtime.NumCPU() {
			t.Errorf("--threads %s gave %d, want every thread (%d)", text, got, runtime.NumCPU())
		}
	}

	// With nothing asked for, the fast miner stays out of it.
	if _, ok := fastMinerHashrate(); ok {
		t.Error("a fast-miner speed was reported with no fast miner running")
	}
}

// TestFastMinerStandIn is not a test. Started as a separate program by the
// tests below, it plays the part of the fast miner without doing any mining.
func TestFastMinerStandIn(t *testing.T) {
	switch os.Getenv("CVN_TEST_STANDIN") {
	case "exit":
		os.Exit(3)
	case "idle":
		time.Sleep(60 * time.Second)
		os.Exit(0)
	case "connect":
		conn, err := net.Dial("tcp", os.Getenv("CVN_TEST_SERVER"))
		if err != nil {
			os.Exit(4)
		}
		fmt.Fprintln(conn, `{"id":1,"jsonrpc":"2.0","method":"login","params":{"login":"CVN_standin","pass":"x"}}`)
		if _, err := bufio.NewReader(conn).ReadString(10); err != nil { // 10 is the line ending
			os.Exit(5)
		}
		fmt.Println(" * HUGE PAGES   permission granted")
		fmt.Println("[2026-11-14 17:38:28.569]  miner    speed 10s/60s/15m 17500.0 n/a n/a H/s max 17500.0 H/s")
		time.Sleep(3 * time.Second)
		os.Exit(0)
	}
}

func standInArguments(t *testing.T, mode string) (string, []string) {
	t.Helper()
	t.Setenv("CVN_TEST_STANDIN", mode)
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	atomic.StoreInt32(&IsSyncingActive, 0)
	atomic.StoreInt32(&MiningHoldActive, 0)
	atomic.StoreInt32(&externalMinersConnected, 0)
	atomic.StoreInt32(&fastMinerActive, 1)
	t.Cleanup(func() { atomic.StoreInt32(&fastMinerActive, 0) })
	return program, []string{"-test.run=TestFastMinerStandIn"}
}

// The fast miner connects, the built-in miner is told to stay idle, and the
// speed the fast miner reports is what the dashboard shows.
func TestFastMinerIsRunAndItsSpeedIsShown(t *testing.T) {
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 32}})
	Params.HeartbeatInitialDifficulty = 32
	t.Setenv("CVN_TEST_SERVER", startWorkServerForTest(t))
	program, arguments := standInArguments(t, "connect")

	finished := make(chan error, 1)
	go func() { finished <- runFastMinerOnce(program, arguments) }()

	deadline := time.Now().Add(20 * time.Second)
	for {
		if speed, ok := fastMinerHashrate(); ok {
			if speed != 17500 {
				t.Errorf("the speed shown is %v, want 17500", speed)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fast miner never connected or never reported a speed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if atomic.LoadInt32(&externalMinersConnected) != 1 {
		t.Error("the node does not count the fast miner as connected")
	}
	select {
	case <-finished:
	case <-time.After(20 * time.Second):
		t.Fatal("the node did not notice the fast miner stopping")
	}
	for wait := 0; atomic.LoadInt32(&externalMinersConnected) != 0; wait++ {
		if wait > 100 {
			t.Fatal("a stopped miner is still counted as connected")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, ok := fastMinerHashrate(); ok {
		t.Error("a speed is still shown after the fast miner stopped")
	}
}

// A miner that runs but never connects is stopped, not left to idle.
func TestFastMinerThatNeverConnectsIsStopped(t *testing.T) {
	saved := fastMinerConnectDeadline
	fastMinerConnectDeadline = 2 * time.Second
	t.Cleanup(func() { fastMinerConnectDeadline = saved })
	program, arguments := standInArguments(t, "idle")

	started := time.Now()
	if err := runFastMinerOnce(program, arguments); err == nil {
		t.Error("a miner that never connected was reported as fine")
	}
	if took := time.Since(started); took > 20*time.Second {
		t.Errorf("it took %v to stop a miner that never connected", took)
	}
}

// A miner that keeps failing is given up on, and the built-in miner is
// released to do the work.
func TestBuiltInMinerTakesOverWhenTheFastMinerKeepsFailing(t *testing.T) {
	saved := fastMinerRestartPause
	fastMinerRestartPause = 50 * time.Millisecond
	t.Cleanup(func() { fastMinerRestartPause = saved })
	program, arguments := standInArguments(t, "exit")

	done := make(chan struct{})
	go func() { superviseFastMiner(program, arguments); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("a miner that fails every time was restarted for ever")
	}
	if atomic.LoadInt32(&fastMinerActive) != 0 {
		t.Error("the built-in miner was not released after the fast miner failed")
	}
}
