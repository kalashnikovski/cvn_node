package node

import (
	"testing"
	"time"
)

// The miner asks which block is newest without reading the block. The short
// answer must always be the same as the long one.
func TestTheShortTipAnswerMatchesTheBlock(t *testing.T) {
	startTestChain(t, "devnet")
	same := func(when string) {
		t.Helper()
		tip := GetLatestBlockInternal()
		if height, hash := latestTipID(); height != tip.Index || hash != tip.Hash {
			t.Fatalf("%s: the short answer is #%d %.16s and the block is #%d %.16s", when, height, hash, tip.Index, tip.Hash)
		}
	}
	same("on the first block")
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 6; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, testAddress("m"), nil)
		same("after a new block")
	}
	// Blocks taken off the top: the answer follows the chain down.
	StateMutex.Lock()
	rollbackChainToHeightLocked(3)
	StateMutex.Unlock()
	same("after blocks were removed")
	extendTestChain(t, timestamp+500, testAddress("other"), nil)
	same("after a different block took a removed one's place")
}

// With a full block at the tip the short answer has to stay quick, or mining
// on top of a full block slows to a crawl (found by the multi-node test of
// full blocks, 19 November 2026).
func TestTheShortTipAnswerIsQuickOnAFullBlock(t *testing.T) {
	startTestChain(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 80; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, owner, nil)
	}
	// One coin each, cut into fifty: about a block's worth of bytes.
	var transfers []Transaction
	for _, coin := range ListSpendableCoins(owner) {
		if len(transfers) == 70 {
			break
		}
		each := (coin.Amount.Units() - 100000) / int64(TxMaxOutputs)
		outputs := make([]UTXOOutput, TxMaxOutputs)
		for p := range outputs {
			outputs[p] = UTXOOutput{Recipient: owner, Amount: UnitsAmount(each)}
		}
		tx, err := signTransfer(privateKey, publicKey, owner, []UTXOInput{walletInput(coin)}, outputs, coin.Amount.Units()-each*int64(TxMaxOutputs))
		if err != nil {
			t.Fatal(err)
		}
		transfers = append(transfers, tx)
	}
	extendTestChain(t, timestamp+45, owner, transfers)
	tip := GetLatestBlockInternal()
	if size := blockSizeBytes(tip); size < 250000 {
		t.Fatalf("the test block is only %d bytes", size)
	}

	const asks = 300
	began := time.Now()
	for i := 0; i < asks; i++ {
		if height, hash := latestTipID(); height != tip.Index || hash != tip.Hash {
			t.Fatal("the short answer is wrong on a full block")
		}
	}
	short := time.Since(began)
	began = time.Now()
	for i := 0; i < asks; i++ {
		GetLatestBlockInternal()
	}
	long := time.Since(began)
	t.Logf("on a %d-byte block: %v per ask by identifier, %v per ask by reading the block", blockSizeBytes(tip), short/asks, long/asks)
	if short*10 > long {
		t.Errorf("asking by identifier took %v for %d asks and reading the block took %v: it should be many times quicker", short, asks, long)
	}
}
