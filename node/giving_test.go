package node

import (
	"bufio"
	"fmt"
	"io"
	"math/big"
	"math/rand"
	"net"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

// GIVING IN THE WALLET
//
// A sender may give something to a second address in the same transfer. It
// is the sender's choice each time, one more output of an ordinary transfer,
// and no rule of the chain (giving.go). These tests check what may be typed,
// that the transfer built pays exactly what was asked and loses no unit, that
// a gift is never made or missed by accident, and that with no gift named a
// transfer is exactly what it was.

// givingChain is a test chain on which `owner`, who has a key, holds one coin
// of 50 CVN for each of the first `coins` blocks. mine adds a block holding
// the transfers given.
func givingChain(t *testing.T, coins int) (privateKey, publicKey, owner string, mine func(transfers ...Transaction)) {
	t.Helper()
	startTestChain(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	owner = strings.TrimSpace(owner)
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < coins; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, owner, nil)
	}
	mine = func(transfers ...Transaction) {
		t.Helper()
		timestamp += 45
		extendTestChain(t, timestamp, testAddress("miner"), transfers)
	}
	return privateKey, publicKey, owner, mine
}

// everyUnitIsThere fails the test unless every coin and the vault together
// are exactly what has been issued.
func everyUnitIsThere(t *testing.T, when string) {
	t.Helper()
	height := GetLatestBlockInternal().Index
	if coins, vault, issued := coinCount(t), VaultBalanceUnits(), issuedUpTo(height); coins+vault != issued {
		t.Fatalf("%s: coins %s and vault %s make %s, but %s has been issued", when, FormatUnits(coins), FormatUnits(vault), FormatUnits(coins+vault), FormatUnits(issued))
	}
}

func TestWhatASenderMayTypeAsAGift(t *testing.T) {
	onNetwork(t, "devnet")
	for _, c := range []struct {
		typed  string
		amount string // the amount being sent, in CVN
		want   int64  // the gift in units; 0 means it must be refused
	}{
		// An amount in CVN.
		{"0.5", "20", 50000000},
		{" 1 ", "20", 100000000},
		{"0.0001", "20", 10000}, // the smallest coin
		{"0.00009999", "20", 0}, // one unit less
		{"0", "20", 0},
		{"-1", "20", 0},
		{"1.000000001", "20", 0}, // nine decimal places
		{"a tenth", "20", 0},
		{"", "20", 0},
		{"   ", "20", 0},
		// A share of the amount being sent.
		{"10%", "20", 200000000},
		{"10 %", "20", 200000000},
		{"2.5%", "20", 50000000},
		{"0.01%", "20", 200000},
		{"100%", "20", 2000000000},
		{"33.33%", "0.00030001", 0},     // 9999 units and a bit: under the smallest coin
		{"33.34%", "0.00030001", 10002}, // rounded down from 10002.33
		{"50%", "0.0002", 10000},
		{"50%", "0.00019999", 0}, // 9999 units
		{"0%", "20", 0},
		{"0.001%", "20", 0},  // three decimal places
		{"10.005%", "20", 0}, // three decimal places, and not to be read as 10%
		{"2.505%", "20", 0},
		{"10.000%", "20", 200000000}, // noughts after the second place change nothing
		{"100.01%", "20", 0},
		{"101%", "20", 0},
		{"-5%", "20", 0},
		{"%", "20", 0},
		{"ten%", "20", 0},
		{"10%%", "20", 0},
		{"1e1%", "20", 0},
	} {
		amountUnits, err := ParseCVN(c.amount)
		if err != nil {
			t.Fatalf("set-up: %q is not an amount", c.amount)
		}
		got, err := ParseGift(c.typed, amountUnits)
		if c.want == 0 {
			if err == nil {
				t.Errorf("%q, sending %s CVN, was taken as a gift of %d units; want it refused", c.typed, c.amount, got)
			}
			if got != 0 {
				t.Errorf("%q was refused and still gave %d units", c.typed, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q, sending %s CVN: a gift of %d units (%v); want %d", c.typed, c.amount, got, err, c.want)
		}
	}
	// A share of nothing, or of more than can exist, is refused.
	for _, amount := range []int64{0, -1, MaxAmountUnits + 1} {
		if got, err := ParseGift("10%", amount); err == nil {
			t.Errorf("10%% of %d units was taken as a gift of %d", amount, got)
		}
	}
}

// A share is worked out without ever multiplying two large numbers. This
// compares it, for amounts of every size up to all the coins there can be
// and for every kind of share, with the same sum done in numbers that have
// no limit.
func TestAShareIsRoundedDownExactlyAtEverySize(t *testing.T) {
	onNetwork(t, "devnet")
	random := rand.New(rand.NewSource(20261126))
	amounts := []int64{1, 9999, 10000, 10001, 123456789, MaxAmountUnits - 1, MaxAmountUnits}
	for i := 0; i < 3000; i++ {
		amounts = append(amounts, 1+random.Int63n(MaxAmountUnits))
		amounts = append(amounts, 1+random.Int63n(1+int64(1)<<uint(random.Intn(58))))
	}
	checked := 0
	for _, amount := range amounts {
		steps := int64(1 + random.Intn(10000)) // 0.01% to 100%
		typed := fmt.Sprintf("%d.%02d%%", steps/100, steps%100)
		want := new(big.Int).Mul(big.NewInt(amount), big.NewInt(steps))
		want.Div(want, big.NewInt(10000))
		got, err := ParseGift(typed, amount)
		if want.Int64() < minimumOutputUnits() {
			if err == nil {
				t.Fatalf("%s of %d units is %s units, under the smallest coin, and was taken as %d", typed, amount, want, got)
			}
			continue
		}
		if err != nil || got != want.Int64() {
			t.Fatalf("%s of %d units: %d (%v); want %s", typed, amount, got, err, want)
		}
		if got > amount {
			t.Fatalf("%s of %d units is %d, more than the amount", typed, amount, got)
		}
		checked++
	}
	if checked < 3000 {
		t.Fatalf("only %d shares were large enough to check", checked)
	}
}

func TestAGiftIsOneMoreOutputOfAnOrdinaryTransfer(t *testing.T) {
	privateKey, publicKey, owner, mine := givingChain(t, 1)
	shop, church := testAddress("shop"), testAddress("church")
	tx, err := BuildSignedTransferWithGift(privateKey, publicKey, shop, cvn("10"), church, cvn("1"), cvn("0.01"), ListSpendableCoins(owner))
	if err != nil {
		t.Fatalf("the transfer with a gift could not be built: %v", err)
	}
	if len(tx.Inputs) != 1 || tx.Inputs[0].AmountUnits != cvn("50") || tx.FreeWillOffering.Units() != cvn("0.01") {
		t.Fatalf("the transfer spends %+v with a fee of %s; want the one coin of 50 and a fee of 0.01", tx.Inputs, FormatUnits(tx.FreeWillOffering.Units()))
	}
	want := []UTXOOutput{
		{Recipient: shop, Amount: UnitsAmount(cvn("10"))},
		{Recipient: church, Amount: UnitsAmount(cvn("1"))},
		{Recipient: owner, Amount: UnitsAmount(cvn("38.99"))},
	}
	if len(tx.Outputs) != len(want) {
		t.Fatalf("the transfer has %d outputs, want %d: %+v", len(tx.Outputs), len(want), tx.Outputs)
	}
	for i, output := range tx.Outputs {
		if strings.TrimSpace(output.Recipient) != want[i].Recipient || output.Amount.Units() != want[i].Amount.Units() {
			t.Errorf("output %d pays %s CVN to %s; want %s to %s", i+1, FormatUnits(output.Amount.Units()), output.Recipient, FormatUnits(want[i].Amount.Units()), want[i].Recipient)
		}
	}
	// The rules take it like any other transfer: a node keeps it for the
	// next block, and a block holding it is valid.
	if err := AdmitTransactionToMempool(tx); err != nil {
		t.Fatalf("a node refused the transfer with a gift: %v", err)
	}
	mine(tx)
	if got := balanceOf(shop); got != cvn("10") {
		t.Errorf("the recipient has %s CVN, want all 10: the gift is in addition to the amount", FormatUnits(got))
	}
	if got := balanceOf(church); got != cvn("1") {
		t.Errorf("the address given to has %s CVN, want 1", FormatUnits(got))
	}
	if got := balanceOf(owner); got != cvn("38.99") {
		t.Errorf("the sender has %s CVN left, want 38.99", FormatUnits(got))
	}
	everyUnitIsThere(t, "after the transfer with a gift")
}

// verdictOnGift is what a node says of a transfer when it is handed in: nil
// if it would be kept for a block. The transfer is taken out again, so that
// the next one can be judged against the same coins.
func verdictOnGift(tx Transaction) error {
	err := AdmitTransactionToMempool(tx)
	MempoolMatrix.Lock()
	delete(MempoolMatrix.Transactions, tx.ID)
	delete(mempoolArrival, tx.ID)
	delete(mempoolEntries, tx.ID)
	MempoolMatrix.Unlock()
	return err
}

// The gift changes what is paid and nothing else: the same coins are spent,
// in the same order, as for a transfer of the amount and the gift together
// with no gift named.
func TestAGiftSpendsTheSameCoinsAsTheSameSumWithoutOne(t *testing.T) {
	privateKey, publicKey, owner, _ := givingChain(t, 4)
	coins := ListSpendableCoins(owner)
	if len(coins) != 4 {
		t.Fatalf("set-up: the owner holds %d coins, want 4", len(coins))
	}
	for _, c := range []struct{ amount, gift, fee string }{
		{"10", "1", "0"},
		{"49", "1", "0"},      // exactly one coin
		{"49", "1", "0.001"},  // one coin and a little: a second coin
		{"120", "25", "0.5"},  // three coins
		{"150", "49.99", "0"}, // all four, change of 0.01
		{"150", "50", "0"},    // all four, no change
	} {
		with, err := BuildSignedTransferWithGift(privateKey, publicKey, testAddress("shop"), cvn(c.amount), testAddress("church"), cvn(c.gift), cvn(c.fee), coins)
		if err != nil {
			t.Fatalf("%s and a gift of %s: %v", c.amount, c.gift, err)
		}
		without, err := BuildSignedTransfer(privateKey, publicKey, testAddress("shop"), cvn(c.amount)+cvn(c.gift), cvn(c.fee), coins)
		if err != nil {
			t.Fatalf("%s with no gift: %v", c.amount, err)
		}
		if fmt.Sprint(with.Inputs) != fmt.Sprint(without.Inputs) || with.FreeWillOffering.Units() != without.FreeWillOffering.Units() {
			t.Errorf("%s and a gift of %s spends %+v at a fee of %d; the same sum with no gift spends %+v at %d", c.amount, c.gift, with.Inputs, with.FreeWillOffering.Units(), without.Inputs, without.FreeWillOffering.Units())
		}
		var in, out int64
		for _, input := range with.Inputs {
			in += input.AmountUnits
		}
		for _, output := range with.Outputs {
			out += output.Amount.Units()
		}
		if in != out+with.FreeWillOffering.Units() || with.Outputs[0].Amount.Units() != cvn(c.amount) || with.Outputs[1].Amount.Units() != cvn(c.gift) {
			t.Errorf("%s and a gift of %s: %d units in, %d out and a fee of %d; outputs %+v", c.amount, c.gift, in, out, with.FreeWillOffering.Units(), with.Outputs)
		}
		if err := verdictOnGift(with); err != nil {
			t.Errorf("%s and a gift of %s: the rules refuse the transfer: %v", c.amount, c.gift, err)
		}
	}
}

// Change too small to be a coin goes to the miner with the fee, with a gift
// as without one, so that the wallet never builds a transfer a node refuses.
func TestChangeTooSmallToBeACoinGoesToTheFeeWithAGiftToo(t *testing.T) {
	privateKey, publicKey, owner, mine := givingChain(t, 1)
	// 50 CVN: 49.9998 sent, 0.00015 given, leaving 0.00005, which is no coin.
	tx, err := BuildSignedTransferWithGift(privateKey, publicKey, testAddress("shop"), cvn("49.9998"), testAddress("church"), cvn("0.00015"), 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Outputs) != 2 || tx.FreeWillOffering.Units() != cvn("0.00005") {
		t.Fatalf("the transfer has %d outputs and a fee of %d units; want two outputs and the 5000 units left over as fee", len(tx.Outputs), tx.FreeWillOffering.Units())
	}
	mine(tx)
	if balanceOf(testAddress("shop")) != cvn("49.9998") || balanceOf(testAddress("church")) != cvn("0.00015") || balanceOf(owner) != 0 {
		t.Errorf("after it was mined: recipient %s, given %s, sender %s", FormatUnits(balanceOf(testAddress("shop"))), FormatUnits(balanceOf(testAddress("church"))), FormatUnits(balanceOf(owner)))
	}
	everyUnitIsThere(t, "after the transfer")
}

func TestAGiftThatCannotBeMadeIsRefused(t *testing.T) {
	privateKey, publicKey, owner, _ := givingChain(t, 1)
	shop, church := testAddress("shop"), testAddress("church")
	coins := ListSpendableCoins(owner)
	for _, c := range []struct {
		what              string
		giftTo            string
		amount, gift, fee int64
		wantInTheReason   string
	}{
		{"a gift to the address the transfer goes to", shop, cvn("10"), cvn("1"), 0, "same address"},
		{"a gift to the address it comes from", owner, cvn("10"), cvn("1"), 0, "sent from"},
		{"a gift to something that is not an address", "the church down the road", cvn("10"), cvn("1"), 0, "not a CVN wallet address"},
		{"a gift to no address", "", cvn("10"), cvn("1"), 0, "not a CVN wallet address"},
		{"a gift under the smallest coin", church, cvn("10"), 9999, 0, "smallest amount"},
		{"a gift of nothing", church, cvn("10"), 0, 0, "smallest amount"},
		{"a gift of less than nothing", church, cvn("10"), -cvn("1"), 0, "smallest amount"},
		{"an amount, gift and fee of more than the wallet holds", church, cvn("45"), cvn("5"), 1, "not enough coins"},
		{"a gift larger than everything that can exist", church, cvn("10"), MaxAmountUnits, 0, "more than can exist"},
		{"a fee of less than nothing", church, cvn("10"), cvn("1"), -1, "negative"},
	} {
		tx, err := BuildSignedTransferWithGift(privateKey, publicKey, shop, c.amount, c.giftTo, c.gift, c.fee, coins)
		if err == nil || !strings.Contains(err.Error(), c.wantInTheReason) {
			t.Errorf("%s: %v; want it refused, the reason speaking of %q", c.what, err, c.wantInTheReason)
		}
		if tx.ID != "" || len(tx.Outputs) != 0 {
			t.Errorf("%s: refused, and a transfer was built all the same: %+v", c.what, tx)
		}
	}
	// Exactly what the wallet holds is taken.
	if _, err := BuildSignedTransferWithGift(privateKey, publicKey, shop, cvn("45"), church, cvn("5"), 0, coins); err != nil {
		t.Errorf("an amount and a gift of exactly what the wallet holds: %v", err)
	}
}

// What the two gift fields of a wallet come to. Both empty: no gift. One
// filled and the other empty: an error, never a guess.
func TestBothGiftFieldsOrNeither(t *testing.T) {
	onNetwork(t, "devnet")
	church := testAddress("church")
	shop := testAddress("shop")
	if to, units, err := giftFromWhatWasTyped("", "  ", shop, cvn("10")); err != nil || to != "" || units != 0 {
		t.Errorf("with both fields empty: %q, %d units, %v; want no gift and no error", to, units, err)
	}
	if to, units, err := giftFromWhatWasTyped(" "+church+" ", " 10% ", shop, cvn("10")); err != nil || to != church || units != cvn("1") {
		t.Errorf("with both fields filled: %q, %d units, %v; want 1 CVN to the address", to, units, err)
	}
	for _, c := range []struct{ to, amount, wantInTheReason string }{
		{church, "", "no amount"},
		{"", "1", "no address"},
		{"", "10%", "no address"},
		{"somebody", "1", "not a CVN wallet address"},
		{church, "lots", "must be a number"},
		{church, "0.00001", "smallest amount"},
		{shop, "1", "same address"},
		{" " + shop + " ", "10%", "same address"},
	} {
		to, units, err := giftFromWhatWasTyped(c.to, c.amount, shop, cvn("10"))
		if err == nil || !strings.Contains(err.Error(), c.wantInTheReason) || to != "" || units != 0 {
			t.Errorf("address %q and amount %q: %q, %d units, %v; want an error speaking of %q and no gift", c.to, c.amount, to, units, err, c.wantInTheReason)
		}
	}
}

// serveWalletFromThisChain answers a wallet's requests from this test's
// chain, as a node on the same machine would.
func serveWalletFromThisChain(t *testing.T) {
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

// transfersWaiting is every transfer a node holds for the next blocks.
func transfersWaiting() []Transaction {
	MempoolMatrix.Lock()
	defer MempoolMatrix.Unlock()
	var waiting []Transaction
	for _, tx := range MempoolMatrix.Transactions {
		waiting = append(waiting, tx)
	}
	return waiting
}

// The window wallet, against a node. With both gift fields empty it sends
// the ordinary transfer it always sent. With one of them filled it sends
// nothing. With both it sends one transfer that pays the recipient in full
// and the gift besides.
func TestTheWindowWalletGivesOnlyWhenBothFieldsAreFilled(t *testing.T) {
	t.Chdir(t.TempDir()) // no saved key of this machine is looked at
	privateKey, _, owner, mine := givingChain(t, 2)
	serveWalletFromThisChain(t)
	wallet := &App{}
	shop, church := testAddress("shop"), testAddress("church")

	for _, c := range []struct{ giftTo, gift, wantInTheAnswer string }{
		{church, "", "no amount"},
		{"", "1", "no address"},
		{"", "10%", "no address"},
		{"not an address", "1", "not a CVN wallet address"},
		{church, "0%", "share to give"},
		{shop, "1", "same address"},
		{owner, "1", "sent from"},
	} {
		answer := wallet.SendWithGiftSubmit(shop, "10", "0", c.giftTo, c.gift, privateKey)
		if !strings.HasPrefix(answer, "REJECTED") || !strings.Contains(answer, c.wantInTheAnswer) {
			t.Errorf("giving %q to %q was answered: %s", c.gift, c.giftTo, answer)
		}
		if waiting := transfersWaiting(); len(waiting) != 0 {
			t.Fatalf("giving %q to %q was refused, and %d transfer(s) were sent all the same", c.gift, c.giftTo, len(waiting))
		}
	}

	// Both fields empty: the ordinary transfer, with no third party in it.
	if answer := wallet.SendWithGiftSubmit(shop, "10", "0", " ", "", privateKey); !strings.HasPrefix(answer, "SUCCESS") || strings.Contains(answer, "given") {
		t.Fatalf("with no gift named the wallet answered: %s", answer)
	}
	waiting := transfersWaiting()
	if len(waiting) != 1 || len(waiting[0].Outputs) != 2 || strings.TrimSpace(waiting[0].Outputs[0].Recipient) != shop || waiting[0].Outputs[0].Amount.Units() != cvn("10") || strings.TrimSpace(waiting[0].Outputs[1].Recipient) != owner {
		t.Fatalf("with no gift named the wallet sent %+v; want one transfer paying the recipient 10 and the change back", waiting)
	}
	mine(waiting[0])

	// Both filled: a tenth of the amount to the address named.
	answer := wallet.SendWithGiftSubmit(shop, "10", "0.01", church, "10%", privateKey)
	if !strings.HasPrefix(answer, "SUCCESS") || !strings.Contains(answer, "10 CVN to the recipient and 1 CVN given to "+church) {
		t.Fatalf("with a gift named the wallet answered: %s", answer)
	}
	waiting = transfersWaiting()
	if len(waiting) != 1 || len(waiting[0].Outputs) != 3 {
		t.Fatalf("with a gift named the wallet sent %+v; want one transfer with three outputs", waiting)
	}
	sent := waiting[0]
	if strings.TrimSpace(sent.Outputs[0].Recipient) != shop || sent.Outputs[0].Amount.Units() != cvn("10") ||
		strings.TrimSpace(sent.Outputs[1].Recipient) != church || sent.Outputs[1].Amount.Units() != cvn("1") ||
		strings.TrimSpace(sent.Outputs[2].Recipient) != owner || sent.FreeWillOffering.Units() != cvn("0.01") {
		t.Fatalf("the transfer sent is %+v; want 10 to the recipient, 1 given, the change back and a fee of 0.01", sent)
	}
	mine(sent)
	if balanceOf(shop) != cvn("20") || balanceOf(church) != cvn("1") || balanceOf(owner) != cvn("100")-cvn("20")-cvn("1")-cvn("0.01") {
		t.Errorf("after both transfers: recipient %s, given %s, sender %s; want 20, 1 and 78.99", FormatUnits(balanceOf(shop)), FormatUnits(balanceOf(church)), FormatUnits(balanceOf(owner)))
	}
	everyUnitIsThere(t, "after both transfers")
}

// FuzzWhatIsTypedAsAGift gives ParseGift anything at all. It must never
// panic; a gift it accepts is at least the smallest coin and no more than
// can exist; a share is never more than the amount it is a share of; and
// what it refuses gives nothing.
func FuzzWhatIsTypedAsAGift(f *testing.F) {
	saved := Params
	if err := SelectNetwork("devnet"); err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { Params = saved; applyParams() })
	for _, seed := range []string{"0.5", "10%", "2.5%", "100%", "0.01%", "%", "", " ", "1e9", "-1%", "99999999999999999999%", "0.00009999", "21000000000", "10%%", "\x00%", "１０%", "1_0%", "+5%", "0x10", "NaN", "Inf%"} {
		f.Add(seed, int64(2000000000))
	}
	f.Add("50%", MaxAmountUnits)
	f.Add("100%", int64(-1))
	f.Fuzz(func(t *testing.T, typed string, amountUnits int64) {
		gift, err := ParseGift(typed, amountUnits)
		if err != nil {
			if gift != 0 {
				t.Fatalf("%q was refused and still gave %d units", typed, gift)
			}
			return
		}
		if gift < minimumOutputUnits() || gift > MaxAmountUnits {
			t.Fatalf("%q, sending %d units, was taken as a gift of %d units", typed, amountUnits, gift)
		}
		if strings.HasSuffix(strings.TrimSpace(typed), "%") && (amountUnits <= 0 || gift > amountUnits) {
			t.Fatalf("%q of %d units was taken as %d units, more than the amount", typed, amountUnits, gift)
		}
	})
}

// What the window wallet shows before a transfer with a gift is sent: the
// gift as a figure, a share worked out, and all that leaves the wallet. It
// comes from the program, not from the screen, and needs no key.
func TestTheWindowWalletShowsTheGiftInFiguresBeforeSending(t *testing.T) {
	onNetwork(t, "devnet")
	wallet := &App{}
	shop, church := testAddress("shop"), testAddress("church")
	shown := wallet.GiftPreview(shop, "10", "0.01", " "+church+" ", "10%")
	for _, want := range []string{"GIFT: 1 CVN will be given to " + church, "in addition to the amount", "In all 11.01 CVN leaves this wallet", "10 CVN to the recipient", "1 CVN given", "a fee of 0.01 CVN"} {
		if !strings.Contains(shown, want) {
			t.Errorf("before sending, the wallet does not say %q: %s", want, shown)
		}
	}
	if shown := wallet.GiftPreview(shop, "20", "", church, "2.5"); !strings.HasPrefix(shown, "GIFT: 2.5 CVN will be given") || !strings.Contains(shown, "In all 22.5 CVN") {
		t.Errorf("for a gift of 2.5 CVN with 20 sent and no fee the wallet says: %s", shown)
	}
	for _, c := range []struct{ amount, fee, giftTo, gift, wantInTheAnswer string }{
		{"10", "0", church, "", "no amount"},
		{"10", "0", "", "1", "no address"},
		{"10", "0", "", "", "No gift was named"},
		{"10", "0", shop, "1", "same address"},
		{"10", "0", church, "200%", "share to give"},
		{"ten", "0", church, "1", "Enter an amount"},
		{"10", "free", church, "1", "The fee must be"},
		{"10", "0", "somewhere", "1", "not a CVN wallet address"},
	} {
		if shown := wallet.GiftPreview(shop, c.amount, c.fee, c.giftTo, c.gift); !strings.HasPrefix(shown, "REJECTED: ") || !strings.Contains(shown, c.wantInTheAnswer) {
			t.Errorf("amount %q, fee %q, giving %q to %q: %s; want it refused, speaking of %q", c.amount, c.fee, c.gift, c.giftTo, shown, c.wantInTheAnswer)
		}
	}
}

// What is typed where the gift's address goes is never shown back when it is
// not an address. Somebody who pastes their private key there by mistake must
// not see it on the screen, and a refusal must not be taken for a success
// because of a word that was typed.
func TestWhatIsTypedAsAGiftsAddressIsNeverShownBack(t *testing.T) {
	t.Chdir(t.TempDir()) // no saved key of this machine is looked at
	privateKey, _, _, _ := givingChain(t, 1)
	serveWalletFromThisChain(t)
	wallet := &App{}
	shop := testAddress("shop")
	for _, typed := range []string{privateKey, "SUCCESS", "SUCCESS: sent", "a secret nobody should see"} {
		for _, answer := range []string{
			wallet.SendWithGiftSubmit(shop, "10", "0", typed, "1", privateKey),
			wallet.GiftPreview(shop, "10", "0", typed, "1"),
		} {
			if !strings.HasPrefix(answer, "REJECTED: ") || strings.Contains(answer, typed) {
				t.Errorf("with %q typed as the address to give to, the wallet answered: %s", typed, answer)
			}
		}
	}
	if waiting := transfersWaiting(); len(waiting) != 0 {
		t.Fatalf("%d transfer(s) were sent", len(waiting))
	}

	// The text wallet: the same text at its question about a gift. Nothing
	// is sent, and what it prints does not hold what was typed.
	said := printedBy(t, func() {
		ask, _ := scriptedAnswers(t, shop, "10", "0", privateKey, "1", "y")
		textWalletSend(ask, bufio.NewReader(strings.NewReader(privateKey+"\n")))
	})
	if strings.Contains(said, privateKey) || !strings.Contains(said, "Nothing was sent") {
		t.Errorf("with the private key typed as the address to give to, the text wallet printed: %s", said)
	}
	if waiting := transfersWaiting(); len(waiting) != 0 {
		t.Fatalf("the text wallet sent %d transfer(s)", len(waiting))
	}
}

// scriptedAnswers stands in for a person at the text wallet: each question
// gets the next answer. It returns the questions that were asked, in order.
func scriptedAnswers(t *testing.T, answers ...string) (ask func(string) string, asked *[]string) {
	t.Helper()
	asked = &[]string{}
	return func(question string) string {
		*asked = append(*asked, question)
		if len(*asked) > len(answers) {
			t.Fatalf("the wallet asked a question nobody expected: %q", question)
		}
		return strings.TrimSpace(answers[len(*asked)-1])
	}, asked
}

// printedBy runs work and returns what the program printed meanwhile.
func printedBy(t *testing.T, work func()) string {
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

// keyReader is the line the text wallet reads unseen, and notes when it was
// read.
type keyReader struct {
	line string
	read bool
}

func (k *keyReader) Read(into []byte) (int, error) {
	if k.read {
		return 0, io.EOF
	}
	k.read = true
	return copy(into, k.line), nil
}

// The text wallet, against a node. The questions come in the order they
// always did (recipient, amount, fee, and then the key, typed unseen), and
// only after the key is the gift asked about: somebody answering from habit
// must never type their key at a question whose answer is shown. With an
// address and a share named, one transfer goes out that pays the recipient
// in full and the gift besides.
func TestTheTextWalletAsksAboutAGiftOnlyAfterTheKey(t *testing.T) {
	t.Chdir(t.TempDir()) // no saved key of this machine is looked at
	privateKey, _, owner, mine := givingChain(t, 1)
	serveWalletFromThisChain(t)
	shop, church := testAddress("shop"), testAddress("church")
	key := &keyReader{line: privateKey + "\n"}
	keyWasReadBeforeTheGiftWasAsked := false
	script, asked := scriptedAnswers(t, shop, "10", "0.01", church, "10%", "y")
	ask := func(question string) string {
		if strings.Contains(question, "Give something") {
			keyWasReadBeforeTheGiftWasAsked = key.read
		}
		return script(question)
	}
	textWalletSend(ask, bufio.NewReader(key))

	if len(*asked) != 6 || !strings.Contains((*asked)[2], "fee") || !strings.Contains((*asked)[3], "Give something") || !strings.Contains((*asked)[4], "How much to give") || !strings.Contains((*asked)[5], "Send this transfer") {
		t.Fatalf("the wallet asked, in order: %q", *asked)
	}
	if !keyWasReadBeforeTheGiftWasAsked {
		t.Fatal("the wallet asked about a gift before it had read the key: the fourth thing typed has always been the key, and this question shows what is typed")
	}
	waiting := transfersWaiting()
	if len(waiting) != 1 || len(waiting[0].Outputs) != 3 {
		t.Fatalf("the wallet sent %+v; want one transfer with three outputs", waiting)
	}
	sent := waiting[0]
	if strings.TrimSpace(sent.Outputs[0].Recipient) != shop || sent.Outputs[0].Amount.Units() != cvn("10") ||
		strings.TrimSpace(sent.Outputs[1].Recipient) != church || sent.Outputs[1].Amount.Units() != cvn("1") ||
		strings.TrimSpace(sent.Outputs[2].Recipient) != owner || sent.FreeWillOffering.Units() != cvn("0.01") {
		t.Fatalf("the transfer sent is %+v; want 10 to the recipient, 1 given, the change back and a fee of 0.01", sent)
	}
	mine(sent)
	everyUnitIsThere(t, "after the transfer")

	// With Enter at the question about a gift, the transfer is the ordinary
	// one, and no further question about giving is asked.
	key = &keyReader{line: privateKey + "\n"}
	script, asked = scriptedAnswers(t, shop, "5", "", "", "y")
	textWalletSend(script, bufio.NewReader(key))
	if len(*asked) != 5 {
		t.Fatalf("with no gift the wallet asked: %q", *asked)
	}
	if waiting = transfersWaiting(); len(waiting) != 1 || len(waiting[0].Outputs) != 2 || waiting[0].Outputs[0].Amount.Units() != cvn("5") {
		t.Fatalf("with no gift the wallet sent %+v; want the ordinary transfer of 5", waiting)
	}

	// The sender's own address as the one to give to: refused at once,
	// before "how much" is asked, and nothing is sent.
	key = &keyReader{line: privateKey + "\n"}
	script, asked = scriptedAnswers(t, shop, "5", "", owner)
	textWalletSend(script, bufio.NewReader(key))
	if len(*asked) != 4 {
		t.Fatalf("with the sender's own address to give to, the wallet asked: %q", *asked)
	}

	// An address and then Enter at "how much": nothing is sent.
	MempoolMatrix.Lock()
	MempoolMatrix.Transactions = make(map[string]Transaction)
	MempoolMatrix.Unlock()
	key = &keyReader{line: privateKey + "\n"}
	script, _ = scriptedAnswers(t, shop, "5", "", church, "")
	textWalletSend(script, bufio.NewReader(key))
	if waiting = transfersWaiting(); len(waiting) != 0 {
		t.Fatalf("with an address to give to and no amount, the wallet sent %d transfer(s)", len(waiting))
	}
}
