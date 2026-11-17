package node

import (
	"fmt"
	"strings"
	"testing"
)

func TestDescribeBlocksAway(t *testing.T) {
	cases := map[int64]string{
		0:       "now",
		-5:      "now",
		40:      "about 30 minutes",
		800:     "about 10 hours",
		1920:    "about 24 hours",
		19200:   "about 10 days",
		172800:  "about 3 months",
		700800:  "about 12 months",
		4905600: "about 7 years",
	}
	for blocks, want := range cases {
		if got := describeBlocksAway(blocks); got != want {
			t.Errorf("%d blocks: %q, want %q", blocks, got, want)
		}
	}
}

// Renewing picks the coins whose seven-year date is within the horizon,
// soonest first; a single coin is enough; coins with no date are left out.
func TestPlanRenewal(t *testing.T) {
	coin := func(id string, sweepAt int64) SpendableCoin {
		return SpendableCoin{TxID: id, Index: 0, Amount: UnitsAmount(cvn("50")), SweepAt: sweepAt}
	}
	const tip = 1000000
	coins := []SpendableCoin{
		coin("far", tip+renewHorizonBlocks+1),
		coin("soon", tip+500),
		coin("edge", tip+renewHorizonBlocks),
		coin("overdue-this-block", tip),
		coin("no-date", 0),
		{TxID: "unusable", Index: 0, SweepAt: tip + 10}, // an amount of nothing
	}
	groups := planRenewal(coins, tip, renewHorizonBlocks)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	var order []string
	for _, c := range groups[0] {
		order = append(order, c.TxID)
	}
	if got := strings.Join(order, " "); got != "overdue-this-block soon edge" {
		t.Errorf("coins chosen, soonest first: %q", got)
	}

	// Asked for everything, the far coin is included; the dateless one never is.
	all := planRenewal(coins, tip, 0)
	if len(all) != 1 || len(all[0]) != 4 || all[0][3].TxID != "far" {
		t.Errorf("renewing everything chose %+v", all)
	}

	// One coin alone is renewed (combining would refuse it).
	if single := planRenewal([]SpendableCoin{coin("only", tip+1)}, tip, renewHorizonBlocks); len(single) != 1 || len(single[0]) != 1 {
		t.Errorf("a single coin near its date was not chosen: %+v", single)
	}
	// Nothing near its date: nothing to do.
	if none := planRenewal([]SpendableCoin{coin("far", tip+renewHorizonBlocks*3)}, tip, renewHorizonBlocks); len(none) != 0 {
		t.Errorf("a coin years from its date was chosen: %+v", none)
	}

	// Many coins: groups of the most one transfer can spend, and no more
	// transfers than one action sends.
	var many []SpendableCoin
	for i := 0; i < TxMaxInputs*combineMaxTransfers+50; i++ {
		many = append(many, coin(fmt.Sprintf("c%05d", i), tip+int64(i)))
	}
	groups = planRenewal(many, tip, renewHorizonBlocks)
	if len(groups) != combineMaxTransfers || len(groups[0]) != TxMaxInputs || groups[0][0].TxID != "c00000" {
		t.Errorf("%d groups, first of %d coins starting with %s", len(groups), len(groups[0]), groups[0][0].TxID)
	}
}

// On a chain: a coin renewed before its date is not swept at that date, the
// balance is unchanged, and the new coin has a new count.
func TestRenewingACoinSavesItFromTheSweep(t *testing.T) {
	timestamp := startSweepChain(t) // a coin is swept five blocks after it is made
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner) // block 1: the coin, due at the end of block 6
	mineNext(t, timestamp, testAddress("other"))
	mineNext(t, timestamp, testAddress("other"))

	coins := ListSpendableCoins(owner)
	if len(coins) != 1 || coins[0].SweepAt != 6 {
		t.Fatalf("set-up: %+v", coins)
	}
	groups := planRenewal(coins, GetLatestBlockInternal().Index, renewHorizonBlocks)
	if len(groups) != 1 || len(groups[0]) != 1 {
		t.Fatalf("the coin near its date was not chosen for renewal: %+v", groups)
	}
	renewal, err := BuildSignedRenewal(privateKey, publicKey, 0, groups[0])
	if err != nil {
		t.Fatalf("building the renewal: %v", err)
	}
	if len(renewal.Inputs) != 1 || len(renewal.Outputs) != 1 || renewal.Outputs[0].Recipient != owner || renewal.Outputs[0].Amount.Units() != cvn("50") {
		t.Fatalf("a renewal must send the whole coin back to its owner: %+v", renewal)
	}
	mineNext(t, timestamp, testAddress("other"), renewal) // block 4
	mineNext(t, timestamp, testAddress("other"))
	mineNext(t, timestamp, testAddress("other")) // block 6: the first coin's date
	mineNext(t, timestamp, testAddress("other"))

	if balanceOf(owner) != cvn("50") {
		t.Errorf("after renewing, the owner has %s, want the whole 50", FormatUnits(balanceOf(owner)))
	}
	if after := ListSpendableCoins(owner); len(after) != 1 || after[0].SweepAt != 9 {
		t.Errorf("the renewed coin should be due at the end of block 9: %+v", after)
	}
	everythingIssuedIsAccountedFor(t, "after renewing", owner, testAddress("other"))

	// A coin the same age that was not renewed lost its fifth.
	if balanceOf(testAddress("other")) >= 6*cvn("50") {
		t.Error("the coins that were not renewed were expected to be swept; this test no longer shows the difference")
	}
}

// Renewing refuses what cannot work, and says why.
func TestRenewalRefusals(t *testing.T) {
	onNetwork(t, "devnet")
	privateKey, publicKey, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	coin := SpendableCoin{TxID: "TX_A", Index: 0, Amount: UnitsAmount(cvn("1")), SweepAt: 100}
	if _, err := BuildSignedRenewal(privateKey, publicKey, 0, nil); err == nil {
		t.Error("a renewal of no coins was built")
	}
	if _, err := BuildSignedRenewal(privateKey, publicKey, -1, []SpendableCoin{coin}); err == nil {
		t.Error("a negative fee was accepted")
	}
	if _, err := BuildSignedRenewal(privateKey, publicKey, cvn("1"), []SpendableCoin{coin}); err == nil {
		t.Error("a fee that swallows the whole coin was accepted")
	}
	// Combining still needs two coins; renewing does not.
	if _, err := BuildSignedCombine(privateKey, publicKey, 0, []SpendableCoin{coin}); err == nil {
		t.Error("combining accepted a single coin")
	}
	if _, err := BuildSignedRenewal(privateKey, publicKey, 0, []SpendableCoin{coin}); err != nil {
		t.Errorf("a single coin could not be renewed: %v", err)
	}
}
