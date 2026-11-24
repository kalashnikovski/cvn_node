package node

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// quickToSeeAFileInUse shortens the wait for a record another program holds.
func quickToSeeAFileInUse(t *testing.T) {
	t.Helper()
	saved := poolRecordOpenWait
	poolRecordOpenWait = 300 * time.Millisecond
	t.Cleanup(func() { poolRecordOpenWait = saved })
}

// mineUntilInABlock mines a block at a time until a payment's transfer is in
// the newest block, with nothing on it yet, and returns the payment.
func (p *payingPool) mineUntilInABlock(t *testing.T, serial uint64) poolPayment {
	t.Helper()
	for i := 0; i < 5; i++ {
		p.mine(t, 1)
		poolSettle()
		poolSettlePayments(time.Now())
		if kept, _ := poolLedger.payment(serial); kept.MinedAtHeight != 0 {
			return kept
		}
	}
	t.Fatal("five blocks on, the payment's transfer is in none of them")
	return poolPayment{}
}

// takeCopy makes a copy of the pool's record as the pool does every hour,
// which checks it, and returns the file and what the check found.
func takeCopy(t *testing.T, folder, stage string) (string, poolRecordReport) {
	t.Helper()
	path, report, err := poolLedger.writeBackup(folder, 0)
	if err != nil {
		t.Fatalf("%s: the record did not pass the check a copy has to pass: %v", stage, err)
	}
	return path, report
}

// payEverything prepares every payment that is due and signs and sends it.
func (p *payingPool) payEverything(t *testing.T) []poolPayment {
	t.Helper()
	now := time.Now()
	payments, reason, err := poolPrepare(now)
	if err != nil {
		t.Fatalf("payments could not be prepared (%s): %v", reason, err)
	}
	for _, payment := range payments {
		if _, err := poolAcceptSigned(payment.Serial, p.sign(t, payment), now); err != nil {
			t.Fatalf("payment %d was refused: %v", payment.Serial, err)
		}
	}
	return payments
}

// mineAndSettle mines blocks, doing after each what the pool does every few
// seconds, until none of the payments given is still on its way.
func (p *payingPool) mineAndSettle(t *testing.T, payments []poolPayment) {
	t.Helper()
	for i := 0; i < 12; i++ {
		p.mine(t, 1)
		poolSettle()
		poolReadChain()
		poolSettlePayments(time.Now())
		settled := 0
		for _, payment := range payments {
			if kept, _ := poolLedger.payment(payment.Serial); kept.State == poolPaymentSettled {
				settled++
			}
		}
		if settled == len(payments) {
			return
		}
	}
	t.Fatal("twelve blocks on, a payment has still not settled")
}

// putBack stops using the pool's record, puts a copy in its place, and opens
// it: what an operator does after losing the record.
func (p *payingPool) putBack(t *testing.T, copyPath string) {
	t.Helper()
	poolLedger.close()
	if err := copyFile(copyPath, p.recordPath); err != nil {
		t.Fatal(err)
	}
	again, err := openPoolLedger(p.recordPath)
	if err != nil {
		t.Fatal(err)
	}
	poolLedger = again
	poolResent.Lock()
	poolResent.at, poolResent.searched = make(map[uint64]time.Time), make(map[uint64]int64)
	poolResent.Unlock()
	poolPageKept.Lock()
	poolPageKept.page, poolPageKept.at = poolPage{}, time.Time{}
	poolPageKept.Unlock()
}

// partsAfter adds up an address's parts of the blocks that count and were
// found above a height.
func partsAfter(record *poolRecord, address string, above int64) int64 {
	record.Lock()
	defer record.Unlock()
	var units int64
	for _, block := range record.blocks {
		if block.State != poolBlockConfirmed || block.Height <= above {
			continue
		}
		for _, part := range block.Parts {
			if part.Address == address {
				units += part.Units
			}
		}
	}
	return units
}

// A copy of the record is checked before it is kept. The record of a pool
// passes that check at every stage of a payment, and a copy that has been
// damaged or altered does not.
func TestCopiesOfThePoolsRecordAreCheckedAndAddUp(t *testing.T) {
	pool := startPayingPool(t)
	quickToSeeAFileInUse(t)
	folder := filepath.Join(t.TempDir(), "backups")

	first, report := takeCopy(t, folder, "with one block counted")
	if report.Addresses != 2 || report.OwedUnits != cvn("50") || report.PaidUnits != 0 || report.Confirmed != 1 ||
		report.Blocks != 3 || report.Waiting != 2 || report.CoinUnits != cvn("50") || report.Shares == 0 {
		t.Errorf("the first copy was read as: %s (%+v)", report, report)
	}
	if !strings.HasPrefix(filepath.Base(first), "pool_"+Params.Name+"_") || !strings.HasSuffix(first, ".db") {
		t.Errorf("the copy is named %s", filepath.Base(first))
	}

	// A payment prepared and given back; then one prepared, sent and settled.
	now := time.Now()
	payments, _, err := poolPrepare(now)
	if err != nil || len(payments) != 1 {
		t.Fatalf("prepared %d payments: %v", len(payments), err)
	}
	if _, report = takeCopy(t, folder, "with a payment prepared"); report.PendingUnits != cvn("50") || report.OwedUnits != 0 || report.PaymentsUnderWay != 1 {
		t.Errorf("with a payment prepared the copy says: %s", report)
	}
	if returned, err := poolLedger.returnPayment(payments[0].Serial, GetLatestBlockInternal().Index); err != nil || !returned {
		t.Fatalf("the payment was not given back: %v", err)
	}
	if _, report = takeCopy(t, folder, "with the payment given back"); report.PendingUnits != 0 || report.OwedUnits != cvn("50") || report.PaymentsUnderWay != 0 {
		t.Errorf("with the payment given back the copy says: %s", report)
	}
	payments = pool.payEverything(t)
	takeCopy(t, folder, "with a payment sent")
	pool.mineAndSettle(t, payments)
	last, report := takeCopy(t, folder, "with the payment settled")
	if report.PaidUnits != cvn("50") || report.PendingUnits != 0 || report.Payments != 2 || report.CoinUnits != report.OwedUnits {
		t.Errorf("with the payment settled the copy says: %s (%+v)", report, report)
	}

	// The record in use is not read from under the pool.
	if _, err := checkPoolRecordFile(pool.recordPath); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Errorf("checking the record a pool has open gave: %v", err)
	}

	// Only the newest are kept.
	for i := 0; i < 4; i++ {
		if _, _, err := poolLedger.writeBackup(folder, 3); err != nil {
			t.Fatal(err)
		}
	}
	// A file written a moment ago may still be held by the system's own
	// reading of it, and is then removed at the next try.
	for wait := 0; wait < 100 && len(listPoolBackups(folder)) != 3; wait++ {
		time.Sleep(50 * time.Millisecond)
		prunePoolBackups(folder, 3)
	}
	kept := listPoolBackups(folder)
	if len(kept) != 3 {
		t.Fatalf("asked to keep three copies, the folder holds %d", len(kept))
	}
	for _, old := range []string{first, last} {
		if _, err := os.Stat(old); err == nil {
			t.Errorf("an older copy was kept: %s", filepath.Base(old))
		}
	}
	if poolBackupIsDue(folder, time.Hour, time.Now()) {
		t.Error("a copy is said to be due a moment after one was made")
	}
	if !poolBackupIsDue(folder, time.Hour, time.Now().Add(61*time.Minute)) || !poolBackupIsDue(filepath.Join(folder, "none"), time.Hour, time.Now()) {
		t.Error("no copy is said to be due after an hour, or with none made yet")
	}

	// A copy that has been altered, or cut short, is refused, and says why.
	good := kept[0]
	alter := func(what string, change func(tx *bbolt.Tx) error) string {
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
			t.Fatalf("%s: %v", what, err)
		}
		db.Close()
		_, err = checkPoolRecordFile(path)
		if err == nil {
			t.Errorf("a copy with %s passed the check", what)
			return ""
		}
		return err.Error()
	}
	if said := alter("more owed to one address", func(tx *bbolt.Tx) error {
		var account poolAccount
		bucket := tx.Bucket(poolAccountsBucket)
		_ = json.Unmarshal(bucket.Get([]byte(pool.bob)), &account)
		account.Owed += cvn("5")
		return putJSON(bucket, []byte(pool.bob), account)
	}); !strings.Contains(said, "does not add up") {
		t.Errorf("more owed to one address was refused with: %s", said)
	}
	if said := alter("more earned by and owed to one address", func(tx *bbolt.Tx) error {
		var account poolAccount
		bucket := tx.Bucket(poolAccountsBucket)
		_ = json.Unmarshal(bucket.Get([]byte(pool.bob)), &account)
		account.Owed += cvn("5")
		account.Earned += cvn("5")
		return putJSON(bucket, []byte(pool.bob), account)
	}); !strings.Contains(said, "the blocks that count give it") {
		t.Errorf("more earned than the blocks give was refused with: %s", said)
	}
	if said := alter("a payment taken out", func(tx *bbolt.Tx) error {
		return tx.Bucket(poolPaymentsBucket).Delete(poolSeqKey(payments[0].Serial))
	}); !strings.Contains(said, "settled payments paid it") {
		t.Errorf("a missing payment was refused with: %s", said)
	}
	if said := alter("a coin taken out", func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(poolCoinsBucket)
		key, _ := bucket.Cursor().First()
		return bucket.Delete(key)
	}); !strings.Contains(said, "holds coins of") {
		t.Errorf("a missing coin was refused with: %s", said)
	}
	if said := alter("a block that cannot be read", func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(poolBlocksBucket)
		key, _ := bucket.Cursor().First()
		return bucket.Put(key, []byte("{not a block"))
	}); !strings.Contains(said, "cannot be read") {
		t.Errorf("an unreadable block was refused with: %s", said)
	}
	if said := alter("its accounts missing", func(tx *bbolt.Tx) error {
		return tx.DeleteBucket(poolAccountsBucket)
	}); !strings.Contains(said, "missing from the file") {
		t.Errorf("a file without accounts was refused with: %s", said)
	}
	whole, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	short := filepath.Join(t.TempDir(), "short.db")
	if err := os.WriteFile(short, whole[:len(whole)/3], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := checkPoolRecordFile(short); err == nil {
		t.Error("a copy cut to a third of its length passed the check")
	}
	if _, err := checkPoolRecordFile(filepath.Join(t.TempDir(), "nothing.db")); err == nil {
		t.Error("a file that is not there passed the check")
	}
}

// The record is lost and an older copy is put back. The copy knows nothing
// of a payment made after it. The pool finds that payment in the chain.
// While it is not yet deep enough to be relied on, nothing is paid; once it
// is, it is taken into the record, and what was paid is not paid again.
func TestAnOlderCopyPutBackPaysNobodyTwice(t *testing.T) {
	pool := startPayingPool(t)
	folder := filepath.Join(t.TempDir(), "backups")
	older, _ := takeCopy(t, folder, "before the payment")

	paid := pool.payEverything(t)
	if len(paid) != 1 || paid[0].debits() != cvn("50") {
		t.Fatalf("the first payment was %+v", paid)
	}
	sent := pool.mineUntilInABlock(t, paid[0].Serial)

	// The record is lost here, and the older copy put in its place.
	pool.putBack(t, older)
	if alice, _ := poolLedger.account(pool.alice, 16); alice.Owed != cvn("37.5") || alice.Paid != 0 {
		t.Fatalf("the copy put back says of Alice: %+v", alice)
	}

	// Asked to pay, the pool reads the chain first, and will not pay.
	if _, _, err := poolPrepare(time.Now()); err == nil || !strings.Contains(err.Error(), "does not know of") {
		t.Fatalf("with a transfer the record does not know, preparing payments gave: %v", err)
	}
	waiting := poolLedger.unknownTransfers()
	if len(waiting) != 1 || waiting[0].Tx.ID != sent.TxID || waiting[0].Height != sent.MinedAtHeight {
		t.Fatalf("%d transfer(s) wait to be taken into the record", len(waiting))
	}
	if alice, _ := poolLedger.account(pool.alice, 16); alice.Paid != 0 || alice.Pending != 0 {
		t.Errorf("a transfer with nothing on it was taken into the record already: %+v", alice)
	}
	// The file says the chain was read to just below that transfer, so that
	// a pool started again finds it again.
	poolLedger.Lock()
	readTo := poolLedger.readTo
	poolLedger.Unlock()
	if readTo >= sent.MinedAtHeight {
		t.Errorf("the file says the chain was read to block %d, and a transfer waits in block %d", readTo, sent.MinedAtHeight)
	}
	pages := http.NewServeMux()
	addPoolPages(pages)
	reply := httptest.NewRecorder()
	pages.ServeHTTP(reply, httptest.NewRequest("GET", "/pool", nil))
	if !strings.Contains(reply.Body.String(), "Payments are waiting") {
		t.Error("the pool page does not say that payments are waiting")
	}

	// Two blocks on, the payment is taken into the record.
	pool.mine(t, 2)
	again, reason, err := poolPrepare(time.Now())
	if err != nil {
		t.Fatalf("with the transfer two blocks deep, preparing payments gave (%s): %v", reason, err)
	}
	if len(poolLedger.unknownTransfers()) != 0 || !poolLedger.knowsTransfer(sent.TxID) {
		t.Error("the transfer is still treated as unknown")
	}
	var taken poolPayment
	poolLedger.Lock()
	for _, payment := range poolLedger.payments {
		if payment.FromChain {
			taken = payment
		}
	}
	poolLedger.Unlock()
	if taken.State != poolPaymentSettled || taken.TxID != sent.TxID || taken.debits() != cvn("50") || taken.FeeUnits != cvn("0.001") || taken.MinedAtHeight != sent.MinedAtHeight {
		t.Fatalf("the payment found in the chain was written down as %+v", taken)
	}
	for _, line := range taken.Lines {
		if line.Beyond != 0 {
			t.Errorf("%s is said to have been paid %s more than it was owed, and was owed all of it", line.Address, FormatUnits(line.Beyond))
		}
	}
	// Each has been paid what the lost record paid, once. What is owed or
	// on its way now is for the blocks that have come to count since the
	// first, and not a unit of the 50 already paid.
	alice, _ := poolLedger.account(pool.alice, 16)
	bob, _ := poolLedger.account(pool.bob, 16)
	if alice.Paid != cvn("37.5") || bob.Paid != cvn("12.5") {
		t.Errorf("Alice has been paid %s and Bob %s by the record; they were paid 37.5 and 12.5", FormatUnits(alice.Paid), FormatUnits(bob.Paid))
	}
	if since := partsAfter(poolLedger, pool.alice, 1); alice.Owed+alice.Pending != since {
		t.Errorf("Alice is owed %s with %s on its way; the blocks since the first give her %s", FormatUnits(alice.Owed), FormatUnits(alice.Pending), FormatUnits(since))
	}
	if since := partsAfter(poolLedger, pool.bob, 1); bob.Owed+bob.Pending != since {
		t.Errorf("Bob is owed %s with %s on its way; the blocks since the first give him %s", FormatUnits(bob.Owed), FormatUnits(bob.Pending), FormatUnits(since))
	}
	for name, account := range map[string]poolAccount{"Alice": alice, "Bob": bob} {
		if account.Earned != account.Owed+account.Pending+account.Paid || account.Owed < 0 {
			t.Errorf("%s's account does not add up: %+v", name, account)
		}
	}

	// The payments prepared now go through, and on the chain each address
	// holds what the payments say it received: the first payment once.
	for _, payment := range again {
		if _, err := poolAcceptSigned(payment.Serial, pool.sign(t, payment), time.Now()); err != nil {
			t.Fatalf("payment %d after the copy was put back: %v", payment.Serial, err)
		}
	}
	pool.mineAndSettle(t, again)
	received := map[string]int64{pool.alice: cvn("37.4995"), pool.bob: cvn("12.4995")}
	for _, payment := range again {
		for _, line := range payment.Lines {
			received[line.Address] += line.Receives
		}
	}
	if balanceOf(pool.alice) != received[pool.alice] || balanceOf(pool.bob) != received[pool.bob] {
		t.Errorf("on the chain Alice holds %s (the payments say %s) and Bob %s (%s)",
			FormatUnits(balanceOf(pool.alice)), FormatUnits(received[pool.alice]), FormatUnits(balanceOf(pool.bob)), FormatUnits(received[pool.bob]))
	}
	// A record that has taken a payment in from the chain still passes the
	// check a copy has to pass, and the page says where the payment came from.
	takeCopy(t, folder, "after the older copy was put back and brought up to date")
	poolPageKept.Lock()
	poolPageKept.at = time.Time{}
	poolPageKept.Unlock()
	reply = httptest.NewRecorder()
	pages.ServeHTTP(reply, httptest.NewRequest("GET", "/pool.json", nil))
	var page poolPage
	if err := json.Unmarshal(reply.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	fromChain := 0
	for _, payment := range page.Payments {
		if payment.FromChain {
			fromChain++
		}
	}
	if fromChain != 1 || page.TransfersNotInRecord != 0 {
		t.Errorf("the page shows %d payment(s) found in the chain and %d transfer(s) not in the record", fromChain, page.TransfersNotInRecord)
	}
}

// A payment made after the copy may have paid for blocks found after the
// copy too. The record put back does not know those blocks, so the address
// was paid more than it says was owed. It is then owed nothing, never less
// than nothing, and the difference is written down.
func TestAPaymentBeyondWhatAnOlderCopyKnows(t *testing.T) {
	pool := startPayingPool(t)
	folder := filepath.Join(t.TempDir(), "backups")
	older, olderReport := takeCopy(t, folder, "before more blocks")

	// Blocks found after the copy come to count, and are paid for.
	for i := 0; i < 4; i++ {
		pool.mine(t, 1)
		poolSettle()
	}
	paid := pool.payEverything(t)
	pool.mineAndSettle(t, paid)
	var paidAlice, paidBob int64
	for _, payment := range paid {
		for _, line := range payment.Lines {
			switch line.Address {
			case pool.alice:
				paidAlice += line.Debit
			case pool.bob:
				paidBob += line.Debit
			}
		}
	}
	if paidAlice <= cvn("37.5") && paidBob <= cvn("12.5") {
		t.Fatalf("the test needs a payment for blocks found after the copy: Alice was paid %s and Bob %s", FormatUnits(paidAlice), FormatUnits(paidBob))
	}

	pool.putBack(t, older)
	prepared, _, err := poolPrepare(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// What the copy knew each was owed, with the blocks it knew were waiting.
	knownAlice := cvn("37.5") + partsAfter(poolLedger, pool.alice, 1) - partsAfter(poolLedger, pool.alice, olderReport.NewestBlock)
	knownBob := cvn("12.5") + partsAfter(poolLedger, pool.bob, 1) - partsAfter(poolLedger, pool.bob, olderReport.NewestBlock)
	var beyondAlice, beyondBob int64
	poolLedger.Lock()
	for _, payment := range poolLedger.payments {
		if !payment.FromChain {
			continue
		}
		for _, line := range payment.Lines {
			switch line.Address {
			case pool.alice:
				beyondAlice += line.Beyond
			case pool.bob:
				beyondBob += line.Beyond
			}
		}
	}
	poolLedger.Unlock()
	if paidAlice > knownAlice && beyondAlice != paidAlice-knownAlice {
		t.Errorf("Alice was paid %s and the copy knew of %s: %s is written down as beyond what was owed", FormatUnits(paidAlice), FormatUnits(knownAlice), FormatUnits(beyondAlice))
	}
	if paidBob > knownBob && beyondBob != paidBob-knownBob {
		t.Errorf("Bob was paid %s and the copy knew of %s: %s is written down as beyond what was owed", FormatUnits(paidBob), FormatUnits(knownBob), FormatUnits(beyondBob))
	}
	if beyondAlice+beyondBob == 0 {
		t.Error("nothing was written down as paid beyond what the copy knew")
	}
	alice, _ := poolLedger.account(pool.alice, 16)
	bob, _ := poolLedger.account(pool.bob, 16)
	for name, account := range map[string]poolAccount{"Alice": alice, "Bob": bob} {
		if account.Owed < 0 || account.Earned != account.Owed+account.Pending+account.Paid {
			t.Errorf("%s's account does not add up: %+v", name, account)
		}
	}
	if alice.Paid != paidAlice || bob.Paid != paidBob {
		t.Errorf("the record says Alice was paid %s and Bob %s; they were paid %s and %s", FormatUnits(alice.Paid), FormatUnits(bob.Paid), FormatUnits(paidAlice), FormatUnits(paidBob))
	}
	// Whoever was paid beyond what the copy knew is owed nothing, and
	// nothing is prepared for them.
	if beyondAlice > 0 && (alice.Owed != 0 || alice.Pending != 0) {
		t.Errorf("Alice was paid more than the copy knew she was owed, and is still owed %s with %s on its way", FormatUnits(alice.Owed), FormatUnits(alice.Pending))
	}
	if beyondBob > 0 && (bob.Owed != 0 || bob.Pending != 0) {
		t.Errorf("Bob was paid more than the copy knew he was owed, and is still owed %s with %s on its way", FormatUnits(bob.Owed), FormatUnits(bob.Pending))
	}
	for _, payment := range prepared {
		for _, line := range payment.Lines {
			if (line.Address == pool.alice && beyondAlice > 0) || (line.Address == pool.bob && beyondBob > 0) {
				t.Errorf("%s CVN is being paid to an address that was paid in full already", FormatUnits(line.Debit))
			}
		}
	}
	takeCopy(t, folder, "after a payment beyond what the copy knew")
}

// A transfer sent by hand with the pool's key, from a wallet and not by the
// pool. It is a payment all the same: what it paid comes off what the record
// says is owed, and a payment of the pool's own that it overtook is undone.
func TestATransferSentByHandFromThePoolsAddress(t *testing.T) {
	pool := startPayingPool(t)
	reward := GetBlockByHeightFromDB(1).Transactions[0].ID

	// The pool has prepared a payment, and has another that was signed and
	// written down as sent but never reached the network.
	prepared, _, err := poolPrepare(time.Now())
	if err != nil || len(prepared) != 1 {
		t.Fatalf("prepared %d payments: %v", len(prepared), err)
	}
	if _, recorded, err := poolLedger.markSent(prepared[0].Serial, pool.sign(t, prepared[0]), time.Now().Unix(), GetLatestBlockInternal().Index); err != nil || !recorded {
		t.Fatalf("the payment was not written down as sent: %v", err)
	}
	if alice, _ := poolLedger.account(pool.alice, 16); alice.Pending != cvn("37.5") {
		t.Fatalf("Alice's account before the transfer by hand: %+v", alice)
	}

	// By hand: 10 CVN to Alice from the same coin, the rest back to the pool.
	byHand, err := signTransfer(pool.privateKey, pool.publicKey, pool.address,
		[]UTXOInput{{SourceTxID: reward, Index: 0, AmountUnits: cvn("50")}},
		[]UTXOOutput{{Recipient: pool.alice, Amount: UnitsAmount(cvn("10"))}, {Recipient: pool.address, Amount: UnitsAmount(cvn("39.999"))}},
		cvn("0.001"))
	if err != nil {
		t.Fatal(err)
	}
	if err := AdmitTransactionToMempool(byHand); err != nil {
		t.Fatalf("the transfer by hand was not taken: %v", err)
	}
	// A block at a time, until it is in the newest block.
	for i := 0; i < 5 && len(poolLedger.unknownTransfers()) == 0; i++ {
		pool.mine(t, 1)
		poolSettle()
		if taken, _ := poolReadChain(); taken != 0 {
			t.Fatal("a transfer with nothing on it was taken into the record")
		}
	}
	if waiting := poolLedger.unknownTransfers(); len(waiting) != 1 || waiting[0].Tx.ID != byHand.ID || waiting[0].Height != GetLatestBlockInternal().Index {
		t.Fatalf("the transfer by hand was not noticed: %d waiting", len(waiting))
	}
	if _, _, err := poolPrepare(time.Now()); err == nil {
		t.Error("payments were prepared while the transfer by hand was not in the record")
	}
	pool.mine(t, 2)
	poolSettle()
	if taken, _ := poolReadChain(); taken != 1 {
		t.Fatalf("two blocks on, %d transfers were taken into the record", taken)
	}

	// The payment it overtook is undone, and its amounts are owed again,
	// less what the transfer by hand paid: 10 and the whole fee.
	if overtaken, _ := poolLedger.payment(prepared[0].Serial); overtaken.State != poolPaymentReturned || overtaken.Tx != nil {
		t.Errorf("the payment that can never be mined now is %s", overtaken.State)
	}
	alice, _ := poolLedger.account(pool.alice, 16)
	bob, _ := poolLedger.account(pool.bob, 16)
	laterAlice, laterBob := partsAfter(poolLedger, pool.alice, 1), partsAfter(poolLedger, pool.bob, 1)
	if alice.Paid != cvn("10.001") || alice.Pending != 0 || alice.Owed != cvn("37.5")+laterAlice-cvn("10.001") {
		t.Errorf("Alice after the transfer by hand: %+v (blocks since give her %s)", alice, FormatUnits(laterAlice))
	}
	if bob.Paid != 0 || bob.Pending != 0 || bob.Owed != cvn("12.5")+laterBob {
		t.Errorf("Bob after the transfer by hand: %+v (blocks since give him %s)", bob, FormatUnits(laterBob))
	}
	// The coin it spent has left the record, and what it sent back has joined.
	poolLedger.Lock()
	_, spentKept := poolLedger.coins[utxoKey(reward, 0)]
	back, backKept := poolLedger.coins[utxoKey(byHand.ID, 1)]
	poolLedger.Unlock()
	if spentKept || !backKept || back.Units != cvn("39.999") || back.HeldBy != 0 {
		t.Errorf("the record's coins after the transfer by hand: spent coin kept %v, coin sent back %+v (kept %v)", spentKept, back, backKept)
	}
	// Paying goes on from there: what is prepared now is what is owed now.
	next, reason, err := poolPrepare(time.Now())
	if err != nil || len(next) == 0 {
		t.Fatalf("after the transfer by hand was taken in, preparing payments gave %d (%s): %v", len(next), reason, err)
	}
	var preparedNow int64
	for _, payment := range next {
		preparedNow += payment.debits()
	}
	if preparedNow != alice.Owed+bob.Owed {
		t.Errorf("%s CVN was prepared; Alice was owed %s and Bob %s", FormatUnits(preparedNow), FormatUnits(alice.Owed), FormatUnits(bob.Owed))
	}
	takeCopy(t, filepath.Join(t.TempDir(), "backups"), "after a transfer by hand")
}

// How far the chain has been read is kept in the record's file, and never
// says more than is safe to start again from.
func TestThePoolRemembersHowFarItHasReadTheChain(t *testing.T) {
	record, _ := recordWithOwed(t, cvn("50"), []int64{3, 1})
	path := record.db.Path()
	chain := map[int64]string{}
	for height := int64(1); height <= 40; height++ {
		chain[height] = strings.Repeat("a", 60) + strings.ToUpper(string(rune('a'+height%26))) + "000"
	}
	hashAt := func(height int64) string { return chain[height] }
	read := func(from, tip int64, found ...poolUnknownTransfer) {
		t.Helper()
		hashes := map[int64]string{}
		for height := from; height <= tip; height++ {
			hashes[height] = chain[height]
		}
		if err := record.noteRead(from, tip, 6, hashes, found); err != nil {
			t.Fatal(err)
		}
	}

	// A record that holds a block and has never read the chain reads from
	// that block.
	if from := record.readFrom(20, 6, hashAt); from != 1 {
		t.Errorf("a record that has read nothing starts at block %d, want 1", from)
	}
	read(1, 20)
	if from := record.readFrom(20, 6, hashAt); from != 21 {
		t.Errorf("with nothing new, reading starts at block %d, want 21", from)
	}
	if from := record.readFrom(23, 6, hashAt); from != 21 {
		t.Errorf("with three new blocks, reading starts at block %d, want 21", from)
	}
	// Blocks 19 and 20 are replaced: reading goes back to 19.
	chain[19], chain[20] = "replaced-19", "replaced-20"
	if from := record.readFrom(23, 6, hashAt); from != 19 {
		t.Errorf("with blocks 19 and 20 replaced, reading starts at block %d, want 19", from)
	}
	// A transfer the record does not know is found in block 22.
	unknown := poolUnknownTransfer{Tx: Transaction{ID: "TX_not_in_the_record"}, Height: 22, Block: chain[22]}
	read(19, 23, unknown)
	if waiting := record.unknownTransfers(); len(waiting) != 1 || waiting[0].Height != 22 {
		t.Fatalf("%d transfers wait", len(waiting))
	}
	record.Lock()
	readTo := record.readTo
	record.Unlock()
	if readTo != 21 {
		t.Errorf("with a transfer waiting in block 22 the file says the chain was read to %d, want 21", readTo)
	}
	// Block 22 is replaced by one without it: the transfer is waited for no
	// longer, and the file moves on.
	chain[22], chain[23] = "replaced-22", "replaced-23"
	if from := record.readFrom(24, 6, hashAt); from != 22 {
		t.Errorf("with block 22 replaced, reading starts at block %d, want 22", from)
	}
	read(22, 24)
	if waiting := record.unknownTransfers(); len(waiting) != 0 {
		t.Errorf("a transfer in a block that was replaced still waits")
	}

	// Opened again, the record goes back six blocks from where the file
	// says it had got to.
	record.close()
	again, err := openPoolLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.close)
	if again.readTo != 24 {
		t.Errorf("opened again, the file says the chain was read to %d, want 24", again.readTo)
	}
	if from := again.readFrom(30, 6, hashAt); from != 19 {
		t.Errorf("opened again, reading starts at block %d, want 19", from)
	}
}

// Putting a copy back with the command: the copy is checked, a pool that is
// running is not touched, and the file replaced is kept.
func TestACopyIsPutBackInPlaceOfThePoolsRecord(t *testing.T) {
	record, _ := recordWithOwed(t, cvn("50"), []int64{3, 1})
	quickToSeeAFileInUse(t)
	// The pool's record lives beside the chain's file: for this test, in
	// a folder of its own.
	savedChainFile := BlockchainFile
	BlockchainFile = filepath.Join(t.TempDir(), "chain.db")
	t.Cleanup(func() { BlockchainFile = savedChainFile })
	copyPath, _, err := record.writeBackup(filepath.Join(t.TempDir(), "backups"), 0)
	if err != nil {
		t.Fatal(err)
	}
	place := poolLedgerPath()

	report, kept, err := restorePoolBackup(copyPath)
	if err != nil || kept != "" || report.OwedUnits != cvn("50") {
		t.Fatalf("putting the copy back where there was no record: kept %q, %v", kept, err)
	}
	if again, err := checkPoolRecordFile(place); err != nil || again.OwedUnits != cvn("50") {
		t.Errorf("the record put in place: %v", err)
	}
	// With a record already there, that one is kept under another name.
	_, kept, err = restorePoolBackup(copyPath)
	if err != nil || kept == "" {
		t.Fatalf("putting the copy back over a record: kept %q, %v", kept, err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("the record that was replaced was not kept: %v", err)
	}
	// A pool that has the record open is left alone.
	running, err := openPoolLedger(place)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := restorePoolBackup(copyPath); err == nil || !strings.Contains(err.Error(), "running") {
		t.Errorf("putting a copy back under a running pool gave: %v", err)
	}
	running.close()
	// A file that is not a whole record changes nothing.
	broken := filepath.Join(t.TempDir(), "broken.db")
	if err := os.WriteFile(broken, []byte("not a record"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(place)
	if _, _, err := restorePoolBackup(broken); err == nil {
		t.Error("a file that is not a record was put in place of one")
	}
	if after, _ := os.ReadFile(place); string(after) != string(before) {
		t.Error("a refused copy changed the record")
	}
}
