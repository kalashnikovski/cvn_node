package node

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"
)

// WHOM ONE RUN OF PAYMENTS PAYS, FIGURE BY FIGURE
//
// (See pool_daily_test.go for what this is about.)

// dailyPlan is a run of payments at `now` by the figures a pool uses: every
// address owed 1 CVN or more, and every address owed 0.002 CVN or more that
// has not been paid in the last twelve hours; a fee of 0.001 CVN a transfer.
func dailyPlan(now int64) poolPaymentPlan {
	return poolPaymentPlan{Now: now, PoolAddress: testAddress("pool"), MinimumUnits: cvn("1"), FeeUnits: cvn("0.001"),
		SmallestUnits: cvn("0.002"), NotPaidSince: now - 12*3600}
}

// owedTo is what the record says an address is owed now.
func owedTo(record *poolRecord, address string) int64 {
	account, _ := record.account(address, 1)
	return account.Owed
}

// notePayment writes into the record's history a payment to the addresses
// given, prepared at `at`, in the state given. Only what a later run reads of
// it is filled in.
func notePayment(record *poolRecord, state string, at int64, addresses ...string) {
	record.Lock()
	defer record.Unlock()
	payment := poolPayment{Serial: record.nextPayment, State: state, PreparedAt: at}
	for _, address := range addresses {
		payment.Lines = append(payment.Lines, poolPaymentLine{Address: address, Debit: 1, Receives: 1})
	}
	record.nextPayment++
	record.payments = append(record.payments, payment)
}

// checkRun checks everything a run's payments must satisfy whoever they
// pay: each would be signed by the paying command; each line debits exactly
// what the address was owed and leaves a coin; the fee is shared to the
// unit; and the coins spent are the outputs and the fee, no more.
func checkRun(t *testing.T, payments []poolPayment, owedBefore map[string]int64, feeUnits int64) map[string]int64 {
	t.Helper()
	paid := map[string]int64{}
	for _, payment := range payments {
		if err := poolPaymentLooksRight(payment, testAddress("pool"), feeUnits); err != nil {
			t.Fatalf("payment %d would be refused by the paying command: %v", payment.Serial, err)
		}
		var debits, receives, in, out int64
		for _, line := range payment.Lines {
			if _, twice := paid[line.Address]; twice {
				t.Fatalf("%s is paid twice in one run", line.Address)
			}
			paid[line.Address] = line.Debit
			if line.Debit != owedBefore[line.Address] {
				t.Fatalf("%s is debited %d units; it was owed %d", line.Address, line.Debit, owedBefore[line.Address])
			}
			if line.Receives < minimumOutputUnits() || line.Receives > line.Debit {
				t.Fatalf("%s receives %d units of %d, which is not a coin or is more than it was owed", line.Address, line.Receives, line.Debit)
			}
			debits += line.Debit
			receives += line.Receives
		}
		for _, input := range payment.Inputs {
			in += input.AmountUnits
		}
		for _, output := range payment.Outputs {
			out += output.Amount.Units()
		}
		if debits-receives != feeUnits || in != out+feeUnits {
			t.Fatalf("payment %d: debited %d, received %d, fee %d; coins in %d, out %d", payment.Serial, debits, receives, feeUnits, in, out)
		}
	}
	return paid
}

func TestARunPaysSmallAmountsDownToTheSmallestPayment(t *testing.T) {
	// 50 CVN among four: 49.9935, 0.003, 0.002 and 0.0015.
	record, addresses := recordWithOwed(t, cvn("50"), []int64{99987, 6, 4, 3})
	owed := map[string]int64{}
	for _, address := range addresses {
		owed[address] = owedTo(record, address)
	}
	if owed[addresses[0]] != cvn("49.9935") || owed[addresses[1]] != cvn("0.003") || owed[addresses[2]] != cvn("0.002") || owed[addresses[3]] != cvn("0.0015") {
		t.Fatalf("set-up: the four are owed %v", owed)
	}

	// By the old rule alone, only the first is paid.
	old := dailyPlan(2000)
	old.SmallestUnits = 0
	payments, _, err := record.preparePayments(old)
	if err != nil || len(payments) != 1 || len(payments[0].Lines) != 1 || payments[0].Lines[0].Address != addresses[0] {
		t.Fatalf("with no rule for small amounts, the run prepared %+v: %v; want the first address alone", payments, err)
	}
	if done, err := record.returnPayment(payments[0].Serial, 101); err != nil || !done {
		t.Fatalf("set-up: the payment could not be undone: %v", err)
	}

	// With it: the three owed 0.002 CVN or more, none of whom was ever
	// paid. The fourth is owed less than the smallest payment, and waits.
	payments, _, err = record.preparePayments(dailyPlan(3000))
	if err != nil || len(payments) != 1 || len(payments[0].Lines) != 3 {
		t.Fatalf("the run prepared %d payment(s): %v; want one paying three addresses", len(payments), err)
	}
	paid := checkRun(t, payments, owed, cvn("0.001"))
	if _, there := paid[addresses[3]]; there || len(paid) != 3 {
		t.Fatalf("the run pays %v; want the first three and not the fourth", paid)
	}
	// The fee of 100000 units among three: 33334, 33333, 33333.
	lines := payments[0].Lines
	if lines[0].Receives != cvn("49.9935")-33334 || lines[1].Receives != 300000-33333 || lines[2].Receives != 200000-33333 {
		t.Errorf("the lines are %+v", lines)
	}
	for i, address := range addresses {
		account, _ := record.account(address, 1)
		if i < 3 && (account.Owed != 0 || account.Pending != owed[address]) {
			t.Errorf("after the run, address %d's account is %+v; want nothing owed and %d on its way", i, account, owed[address])
		}
		if i == 3 && (account.Owed != owed[address] || account.Pending != 0) {
			t.Errorf("after the run, the address owed too little has %+v; want it untouched", account)
		}
	}
}

// The least a pool pays, paid alone: the whole fee falls on it, and what is
// left is still a coin. One unit less is not paid.
func TestTheSmallestPaymentPaidAloneLeavesACoin(t *testing.T) {
	record, addresses := recordWithOwed(t, cvn("0.002"), []int64{10})
	owed := map[string]int64{addresses[0]: owedTo(record, addresses[0])}
	if owed[addresses[0]] != 200000 {
		t.Fatalf("set-up: the address is owed %d units, want 200000", owed[addresses[0]])
	}
	payments, reason, err := record.preparePayments(dailyPlan(3000))
	if err != nil || len(payments) != 1 || len(payments[0].Lines) != 1 {
		t.Fatalf("the run prepared %d payment(s) (%q): %v", len(payments), reason, err)
	}
	checkRun(t, payments, owed, cvn("0.001"))
	if line := payments[0].Lines[0]; line.Receives != 100000 || payments[0].ChangeUnits != 0 {
		t.Errorf("the address receives %d units with %d in change; want 100000 and none", line.Receives, payments[0].ChangeUnits)
	}

	less, lessAddresses := recordWithOwed(t, cvn("0.002")-1, []int64{10})
	if got := owedTo(less, lessAddresses[0]); got != 199999 {
		t.Fatalf("set-up: the address is owed %d units, want 199999", got)
	}
	if payments, reason, err := less.preparePayments(dailyPlan(3000)); err != nil || len(payments) != 0 || !strings.Contains(reason, "nobody is owed 0.002 CVN or more") {
		t.Errorf("an address owed one unit less than the smallest payment: %d payment(s) (%q): %v; want none, and the reason that nobody is owed the least the pool pays", len(payments), reason, err)
	}
}

// Who has been "paid lately": a payment prepared within the last twelve
// hours and not undone, whatever became of it since.
func TestWhoCountsAsPaidInTheLastTwelveHours(t *testing.T) {
	// 3 CVN among six, half a coin each: all under the minimum.
	record, addresses := recordWithOwed(t, cvn("3"), []int64{10, 10, 10, 10, 10, 10})
	const now = int64(1_800_000_000)
	const twelveHours = int64(12 * 3600)
	notePayment(record, poolPaymentSettled, now-twelveHours, addresses[0])   // exactly twelve hours ago: due again
	notePayment(record, poolPaymentSettled, now-twelveHours+1, addresses[1]) // a second less: not yet
	notePayment(record, poolPaymentReturned, now-3600, addresses[2])         // undone: never paid
	notePayment(record, poolPaymentSent, now-3600, addresses[3])             // sent an hour ago: paid lately
	// addresses[4] has never been paid.
	notePayment(record, poolPaymentSettled, now+3600, addresses[5]) // dated an hour from now, by a clock since set back: not counted
	owed := map[string]int64{}
	for _, address := range addresses {
		owed[address] = owedTo(record, address)
		if owed[address] != cvn("0.5") {
			t.Fatalf("set-up: an address is owed %d units", owed[address])
		}
	}
	payments, _, err := record.preparePayments(dailyPlan(now))
	if err != nil {
		t.Fatal(err)
	}
	paid := checkRun(t, payments, owed, cvn("0.001"))
	for i, want := range []bool{true, false, true, false, true, true} {
		if _, got := paid[addresses[i]]; got != want {
			t.Errorf("address %d: paid %v, want %v", i, got, want)
		}
	}
}

// The figures a run is given are checked before anything is prepared: the
// smallest payment must leave a coin after the whole fee, and cannot be more
// than the minimum.
func TestARunRefusesFiguresThatCouldNotLeaveACoin(t *testing.T) {
	record, _ := recordWithOwed(t, cvn("50"), []int64{600, 390, 10})
	plan := dailyPlan(3000)
	plan.SmallestUnits = plan.FeeUnits + minimumOutputUnits()
	if _, _, err := record.preparePayments(plan); err == nil {
		t.Error("a smallest payment that the fee would leave less than a coin of was accepted")
	}
	plan.SmallestUnits++
	if payments, _, err := record.preparePayments(plan); err != nil || len(payments) != 1 {
		t.Errorf("a smallest payment one unit above the fee and the smallest coin: %d payment(s): %v", len(payments), err)
	}
	plan = dailyPlan(3000)
	plan.SmallestUnits = plan.MinimumUnits + 1
	if _, _, err := record.preparePayments(plan); err == nil {
		t.Error("a smallest payment above the minimum payment was accepted")
	}
	// And the figures the program itself uses pass that check on every
	// network, with room to spare.
	for _, network := range []string{"devnet", "cvn_testnet"} {
		onNetwork(t, network)
		if poolSmallestPaymentUnits <= poolPaymentFeeUnits+minimumOutputUnits() || poolSmallestPaymentUnits > poolMinimumPaymentUnits {
			t.Errorf("on %s the pool's smallest payment is %d units, its fee %d and the smallest coin %d", network, poolSmallestPaymentUnits, poolPaymentFeeUnits, minimumOutputUnits())
		}
	}
	if poolPayAnyAmountAfter <= 0 || poolPayAnyAmountAfter > 12*time.Hour {
		t.Errorf("an address owed a little waits %v for its turn; with runs up to a day apart that must be twelve hours or less for a run to reach it within a day", poolPayAnyAmountAfter)
	}
	if poolPayRunSlack < 5*time.Minute || poolPayRunSlack > time.Hour {
		t.Errorf("a node looks back %v less than it says; that slack is for runs that start some minutes early or late", poolPayRunSlack)
	}
}

// Many pools of made-up miners, each with its own history of payments, and
// one run on each. The run must pay exactly the addresses the two rules
// name, worked out here by themselves, and every payment must hold together
// to the unit. The seed is fixed, so a failure can be repeated.
func TestARunPaysExactlyWhomTheTwoRulesName(t *testing.T) {
	random := rand.New(rand.NewSource(20261126))
	const now = int64(1_800_000_000)
	checked, paying := 0, 0
	for round := 0; round < 40; round++ {
		// Up to forty addresses, so that one transfer has room for all.
		members := 1 + random.Intn(40)
		weights := make([]int64, members)
		for i := range weights {
			// Mostly small parts, some large ones.
			weights[i] = 1 + random.Int63n(20)
			if random.Intn(5) == 0 {
				weights[i] = 1 + random.Int63n(200000)
			}
		}
		reward := int64(1+random.Intn(5000)) * 1000000 // 0.01 to 50 CVN
		record, addresses := recordWithOwed(t, reward, weights)
		plan := dailyPlan(now)
		owed, want := map[string]int64{}, map[string]bool{}
		for _, address := range addresses {
			owed[address] = owedTo(record, address)
			lately := false
			switch random.Intn(5) {
			case 0:
				notePayment(record, poolPaymentSettled, now-random.Int63n(12*3600), address)
				lately = true
			case 1:
				notePayment(record, poolPaymentSettled, now-12*3600-random.Int63n(100000), address)
			case 2:
				notePayment(record, poolPaymentReturned, now-random.Int63n(12*3600), address)
			}
			want[address] = owed[address] >= plan.MinimumUnits || (owed[address] >= plan.SmallestUnits && !lately)
		}
		// The pool's one coin is the block's reward. What it does not pay
		// out comes back as change, and change must be a coin or nothing.
		// A round where it would be neither is one where the pool rightly
		// leaves somebody for the next run; those are not what is tested
		// here.
		change := reward
		for _, address := range addresses {
			if want[address] {
				change -= owed[address]
			}
		}
		if change != 0 && change < minimumOutputUnits() {
			continue
		}
		checked++
		payments, reason, err := record.preparePayments(plan)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		paid := checkRun(t, payments, owed, plan.FeeUnits)
		var wrong []string
		for _, address := range addresses {
			if _, got := paid[address]; got != want[address] {
				wrong = append(wrong, fmt.Sprintf("%s owed %d: paid %v, want %v", address, owed[address], got, want[address]))
			}
		}
		sort.Strings(wrong)
		if len(wrong) > 0 {
			t.Fatalf("round %d (%d addresses, reward %d, reason %q): %v", round, members, reward, reason, wrong)
		}
		var owedInAll, stillOwed, onItsWay int64
		for _, address := range addresses {
			account, _ := record.account(address, 1)
			owedInAll += owed[address]
			stillOwed += account.Owed
			onItsWay += account.Pending
			if (account.Pending != 0) != want[address] {
				t.Fatalf("round %d: %s has %d on its way; paid in this run: %v", round, address, account.Pending, want[address])
			}
		}
		if stillOwed+onItsWay != owedInAll {
			t.Fatalf("round %d: %d was owed in all; now %d is owed and %d on its way", round, owedInAll, stillOwed, onItsWay)
		}
		if len(paid) > 0 {
			paying++
		}
	}
	if checked < 30 || paying < 20 {
		t.Fatalf("only %d of the 40 rounds were checked, and in only %d was anybody paid", checked, paying)
	}
}

// One coin cannot pay more than one transfer in a run: its change is not a
// coin the pool can spend until it is in a block. With sixty addresses due
// and one coin, the forty-nine owed most are paid, the rest wait, and the
// run says why.
func TestWhenThePoolsCoinsDoNotReachTheRestWaitAndTheRunSaysSo(t *testing.T) {
	weights := make([]int64, 60)
	for i := range weights {
		weights[i] = int64(100 + i) // each owed a little more than the one before
	}
	record, addresses := recordWithOwed(t, cvn("0.6"), weights)
	owed := map[string]int64{}
	for _, address := range addresses {
		if owed[address] = owedTo(record, address); owed[address] < cvn("0.002") || owed[address] >= cvn("1") {
			t.Fatalf("set-up: an address is owed %d units; want every one between the smallest payment and the minimum", owed[address])
		}
	}
	payments, reason, err := record.preparePayments(dailyPlan(3000))
	if err != nil || len(payments) != 1 || len(payments[0].Lines) != 49 {
		t.Fatalf("the run prepared %d payment(s) (%q): %v; want one, paying forty-nine addresses", len(payments), reason, err)
	}
	if !strings.Contains(reason, "later run") {
		t.Errorf("eleven addresses were left for want of a coin, and the run said: %q", reason)
	}
	paid := checkRun(t, payments, owed, cvn("0.001"))
	for i, address := range addresses {
		_, got := paid[address]
		if want := i >= 11; got != want { // the eleven owed least are the first eleven
			t.Errorf("address %d, owed %d units: paid %v, want %v", i, owed[address], got, want)
		}
		if account, _ := record.account(address, 1); !got && (account.Owed != owed[address] || account.Pending != 0) {
			t.Errorf("address %d was not paid, and its account is %+v", i, account)
		}
	}
	// A second run before the change has been mined has no coin at all: it
	// pays nobody, and says so.
	if again, why, err := record.preparePayments(dailyPlan(3000)); err != nil || len(again) != 0 || !strings.Contains(why, "settled coins") {
		t.Errorf("a second run with no coin left prepared %d payment(s) (%q): %v", len(again), why, err)
	}
}

// A coin that would leave change too small to be a coin cannot pay: the
// address waits for another coin, nothing is lost, and the run says why.
func TestACoinThatWouldLeaveCrumbsAsChangePaysNobody(t *testing.T) {
	// 0.5 CVN between two: 0.499995 and 0.000005. The second is far under
	// the smallest payment, so the first would be paid alone and leave 500
	// units of change, which is no coin.
	record, addresses := recordWithOwed(t, cvn("0.5"), []int64{99999, 1})
	if a, b := owedTo(record, addresses[0]), owedTo(record, addresses[1]); a != 49999500 || b != 500 {
		t.Fatalf("set-up: the two are owed %d and %d units", a, b)
	}
	payments, reason, err := record.preparePayments(dailyPlan(3000))
	if err != nil || len(payments) != 0 || !strings.Contains(reason, "settled coins") {
		t.Fatalf("the run prepared %d payment(s) (%q): %v; want none, and a reason that speaks of the pool's coins", len(payments), reason, err)
	}
	if account, _ := record.account(addresses[0], 1); account.Owed != 49999500 || account.Pending != 0 {
		t.Errorf("the address that could not be paid has %+v; want it still owed everything", account)
	}
}
