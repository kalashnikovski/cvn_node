package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// A SWEPT FIFTH IS KEPT ON THE COIN FOR ITS OWNER
//
// At a coin's seven-year date a fifth of its amount is set aside on the coin
// itself. It is still the owner's: spending the coin, to anyone or to their
// own address, spends the amount and the fifth together. Only at the coin's
// next date, seven years on, does a fifth that nobody took back go to the
// vault, and a new fifth is then set aside.
//
// These tests run on a chain where a coin's date is five blocks after it was
// made or last swept, and read coins as they are written in the database
// (storedCoin, in coins_conserved_test.go).

// theCoinOf returns the one coin an address holds, with its key. It fails
// the test if the address holds none or several.
func theCoinOf(t *testing.T, address string) (string, storedCoin) {
	t.Helper()
	var key string
	var found storedCoin
	count := 0
	for k, coin := range everyStoredCoin(t) {
		if coin.Recipient == address {
			key, found = k, coin
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%s holds %d coins, want one", address, count)
	}
	return key, found
}

// wantCoin fails the test unless the address's one coin has this amount,
// this much set aside and this age.
func wantCoin(t *testing.T, when, address string, amount, held, age int64) {
	t.Helper()
	_, coin := theCoinOf(t, address)
	if coin.Amount != amount || coin.Held != held || coin.Age != age {
		t.Fatalf("%s: the coin holds %s with %s set aside, age %d; want %s with %s set aside, age %d",
			when, FormatUnits(coin.Amount), FormatUnits(coin.Held), coin.Age, FormatUnits(amount), FormatUnits(held), age)
	}
}

// vaultRecord reads the vault's balance after the newest block straight from
// its record, and fails the test if it cannot be read.
func vaultRecord(t *testing.T) int64 {
	t.Helper()
	tip := GetLatestBlockInternal()
	var units int64
	err := GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		var readErr error
		units, readErr = readVaultUnits(dbTx, tip.Index, tip.Hash)
		return readErr
	})
	if err != nil {
		t.Fatalf("the vault's record for block #%d could not be read: %v", tip.Index, err)
	}
	return units
}

// nothingCreatedOrLost checks that every coin, with what is set aside on it,
// and the vault together are exactly what has been issued.
func nothingCreatedOrLost(t *testing.T, when string) {
	t.Helper()
	height := GetLatestBlockInternal().Index
	if coins, vault, issued := coinCount(t), vaultRecord(t), issuedUpTo(height); coins+vault != issued {
		t.Fatalf("%s (block %d): coins %s and vault %s make %s, but %s has been issued", when, height, FormatUnits(coins), FormatUnits(vault), FormatUnits(coins+vault), FormatUnits(issued))
	}
}

// mineTo mines empty blocks, paid to `miner`, until the chain is at `height`.
func mineTo(t *testing.T, timestamp *int64, height int64, miner string) {
	t.Helper()
	for GetLatestBlockInternal().Index < height {
		mineNext(t, timestamp, miner)
	}
}

// verdictOnTransfer is what the rules say of a transfer against the coins as
// they are now: nil if it may be mined, and the reason if not.
func verdictOnTransfer(t *testing.T, tx Transaction) error {
	t.Helper()
	var verdict error
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		_, _, verdict = checkTransferAgainstCoins(tx, bucketLookup(dbTx.Bucket(utxoBucket)), nil)
		return nil
	})
	return verdict
}

// spendStating builds a transfer that spends the owner's one coin, stating
// it to hold `stated` units, and pays all of that back to the owner.
func spendStating(t *testing.T, privateKey, publicKey, owner string, stated int64) Transaction {
	t.Helper()
	key, _ := theCoinOf(t, owner)
	separator := strings.LastIndex(key, "_")
	tx, err := signTransfer(privateKey, publicKey, owner,
		[]UTXOInput{{SourceTxID: key[:separator], Index: 0, AmountUnits: stated}},
		[]UTXOOutput{{Recipient: owner, Amount: UnitsAmount(stated)}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestASweptFifthStaysOnTheCoin(t *testing.T) {
	timestamp := startSweepChain(t)
	idle := testAddress("idle")
	mineNext(t, timestamp, idle) // block 1: the coin, whose date is the end of block 6
	mineTo(t, timestamp, 5, testAddress("other"))
	wantCoin(t, "one block before its date", idle, cvn("50"), 0, 1)

	mineNext(t, timestamp, testAddress("third")) // block 6
	wantCoin(t, "after its date", idle, cvn("40"), cvn("10"), 6)
	if got := vaultRecord(t); got != 0 {
		t.Errorf("the vault holds %s after the coin's first date; the fifth is set aside on the coin, and the vault gets nothing yet", FormatUnits(got))
	}
	// It is still its owner's: the address shows 50, and the wallet is
	// offered the coin at 50, with its next date.
	if got := balanceOf(idle); got != cvn("50") {
		t.Errorf("the address shows %s CVN, want 50: what is set aside is still its owner's", FormatUnits(got))
	}
	coins := ListSpendableCoins(idle)
	if len(coins) != 1 || coins[0].Amount.Units() != cvn("50") || coins[0].SweepAt != 11 {
		t.Errorf("the wallet is offered %+v; want one coin of 50 CVN whose next date is block 11", coins)
	}
	nothingCreatedOrLost(t, "after the first date")
}

func TestSpendingTheCoinGivesTheFifthBack(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner) // block 1
	mineTo(t, timestamp, 5, testAddress("other"))
	mineNext(t, timestamp, testAddress("third")) // block 6: a fifth is set aside
	wantCoin(t, "after its date", owner, cvn("40"), cvn("10"), 6)

	// The owner renews: one transfer of the coin to its own address, no fee.
	renewal, err := BuildSignedRenewal(privateKey, publicKey, 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatalf("the renewal could not be built: %v", err)
	}
	if len(renewal.Inputs) != 1 || renewal.Inputs[0].AmountUnits != cvn("50") || len(renewal.Outputs) != 1 || renewal.Outputs[0].Amount.Units() != cvn("50") {
		t.Fatalf("the renewal spends %+v into %+v; want the coin stated at 50 and one new coin of 50", renewal.Inputs, renewal.Outputs)
	}
	mineNext(t, timestamp, testAddress("third"), renewal) // block 7
	wantCoin(t, "after renewing", owner, cvn("50"), 0, 7)
	if got := vaultRecord(t); got != 0 {
		t.Errorf("the vault holds %s; a fifth taken back by its owner never reaches it", FormatUnits(got))
	}
	nothingCreatedOrLost(t, "after renewing")
}

func TestAnInputMustStateAmountAndFifthTogether(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner)
	mineTo(t, timestamp, 6, testAddress("other")) // the coin is now 40 with 10 set aside

	if verdict := verdictOnTransfer(t, spendStating(t, privateKey, publicKey, owner, cvn("40"))); verdict == nil {
		t.Error("a transfer that stated the coin's amount without the fifth set aside was accepted; its owner would have signed the fifth away")
	}
	if verdict := verdictOnTransfer(t, spendStating(t, privateKey, publicKey, owner, cvn("50")+1)); verdict == nil {
		t.Error("a transfer that stated one unit more than the amount and the fifth was accepted")
	}
	if verdict := verdictOnTransfer(t, spendStating(t, privateKey, publicKey, owner, cvn("50")-1)); verdict == nil {
		t.Error("a transfer that stated one unit less than the amount and the fifth was accepted")
	}
	whole := spendStating(t, privateKey, publicKey, owner, cvn("50"))
	if verdict := verdictOnTransfer(t, whole); verdict != nil {
		t.Fatalf("a transfer that stated the amount and the fifth together was refused: %v", verdict)
	}
	// And in a block: the understated one is refused there too, the whole
	// one is mined.
	*timestamp += 45
	tip := GetLatestBlockInternal()
	understated := spendStating(t, privateKey, publicKey, owner, cvn("40"))
	bad := mineTestBlockPaying(t, *timestamp, testAddress("miner"), []UTXOOutput{{Recipient: testAddress("miner"), Amount: UnitsAmount(cvn("50"))}}, []Transaction{understated})
	if ValidateBlockSignatures(bad) {
		t.Error("a block holding the understated transfer passed the check")
	}
	if SaveBlockToStorageInternal(bad) || GetLatestBlockInternal().Hash != tip.Hash {
		t.Fatal("a block holding the understated transfer was stored")
	}
	extendTestChain(t, *timestamp, testAddress("miner"), []Transaction{whole})
	wantCoin(t, "after spending it whole", owner, cvn("50"), 0, 7)
}

func TestTheFifthGoesToTheVaultAtTheNextDate(t *testing.T) {
	timestamp := startSweepChain(t)
	idle := testAddress("idle")
	mineNext(t, timestamp, idle) // block 1
	// Every other block is mined to an address whose coins have their own
	// second dates from block 12 on, so until then the vault can hold only
	// what came from the idle coin.
	mineTo(t, timestamp, 10, testAddress("idle too"))
	wantCoin(t, "between its dates", idle, cvn("40"), cvn("10"), 6)
	if got := vaultRecord(t); got != 0 {
		t.Fatalf("the vault holds %s before the coin's second date; want nothing", FormatUnits(got))
	}
	nothingCreatedOrLost(t, "before the second date")

	mineNext(t, timestamp, testAddress("idle too")) // block 11: the second date
	wantCoin(t, "after its second date", idle, cvn("32"), cvn("8"), 11)
	// The 10 set aside at block 6 is now the vault's. The fifths of the
	// coins of blocks 2 to 5 were set aside at blocks 7 to 10 and are still
	// on their coins.
	if got := vaultRecord(t); got != cvn("10") {
		t.Fatalf("after the second date the vault holds %s; want exactly the 10 that had been set aside at the first", FormatUnits(got))
	}
	nothingCreatedOrLost(t, "after the second date")

	mineTo(t, timestamp, 16, testAddress("idle too")) // block 16: the third date
	wantCoin(t, "after its third date", idle, cvn("25.6"), cvn("6.4"), 16)
	nothingCreatedOrLost(t, "after the third date")
}

func TestSpendingInTheBlockOfTheSecondDateStillTakesItBack(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner) // block 1; dates at blocks 6 and 11
	mineTo(t, timestamp, 10, testAddress("idle too"))
	wantCoin(t, "between its dates", owner, cvn("40"), cvn("10"), 6)

	// Block 11 is the date at which the fifth would go to the vault. The
	// sweep comes at the end of the block, so a transfer in that very block
	// still spends the coin with its fifth.
	renewal, err := BuildSignedRenewal(privateKey, publicKey, 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, testAddress("idle too"), renewal) // block 11
	wantCoin(t, "after renewing in the block of its second date", owner, cvn("50"), 0, 11)
	if got := vaultRecord(t); got != 0 {
		t.Errorf("the vault holds %s; the fifth was taken back in time and nothing should have reached it", FormatUnits(got))
	}
	nothingCreatedOrLost(t, "after block 11")
}

// At a coin's first date its worth to its owner does not change (the fifth
// is only set aside), so a transfer signed before that date is still good
// after it. At the second date the fifth goes to the vault and the coin is
// worth less, so a transfer signed before it no longer matches and is
// refused: nobody can be made to give up what they did not sign away.
func TestATransferSignedBeforeADate(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner)
	mineTo(t, timestamp, 5, testAddress("idle too"))
	signedBeforeFirst := spendStating(t, privateKey, publicKey, owner, cvn("50"))
	mineNext(t, timestamp, testAddress("idle too")) // block 6: the first date passes
	wantCoin(t, "after its first date", owner, cvn("40"), cvn("10"), 6)
	if verdict := verdictOnTransfer(t, signedBeforeFirst); verdict != nil {
		t.Fatalf("a transfer signed before the coin's first date was refused after it: %v", verdict)
	}

	mineTo(t, timestamp, 10, testAddress("idle too"))
	signedBeforeSecond := spendStating(t, privateKey, publicKey, owner, cvn("50"))
	if verdict := verdictOnTransfer(t, signedBeforeSecond); verdict != nil {
		t.Fatalf("set-up: before the second date the transfer should be good: %v", verdict)
	}
	mineNext(t, timestamp, testAddress("idle too")) // block 11: the fifth goes to the vault
	wantCoin(t, "after its second date", owner, cvn("32"), cvn("8"), 11)
	if verdict := verdictOnTransfer(t, signedBeforeSecond); verdict == nil {
		t.Error("a transfer signed for 50 before the second date was accepted after it, when the coin is worth 40")
	}
	if err := AdmitTransactionToMempool(signedBeforeSecond); err == nil {
		t.Error("a node took such a transfer among those waiting")
	}
	// Signed afresh for what the coin is now worth, it is good.
	if verdict := verdictOnTransfer(t, spendStating(t, privateKey, publicKey, owner, cvn("40"))); verdict != nil {
		t.Errorf("a transfer for the coin's present worth of 40 was refused: %v", verdict)
	}
}

// A small coin. At its first date a fifth is set aside like any other. At
// its second, what it would keep after another fifth is less than the
// smallest coin allowed, so no new fifth is taken; the fifth set aside seven
// years before goes to the vault, as every such fifth does. After that
// nothing more is ever taken from it.
func TestASmallCoinLosesOnlyTheFifthSetAside(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	tiny := testAddress("tiny")
	mineNext(t, timestamp, owner) // block 1
	// Block 2: a coin of 0.00015 CVN. A fifth is 0.00003, leaving 0.00012,
	// which is still a coin; a fifth of that would leave 0.000096, which
	// is not.
	pay, err := BuildSignedTransfer(privateKey, publicKey, tiny, 15000, 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, testAddress("other"), pay)
	wantCoin(t, "when made", tiny, 15000, 0, 2)
	key, _ := theCoinOf(t, tiny)
	sweptAt := func(height int64) bool {
		found := false
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			for _, record := range loadSweepRecords(dbTx, height) {
				if record.Key == key {
					found = true
				}
			}
			return nil
		})
		return found
	}

	mineTo(t, timestamp, 7, testAddress("other")) // first date
	wantCoin(t, "after its first date", tiny, 12000, 3000, 7)
	// Between its dates the wallet is offered it whole, with its date, and
	// renewing takes it in: its owner can still take the fifth back.
	if coins := ListSpendableCoins(tiny); len(coins) != 1 || coins[0].Amount.Units() != 15000 || coins[0].HeldUnits != 3000 || coins[0].SweepAt != 12 {
		t.Errorf("between its dates the wallet is offered %+v; want the coin at 0.00015 CVN with 0.00003 set aside and block 12 as its date", coins)
	} else if groups := planRenewal(coins, 7, renewHorizonBlocks); len(groups) != 1 || len(groups[0]) != 1 {
		t.Errorf("renewing would not take in the small coin with a fifth set aside: %+v", groups)
	}
	mineTo(t, timestamp, 11, testAddress("other"))
	vaultBefore := vaultRecord(t)
	expiringFromOthers := int64(0)
	for k, coin := range everyStoredCoin(t) {
		if k != key && coin.Age == 7 { // other coins whose second date is also block 12
			expiringFromOthers += coin.Held
		}
	}
	mineNext(t, timestamp, testAddress("other")) // block 12: its second date
	wantCoin(t, "after its second date", tiny, 12000, 0, 12)
	if !sweptAt(12) {
		t.Error("block 12's sweep does not record the small coin; removing the block could not put its fifth back")
	}
	if got := vaultRecord(t) - vaultBefore - expiringFromOthers; got != 3000 {
		t.Errorf("the vault received %d units from the small coin at its second date; want the 3000 that were set aside", got)
	}
	nothingCreatedOrLost(t, "after the small coin's second date")

	// Its third date and its fourth: nothing happens to it.
	mineTo(t, timestamp, 22, testAddress("other"))
	wantCoin(t, "two dates later", tiny, 12000, 0, 12)
	if sweptAt(17) || sweptAt(22) {
		t.Error("a small coin with nothing set aside was swept again")
	}
	// The wallet is shown no date for it: nothing will happen to it.
	if coins := ListSpendableCoins(tiny); len(coins) != 1 || coins[0].SweepAt != 0 || coins[0].Amount.Units() != 12000 {
		t.Errorf("the wallet is offered %+v; want the coin of 0.00012 CVN with no date, since nothing will be taken from it", coins)
	}
	nothingCreatedOrLost(t, "at the end")
}

// Removing a block undoes what its sweep did, exactly: a fifth it set aside
// is back in the coin's amount, and a fifth it sent to the vault is set
// aside on the coin again. Putting the blocks back does the same sweeps
// again. On this chain one rollback can cross both dates of the same coin.
func TestSettingAsideAndExpiryAreUndoneWithTheirBlocks(t *testing.T) {
	timestamp := startSweepChain(t)
	idle := testAddress("idle")
	mineNext(t, timestamp, idle)
	fingerprints := map[int64]string{}
	for height := int64(2); height <= 12; height++ {
		mineNext(t, timestamp, testAddress("idle too"))
		fingerprints[height] = coinSetFingerprint()
	}
	tip := GetLatestBlockInternal()
	wantCoin(t, "at block 12", idle, cvn("32"), cvn("8"), 11)

	// A rival for the newest block's place is judged against the coins as
	// they were before the newest block. Block 12 swept the coin of block 2
	// for the second time.
	blockTwoCoin := utxoKey(GetBlockByHeightFromDB(2).Transactions[0].ID, 0)
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		record, found := lookupWithBlockUndone(dbTx, dbTx.Bucket(utxoBucket), tip)(blockTwoCoin)
		raw, _ := json.Marshal(record)
		var seen storedCoin
		_ = json.Unmarshal(raw, &seen)
		if !found || seen.Amount != cvn("40") || seen.Held != cvn("10") || seen.Age != 7 {
			t.Errorf("with block 12 undone, the coin of block 2 reads %s with %s set aside, age %d; want 40 with 10 set aside, age 7", FormatUnits(seen.Amount), FormatUnits(seen.Held), seen.Age)
		}
		return nil
	})

	rollBackTo := func(height int64) []Block {
		t.Helper()
		StateMutex.Lock()
		removed := rollbackChainToHeightLocked(height)
		StateMutex.Unlock()
		if got := GetLatestBlockInternal().Index; got != height {
			t.Fatalf("the rollback stopped at block %d, want %d", got, height)
		}
		if coinSetFingerprint() != fingerprints[height] {
			t.Fatalf("rolling back to block %d did not give back the coins of block %d", height, height)
		}
		nothingCreatedOrLost(t, "after a rollback")
		return removed
	}
	putBack := func(height int64, removed []Block) {
		t.Helper()
		StateMutex.Lock()
		restoreChainLocked(height, removed)
		StateMutex.Unlock()
		if GetLatestBlockInternal().Hash != tip.Hash || coinSetFingerprint() != fingerprints[12] {
			t.Fatal("putting the blocks back did not give back the same chain and the same coins")
		}
	}

	// Back over block 11, where the idle coin's fifth went to the vault.
	removed := rollBackTo(10)
	wantCoin(t, "with blocks 11 and 12 removed", idle, cvn("40"), cvn("10"), 6)
	if got := vaultRecord(t); got != 0 {
		t.Fatalf("with block 11 removed the vault holds %s; want nothing", FormatUnits(got))
	}
	putBack(10, removed)

	// Back over both dates in one rollback: blocks 6 and 11.
	removed = rollBackTo(5)
	wantCoin(t, "with blocks 6 to 12 removed", idle, cvn("50"), 0, 1)
	putBack(5, removed)
	wantCoin(t, "after everything was put back", idle, cvn("32"), cvn("8"), 11)
}

// A backup of a chain is checked by adding up its coins and its vault
// against what was issued. What is set aside on coins is part of that sum:
// a backup of a chain that has passed a date must pass its check, and one
// from which a fifth has gone missing must fail it.
func TestABackupCountsWhatIsSetAside(t *testing.T) {
	timestamp := startSweepChain(t)
	mineNext(t, timestamp, testAddress("idle"))
	mineTo(t, timestamp, 12, testAddress("idle too"))
	setAside := int64(0)
	for _, coin := range everyStoredCoin(t) {
		setAside += coin.Held
	}
	if setAside == 0 || vaultRecord(t) == 0 {
		t.Fatalf("set-up: the chain should have fifths set aside (%s) and coins in the vault (%s)", FormatUnits(setAside), FormatUnits(vaultRecord(t)))
	}
	path, report, err := writeBackup(filepath.Join(t.TempDir(), "backups"), 7)
	if err != nil {
		t.Fatalf("a backup of a chain with fifths set aside did not pass its own check: %v", err)
	}
	if report.CoinsUnits+report.VaultUnits != 12*cvn("50") {
		t.Errorf("the backup adds up to %s; want the 600 CVN issued", FormatUnits(report.CoinsUnits+report.VaultUnits))
	}

	// The same file with the fifth taken off one coin: coins have vanished,
	// and the check must say so.
	db, err := bbolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Update(func(dbTx *bbolt.Tx) error {
		bucket := dbTx.Bucket(utxoBucket)
		cursor := bucket.Cursor()
		for key, raw := cursor.First(); key != nil; key, raw = cursor.Next() {
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil || fields["held"] == nil {
				continue
			}
			delete(fields, "held")
			altered, _ := json.Marshal(fields)
			return bucket.Put(append([]byte{}, key...), altered)
		}
		t.Fatal("set-up: no coin in the backup has anything set aside")
		return nil
	})
	if closeErr := db.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	if _, err := checkChainFile(path, buildGenesisBlock().Hash); err == nil || !strings.Contains(err.Error(), "do not add up") {
		t.Errorf("a chain file from which a fifth set aside had been removed passed its check (err: %v)", err)
	}
}

// The wallet is told how much of a coin is set aside, and until when.
func TestTheWalletIsToldWhatIsSetAside(t *testing.T) {
	timestamp := startSweepChain(t)
	idle := testAddress("idle")
	mineNext(t, timestamp, idle)
	mineTo(t, timestamp, 6, testAddress("other"))
	raw, _ := json.Marshal(ListSpendableCoins(idle))
	for _, part := range []string{`"amount":5000000000`, `"held_units":1000000000`, `"sweep_at":11`} {
		if !strings.Contains(string(raw), part) {
			t.Errorf("a wallet asking for the coin is not told %s: %s", part, raw)
		}
	}
	// A coin nothing has been set aside on is written as it always was.
	raw, _ = json.Marshal(ListSpendableCoins(testAddress("other")))
	if strings.Contains(string(raw), "held_units") {
		t.Errorf("coins with nothing set aside are listed with the field all the same: %s", raw)
	}
}

// What the text wallet says about the coins' dates.
func TestWhatTheWalletSaysAboutDates(t *testing.T) {
	onNetwork(t, "devnet")
	plain := SpendableCoin{TxID: "TX_a", Amount: UnitsAmount(cvn("50")), SweepAt: 5000}
	swept := SpendableCoin{TxID: "TX_b", Amount: UnitsAmount(cvn("50")), HeldUnits: cvn("10"), SweepAt: 9000}
	sweptSooner := SpendableCoin{TxID: "TX_c", Amount: UnitsAmount(cvn("5")), HeldUnits: cvn("1"), SweepAt: 7000}
	undated := SpendableCoin{TxID: "TX_d", Amount: UnitsAmount(12000)}

	if lines := describeCoinDates([]SpendableCoin{undated}, 1000, true); len(lines) != 0 {
		t.Errorf("for a coin with no date the wallet said: %q", lines)
	}
	lines := strings.Join(describeCoinDates([]SpendableCoin{plain, undated}, 1000, true), "\n")
	if !strings.Contains(lines, "at the end of block #5000") || !strings.Contains(lines, "a fifth of it is set aside") || strings.Contains(lines, "was set aside") || !strings.Contains(lines, "no fee") {
		t.Errorf("for a coin nearing its first date the wallet said:\n%s", lines)
	}
	lines = strings.Join(describeCoinDates([]SpendableCoin{swept, sweptSooner, plain}, 1000, true), "\n")
	for _, want := range []string{"11 CVN of this was set aside", "It is still yours", "before the end of block #7000", "goes to the vault", "at the end of block #5000"} {
		if !strings.Contains(lines, want) {
			t.Errorf("the wallet did not say %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, "moves to the vault at the end of") {
		t.Errorf("the wallet still says a fifth moves to the vault at the first date:\n%s", lines)
	}
	// With no node to say what the newest block is, the blocks are named
	// and no time is guessed.
	if lines = strings.Join(describeCoinDates([]SpendableCoin{swept}, 0, false), "\n"); strings.Contains(lines, "from now") || !strings.Contains(lines, "block #9000") {
		t.Errorf("with the height unknown the wallet said:\n%s", lines)
	}
}

// Renewing takes in every coin with a fifth set aside, however far its next
// date, since renewing is how its owner takes the fifth back; and its
// result says how much was taken back.
func TestRenewTakesInEveryCoinWithAFifthSetAside(t *testing.T) {
	onNetwork(t, "devnet")
	const tip = 1000000
	far := tip + renewHorizonBlocks*5
	coins := []SpendableCoin{
		{TxID: "plain-far", Amount: UnitsAmount(cvn("50")), SweepAt: far},
		{TxID: "plain-near", Amount: UnitsAmount(cvn("50")), SweepAt: tip + 10},
		{TxID: "swept-far", Amount: UnitsAmount(cvn("50")), HeldUnits: cvn("10"), SweepAt: far},
		{TxID: "small-undated", Amount: UnitsAmount(12000)},
	}
	groups := planRenewal(coins, tip, renewHorizonBlocks)
	if len(groups) != 1 || len(groups[0]) != 2 || groups[0][0].TxID != "plain-near" || groups[0][1].TxID != "swept-far" {
		t.Fatalf("renewing chose %+v; want the coin near its date and the one with a fifth set aside, and no others", groups)
	}
	result := RenewResult{Address: "CVN_x", CoinsRenewed: 2, TakenBackUnits: cvn("10"), Transfers: []Transaction{{}}}
	if summary := result.Summary(); !strings.Contains(summary, "hold the 10 CVN that had been set aside") {
		t.Errorf("the result does not say what was taken back: %q", summary)
	}
	if summary := (RenewResult{Address: "CVN_x", CoinsRenewed: 1, Transfers: []Transaction{{}}}).Summary(); strings.Contains(summary, "set aside") {
		t.Errorf("the result speaks of something set aside when nothing was: %q", summary)
	}
}

// The explorer's figure, and the node's answer that carries it, are the sum
// of every fifth set aside on a coin.
func TestTheFigureOfAllFifthsSetAside(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner)
	mineTo(t, timestamp, 12, testAddress("idle too"))
	sum := func() int64 {
		var total int64
		for _, coin := range everyStoredCoin(t) {
			total += coin.Held
		}
		return total
	}
	answered := func() int64 {
		recorder := httptest.NewRecorder()
		syncAPI().ServeHTTP(recorder, httptest.NewRequest("GET", "/block", nil))
		var reply struct {
			Held  *int64 `json:"held_units"`
			Vault int64  `json:"vault_units"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &reply); err != nil || reply.Held == nil {
			t.Fatalf("the node's answer about its newest block does not say what is set aside: %s", recorder.Body.String())
		}
		if reply.Vault != vaultRecord(t) {
			t.Errorf("the node answers a vault of %d units; its record says %d", reply.Vault, vaultRecord(t))
		}
		return *reply.Held
	}
	if want := sum(); want == 0 || SetAsideTotalUnits() != want || answered() != want {
		t.Fatalf("set aside on coins: %s; the program's figure is %s and the node answers %s", FormatUnits(want), FormatUnits(SetAsideTotalUnits()), FormatUnits(answered()))
	}
	// After a rollback to before any date the figure is nought, and with
	// the blocks put back it is what it was.
	atTwelve := sum()
	StateMutex.Lock()
	removed := rollbackChainToHeightLocked(5)
	StateMutex.Unlock()
	if SetAsideTotalUnits() != 0 || sum() != 0 || answered() != 0 {
		t.Errorf("rolled back to before any date, %s is still counted as set aside", FormatUnits(SetAsideTotalUnits()))
	}
	StateMutex.Lock()
	restoreChainLocked(5, removed)
	StateMutex.Unlock()
	if SetAsideTotalUnits() != atTwelve || sum() != atTwelve || answered() != atTwelve {
		t.Errorf("with the blocks put back the figure is %s; want %s again", FormatUnits(SetAsideTotalUnits()), FormatUnits(atTwelve))
	}

	// A transfer that spends a coin with a fifth set aside takes it back.
	_, coin := theCoinOf(t, owner)
	if coin.Held == 0 {
		t.Fatal("set-up: the owner's coin should have a fifth set aside")
	}
	renewal, err := BuildSignedRenewal(privateKey, publicKey, 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, testAddress("idle too"), renewal)
	// Block 13 also sweeps other coins; only the sum of what is on coins
	// now is asked of the figure.
	if want := sum(); SetAsideTotalUnits() != want || answered() != want {
		t.Errorf("after a renewal: set aside on coins %s; the program's figure is %s and the node answers %s", FormatUnits(want), FormatUnits(SetAsideTotalUnits()), FormatUnits(answered()))
	}
	if _, renewed := theCoinOf(t, owner); renewed.Held != 0 || renewed.Amount != coin.Amount+coin.Held {
		t.Errorf("the renewed coin holds %s with %s set aside; want %s and nothing", FormatUnits(renewed.Amount), FormatUnits(renewed.Held), FormatUnits(coin.Amount+coin.Held))
	}
}

// A pool pays from coins it has on record at the amount they were made
// with. A coin of the pool's that has passed its first date is worth the
// same to spend (its amount and the fifth set aside), so the pool can still
// pay from it. A coin a day or less from its date is left alone, as before:
// a payment spending it could be overtaken by the sweep.
func TestThePoolAndACoinWithAFifthSetAside(t *testing.T) {
	startTestChain(t, "devnet")
	// A coin's date is 1,925 blocks after it was made or last swept, five
	// more than the day a pool keeps clear of it.
	economics := *Params.Economics
	economics.SweepIntervalBlocks = poolSweepMarginBlocks + 5
	Params.Economics = &economics
	savedPool := poolAddress
	poolAddress = testAddress("the pool")
	t.Cleanup(func() { poolAddress = savedPool })

	timestamp := Params.GenesisTimestamp + 100
	mine := func() {
		timestamp += 45
		extendTestChain(t, timestamp, poolAddress, nil)
	}
	mine() // block 1: a coin of the pool's, of 50 CVN
	onRecord := poolCoin{TxID: GetBlockByHeightFromDB(1).Transactions[0].ID, Index: 0, Units: cvn("50")}
	for GetLatestBlockInternal().Index < 5 {
		mine()
	}
	if !poolCoinUsable(onRecord) {
		t.Fatal("at block 5 a coin made at block 1 is six blocks more than a day from its date, and the pool would not spend it")
	}
	mine() // block 6: a day from its date
	if poolCoinUsable(onRecord) {
		t.Error("at block 6 the coin is a day from its date and the pool would still spend it")
	}

	// The same coin as a sweep would leave it: 40 with 10 set aside, its
	// age the block of the sweep. It is worth 50 to spend, as on record.
	rewrite := func(record UTXORecord) {
		t.Helper()
		raw, _ := json.Marshal(record)
		if err := GlobalBoltEngine.Update(func(dbTx *bbolt.Tx) error {
			return dbTx.Bucket(utxoBucket).Put([]byte(onRecord.key()), raw)
		}); err != nil {
			t.Fatal(err)
		}
	}
	tip := GetLatestBlockInternal().Index
	rewrite(UTXORecord{Amount: UnitsAmount(cvn("40")), Recipient: poolAddress, Age: tip, HeldUnits: cvn("10")})
	if !poolCoinUsable(onRecord) {
		t.Error("a coin of 40 CVN with 10 set aside is worth the 50 on the pool's record, and the pool would not spend it")
	}
	// After its second date the 10 is gone to the vault: it is no longer
	// the coin the pool has on record, and the pool must not pay from it
	// at the old figure.
	rewrite(UTXORecord{Amount: UnitsAmount(cvn("32")), Recipient: poolAddress, Age: tip, HeldUnits: cvn("8")})
	if poolCoinUsable(onRecord) {
		t.Error("a coin now worth 40 CVN is taken for the 50 on the pool's record")
	}
}

// ---------------------------------------------------------------------------
// Added after the reviews of this change
// ---------------------------------------------------------------------------

// heldOnCoins is the sum of every fifth set aside, read from the coins.
func heldOnCoins(t *testing.T) int64 {
	t.Helper()
	var total int64
	for _, coin := range everyStoredCoin(t) {
		total += coin.Held
	}
	return total
}

// takenByTheNode offers a block the way one from a neighbour is offered:
// checked first, then stored.
func takenByTheNode(block Block) bool {
	return ValidateBlockSignatures(block) && SaveBlockToStorageInternal(block)
}

// isWaiting reports whether a transfer is among those a node holds for the
// next blocks.
func isWaiting(id string) bool {
	MempoolMatrix.Lock()
	defer MempoolMatrix.Unlock()
	_, waiting := MempoolMatrix.Transactions[id]
	return waiting
}

// Removing a block that spent a coin with a fifth set aside puts the coin
// back as it was, fifth and all, and the transfer that spent it goes back to
// waiting and is still good. The transfer here pays a fee and spends an
// ordinary coin beside the swept one.
func TestRemovingABlockThatSpentTheCoinPutsItsFifthBack(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	first := mineNext(t, timestamp, owner)           // block 1: the coin that will be swept
	mineTo(t, timestamp, 6, testAddress("idle too")) // its first date
	second := mineNext(t, timestamp, owner)          // block 7: a second coin, with nothing set aside
	sweptKey, plainKey := utxoKey(first.Transactions[0].ID, 0), utxoKey(second.Transactions[0].ID, 0)
	if coins := everyStoredCoin(t); coins[sweptKey].Amount != cvn("40") || coins[sweptKey].Held != cvn("10") || coins[plainKey].Amount != cvn("50") || coins[plainKey].Held != 0 {
		t.Fatalf("set-up: the owner's coins are %+v and %+v; want 40 with 10 set aside, and 50", coins[sweptKey], coins[plainKey])
	}
	if got := balanceOf(owner); got != cvn("100") {
		t.Fatalf("set-up: the owner's address shows %s CVN, want 100", FormatUnits(got))
	}

	// 80 CVN to a shop with a fee of 1: it takes both coins, each stated at
	// what it is worth to spend.
	pay, err := BuildSignedTransfer(privateKey, publicKey, testAddress("shop"), cvn("80"), cvn("1"), ListSpendableCoins(owner))
	if err != nil {
		t.Fatalf("the payment could not be built: %v", err)
	}
	if len(pay.Inputs) != 2 || pay.Inputs[0].AmountUnits != cvn("50") || pay.Inputs[1].AmountUnits != cvn("50") {
		t.Fatalf("the payment spends %+v; want both coins, each stated at 50", pay.Inputs)
	}
	atSeven, heldAtSeven := coinSetFingerprint(), heldOnCoins(t)
	mineNext(t, timestamp, testAddress("idle too"), pay) // block 8
	tip := GetLatestBlockInternal()
	if len(tip.Transactions) != 2 {
		t.Fatal("block 8 does not hold the payment")
	}
	coins := everyStoredCoin(t)
	if _, still := coins[sweptKey]; still {
		t.Error("the swept coin is still there after it was spent")
	}
	wantCoin(t, "after the payment", testAddress("shop"), cvn("80"), 0, 8)
	wantCoin(t, "after the payment", owner, cvn("19"), 0, 8)
	nothingCreatedOrLost(t, "after the payment")
	if SetAsideTotalUnits() != heldOnCoins(t) {
		t.Errorf("after the payment the program counts %s set aside and the coins hold %s", FormatUnits(SetAsideTotalUnits()), FormatUnits(heldOnCoins(t)))
	}
	atEight := coinSetFingerprint()

	// Block 8 is removed.
	StateMutex.Lock()
	removed := rollbackChainToHeightLocked(7)
	StateMutex.Unlock()
	if coinSetFingerprint() != atSeven {
		t.Fatal("removing block 8 did not give back the coins of block 7")
	}
	coins = everyStoredCoin(t)
	if back := coins[sweptKey]; back.Amount != cvn("40") || back.Held != cvn("10") || back.Age != 6 || back.Recipient != owner {
		t.Fatalf("with block 8 removed the swept coin reads %+v; want 40 with 10 set aside, age 6, its owner's", back)
	}
	if got := balanceOf(owner); got != cvn("100") {
		t.Errorf("with block 8 removed the owner's address shows %s CVN, want 100 again", FormatUnits(got))
	}
	if SetAsideTotalUnits() != heldAtSeven || heldOnCoins(t) != heldAtSeven {
		t.Errorf("with block 8 removed %s is counted as set aside; want %s again", FormatUnits(SetAsideTotalUnits()), FormatUnits(heldAtSeven))
	}
	nothingCreatedOrLost(t, "with block 8 removed")
	// The payment goes back to waiting, and is good against the coin with
	// its fifth back on it.
	for deadline := time.Now().Add(10 * time.Second); !isWaiting(pay.ID); {
		if time.Now().After(deadline) {
			t.Fatal("the payment of the removed block did not go back to waiting")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if verdict := verdictOnTransfer(t, pay); verdict != nil {
		t.Errorf("with block 8 removed its payment is no longer good: %v", verdict)
	}

	// And put back.
	StateMutex.Lock()
	restoreChainLocked(7, removed)
	StateMutex.Unlock()
	if GetLatestBlockInternal().Hash != tip.Hash || coinSetFingerprint() != atEight {
		t.Fatal("putting block 8 back did not give back the same chain and the same coins")
	}
	if isWaiting(pay.ID) {
		t.Error("the payment is still waiting after the block that holds it was put back")
	}
}

// Two blocks compete for the place at a coin's date. One holds the owner's
// renewal and the other does not. Whichever holds the place, the coins and
// the vault must be exactly what that block alone would have left, with no
// trace of the block it replaced: at the first date a fifth set aside or
// not, at the second the fifth gone to the vault or taken back.
func TestRivalBlocksAtACoinsDate(t *testing.T) {
	for _, date := range []struct {
		name                          string
		height                        int64
		amountLeft, setAside, toVault int64
	}{
		{"the first date", 6, cvn("40"), cvn("10"), 0},
		{"the second date", 11, cvn("32"), cvn("8"), cvn("10")},
	} {
		t.Run(date.name, func(t *testing.T) {
			timestamp := startSweepChain(t)
			privateKey, publicKey, owner, err := GenerateKeyPair()
			if err != nil {
				t.Fatal(err)
			}
			mineNext(t, timestamp, owner)
			mineTo(t, timestamp, date.height-1, testAddress("idle too"))
			renewal, err := BuildSignedRenewal(privateKey, publicKey, 0, ListSpendableCoins(owner))
			if err != nil {
				t.Fatal(err)
			}
			if renewal.Inputs[0].AmountUnits != cvn("50") {
				t.Fatalf("set-up: the renewal states the coin at %s, want 50", FormatUnits(renewal.Inputs[0].AmountUnits))
			}

			// Twenty blocks without the renewal and twenty with it, all for
			// the same place and mined before any is stored. Which of two
			// wins is settled by the rule for rivals, so they are put in
			// that order and three are picked: a block with the renewal, a
			// block without it that beats it, and a block with the renewal
			// that beats that. (Only if every block with the renewal came
			// together in the order would there be no such three: one
			// chance in thousands of millions.)
			*timestamp += 45
			type candidate struct {
				block    Block
				renewing bool
			}
			var candidates []candidate
			for i := 0; i < 20; i++ {
				candidates = append(candidates,
					candidate{mineTestBlock(t, *timestamp, testAddress(fmt.Sprintf("plain miner %d", i)), nil, 0), false},
					candidate{mineTestBlock(t, *timestamp, testAddress(fmt.Sprintf("renewing miner %d", i)), []Transaction{renewal}, 0), true})
			}
			sort.Slice(candidates, func(i, j int) bool { return siblingWins(candidates[i].block, candidates[j].block) })
			var worse, plain, better *Block
			for i := 1; i < len(candidates)-1 && better == nil; i++ {
				if candidates[i].renewing {
					continue
				}
				var before, after *Block
				for j := range candidates {
					if candidates[j].renewing && j < i && before == nil {
						before = &candidates[j].block
					}
					if candidates[j].renewing && j > i {
						after = &candidates[j].block
					}
				}
				if before != nil && after != nil {
					worse, plain, better = after, &candidates[i].block, before
				}
			}
			if better == nil {
				t.Fatal("the forty blocks did not fall in an order this test can use; run it again")
			}
			if !siblingWins(*plain, *worse) || !siblingWins(*better, *plain) {
				t.Fatal("the three blocks are not rivals in the order this test needs")
			}
			minerOf := func(block *Block) string { return block.Transactions[0].Outputs[0].Recipient }

			check := func(when string, holder *Block, renewed bool) {
				t.Helper()
				if GetLatestBlockInternal().Hash != holder.Hash {
					t.Fatalf("%s: the newest block is not the one that should hold the place", when)
				}
				wantVault, wantBalance := date.toVault, date.amountLeft+date.setAside
				if renewed {
					wantVault, wantBalance = 0, cvn("50")
					wantCoin(t, when, owner, cvn("50"), 0, date.height)
				} else {
					wantCoin(t, when, owner, date.amountLeft, date.setAside, date.height)
				}
				if got := vaultRecord(t); got != wantVault {
					t.Errorf("%s: the vault holds %s, want %s", when, FormatUnits(got), FormatUnits(wantVault))
				}
				if got := balanceOf(owner); got != wantBalance {
					t.Errorf("%s: the owner's address shows %s CVN, want %s", when, FormatUnits(got), FormatUnits(wantBalance))
				}
				for _, other := range []*Block{worse, plain, better} {
					want := int64(0)
					if other == holder {
						want = cvn("50")
					}
					if got := balanceOf(minerOf(other)); got != want {
						t.Errorf("%s: the miner of one of the three blocks has %s CVN, want %s", when, FormatUnits(got), FormatUnits(want))
					}
				}
				if SetAsideTotalUnits() != heldOnCoins(t) {
					t.Errorf("%s: the program counts %s set aside and the coins hold %s", when, FormatUnits(SetAsideTotalUnits()), FormatUnits(heldOnCoins(t)))
				}
				nothingCreatedOrLost(t, when)
			}

			if !takenByTheNode(*worse) {
				t.Fatal("the first block, with the renewal, was refused")
			}
			check("with the renewing block in place", worse, true)
			if !takenByTheNode(*plain) {
				t.Fatal("the block without the renewal, which beats the first, was refused")
			}
			check("after the block without the renewal took the place", plain, false)
			if !takenByTheNode(*better) {
				t.Fatal("the second block with the renewal, which beats the one in place, was refused: it must be judged against the coin as it was before the date")
			}
			check("after a block with the renewal took the place back", better, true)

			// Blocks that lose change nothing.
			atRest := coinSetFingerprint()
			if SaveBlockToStorageInternal(*plain) || SaveBlockToStorageInternal(*worse) {
				t.Fatal("a rival block that loses was stored")
			}
			if coinSetFingerprint() != atRest {
				t.Fatal("offering blocks that lose changed the coins")
			}
			check("after the losing blocks were offered again", better, true)
		})
	}
}

// A renewal still waiting when the coin's second date passes is no longer
// good: it was signed for the coin with its fifth. A miner filling its next
// block leaves it out and stops keeping it. The fifth has gone to the vault.
func TestARenewalThatWaitedPastTheSecondDateIsDropped(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner)
	mineTo(t, timestamp, 10, testAddress("idle too"))
	renewal, err := BuildSignedRenewal(privateKey, publicKey, 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	if err := AdmitTransactionToMempool(renewal); err != nil {
		t.Fatalf("set-up: the renewal was not taken among those waiting: %v", err)
	}
	if chosen, _ := SelectMempoolTransactionsForBlock(10); len(chosen) != 1 || chosen[0].ID != renewal.ID {
		t.Fatalf("set-up: a miner filling block 11 would choose %d transfers; want the renewal", len(chosen))
	}
	mineNext(t, timestamp, testAddress("idle too")) // block 11, mined by someone who left it out
	wantCoin(t, "after its second date", owner, cvn("32"), cvn("8"), 11)
	if chosen, _ := SelectMempoolTransactionsForBlock(10); len(chosen) != 0 {
		t.Errorf("a miner filling block 12 would put in %d transfer(s); the renewal is no longer good and must be left out", len(chosen))
	}
	if isWaiting(renewal.ID) {
		t.Error("the node still keeps the renewal, which can never be mined now")
	}
	// The block the miner would have built with it is refused.
	*timestamp += 45
	bad := mineTestBlockPaying(t, *timestamp, testAddress("idle too"), []UTXOOutput{{Recipient: testAddress("idle too"), Amount: UnitsAmount(cvn("50"))}}, []Transaction{renewal})
	if ValidateBlockSignatures(bad) || SaveBlockToStorageInternal(bad) {
		t.Error("a block holding the stale renewal was accepted")
	}
	nothingCreatedOrLost(t, "at the end")
}

// whatIsPrinted runs work and returns what the program printed meanwhile.
func whatIsPrinted(t *testing.T, work func()) string {
	t.Helper()
	backgroundWork.Wait() // nothing else is printing
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = writer
	printed := make(chan string)
	go func() {
		raw, _ := io.ReadAll(reader)
		printed <- string(raw)
	}()
	work()
	os.Stdout = saved
	writer.Close()
	return <-printed
}

// Only the sweep writes what is set aside on a coin, and never less than
// nothing. A record that says otherwise is damage. The wallet is not offered
// such a coin and it cannot be spent; and when its date comes the node
// refuses the block, changes nothing, and says why, so that whoever runs it
// can see the database is damaged.
func TestACoinRecordWithLessThanNothingSetAsideStopsTheBlock(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner)
	mineTo(t, timestamp, 5, testAddress("other"))
	key, _ := theCoinOf(t, owner)
	raw, _ := json.Marshal(UTXORecord{Amount: UnitsAmount(cvn("50")), Recipient: owner, Age: 1, HeldUnits: -1})
	if err := GlobalBoltEngine.Update(func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(utxoBucket).Put([]byte(key), raw)
	}); err != nil {
		t.Fatal(err)
	}
	if coins := ListSpendableCoins(owner); len(coins) != 0 {
		t.Errorf("the wallet is offered a coin with less than nothing set aside: %+v", coins)
	}
	for _, stated := range []int64{cvn("50"), cvn("50") - 1} {
		if verdict := verdictOnTransfer(t, spendStating(t, privateKey, publicKey, owner, stated)); verdict == nil {
			t.Errorf("a transfer spending the damaged coin, stating %d units, was accepted", stated)
		}
	}

	before, tip := coinSetFingerprint(), GetLatestBlockInternal()
	*timestamp += 45
	block := mineTestBlock(t, *timestamp, testAddress("other"), nil, 0) // block 6: the coin's date
	stored := true
	said := whatIsPrinted(t, func() { stored = SaveBlockToStorageInternal(block) })
	if stored || GetLatestBlockInternal().Hash != tip.Hash {
		t.Fatal("a block was stored over a coin record with less than nothing set aside")
	}
	if coinSetFingerprint() != before {
		t.Error("the refused block changed the coins")
	}
	if !strings.Contains(said, "STORAGE REFUSAL") || !strings.Contains(said, "set aside") {
		t.Errorf("the node refused the block without saying why; it printed: %q", said)
	}
}

// sweepByHand writes one coin record exactly as given into a database, due
// at the next date, runs the program's own sweep over it, and returns what
// the sweep left in its place, what it sent to the vault and whether it
// recorded the coin. Nothing is kept: the database is afterwards as it was.
func sweepByHand(t testing.TB, db *bbolt.DB, coin UTXORecord) (after UTXORecord, toVault int64, recorded bool) {
	t.Helper()
	const key = "TX_WRITTEN_BY_HAND_0"
	date := 1 + Params.Economics.SweepIntervalBlocks
	coin.Age = 1
	undo := errors.New("undo")
	err := db.Update(func(dbTx *bbolt.Tx) error {
		bUTXO, err := dbTx.CreateBucketIfNotExists(utxoBucket)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(coin)
		if err != nil {
			return err
		}
		if err := bUTXO.Put([]byte(key), raw); err != nil {
			return err
		}
		// The sweep finds a coin through the block that made it or the
		// sweep that last touched it. Here it is a sweep at block 1.
		if err := storeSweepRecords(dbTx, 1, []SweptCoin{{Key: key, Before: coin, Kept: true}}); err != nil {
			return err
		}
		var changed bool
		if toVault, changed, err = applySweep(dbTx, bUTXO, Block{Index: date}); err != nil {
			return err
		}
		if err := json.Unmarshal(bUTXO.Get([]byte(key)), &after); err != nil {
			return err
		}
		for _, record := range loadSweepRecords(dbTx, date) {
			if record.Key == key {
				recorded = true
				if before, _ := json.Marshal(record.Before); string(before) != string(raw) {
					return fmt.Errorf("the sweep recorded the coin as %s; it was %s", before, raw)
				}
			}
		}
		if changed != recorded {
			return fmt.Errorf("the sweep says it changed a coin (%v) and recorded it (%v)", changed, recorded)
		}
		return undo
	})
	if err != undo {
		t.Fatalf("the sweep of a coin of %d units with %d set aside failed: %v", coin.Amount.Units(), coin.HeldUnits, err)
	}
	return after, toVault, recorded
}

// emptyDatabase opens a database with nothing in it, for sweepByHand.
func emptyDatabase(t testing.TB) *bbolt.DB {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "by-hand.db"), 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// The sweep's arithmetic on coins written by hand, with the answers worked
// out by hand: awkward amounts, the amounts either side of the size below
// which no fifth is taken, and the largest amount there can be.
func TestTheSweepOnCoinsWrittenByHand(t *testing.T) {
	onNetwork(t, "devnet")
	db := emptyDatabase(t)
	date := 1 + Params.Economics.SweepIntervalBlocks
	const whole = int64(210000000000000000) // every coin there will ever be: 2,100,000,000 CVN
	if whole != MaxAmountUnits || minimumOutputUnits() != 10000 {
		t.Fatalf("set-up: this test's figures are for a supply of %d units and a smallest coin of 10000; the program has %d and %d", whole, MaxAmountUnits, minimumOutputUnits())
	}
	for _, c := range []struct {
		what                 string
		amount, held         int64
		wantAmount, wantHeld int64
		wantToVault          int64
		wantTouched          bool
	}{
		{"50 CVN, never swept", 5000000000, 0, 4000000000, 1000000000, 0, true},
		{"40 CVN with 10 set aside", 4000000000, 1000000000, 3200000000, 800000000, 1000000000, true},
		{"an amount that does not divide by five", 4999999999, 0, 4000000000, 999999999, 0, true},
		{"one unit more than divides", 5000000001, 0, 4000000001, 1000000000, 0, true},
		{"four units more than divides, three set aside", 5000000004, 3, 4000000004, 1000000000, 3, true},
		{"leaves exactly the smallest coin", 12500, 0, 10000, 2500, 0, true},
		{"leaves exactly the smallest coin, the fifth rounded down", 12499, 0, 10000, 2499, 0, true},
		{"would leave one unit less than the smallest coin", 12498, 0, 12498, 0, 0, false},
		{"the same, with a fifth set aside before", 12498, 3124, 12498, 0, 3124, true},
		{"the smallest coin", 10000, 0, 10000, 0, 0, false},
		{"the smallest coin with one unit set aside", 10000, 1, 10000, 0, 1, true},
		{"every coin there is, in one", whole, 0, 168000000000000000, 42000000000000000, 0, true},
		{"one unit less than that", whole - 1, 0, 168000000000000000, 41999999999999999, 0, true},
		{"four fifths of everything, with the last fifth set aside", 168000000000000000, 42000000000000000, 134400000000000000, 33600000000000000, 42000000000000000, true},
	} {
		after, toVault, recorded := sweepByHand(t, db, UTXORecord{Amount: UnitsAmount(c.amount), Recipient: testAddress("by hand"), HeldUnits: c.held})
		wantAge := int64(1)
		if c.wantTouched {
			wantAge = date
		}
		if after.Amount.Units() != c.wantAmount || after.HeldUnits != c.wantHeld || toVault != c.wantToVault || recorded != c.wantTouched || after.Age != wantAge || after.Recipient != testAddress("by hand") {
			t.Errorf("%s (%d units, %d set aside): the sweep left %d with %d set aside at age %d, sent %d to the vault, recorded %v; want %d with %d at age %d, %d to the vault, recorded %v",
				c.what, c.amount, c.held, after.Amount.Units(), after.HeldUnits, after.Age, toVault, recorded, c.wantAmount, c.wantHeld, wantAge, c.wantToVault, c.wantTouched)
		}
		if after.SpendUnits() != c.amount || after.SpendUnits()+toVault != c.amount+c.held {
			t.Errorf("%s: units appeared or went missing", c.what)
		}
	}
}

// FuzzAFifthIsSetAsideOrNothing runs the program's own sweep over one coin
// of any amount, with anything set aside on it, and checks: the coin's
// amount and its new fifth are together exactly the amount it had; the new
// fifth is a fifth rounded down, or nothing when what remained would be
// less than the smallest coin; the vault receives exactly what had been set
// aside before, and nothing of the new fifth; a coin nothing happens to is
// left as it was; and no unit appears or goes missing.
func FuzzAFifthIsSetAsideOrNothing(f *testing.F) {
	saved := Params
	if err := SelectNetwork("devnet"); err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { Params = saved; applyParams() })
	db := emptyDatabase(f)
	minimum := minimumOutputUnits()
	date := 1 + Params.Economics.SweepIntervalBlocks
	f.Add(int64(5000000000), int64(0))
	f.Add(int64(4000000000), int64(1000000000))
	f.Add(int64(15000), int64(0))
	f.Add(int64(12000), int64(3000))
	f.Add(int64(12498), int64(0))
	f.Add(int64(12498), int64(3124))
	f.Add(int64(12499), int64(0))
	f.Add(int64(12500), int64(1))
	f.Add(int64(10000), int64(0))
	f.Add(int64(4999999999), int64(7))
	f.Add(MaxAmountUnits, int64(0))
	f.Fuzz(func(t *testing.T, amount, setAside int64) {
		// Any amount a coin can hold, and anything set aside up to a
		// quarter of it (a fifth of what it held before is a quarter of
		// what it kept).
		if amount < 0 {
			amount = -(amount + 1)
		}
		if amount %= MaxAmountUnits + 1; amount == 0 {
			amount = 1
		}
		if setAside < 0 {
			setAside = -(setAside + 1)
		}
		setAside %= amount/4 + 1

		before := UTXORecord{Amount: UnitsAmount(amount), Recipient: "CVN_fuzz", HeldUnits: setAside}
		after, toVault, recorded := sweepByHand(t, db, before)

		wantNew := int64(0)
		if amount-amount/5 >= minimum {
			wantNew = amount / 5
		}
		if after.HeldUnits != wantNew || after.Amount.Units() != amount-wantNew {
			t.Fatalf("a coin of %d: the sweep left %d with %d set aside; want %d with %d (a fifth rounded down, or nothing if less than %d would be left)", amount, after.Amount.Units(), after.HeldUnits, amount-wantNew, wantNew, minimum)
		}
		if toVault != setAside {
			t.Fatalf("a coin of %d with %d set aside: the vault received %d; want exactly what had been set aside", amount, setAside, toVault)
		}
		if after.SpendUnits()+toVault != before.SpendUnits() || after.SpendUnits() != amount {
			t.Fatalf("a coin of %d with %d set aside: after the sweep it is worth %d and the vault received %d; units appeared or went missing", amount, setAside, after.SpendUnits(), toVault)
		}
		touched := setAside > 0 || wantNew > 0
		wantAge := int64(1)
		if touched {
			wantAge = date
		}
		if recorded != touched || after.Age != wantAge || after.Recipient != before.Recipient {
			t.Fatalf("a coin of %d with %d set aside: recorded %v, age %d, owner %q; want recorded %v, age %d, owner unchanged", amount, setAside, recorded, after.Age, after.Recipient, touched, wantAge)
		}
		if after.HeldUnits < 0 || after.Amount.Units() <= 0 || after.SpendUnits() > MaxAmountUnits {
			t.Fatalf("a coin of %d became %d with %d set aside, which is not a coin", amount, after.Amount.Units(), after.HeldUnits)
		}
	})
}

// serveTheWallet answers a wallet's requests from this test's chain, as a
// node on the same machine would.
func serveTheWallet(t *testing.T) {
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
	// A node gives each caller only so many lists of coins in a short time,
	// and every test here asks from the same address. Each starts with a
	// fresh allowance.
	IPTokensMutex.Lock()
	for key := range IPTokensMap {
		if strings.HasPrefix(key, "coins|") {
			delete(IPTokensMap, key)
		}
	}
	IPTokensMutex.Unlock()
}

// theOneWaiting returns the single transfer a node holds for the next
// blocks, and fails the test if there is none or more than one.
func theOneWaiting(t *testing.T) Transaction {
	t.Helper()
	MempoolMatrix.Lock()
	defer MempoolMatrix.Unlock()
	if len(MempoolMatrix.Transactions) != 1 {
		t.Fatalf("%d transfers are waiting, want one", len(MempoolMatrix.Transactions))
	}
	for _, tx := range MempoolMatrix.Transactions {
		return tx
	}
	return Transaction{}
}

// The wallet's own renewing, from end to end against a node: a coin with a
// fifth set aside is sent back to its owner with no fee, the wallet says
// how much that takes back once it confirms, and when it is mined the coin
// is whole again. With a date so near and no fee, the wallet also says to
// check that it went through.
func TestTheWalletRenewsFreeAndSaysWhatItTakesBack(t *testing.T) {
	timestamp := startSweepChain(t)
	serveTheWallet(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner)
	mineTo(t, timestamp, 7, testAddress("idle too")) // 40 with 10 set aside; its next date is block 11
	result, err := RenewCoinsFromWallet(privateKey, publicKey, 0, false)
	if err != nil {
		t.Fatalf("the renewal failed: %v", err)
	}
	if result.CoinsRenewed != 1 || len(result.Transfers) != 1 || result.TakenBackUnits != cvn("10") || !result.CloseToDate {
		t.Fatalf("the wallet renewed %d coin(s) in %d transfer(s), taking back %s, date close: %v; want 1, 1, 10 and true", result.CoinsRenewed, len(result.Transfers), FormatUnits(result.TakenBackUnits), result.CloseToDate)
	}
	sent := theOneWaiting(t)
	if sent.ID != result.Transfers[0].ID || sent.FreeWillOffering.Units() != 0 || len(sent.Inputs) != 1 || sent.Inputs[0].AmountUnits != cvn("50") || sent.Outputs[0].Amount.Units() != cvn("50") {
		t.Fatalf("the node holds %+v; want the wallet's renewal: no fee, the coin stated at 50, one new coin of 50", sent)
	}
	summary := result.Summary()
	for _, want := range []string{"Once they confirm", "10 CVN that had been set aside", "less than two days", "no fee"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the wallet does not say %q: %s", want, summary)
		}
	}
	if strings.Contains(summary, "This takes back") {
		t.Errorf("the wallet says the fifth is taken back before the renewal has confirmed: %s", summary)
	}
	// With no date near, nothing is said about waiting.
	farOff := result
	farOff.CloseToDate = false
	if strings.Contains(farOff.Summary(), "less than two days") {
		t.Errorf("the wallet warns of a near date when there is none to warn of: %s", farOff.Summary())
	}

	chosen, _ := SelectMempoolTransactionsForBlock(10)
	mineNext(t, timestamp, testAddress("idle too"), chosen...) // block 8
	wantCoin(t, "after the renewal was mined", owner, cvn("50"), 0, 8)
	if got := vaultRecord(t); got != 0 {
		t.Errorf("the vault holds %s; want nothing", FormatUnits(got))
	}
	nothingCreatedOrLost(t, "after the renewal was mined")
}

// The window wallet's Renew and Combine send with no fee when the screen
// passes none (the box "pay a fee" unticked), and with the fee named when it
// passes one.
func TestTheWindowWalletRenewsAndCombinesFreeUnlessAFeeIsNamed(t *testing.T) {
	t.Chdir(t.TempDir()) // no saved key of this machine is looked at
	timestamp := startSweepChain(t)
	serveTheWallet(t)
	privateKey, _, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	wallet := &App{}
	mineNext(t, timestamp, owner)
	mineTo(t, timestamp, 7, testAddress("idle too")) // the owner's coin: 40 with 10 set aside
	mineIt := func() Transaction {
		t.Helper()
		sent := theOneWaiting(t)
		mineNext(t, timestamp, testAddress("idle too"), sent)
		if isWaiting(sent.ID) {
			t.Fatal("the transfer is still waiting after it was mined")
		}
		return sent
	}

	if answer := wallet.RenewCoinsSubmit("", privateKey, ""); !strings.HasPrefix(answer, "SUCCESS") || !strings.Contains(answer, "10 CVN that had been set aside") {
		t.Fatalf("renewing with no fee named answered: %s", answer)
	}
	if sent := mineIt(); sent.FreeWillOffering.Units() != 0 || sent.Outputs[0].Amount.Units() != cvn("50") {
		t.Errorf("renewing with no fee named sent a fee of %s and a new coin of %s; want none and 50", FormatUnits(sent.FreeWillOffering.Units()), FormatUnits(sent.Outputs[0].Amount.Units()))
	}
	if answer := wallet.RenewCoinsSubmit("0.25", privateKey, ""); !strings.HasPrefix(answer, "SUCCESS") {
		t.Fatalf("renewing with a fee of 0.25 answered: %s", answer)
	}
	if sent := mineIt(); sent.FreeWillOffering.Units() != cvn("0.25") || sent.Outputs[0].Amount.Units() != cvn("49.75") {
		t.Errorf("renewing with a fee of 0.25 sent a fee of %s and a new coin of %s; want 0.25 and 49.75", FormatUnits(sent.FreeWillOffering.Units()), FormatUnits(sent.Outputs[0].Amount.Units()))
	}

	// A second coin, so that there is something to combine.
	mineNext(t, timestamp, owner)
	if answer := wallet.CombineCoinsSubmit("", privateKey); !strings.HasPrefix(answer, "SUCCESS") {
		t.Fatalf("combining with no fee named answered: %s", answer)
	}
	if sent := mineIt(); sent.FreeWillOffering.Units() != 0 || len(sent.Inputs) != 2 || sent.Outputs[0].Amount.Units() != cvn("99.75") {
		t.Errorf("combining with no fee named sent a fee of %s, %d coins in and a new coin of %s; want none, 2 and 99.75", FormatUnits(sent.FreeWillOffering.Units()), len(sent.Inputs), FormatUnits(sent.Outputs[0].Amount.Units()))
	}
	mineNext(t, timestamp, owner)
	if answer := wallet.CombineCoinsSubmit("0.5", privateKey); !strings.HasPrefix(answer, "SUCCESS") {
		t.Fatalf("combining with a fee of 0.5 answered: %s", answer)
	}
	if sent := mineIt(); sent.FreeWillOffering.Units() != cvn("0.5") || sent.Outputs[0].Amount.Units() != cvn("149.25") {
		t.Errorf("combining with a fee of 0.5 sent a fee of %s and a new coin of %s; want 0.5 and 149.25", FormatUnits(sent.FreeWillOffering.Units()), FormatUnits(sent.Outputs[0].Amount.Units()))
	}
	if answer := wallet.RenewCoinsSubmit("a lot", privateKey, ""); !strings.HasPrefix(answer, "REJECTED") {
		t.Errorf("a fee that is not a number was answered: %s", answer)
	}
	nothingCreatedOrLost(t, "at the end")
}

// A doctored backup. The check adds up the coins, so it must not be possible
// to hide a change behind arithmetic: less than nothing set aside on a coin,
// or figures so large that the sum goes round the clock and comes back to
// the right total.
func TestABackupWithImpossibleFiguresSetAsideFailsItsCheck(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineTo(t, timestamp, 3, owner)
	mineTo(t, timestamp, 12, testAddress("idle too"))
	doctored := func(name string, change func(n int, fields map[string]json.RawMessage) bool) error {
		t.Helper()
		path, _, err := writeBackup(filepath.Join(t.TempDir(), name), 7)
		if err != nil {
			t.Fatalf("set-up: the backup failed: %v", err)
		}
		db, err := bbolt.Open(path, 0600, nil)
		if err != nil {
			t.Fatal(err)
		}
		changed := 0
		err = db.Update(func(dbTx *bbolt.Tx) error {
			bucket := dbTx.Bucket(utxoBucket)
			altered := map[string][]byte{}
			if err := bucket.ForEach(func(key, raw []byte) error {
				var fields map[string]json.RawMessage
				if json.Unmarshal(raw, &fields) != nil {
					return nil
				}
				if change(changed, fields) {
					changed++
					altered[string(key)], _ = json.Marshal(fields)
				}
				return nil
			}); err != nil {
				return err
			}
			for key, raw := range altered {
				if err := bucket.Put([]byte(key), raw); err != nil {
					return err
				}
			}
			return nil
		})
		if closeErr := db.Close(); err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		if changed == 0 {
			t.Fatal("set-up: no coin in the backup was changed")
		}
		_, checkErr := checkChainFile(path, buildGenesisBlock().Hash)
		return checkErr
	}
	number := func(raw json.RawMessage) int64 {
		var n int64
		if raw == nil {
			return 0 // a coin with nothing set aside does not carry the figure
		}
		if err := json.Unmarshal(raw, &n); err != nil {
			t.Fatalf("set-up: %s is not a number", raw)
		}
		return n
	}
	figure := func(n int64) json.RawMessage { return json.RawMessage(strconv.FormatInt(n, 10)) }

	// Written back as it was, it passes.
	if err := doctored("untouched", func(n int, fields map[string]json.RawMessage) bool { return n == 0 && fields["held"] != nil }); err != nil {
		t.Fatalf("set-up: a backup written back as it was fails its check: %v", err)
	}
	// One coin with less than nothing set aside, and its amount raised by as
	// much: the sum is still right.
	err = doctored("negative", func(n int, fields map[string]json.RawMessage) bool {
		if n > 0 || fields["held"] == nil {
			return false
		}
		held, amount := number(fields["held"]), number(fields["amount"])
		fields["held"], fields["amount"] = figure(-held), figure(amount+2*held)
		return true
	})
	if err == nil {
		t.Error("a backup with less than nothing set aside on a coin passed its check")
	}
	// Four coins with 2^62 units more set aside on each: 2^64 in all, which
	// a plain sum in 64 bits counts as nothing.
	err = doctored("round the clock", func(n int, fields map[string]json.RawMessage) bool {
		if n >= 4 || fields["held"] == nil {
			return false
		}
		fields["held"] = figure(number(fields["held"]) + (1 << 62))
		return true
	})
	if err == nil {
		t.Error("a backup with impossible amounts set aside, which add up to the right total only by overflowing, passed its check")
	}
	// And with figures that each could be real: 128 coins with 2^57 units
	// more set aside on each (about 1,441 million CVN, less than the whole
	// supply) are 2^64 in all again. Only a sum checked at every step
	// refuses that. The owner's three coins are first split into fifty
	// each, so that there are coins enough.
	var split []Transaction
	for _, coin := range ListSpendableCoins(owner) {
		var outputs []UTXOOutput
		for i := 0; i < TxMaxOutputs; i++ {
			outputs = append(outputs, UTXOOutput{Recipient: testAddress(fmt.Sprintf("part %d of %s", i, coin.TxID)), Amount: UnitsAmount(coin.Amount.Units() / TxMaxOutputs)})
		}
		tx, err := signTransfer(privateKey, publicKey, owner, []UTXOInput{walletInput(coin)}, outputs, coin.Amount.Units()%TxMaxOutputs)
		if err != nil {
			t.Fatal(err)
		}
		split = append(split, tx)
	}
	if len(split) != 3 {
		t.Fatalf("set-up: the owner holds %d coins, want 3", len(split))
	}
	mineNext(t, timestamp, testAddress("idle too"), split...)
	altered := 0
	err = doctored("each figure possible", func(n int, fields map[string]json.RawMessage) bool {
		if n >= 128 {
			return false
		}
		fields["held"] = figure(number(fields["held"]) + (1 << 57))
		altered++
		return true
	})
	if altered != 128 {
		t.Fatalf("set-up: %d coins were altered, want 128", altered)
	}
	if err == nil {
		t.Error("a backup in which 128 coins each have a possible but false amount set aside, adding up to the right total only by overflowing, passed its check")
	}
}

// With the newest block known, the text wallet warns when a date is less
// than two days off: a renewal with no fee might wait too long.
func TestTheWalletWarnsWhenADateIsNear(t *testing.T) {
	onNetwork(t, "devnet")
	swept := SpendableCoin{TxID: "TX_b", Amount: UnitsAmount(cvn("50")), HeldUnits: cvn("10"), SweepAt: 9000}
	near := strings.Join(describeCoinDates([]SpendableCoin{swept}, 9000-renewUrgentBlocks, true), "\n")
	if !strings.Contains(near, "less than two days") || !strings.Contains(near, "small fee") {
		t.Errorf("two days before a date the wallet said:\n%s", near)
	}
	far := strings.Join(describeCoinDates([]SpendableCoin{swept}, 9000-renewUrgentBlocks-1, true), "\n")
	if strings.Contains(far, "less than two days") || !strings.Contains(far, "with no fee") {
		t.Errorf("more than two days before a date the wallet said:\n%s", far)
	}
	unknown := strings.Join(describeCoinDates([]SpendableCoin{swept}, 0, false), "\n")
	if strings.Contains(unknown, "less than two days") {
		t.Errorf("with the newest block unknown the wallet guessed that a date was near:\n%s", unknown)
	}
	plain := SpendableCoin{TxID: "TX_a", Amount: UnitsAmount(cvn("50")), SweepAt: 5000}
	if lines := strings.Join(describeCoinDates([]SpendableCoin{plain, swept}, 5000-10, true), "\n"); !strings.Contains(lines, "less than two days") {
		t.Errorf("ten blocks before a coin's first date the wallet said:\n%s", lines)
	}
}
