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
	// Unless a test says otherwise, its pool is one whose miners are all too
	// small to be paid in the block: every part goes to the pool's address
	// and is owed, which is the part of a pool that keeps a record of debts.
	// Paying in the block has tests of its own (pool_inblock_test.go).
	savedFloor := poolInBlockFloorUnits
	poolInBlockFloorUnits = MaxAmountUnits
	t.Cleanup(func() { poolInBlockFloorUnits = savedFloor })
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

// addBlockKeptByThePool writes a block into a record as a pool does whose
// miners are all too small to be paid in the block: divided among the shares
// up to `winning`, and all of it paid to the pool's address.
func addBlockKeptByThePool(record *poolRecord, block Block, finder string, winning uint64, at int64) (poolBlock, error) {
	var rewardUnits int64
	if len(block.Transactions) > 0 {
		for _, output := range block.Transactions[0].Outputs {
			rewardUnits += output.Amount.Units()
		}
	}
	record.Lock()
	parts, windowWork := poolSplit(record.shares, winning, poolWindowWork(block.Difficulty), rewardUnits)
	record.Unlock()
	return record.addBlock(block, finder, parts, 0, rewardUnits, windowWork, at)
}

// freshWork asks the pool for new work, as it stands now: with the division
// of the shares handed in so far written into it.
func (m *fakeMiner) freshWork() minerJob {
	m.t.Helper()
	result, errorText, _ := m.call("getjob", map[string]string{})
	if errorText != "" {
		m.t.Fatalf("no fresh work: %s", errorText)
	}
	var job minerJob
	if err := json.Unmarshal(result, &job); err != nil || job.JobID == "" {
		m.t.Fatalf("unexpected work: %s", result)
	}
	return job
}

// shareThenFreshWork has a miner hand in one share that is not a block, and
// then take the work the pool makes now. A block's division is written into
// its work when the work is made, so shares count from the next work on.
func (m *fakeMiner) shareThenFreshWork(job minerJob, networkDifficulty int) minerJob {
	m.t.Helper()
	nonce, _ := m.searchWork(job, 0, func(work string) bool { return meets(4)(work) && !meets(networkDifficulty)(work) })
	if _, errorText, _ := m.call("submit", map[string]string{"job_id": job.JobID, "nonce": nonce}); errorText != "" {
		m.t.Fatalf("share refused: %s", errorText)
	}
	return m.freshWork()
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

	// Bob hands in one share and is sent fresh work. That work's reward is
	// divided among the shares so far: Alice's three and Bob's one.
	bobJob = second.shareThenFreshWork(bobJob, 64)
	// Bob's next answer meets the network's difficulty: it is a block.
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
	if view.Shares != 5 || view.BlocksFound != 1 || view.Blocks[0].Hash != tip.Hash || len(view.Blocks[0].Parts) != 2 {
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

// confirmAfter makes a block count once `blocks` more stand on it, in place
// of the hundred a real network needs.
func confirmAfter(t *testing.T, blocks int64) {
	t.Helper()
	economics := NewGenesisEconomics
	if Params.Economics != nil {
		economics = *Params.Economics
	}
	economics.ReorgLimitBlocks = blocks
	Params.Economics = &economics
	if poolConfirmDepth() != blocks {
		t.Fatalf("the pool waits for %d blocks, want %d", poolConfirmDepth(), blocks)
	}
}

// mineThroughPool has a stand-in miner find the block its work is for, and
// returns its work for the block after.
func (m *fakeMiner) mineThroughPool(job minerJob) minerJob {
	m.t.Helper()
	tip := GetLatestBlockInternal()
	difficulty := CalculateNextDifficulty(tip, minerBlockDate(tip, time.Now().Unix()))
	var from uint32
	var pushed []minerJob
	for GetLatestBlockInternal().Index < job.Height {
		var nonce string
		nonce, from = m.searchWork(job, from, meets(difficulty))
		_, errorText, more := m.call("submit", map[string]string{"job_id": job.JobID, "nonce": nonce})
		if errorText != "" {
			m.t.Fatalf("an answer for block %d was refused: %s", job.Height, errorText)
		}
		pushed = append(pushed, more...)
	}
	for _, next := range pushed {
		if next.Height > job.Height {
			return next
		}
	}
	for {
		if next := m.waitForPushedJob(); next.Height > job.Height {
			return next
		}
	}
}

func TestPoolBlockCountsOnceItIsDeepEnough(t *testing.T) {
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 64}})
	Params.HeartbeatInitialDifficulty = 64
	confirmAfter(t, 2)
	server, recordPath := startPoolForTest(t, 4)
	alice, bob := testAddress("alice"), testAddress("bob")
	first := connectFakeMiner(t, server)
	aliceJob := poolLogin(t, first, alice)
	second := connectFakeMiner(t, server)
	bobJob := poolLogin(t, second, bob)

	shareOnly := func(work string) bool { return meets(4)(work) && !meets(64)(work) }
	var nonce string
	var from uint32
	for i := 0; i < 3; i++ {
		nonce, from = first.searchWork(aliceJob, from, shareOnly)
		if _, errorText, _ := first.call("submit", map[string]string{"job_id": aliceJob.JobID, "nonce": nonce}); errorText != "" {
			t.Fatalf("share refused: %s", errorText)
		}
	}
	bobJob = second.shareThenFreshWork(bobJob, 64)
	blockNonce, _ := second.searchWork(bobJob, 0, meets(64))
	_, errorText, pushed := second.call("submit", map[string]string{"job_id": bobJob.JobID, "nonce": blockNonce})
	if errorText != "" || GetLatestBlockInternal().Index != 1 {
		t.Fatalf("block 1 was not found: %s", errorText)
	}
	firstBlock := GetLatestBlockInternal().Hash

	// Found, and not counted yet: nothing stands on it.
	if confirmed, lost := poolSettle(); confirmed != 0 || lost != 0 {
		t.Fatalf("a block with nothing on it was settled: %d confirmed, %d lost", confirmed, lost)
	}
	view := poolLedger.view(64, time.Now().Unix(), 10)
	if view.Waiting != 1 || view.Confirmed != 0 || view.Owed != 0 || view.Earned != 0 {
		t.Fatalf("before it counts: %d waiting, %d confirmed, %s owed", view.Waiting, view.Confirmed, FormatUnits(view.Owed))
	}

	// Two more blocks on top of it.
	var next minerJob
	for _, job := range pushed {
		if job.Height == 2 {
			next = job
		}
	}
	for next.Height != 2 {
		next = second.waitForPushedJob()
	}
	next = second.mineThroughPool(next)
	if confirmed, _ := poolSettle(); confirmed != 0 {
		t.Fatal("a block with one block on it was counted; it needs two")
	}
	second.mineThroughPool(next)
	if GetLatestBlockInternal().Index != 3 {
		t.Fatalf("the chain is at %d, want 3", GetLatestBlockInternal().Index)
	}
	if confirmed, lost := poolSettle(); confirmed != 1 || lost != 0 {
		t.Fatalf("with two blocks on block 1: %d confirmed, %d lost; want 1 and 0", confirmed, lost)
	}

	// Block 1 counts, and its parts are owed: 37.5 and 12.5, as it was
	// divided when it was found. Blocks 2 and 3 are still waiting.
	view = poolLedger.view(64, time.Now().Unix(), 10)
	owed := map[string]int64{}
	for _, address := range view.Addresses {
		owed[address.Address] = address.Account.Owed
		if address.Account.Earned != address.Account.Owed || address.Account.Paid != 0 {
			t.Errorf("%s: earned %d, owed %d, paid %d", address.Address, address.Account.Earned, address.Account.Owed, address.Account.Paid)
		}
	}
	if view.Confirmed != 1 || view.Waiting != 2 || view.Lost != 0 || view.Owed != cvn("50") || owed[alice] != cvn("37.5") || owed[bob] != cvn("12.5") {
		t.Fatalf("after block 1 counts: %d confirmed, %d waiting, owed %s (Alice %s, Bob %s)",
			view.Confirmed, view.Waiting, FormatUnits(view.Owed), FormatUnits(owed[alice]), FormatUnits(owed[bob]))
	}
	for _, block := range view.Blocks {
		if block.Height == 1 && (block.Hash != firstBlock || block.State != poolBlockConfirmed || block.DecidedAtHeight != 3) {
			t.Errorf("block 1 is recorded as %+v", block)
		}
	}

	// Looking again changes nothing: a block is credited once.
	if confirmed, lost := poolSettle(); confirmed != 0 || lost != 0 {
		t.Errorf("settling again: %d confirmed, %d lost", confirmed, lost)
	}
	if _, changed, _ := poolLedger.decide(1, firstBlock, true, 3, ""); changed {
		t.Error("a block that already counts was decided a second time")
	}
	if _, changed, _ := poolLedger.decide(9, strings.Repeat("cd", 32), true, 30, ""); changed {
		t.Error("a block the pool never found was decided")
	}
	if again := poolLedger.view(64, time.Now().Unix(), 10); again.Owed != cvn("50") {
		t.Errorf("after settling again %s is owed, want 50", FormatUnits(again.Owed))
	}

	// The pages show what is owed, in all and for one address.
	pages := http.NewServeMux()
	addPoolPages(pages)
	reply := httptest.NewRecorder()
	pages.ServeHTTP(reply, httptest.NewRequest("GET", "/pool.json", nil))
	var page poolPage
	if err := json.Unmarshal(reply.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.OwedUnits != cvn("50") || page.BlocksConfirmed != 1 || page.BlocksWaiting != 2 || page.ConfirmBlocks != 2 || page.HeldUnits != cvn("150") {
		t.Errorf("pool.json: owed %s, %d confirmed, %d waiting, held %s", page.Owed, page.BlocksConfirmed, page.BlocksWaiting, page.Held)
	}
	reply = httptest.NewRecorder()
	pages.ServeHTTP(reply, httptest.NewRequest("GET", "/pool.json?address="+alice, nil))
	var one poolPageAddress
	if err := json.Unmarshal(reply.Body.Bytes(), &one); err != nil || one.Address != alice || one.Owed != "37.5" || one.Work != 12 {
		t.Errorf("pool.json for Alice: %s", reply.Body.String())
	}
	reply = httptest.NewRecorder()
	pages.ServeHTTP(reply, httptest.NewRequest("GET", "/pool.json?address=nonsense", nil))
	if reply.Code != http.StatusBadRequest {
		t.Errorf("pool.json for something that is not an address answered %d", reply.Code)
	}

	// What is owed is still owed when the record is opened again.
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
	if view.Owed != cvn("50") || view.Confirmed != 1 || view.Waiting != 2 {
		t.Errorf("opened again: owed %s, %d confirmed, %d waiting", FormatUnits(view.Owed), view.Confirmed, view.Waiting)
	}
	if account, _ := again.account(alice, 64); account.Owed != cvn("37.5") {
		t.Errorf("opened again, Alice is owed %s", FormatUnits(account.Owed))
	}
}

func TestPoolBlockThatWasReplacedCountsForNothing(t *testing.T) {
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 64}})
	Params.HeartbeatInitialDifficulty = 64
	confirmAfter(t, 2)
	server, _ := startPoolForTest(t, 4)
	carol, dave := testAddress("carol"), testAddress("dave")

	// The pool once found a block 1 of its own, on work of Carol's, and
	// recorded it. The network settled on another block 1.
	place, err := poolLedger.addShare(carol, 4, time.Now().Unix(), 64)
	if err != nil {
		t.Fatal(err)
	}
	replaced := Block{Index: 1, Hash: strings.Repeat("ab", 32), Difficulty: 64, Transactions: []Transaction{
		{Outputs: []UTXOOutput{{Recipient: testAddress("pool"), Amount: UnitsAmount(cvn("50"))}}},
	}}
	recorded, err := addBlockKeptByThePool(poolLedger, replaced, carol, place, time.Now().Unix())
	if err != nil || len(recorded.Parts) != 1 || recorded.Parts[0].Units != cvn("50") {
		t.Fatalf("recording the block: %v %+v", err, recorded)
	}

	miner := connectFakeMiner(t, server)
	job := poolLogin(t, miner, dave)
	for i := 0; i < 3; i++ {
		job = miner.mineThroughPool(job)
	}
	if GetLatestBlockInternal().Index != 3 || GetBlockByHeightFromDB(1).Hash == replaced.Hash {
		t.Fatal("the chain should hold three blocks, the first of them not the replaced one")
	}
	if confirmed, lost := poolSettle(); confirmed != 1 || lost != 1 {
		t.Fatalf("%d confirmed and %d lost; want the real block 1 confirmed and the replaced one lost", confirmed, lost)
	}

	view := poolLedger.view(64, time.Now().Unix(), 10)
	if view.Lost != 1 || view.Confirmed != 1 || view.Waiting != 2 {
		t.Errorf("%d lost, %d confirmed, %d waiting", view.Lost, view.Confirmed, view.Waiting)
	}
	// Exactly the real block's reward is owed. The replaced block added
	// nothing, though it was recorded as paying Carol 50.
	if view.Owed != cvn("50") {
		t.Errorf("%s CVN is owed; the one block that counts paid 50", FormatUnits(view.Owed))
	}
	var realParts []poolPart
	for _, block := range view.Blocks {
		switch {
		case block.Hash == replaced.Hash:
			if block.State != poolBlockLost || block.DecidedAtHeight != 3 {
				t.Errorf("the replaced block is recorded as %+v", block)
			}
		case block.Height == 1:
			realParts = block.Parts
		}
	}
	// Carol's share was the only one when the work for the real block 1
	// was made, so that block is hers too, and it is all she is owed: once,
	// for the block that counts, and nothing for the one that was replaced.
	var carolPart int64
	for _, part := range realParts {
		if part.Address == carol {
			carolPart = part.Units
		}
	}
	if account, _ := poolLedger.account(carol, 64); account.Owed != carolPart || carolPart != cvn("50") || account.Earned != cvn("50") {
		t.Errorf("Carol is owed %s; her part of the block that counts is %s", FormatUnits(account.Owed), FormatUnits(carolPart))
	}
}
