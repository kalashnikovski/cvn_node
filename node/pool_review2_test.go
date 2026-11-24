package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// Tests for the second outside review of the pool (Codex, 25 November 2026,
// on commit 52326c4).

// readToIn says what a record's file records of how far the chain was read.
func readToIn(t *testing.T, path string) (string, bool) {
	t.Helper()
	db, err := bbolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value []byte
	_ = db.View(func(tx *bbolt.Tx) error {
		if meta := tx.Bucket(poolMetaBucket); meta != nil {
			value = meta.Get(poolReadToKey)
		}
		return nil
	})
	return string(value), value != nil
}

// Finding 1. A copy does not say how far the chain was read, and a copy put
// back has whatever it says of it taken out: the pool reads the chain again
// from the copy's first block, so an edited figure cannot make it skip a
// payment.
func TestACopyDoesNotSayHowFarTheChainWasRead(t *testing.T) {
	record, _ := recordWithOwed(t, cvn("50"), []int64{3, 1})
	if err := record.noteRead(1, 900, 6, map[int64]string{900: "abc"}, nil); err != nil {
		t.Fatal(err)
	}
	record.Lock()
	readTo := record.readTo
	record.Unlock()
	if readTo != 900 {
		t.Fatalf("the record itself says it read to %d", readTo)
	}
	quickToSeeAFileInUse(t)
	copyPath, _, err := record.writeBackup(filepath.Join(t.TempDir(), "backups"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if value, found := readToIn(t, copyPath); found {
		t.Errorf("the copy says the chain was read to %q", value)
	}

	// Somebody edits the copy to say the chain was read to the tip.
	db, err := bbolt.Open(copyPath, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(poolMetaBucket).Put(poolReadToKey, []byte("5000"))
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	savedChainFile := BlockchainFile
	BlockchainFile = filepath.Join(t.TempDir(), "chain.db")
	t.Cleanup(func() { BlockchainFile = savedChainFile })
	if _, _, err := restorePoolBackup(copyPath); err != nil {
		t.Fatal(err)
	}
	if value, found := readToIn(t, poolLedgerPath()); found {
		t.Errorf("the copy put back still says the chain was read to %q", value)
	}
	// Opened, it reads from its first block.
	again, err := openPoolLedger(poolLedgerPath())
	if err != nil {
		t.Fatal(err)
	}
	defer again.close()
	if from := again.readFrom(5000, 6, func(int64) string { return "" }); from != 1 {
		t.Errorf("the copy put back starts reading the chain at block %d, want 1", from)
	}
}

// Finding 2. Answers without the work are counted for the address they come
// from across its connections, and an address that sends too many is refused
// for a while, however often it reconnects.
func TestAnAddressSendingJunkIsRefusedForAWhile(t *testing.T) {
	savedAllowed, savedOver, savedFor := poolStrikesAllowed, poolStrikesOver, poolStrikeoutFor
	poolStrikesAllowed, poolStrikesOver, poolStrikeoutFor = 5, time.Minute, 10*time.Minute
	t.Cleanup(func() {
		poolStrikesAllowed, poolStrikesOver, poolStrikeoutFor = savedAllowed, savedOver, savedFor
		poolStrikes.Lock()
		poolStrikes.byHost = make(map[string]poolStrikeCount)
		poolStrikes.Unlock()
	})
	now := time.Now()
	for i := 1; i < 5; i++ {
		if poolNoteBadWork("203.0.113.9", now) {
			t.Fatalf("refused after %d answers without the work, allowed 5", i)
		}
	}
	if !poolNoteBadWork("203.0.113.9", now) {
		t.Fatal("not refused after 5 answers without the work")
	}
	if !poolStruckOut("203.0.113.9", now.Add(9*time.Minute)) || poolStruckOut("203.0.113.9", now.Add(11*time.Minute)) {
		t.Error("the refusal does not last ten minutes and then end")
	}
	if poolStruckOut("198.51.100.4", now) {
		t.Error("another address was refused with it")
	}
	if !poolStruckOut("203.0.113.9", time.Now()) || poolAdmit("203.0.113.9") {
		t.Error("a refused address was let in")
	}
	// Spread out, the same number is not refused.
	for i := 0; i < 10; i++ {
		if poolNoteBadWork("192.0.2.1", now.Add(time.Duration(i)*2*time.Minute)) {
			t.Fatalf("an address sending one answer without the work every two minutes was refused at the %d-th", i+1)
		}
	}
}

// Findings 2, 3 and 5, at a real pool: the same nonce is not checked twice,
// fresh work is built at most once a second for a miner, and a connection
// stays with the address it logged in with.
func TestAPoolMinerCannotMakeItWorkForNothing(t *testing.T) {
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 64}})
	Params.HeartbeatInitialDifficulty = 64
	server, _ := startPoolForTest(t, 4)
	stratumJobAtMostEvery = time.Second // as a real pool has it
	miner := connectFakeMiner(t, server)
	alice := testAddress("alice")
	job := poolLogin(t, miner, alice)

	// A nonce without the work, sent twice: the second time it is a
	// duplicate, and no proof is checked again.
	bad, _ := miner.searchWork(job, 0, func(work string) bool { return !meets(4)(work) })
	if _, errorText, _ := miner.call("submit", map[string]string{"job_id": job.JobID, "nonce": bad}); errorText != stratumBadWork {
		t.Fatalf("an answer without the work got %q", errorText)
	}
	if _, errorText, _ := miner.call("submit", map[string]string{"job_id": job.JobID, "nonce": bad}); errorText != poolDuplicateShare {
		t.Errorf("the same answer without the work sent again got %q", errorText)
	}

	// Asked for fresh work again and again, it is given the same work.
	first := miner.freshWork()
	if again := miner.freshWork(); again.JobID != first.JobID {
		t.Error("fresh work was built twice within a second")
	}
	stratumJobAtMostEvery = 0
	if again := miner.freshWork(); again.JobID == first.JobID {
		t.Error("with no limit, fresh work was not built")
	}

	// Logging in again as another address is refused; as the same one, not.
	if _, errorText, _ := miner.call("login", map[string]string{"login": testAddress("bob"), "pass": "x"}); !strings.Contains(errorText, "already logged in as "+alice) {
		t.Errorf("logging in again as another address got %q", errorText)
	}
	if _, errorText, _ := miner.call("login", map[string]string{"login": alice, "pass": "x"}); errorText != "" {
		t.Errorf("logging in again as the same address got %q", errorText)
	}
	// The connection is still there and still Alice's.
	if share, _ := miner.searchWork(miner.freshWork(), 1000, func(work string) bool { return meets(4)(work) && !meets(64)(work) }); share == "" {
		t.Fatal("no share found")
	}
}

// Finding 4. When the pool's coins are too many and too small for a payment
// to gather enough from the 200 a transfer may spend, it combines them first.
func TestThePoolCombinesCoinsTooSmallToPayWith(t *testing.T) {
	record, addresses := recordWithOwed(t, cvn("1.2"), []int64{1})
	pool := testAddress("pool")
	// 300 coins of 0.004 CVN in place of the one reward coin: 1.2 CVN in
	// all, and 0.8 CVN in the largest 200.
	record.Lock()
	record.coins = make(map[string]poolCoin)
	err := record.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(poolCoinsBucket)
		key, _ := bucket.Cursor().First()
		if err := bucket.Delete(key); err != nil {
			return err
		}
		for i := 0; i < 300; i++ {
			coin := poolCoin{TxID: fmt.Sprintf("TX_small_%03d", i), Index: 0, Units: cvn("0.004")}
			record.coins[coin.key()] = coin
			if err := putJSON(bucket, []byte(coin.key()), coin); err != nil {
				return err
			}
		}
		return nil
	})
	record.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if account, _ := record.account(addresses[0], 1000); account.Owed != cvn("1.2") {
		t.Fatalf("the miner is owed %s", FormatUnits(account.Owed))
	}

	plan := poolPaymentPlan{Now: 3000, PoolAddress: pool, MinimumUnits: cvn("1"), FeeUnits: cvn("0.001")}
	prepared, reason, err := record.preparePayments(plan)
	if err != nil || len(prepared) != 1 {
		t.Fatalf("prepared %d (%s): %v", len(prepared), reason, err)
	}
	combine := prepared[0]
	if len(combine.Lines) != 0 || len(combine.Inputs) != TxMaxInputs || combine.FeeUnits != 0 || combine.ChangeUnits != cvn("0.8") ||
		len(combine.Outputs) != 1 || combine.Outputs[0].Recipient != pool || combine.Outputs[0].Amount.Units() != cvn("0.8") {
		t.Fatalf("the combining transfer was prepared as %d lines, %d inputs, fee %d, change %d, outputs %+v",
			len(combine.Lines), len(combine.Inputs), combine.FeeUnits, combine.ChangeUnits, combine.Outputs)
	}
	if !strings.Contains(reason, "combines") {
		t.Errorf("the reason given was %q", reason)
	}
	// Nobody's account moved: combining pays nobody.
	if account, _ := record.account(addresses[0], 1000); account.Owed != cvn("1.2") || account.Pending != 0 {
		t.Errorf("after combining, the miner's account is %+v", account)
	}
	// The paying command signs it, and nothing like it that pays anybody.
	if err := poolPaymentLooksRight(combine, pool, plan.FeeUnits); err != nil {
		t.Errorf("the paying command refuses the combining transfer: %v", err)
	}
	stolen := combine
	stolen.Outputs = []UTXOOutput{{Recipient: testAddress("thief"), Amount: UnitsAmount(cvn("0.8"))}}
	withFee := combine
	withFee.FeeUnits, withFee.ChangeUnits = cvn("0.1"), cvn("0.7")
	withFee.Outputs = []UTXOOutput{{Recipient: pool, Amount: UnitsAmount(cvn("0.7"))}}
	short := combine
	short.Outputs = []UTXOOutput{{Recipient: pool, Amount: UnitsAmount(cvn("0.5"))}}
	for name, bad := range map[string]poolPayment{"to another address": stolen, "with a fee": withFee, "returning less than it spends": short} {
		if err := poolPaymentLooksRight(bad, pool, plan.FeeUnits); err == nil {
			t.Errorf("the paying command would sign a transfer that pays nobody %s", name)
		}
	}
	// A record with a combining transfer under way passes the check of a copy.
	quickToSeeAFileInUse(t)
	if _, _, err := record.writeBackup(filepath.Join(t.TempDir(), "backups"), 0); err != nil {
		t.Errorf("a record combining its coins does not pass the check: %v", err)
	}
}

// Finding 6. A copy in which a payment spends other coins than those set
// aside for it, of the same value, is refused.
func TestACopyWhosePaymentSpendsOtherCoinsIsRefused(t *testing.T) {
	record, _ := recordWithOwed(t, cvn("50"), []int64{3, 1})
	if _, _, err := record.preparePayments(poolPaymentPlan{Now: 3000, PoolAddress: testAddress("pool"), MinimumUnits: cvn("1"), FeeUnits: cvn("0.001")}); err != nil {
		t.Fatal(err)
	}
	quickToSeeAFileInUse(t)
	good, _, err := record.writeBackup(filepath.Join(t.TempDir(), "backups"), 0)
	if err != nil {
		t.Fatal(err)
	}
	altered := filepath.Join(t.TempDir(), "altered.db")
	if err := copyFile(good, altered); err != nil {
		t.Fatal(err)
	}
	db, err := bbolt.Open(altered, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(poolPaymentsBucket)
		key, raw := bucket.Cursor().First()
		var payment poolPayment
		if err := json.Unmarshal(raw, &payment); err != nil {
			return err
		}
		payment.Inputs[0].Index = 7 // another coin, the same amount
		return putJSON(bucket, key, payment)
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := checkPoolRecordFile(altered); err == nil || !strings.Contains(err.Error(), "not set aside for it") {
		t.Errorf("a copy whose payment spends another coin gave: %v", err)
	}
	_ = os.Remove(altered)
}

// Finding 8. Text from a pool cannot turn the screen round or hide itself.
func TestTextFromAPoolCannotTurnTheScreenRound(t *testing.T) {
	for _, sneaky := range []string{"\u202e", "\u2066", "\u200f", "\u200b", "\u2028", "\ufeff"} {
		if got := shownPlainly("dev"+sneaky+"net", 40); strings.Contains(got, sneaky) {
			t.Errorf("U+%04X was printed: %q", []rune(sneaky)[0], got)
		}
	}
	if got := shownPlainly("devnet ünï", 40); got != "devnet ünï" {
		t.Errorf("ordinary letters were changed: %q", got)
	}
}
