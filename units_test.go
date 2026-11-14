package main

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestUnitConversions(t *testing.T) {
	cases := []struct {
		amount float64
		units  int64
	}{
		{0, 0}, {50, 5000000000}, {0.01, 1000000}, {0.00000001, 1},
		{0.1 + 0.2, 30000000},     // floating point says 0.30000000000000004
		{1000.01 - 1000, 1000000}, // floating point says 0.00999999999999
		{12.5, 1250000000},
	}
	for _, c := range cases {
		got, ok := AmountToUnits(c.amount)
		if !ok || got != c.units {
			t.Errorf("AmountToUnits(%v) = %d, %v; want %d", c.amount, got, ok, c.units)
		}
	}
	for _, bad := range []float64{-1, TxMaxAmount * 2} {
		if _, ok := AmountToUnits(bad); ok {
			t.Errorf("AmountToUnits(%v) was accepted", bad)
		}
	}
	if FormatUnits(5000000000) != "50" || FormatUnits(1000000) != "0.01" || FormatUnits(1) != "0.00000001" || FormatUnits(150000000) != "1.5" {
		t.Error("FormatUnits output changed")
	}
	if BlockRewardUnits(7000) != 50*UnitsPerCVN {
		t.Errorf("reward at block 7000 is %d units", BlockRewardUnits(7000))
	}
}

// Up to 20,000,000 CVN in a single amount, units survive the trip through the
// decimal number that is stored and sent.
func TestUnitsSurviveStorageFormat(t *testing.T) {
	random := rand.New(rand.NewSource(7))
	const limit = int64(20000000) * UnitsPerCVN
	for i := 0; i < 500000; i++ {
		units := random.Int63n(limit)
		if got, ok := AmountToUnits(UnitsToAmount(units)); !ok || got != units {
			t.Fatalf("%d units came back as %d", units, got)
		}
	}
}

func TestAddUnitsRefusesImpossibleTotals(t *testing.T) {
	if _, err := addUnits(MaxAmountUnits, 1); err == nil {
		t.Error("a total above everything that can exist was accepted")
	}
	if _, err := addUnits(-1, 5); err == nil {
		t.Error("a negative amount was accepted")
	}
	if sum, err := addUnits(40, 2); err != nil || sum != 42 {
		t.Errorf("addUnits(40, 2) = %d, %v", sum, err)
	}
}

// signedTestTransfer spends one named coin and pays `amount` to a recipient.
func signedTestTransfer(t *testing.T, privateKey, publicKey, sender, sourceTx string, amount float64) Transaction {
	t.Helper()
	tx := Transaction{
		Version: 1, PublicKey: publicKey, Timestamp: 1, DataSizeKB: 1, Witness: sender,
		Inputs:  []UTXOInput{{SourceTxID: sourceTx, Index: 0}},
		Outputs: []UTXOOutput{{Recipient: "CVN_recipient", Amount: amount}},
	}
	tx.ID = ComputeTransactionID(tx)
	r, s, err := SignTransactionPayload(privateKey, tx.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx.Signature = r + "|" + s
	return tx
}

func TestWholeUnitTransferRules(t *testing.T) {
	privateKey, publicKey, address, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	coins := map[string]UTXORecord{"TX_A_0": {Amount: 50, Recipient: address}}
	lookup := func(key string) (UTXORecord, bool) { record, ok := coins[key]; return record, ok }

	exact := signedTestTransfer(t, privateKey, publicKey, address, "TX_A", 50)
	if _, _, err := checkTransferAgainstCoins(exact, lookup, nil, true); err != nil {
		t.Errorf("paying out exactly what is taken in was refused: %v", err)
	}

	oneUnitOver := signedTestTransfer(t, privateKey, publicKey, address, "TX_A", 50.00000001)
	if _, _, err := checkTransferAgainstCoins(oneUnitOver, lookup, nil, true); err == nil {
		t.Error("a transfer paying out one unit more than it takes in was accepted under whole-unit rules")
	}
	// This is the gap the whole-unit rule closed: the older rule allows it.
	if _, _, err := checkTransferAgainstCoins(oneUnitOver, lookup, nil, false); err != nil {
		t.Errorf("the pre-fork rule is expected to tolerate one unit (history must stay valid): %v", err)
	}

	dust := signedTestTransfer(t, privateKey, publicKey, address, "TX_A", 0.000000001)
	if _, _, err := checkTransferAgainstCoins(dust, lookup, nil, true); err == nil {
		t.Error("an output smaller than one unit was accepted")
	}
}

func TestWalletBuildsExactTransfers(t *testing.T) {
	privateKey, publicKey, address, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	coins := map[string]UTXORecord{
		"TX_A_0": {Amount: 50, Recipient: address},
		"TX_B_0": {Amount: 0.1, Recipient: address},
		"TX_C_0": {Amount: 0.2, Recipient: address},
	}
	lookup := func(key string) (UTXORecord, bool) { record, ok := coins[key]; return record, ok }
	spendable := []SpendableCoin{{TxID: "TX_A", Amount: 50}, {TxID: "TX_C", Amount: 0.2}, {TxID: "TX_B", Amount: 0.1}}

	tx, err := BuildSignedTransfer(privateKey, publicKey, "CVN_recipient", 50.25, 0.01, spendable)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(tx.Inputs) != 3 || len(tx.Outputs) != 2 {
		t.Fatalf("transfer has %d inputs and %d outputs, want 3 and 2", len(tx.Inputs), len(tx.Outputs))
	}
	if change, _ := AmountToUnits(tx.Outputs[1].Amount); change != 4000000 {
		t.Errorf("change is %d units, want 4,000,000 (0.04 CVN)", change)
	}
	fee, _, err := checkTransferAgainstCoins(tx, lookup, nil, true)
	if feeUnits, _ := AmountToUnits(fee); err != nil || feeUnits != 1000000 {
		t.Errorf("whole-unit check: fee %v, err %v; want 0.01", fee, err)
	}
	if _, _, err := checkTransferAgainstCoins(tx, lookup, nil, false); err != nil {
		t.Errorf("the transfer must also be valid under the older rule: %v", err)
	}

	if _, err := BuildSignedTransfer(privateKey, publicKey, "CVN_recipient", 60, 0, spendable); err == nil {
		t.Error("a transfer for more than the wallet holds was built")
	}
	if _, err := BuildSignedTransfer(privateKey, publicKey, "CVN_recipient", 0.000000001, 0, spendable); err == nil {
		t.Error("a transfer of less than one unit was built")
	}
}

func TestWholeUnitRewardRules(t *testing.T) {
	saved := UnitsForkActivationHeight
	UnitsForkActivationHeight = 100
	defer func() { UnitsForkActivationHeight = saved }()

	block := func(height int64, rewards ...float64) Block {
		coinbase := Transaction{Version: 1, ID: fmt.Sprintf("TX_COINBASE_H%d_x", height), Inputs: []UTXOInput{}}
		for _, amount := range rewards {
			coinbase.Outputs = append(coinbase.Outputs, UTXOOutput{Recipient: "CVN_miner", Amount: amount})
		}
		return Block{Index: height, Transactions: []Transaction{coinbase}}
	}
	nothing := func(string) (UTXORecord, bool) { return UTXORecord{}, false }

	accept := map[string]Block{
		"exactly the reward":            block(100, 50),
		"80/20 split with an uncle":     block(100, 40, 10),
		"less than the reward":          block(100, 49.5),
		"old tolerance before the fork": block(99, 50.00000001),
	}
	for name, b := range accept {
		if _, _, err := ValidateBlockTransactions(b, nothing); err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
	}
	refuse := map[string]Block{
		"one unit too much":                block(100, 50.00000001),
		"uncle split one unit too much":    block(100, 40, 10.00000001),
		"clearly too much before the fork": block(99, 50.02),
		"three reward outputs":             block(100, 20, 20, 10),
	}
	for name, b := range refuse {
		if _, _, err := ValidateBlockTransactions(b, nothing); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
