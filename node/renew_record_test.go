package node

import (
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// keepRenewalNotesIn makes the wallet keep its notes of renewals in a folder
// of the test's own, and puts the usual place back afterwards.
func keepRenewalNotesIn(t *testing.T) string {
	t.Helper()
	saved := renewalRecordFile
	renewalRecordFile = filepath.Join(t.TempDir(), "wallet_renewals.json")
	t.Cleanup(func() { renewalRecordFile = saved })
	return renewalRecordFile
}

// renewalOf is a transfer that spends the coins named, as far as the notes
// are concerned: only its inputs matter to them.
func renewalOf(coins ...SpendableCoin) Transaction {
	var tx Transaction
	for _, coin := range coins {
		tx.Inputs = append(tx.Inputs, walletInput(coin))
	}
	return tx
}

func noteCoin(name string) SpendableCoin {
	return SpendableCoin{TxID: "TX_" + name, Index: 0, Amount: UnitsAmount(cvn("50")), SweepAt: 1000}
}

// recordAsWritten is the bytes of the record, to show that looking at it
// changed nothing.
func recordAsWritten(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(renewalRecordFile)
	if err != nil {
		t.Fatalf("the record could not be read: %v", err)
	}
	return string(raw)
}

func TestARenewalIsOfferedAgainOnlyAfterThreeDaysAndOnlyWhileUnspent(t *testing.T) {
	keepRenewalNotesIn(t)
	owner, other := testAddress("owner"), testAddress("someone else")
	sent := time.Unix(1_800_000_000, 0)
	a, b, c := noteCoin("a"), noteCoin("b"), noteCoin("c")

	if waiting := renewalsNotGoneThrough(owner, []SpendableCoin{a, b}, sent); waiting.any() {
		t.Fatalf("with nothing written down the wallet found %d coins waiting", len(waiting.Coins))
	}
	if err := noteRenewalSent(owner, []Transaction{renewalOf(a, b)}, 0, sent); err != nil {
		t.Fatal(err)
	}
	if err := noteRenewalSent(other, []Transaction{renewalOf(c)}, 0, sent); err != nil {
		t.Fatal(err)
	}
	written := recordAsWritten(t)

	// One second short of three days: nothing is said.
	early := sent.Add(renewalRetryAfter - time.Second)
	if waiting := renewalsNotGoneThrough(owner, []SpendableCoin{a, b}, early); waiting.any() {
		t.Errorf("%d coins were offered again before three days had passed", len(waiting.Coins))
	}

	// At three days, with both coins still unspent: both are offered, the
	// date is when the renewal was sent, and the question says how many
	// transfers a second try is and what they cost together.
	due := sent.Add(renewalRetryAfter)
	waiting := renewalsNotGoneThrough(owner, []SpendableCoin{b, a, c}, due)
	if len(waiting.Coins) != 2 || !waiting.FirstSent.Equal(sent) || waiting.FeeUnits != 0 {
		t.Fatalf("at three days: %d coins offered, sent %v, fee %d; want the 2 of this address, sent %v, no fee", len(waiting.Coins), waiting.FirstSent, waiting.FeeUnits, sent)
	}
	if text := waiting.describe(due); !strings.Contains(text, "2 coin(s)") || !strings.Contains(text, "3 days ago") || !strings.Contains(text, "Nothing is lost") || !strings.Contains(text, "with no fee") {
		t.Errorf("what the wallet says: %q", text)
	}
	if waiting.transfers() != 1 || waiting.proposedFeeUnits() != renewalRetryFeeUnits {
		t.Errorf("a second try of 2 coins is %d transfer(s) at %d units; want 1 at %d", waiting.transfers(), waiting.proposedFeeUnits(), renewalRetryFeeUnits)
	}
	if question := waiting.question(waiting.proposedFeeUnits()); !strings.Contains(question, "1 transfer(s)") || !strings.Contains(question, "0.001 CVN each") || !strings.Contains(question, "0.001 CVN in all") {
		t.Errorf("the question asked: %q", question)
	}

	// One of them is no longer listed: only the other is offered.
	if waiting = renewalsNotGoneThrough(owner, []SpendableCoin{b}, due); len(waiting.Coins) != 1 || waiting.Coins[0].TxID != b.TxID {
		t.Fatalf("with one coin not listed the wallet offered %+v; want the other one only", waiting.Coins)
	}
	// A node that lists nothing at all (it is starting up, or behind, or
	// the coins are tied up in a waiting transfer) gets no offer, and
	// costs the wallet nothing: when the coins are listed again, the
	// reminder is still there.
	if waiting = renewalsNotGoneThrough(owner, nil, due); waiting.any() {
		t.Errorf("with no coin listed, %d were offered", len(waiting.Coins))
	}
	if waiting = renewalsNotGoneThrough(owner, []SpendableCoin{a, b}, due); len(waiting.Coins) != 2 {
		t.Errorf("after a node had listed no coins, %d of the 2 are offered; the reminder must not be lost", len(waiting.Coins))
	}
	// A coin that cannot be renewed (it has no date, or holds nothing) is
	// not counted among those to send again.
	undated := a
	undated.SweepAt = 0
	if waiting = renewalsNotGoneThrough(owner, []SpendableCoin{undated, b}, due); len(waiting.Coins) != 1 {
		t.Errorf("a coin with no date was offered for renewal: %+v", waiting.Coins)
	}
	// Looking, however often and whatever was listed, wrote nothing.
	if now := recordAsWritten(t); now != written {
		t.Errorf("looking at the record changed it:\n%s\nwas\n%s", now, written)
	}

	// A month after it was sent, the wallet stops offering.
	if waiting = renewalsNotGoneThrough(owner, []SpendableCoin{a, b}, sent.Add(renewalNoteLife)); waiting.any() {
		t.Errorf("a month on, %d coins are still offered", len(waiting.Coins))
	}
	if waiting = renewalsNotGoneThrough(owner, []SpendableCoin{a, b}, sent.Add(renewalNoteLife-time.Second)); len(waiting.Coins) != 2 {
		t.Errorf("a second short of a month, %d coins are offered; want 2", len(waiting.Coins))
	}
	// And the old notes go when the record is next written.
	later := sent.Add(renewalNoteLife + time.Hour)
	if err := noteRenewalSent(owner, []Transaction{renewalOf(noteCoin("d"))}, 0, later); err != nil {
		t.Fatal(err)
	}
	if notes := loadRenewalNotes(); len(notes) != 1 || notes[0].Sent != later.Unix() {
		t.Errorf("after a note was written a month on, the record holds %+v; want that note alone", notes)
	}
}

// Sending a renewal again is written down afresh, with its fee: the coins
// move to the newest note, the three days start again, and what the wallet
// says and proposes next time follows from the fee that was passed over.
func TestARenewalSentAgainIsNotedAfreshWithItsFee(t *testing.T) {
	keepRenewalNotesIn(t)
	owner := testAddress("owner")
	first := time.Unix(1_800_000_000, 0)
	a, b := noteCoin("a"), noteCoin("b")
	if err := noteRenewalSent(owner, []Transaction{renewalOf(a, b)}, 0, first); err != nil {
		t.Fatal(err)
	}
	again := first.Add(4 * 24 * time.Hour)
	if err := noteRenewalSent(owner, []Transaction{renewalOf(a)}, renewalRetryFeeUnits, again); err != nil {
		t.Fatal(err)
	}
	notes := loadRenewalNotes()
	if len(notes) != 2 || len(notes[0].Coins) != 1 || notes[0].Coins[0] != utxoKey(b.TxID, 0) || notes[0].FeeUnits != 0 ||
		notes[1].Sent != again.Unix() || notes[1].Coins[0] != utxoKey(a.TxID, 0) || notes[1].FeeUnits != renewalRetryFeeUnits {
		t.Fatalf("after one coin was sent again the notes are %+v", notes)
	}
	// A day after the second sending: the coin sent again is not offered
	// (one day), the other still is (five days).
	waiting := renewalsNotGoneThrough(owner, []SpendableCoin{a, b}, again.Add(24*time.Hour))
	if len(waiting.Coins) != 1 || waiting.Coins[0].TxID != b.TxID {
		t.Errorf("a day after one coin was sent again the wallet offered %+v; want only the other", waiting.Coins)
	}
	// Three days after the paid second try, with that coin still unspent:
	// the wallet says it paid a fee, and proposes twice as much.
	waiting = renewalsNotGoneThrough(owner, []SpendableCoin{a}, again.Add(renewalRetryAfter))
	if len(waiting.Coins) != 1 || waiting.FeeUnits != renewalRetryFeeUnits || waiting.proposedFeeUnits() != 2*renewalRetryFeeUnits {
		t.Fatalf("after a paid second try: %d coins, fee noted %d, proposed %d; want 1, %d and %d", len(waiting.Coins), waiting.FeeUnits, waiting.proposedFeeUnits(), renewalRetryFeeUnits, 2*renewalRetryFeeUnits)
	}
	if text := waiting.describe(again.Add(renewalRetryAfter)); strings.Contains(text, "with no fee") || !strings.Contains(text, "a fee of 0.001 CVN") || !strings.Contains(text, "larger fee") {
		t.Errorf("what the wallet says of a paid renewal that did not go through: %q", text)
	}
}

// A record that is damaged, empty, enormous or held by another program
// never stops the wallet: it is read as holding nothing. And it is not
// thrown away: it is set aside under another name before a new record is
// written.
func TestADamagedRecordOfRenewalsIsReadAsEmptyAndSetAside(t *testing.T) {
	file := keepRenewalNotesIn(t)
	owner := testAddress("owner")
	now := time.Unix(1_800_000_000, 0)
	one := `{"address":"x","sent":1,"coins":["c"]}`
	huge := "[" + strings.Repeat(one+",", renewalRecordMaxSize/len(one)+1) + one + "]"
	if len(huge) <= renewalRecordMaxSize {
		t.Fatalf("set-up: the large record is %d bytes, not more than the %d that are read", len(huge), renewalRecordMaxSize)
	}
	for name, contents := range map[string]string{
		"not JSON":               "this is not a record",
		"empty":                  "",
		"another shape":          `{"address": "CVN_x"}`,
		"larger than is read":    huge,
		"cut short":              `[{"address":"CVN_x","sent":17`,
		"numbers where text was": `[{"address":7,"sent":"soon","coins":"all"}]`,
	} {
		_ = os.Remove(file + ".unreadable")
		if err := os.WriteFile(file, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if notes, unreadable := readRenewalRecord(); len(notes) != 0 || !unreadable {
			t.Errorf("%s: read as %d notes, unreadable %v; want none, and known to be unreadable", name, len(notes), unreadable)
		}
		if waiting := renewalsNotGoneThrough(owner, []SpendableCoin{noteCoin("a")}, now.Add(10*24*time.Hour)); waiting.any() {
			t.Errorf("%s: %d coins offered", name, len(waiting.Coins))
		}
		if err := noteRenewalSent(owner, []Transaction{renewalOf(noteCoin("a"))}, 0, now); err != nil {
			t.Errorf("%s: a new note could not be written: %v", name, err)
		}
		if notes := loadRenewalNotes(); len(notes) != 1 || notes[0].Address != owner {
			t.Errorf("%s: after a new note the record holds %+v", name, notes)
		}
		if kept, err := os.ReadFile(file + ".unreadable"); err != nil || string(kept) != contents {
			t.Errorf("%s: the record that could not be read was not set aside as it was (err %v, %d bytes kept of %d)", name, err, len(kept), len(contents))
		}
	}
	// Notes with parts missing are left out; the rest of the record is read.
	partly := `[{"address":"","sent":5,"coins":["c"]},{"address":"a","sent":0,"coins":["c"]},{"address":"a","sent":5,"coins":[]},{"address":"a","sent":5,"fee":-1,"coins":["c"]},null,{"address":"good","sent":5,"coins":["c"]}]`
	if err := os.WriteFile(file, []byte(partly), 0600); err != nil {
		t.Fatal(err)
	}
	if notes, unreadable := readRenewalRecord(); len(notes) != 1 || notes[0].Address != "good" || unreadable {
		t.Errorf("a record with some notes incomplete was read as %+v (unreadable %v); want the one whole note", notes, unreadable)
	}
	// No file at all is not a damaged file. And where the record cannot be
	// written, the failure is reported, not hidden.
	renewalRecordFile = filepath.Join(t.TempDir(), "no such folder", "wallet_renewals.json")
	if notes, unreadable := readRenewalRecord(); notes != nil || unreadable {
		t.Errorf("a missing record was read as %+v, unreadable %v", notes, unreadable)
	}
	if err := noteRenewalSent(owner, []Transaction{renewalOf(noteCoin("a"))}, 0, now); err == nil {
		t.Error("a note that could not be written was reported as written")
	}
}

// The record never grows without limit: the newest notes are kept, and no
// more coins in all than can always be read back.
func TestTheRecordOfRenewalsKeepsOnlyTheNewestNotes(t *testing.T) {
	file := keepRenewalNotesIn(t)
	start := time.Unix(1_800_000_000, 0)
	for i := 0; i < renewalNotesKept+25; i++ {
		owner := testAddress(fmt.Sprint("owner ", i))
		if err := noteRenewalSent(owner, []Transaction{renewalOf(noteCoin(fmt.Sprint("coin", i)))}, 0, start.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	notes := loadRenewalNotes()
	if len(notes) != renewalNotesKept {
		t.Fatalf("%d notes are kept, want the newest %d", len(notes), renewalNotesKept)
	}
	if notes[0].Sent != start.Add(25*time.Minute).Unix() {
		t.Errorf("the oldest note kept was sent at %d; want the 26th", notes[0].Sent)
	}

	// Many large renewals, each of the most coins one renewing can spend,
	// with names as long as a coin's really is: the record stays within the
	// coins it may hold and small enough to be read back.
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	perRenewal := TxMaxInputs * combineMaxTransfers
	for i := 0; i < 12; i++ {
		var transfer Transaction
		for c := 0; c < perRenewal; c++ {
			transfer.Inputs = append(transfer.Inputs, UTXOInput{SourceTxID: fmt.Sprintf("TX_%064x", i*perRenewal+c), Index: 49})
		}
		if err := noteRenewalSent(testAddress(fmt.Sprint("large ", i)), []Transaction{transfer}, 0, start.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	notes, unreadable := readRenewalRecord()
	coins := 0
	for _, note := range notes {
		coins += len(note.Coins)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if unreadable || len(notes) != renewalCoinsKept/perRenewal || coins > renewalCoinsKept || info.Size() >= renewalRecordMaxSize {
		t.Errorf("after twelve large renewals: %d notes, %d coins, %d bytes, unreadable %v; want the newest %d notes, within %d coins and %d bytes", len(notes), coins, info.Size(), unreadable, renewalCoinsKept/perRenewal, renewalCoinsKept, renewalRecordMaxSize)
	}
	if len(notes) > 0 && notes[len(notes)-1].Address != testAddress("large 11") {
		t.Error("the newest note is not the last one written")
	}
}

// serveWalletRequests answers a wallet's requests from this test's chain, as
// a node on the same machine would.
func serveWalletRequests(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(syncAPI())
	t.Cleanup(server.Close)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if Params.SyncPort, err = strconv.Atoi(port); err != nil {
		t.Fatal(err)
	}
	// A wallet that gets no answer from the first node tries the second
	// port on this machine and then the network's seeds. In a test there
	// must be nowhere else to go: not the miner that may be running on this
	// machine, and not a real server.
	Params.SyncFallbackPort = Params.SyncPort
	Params.SeedHosts = nil
	// A node gives each caller only so many lists of coins in a short
	// time, and every test here asks from the same address. Each starts
	// with a fresh allowance.
	IPTokensMutex.Lock()
	for key := range IPTokensMap {
		if strings.HasPrefix(key, "coins|") {
			delete(IPTokensMap, key)
		}
	}
	IPTokensMutex.Unlock()
}

// waitingTransfers is the transfers a node holds for the next block.
func waitingTransfers() []Transaction {
	MempoolMatrix.RLock()
	defer MempoolMatrix.RUnlock()
	var list []Transaction
	for _, tx := range MempoolMatrix.Transactions {
		list = append(list, tx)
	}
	return list
}

// forgetWaitingTransfers is what every node does to a transfer that has
// waited two hours: it drops it, and the coins are as they were.
func forgetWaitingTransfers() {
	MempoolMatrix.Lock()
	defer MempoolMatrix.Unlock()
	for id := range MempoolMatrix.Transactions {
		removeFromMempoolLocked(id)
	}
}

// walletWithOneCoinNearItsDate starts a chain on which a coin's date is five
// blocks after it is made, serves a wallet's requests from it, and gives a
// new wallet one coin.
func walletWithOneCoinNearItsDate(t *testing.T) (privateKey, publicKey, owner string, timestamp *int64) {
	t.Helper()
	timestamp = startSweepChain(t)
	keepRenewalNotesIn(t)
	serveWalletRequests(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner) // block 1: the coin
	mineNext(t, timestamp, testAddress("other"))
	return privateKey, publicKey, strings.TrimSpace(owner), timestamp
}

// End to end, against a chain: a renewal sent free is written down; if the
// network drops it, the wallet sends it again with a fee after three days,
// and not before; once it is mined there is nothing more to offer. A
// renewal sent with a fee from the start is not written down.
func TestARenewalThatDidNotGoThroughIsSentAgainWithAFee(t *testing.T) {
	privateKey, publicKey, owner, timestamp := walletWithOneCoinNearItsDate(t)
	began := time.Now()

	// A renewal with no fee, as the wallet sends it.
	result, err := RenewCoinsFromWallet(privateKey, publicKey, 0, false)
	if err != nil || len(result.Transfers) != 1 || result.NoteProblem != nil || result.SentAgain {
		t.Fatalf("the renewal: %d transfers, err %v, note problem %v, marked as a second try %v", len(result.Transfers), err, result.NoteProblem, result.SentAgain)
	}
	free := result.Transfers[0]
	if free.FreeWillOffering.Units() != 0 {
		t.Fatalf("the renewal pays a fee of %d units, want none", free.FreeWillOffering.Units())
	}
	notes := loadRenewalNotes()
	if len(notes) != 1 || notes[0].Address != owner || notes[0].FeeUnits != 0 || len(notes[0].Coins) != 1 || notes[0].Coins[0] != utxoKey(free.Inputs[0].SourceTxID, free.Inputs[0].Index) {
		t.Fatalf("after a free renewal the wallet's notes are %+v", notes)
	}

	// The network never mines it and, after two hours, forgets it.
	forgetWaitingTransfers()
	if coins := ListSpendableCoins(owner); len(coins) != 1 {
		t.Fatalf("set-up: the coin should be as it was; the wallet holds %d", len(coins))
	}

	// Two days on: too soon. Nothing is sent.
	if again, err := RenewAgainFromWallet(privateKey, publicKey, renewalRetryFeeUnits, began.Add(2*24*time.Hour)); err == nil || len(again.Transfers) != 0 {
		t.Fatalf("two days on the wallet sent the renewal again (%d transfers, err %v)", len(again.Transfers), err)
	}
	if waiting := waitingTransfers(); len(waiting) != 0 {
		t.Fatalf("two days on, %d transfers were handed to the node", len(waiting))
	}

	// Three days and an hour on: it is sent again. No fee is named, so it
	// is the one the wallet proposes.
	later := began.Add(renewalRetryAfter + time.Hour)
	again, err := RenewAgainFromWallet(privateKey, publicKey, 0, later)
	if err != nil || len(again.Transfers) != 1 || again.NoteProblem != nil || !again.SentAgain {
		t.Fatalf("the second try: %d transfers, err %v, note problem %v, marked as a second try %v", len(again.Transfers), err, again.NoteProblem, again.SentAgain)
	}
	if !strings.Contains(again.Summary(), "Only the renewal that had not gone through was sent again") {
		t.Errorf("the summary of a second try does not say that other coins were not renewed: %q", again.Summary())
	}
	paid := again.Transfers[0]
	if paid.FreeWillOffering.Units() != renewalRetryFeeUnits || paid.Inputs[0].SourceTxID != free.Inputs[0].SourceTxID {
		t.Fatalf("the second try pays %d units and spends %s; want %d and the same coin", paid.FreeWillOffering.Units(), paid.Inputs[0].SourceTxID, renewalRetryFeeUnits)
	}
	if out := paid.Outputs; len(out) != 1 || out[0].Recipient != owner || out[0].Amount.Units() != cvn("50")-renewalRetryFeeUnits {
		t.Fatalf("the second try's outputs are %+v; want the whole coin less the fee, back to its owner", out)
	}
	if notes = loadRenewalNotes(); len(notes) != 1 || notes[0].Sent != later.Unix() || notes[0].FeeUnits != renewalRetryFeeUnits {
		t.Fatalf("after the second try the notes are %+v; want it noted afresh with its fee", notes)
	}

	// A miner takes it. Later still, there is nothing to offer.
	chosen, fees := SelectMempoolTransactionsForBlock(TxMaxPerBlock - 1)
	if len(chosen) != 1 || chosen[0].ID != paid.ID || fees != renewalRetryFeeUnits {
		t.Fatalf("the miner chose %d transfers with fees %d; want the second try", len(chosen), fees)
	}
	mineNext(t, timestamp, testAddress("other"), chosen...)
	coins := ListSpendableCoins(owner)
	if len(coins) != 1 || coins[0].TxID != paid.ID {
		t.Fatalf("after the block the wallet holds %+v; want the one renewed coin", coins)
	}
	if waiting := renewalsNotGoneThrough(owner, coins, later.Add(10*24*time.Hour)); waiting.any() {
		t.Errorf("after the renewal was mined, %d coins are still offered", len(waiting.Coins))
	}

	// A renewal that pays a fee from the start is not written down: the
	// reminder begins with the free ones.
	before := recordAsWritten(t)
	withFee, err := RenewCoinsFromWallet(privateKey, publicKey, 1000, true)
	if err != nil || len(withFee.Transfers) != 1 {
		t.Fatalf("a renewal with a fee: %d transfers, err %v", len(withFee.Transfers), err)
	}
	if after := recordAsWritten(t); after != before {
		t.Errorf("a renewal sent with a fee was written down:\n%s", after)
	}
}

// A renewal goes through even when the wallet cannot write its note of it,
// and the owner is told which file could not be written.
func TestARenewalIsSentEvenIfItsNoteCannotBeWritten(t *testing.T) {
	privateKey, publicKey, _, _ := walletWithOneCoinNearItsDate(t)
	renewalRecordFile = filepath.Join(t.TempDir(), "no such folder", "wallet_renewals.json")
	result, err := RenewCoinsFromWallet(privateKey, publicKey, 0, false)
	if err != nil || len(result.Transfers) != 1 {
		t.Fatalf("the renewal: %d transfers, err %v; it should have been sent", len(result.Transfers), err)
	}
	if len(waitingTransfers()) != 1 {
		t.Fatal("the renewal did not reach the node")
	}
	if result.NoteProblem == nil {
		t.Fatal("the wallet could not write its note and did not say so")
	}
	if summary := result.Summary(); !strings.Contains(summary, "could not write its note") || !strings.Contains(summary, filepath.Join("no such folder", "wallet_renewals.json")) || !strings.Contains(summary, "Renewed 1 coin(s)") {
		t.Errorf("what the owner is told: %q", summary)
	}
}

// A wallet with more coins than one transfer spends: a renewal is several
// transfers. Every coin of every transfer is noted. If the node refuses one
// part-way, what was sent is still noted and the owner is told where it
// stopped. A second try sends them all again, far from their date though
// they are.
func TestARenewalOfManyCoinsIsNotedAndSentAgainWhole(t *testing.T) {
	startTestChain(t, "devnet") // the launch figures: a coin's date is seven years off
	keepRenewalNotesIn(t)
	serveWalletRequests(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	owner = strings.TrimSpace(owner)
	wallet := shareWallet{privateKey, publicKey, owner}
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 5; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, owner, nil)
	}
	var cuts []Transaction
	for _, coin := range ListSpendableCoins(owner) {
		cuts = append(cuts, wallet.spend(t, coin, TxMaxOutputs, 0))
	}
	timestamp += 45
	extendTestChain(t, timestamp, testAddress("a miner"), cuts)
	if held := len(ListSpendableCoins(owner)); held != 250 {
		t.Fatalf("set-up: the wallet holds %d coins, want 250", held)
	}
	began := time.Now()

	// The node has room for one waiting transfer only, so it takes the
	// first of the two and refuses the second.
	savedRoom := MempoolMaxTransactions
	MempoolMaxTransactions = 1
	t.Cleanup(func() { MempoolMaxTransactions = savedRoom })
	result, err := RenewCoinsFromWallet(privateKey, publicKey, 0, true)
	if err == nil || len(result.Transfers) != 1 || result.CoinsRenewed != TxMaxInputs {
		t.Fatalf("with room for one transfer: %d sent, %d coins, err %v; want one transfer of %d coins and the refusal of the second", len(result.Transfers), result.CoinsRenewed, err, TxMaxInputs)
	}
	notes := loadRenewalNotes()
	if len(notes) != 1 || len(notes[0].Coins) != TxMaxInputs {
		t.Fatalf("after a renewal that stopped part-way there are %d notes; want one, of the %d coins that were sent and no others", len(notes), TxMaxInputs)
	}
	sentCoins := make(map[string]bool)
	for _, input := range result.Transfers[0].Inputs {
		sentCoins[utxoKey(input.SourceTxID, input.Index)] = true
	}
	for _, key := range notes[0].Coins {
		if !sentCoins[key] {
			t.Fatalf("the note names coin %s, which was not in the transfer that was sent", key)
		}
	}

	// With room again, the whole wallet: two transfers, 250 coins noted.
	MempoolMaxTransactions = savedRoom
	forgetWaitingTransfers()
	result, err = RenewCoinsFromWallet(privateKey, publicKey, 0, true)
	if err != nil || len(result.Transfers) != 2 || result.CoinsRenewed != 250 {
		t.Fatalf("the whole renewal: %d transfers, %d coins, err %v; want 2 and 250", len(result.Transfers), result.CoinsRenewed, err)
	}
	noted := 0
	for _, note := range loadRenewalNotes() {
		noted += len(note.Coins)
	}
	if noted != 250 {
		t.Fatalf("%d coins are noted, want all 250, each once", noted)
	}

	// The network drops both. Three days on, the wallet asks about all 250
	// as two transfers, and a second try sends two transfers that pay.
	forgetWaitingTransfers()
	later := began.Add(renewalRetryAfter + time.Hour)
	waiting := renewalsNotGoneThrough(owner, ListSpendableCoins(owner), later)
	if len(waiting.Coins) != 250 || waiting.transfers() != 2 {
		t.Fatalf("three days on: %d coins waiting as %d transfers; want 250 as 2", len(waiting.Coins), waiting.transfers())
	}
	if question := waiting.question(waiting.proposedFeeUnits()); !strings.Contains(question, "2 transfer(s)") || !strings.Contains(question, "0.002 CVN in all") {
		t.Errorf("the question asked: %q", question)
	}
	again, err := RenewAgainFromWallet(privateKey, publicKey, 0, later)
	if err != nil || len(again.Transfers) != 2 || again.CoinsRenewed != 250 {
		t.Fatalf("the second try: %d transfers, %d coins, err %v; want 2 and 250", len(again.Transfers), again.CoinsRenewed, err)
	}
	spent := make(map[string]bool)
	for _, tx := range again.Transfers {
		if tx.FreeWillOffering.Units() != renewalRetryFeeUnits {
			t.Errorf("a transfer of the second try pays %d units, want %d", tx.FreeWillOffering.Units(), renewalRetryFeeUnits)
		}
		for _, input := range tx.Inputs {
			spent[utxoKey(input.SourceTxID, input.Index)] = true
		}
	}
	if len(spent) != 250 {
		t.Errorf("the second try spends %d different coins, want all 250", len(spent))
	}
}

// The text wallet's second try: it shows what would be sent and what it
// costs in all, and sends only on a plain yes.
func TestTheTextWalletSendsARenewalAgainOnlyOnAPlainYes(t *testing.T) {
	privateKey, publicKey, owner, _ := walletWithOneCoinNearItsDate(t)
	now := time.Now()
	coins := ListSpendableCoins(owner)
	// A renewal sent free four days ago, whose coin is still unspent.
	note := func() {
		t.Helper()
		if err := noteRenewalSent(owner, []Transaction{renewalOf(coins...)}, 0, now.Add(-4*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	// answering plays the owner: it gives the answers in turn and counts
	// the questions asked.
	answering := func(answers ...string) (ask func(string) string, asked *int) {
		asked = new(int)
		return func(string) string {
			*asked++
			if *asked > len(answers) {
				t.Fatalf("the wallet asked a question the test did not expect (number %d)", *asked)
			}
			return answers[*asked-1]
		}, asked
	}
	cases := []struct {
		name         string
		answers      []string
		wantDealt    bool  // the matter ended here; renewing does not go on
		wantFeeUnits int64 // the fee of the one transfer sent; -1 for none sent
	}{
		{"leave it", []string{"n"}, false, -1},
		{"leave it, in full", []string{"No"}, false, -1},
		{"the fee proposed, then yes", []string{"", "y"}, true, renewalRetryFeeUnits},
		{"the fee proposed, then no", []string{"", "n"}, true, -1},
		{"the fee proposed, then nothing typed", []string{"", ""}, true, -1},
		{"another fee, then yes", []string{"0.002", "yes"}, true, 200000},
		{"another fee, then anything but yes", []string{"0.002", "ok"}, true, -1},
		{"not a number", []string{"plenty"}, true, -1},
		{"nought", []string{"0"}, true, -1},
	}
	for _, c := range cases {
		forgetWaitingTransfers()
		note()
		ask, asked := answering(c.answers...)
		dealt := textWalletRenewAgain(ask, privateKey, publicKey, owner, coins, now)
		waiting := waitingTransfers()
		if dealt != c.wantDealt || *asked != len(c.answers) {
			t.Errorf("%s: dealt with here %v after %d question(s); want %v after %d", c.name, dealt, *asked, c.wantDealt, len(c.answers))
		}
		switch {
		case c.wantFeeUnits < 0 && len(waiting) != 0:
			t.Errorf("%s: %d transfer(s) were sent; want none", c.name, len(waiting))
		case c.wantFeeUnits >= 0 && (len(waiting) != 1 || waiting[0].FreeWillOffering.Units() != c.wantFeeUnits):
			t.Errorf("%s: %d transfer(s) sent; want one paying %d units", c.name, len(waiting), c.wantFeeUnits)
		}
	}
	// With no such renewal noted, the wallet asks nothing and goes on.
	forgetWaitingTransfers()
	if err := os.Remove(renewalRecordFile); err != nil {
		t.Fatal(err)
	}
	ask, asked := answering()
	if textWalletRenewAgain(ask, privateKey, publicKey, owner, coins, now) || *asked != 0 {
		t.Error("with nothing noted, the wallet asked about a second try")
	}
}

// The window wallet's second try. Asked plainly, it says a renewal has not
// gone through and sends nothing; "again" sends it with the fee the
// question named; "skip" and "all" renew as usual.
func TestTheWindowWalletAsksBeforeSendingARenewalAgain(t *testing.T) {
	privateKey, _, owner, _ := walletWithOneCoinNearItsDate(t)
	app := NewApp()
	coins := ListSpendableCoins(owner)
	note := func() {
		t.Helper()
		forgetWaitingTransfers()
		if err := noteRenewalSent(owner, []Transaction{renewalOf(coins...)}, 0, time.Now().Add(-4*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	// Asked plainly: the question, with the number of transfers and the
	// cost in all, and nothing sent. The fee typed in the form is not
	// what a second try would pay, and the question does not name it.
	note()
	answer := app.RenewCoinsSubmit("0.5", privateKey, "")
	if !strings.HasPrefix(answer, "NOT THROUGH: ") || !strings.Contains(answer, "1 transfer(s)") || !strings.Contains(answer, "0.001 CVN each") || !strings.Contains(answer, "0.001 CVN in all") || strings.Contains(answer, "0.5") {
		t.Errorf("asked plainly, the wallet answered %q", answer)
	}
	if waiting := waitingTransfers(); len(waiting) != 0 {
		t.Fatalf("asked plainly, the wallet sent %d transfer(s) before any answer", len(waiting))
	}

	// "again", as the screen sends it (with no fee of its own): the fee
	// that the question named.
	answer = app.RenewCoinsSubmit("", privateKey, "again")
	waiting := waitingTransfers()
	if !strings.HasPrefix(answer, "SUCCESS: ") || len(waiting) != 1 || waiting[0].FreeWillOffering.Units() != renewalRetryFeeUnits {
		t.Fatalf("\"again\": answered %q and sent %d transfer(s); want one paying %d units", answer, len(waiting), renewalRetryFeeUnits)
	}
	// "again" with nothing waiting to be sent again: refused, nothing sent.
	forgetWaitingTransfers()
	if answer = app.RenewCoinsSubmit("", privateKey, "again"); !strings.HasPrefix(answer, "REJECTED: ") || len(waitingTransfers()) != 0 {
		t.Errorf("\"again\" with no renewal waiting: answered %q, %d transfer(s) sent", answer, len(waitingTransfers()))
	}

	// "skip" and "all": no question; an ordinary renewal with the fee typed.
	for _, mode := range []string{"skip", "all"} {
		note()
		answer = app.RenewCoinsSubmit("0.25", privateKey, mode)
		waiting = waitingTransfers()
		if !strings.HasPrefix(answer, "SUCCESS: ") || len(waiting) != 1 || waiting[0].FreeWillOffering.Units() != cvn("0.25") {
			t.Errorf("%q: answered %q and sent %d transfer(s); want an ordinary renewal paying 0.25 CVN", mode, answer, len(waiting))
		}
	}
	// With nothing noted, asked plainly: an ordinary renewal at once.
	forgetWaitingTransfers()
	if err := os.Remove(renewalRecordFile); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if answer = app.RenewCoinsSubmit("", privateKey, ""); !strings.HasPrefix(answer, "SUCCESS: ") || len(waitingTransfers()) != 1 {
		t.Errorf("with nothing noted: answered %q, %d transfer(s) sent; want an ordinary renewal", answer, len(waitingTransfers()))
	}
}
