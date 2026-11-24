package node

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// Tests for the third outside review of the pool (Gemini 4 Hydrogen,
// 25 November 2026, on commit b1700e6).

// Finding 1. A payment's transfer mined in a block that replaced one already
// looked at, at or below the height the chain stood at when it was sent, is
// still found.
func TestAPaymentMinedInAReplacedBlockIsStillFound(t *testing.T) {
	pool := startPayingPool(t)
	now := time.Now()
	payments, _, err := poolPrepare(now)
	if err != nil || len(payments) != 1 {
		t.Fatalf("prepared %d: %v", len(payments), err)
	}
	signed := pool.sign(t, payments[0])
	if _, err := poolAcceptSigned(payments[0].Serial, signed, now); err != nil {
		t.Fatal(err)
	}
	// Blocks are mined without the pool looking, until the transfer is in
	// one of them.
	sent, _ := poolLedger.payment(payments[0].Serial)
	minedAt := int64(0)
	for i := 0; i < 5 && minedAt == 0; i++ {
		pool.mine(t, 1)
		minedAt, _, _ = poolFindTransfer(sent.TxID, sent.SentAtHeight-2, GetLatestBlockInternal().Index)
	}
	if minedAt == 0 {
		t.Fatal("the transfer was not mined in five blocks")
	}
	// The pool believes it has already looked as far as the tip, but the
	// block it saw there has since been replaced. It must look again from
	// below, and find the transfer.
	poolResent.Lock()
	poolResent.searched[sent.Serial] = GetLatestBlockInternal().Index
	poolResent.searchedHash[sent.Serial] = strings.Repeat("ee", 32)
	poolResent.Unlock()
	poolSettlePayments(now)
	if kept, _ := poolLedger.payment(sent.Serial); kept.MinedAtHeight != minedAt {
		t.Errorf("the transfer mined in block %d was not found after the block looked at before had changed (found at %d)", minedAt, kept.MinedAtHeight)
	}
}

// Finding 2. A record put back reads the chain from its first block, not
// from the first of the blocks it keeps in memory.
func TestAPutBackRecordReadsFromItsFirstBlockEver(t *testing.T) {
	record, addresses := recordWithOwed(t, cvn("50"), []int64{3, 1})
	path := record.db.Path()
	saved := poolBlocksKept
	poolBlocksKept = 2
	t.Cleanup(func() { poolBlocksKept = saved })
	for height := int64(2); height <= 6; height++ {
		block := Block{Index: height, Hash: fmt.Sprintf("%064x", height), Difficulty: 4, Transactions: []Transaction{
			{ID: fmt.Sprintf("TX_COINBASE_H%d_t", height), Outputs: []UTXOOutput{{Recipient: addresses[0], Amount: UnitsAmount(cvn("50"))}}},
		}}
		parts := []poolPart{{Address: addresses[0], Units: cvn("50"), Work: 4, InBlock: true}}
		if _, err := record.addBlock(block, addresses[0], parts, 0, 0, 4, 2000); err != nil {
			t.Fatal(err)
		}
		if _, _, err := record.decide(height, block.Hash, true, height+100, block.Transactions[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	record.close()
	again, err := openPoolLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.close()
	lowestKept := again.blocks[0].Height
	if lowestKept <= 1 {
		t.Fatalf("the test needs block 1 out of memory; the lowest kept is %d", lowestKept)
	}
	if from := again.readFrom(500, 6, func(int64) string { return "" }); from != 1 {
		t.Errorf("a record that keeps blocks from %d in memory starts reading at %d, want 1, its first block ever", lowestKept, from)
	}
}

// Finding 4. A payment prepared before the record was opened is not given
// back ten minutes on: the transfer the program before may have signed for
// it could still be mined.
func TestAPaymentPreparedBeforeARestartIsHeld(t *testing.T) {
	record, _ := recordWithOwed(t, cvn("50"), []int64{3, 1})
	path := record.db.Path()
	prepared, _, err := record.preparePayments(poolPaymentPlan{Now: time.Now().Add(-time.Hour).Unix(), PoolAddress: testAddress("pool"), MinimumUnits: cvn("1"), FeeUnits: cvn("0.001")})
	if err != nil || len(prepared) != 1 {
		t.Fatalf("prepared %d: %v", len(prepared), err)
	}
	record.close()
	again, err := openPoolLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.close()
	now := time.Now()
	if returned := again.returnStalePayments(now.Add(-poolPreparedFor).Unix(), 200); len(returned) != 0 {
		t.Error("a payment prepared before the record was opened was given back ten minutes on")
	}
	// Once the record has been open as long as a transfer can wait, it is.
	later := now.Add(MempoolMaxAge + poolPreparedFor + time.Minute)
	if returned := again.returnStalePayments(later.Add(-poolPreparedFor).Unix(), 300); len(returned) != 1 {
		t.Errorf("after the record had been open %v, %d payments were given back, want 1", MempoolMaxAge+poolPreparedFor, len(returned))
	}
}

// Finding 7. An address owed more than the pool's coins can pay does not
// hold up those that can be paid.
func TestOneAddressThatCannotBePaidDoesNotHoldUpTheRest(t *testing.T) {
	record, addresses := recordWithOwed(t, cvn("12"), []int64{5, 1})
	// The reward coin is 12 CVN; the first address is owed 10, the second 2.
	// Make the first owed 20 by hand, more than there is.
	record.Lock()
	first := record.accounts[addresses[0]]
	first.Owed += cvn("10")
	first.Earned += cvn("10")
	record.accounts[addresses[0]] = first
	record.Unlock()
	prepared, reason, err := record.preparePayments(poolPaymentPlan{Now: 3000, PoolAddress: testAddress("pool"), MinimumUnits: cvn("1"), FeeUnits: cvn("0.001")})
	if err != nil || len(prepared) != 1 || len(prepared[0].Lines) != 1 || prepared[0].Lines[0].Address != addresses[1] {
		t.Fatalf("prepared %d payments (%s): %v", len(prepared), reason, err)
	}
}

// Finding 6. Work that paid the whole block to its miner, made before any
// share, is out of date once another address has one, and that miner is
// sent new work at once.
func TestWorkMadeBeforeAnyShareGoesOutOfDate(t *testing.T) {
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 64}})
	Params.HeartbeatInitialDifficulty = 64
	server, _ := startPoolForTest(t, 4)
	first, second := connectFakeMiner(t, server), connectFakeMiner(t, server)
	aliceJob := poolLogin(t, first, testAddress("alice"))
	bobJob := poolLogin(t, second, testAddress("bob"))
	share := func(work string) bool { return meets(4)(work) && !meets(64)(work) }

	// Alice's own shares on her first work are fine while only she has any.
	for i := uint32(0); i < 2; i++ {
		nonce, _ := first.searchWork(aliceJob, i*50000, share)
		if _, errorText, _ := first.call("submit", map[string]string{"job_id": aliceJob.JobID, "nonce": nonce}); errorText != "" {
			t.Fatalf("Alice's share on her first work was refused: %s", errorText)
		}
	}
	// Bob's first work was made before any share: now that Alice has some,
	// it is out of date, and is not counted against him.
	nonce, _ := second.searchWork(bobJob, 0, share)
	started := time.Now()
	_, errorText, pushed := second.call("submit", map[string]string{"job_id": bobJob.JobID, "nonce": nonce})
	if !strings.Contains(errorText, "stale") {
		t.Errorf("a share on work made before anybody's share, with Alice's shares now in, got %q", errorText)
	}
	// He is sent new work without asking, well before the usual fifteen
	// seconds.
	for len(pushed) == 0 && time.Since(started) < 5*time.Second {
		pushed = append(pushed, second.waitForPushedJob())
	}
	if len(pushed) == 0 || pushed[len(pushed)-1].JobID == bobJob.JobID || time.Since(started) > 5*time.Second {
		t.Errorf("Bob was not sent new work promptly (%v)", time.Since(started))
	}
}

// Findings 9 and 10. Logging in again does not build work faster than asking
// for it, and a connection that never logs in is closed.
func TestLoginAgainAndNeverLoggingIn(t *testing.T) {
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 64}})
	Params.HeartbeatInitialDifficulty = 64
	server, _ := startPoolForTest(t, 4)
	stratumJobAtMostEvery = time.Second
	savedLogin := stratumLoginWithin
	stratumLoginWithin = 300 * time.Millisecond
	t.Cleanup(func() { stratumLoginWithin = savedLogin })

	miner := connectFakeMiner(t, server)
	alice := testAddress("alice")
	first := poolLogin(t, miner, alice)
	if again := poolLogin(t, miner, alice); again.JobID != first.JobID {
		t.Error("logging in again within a second built fresh work")
	}

	idle := connectFakeMiner(t, server)
	// It says it is still there, again and again, and never logs in.
	closed := false
	for i := 0; i < 10 && !closed; i++ {
		_, _ = idle.conn.Write([]byte(`{"id":1,"jsonrpc":"2.0","method":"keepalived","params":{}}` + string(rune(10))))
		_ = idle.conn.SetReadDeadline(time.Now().Add(time.Second))
		buffer := make([]byte, 4096)
		if _, err := idle.conn.Read(buffer); err != nil && !strings.Contains(err.Error(), "timeout") {
			closed = true
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !closed {
		t.Error("a connection that never logged in was kept open by saying it was still there")
	}
}

// Finding 8. The check of a copy refuses a payment whose outputs or kept
// transfer have been altered, and a waiting block whose parts do not add up.
func TestTheCheckOfACopyLooksAtOutputsAndWaitingBlocks(t *testing.T) {
	record, addresses := recordWithOwed(t, cvn("50"), []int64{3, 1})
	prepared, _, err := record.preparePayments(poolPaymentPlan{Now: 3000, PoolAddress: testAddress("pool"), MinimumUnits: cvn("1"), FeeUnits: cvn("0.001")})
	if err != nil || len(prepared) != 1 {
		t.Fatal(err)
	}
	waiting := Block{Index: 2, Hash: strings.Repeat("cd", 32), Difficulty: 4, Transactions: []Transaction{
		{ID: "TX_COINBASE_H2_t", Outputs: []UTXOOutput{{Recipient: addresses[0], Amount: UnitsAmount(cvn("50"))}}},
	}}
	if _, err := record.addBlock(waiting, addresses[0], []poolPart{{Address: addresses[0], Units: cvn("50"), Work: 4, InBlock: true}}, 0, 0, 4, 2000); err != nil {
		t.Fatal(err)
	}
	quickToSeeAFileInUse(t)
	good, _, err := record.writeBackup(filepath.Join(t.TempDir(), "backups"), 0)
	if err != nil {
		t.Fatalf("a sound record did not pass: %v", err)
	}
	alter := func(what string, change func(tx *bbolt.Tx) error) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "altered.db")
		if err := copyFile(good, path); err != nil {
			t.Fatal(err)
		}
		db, err := bbolt.Open(path, 0600, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Update(change); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if _, err := checkPoolRecordFile(path); err == nil {
			t.Errorf("a copy with %s passed the check", what)
		}
	}
	alter("a payment paying another address", func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(poolPaymentsBucket)
		key, raw := bucket.Cursor().First()
		var payment poolPayment
		_ = json.Unmarshal(raw, &payment)
		payment.Outputs[0].Recipient = testAddress("thief")
		return putJSON(bucket, key, payment)
	})
	alter("a waiting block whose parts do not add up", func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(poolBlocksBucket)
		key := poolBlockKey(2, waiting.Hash)
		var block poolBlock
		_ = json.Unmarshal(bucket.Get(key), &block)
		block.Parts[0].Units = cvn("60")
		return putJSON(bucket, key, block)
	})
	alter("a part of less than nothing", func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(poolBlocksBucket)
		key := poolBlockKey(2, waiting.Hash)
		var block poolBlock
		_ = json.Unmarshal(bucket.Get(key), &block)
		block.Parts = append(block.Parts, poolPart{Address: addresses[1], Units: -1})
		block.Parts[0].Units++
		return putJSON(bucket, key, block)
	})
}

// Finding 12. The paying command holds the node to the fee it knows, and
// will not sign a payment that spends a coin twice or pays an address twice.
func TestThePayingCommandHoldsTheNodeToItsOwnFee(t *testing.T) {
	record, _ := recordWithOwed(t, cvn("50"), []int64{3, 1})
	pool := testAddress("pool")
	prepared, _, err := record.preparePayments(poolPaymentPlan{Now: 3000, PoolAddress: pool, MinimumUnits: cvn("1"), FeeUnits: poolPaymentFeeUnits})
	if err != nil || len(prepared) != 1 {
		t.Fatal(err)
	}
	good := prepared[0]
	if err := poolPaymentLooksRight(good, pool, poolPaymentFeeUnits); err != nil {
		t.Fatalf("the payment as prepared is refused: %v", err)
	}
	dearer := good
	dearer.FeeUnits = 10 * poolPaymentFeeUnits
	if err := poolPaymentLooksRight(dearer, pool, dearer.FeeUnits); err == nil {
		t.Error("a payment with ten times the fee was signed because the node said so")
	}
	twice := good
	twice.Lines = append([]poolPaymentLine{}, good.Lines...)
	twice.Lines[1].Address = twice.Lines[0].Address
	twice.Outputs = append([]UTXOOutput{}, good.Outputs...)
	twice.Outputs[1].Recipient = twice.Outputs[0].Recipient
	if err := poolPaymentLooksRight(twice, pool, poolPaymentFeeUnits); err == nil {
		t.Error("a payment paying one address twice was signed")
	}
}
