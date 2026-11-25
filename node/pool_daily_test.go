package node

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// A POOL PAYS WHAT IT OWES WITHIN A DAY
//
// Every run of payments pays each address owed the minimum (1 CVN) or more.
// An address owed less used to wait until it was owed that much, which for a
// small miner could be many days. Now an address that has not been paid in
// the last twelve hours is paid whatever it is owed, down to the smallest
// payment worth making. So nobody is sent a crumb every few hours, and a
// run reaches every such address within a day of its last payment.
//
// A node looks back half an hour less than twelve hours (poolPayRunSlack),
// because runs do not begin to the second: an address paid by one run must
// not be passed over by the run that comes twelve hours later.
//
// The tests in this file go through the pool as a node runs it, on a test
// chain with a real key. How one run chooses whom to pay is tested figure by
// figure in pool_daily_plan_test.go.

// withPoolMinimum sets the amount from which every run pays, for one test.
func withPoolMinimum(t *testing.T, units int64) {
	t.Helper()
	saved := poolMinimumPaymentUnits
	poolMinimumPaymentUnits = units
	t.Cleanup(func() { poolMinimumPaymentUnits = saved })
}

// sendAndSettle signs the payments a run prepared, hands them to the node,
// and mines until every one of them is in a block that counts.
func (p *payingPool) sendAndSettle(t *testing.T, payments []poolPayment, now time.Time) {
	t.Helper()
	for _, payment := range payments {
		if _, err := poolAcceptSigned(payment.Serial, p.sign(t, payment), now); err != nil {
			t.Fatalf("payment %d, signed, was refused: %v", payment.Serial, err)
		}
	}
	for i := 0; i < 8; i++ {
		p.mine(t, 1)
		poolSettlePayments(now)
	}
	// The blocks just mined come to count, and what they earned is owed.
	poolSettle()
	for _, payment := range payments {
		if kept, _ := poolLedger.payment(payment.Serial); kept.State != poolPaymentSettled {
			t.Fatalf("eight blocks on, payment %d is %s", payment.Serial, kept.State)
		}
	}
}

// linesOf is whom a run pays, and how much each is debited.
func linesOf(payments []poolPayment) map[string]int64 {
	debits := map[string]int64{}
	for _, payment := range payments {
		for _, line := range payment.Lines {
			debits[line.Address] += line.Debit
		}
	}
	return debits
}

func TestAnAddressOwedLessThanTheMinimumIsPaidWithinADay(t *testing.T) {
	pool := startPayingPool(t) // Alice is owed 37.5 CVN and Bob 12.5
	// The minimum is put above both, so that neither is paid by the
	// ordinary rule: they stand for miners owed less than 1 CVN.
	withPoolMinimum(t, cvn("40"))
	now := time.Now()

	// Neither has ever been paid, so both are paid what they are owed.
	payments, reason, err := poolPrepare(now)
	if err != nil || len(payments) != 1 || len(payments[0].Lines) != 2 {
		t.Fatalf("with two addresses owed less than the minimum and never paid, a run prepared %d payment(s) (%q): %v; want one paying both", len(payments), reason, err)
	}
	if alice, bob := payments[0].Lines[0], payments[0].Lines[1]; alice.Address != pool.alice || alice.Debit != cvn("37.5") || alice.Receives != cvn("37.4995") ||
		bob.Address != pool.bob || bob.Debit != cvn("12.5") || bob.Receives != cvn("12.4995") {
		t.Fatalf("the lines are %+v; want Alice 37.5 and Bob 12.5, each less half the fee", payments[0].Lines)
	}
	pool.sendAndSettle(t, payments, now)

	// The blocks mined meanwhile have earned them more. With the minimum
	// out of reach, they wait: they were paid a moment ago.
	withPoolMinimum(t, cvn("1000000"))
	alice, _ := poolLedger.account(pool.alice, 16)
	bob, _ := poolLedger.account(pool.bob, 16)
	if bob.Owed < cvn("1") || alice.Paid != cvn("37.5") || bob.Paid != cvn("12.5") {
		t.Fatalf("set-up: after the payment Alice's account is %+v and Bob's %+v", alice, bob)
	}
	if again, why, err := poolPrepare(now.Add(time.Hour)); err != nil || len(again) != 0 || !strings.Contains(why, "paid too lately") {
		t.Fatalf("an hour after they were paid, a run prepared %d payment(s) (%q): %v; want none, and the reason that they were paid too lately", len(again), why, err)
	}
	if again, _, err := poolPrepare(now.Add(11*time.Hour + 30*time.Minute - time.Second)); err != nil || len(again) != 0 {
		t.Fatalf("a second short of eleven and a half hours after they were paid, a run prepared %d payment(s): %v; want none", len(again), err)
	}

	// A run that begins a few minutes short of twelve hours after the
	// payment was stamped, as the run twelve hours after the last one may:
	// paid again, whatever they are owed.
	owedNow := map[string]int64{}
	for _, address := range []string{pool.alice, pool.bob} {
		if account, _ := poolLedger.account(address, 16); account.Owed > 0 {
			owedNow[address] = account.Owed
		}
	}
	due := now.Add(11*time.Hour + 30*time.Minute)
	payments, reason, err = poolPrepare(due)
	if err != nil || len(payments) == 0 {
		t.Fatalf("eleven and a half hours after they were paid, a run prepared nothing (%q): %v; the run that comes twelve hours after the last must not pass them over for starting a little early", reason, err)
	}
	paid := linesOf(payments)
	if len(paid) != len(owedNow) {
		t.Fatalf("twelve hours on, the run pays %d address(es); %d are owed something", len(paid), len(owedNow))
	}
	for address, owed := range owedNow {
		if paid[address] != owed {
			t.Errorf("twelve hours on, %s is debited %s; it is owed %s", address, FormatUnits(paid[address]), FormatUnits(owed))
		}
	}
}

// An address owed the minimum or more is paid on every run, however lately
// it was paid: the rule for small amounts takes nothing from the old one.
func TestAnAddressOwedTheMinimumIsPaidOnEveryRun(t *testing.T) {
	pool := startPayingPool(t)
	withPoolMinimum(t, cvn("40"))
	now := time.Now()
	payments, _, err := poolPrepare(now)
	if err != nil || len(payments) != 1 {
		t.Fatalf("the first run prepared %d payment(s): %v", len(payments), err)
	}
	pool.sendAndSettle(t, payments, now)

	// Bob found the blocks since and is owed far more than 1 CVN.
	withPoolMinimum(t, cvn("1"))
	bob, _ := poolLedger.account(pool.bob, 16)
	if bob.Owed < cvn("1") {
		t.Fatalf("set-up: Bob is owed %s", FormatUnits(bob.Owed))
	}
	next, reason, err := poolPrepare(now.Add(time.Minute))
	if err != nil || linesOf(next)[pool.bob] != bob.Owed {
		t.Fatalf("a minute after he was paid, Bob, owed %s, is debited %s (%q): %v", FormatUnits(bob.Owed), FormatUnits(linesOf(next)[pool.bob]), reason, err)
	}
}

// A payment that was prepared and never signed is undone, and is no payment:
// the addresses in it have not been paid, and the next run pays them.
func TestAPaymentThatWasUndoneDoesNotCountAsPaid(t *testing.T) {
	pool := startPayingPool(t)
	withPoolMinimum(t, cvn("40"))
	now := time.Now()
	first, _, err := poolPrepare(now)
	if err != nil || len(first) != 1 {
		t.Fatalf("the first run prepared %d payment(s): %v", len(first), err)
	}
	// Nobody signs it. Eleven minutes later it is undone by the next run,
	// which pays the same addresses.
	later := now.Add(11 * time.Minute)
	second, reason, err := poolPrepare(later)
	if kept, _ := poolLedger.payment(first[0].Serial); kept.State != poolPaymentReturned {
		t.Fatalf("eleven minutes on, the unsigned payment is %s", kept.State)
	}
	if err != nil || len(second) != 1 || linesOf(second)[pool.alice] != cvn("37.5") || linesOf(second)[pool.bob] != cvn("12.5") {
		t.Fatalf("after the unsigned payment was undone, the next run prepared %d payment(s) (%q): %v; want both addresses paid", len(second), reason, err)
	}
}

// What the pool's page says must be the figures the pool pays by.
func TestThePoolsPageSaysWhatItPaysBy(t *testing.T) {
	startPayingPool(t)
	if page := readPoolPage(time.Now()); page.MinimumPayment != "1" || page.PaymentFee != "0.001" || page.SmallestPayment != "0.002" || page.PayAnyAmountAfterHours != 12 {
		t.Errorf("the page gives a minimum of %s CVN, a fee of %s, a smallest payment of %s and %d hours", page.MinimumPayment, page.PaymentFee, page.SmallestPayment, page.PayAnyAmountAfterHours)
	}
	pages := http.NewServeMux()
	addPoolPages(pages)
	reply := httptest.NewRecorder()
	pages.ServeHTTP(reply, httptest.NewRequest("GET", "/pool", nil))
	for _, want := range []string{"owed 1 CVN or more", "not been paid in the last 12 hours", "0.002 CVN or more"} {
		if !strings.Contains(reply.Body.String(), want) {
			t.Errorf("the pool's page does not say %q", want)
		}
	}
}

// A pool keeps its promise only if it pays at least once a day, so the
// installer does not take a longer interval.
func TestTheInstallerTakesNoIntervalLongerThanADay(t *testing.T) {
	raw, err := os.ReadFile("../deploy/install_pool.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if !strings.Contains(script, `if [ "$PAY_HOURS" -gt 24 ]; then`) {
		t.Error("the pool's installer does not refuse more than 24 hours between payments")
	}
	if strings.Contains(script, "168") {
		t.Error("the pool's installer still speaks of 168 hours")
	}
	// A figure of three digits or more is refused before it is compared: a
	// number too long for the shell to compare would otherwise slip past.
	if !strings.Contains(script, `''|*[!0-9]*|0*|???*)`) {
		t.Error("the pool's installer does not refuse a figure of three digits or more outright")
	}
}
