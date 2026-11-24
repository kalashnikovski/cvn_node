package node

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"go.etcd.io/bbolt"
)

// Tests for what an outside review of the pool found (November 2026).

// A block is written into the record before it is placed on the chain, and
// taken out again if it could not be placed.
func TestABlockThatCouldNotBePlacedLeavesTheRecord(t *testing.T) {
	record, addresses := recordWithOwed(t, cvn("50"), []int64{3, 1})
	path := record.db.Path()
	never := Block{Index: 2, Hash: strings.Repeat("cd", 32), Difficulty: 4, Transactions: []Transaction{
		{ID: "TX_COINBASE_H2_test", Outputs: []UTXOOutput{{Recipient: addresses[0], Amount: UnitsAmount(cvn("50"))}}},
	}}
	parts := []poolPart{{Address: addresses[0], Units: cvn("50"), Work: 4, InBlock: true}}
	if _, err := record.addBlock(never, addresses[0], parts, 0, 0, 4, 2000); err != nil {
		t.Fatal(err)
	}
	if len(record.waitingBlocks()) != 1 {
		t.Fatalf("%d blocks wait in the record, want the one just written", len(record.waitingBlocks()))
	}
	record.dropBlock(never.Index, strings.Repeat("ee", 32)) // another block: nothing happens
	if len(record.waitingBlocks()) != 1 {
		t.Fatal("dropping a block the record does not hold removed one it does")
	}
	record.dropBlock(never.Index, never.Hash)
	if len(record.waitingBlocks()) != 0 || record.found != 1 {
		t.Fatalf("after the block was dropped: %d waiting, %d found in all", len(record.waitingBlocks()), record.found)
	}
	// A block that counts is not dropped, whatever is asked.
	record.dropBlock(1, strings.Repeat("ab", 32))
	if record.confirmed != 1 || record.found != 1 {
		t.Error("a block that counts was dropped")
	}
	record.close()
	again, err := openPoolLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.close()
	if again.found != 1 || len(again.waitingBlocks()) != 0 {
		t.Errorf("opened again, the record holds %d blocks, %d of them waiting", again.found, len(again.waitingBlocks()))
	}
}

// The work of a window cannot wrap round to less than nothing.
func TestThePoolsWindowNeverWrapsRound(t *testing.T) {
	if got := poolWindowWork(math.MaxInt); got != math.MaxInt64 {
		t.Errorf("the window at the largest difficulty there is: %d", got)
	}
	if got := poolWindowWork(1000); got != 2000 {
		t.Errorf("the window at difficulty 1000 is %d, want 2000", got)
	}
	if got := poolWindowWork(0); got != 2 {
		t.Errorf("the window at no difficulty is %d", got)
	}
}

// A file that says the chain was read further than the chain goes is not
// believed: the chain is read again from the record's first block.
func TestACursorBeyondTheChainIsNotBelieved(t *testing.T) {
	record, _ := recordWithOwed(t, cvn("50"), []int64{3, 1})
	hashAt := func(int64) string { return "" }
	record.Lock()
	record.readTo = 5000
	record.Unlock()
	if from := record.readFrom(40, 6, hashAt); from != 1 {
		t.Errorf("with the file saying block 5000 and the chain at 40, reading starts at block %d, want 1", from)
	}
	record.Lock()
	record.readTo = 30
	record.Unlock()
	if from := record.readFrom(40, 6, hashAt); from != 25 {
		t.Errorf("with the file saying block 30, reading starts at block %d, want 25", from)
	}
}

// A copy in which a payment was sent and its transfer is gone is refused:
// that payment could never be handed to the network again.
func TestACopyWithASentPaymentAndNoTransferIsRefused(t *testing.T) {
	record, _ := recordWithOwed(t, cvn("50"), []int64{3, 1})
	payments, _, err := record.preparePayments(poolPaymentPlan{Now: 3000, PoolAddress: testAddress("pool"), MinimumUnits: cvn("1"), FeeUnits: cvn("0.001")})
	if err != nil || len(payments) != 1 {
		t.Fatalf("prepared %d payments: %v", len(payments), err)
	}
	if _, recorded, err := record.markSent(payments[0].Serial, Transaction{ID: "TX_the_payment"}, 3001, 101); err != nil || !recorded {
		t.Fatalf("the payment was not written down as sent: %v", err)
	}
	folder := filepath.Join(t.TempDir(), "backups")
	good, _, err := record.writeBackup(folder, 0)
	if err != nil {
		t.Fatalf("a record with a payment sent did not pass its check: %v", err)
	}
	damaged := filepath.Join(t.TempDir(), "damaged.db")
	if err := copyFile(good, damaged); err != nil {
		t.Fatal(err)
	}
	db, err := bbolt.Open(damaged, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(poolPaymentsBucket)
		var payment poolPayment
		if err := json.Unmarshal(bucket.Get(poolSeqKey(payments[0].Serial)), &payment); err != nil {
			return err
		}
		payment.Tx = nil
		return putJSON(bucket, poolSeqKey(payment.Serial), payment)
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := checkPoolRecordFile(damaged); err == nil || !strings.Contains(err.Error(), "no longer holds its transfer") {
		t.Errorf("a copy with a sent payment and no transfer gave: %v", err)
	}
}

// What an address has done in the window is worked out once for each new
// share, however often the pool page is asked for it.
func TestAnAddressesWorkIsKeptBetweenShares(t *testing.T) {
	record, addresses := recordWithOwed(t, cvn("50"), []int64{3, 1})
	if _, work := record.account(addresses[0], 1000); work != 3 {
		t.Fatalf("the first address has done %d, want 3", work)
	}
	record.Lock()
	kept := record.worked.byAddress
	record.Unlock()
	for i := 0; i < 5; i++ {
		record.account(addresses[1], 1000)
	}
	record.Lock()
	same := len(kept) > 0 && len(record.worked.byAddress) == len(kept) && record.worked.seq == record.nextSeq
	record.Unlock()
	if !same {
		t.Error("the work of each address was worked out again with no new share")
	}
	if _, err := record.addShare(addresses[1], 5, 4000, 1000); err != nil {
		t.Fatal(err)
	}
	if _, work := record.account(addresses[1], 1000); work != 6 {
		t.Errorf("after another share of 5 the second address has done %d, want 6", work)
	}
	if _, work := record.account(testAddress("a stranger"), 1000); work != 0 {
		t.Errorf("an address that has done nothing has done %d", work)
	}
}
