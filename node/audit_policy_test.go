package node

import (
	"strings"
	"testing"
	"time"
)

// Audit finding: a full mempool refused every new transfer, whatever it
// paid. Anyone could keep it full of fee-less transfers of dust and shut
// everyone else out at no cost.
func TestAFeeGetsIntoAFullMempool(t *testing.T) {
	savedLimit := MempoolMaxTransactions
	MempoolMaxTransactions = 3
	t.Cleanup(func() { MempoolMaxTransactions = savedLimit })
	startTestChain(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp
	for i := 0; i < 6; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, owner, nil)
	}
	coins := ListSpendableCoins(owner)
	transfer := func(coin SpendableCoin, fee string) Transaction {
		tx, err := BuildSignedTransfer(privateKey, publicKey, "CVN_someone", cvn("1"), cvn(fee), []SpendableCoin{coin})
		if err != nil {
			t.Fatal(err)
		}
		return tx
	}
	// Three fee-less transfers fill the pool.
	var free []Transaction
	for i := 0; i < 3; i++ {
		tx := transfer(coins[i], "0")
		if err := AdmitTransactionToMempool(tx); err != nil {
			t.Fatalf("free transfer %d refused: %v", i, err)
		}
		free = append(free, tx)
		time.Sleep(5 * time.Millisecond) // so that their arrival times differ
	}
	// A fourth fee-less one has no claim to a place.
	if err := AdmitTransactionToMempool(transfer(coins[3], "0")); err == nil || !strings.Contains(err.Error(), "full") {
		t.Errorf("a fee-less transfer got into a pool full of fee-less transfers: %v", err)
	}
	// One that pays does, and it takes the place of the last to arrive.
	paying := transfer(coins[4], "0.01")
	if err := AdmitTransactionToMempool(paying); err != nil {
		t.Fatalf("a transfer paying a fee was shut out of a pool full of fee-less ones: %v", err)
	}
	MempoolMatrix.RLock()
	_, payingIn := MempoolMatrix.Transactions[paying.ID]
	_, firstIn := MempoolMatrix.Transactions[free[0].ID]
	_, lastIn := MempoolMatrix.Transactions[free[2].ID]
	size := len(MempoolMatrix.Transactions)
	MempoolMatrix.RUnlock()
	if !payingIn || !firstIn || lastIn || size != 3 {
		t.Errorf("pool of %d: paying transfer in %v, longest-waiting free one kept %v, last-arrived free one kept %v", size, payingIn, firstIn, lastIn)
	}
	// A transfer paying less than everything waiting does not push one out.
	MempoolMatrix.Lock()
	for id := range MempoolMatrix.Transactions {
		mempoolEntries[id] = mempoolEntry{fee: cvn("1")}
	}
	MempoolMatrix.Unlock()
	if err := AdmitTransactionToMempool(transfer(coins[5], "0.5")); err == nil {
		t.Error("a transfer paying less than every one waiting pushed one of them out")
	}
}

// Audit finding: the wallets would send coins to anything that was not
// blank. Coins sent to text that is not an address can never be spent.
func TestWalletRefusesToSendToSomethingThatIsNotAnAddress(t *testing.T) {
	onNetwork(t, "devnet")
	privateKey, publicKey, real, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if !looksLikeWalletAddress(real) {
		t.Fatalf("a freshly made address was not recognised: %s", real)
	}
	for _, text := range []string{
		"", "CVN_", "CVN_miner", "hello", real[:len(real)-1], real + "0", strings.ToUpper(real),
		"cvn_" + real[4:], real[:10] + "g" + real[11:], " " + real, "0x" + real[4:],
	} {
		if looksLikeWalletAddress(text) {
			t.Errorf("%q was taken for a wallet address", text)
		}
		// The wallet trims stray spaces before judging, which is harmless.
		if strings.TrimSpace(text) == "" || looksLikeWalletAddress(strings.TrimSpace(text)) {
			continue
		}
		// Refused before any node is asked for anything.
		if _, err := SendTransferFromWallet(privateKey, publicKey, text, cvn("1"), 0); err == nil || !strings.Contains(err.Error(), "not a CVN wallet address") {
			t.Errorf("the wallet did not refuse to send to %q: %v", text, err)
		}
	}
}

// Audit finding: every request for the list of balances recounted every coin.
func TestBalanceRecountsAreSpacedOut(t *testing.T) {
	startTestChain(t, "devnet")
	lastBalanceRecount = 0
	if !recountBalancesIfStale() {
		t.Fatal("the first request should recount")
	}
	for i := 0; i < 5; i++ {
		if recountBalancesIfStale() {
			t.Fatal("a request moments later recounted every coin again")
		}
	}
	lastBalanceRecount = time.Now().Unix() - int64(balanceRecountInterval/time.Second) - 1
	if !recountBalancesIfStale() {
		t.Error("after the interval a request should recount")
	}
}
