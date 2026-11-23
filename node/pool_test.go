package node

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// startPoolForTest turns the node under test into a pool whose miners are
// asked for `shareDifficulty`, and returns where they connect and where the
// pool's record is kept. The chain must have been started first, so that it
// is the last thing taken down.
func startPoolForTest(t *testing.T, shareDifficulty int) (server, recordPath string) {
	t.Helper()
	savedStart, savedMin, savedShares, savedAfter := poolStartDifficulty, poolMinShareDifficulty, poolRetargetShares, poolRetargetAfter
	poolStartDifficulty, poolMinShareDifficulty = shareDifficulty, 1
	// The difficulty stays where the test put it.
	poolRetargetShares, poolRetargetAfter = 1000000, time.Hour
	poolAddress = testAddress("pool")
	recordPath = filepath.Join(t.TempDir(), "pool.db")
	record, err := openPoolLedger(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	poolLedger = record
	atomic.StoreInt32(&externalMinersConnected, 0)
	// Registered before the work server's own clean-up, so it runs after it:
	// nothing is still being done for a miner when the pool is put away.
	t.Cleanup(func() {
		poolLedger.close()
		poolLedger = nil
		poolAddress = ""
		poolStartDifficulty, poolMinShareDifficulty, poolRetargetShares, poolRetargetAfter = savedStart, savedMin, savedShares, savedAfter
		poolPageKept.Lock()
		poolPageKept.page, poolPageKept.at = poolPage{}, time.Time{}
		poolPageKept.Unlock()
	})
	return startWorkServerForTest(t), recordPath
}

// poolLogin logs a stand-in miner in and returns its first work.
func poolLogin(t *testing.T, miner *fakeMiner, address string) minerJob {
	t.Helper()
	result, errorText, _ := miner.call("login", map[string]string{"login": address, "pass": "x", "agent": "test"})
	if errorText != "" {
		t.Fatalf("login refused: %s", errorText)
	}
	var login struct {
		Status string   `json:"status"`
		Job    minerJob `json:"job"`
	}
	if err := json.Unmarshal(result, &login); err != nil || login.Status != "OK" {
		t.Fatalf("unexpected login reply: %s", result)
	}
	return login.Job
}

// searchWork tries nonces from `from` upward and returns the first whose
// work `wanted` accepts, with the nonce after it.
func (m *fakeMiner) searchWork(job minerJob, from uint32, wanted func(work string) bool) (nonceHex string, next uint32) {
	m.t.Helper()
	blob, err := hex.DecodeString(job.Blob)
	if err != nil || len(blob) != powBlobSize {
		m.t.Fatalf("the blob should be %d bytes, got %q", powBlobSize, job.Blob)
	}
	key, _ := hex.DecodeString(job.SeedHash)
	for nonce := from; nonce < from+200000; nonce++ {
		blob[39], blob[40], blob[41], blob[42] = byte(nonce), byte(nonce>>8), byte(nonce>>16), byte(nonce>>24)
		work, ok := randomXVerify(key, blob)
		if !ok {
			m.t.Fatal("RandomX failed")
		}
		if wanted(work) {
			return hex.EncodeToString(blob[39:43]), nonce + 1
		}
	}
	m.t.Fatal("no suitable nonce found")
	return "", 0
}

func meets(difficulty int) func(string) bool {
	return func(work string) bool { return work <= HeartbeatTargetHex(difficulty) }
}

// A pool on a chain whose blocks take 64 hashes, asking its miners for 4.
func TestPoolCountsSharesAndDividesABlock(t *testing.T) {
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 64}})
	Params.HeartbeatInitialDifficulty = 64
	server, recordPath := startPoolForTest(t, 4)
	alice, bob := testAddress("alice"), testAddress("bob")

	first := connectFakeMiner(t, server)
	aliceJob := poolLogin(t, first, alice+".desk")
	second := connectFakeMiner(t, server)
	bobJob := poolLogin(t, second, bob)

	if aliceJob.Blob == bobJob.Blob {
		t.Fatal("two miners were given the same block to work on")
	}
	if aliceJob.Target != stratumTargetHex(4) || bobJob.Target != stratumTargetHex(4) {
		t.Fatalf("miners should be asked for difficulty 4, not the network's: %s %s", aliceJob.Target, bobJob.Target)
	}
	if atomic.LoadInt32(&externalMinersConnected) != 2 {
		t.Errorf("the pool counts %d miners, want 2", atomic.LoadInt32(&externalMinersConnected))
	}

	// A share is an answer that meets the miner's difficulty. These three
	// are far from the network's, so they are shares and nothing more.
	shareOnly := func(work string) bool { return meets(4)(work) && !meets(64)(work) }
	var nonce string
	var from uint32
	for i := 0; i < 3; i++ {
		nonce, from = first.searchWork(aliceJob, from, shareOnly)
		if result, errorText, _ := first.call("submit", map[string]string{"job_id": aliceJob.JobID, "nonce": nonce}); errorText != "" || !strings.Contains(string(result), "OK") {
			t.Fatalf("share %d was refused: %s %s", i+1, errorText, result)
		}
	}
	if GetLatestBlockInternal().Index != 0 {
		t.Fatal("a share that was not a block changed the chain")
	}
	// The same answer twice counts once.
	if _, errorText, _ := first.call("submit", map[string]string{"job_id": aliceJob.JobID, "nonce": nonce}); errorText != poolDuplicateShare {
		t.Errorf("a share handed in twice got %q", errorText)
	}
	// An answer without the work counts for nothing.
	tooLittle, _ := first.searchWork(aliceJob, from, func(work string) bool { return !meets(4)(work) })
	if _, errorText, _ := first.call("submit", map[string]string{"job_id": aliceJob.JobID, "nonce": tooLittle}); errorText != stratumBadWork {
		t.Errorf("an answer without the work got %q", errorText)
	}
	// While the pool is behind the network it takes no shares in.
	held, _ := first.searchWork(aliceJob, from, shareOnly)
	atomic.StoreInt32(&MiningHoldActive, 1)
	_, errorText, _ := first.call("submit", map[string]string{"job_id": aliceJob.JobID, "nonce": held})
	atomic.StoreInt32(&MiningHoldActive, 0)
	if !strings.Contains(errorText, "waiting for the network") {
		t.Errorf("a share handed to a pool that is behind got %q", errorText)
	}
	view := poolLedger.view(64, time.Now().Unix(), 10)
	if view.Shares != 3 || len(view.Addresses) != 1 || view.Addresses[0].Address != alice || view.Addresses[0].Work != 12 {
		t.Fatalf("after three shares at difficulty 4 the record holds %+v", view)
	}

	// Bob's answer meets the network's difficulty: it is a share and a block.
	blockNonce, _ := second.searchWork(bobJob, 0, meets(64))
	if result, errorText, _ := second.call("submit", map[string]string{"job_id": bobJob.JobID, "nonce": blockNonce}); errorText != "" || !strings.Contains(string(result), "OK") {
		t.Fatalf("the block was refused: %s %s", errorText, result)
	}
	tip := GetLatestBlockInternal()
	if tip.Index != 1 || tip.Transactions[0].Outputs[0].Recipient != testAddress("pool") {
		t.Fatalf("block 1 should exist and pay the pool: height %d", tip.Index)
	}
	if balanceOf(testAddress("pool")) != cvn("50") || balanceOf(bob) != 0 {
		t.Errorf("the pool holds %s and Bob %s; the pool should hold the 50", FormatUnits(balanceOf(testAddress("pool"))), FormatUnits(balanceOf(bob)))
	}

	// The reward is divided by work: Alice did 12, Bob 4.
	view = poolLedger.view(64, time.Now().Unix(), 10)
	if view.BlocksFound != 1 || len(view.Blocks) != 1 {
		t.Fatalf("the record holds %d blocks, want 1", view.BlocksFound)
	}
	found := view.Blocks[0]
	if found.Height != 1 || found.Hash != tip.Hash || found.Finder != bob || found.State != poolBlockWaiting || found.RewardUnits != cvn("50") {
		t.Fatalf("the block is recorded as %+v", found)
	}
	parts := map[string]int64{}
	var total int64
	for _, part := range found.Parts {
		parts[part.Address] = part.Units
		total += part.Units
	}
	if total != cvn("50") || parts[alice] != cvn("37.5") || parts[bob] != cvn("12.5") {
		t.Errorf("50 CVN was divided as Alice %s, Bob %s (total %s); want 37.5 and 12.5",
			FormatUnits(parts[alice]), FormatUnits(parts[bob]), FormatUnits(total))
	}

	// An answer to the old block is stale now, and is not a share.
	stale, _ := first.searchWork(aliceJob, from, shareOnly)
	if _, errorText, _ := first.call("submit", map[string]string{"job_id": aliceJob.JobID, "nonce": stale}); !strings.Contains(errorText, "stale") {
		t.Errorf("an answer to a block already found got %q", errorText)
	}

	// The pool's pages show the same.
	pages := http.NewServeMux()
	addPoolPages(pages)
	reply := httptest.NewRecorder()
	pages.ServeHTTP(reply, httptest.NewRequest("GET", "/pool.json", nil))
	var page poolPage
	if err := json.Unmarshal(reply.Body.Bytes(), &page); err != nil || reply.Code != 200 {
		t.Fatalf("pool.json: %d %v", reply.Code, err)
	}
	if page.PoolAddress != testAddress("pool") || page.BlocksFound != 1 || page.Miners != 2 || page.Height != 1 || page.FeePercent != 0 ||
		len(page.Blocks) != 1 || len(page.Blocks[0].Parts) != 2 || page.Blocks[0].Parts[0].Amount != "37.5" {
		t.Errorf("pool.json shows %+v", page)
	}
	reply = httptest.NewRecorder()
	pages.ServeHTTP(reply, httptest.NewRequest("GET", "/pool", nil))
	if body := reply.Body.String(); reply.Code != 200 || !strings.Contains(body, testAddress("pool")) || !strings.Contains(body, alice) || strings.Contains(body, "<script") {
		t.Errorf("the pool page: status %d, %d bytes", reply.Code, len(body))
	}

	// What was recorded is still there when the record is opened again.
	first.conn.Close()
	second.conn.Close()
	for i := 0; i < 200 && atomic.LoadInt32(&externalMinersConnected) != 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	poolLedger.close()
	again, err := openPoolLedger(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	poolLedger = again
	view = again.view(64, time.Now().Unix(), 10)
	if view.Shares != 4 || view.BlocksFound != 1 || view.Blocks[0].Hash != tip.Hash || len(view.Blocks[0].Parts) != 2 {
		t.Errorf("after opening the record again it holds %d shares and %d blocks", view.Shares, view.BlocksFound)
	}
}

func TestANodeThatIsNotAPoolHasNoPoolPage(t *testing.T) {
	pages := http.NewServeMux()
	addPoolPages(pages)
	for _, path := range []string{"/pool", "/pool.json"} {
		reply := httptest.NewRecorder()
		pages.ServeHTTP(reply, httptest.NewRequest("GET", path, nil))
		if reply.Code != http.StatusNotFound {
			t.Errorf("%s answered %d on a node that is not a pool", path, reply.Code)
		}
	}
}

func sharesOf(weights ...interface{}) []poolShare {
	var shares []poolShare
	for i := 0; i+1 < len(weights); i += 2 {
		shares = append(shares, poolShare{Seq: uint64(len(shares)), Address: weights[i].(string), Weight: int64(weights[i+1].(int))})
	}
	return shares
}

func partsByAddress(parts []poolPart) (map[string]poolPart, int64) {
	byAddress := map[string]poolPart{}
	var total int64
	for _, part := range parts {
		byAddress[part.Address] = part
		total += part.Units
	}
	return byAddress, total
}

func TestPoolSplitAddsUpToTheReward(t *testing.T) {
	// Oldest first. The window of 100 reaches back through the last four
	// shares and half of the one before them.
	shares := sharesOf("old", 1000, "a", 40, "b", 30, "a", 20, "c", 10, "b", 20)
	parts, windowWork := poolSplit(shares, 5, 100, 1000)
	byAddress, total := partsByAddress(parts)
	if windowWork != 100 || total != 1000 {
		t.Fatalf("window %d, total %d", windowWork, total)
	}
	// b 20+30, c 10, a 20, and the first 20 of a's 40.
	if byAddress["b"].Work != 50 || byAddress["a"].Work != 40 || byAddress["c"].Work != 10 || byAddress["old"].Work != 0 {
		t.Errorf("work counted: %+v", byAddress)
	}
	if byAddress["b"].Units != 500 || byAddress["a"].Units != 400 || byAddress["c"].Units != 100 {
		t.Errorf("units: %+v", byAddress)
	}
	if _, reached := byAddress["old"]; reached {
		t.Error("a share outside the window was given a part")
	}

	// Shares after the winning one do not count.
	parts, _ = poolSplit(shares, 3, 1000000, 90)
	byAddress, total = partsByAddress(parts)
	if total != 90 || byAddress["c"].Work != 0 || byAddress["b"].Work != 30 || byAddress["old"].Work != 1000 {
		t.Errorf("counting back from share 3: %+v", byAddress)
	}

	// Units that do not divide go to the address with the most work, and
	// between equals to the address that sorts first. Nothing is lost.
	parts, _ = poolSplit(sharesOf("x", 1, "y", 1, "z", 1), 2, 1000, 100)
	byAddress, total = partsByAddress(parts)
	if total != 100 || byAddress["x"].Units != 34 || byAddress["y"].Units != 33 || byAddress["z"].Units != 33 {
		t.Errorf("100 units among three equals: %+v", byAddress)
	}

	// Amounts as large as they get: a whole reward against a large window.
	big := sharesOf("p", 2000000000, "q", 3000000000, "r", 1)
	parts, _ = poolSplit(big, 2, 9000000000000, cvn("50")+7)
	_, total = partsByAddress(parts)
	if total != cvn("50")+7 {
		t.Errorf("large amounts add up to %d, want %d", total, cvn("50")+7)
	}

	// One miner alone gets all of it; no shares, no parts.
	if parts, _ = poolSplit(sharesOf("solo", 7), 0, 50, 12345); len(parts) != 1 || parts[0].Units != 12345 {
		t.Errorf("one miner alone: %+v", parts)
	}
	if parts, work := poolSplit(nil, 0, 50, 12345); len(parts) != 0 || work != 0 {
		t.Errorf("no shares: %+v", parts)
	}
}

func TestPoolDifficultyFollowsTheMiner(t *testing.T) {
	savedMin := poolMinShareDifficulty
	poolMinShareDifficulty = 2500
	defer func() { poolMinShareDifficulty = savedMin }()

	// 10,000 hashes a second should be asked for 300,000: one share in 30 s.
	// From 50,000 that is more than four times, so it takes two steps.
	next := poolNextShareDifficulty(50000, 6*50000, 6, 30, 0)
	if next != 200000 {
		t.Errorf("a fast miner went from 50000 to %d, want 200000 (four times at most)", next)
	}
	if next = poolNextShareDifficulty(200000, 6*200000, 6, 120, 0); next != 300000 {
		t.Errorf("then to %d, want 300000", next)
	}
	// Once there, a little luck either way changes nothing.
	for _, shares := range []int64{5, 6, 7} {
		if next = poolNextShareDifficulty(300000, shares*300000, int(shares), 180, 0); next != 300000 {
			t.Errorf("%d shares in 180 s moved a settled miner to %d", shares, next)
		}
	}
	// A slow machine: 200 hashes a second, one share in two minutes.
	if next = poolNextShareDifficulty(50000, 50000, 1, 250, 0); next != 12500 {
		t.Errorf("a slow miner went to %d, want 12500 (a quarter at most)", next)
	}
	// Nothing at all in two minutes: easier, but never below the least.
	if next = poolNextShareDifficulty(50000, 0, 0, 120, 0); next != 12500 {
		t.Errorf("a silent miner went to %d, want 12500", next)
	}
	if next = poolNextShareDifficulty(4000, 0, 0, 600, 0); next != 2500 {
		t.Errorf("a silent miner at 4000 went to %d, want the least, 2500", next)
	}
	// Never more than the network itself asks.
	if next = poolNextShareDifficulty(50000, 6*50000, 6, 30, 80000); next != 80000 {
		t.Errorf("with the network at 80000 the miner went to %d", next)
	}

	// The same through a miner's own record.
	var miner poolMiner
	start := time.Unix(1000, 0)
	savedStart, savedShares := poolStartDifficulty, poolRetargetShares
	poolStartDifficulty, poolRetargetShares = 50000, 6
	defer func() { poolStartDifficulty, poolRetargetShares = savedStart, savedShares }()
	miner.begin(start)
	for i := 1; i <= 6; i++ {
		miner.noteShare(50000, start.Add(time.Duration(i)*5*time.Second), 0)
	}
	if miner.asked() != 200000 || !miner.takeChanged() || miner.takeChanged() {
		t.Errorf("after six quick shares the miner is asked for %d", miner.asked())
	}
	miner.easeIfQuiet(start.Add(30*time.Second+poolRetargetAfter), 0)
	if miner.asked() != 50000 {
		t.Errorf("after two silent minutes the miner is asked for %d, want 50000", miner.asked())
	}
}

func TestPoolRecordForgetsSharesNoBlockCouldReach(t *testing.T) {
	onNetwork(t, "devnet")
	path := filepath.Join(t.TempDir(), "pool.db")
	record, err := openPoolLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	// 1,500 shares of weight 10 on a network at difficulty 100: a window is
	// 200 of work, and four windows are kept, which is 80 shares. The first
	// thousand are an hour old.
	for i := 0; i < 1500; i++ {
		at := now
		if i < 1000 {
			at = now - 3600
		}
		if _, err := record.addShare(testAddress("m"), 10, at, 100); err != nil {
			t.Fatal(err)
		}
	}
	record.Lock()
	held := len(record.shares)
	oldest := record.shares[0].Seq
	record.Unlock()
	// At the 1,500th share the 500 from the last ten minutes are all kept,
	// and nothing older is needed.
	if held != 500 || oldest != 1000 {
		t.Errorf("the record holds %d shares from number %d; want the 500 recent ones, from 1000", held, oldest)
	}
	record.close()
	again, err := openPoolLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.close()
	if len(again.shares) != held || again.nextSeq != 1500 {
		t.Errorf("opened again: %d shares, next number %d", len(again.shares), again.nextSeq)
	}
	if _, err := again.addShare(testAddress("m"), 0, now, 100); err == nil {
		t.Error("a share with no work was recorded")
	}
}

func TestPoolOptionIsReadFromTheCommandLine(t *testing.T) {
	onNetwork(t, "devnet")
	good := testAddress("pool")
	if address, on, err := poolAddressRequested([]string{"cvn_node", "--network", "devnet", "--pool", good}); err != nil || !on || address != good {
		t.Errorf("--pool with an address: %q %v %v", address, on, err)
	}
	if _, on, err := poolAddressRequested([]string{"cvn_node", "--network", "devnet"}); err != nil || on {
		t.Errorf("no --pool: %v %v", on, err)
	}
	for _, args := range [][]string{
		{"cvn_node", "--pool"},
		{"cvn_node", "--pool", "--seeds", "1.2.3.4"},
		{"cvn_node", "--pool", "not-an-address"},
		{"cvn_node", "--pool", good[:len(good)-1] + "0"},
	} {
		if _, on, err := poolAddressRequested(args); err == nil || on {
			t.Errorf("%v was accepted", args)
		}
	}
}

func TestPoolLimitsConnectionsFromOneAddress(t *testing.T) {
	for i := 0; i < poolConnectionsPerHost; i++ {
		if !poolAdmit("203.0.113.9") {
			t.Fatalf("connection %d was refused", i+1)
		}
	}
	if poolAdmit("203.0.113.9") {
		t.Error("one address was given more connections than the limit")
	}
	if !poolAdmit("203.0.113.10") {
		t.Error("another address was refused because the first was full")
	}
	poolRelease("203.0.113.10")
	poolRelease("203.0.113.9")
	if !poolAdmit("203.0.113.9") {
		t.Error("a place given back was not handed out again")
	}
	for i := 0; i < poolConnectionsPerHost; i++ {
		poolRelease("203.0.113.9")
	}
	poolConnections.Lock()
	left, all := len(poolConnections.byHost), poolConnections.all
	poolConnections.Unlock()
	if left != 0 || all != 0 {
		t.Errorf("after everyone left, %d addresses and %d connections are still counted", left, all)
	}
}
