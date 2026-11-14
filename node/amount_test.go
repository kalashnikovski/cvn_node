package node

import (
	"encoding/json"
	"fmt"
	"testing"
)

// onNetwork runs the rest of a test with the named network's settings and
// restores the previous ones afterwards.
func onNetwork(t *testing.T, name string) {
	t.Helper()
	saved := Params
	if err := SelectNetwork(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Params = saved; applyParams() })
}

func TestParseCVN(t *testing.T) {
	good := map[string]int64{
		"50": 5000000000, "0.01": 1000000, "0.00000001": 1, "1000.01": 100001000000,
		" 12.5 ": 1250000000, ".5": 50000000, "7.": 700000000, "1.230000000": 123000000,
		"0": 0, "2100000000": MaxAmountUnits,
	}
	for text, want := range good {
		if got, err := ParseCVN(text); err != nil || got != want {
			t.Errorf("ParseCVN(%q) = %d, %v; want %d", text, got, err, want)
		}
	}
	for _, text := range []string{"", "abc", "-1", "1e5", "1,000", "0.000000001", "1.2.3", "2100000000.00000001", "99999999999999999999", "+5", "0x10"} {
		if got, err := ParseCVN(text); err == nil {
			t.Errorf("ParseCVN(%q) was accepted as %d", text, got)
		}
	}
	// What a person types is what is sent: no floating-point surprises.
	if a, _ := ParseCVN("0.1"); a+cvn("0.2") != cvn("0.3") {
		t.Error("0.1 + 0.2 is not exactly 0.3 in units")
	}
}

func TestFormatUnits(t *testing.T) {
	cases := map[int64]string{5000000000: "50", 1000000: "0.01", 1: "0.00000001", 150000000: "1.5", 0: "0", -250000000: "-2.5"}
	for units, want := range cases {
		if got := FormatUnits(units); got != want {
			t.Errorf("FormatUnits(%d) = %q, want %q", units, got, want)
		}
	}
	for _, text := range []string{"50", "0.01", "0.00000001", "123456.78901234"} {
		if FormatUnits(cvn(text)) != text {
			t.Errorf("%q does not survive ParseCVN then FormatUnits: %q", text, FormatUnits(cvn(text)))
		}
	}
}

// On a network that writes units, an amount is a plain whole number in
// blocks and in what is hashed, and nothing else is accepted.
func TestAmountsAsWholeUnits(t *testing.T) {
	onNetwork(t, "devnet")

	encoded, _ := json.Marshal(UTXOOutput{Recipient: "CVN_a", Amount: UnitsAmount(cvn("50.25"))})
	if string(encoded) != `{"recipient":"CVN_a","amount":5025000000}` {
		t.Errorf("written as %s", encoded)
	}
	var decoded UTXOOutput
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Amount.Units() != cvn("50.25") || !decoded.Amount.Spendable() {
		t.Errorf("read back as %d units, spendable %v, err %v", decoded.Amount.Units(), decoded.Amount.Spendable(), err)
	}
	if UnitsAmount(cvn("50.25")).hashText() != "5025000000" {
		t.Errorf("hashed as %q", UnitsAmount(cvn("50.25")).hashText())
	}

	// Amounts far beyond what a decimal number can hold exactly still work.
	large := MaxAmountUnits - 1
	encoded, _ = json.Marshal(UnitsAmount(large))
	var back Amount
	if json.Unmarshal(encoded, &back); back.Units() != large {
		t.Errorf("%d units came back as %d", large, back.Units())
	}

	for _, bad := range []string{`50.25`, `-5`, `1e3`, `"100"`, `null`, `true`, `100.0`, `99999999999999999999`, `210000000000000001`} {
		var amount Amount
		_ = json.Unmarshal([]byte(bad), &amount)
		if amount.Spendable() || amount.ValidFee() {
			t.Errorf("%s was accepted as an amount of %d units", bad, amount.Units())
		}
	}
	var zero Amount
	_ = json.Unmarshal([]byte(`0`), &zero)
	if zero.Spendable() || !zero.ValidFee() {
		t.Error("zero must be a valid fee but not a valid output")
	}
}

// On testnet-1 an amount is written as the decimal number it always was, and
// the exact number is kept so old hashes can be reproduced.
func TestAmountsOnTestnet1KeepTheirDecimalForm(t *testing.T) {
	onNetwork(t, "testnet-1")

	// A reward output from a real block on that network, mined before
	// amounts were built from units: 50 plus a 0.01 fee, as floating point.
	const realBlockText = `{"recipient":"CVN_a","amount":50.00999999999999}`
	var output UTXOOutput
	if err := json.Unmarshal([]byte(realBlockText), &output); err != nil {
		t.Fatal(err)
	}
	if output.Amount.Units() != cvn("50.01") {
		t.Errorf("worth %s CVN to the rules, want 50.01", FormatUnits(output.Amount.Units()))
	}
	if output.Amount.hashText() != "50.00999999999999" {
		t.Errorf("hashed as %q; the original text must be reproduced exactly", output.Amount.hashText())
	}
	rewritten, _ := json.Marshal(output)
	if string(rewritten) != realBlockText {
		t.Errorf("written back as %s, want %s", rewritten, realBlockText)
	}

	// New amounts made by this build are exact and written as plain decimals.
	fresh, _ := json.Marshal(UTXOOutput{Recipient: "CVN_a", Amount: UnitsAmount(cvn("50.01"))})
	if string(fresh) != `{"recipient":"CVN_a","amount":50.01}` {
		t.Errorf("new amount written as %s", fresh)
	}
	if UnitsAmount(cvn("50")).hashText() != "50" {
		t.Errorf("50 CVN hashed as %q", UnitsAmount(cvn("50")).hashText())
	}

	for _, bad := range []string{`-1`, `1e30`, `"abc"`, `null`} {
		var amount Amount
		_ = json.Unmarshal([]byte(bad), &amount)
		if amount.Spendable() {
			t.Errorf("%s was accepted as a spendable amount", bad)
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

// signedTestTransfer spends one named coin and pays `units` to a recipient.
func signedTestTransfer(t *testing.T, privateKey, publicKey, sender, sourceTx string, units int64) Transaction {
	t.Helper()
	tx := Transaction{
		Version: 1, PublicKey: publicKey, Timestamp: 1, DataSizeKB: 1, Witness: sender,
		Inputs:  []UTXOInput{{SourceTxID: sourceTx, Index: 0}},
		Outputs: []UTXOOutput{{Recipient: "CVN_recipient", Amount: UnitsAmount(units)}},
	}
	if Params.StrictFormat {
		// The launch format has no size field, and the sender signs the
		// amount of the coin spent (50 CVN in every test that uses this)
		// and the fee.
		tx.DataSizeKB = 0
		tx.Inputs[0].AmountUnits = cvn("50")
		if fee := cvn("50") - units; fee >= 0 {
			tx.FreeWillOffering = UnitsAmount(fee)
		}
	}
	tx.ID = ComputeTransactionID(tx)
	r, s, err := SignTransactionPayload(privateKey, tx.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx.Signature = r + "|" + s
	return tx
}

func TestTransferAmountRules(t *testing.T) {
	for _, network := range []string{"testnet-1", "devnet"} {
		t.Run(network, func(t *testing.T) {
			onNetwork(t, network)
			privateKey, publicKey, address, err := GenerateKeyPair()
			if err != nil {
				t.Fatal(err)
			}
			coins := map[string]UTXORecord{"TX_A_0": {Amount: UnitsAmount(cvn("50")), Recipient: address}}
			lookup := func(key string) (UTXORecord, bool) { record, ok := coins[key]; return record, ok }

			exact := signedTestTransfer(t, privateKey, publicKey, address, "TX_A", cvn("50"))
			if fee, _, err := checkTransferAgainstCoins(exact, lookup, nil); err != nil || fee != 0 {
				t.Errorf("paying out exactly what is taken in: fee %d, err %v", fee, err)
			}
			withFee := signedTestTransfer(t, privateKey, publicKey, address, "TX_A", cvn("49.99"))
			if fee, _, err := checkTransferAgainstCoins(withFee, lookup, nil); err != nil || fee != cvn("0.01") {
				t.Errorf("fee is %d units, err %v; want 0.01 CVN", fee, err)
			}
			oneUnitOver := signedTestTransfer(t, privateKey, publicKey, address, "TX_A", cvn("50")+1)
			if _, _, err := checkTransferAgainstCoins(oneUnitOver, lookup, nil); err == nil {
				t.Error("a transfer paying out one unit more than it takes in was accepted")
			}
			nothing := signedTestTransfer(t, privateKey, publicKey, address, "TX_A", 0)
			if _, _, err := checkTransferAgainstCoins(nothing, lookup, nil); err == nil {
				t.Error("an output of zero was accepted")
			}
			if _, _, err := checkTransferAgainstCoins(exact, lookup, map[string]bool{"TX_A_0": true}); err == nil {
				t.Error("a coin already being spent was accepted again")
			}

			// Changing an amount after signing must break the transfer.
			tampered := exact
			tampered.Outputs = []UTXOOutput{{Recipient: "CVN_recipient", Amount: UnitsAmount(cvn("49"))}}
			if _, _, err := checkTransferAgainstCoins(tampered, lookup, nil); err == nil {
				t.Error("a transfer whose amount was changed after signing was accepted")
			}
		})
	}
}

func TestWalletBuildsExactTransfers(t *testing.T) {
	onNetwork(t, "devnet")
	privateKey, publicKey, address, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	coins := map[string]UTXORecord{
		"TX_A_0": {Amount: UnitsAmount(cvn("50")), Recipient: address},
		"TX_B_0": {Amount: UnitsAmount(cvn("0.1")), Recipient: address},
		"TX_C_0": {Amount: UnitsAmount(cvn("0.2")), Recipient: address},
	}
	lookup := func(key string) (UTXORecord, bool) { record, ok := coins[key]; return record, ok }
	spendable := []SpendableCoin{
		{TxID: "TX_A", Amount: UnitsAmount(cvn("50"))},
		{TxID: "TX_C", Amount: UnitsAmount(cvn("0.2"))},
		{TxID: "TX_B", Amount: UnitsAmount(cvn("0.1"))},
	}

	tx, err := BuildSignedTransfer(privateKey, publicKey, "CVN_recipient", cvn("50.25"), cvn("0.01"), spendable)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(tx.Inputs) != 3 || len(tx.Outputs) != 2 {
		t.Fatalf("transfer has %d inputs and %d outputs, want 3 and 2", len(tx.Inputs), len(tx.Outputs))
	}
	if tx.Outputs[1].Amount.Units() != cvn("0.04") || tx.Outputs[1].Recipient != address {
		t.Errorf("change is %s CVN to %s, want 0.04 back to the sender", tx.Outputs[1].Amount, tx.Outputs[1].Recipient)
	}
	if fee, _, err := checkTransferAgainstCoins(tx, lookup, nil); err != nil || fee != cvn("0.01") {
		t.Errorf("fee %d units, err %v; want 0.01 CVN", fee, err)
	}
	if _, err := BuildSignedTransfer(privateKey, publicKey, "CVN_recipient", cvn("60"), 0, spendable); err == nil {
		t.Error("a transfer for more than the wallet holds was built")
	}
	if _, err := BuildSignedTransfer(privateKey, publicKey, "CVN_recipient", 0, 0, spendable); err == nil {
		t.Error("a transfer of nothing was built")
	}

	// A transfer survives being written out and read back, as it is between nodes.
	wire, _ := json.Marshal(tx)
	var received Transaction
	if err := json.Unmarshal(wire, &received); err != nil {
		t.Fatal(err)
	}
	if _, _, err := checkTransferAgainstCoins(received, lookup, nil); err != nil {
		t.Errorf("the transfer was refused after a round trip: %v", err)
	}
}

func TestRewardRules(t *testing.T) {
	for _, network := range []string{"testnet-1", "devnet"} {
		t.Run(network, func(t *testing.T) {
			onNetwork(t, network)
			block := func(rewards ...int64) Block {
				coinbase := Transaction{
					Version: 1, ID: fmt.Sprintf("TX_COINBASE_H%d_0a", 100), Inputs: []UTXOInput{}, Witness: "CVN_miner",
					PublicKey: CoinbasePublicKeyMarker, Signature: CoinbaseSignatureMarker,
				}
				for _, units := range rewards {
					coinbase.Outputs = append(coinbase.Outputs, UTXOOutput{Recipient: "CVN_miner", Amount: UnitsAmount(units)})
				}
				return Block{Index: 100, Transactions: []Transaction{coinbase}}
			}
			nothing := func(string) (UTXORecord, bool) { return UTXORecord{}, false }

			accept := map[string]Block{
				"exactly the reward":   block(cvn("50")),
				"less than the reward": block(cvn("49.5")),
			}
			// Two reward outputs are the uncle split, which only the first
			// test network has.
			split := block(cvn("40"), cvn("10"))
			if _, _, err := ValidateBlockTransactions(split, nothing, 0); (err == nil) == Params.NoUncles {
				t.Errorf("a reward split in two on %s: err %v", network, err)
			}
			for name, b := range accept {
				if _, _, err := ValidateBlockTransactions(b, nothing, 0); err != nil {
					t.Errorf("%s: refused: %v", name, err)
				}
			}
			refuse := map[string]Block{
				"one unit too much":             block(cvn("50") + 1),
				"uncle split one unit too much": block(cvn("40"), cvn("10")+1),
				"three reward outputs":          block(cvn("20"), cvn("20"), cvn("10")),
				"a reward output of zero":       block(cvn("50"), 0),
			}
			for name, b := range refuse {
				if _, _, err := ValidateBlockTransactions(b, nothing, 0); err == nil {
					t.Errorf("%s: accepted", name)
				}
			}
		})
	}
}
