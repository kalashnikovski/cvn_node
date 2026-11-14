package node

import (
	"crypto/elliptic"
	"math/big"
	"strings"
	"testing"
)

// launchTransfer returns a wallet-built transfer on the launch rules, with
// the wallet's key and the coin set it spends from.
func launchTransfer(t *testing.T) (tx Transaction, privateKey, publicKey string, lookup utxoLookup) {
	t.Helper()
	onNetwork(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	coins := map[string]UTXORecord{"TX_A_0": {Amount: UnitsAmount(cvn("50")), Recipient: owner}}
	lookup = func(key string) (UTXORecord, bool) { record, ok := coins[key]; return record, ok }
	tx, err = BuildSignedTransfer(privateKey, publicKey, "CVN_someone", cvn("10"), cvn("0.5"), []SpendableCoin{{TxID: "TX_A", Index: 0, Amount: UnitsAmount(cvn("50"))}})
	if err != nil {
		t.Fatal(err)
	}
	if fee, _, err := checkTransferAgainstCoins(tx, lookup, nil); err != nil || fee != cvn("0.5") {
		t.Fatalf("the wallet's own transfer: fee %d, err %v", fee, err)
	}
	return tx, privateKey, publicKey, lookup
}

func resign(t *testing.T, tx Transaction, privateKey string) Transaction {
	t.Helper()
	tx.ID = ComputeTransactionID(tx)
	r, s, err := SignTransactionPayload(privateKey, tx.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx.Signature = r + "|" + s
	return tx
}

// Audit finding: a wallet works out its change from the coin amounts a node
// reports. A node that understates them makes the wallet sign away the
// difference as a fee. The sender now signs the amount of every coin spent
// and the fee, so such a transfer is simply invalid.
func TestALyingNodeCannotTurnChangeIntoAFee(t *testing.T) {
	_, privateKey, publicKey, lookup := launchTransfer(t)

	// The node says the 50 CVN coin holds 11. The wallet sends 10, pays the
	// 0.5 fee it was asked for, and believes 0.5 comes back as change.
	understated := []SpendableCoin{{TxID: "TX_A", Index: 0, Amount: UnitsAmount(cvn("11"))}}
	tx, err := BuildSignedTransfer(privateKey, publicKey, "CVN_someone", cvn("10"), cvn("0.5"), understated)
	if err != nil {
		t.Fatal(err)
	}
	if fee, _, err := checkTransferAgainstCoins(tx, lookup, nil); err == nil {
		t.Fatalf("the transfer was accepted, and %s CVN of the sender's money became a fee", FormatUnits(fee))
	}
}

func TestTheFeeASenderSignsIsTheFeeTaken(t *testing.T) {
	tx, privateKey, _, lookup := launchTransfer(t)
	for name, change := range map[string]func(*Transaction){
		"a smaller fee than is really paid": func(tx *Transaction) { tx.FreeWillOffering = UnitsAmount(cvn("0.1")) },
		"a larger fee than is really paid":  func(tx *Transaction) { tx.FreeWillOffering = UnitsAmount(cvn("5")) },
		"an input amount that is not the coin's": func(tx *Transaction) {
			tx.Inputs = []UTXOInput{{SourceTxID: "TX_A", Index: 0, AmountUnits: cvn("49")}}
		},
		"no input amount": func(tx *Transaction) { tx.Inputs = []UTXOInput{{SourceTxID: "TX_A", Index: 0}} },
	} {
		altered := tx
		change(&altered)
		altered = resign(t, altered, privateKey)
		if _, _, err := checkTransferAgainstCoins(altered, lookup, nil); err == nil {
			t.Errorf("a transfer stating %s was accepted", name)
		}
	}
}

// Audit finding: a signature could be rewritten by anyone into another that
// is just as valid, so the same transfer could appear in a block in more
// than one form. On the launch rules there is one form.
func TestATransferHasOneForm(t *testing.T) {
	tx, privateKey, _, lookup := launchTransfer(t)
	parts := strings.Split(tx.Signature, "|")
	if len(parts) != 2 || len(parts[0]) != 64 || len(parts[1]) != 64 || tx.Signature != strings.ToLower(tx.Signature) {
		t.Fatalf("the wallet's signature is not in the fixed form: %q", tx.Signature)
	}
	s, _ := new(big.Int).SetString(parts[1], 16)
	order := elliptic.P256().Params().N
	if s.Cmp(new(big.Int).Rsh(order, 1)) > 0 {
		t.Fatal("the wallet signed with the high form of the signature")
	}
	highS := new(big.Int).Sub(order, s)

	rewritten := map[string]func(*Transaction){
		"the mirror-image signature": func(tx *Transaction) { tx.Signature = parts[0] + "|" + padHex64(highS) },
		"a signature in capitals":    func(tx *Transaction) { tx.Signature = strings.ToUpper(tx.Signature) },
		"a signature with a leading zero added": func(tx *Transaction) {
			tx.Signature = "0" + parts[0] + "|" + parts[1]
		},
	}
	for name, change := range rewritten {
		altered := tx
		change(&altered)
		if _, _, err := checkTransferAgainstCoins(altered, lookup, nil); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// These change what is signed, so they are signed again; the form is
	// still refused.
	resigned := map[string]func(*Transaction){
		"a note in the sender field": func(tx *Transaction) { tx.Witness = "hello from me" },
		"a public key in capitals":   func(tx *Transaction) { tx.PublicKey = strings.ToUpper(tx.PublicKey) },
	}
	for name, change := range resigned {
		altered := tx
		change(&altered)
		altered = resign(t, altered, privateKey)
		if _, _, err := checkTransferAgainstCoins(altered, lookup, nil); err == nil {
			t.Errorf("a transfer with %s was accepted", name)
		}
	}
}

// The first test network keeps its looser rules: its old transfers must stay valid.
func TestTestnet1TransfersAreUnchanged(t *testing.T) {
	onNetwork(t, "testnet-1")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	coins := map[string]UTXORecord{"TX_A_0": {Amount: UnitsAmount(cvn("50")), Recipient: owner}}
	lookup := func(key string) (UTXORecord, bool) { record, ok := coins[key]; return record, ok }
	tx, err := BuildSignedTransfer(privateKey, publicKey, "CVN_someone", cvn("10"), cvn("0.5"), []SpendableCoin{{TxID: "TX_A", Index: 0, Amount: UnitsAmount(cvn("50"))}})
	if err != nil {
		t.Fatal(err)
	}
	if tx.Inputs[0].AmountUnits != 0 {
		t.Error("a testnet-1 transfer gained a field its format does not have")
	}
	if fee, _, err := checkTransferAgainstCoins(tx, lookup, nil); err != nil || fee != cvn("0.5") {
		t.Fatalf("a testnet-1 transfer: fee %d, err %v", fee, err)
	}
	// A signature in the short form older wallets produced is still accepted there.
	parts := strings.Split(tx.Signature, "|")
	tx.Signature = strings.TrimLeft(parts[0], "0") + "|" + strings.TrimLeft(parts[1], "0")
	if _, _, err := checkTransferAgainstCoins(tx, lookup, nil); err != nil {
		t.Errorf("testnet-1 refused a signature without leading zeros: %v", err)
	}
}
