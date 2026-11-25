package node

import (
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// TRANSFERS THAT PAY A FEE MAY FILL AT MOST FOUR FIFTHS OF A BLOCK
//
// A rule of the chain since the restart of November 2026. In any block at
// most 400 transfers may pay a fee (four fifths of the 500 transaction
// slots), and the transfers that pay may together measure at most four
// fifths of the block size limit (240,000 of 300,000 bytes). The last fifth
// can hold only transfers that pay nothing, or stay empty, so a miner gains
// nothing in fees by leaving free transfers out.
//
// The figures in these tests are written out. They are not asked of the
// code, which would agree with whatever it said.

const (
	testPayingTransfersLimit = 400
	testPayingBytesLimit     = 240000
)

// shareWallet is a wallet the tests of this rule spend from.
type shareWallet struct{ private, public, address string }

// spend builds a transfer of one whole coin back to the wallet as `pieces`
// equal coins, paying exactly feeUnits. What does not divide goes into the
// last piece.
func (w shareWallet) spend(t *testing.T, coin SpendableCoin, pieces int, feeUnits int64) Transaction {
	t.Helper()
	each := (coin.Amount.Units() - feeUnits) / int64(pieces)
	outputs := make([]UTXOOutput, pieces)
	for i := range outputs {
		outputs[i] = UTXOOutput{Recipient: w.address, Amount: UnitsAmount(each)}
	}
	outputs[pieces-1].Amount = UnitsAmount(coin.Amount.Units() - feeUnits - each*int64(pieces-1))
	tx, err := signTransfer(w.private, w.public, w.address, []UTXOInput{walletInput(coin)}, outputs, feeUnits)
	if err != nil {
		t.Fatal(err)
	}
	if tx.FreeWillOffering.Units() != feeUnits {
		t.Fatalf("a transfer built to pay %d units states a fee of %d", feeUnits, tx.FreeWillOffering.Units())
	}
	return tx
}

// chainWithCoins starts a devnet test chain and mines `rewards` blocks to a
// new wallet, which then holds that many coins of 50 CVN.
func chainWithCoins(t *testing.T, rewards int) (shareWallet, *int64) {
	t.Helper()
	startTestChain(t, "devnet")
	private, public, address, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < rewards; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, address, nil)
	}
	return shareWallet{private, public, address}, &timestamp
}

// chainWithSmallCoins gives a wallet ten coins of 50 CVN and cuts each into
// fifty, in one block, so that it holds 500 coins of 1 CVN: enough for a
// block with every transfer slot used.
func chainWithSmallCoins(t *testing.T) (shareWallet, *int64, []SpendableCoin) {
	t.Helper()
	wallet, timestamp := chainWithCoins(t, 10)
	var cuts []Transaction
	for _, coin := range ListSpendableCoins(wallet.address) {
		cuts = append(cuts, wallet.spend(t, coin, TxMaxOutputs, 0))
	}
	*timestamp += 45
	extendTestChain(t, *timestamp, testAddress("a miner"), cuts)
	pieces := ListSpendableCoins(wallet.address)
	if len(pieces) != 500 {
		t.Fatalf("the wallet holds %d small coins, want 500", len(pieces))
	}
	return wallet, timestamp, pieces
}

// rulesVerdict is what the rules say of a block that would follow the newest
// block: nil if its transactions are valid, and the reason if they are not.
func rulesVerdict(t *testing.T, block Block) error {
	t.Helper()
	var verdict error
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		vaultBefore, err := readVaultUnits(dbTx, block.Index-1, block.PrevHash)
		if err != nil {
			t.Fatalf("the vault's record could not be read: %v", err)
		}
		_, _, verdict = ValidateBlockTransactions(block, bucketLookup(dbTx.Bucket(utxoBucket)), vaultBefore)
		return nil
	})
	return verdict
}

// refusedForPayingTooMuch fails the test unless the block is refused, for
// the reason that too much of it pays a fee, by the check a neighbour's
// block gets and separately by storing. (Storing checks again by itself: it
// is how blocks are put back after a change of chain that failed.)
func refusedForPayingTooMuch(t *testing.T, block Block, what string) {
	t.Helper()
	verdict := rulesVerdict(t, block)
	if verdict == nil {
		t.Errorf("%s: the rules found nothing wrong with it", what)
	} else if !strings.Contains(verdict.Error(), "pay a fee") {
		t.Errorf("%s: refused, but for another reason: %v", what, verdict)
	}
	if ValidateBlockSignatures(block) {
		t.Errorf("%s: it passed the check", what)
	}
	if SaveBlockToStorageInternal(block) {
		t.Fatalf("%s: it was stored", what)
	}
}

// payingPart counts the transfers in a block's contents that pay a fee, and
// measures them.
func payingPart(transfers []Transaction) (count, size int) {
	for _, tx := range transfers {
		if tx.FreeWillOffering.Units() > 0 {
			count++
			size += transactionSizeBytes(tx)
		}
	}
	return count, size
}

func TestABlockMayHoldAtMost400PayingTransfers(t *testing.T) {
	wallet, timestamp, pieces := chainWithSmallCoins(t)
	var paying, free []Transaction
	for _, piece := range pieces[:401] {
		paying = append(paying, wallet.spend(t, piece, 1, 1)) // a fee of one unit
	}
	for _, piece := range pieces[401:] {
		free = append(free, wallet.spend(t, piece, 1, 0))
	}
	if len(paying) != 401 || len(free) != 99 {
		t.Fatalf("set-up: %d paying and %d free transfers", len(paying), len(free))
	}
	*timestamp += 45

	// 401 transfers that each pay one unit. In bytes they are far under the
	// limit, so it is the count that refuses them.
	over := mineTestBlock(t, *timestamp, testAddress("miner"), paying, 0)
	if _, size := payingPart(over.Transactions); size >= testPayingBytesLimit || blockSizeBytes(over) > Params.MaxBlockBytes {
		t.Fatalf("set-up: the block is too large to test the count alone (%d paying bytes)", size)
	}
	refusedForPayingTooMuch(t, over, "a block with 401 transfers that pay a fee")

	// The very same block with its last transfer paying nothing: a fee of
	// one unit pays, and a fee of none does not.
	oneFree := append(append([]Transaction{}, paying[:400]...), wallet.spend(t, pieces[400], 1, 0))
	if verdict := rulesVerdict(t, mineTestBlock(t, *timestamp, testAddress("miner"), oneFree, 0)); verdict != nil {
		t.Errorf("a block with 400 paying transfers and one free was refused: %v", verdict)
	}

	// 400 that pay and 99 that do not: every slot a block has, and taken.
	atLimit := mineTestBlock(t, *timestamp, testAddress("miner"), append(append([]Transaction{}, paying[:400]...), free...), 0)
	if len(atLimit.Transactions) != TxMaxPerBlock {
		t.Fatalf("set-up: the block holds %d transactions, want all %d slots used", len(atLimit.Transactions), TxMaxPerBlock)
	}
	if !ValidateBlockSignatures(atLimit) || !SaveBlockToStorageInternal(atLimit) {
		t.Fatalf("a block with 400 paying transfers and 99 free ones was refused: %v", rulesVerdict(t, atLimit))
	}
}

func TestPayingTransfersMayFillAtMostFourFifthsOfTheBytes(t *testing.T) {
	wallet, timestamp := chainWithCoins(t, 100)
	if Params.MaxBlockBytes != 300000 {
		t.Fatalf("the block size limit is %d bytes, and this test was written for 300,000", Params.MaxBlockBytes)
	}
	coins := ListSpendableCoins(wallet.address)

	// Large transfers, one coin cut into fifty, each paying one unit, until
	// they measure more than 240,000 bytes together.
	var paying []Transaction
	payingBytes, used := 0, 0
	for payingBytes <= testPayingBytesLimit {
		tx := wallet.spend(t, coins[used], TxMaxOutputs, 1)
		paying = append(paying, tx)
		payingBytes += transactionSizeBytes(tx)
		used++
	}
	*timestamp += 45
	over := mineTestBlock(t, *timestamp, testAddress("miner"), paying, 0)
	if size := blockSizeBytes(over); size > Params.MaxBlockBytes || len(paying) >= testPayingTransfersLimit {
		t.Fatalf("set-up: the block is %d bytes with %d paying transfers; it must be within the size limit and the count for the bytes alone to refuse it", size, len(paying))
	}
	refusedForPayingTooMuch(t, over, fmt.Sprintf("a block whose paying transfers measure %d bytes", payingBytes))

	// One large transfer fewer, topped up with small paying transfers to
	// within one small transfer of the limit...
	within := append([]Transaction{}, paying[:len(paying)-1]...)
	_, withinBytes := payingPart(within)
	small := coins[used+20:] // coins kept for small transfers
	for i := 0; i < len(small); i++ {
		tx := wallet.spend(t, small[i], 1, 1)
		if withinBytes+transactionSizeBytes(tx) > testPayingBytesLimit {
			break
		}
		within = append(within, tx)
		withinBytes += transactionSizeBytes(tx)
	}
	if short := testPayingBytesLimit - withinBytes; short > 1000 {
		t.Fatalf("set-up: the paying transfers are %d bytes short of the limit; they should be within one small transfer of it", short)
	}
	// ...a reward that pays fifty addresses, the largest a reward can be.
	// It is not a transfer: were it counted with the paying ones, this block
	// would be over the limit.
	reward := equalParts(cvn("50"), RewardMaxOutputs)
	rewardBytes := blockSizeBytes(mineTestBlockPaying(t, *timestamp, testAddress("miner"), reward, nil))
	if withinBytes+rewardBytes <= testPayingBytesLimit {
		t.Fatalf("set-up: the paying transfers (%d bytes) and the reward (%d) do not pass the limit together", withinBytes, rewardBytes)
	}
	// ...and free large transfers on top, into the last fifth, up to the
	// size limit of the block.
	contents := within
	total := rewardBytes + withinBytes
	for i := 0; i < 20; i++ {
		tx := wallet.spend(t, coins[used+i], TxMaxOutputs, 0)
		if total+transactionSizeBytes(tx) > Params.MaxBlockBytes {
			break
		}
		contents = append(contents, tx)
		total += transactionSizeBytes(tx)
	}
	full := mineTestBlockPaying(t, *timestamp, testAddress("miner"), reward, contents)
	if size := blockSizeBytes(full); size <= 290000 || size > Params.MaxBlockBytes {
		t.Fatalf("set-up: the full block is %d bytes; it should be just under the limit of %d", size, Params.MaxBlockBytes)
	}
	if !ValidateBlockSignatures(full) || !SaveBlockToStorageInternal(full) {
		t.Fatalf("a full block with %d bytes of paying transfers (limit %d) and free ones in the rest was refused: %v", withinBytes, testPayingBytesLimit, rulesVerdict(t, full))
	}
}

// The limit in bytes is exact: paying transfers of exactly four fifths of
// the size limit are taken, and one byte more is not.
func TestThePayingBytesLimitIsExactToTheByte(t *testing.T) {
	wallet, timestamp := chainWithCoins(t, 13)
	coins := ListSpendableCoins(wallet.address)
	// A free transfer stands first in the block and the paying ones after
	// it. It is in neither figure: a program that counted it with the
	// paying ones would refuse the exact fit below.
	contents := []Transaction{wallet.spend(t, coins[0], 1, 0)}
	for _, coin := range coins[1:] {
		contents = append(contents, wallet.spend(t, coin, 1, 1))
	}
	paid, payingBytes := payingPart(contents)
	if paid != 12 {
		t.Fatalf("set-up: %d paying transfers, want 12", paid)
	}
	*timestamp += 45
	block := mineTestBlock(t, *timestamp, testAddress("miner"), contents, 0)

	// sizeLimitGiving finds a block size limit whose four fifths (the limit
	// less a fifth of it, rounded down) is exactly the figure wanted.
	sizeLimitGiving := func(fourFifths int) int {
		for limit := fourFifths; limit <= 2*fourFifths; limit++ {
			if limit-limit/5 == fourFifths {
				return limit
			}
		}
		t.Fatalf("no size limit has four fifths of %d", fourFifths)
		return 0
	}
	Params.MaxBlockBytes = sizeLimitGiving(payingBytes)
	if blockSizeBytes(block) > Params.MaxBlockBytes {
		t.Fatalf("set-up: the block is %d bytes, over the size limit of %d; it must be the paying part that is at its limit", blockSizeBytes(block), Params.MaxBlockBytes)
	}
	if verdict := rulesVerdict(t, block); verdict != nil {
		t.Errorf("paying transfers of exactly four fifths of the size limit (%d of %d bytes) were refused: %v", payingBytes, Params.MaxBlockBytes, verdict)
	}
	Params.MaxBlockBytes = sizeLimitGiving(payingBytes - 1)
	if blockSizeBytes(block) > Params.MaxBlockBytes {
		t.Fatalf("set-up: the block is %d bytes, over the size limit of %d", blockSizeBytes(block), Params.MaxBlockBytes)
	}
	refusedForPayingTooMuch(t, block, fmt.Sprintf("paying transfers of %d bytes where four fifths of the size limit is %d", payingBytes, payingBytes-1))
}

// What this program's own miner puts in a block when far more transfers are
// waiting than fit: never more paying ones than the rule allows, and the
// block it builds is taken.
func TestTheMinersOwnCrowdedBlockKeepsTheRuleAndIsTaken(t *testing.T) {
	wallet, timestamp, pieces := chainWithSmallCoins(t)
	for i, piece := range pieces {
		fee := int64(1 + i) // 450 that pay, each a different fee
		if i >= 450 {
			fee = 0 // and 50 that do not
		}
		if err := AdmitTransactionToMempool(wallet.spend(t, piece, 1, fee)); err != nil {
			t.Fatalf("a transfer was not taken: %v", err)
		}
	}
	*timestamp += 45
	contents := buildBlockContents(GetLatestBlockInternal(), testAddress("miner"), *timestamp, "test")
	paying, _ := payingPart(contents[1:])
	if paying != testPayingTransfersLimit || len(contents)-1-paying != 50 {
		t.Fatalf("with 450 paying and 50 free transfers waiting the miner's block holds %d paying and %d free; want 400 and 50", paying, len(contents)-1-paying)
	}
	// The 400 that pay the most.
	for _, tx := range contents[1:] {
		if fee := tx.FreeWillOffering.Units(); fee > 0 && fee <= 50 {
			t.Fatalf("a transfer paying %d units was taken ahead of ones paying more", fee)
		}
	}
	block := mineTestBlockPaying(t, *timestamp, testAddress("miner"), contents[0].Outputs, contents[1:])
	if !ValidateBlockSignatures(block) || !SaveBlockToStorageInternal(block) {
		t.Fatalf("the block this program's miner built was refused: %v", rulesVerdict(t, block))
	}
}

// The same by bytes: with more large paying transfers waiting than four
// fifths of a block, the miner takes as many as the rule allows, fills the
// rest with free ones, and the block is taken.
func TestTheMinersOwnFullBlockKeepsTheRuleByBytes(t *testing.T) {
	wallet, timestamp := chainWithCoins(t, 90)
	coins := ListSpendableCoins(wallet.address)
	smallestPaying := 0
	for i, coin := range coins[:85] {
		fee := int64(1000 + i) // 70 large transfers that pay
		if i >= 70 {
			fee = 0 // and 15 large ones that do not
		}
		tx := wallet.spend(t, coin, TxMaxOutputs, fee)
		if size := transactionSizeBytes(tx); fee > 0 && (smallestPaying == 0 || size < smallestPaying) {
			smallestPaying = size
		}
		if err := AdmitTransactionToMempool(tx); err != nil {
			t.Fatalf("a transfer was not taken: %v", err)
		}
	}
	*timestamp += 45
	contents := buildBlockContents(GetLatestBlockInternal(), testAddress("miner"), *timestamp, "test")
	paying, payingBytes := payingPart(contents[1:])
	if payingBytes > testPayingBytesLimit {
		t.Fatalf("the miner's block holds %d bytes of paying transfers, over the limit of %d", payingBytes, testPayingBytesLimit)
	}
	if payingBytes+smallestPaying <= testPayingBytesLimit {
		t.Errorf("the miner's block holds %d paying transfers (%d bytes) and one more would have fitted under the limit of %d", paying, payingBytes, testPayingBytesLimit)
	}
	if free := len(contents) - 1 - paying; free == 0 {
		t.Error("the miner's block holds no free transfer, though the last fifth had room and free ones were waiting")
	}
	block := mineTestBlockPaying(t, *timestamp, testAddress("miner"), contents[0].Outputs, contents[1:])
	if size := blockSizeBytes(block); size <= testPayingBytesLimit {
		t.Errorf("the miner's block is only %d bytes; with free transfers waiting it should be larger than the paying part may be", size)
	}
	if !ValidateBlockSignatures(block) || !SaveBlockToStorageInternal(block) {
		t.Fatalf("the block this program's miner built was refused: %v", rulesVerdict(t, block))
	}
}

// Where a transfer stands in a block makes no difference: free transfers
// ahead of the paying ones are not counted with them.
func TestFreeTransfersAheadOfPayingOnesAreNotCounted(t *testing.T) {
	wallet, timestamp, pieces := chainWithSmallCoins(t)
	var free, paying []Transaction
	for _, piece := range pieces[:99] {
		free = append(free, wallet.spend(t, piece, 1, 0))
	}
	for _, piece := range pieces[99:] {
		paying = append(paying, wallet.spend(t, piece, 1, 1))
	}
	if len(free) != 99 || len(paying) != 401 {
		t.Fatalf("set-up: %d free and %d paying transfers", len(free), len(paying))
	}
	*timestamp += 45
	// 98 free, then 401 that pay: within a block's slots, and refused for
	// the paying ones.
	over := mineTestBlock(t, *timestamp, testAddress("miner"), append(append([]Transaction{}, free[:98]...), paying...), 0)
	if len(over.Transactions) != TxMaxPerBlock {
		t.Fatalf("set-up: the block holds %d transactions, want %d", len(over.Transactions), TxMaxPerBlock)
	}
	refusedForPayingTooMuch(t, over, "a block with 98 free transfers and then 401 that pay")
	// 99 free, then 400 that pay: taken.
	atLimit := mineTestBlock(t, *timestamp, testAddress("miner"), append(append([]Transaction{}, free...), paying[:400]...), 0)
	if !ValidateBlockSignatures(atLimit) || !SaveBlockToStorageInternal(atLimit) {
		t.Fatalf("a block with 99 free transfers and then 400 that pay was refused: %v", rulesVerdict(t, atLimit))
	}
}

// On a network with no limit on the size of a block there is no limit in
// bytes on the paying part either, and the limit of 400 still holds. (Only
// tests run such a network; the rule must still be whole there.)
func TestWithNoSizeLimitTheCountOfPayingTransfersStillHolds(t *testing.T) {
	wallet, timestamp, pieces := chainWithSmallCoins(t)
	var paying []Transaction
	for _, piece := range pieces[:401] {
		paying = append(paying, wallet.spend(t, piece, 1, 1))
	}
	*timestamp += 45
	over := mineTestBlock(t, *timestamp, testAddress("miner"), paying, 0)
	atLimit := mineTestBlock(t, *timestamp, testAddress("miner"), paying[:400], 0)
	Params.MaxBlockBytes = 0
	if verdict := rulesVerdict(t, atLimit); verdict != nil {
		t.Errorf("with no size limit, a block of 400 paying transfers was refused: %v", verdict)
	}
	refusedForPayingTooMuch(t, over, "with no size limit, a block of 401 paying transfers")

	// This program's own filling, with no size limit: 400 of 600 small
	// paying transfers, and every one of 300 large ones, which together
	// measure far more than 240,000 bytes.
	const height = 100000
	for _, lanes := range []bool{true, false} {
		small := waiting("P", 600, func(i int) int64 { return int64(1000 + i) }, height, 400)
		if chosen, _ := fillBlockFromCandidates(small, TxMaxPerBlock-1, 0, height, lanes); len(chosen) != 400 {
			t.Errorf("lanes %v, no size limit: of 600 paying transfers the block holds %d; want 400", lanes, len(chosen))
		}
		large := waiting("L", 300, func(i int) int64 { return int64(1000 + i) }, height, 2000)
		if chosen, _ := fillBlockFromCandidates(large, TxMaxPerBlock-1, 0, height, lanes); len(chosen) != 300 {
			t.Errorf("lanes %v, no size limit: of 300 large paying transfers the block holds %d; want all 300", lanes, len(chosen))
		}
	}
}

// A paying transfer too large for what is left of the paying part is passed
// over, and the smaller paying ones and the free ones behind it still go
// in, up to the limit exactly.
func TestAPayingTransferTooLargeForWhatIsLeftIsPassedOver(t *testing.T) {
	onNetwork(t, "devnet")
	const height = 100000
	// By fee, highest first: nine of 29,000 bytes, then twenty of 1,000.
	// Eight large ones are 232,000 bytes; the ninth would pass 240,000;
	// eight small ones then bring the paying part to 240,000 exactly.
	candidates := waiting("L", 9, func(i int) int64 { return int64(5000 + i) }, height, 29000)
	candidates = append(candidates, waiting("S", 20, func(i int) int64 { return int64(1000 + i) }, height, 1000)...)
	candidates = append(candidates, waiting("F", 30, noFee, height, 1000)...)
	for _, lanes := range []bool{true, false} {
		chosen, fees := fillBlockFromCandidates(candidates, TxMaxPerBlock-1, blockTransferBudgetBytes(), height, lanes)
		checkFilledBlock(t, candidates, chosen, fees, TxMaxPerBlock-1, blockTransferBudgetBytes())
		if counts := countByPrefix(chosen); counts["L"] != 8 || counts["S"] != 8 || counts["F"] != 30 {
			t.Errorf("lanes %v: the block holds %d large paying, %d small paying and %d free; want 8, 8 (240,000 bytes of paying transfers exactly) and all 30 free", lanes, counts["L"], counts["S"], counts["F"])
		}
	}
}

// Room the free lane does not use goes to paying transfers, as far as the
// rule allows and no further.
func TestPayingTransfersTakeWhatTheFreeLaneLeavesUpToTheRule(t *testing.T) {
	onNetwork(t, "devnet")
	const height = 100000
	rising := func(i int) int64 { return int64(1000 + i) }
	// By bytes: 600 paying transfers of 1,200 bytes and nothing free. The
	// paying lane is four fifths of the 294,000 bytes a miner has for
	// transfers, 235,200, which is 196 of them; the rule allows 240,000,
	// which is 200.
	crowd := waiting("P", 600, rising, height, 1200)
	if chosen, _ := fillBlockFromCandidates(crowd, TxMaxPerBlock-1, blockTransferBudgetBytes(), height, true); len(chosen) != 200 {
		t.Errorf("600 paying transfers of 1,200 bytes: the block holds %d; want 200, which is 240,000 bytes", len(chosen))
	}
	// By count, in a block with room for ten: the paying lane is eight,
	// and with nothing free waiting the other two go to paying transfers,
	// since ten is far under what the rule allows.
	if chosen, _ := fillBlockFromCandidates(waiting("P", 600, rising, height, 400), 10, 0, height, true); len(chosen) != 10 {
		t.Errorf("room for ten and only paying transfers waiting: the block holds %d; want 10", len(chosen))
	}
}

// A rival for the newest block's place that breaks the rule, and would
// otherwise win, is refused, and nothing at all is changed by the attempt:
// the newest block, every coin and the vault are as they were, and the
// chain goes on.
func TestARivalBlockThatBreaksTheRuleChangesNothing(t *testing.T) {
	wallet, timestamp, pieces := chainWithSmallCoins(t)
	var paying []Transaction
	for _, piece := range pieces[:401] {
		paying = append(paying, wallet.spend(t, piece, 1, 1))
	}
	*timestamp += 45
	// An honest block and a rival on the same parent holding 401 paying
	// transfers. Which of two blocks wins is settled by their identifiers,
	// so fresh pairs are mined until the rival is the one that would win.
	// Each pair is an even chance; sixty in a row do not fail.
	var honest, rival Block
	for tries := 0; ; tries++ {
		if tries == 60 {
			t.Fatal("could not mine a rival that would win")
		}
		honest = mineTestBlock(t, *timestamp, testAddress("honest miner"), paying[:5], 0)
		rival = mineTestBlock(t, *timestamp, testAddress("greedy miner"), paying, 0)
		if !siblingWins(honest, rival) {
			break
		}
	}
	if !ValidateBlockSignatures(honest) || !SaveBlockToStorageInternal(honest) {
		t.Fatal("the honest block was refused")
	}
	before := coinSetFingerprint()
	// The rules judge a rival against the coins as they were before the
	// block it would replace. Judged so, it is refused for the paying
	// transfers and for nothing else.
	var verdict error
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		vaultBefore, err := readVaultUnits(dbTx, rival.Index-1, rival.PrevHash)
		if err != nil {
			t.Fatalf("the vault's record could not be read: %v", err)
		}
		_, _, verdict = ValidateBlockTransactions(rival, lookupWithBlockUndone(dbTx, dbTx.Bucket(utxoBucket), honest), vaultBefore)
		return nil
	})
	if verdict == nil || !strings.Contains(verdict.Error(), "pay a fee") {
		t.Fatalf("the rival should be refused for its paying transfers; the rules said: %v", verdict)
	}
	if ValidateBlockSignatures(rival) {
		t.Error("a rival block with 401 paying transfers passed the check")
	}
	if SaveBlockToStorageInternal(rival) {
		t.Fatal("a rival block with 401 paying transfers was stored")
	}
	if tip := GetLatestBlockInternal(); tip.Hash != honest.Hash {
		t.Fatalf("the newest block is now %.16s, not the honest block", tip.Hash)
	}
	if coinSetFingerprint() != before {
		t.Fatal("the coins or the vault changed when a rule-breaking rival was offered")
	}
	if balanceOf(testAddress("greedy miner")) != 0 {
		t.Error("the miner of the refused rival holds coins")
	}
	// The chain goes on from the honest block, and the transfers the rival
	// wanted to carry are still good for a block that keeps the rule.
	*timestamp += 45
	next := mineTestBlock(t, *timestamp, testAddress("honest miner"), paying[5:400], 0)
	if !ValidateBlockSignatures(next) || !SaveBlockToStorageInternal(next) {
		t.Fatalf("the block after was refused: %v", rulesVerdict(t, next))
	}
}

// checkFilledBlock fails the test if what fillBlockFromCandidates chose
// breaks the rule, or the limits it was given.
func checkFilledBlock(t *testing.T, candidates []mempoolCandidate, chosen []Transaction, fees int64, limit, budgetBytes int) {
	t.Helper()
	byID := make(map[string]mempoolCandidate, len(candidates))
	for _, entry := range candidates {
		byID[entry.tx.ID] = entry
	}
	paying, payingBytes, totalBytes := 0, 0, 0
	var totalFees int64
	spent := make(map[string]bool)
	seen := make(map[string]bool)
	for _, tx := range chosen {
		entry, known := byID[tx.ID]
		if !known || seen[tx.ID] {
			t.Fatalf("the block holds %s, which was not waiting or is there twice", tx.ID)
		}
		seen[tx.ID] = true
		for _, record := range entry.spent {
			if spent[record.Key] {
				t.Fatalf("the block spends coin %s twice", record.Key)
			}
			spent[record.Key] = true
		}
		totalBytes += entry.size
		totalFees += entry.fee
		if entry.fee > 0 {
			paying++
			payingBytes += entry.size
		}
	}
	if paying > testPayingTransfersLimit || payingBytes > testPayingBytesLimit {
		t.Fatalf("the block holds %d paying transfers measuring %d bytes; the rule allows %d and %d", paying, payingBytes, testPayingTransfersLimit, testPayingBytesLimit)
	}
	if len(chosen) > limit || (budgetBytes > 0 && totalBytes > budgetBytes) {
		t.Fatalf("the block holds %d transfers of %d bytes; it was given room for %d and %d", len(chosen), totalBytes, limit, budgetBytes)
	}
	if fees != totalFees {
		t.Fatalf("the fees were added up as %d; the transfers chosen pay %d", fees, totalFees)
	}
}

// Made-up mempools of every mixture, with the lanes on and off, small and
// large limits: what this program chooses for a block never has more paying
// transfers, or more bytes of them, than the rule allows.
func TestThisProgramNeverFillsABlockTheRuleRefuses(t *testing.T) {
	onNetwork(t, "devnet")
	const height = 100000
	arrived := time.Unix(1_800_000_000, 0)
	random := rand.New(rand.NewSource(20261126)) // fixed, so a failure can be repeated
	for round := 0; round < 400; round++ {
		count := random.Intn(1200)
		mostlyPaying, mostlyLarge := random.Intn(3) > 0, random.Intn(3) == 0
		candidates := make([]mempoolCandidate, count)
		for i := range candidates {
			id := fmt.Sprintf("T%d-%04d", round, i)
			var fee int64
			if (mostlyPaying && random.Intn(10) > 0) || (!mostlyPaying && random.Intn(3) == 0) {
				fee = 1 + random.Int63n(100000)
			}
			size := 300 + random.Intn(400)
			if mostlyLarge || random.Intn(8) == 0 {
				size = 2000 + random.Intn(28000)
			}
			age := int64(height) - freeLaneMinAgeBlocks - 5 + int64(random.Intn(10)) // some settled, some young
			coin := "coin-of-" + id
			if i > 0 && random.Intn(50) == 0 {
				coin = candidates[i-1].spent[0].Key // now and then two transfers spend the same coin
			}
			candidates[i] = mempoolCandidate{
				tx: Transaction{ID: id}, fee: fee, size: size,
				spent:   []SpentRecord{{Key: coin, Record: UTXORecord{Age: age}}},
				arrived: arrived.Add(time.Duration(random.Intn(1000)) * time.Second),
			}
		}
		limit := []int{4, 60, TxMaxPerBlock - 1}[random.Intn(3)]
		budget := []int{0, 10000, blockTransferBudgetBytes()}[random.Intn(3)]
		lanes := round%2 == 0
		chosen, fees := fillBlockFromCandidates(candidates, limit, budget, height, lanes)
		checkFilledBlock(t, candidates, chosen, fees, limit, budget)
	}

	// The plainest case: 600 small transfers that all pay and nothing else
	// waiting. 400 go in and the rest of the block stays empty.
	for _, lanes := range []bool{true, false} {
		crowd := waiting("P", 600, func(i int) int64 { return int64(1000 + i) }, height, 400)
		chosen, fees := fillBlockFromCandidates(crowd, TxMaxPerBlock-1, blockTransferBudgetBytes(), height, lanes)
		checkFilledBlock(t, crowd, chosen, fees, TxMaxPerBlock-1, blockTransferBudgetBytes())
		if len(chosen) != testPayingTransfersLimit {
			t.Errorf("lanes %v: with 600 paying transfers waiting the block holds %d; want 400, the most that may pay", lanes, len(chosen))
		}
	}
	// And with free transfers behind them in the same list, as when lanes
	// are off: the paying ones that do not fit are passed over, and the
	// free ones are still reached.
	mixed := append(waiting("P", 600, func(i int) int64 { return int64(1000 + i) }, height, 400), waiting("F", 200, noFee, height, 400)...)
	chosen, fees := fillBlockFromCandidates(mixed, TxMaxPerBlock-1, blockTransferBudgetBytes(), height, false)
	checkFilledBlock(t, mixed, chosen, fees, TxMaxPerBlock-1, blockTransferBudgetBytes())
	if counts := countByPrefix(chosen); counts["P"] != 400 || counts["F"] != 99 {
		t.Errorf("without lanes, 600 paying and 200 free waiting: the block holds %d paying and %d free; want 400 and 99", counts["P"], counts["F"])
	}
}

// FuzzABlockFilledFromAnyMempoolKeepsTheRule gives the block filler waiting
// transfers made from whatever bytes the fuzzer invents and checks the same
// thing. Every three bytes make one transfer: what it pays, how large it is,
// and how old its coin is.
func FuzzABlockFilledFromAnyMempoolKeepsTheRule(f *testing.F) {
	saved := Params
	if err := SelectNetwork("devnet"); err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { Params = saved; applyParams() })
	smallPaying := bytes.Repeat([]byte{1, 2, 0}, 600)        // 600 small paying transfers of settled coins
	largePaying := bytes.Repeat([]byte{7, 255, 1}, 300)      // 300 large paying transfers
	mixture := bytes.Repeat([]byte{0, 9, 0, 3, 200, 1}, 400) // free and paying, small and large
	f.Add([]byte{}, true)
	f.Add(smallPaying, true)
	f.Add(smallPaying, false)
	f.Add(largePaying, true)
	f.Add(mixture, true)
	f.Add(mixture, false)
	f.Fuzz(func(t *testing.T, data []byte, lanes bool) {
		const height = 100000
		arrived := time.Unix(1_800_000_000, 0)
		var candidates []mempoolCandidate
		for i := 0; i+2 < len(data) && len(candidates) < 3000; i += 3 {
			id := fmt.Sprintf("T%05d", len(candidates))
			fee := int64(data[i] % 8) // one in eight pays nothing
			size := 250 + int(data[i+1])*110
			age := int64(height) - freeLaneMinAgeBlocks - 1 + int64(data[i+2]%2)*2 // settled or young
			coin := "coin-of-" + id
			if data[i+2] >= 250 && len(candidates) > 0 {
				coin = candidates[len(candidates)-1].spent[0].Key // spends the coin the one before it spends
			}
			candidates = append(candidates, mempoolCandidate{
				tx: Transaction{ID: id}, fee: fee, size: size,
				spent:   []SpentRecord{{Key: coin, Record: UTXORecord{Age: age}}},
				arrived: arrived.Add(time.Duration(data[i+2]) * time.Second),
			})
		}
		limit, budget := TxMaxPerBlock-1, blockTransferBudgetBytes()
		chosen, fees := fillBlockFromCandidates(candidates, limit, budget, height, lanes)
		checkFilledBlock(t, candidates, chosen, fees, limit, budget)
	})
}
