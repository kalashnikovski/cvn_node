package node

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"go.etcd.io/bbolt"
)

// A BLOCK'S REWARD MAY PAY SEVERAL ADDRESSES
//
// Up to RewardMaxOutputs of them, each once, and together no more than the
// block may pay. A pool uses this to pay its miners in the block itself.

// mineTestBlockPaying builds a block on the current tip whose reward has the
// outputs given, without saving it. It is mineTestBlock with the reward's
// outputs chosen by the test.
func mineTestBlockPaying(t *testing.T, timestamp int64, miner string, outputs []UTXOOutput, transfers []Transaction) Block {
	t.Helper()
	tip := GetLatestBlockInternal()
	index := tip.Index + 1
	difficulty := 2
	if index > 10 || IsHeartbeatHeight(index) {
		difficulty = CalculateNextDifficulty(tip, timestamp)
	}
	testCoinbaseCounter++
	coinbase := Transaction{
		Version: 1, ID: fmt.Sprintf("TX_COINBASE_H%d_%d", index, testCoinbaseCounter), Witness: miner,
		PublicKey: CoinbasePublicKeyMarker, Signature: CoinbaseSignatureMarker, Timestamp: timestamp,
		Inputs: []UTXOInput{}, Outputs: outputs,
	}
	block := Block{
		Version: 1, Index: index, Timestamp: timestamp, PrevHash: tip.Hash, Difficulty: difficulty,
		Transactions: append([]Transaction{coinbase}, transfers...),
		UncleHash:    emptyUncleHash, UncleMiner: emptyUncleMiner,
	}
	digest := BlockTransactionsDigest(index, block.Transactions)
	for nonce := int64(0); ; nonce++ {
		block.Nonce = nonce
		block.Hash = CalculateHashWithTxDigest(block, digest)
		if BlockProofValid(block, difficulty) {
			return block
		}
	}
}

// allowanceNow is what the next block may pay when it carries no transfers.
func allowanceNow() int64 {
	return accountForBlock(GetLatestBlockInternal().Index+1, 0, VaultBalanceUnits()).MinerAllowanceUnits()
}

// equalParts pays `count` addresses the same amount, and the first of them
// whatever does not divide, so that the parts are exactly `units`.
func equalParts(units int64, count int) []UTXOOutput {
	outputs := make([]UTXOOutput, count)
	for i := range outputs {
		outputs[i] = UTXOOutput{Recipient: testAddress(fmt.Sprintf("paid by the block %d", i)), Amount: UnitsAmount(units / int64(count))}
	}
	outputs[0].Amount = UnitsAmount(units/int64(count) + units%int64(count))
	return outputs
}

// Fifty addresses and no more; each once; together the reward and not a
// unit over; each part a coin in its own right.
func TestABlocksRewardMayPayFiftyAddresses(t *testing.T) {
	startTestChain(t, "devnet")
	timestamp := Params.GenesisTimestamp + 100
	miner := testAddress("the pool")
	allowed := allowanceNow()
	if RewardMaxOutputs != 50 || allowed != cvn("50") {
		t.Fatalf("a reward may pay %d addresses and the first block %s CVN", RewardMaxOutputs, FormatUnits(allowed))
	}

	refused := func(what string, outputs []UTXOOutput) {
		t.Helper()
		if block := mineTestBlockPaying(t, timestamp, miner, outputs, nil); ValidateBlockSignatures(block) {
			t.Errorf("a block whose reward %s was accepted", what)
		}
	}
	refused("pays fifty-one addresses", equalParts(allowed, 51))
	refused("pays a unit more than the block may, in fifty parts", append(equalParts(allowed, 49), UTXOOutput{Recipient: testAddress("one more"), Amount: UnitsAmount(minimumOutputUnits())}))
	twice := equalParts(allowed, 3)
	twice[2].Recipient = twice[0].Recipient
	refused("pays one address twice", twice)
	tooSmall := equalParts(allowed-minimumOutputUnits()+1, 2)
	refused("has a part too small to be a coin", append(tooSmall, UTXOOutput{Recipient: testAddress("a crumb"), Amount: UnitsAmount(minimumOutputUnits() - 1)}))
	refused("has a part of nothing", append(equalParts(allowed, 2), UTXOOutput{Recipient: testAddress("nothing"), Amount: UnitsAmount(0)}))
	refused("pays something that is not an address", append(equalParts(allowed-cvn("1"), 2), UTXOOutput{Recipient: "CVN_not_an_address", Amount: UnitsAmount(cvn("1"))}))

	// Fifty is accepted, stored, and every one of the fifty holds its part.
	fifty := equalParts(allowed, RewardMaxOutputs)
	block := mineTestBlockPaying(t, timestamp, miner, fifty, nil)
	if !ValidateBlockSignatures(block) || !SaveBlockToStorageInternal(block) {
		t.Fatal("a block whose reward pays fifty addresses was refused")
	}
	var held int64
	for i, output := range fifty {
		if balanceOf(output.Recipient) != output.Amount.Units() {
			t.Errorf("address %d of the fifty holds %s CVN, want %s", i, FormatUnits(balanceOf(output.Recipient)), output.Amount)
		}
		held += balanceOf(output.Recipient)
	}
	if held != allowed {
		t.Errorf("the fifty hold %s CVN together; the block paid %s", FormatUnits(held), FormatUnits(allowed))
	}
	// The room a miner leaves for its header and reward holds such a reward.
	if size := len(blockHeaderEncoding(block, emptyUncleHash)) + transactionSizeBytes(block.Transactions[0]); size > blockRewardReserveBytes {
		t.Errorf("a header and a reward paying fifty addresses take %d bytes; a miner leaves %d", size, blockRewardReserveBytes)
	}
	if blockSizeBytes(block)+blockTransferBudgetBytes() > Params.MaxBlockBytes {
		t.Errorf("a block with this reward and as many transfers as a miner puts in would be over the limit")
	}
}

// A reward paid to three addresses: each holds a coin of its own, which it
// can spend like any other; what the reward left unpaid went to the vault;
// and the chain's coins add up to exactly what has been issued.
func TestCoinsFromARewardInPartsAreOrdinaryCoins(t *testing.T) {
	startTestChain(t, "devnet")
	timestamp := Params.GenesisTimestamp + 100
	alicePrivate, alicePublic, alice, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	alice = strings.TrimSpace(alice)
	bob, carol, dave := testAddress("bob"), testAddress("carol"), testAddress("dave")

	// Half a coin less than the block may pay is left unpaid.
	allowed := allowanceNow()
	vaultBefore := VaultBalanceUnits()
	outputs := []UTXOOutput{
		{Recipient: alice, Amount: UnitsAmount(cvn("30"))},
		{Recipient: bob, Amount: UnitsAmount(cvn("15"))},
		{Recipient: carol, Amount: UnitsAmount(allowed - cvn("45.5"))},
	}
	block := mineTestBlockPaying(t, timestamp, testAddress("the pool"), outputs, nil)
	if !ValidateBlockSignatures(block) || !SaveBlockToStorageInternal(block) {
		t.Fatal("a block whose reward pays three addresses was refused")
	}
	if balanceOf(alice) != cvn("30") || balanceOf(bob) != cvn("15") || balanceOf(carol) != allowed-cvn("45.5") || balanceOf(testAddress("the pool")) != 0 {
		t.Errorf("after the block: Alice %s, Bob %s, Carol %s, the pool %s", FormatUnits(balanceOf(alice)), FormatUnits(balanceOf(bob)),
			FormatUnits(balanceOf(carol)), FormatUnits(balanceOf(testAddress("the pool"))))
	}
	if gained := VaultBalanceUnits() - vaultBefore; gained < cvn("0.5") {
		t.Errorf("half a coin was left unpaid and the vault gained %s", FormatUnits(gained))
	}
	// Each part is a coin named by its place in the reward.
	rewardID := block.Transactions[0].ID
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		lookup := bucketLookup(tx.Bucket(utxoBucket))
		for index, output := range outputs {
			coin, found := lookup(utxoKey(rewardID, index))
			if !found || strings.TrimSpace(coin.Recipient) != output.Recipient || coin.Amount.Units() != output.Amount.Units() {
				t.Errorf("part %d of the reward is not a coin of %s for %s", index, output.Amount, output.Recipient)
			}
		}
		return nil
	})

	// Alice spends hers in the next block.
	coins := ListSpendableCoins(alice)
	if len(coins) != 1 || coins[0].TxID != rewardID || coins[0].Index != 0 {
		t.Fatalf("Alice's coins: %+v", coins)
	}
	transfer, err := BuildSignedTransfer(alicePrivate, alicePublic, dave, cvn("10"), cvn("0.01"), coins)
	if err != nil {
		t.Fatal(err)
	}
	extendTestChain(t, timestamp+45, testAddress("another miner"), []Transaction{transfer})
	if balanceOf(dave) != cvn("10") || balanceOf(alice) != cvn("19.99") || balanceOf(bob) != cvn("15") {
		t.Errorf("after Alice's transfer: Dave %s, Alice %s, Bob %s", FormatUnits(balanceOf(dave)), FormatUnits(balanceOf(alice)), FormatUnits(balanceOf(bob)))
	}
	// A copy of the chain is checked to hold exactly what has been issued.
	if _, report, err := writeBackup(filepath.Join(t.TempDir(), "backups"), 0); err != nil {
		t.Errorf("the chain does not add up after a reward in parts: %v", err)
	} else if report.Height != 2 {
		t.Errorf("the copy checked ends at block %d", report.Height)
	}
}
