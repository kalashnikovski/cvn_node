package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// payingPool is a pool on a test chain whose address has a real key, with
// two stand-in miners, after one block has come to count: Alice is owed
// 37.5 CVN and Bob 12.5.
type payingPool struct {
	privateKey, publicKey, address string
	alice, bob                     string
	miner                          *fakeMiner // Bob's connection
	next                           minerJob   // Bob's work for the next block
	recordPath                     string
}

func startPayingPool(t *testing.T) *payingPool {
	t.Helper()
	startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 64}})
	Params.HeartbeatInitialDifficulty = 64
	confirmAfter(t, 2)
	privateKey, publicKey, address, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	server, recordPath := startPoolForTest(t, 4)
	pool := &payingPool{privateKey: privateKey, publicKey: publicKey, address: strings.TrimSpace(address),
		alice: testAddress("alice"), bob: testAddress("bob"), recordPath: recordPath}
	poolAddress = pool.address
	t.Cleanup(func() {
		poolResent.Lock()
		poolResent.at, poolResent.searched = make(map[uint64]time.Time), make(map[uint64]int64)
		poolResent.Unlock()
	})

	first := connectFakeMiner(t, server)
	aliceJob := poolLogin(t, first, pool.alice)
	pool.miner = connectFakeMiner(t, server)
	bobJob := poolLogin(t, pool.miner, pool.bob)
	shareOnly := func(work string) bool { return meets(4)(work) && !meets(64)(work) }
	var from uint32
	for i := 0; i < 3; i++ {
		var nonce string
		nonce, from = first.searchWork(aliceJob, from, shareOnly)
		if _, errorText, _ := first.call("submit", map[string]string{"job_id": aliceJob.JobID, "nonce": nonce}); errorText != "" {
			t.Fatalf("share refused: %s", errorText)
		}
	}
	first.conn.Close()
	blockNonce, _ := pool.miner.searchWork(bobJob, 0, meets(64))
	_, errorText, pushed := pool.miner.call("submit", map[string]string{"job_id": bobJob.JobID, "nonce": blockNonce})
	if errorText != "" || GetLatestBlockInternal().Index != 1 {
		t.Fatalf("block 1 was not found: %s", errorText)
	}
	for _, job := range pushed {
		if job.Height == 2 {
			pool.next = job
		}
	}
	for pool.next.Height != 2 {
		pool.next = pool.miner.waitForPushedJob()
	}
	pool.mine(t, 2)
	if confirmed, lost := poolSettle(); confirmed != 1 || lost != 0 {
		t.Fatalf("block 1 should count by now: %d confirmed, %d lost", confirmed, lost)
	}
	if alice, _ := poolLedger.account(pool.alice, 64); alice.Owed != cvn("37.5") {
		t.Fatalf("Alice is owed %s, want 37.5", FormatUnits(alice.Owed))
	}
	return pool
}

// mine has Bob find the next `blocks` blocks.
func (p *payingPool) mine(t *testing.T, blocks int) {
	t.Helper()
	for i := 0; i < blocks; i++ {
		p.next = p.miner.mineThroughPool(p.next)
	}
}

func (p *payingPool) sign(t *testing.T, payment poolPayment) Transaction {
	t.Helper()
	signed, err := signTransfer(p.privateKey, p.publicKey, p.address, payment.Inputs, payment.Outputs, payment.FeeUnits)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestPoolPaysWhatIsOwed(t *testing.T) {
	pool := startPayingPool(t)
	now := time.Now()

	// The node prepares one transfer paying both, from the reward of the
	// block that counts. The fee of 0.001 is shared: 0.0005 each.
	payments, reason, err := poolPrepare(now)
	if err != nil || len(payments) != 1 {
		t.Fatalf("prepared %d payments (%s): %v", len(payments), reason, err)
	}
	payment := payments[0]
	if payment.Serial != 1 || payment.State != poolPaymentPrepared || payment.FeeUnits != cvn("0.001") || payment.ChangeUnits != 0 ||
		len(payment.Inputs) != 1 || payment.Inputs[0].AmountUnits != cvn("50") || len(payment.Outputs) != 2 || len(payment.Lines) != 2 {
		t.Fatalf("the payment was prepared as %+v", payment)
	}
	if payment.Inputs[0].SourceTxID != GetBlockByHeightFromDB(1).Transactions[0].ID {
		t.Error("the payment does not spend the reward of the block that counts")
	}
	if alice, bob := payment.Lines[0], payment.Lines[1]; alice.Address != pool.alice || alice.Debit != cvn("37.5") || alice.Receives != cvn("37.4995") ||
		bob.Address != pool.bob || bob.Debit != cvn("12.5") || bob.Receives != cvn("12.4995") {
		t.Errorf("the lines are %+v", payment.Lines)
	}
	// What was owed is now on its way, and nothing more is prepared for it.
	alice, _ := poolLedger.account(pool.alice, 64)
	if alice.Owed != 0 || alice.Pending != cvn("37.5") || alice.Paid != 0 || alice.Earned != cvn("37.5") {
		t.Errorf("after preparing, Alice's account is %+v", alice)
	}
	if again, why, _ := poolPrepare(now); len(again) != 0 || why == "" {
		t.Errorf("a second run prepared %d payments (%q) for amounts already on their way", len(again), why)
	}

	// Only the payment as prepared, signed by the pool's address, is taken.
	strangerKey, strangerPublic, strangerAddress, _ := GenerateKeyPair()
	forged, err := signTransfer(strangerKey, strangerPublic, strings.TrimSpace(strangerAddress), payment.Inputs, payment.Outputs, payment.FeeUnits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := poolAcceptSigned(payment.Serial, forged, now); err == nil {
		t.Error("a transfer signed by another key was taken as the pool's payment")
	}
	swapped := []UTXOOutput{{Recipient: pool.bob, Amount: payment.Outputs[0].Amount}, {Recipient: pool.alice, Amount: payment.Outputs[1].Amount}}
	changed, err := signTransfer(pool.privateKey, pool.publicKey, pool.address, payment.Inputs, swapped, payment.FeeUnits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := poolAcceptSigned(payment.Serial, changed, now); err == nil {
		t.Error("a transfer paying other amounts than were prepared was taken")
	}
	if _, err := poolAcceptSigned(77, pool.sign(t, payment), now); err == nil {
		t.Error("a transfer for a payment that does not exist was taken")
	}
	if kept, _ := poolLedger.payment(payment.Serial); kept.State != poolPaymentPrepared {
		t.Fatalf("after three refusals the payment is %s", kept.State)
	}

	signed := pool.sign(t, payment)
	id, err := poolAcceptSigned(payment.Serial, signed, now)
	if err != nil || id != signed.ID {
		t.Fatalf("the signed payment was refused: %v", err)
	}
	MempoolMatrix.RLock()
	_, waiting := MempoolMatrix.Transactions[signed.ID]
	MempoolMatrix.RUnlock()
	if kept, _ := poolLedger.payment(payment.Serial); kept.State != poolPaymentSent || kept.TxID != signed.ID || kept.Tx == nil || !waiting {
		t.Fatalf("after signing the payment is %s and its transfer waiting for a block: %v", kept.State, waiting)
	}
	// Handed in twice, it is sent once. A different signing of it is refused.
	if id, err := poolAcceptSigned(payment.Serial, signed, now); err != nil || id != signed.ID {
		t.Errorf("the same transfer handed in again: %v", err)
	}
	time.Sleep(1100 * time.Millisecond) // a later date makes a different transfer
	if _, err := poolAcceptSigned(payment.Serial, pool.sign(t, payment), now); err == nil {
		t.Error("a second signing of a payment already sent was taken")
	}

	// Sent is not paid. It is paid when its transfer is in a block with
	// two more on it.
	if settled := poolSettlePayments(now); settled != 0 {
		t.Fatal("a payment not yet in a block was settled")
	}
	mined := int64(0)
	for i := 0; i < 4 && mined == 0; i++ {
		pool.mine(t, 1)
		poolSettlePayments(now)
		kept, _ := poolLedger.payment(payment.Serial)
		mined = kept.MinedAtHeight
	}
	if mined == 0 {
		t.Fatal("four blocks on, the payment's transfer is in none of them")
	}
	if kept, _ := poolLedger.payment(payment.Serial); kept.State != poolPaymentSent || kept.MinedInBlock != GetBlockByHeightFromDB(mined).Hash {
		t.Fatalf("mined in block %d, the payment is %s", mined, kept.State)
	}
	if alice, _ = poolLedger.account(pool.alice, 64); alice.Pending != cvn("37.5") || alice.Paid != 0 {
		t.Errorf("before it is deep enough, Alice's account is %+v", alice)
	}
	for GetLatestBlockInternal().Index < mined+2 {
		pool.mine(t, 1)
	}
	if settled := poolSettlePayments(now); settled != 1 {
		t.Fatalf("with two blocks on its transfer, %d payments were settled", settled)
	}
	kept, _ := poolLedger.payment(payment.Serial)
	if kept.State != poolPaymentSettled || kept.Tx != nil || kept.TxID != signed.ID {
		t.Errorf("the settled payment is %+v", kept)
	}
	alice, _ = poolLedger.account(pool.alice, 64)
	bob, _ := poolLedger.account(pool.bob, 64)
	if alice.Paid != cvn("37.5") || alice.Pending != 0 || bob.Paid != cvn("12.5") || bob.Pending != 0 {
		t.Errorf("after settling: Alice %+v, Bob %+v", alice, bob)
	}
	for _, account := range []poolAccount{alice, bob} {
		if account.Earned != account.Paid+account.Pending+account.Owed {
			t.Errorf("an account does not add up: %+v", account)
		}
	}
	// On the chain each holds what was owed less its half of the fee.
	if balanceOf(pool.alice) != cvn("37.4995") || balanceOf(pool.bob) != cvn("12.4995") {
		t.Errorf("on the chain Alice holds %s and Bob %s", FormatUnits(balanceOf(pool.alice)), FormatUnits(balanceOf(pool.bob)))
	}
	// The coin that paid is gone from the record, and settling again does nothing.
	if _, _, still := poolLedger.settlePayment(payment.Serial, 99); still != nil {
		t.Error(still)
	}
	if settled := poolSettlePayments(now); settled != 0 {
		t.Error("a settled payment was settled again")
	}
	poolLedger.Lock()
	_, spentCoinKept := poolLedger.coins[utxoKey(payment.Inputs[0].SourceTxID, 0)]
	poolLedger.Unlock()
	if spentCoinKept {
		t.Error("the coin the payment spent is still in the record")
	}

	// The pages show it, and it is all there when the record is opened again.
	pages := http.NewServeMux()
	addPoolPages(pages)
	reply := httptest.NewRecorder()
	pages.ServeHTTP(reply, httptest.NewRequest("GET", "/pool.json", nil))
	var page poolPage
	if err := json.Unmarshal(reply.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.PaidUnits != cvn("50") || page.PaymentsSettled != 1 || len(page.Payments) != 1 || page.Payments[0].State != poolPaymentSettled ||
		page.Payments[0].TxID != signed.ID || page.Payments[0].Units != cvn("50") || page.MinimumPayment != "1" || page.PaymentFee != "0.001" {
		t.Errorf("pool.json shows paid %s, %d payments: %+v", page.Paid, page.PaymentsSettled, page.Payments)
	}
	pool.miner.conn.Close()
	for i := 0; i < 200 && atomic.LoadInt32(&externalMinersConnected) != 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	poolLedger.close()
	again, err := openPoolLedger(pool.recordPath)
	if err != nil {
		t.Fatal(err)
	}
	poolLedger = again
	if kept, found := again.payment(payment.Serial); !found || kept.State != poolPaymentSettled || again.nextPayment != 2 || again.paymentsSettled != 1 {
		t.Errorf("opened again: payment %+v, next number %d", kept, again.nextPayment)
	}
	if alice, _ = again.account(pool.alice, 64); alice.Paid != cvn("37.5") {
		t.Errorf("opened again, Alice has been paid %s", FormatUnits(alice.Paid))
	}
}

func TestPoolPaymentNeverSignedIsUndone(t *testing.T) {
	pool := startPayingPool(t)
	now := time.Now()
	payments, _, err := poolPrepare(now)
	if err != nil || len(payments) != 1 {
		t.Fatalf("prepared %d payments: %v", len(payments), err)
	}
	// Nine minutes on it still stands; eleven minutes on it is undone.
	poolSettlePayments(now.Add(9 * time.Minute))
	if kept, _ := poolLedger.payment(1); kept.State != poolPaymentPrepared {
		t.Fatalf("nine minutes on, the payment is %s", kept.State)
	}
	poolSettlePayments(now.Add(11 * time.Minute))
	kept, _ := poolLedger.payment(1)
	alice, _ := poolLedger.account(pool.alice, 64)
	if kept.State != poolPaymentReturned || alice.Owed != cvn("37.5") || alice.Pending != 0 {
		t.Fatalf("eleven minutes on, the payment is %s and Alice's account %+v", kept.State, alice)
	}
	// Signed now, it is too late: nothing is sent.
	if _, err := poolAcceptSigned(1, pool.sign(t, payments[0]), now.Add(12*time.Minute)); err == nil {
		t.Error("a payment that had been undone was sent after all")
	}
	MempoolMatrix.RLock()
	waiting := len(MempoolMatrix.Transactions)
	MempoolMatrix.RUnlock()
	if waiting != 0 {
		t.Errorf("%d transfers are waiting for a block; none was sent", waiting)
	}
	// The amounts and the coin are there for the next run, as a new payment.
	payments, _, err = poolPrepare(now.Add(12 * time.Minute))
	if err != nil || len(payments) != 1 || payments[0].Serial != 2 || payments[0].Inputs[0].AmountUnits != cvn("50") {
		t.Fatalf("the next run prepared %+v: %v", payments, err)
	}
	// The command can also give a payment back at once.
	if done, err := poolLedger.returnPayment(2, 3); err != nil || !done {
		t.Fatalf("giving a prepared payment back: %v %v", done, err)
	}
	if done, _ := poolLedger.returnPayment(2, 3); done {
		t.Error("a payment was given back twice")
	}
	if free, held, freeUnits, _ := poolLedger.coinsHeld(); free == 0 || held != 0 || freeUnits < cvn("50") {
		t.Errorf("after giving back: %d coins free (%s), %d held", free, FormatUnits(freeUnits), held)
	}
}

// recordWithOwed makes a record in which `addresses` addresses share the
// reward of one block that counts, in proportion to the weights given.
func recordWithOwed(t *testing.T, rewardUnits int64, weights []int64) (*poolRecord, []string) {
	t.Helper()
	onNetwork(t, "devnet")
	record, err := openPoolLedger(filepath.Join(t.TempDir(), "pool.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(record.close)
	var addresses []string
	var place uint64
	var total int64
	for i, weight := range weights {
		address := testAddress(fmt.Sprintf("member %03d", i))
		addresses = append(addresses, address)
		if place, err = record.addShare(address, weight, 1000, 1); err != nil {
			t.Fatal(err)
		}
		total += weight
	}
	block := Block{Index: 1, Hash: strings.Repeat("ab", 32), Difficulty: int(total), Transactions: []Transaction{
		{ID: "TX_COINBASE_H1_test", Outputs: []UTXOOutput{{Recipient: testAddress("pool"), Amount: UnitsAmount(rewardUnits)}}},
	}}
	if _, err := record.addBlock(block, addresses[0], place, 1000); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := record.decide(1, block.Hash, true, 101, "TX_COINBASE_H1_test"); err != nil || !changed {
		t.Fatalf("the block did not come to count: %v", err)
	}
	return record, addresses
}

func TestPoolPaymentsAreBuiltInTransfersOfFortyNine(t *testing.T) {
	// Sixty addresses with an equal part of 120 CVN: 2 CVN each.
	weights := make([]int64, 60)
	for i := range weights {
		weights[i] = 10
	}
	record, addresses := recordWithOwed(t, cvn("120"), weights)
	plan := poolPaymentPlan{Now: 2000, PoolAddress: testAddress("pool"), MinimumUnits: cvn("1"), FeeUnits: cvn("0.001")}
	payments, reason, err := record.preparePayments(plan)
	if err != nil {
		t.Fatal(err)
	}
	// One coin of 120 CVN: the first transfer pays 49 addresses 98 CVN and
	// takes the coin. The other eleven wait until its change has settled.
	if len(payments) != 1 || len(payments[0].Lines) != 49 || len(payments[0].Outputs) != 50 || payments[0].ChangeUnits != cvn("22") || reason == "" {
		t.Fatalf("prepared %d payments (%q); the first pays %d addresses with %s change", len(payments), reason, len(payments[0].Lines), FormatUnits(payments[0].ChangeUnits))
	}
	// 0.001 CVN is 100,000 units; among 49 that is 2,040 each and 40 over,
	// which fall one each on the first forty.
	var debited, received int64
	for i, line := range payments[0].Lines {
		fee := int64(2040)
		if i < 40 {
			fee = 2041
		}
		if line.Debit != cvn("2") || line.Receives != cvn("2")-fee {
			t.Fatalf("line %d: debit %d, receives %d", i, line.Debit, line.Receives)
		}
		debited += line.Debit
		received += line.Receives
	}
	if debited-received != cvn("0.001") || payments[0].Inputs[0].AmountUnits != received+cvn("0.001")+payments[0].ChangeUnits {
		t.Errorf("the transfer does not add up: debited %d, received %d", debited, received)
	}
	if last := payments[0].Outputs[49]; last.Recipient != testAddress("pool") || last.Amount.Units() != cvn("22") {
		t.Errorf("the change output is %+v", last)
	}
	if err := poolPaymentLooksRight(payments[0], testAddress("pool"), cvn("0.001")); err != nil {
		t.Errorf("the paying command would refuse it: %v", err)
	}

	// Once the first has settled, its change pays the rest.
	signed := Transaction{ID: "TX_FIRST_PAYMENT"}
	if _, ok, err := record.markSent(1, signed, 2001, 101); err != nil || !ok {
		t.Fatal(err)
	}
	if again, _, _ := record.preparePayments(plan); len(again) != 0 {
		t.Fatal("the change of a payment not yet settled was spent")
	}
	if err := record.noteMined(1, 102, strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := record.settlePayment(1, 202); err != nil || !ok {
		t.Fatalf("settling: %v", err)
	}
	payments, reason, err = record.preparePayments(plan)
	if err != nil || len(payments) != 1 || len(payments[0].Lines) != 11 || payments[0].ChangeUnits != 0 || reason != "" {
		t.Fatalf("the second run prepared %+v (%q): %v", payments, reason, err)
	}
	if input := payments[0].Inputs[0]; input.SourceTxID != "TX_FIRST_PAYMENT" || input.Index != 49 || input.AmountUnits != cvn("22") {
		t.Errorf("the second payment spends %+v, not the change of the first", input)
	}
	paidOnce := map[string]bool{}
	for _, line := range payments[0].Lines {
		paidOnce[line.Address] = true
	}
	if len(paidOnce) != 11 {
		t.Error("an address appears twice in one payment")
	}
	for _, address := range addresses {
		account, _ := record.account(address, 1)
		if account.Earned != cvn("2") || account.Earned != account.Paid+account.Pending+account.Owed || account.Owed != 0 {
			t.Fatalf("%s: %+v", address, account)
		}
	}
}

func TestPoolLeavesSmallAmountsUntilTheyGrow(t *testing.T) {
	// 50 CVN among three: 30, 19.5 and 0.5.
	record, addresses := recordWithOwed(t, cvn("50"), []int64{600, 390, 10})
	plan := poolPaymentPlan{Now: 2000, PoolAddress: testAddress("pool"), MinimumUnits: cvn("1"), FeeUnits: cvn("0.001")}
	payments, _, err := record.preparePayments(plan)
	if err != nil || len(payments) != 1 || len(payments[0].Lines) != 2 {
		t.Fatalf("prepared %+v: %v", payments, err)
	}
	if payments[0].ChangeUnits != cvn("0.5") || payments[0].Lines[0].Address != addresses[0] {
		t.Errorf("change %s, first line %s", FormatUnits(payments[0].ChangeUnits), payments[0].Lines[0].Address)
	}
	small, _ := record.account(addresses[2], 1)
	if small.Owed != cvn("0.5") || small.Pending != 0 {
		t.Errorf("the address owed half a coin: %+v", small)
	}
	// A coin the chain no longer holds, or that a waiting transfer already
	// spends, is not used.
	if done, err := record.returnPayment(1, 101); err != nil || !done {
		t.Fatal(err)
	}
	plan.Usable = func(poolCoin) bool { return false }
	payments, reason, err := record.preparePayments(plan)
	if err != nil || len(payments) != 0 || reason == "" {
		t.Errorf("with no usable coin, prepared %d payments (%q): %v", len(payments), reason, err)
	}
	// A minimum the fee would eat is refused outright.
	plan.MinimumUnits = cvn("0.001")
	if _, _, err := record.preparePayments(plan); err == nil {
		t.Error("a minimum payment no larger than the fee was accepted")
	}
}

func TestPayingCommandRefusesAPaymentThatDoesNotHangTogether(t *testing.T) {
	onNetwork(t, "devnet")
	own, other := testAddress("pool"), testAddress("someone")
	good := poolPayment{
		Serial: 1, FeeUnits: cvn("0.001"), ChangeUnits: cvn("9"),
		Lines:  []poolPaymentLine{{Address: other, Debit: cvn("1"), Receives: cvn("0.999")}},
		Inputs: []UTXOInput{{SourceTxID: "TX_A", Index: 0, AmountUnits: cvn("10")}},
		Outputs: []UTXOOutput{
			{Recipient: other, Amount: UnitsAmount(cvn("0.999"))},
			{Recipient: own, Amount: UnitsAmount(cvn("9"))},
		},
	}
	if err := poolPaymentLooksRight(good, own, cvn("0.001")); err != nil {
		t.Fatalf("a sound payment was refused: %v", err)
	}
	wrong := map[string]func(*poolPayment){
		"the change goes elsewhere":      func(p *poolPayment) { p.Outputs[1].Recipient = other },
		"the fee is large":               func(p *poolPayment) { p.FeeUnits = cvn("5") },
		"an output pays another address": func(p *poolPayment) { p.Outputs[0].Recipient = own },
		"an output pays more":            func(p *poolPayment) { p.Outputs[0].Amount = UnitsAmount(cvn("5")) },
		"it spends more than it accounts for": func(p *poolPayment) {
			p.Inputs = append(p.Inputs, UTXOInput{SourceTxID: "TX_B", AmountUnits: cvn("40")})
		},
		"it pays nobody": func(p *poolPayment) { p.Lines, p.Outputs = nil, p.Outputs[1:] },
		"it has an output too many": func(p *poolPayment) {
			p.Outputs = append(p.Outputs, UTXOOutput{Recipient: other, Amount: UnitsAmount(1)})
		},
	}
	for what, change := range wrong {
		copied := good
		copied.Lines = append([]poolPaymentLine(nil), good.Lines...)
		copied.Inputs = append([]UTXOInput(nil), good.Inputs...)
		copied.Outputs = append([]UTXOOutput(nil), good.Outputs...)
		change(&copied)
		if err := poolPaymentLooksRight(copied, own, cvn("0.001")); err == nil {
			t.Errorf("a payment in which %s was accepted", what)
		}
	}
}

func TestPayPortAnswersOnlyThisMachine(t *testing.T) {
	pages := poolPayAPI()
	ask := func(method, from string, headers map[string]string) int {
		request := httptest.NewRequest(method, "/prepare", bytes.NewReader([]byte("{}")))
		request.RemoteAddr = from
		request.Header.Set("Content-Type", "application/json")
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		reply := httptest.NewRecorder()
		pages.ServeHTTP(reply, request)
		return reply.Code
	}
	if code := ask("POST", "203.0.113.7:40000", nil); code != http.StatusForbidden {
		t.Errorf("a caller from elsewhere got %d", code)
	}
	// A web server on this machine, passing on a stranger's request.
	for _, header := range []string{"X-Forwarded-For", "Forwarded", "Via", "X-Real-Ip"} {
		if code := ask("POST", "127.0.0.1:40000", map[string]string{header: "203.0.113.7"}); code != http.StatusForbidden {
			t.Errorf("a request passed on with %s got %d", header, code)
		}
	}
	if code := ask("GET", "127.0.0.1:40000", nil); code != http.StatusMethodNotAllowed {
		t.Errorf("a GET got %d", code)
	}
	// What a web page could make a browser on this machine send.
	if code := ask("POST", "127.0.0.1:40000", map[string]string{"Content-Type": "text/plain"}); code != http.StatusUnsupportedMediaType {
		t.Errorf("a post as a browser form would send it got %d", code)
	}
	// From this machine, to a node that is not a pool: an answer, saying so.
	if code := ask("POST", "127.0.0.1:40000", nil); code != http.StatusServiceUnavailable {
		t.Errorf("a node that is not a pool answered %d", code)
	}
}

// The paying command itself, against a pool in this test program.
func TestPoolPayCommandSignsAndSends(t *testing.T) {
	pool := startPayingPool(t)
	// The work port plus one, on a port that is free.
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	free.Close()
	Params.StratumPort = port - 1
	server := &http.Server{Handler: poolPayAPI()}
	listener, err := net.Listen("tcp", net.JoinHostPort(poolPayHost(), fmt.Sprint(poolPayPort())))
	if err != nil {
		t.Skipf("port %d could not be opened: %v", port, err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Close() })

	// A key that is not the pool's signs nothing.
	strangerKey, _, _, _ := GenerateKeyPair()
	wrongFile := filepath.Join(t.TempDir(), "wrong.txt")
	if err := os.WriteFile(wrongFile, []byte(strangerKey+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := RunPoolPay([]string{"cvn_node", "--pool-pay", "--password-file", wrongFile}); code == 0 {
		t.Error("the command ended well with a key that is not the pool's")
	}
	if unsettled := poolLedger.unsettledPayments(); len(unsettled) != 0 {
		t.Fatalf("after the wrong key, %d payments are left prepared", len(unsettled))
	}
	if alice, _ := poolLedger.account(pool.alice, 64); alice.Owed != cvn("37.5") {
		t.Fatalf("after the wrong key, Alice is owed %s", FormatUnits(alice.Owed))
	}

	keyFile := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(keyFile, []byte(pool.privateKey+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := RunPoolPay([]string{"cvn_node", "--pool-pay", "--password-file", keyFile}); code != 0 {
		t.Fatalf("the command ended with %d", code)
	}
	sent := poolLedger.unsettledPayments()
	if len(sent) != 1 || sent[0].State != poolPaymentSent || sent[0].TxID == "" {
		t.Fatalf("after the command: %+v", sent)
	}
	MempoolMatrix.RLock()
	_, waiting := MempoolMatrix.Transactions[sent[0].TxID]
	MempoolMatrix.RUnlock()
	if !waiting {
		t.Error("the payment's transfer is not waiting for a block")
	}
	// Run again with nothing owed, it says so and ends well.
	if code := RunPoolPay([]string{"cvn_node", "--pool-pay", "--password-file", keyFile}); code != 0 {
		t.Errorf("with nothing to pay the command ended with %d", code)
	}
	if again := poolLedger.unsettledPayments(); len(again) != 1 {
		t.Errorf("a second run left %d payments under way", len(again))
	}
}

// The password file opens the pool's key. One that anybody but its owner can
// read or change is not used.
func TestAPasswordFileOthersCanReadIsNotUsed(t *testing.T) {
	for mode, tooOpen := range map[os.FileMode]bool{
		0o600: false, 0o400: false, 0o700: false,
		0o640: true, 0o644: true, 0o604: true, 0o620: true, 0o602: true, 0o666: true, 0o777: true,
	} {
		if passwordFileTooOpen(mode) != tooOpen {
			t.Errorf("a file with permissions %04o: too open is %v, want %v", mode, !tooOpen, tooOpen)
		}
	}
	if runtime.GOOS == "windows" {
		return // permissions are not written this way there, and nothing is checked
	}
	withWorkPort(t, 28083)
	file := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(file, []byte("correct horse battery\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if code := RunPoolPay([]string{"cvn_node", "--pool-pay", "--password-file", file}); code == 0 {
		t.Error("the paying command used a password file that every user of the machine can read")
	}
}

// A pool that runs for a year finds a great many blocks. All stay in the
// file; only the newest decided ones are kept in memory.
func TestPoolRecordKeepsOnlyTheNewestBlocksInMemory(t *testing.T) {
	onNetwork(t, "devnet")
	saved := poolBlocksKept
	poolBlocksKept = 2
	defer func() { poolBlocksKept = saved }()
	path := filepath.Join(t.TempDir(), "pool.db")
	record, err := openPoolLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	member := testAddress("member")
	for height := int64(1); height <= 5; height++ {
		place, err := record.addShare(member, 10, 1000, 10)
		if err != nil {
			t.Fatal(err)
		}
		block := Block{Index: height, Hash: fmt.Sprintf("%064d", height), Difficulty: 10, Transactions: []Transaction{
			{Outputs: []UTXOOutput{{Recipient: testAddress("pool"), Amount: UnitsAmount(cvn("50"))}}},
		}}
		if _, err := record.addBlock(block, member, place, 1000); err != nil {
			t.Fatal(err)
		}
	}
	// Five waiting: all are kept, however many that is.
	if len(record.blocks) != 5 {
		t.Fatalf("%d waiting blocks are kept, want all 5", len(record.blocks))
	}
	for height := int64(1); height <= 4; height++ {
		if _, changed, err := record.decide(height, fmt.Sprintf("%064d", height), true, height+100, fmt.Sprintf("TX_REWARD_%d", height)); err != nil || !changed {
			t.Fatalf("block %d: %v", height, err)
		}
	}
	check := func(record *poolRecord, when string) {
		t.Helper()
		view := record.view(10, 2000, 10)
		if view.BlocksFound != 5 || view.Confirmed != 4 || view.Waiting != 1 || view.Lost != 0 || view.Earned != cvn("200") {
			t.Errorf("%s: %d found, %d confirmed, %d waiting, %s earned", when, view.BlocksFound, view.Confirmed, view.Waiting, FormatUnits(view.Earned))
		}
		// The waiting one and the two newest decided, newest first.
		if len(view.Blocks) != 3 || view.Blocks[0].Height != 5 || view.Blocks[1].Height != 4 || view.Blocks[2].Height != 3 {
			t.Errorf("%s: the blocks kept are %d", when, len(view.Blocks))
		}
		if free, _, units, _ := record.coinsHeld(); free != 4 || units != cvn("200") {
			t.Errorf("%s: %d coins of %s are free", when, free, FormatUnits(units))
		}
	}
	check(record, "after deciding four")
	record.close()
	again, err := openPoolLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.close()
	check(again, "opened again")
	// The one still waiting can still be decided.
	if _, changed, err := again.decide(5, fmt.Sprintf("%064d", 5), false, 105, ""); err != nil || !changed {
		t.Errorf("the waiting block could not be decided after opening again: %v", err)
	}
	if view := again.view(10, 2000, 10); view.Lost != 1 || view.Waiting != 0 || len(view.Blocks) != 2 {
		t.Errorf("after the last was lost: %d lost, %d waiting, %d kept", view.Lost, view.Waiting, len(view.Blocks))
	}
}
