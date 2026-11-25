package node

import (
	"sort"
	"testing"

	"go.etcd.io/bbolt"
)

// WHO RECEIVES THE FEES
//
// Every fee in a block is its miner's. No part of a fee goes to the vault by
// rule. (Decided 25 November 2026. Before that a fifth of every fee went to
// the vault while new coins were still being issued.) A miner may still
// claim less than a block allows, and what it leaves goes to the vault: that
// is the miner's choice, not the rule's.
//
// The amounts in these tests are written out as figures. A test that asked
// accountForBlock what a block may pay would agree with whatever it said.

// vaultNow reads the vault's balance after the newest block straight from
// its record, and fails the test if the record cannot be read.
// (VaultBalanceUnits answers nought when a read fails, so a test that
// expects nought must not lean on it.)
func vaultNow(t *testing.T) int64 {
	t.Helper()
	tip := GetLatestBlockInternal()
	if tip.Hash == "" {
		t.Fatal("the newest block could not be read")
	}
	var units int64
	err := GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		var readErr error
		units, readErr = readVaultUnits(dbTx, tip.Index, tip.Hash)
		return readErr
	})
	if err != nil {
		t.Fatalf("the vault's record for block #%d could not be read: %v", tip.Index, err)
	}
	return units
}

// feeChain starts a chain with a vault that pays a tenth of itself each
// block and no sweep, and gives one wallet four blocks' rewards as four
// coins of 50 CVN. Each call of payWithFee spends the next of those coins:
// 20 CVN to a recipient, the fee named, and the rest back to the wallet.
func feeChain(t *testing.T) (timestamp *int64, payWithFee func(feeUnits int64) Transaction) {
	t.Helper()
	startVaultChain(t)
	privateKey, publicKey, sender, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	at := Params.GenesisTimestamp + 100
	for i := 0; i < 4; i++ {
		at += 45
		extendTestChain(t, at, sender, nil)
	}
	coins := ListSpendableCoins(sender)
	if len(coins) != 4 {
		t.Fatalf("the wallet holds %d coins, want 4", len(coins))
	}
	next := 0
	return &at, func(feeUnits int64) Transaction {
		t.Helper()
		if next >= len(coins) {
			t.Fatal("the test wallet has no coin left")
		}
		transfer, err := BuildSignedTransfer(privateKey, publicKey, testAddress("recipient"), cvn("20"), feeUnits, coins[next:next+1])
		if err != nil {
			t.Fatal(err)
		}
		next++
		return transfer
	}
}

// accepted reports whether a block passes the check a neighbour's block
// gets and is then stored.
func accepted(block Block) bool {
	return ValidateBlockSignatures(block) && SaveBlockToStorageInternal(block)
}

// refusedBothWays fails the test unless the block is refused by the check
// and, separately, by storing. Storing checks again by itself: it is how
// blocks are put back after a change of chain that failed.
func refusedBothWays(t *testing.T, block Block, what string) {
	t.Helper()
	if ValidateBlockSignatures(block) {
		t.Errorf("%s passed the check", what)
	}
	if SaveBlockToStorageInternal(block) {
		t.Fatalf("%s was stored", what)
	}
}

// leaveInVault mines an empty block whose miner claims `units` less than the
// 50 CVN it may, which puts that much in the vault. The vault must be empty
// before it.
func leaveInVault(t *testing.T, timestamp *int64, units int64) {
	t.Helper()
	if before := vaultNow(t); before != 0 {
		t.Fatalf("the vault already holds %s CVN", FormatUnits(before))
	}
	*timestamp += 45
	if !accepted(mineTestBlock(t, *timestamp, testAddress("a modest miner"), nil, cvn("50")-units)) {
		t.Fatal("a block that claimed less than it may was refused")
	}
	if got := vaultNow(t); got != units {
		t.Fatalf("the vault holds %s CVN, want the %s a miner left", FormatUnits(got), FormatUnits(units))
	}
}

func TestEveryFeeGoesToTheMiner(t *testing.T) {
	timestamp, payWithFee := feeChain(t)
	transfer := payWithFee(cvn("5"))

	// The miner takes the 50 CVN reward and the whole 5 CVN fee.
	*timestamp += 45
	block := mineTestBlock(t, *timestamp, testAddress("miner"), []Transaction{transfer}, cvn("55"))
	if !ValidateBlockSignatures(block) {
		t.Fatal("a block whose miner took the reward and the whole of the fee was refused")
	}
	if !SaveBlockToStorageInternal(block) {
		t.Fatal("a block whose miner took the reward and the whole of the fee was not stored")
	}
	if got := balanceOf(testAddress("miner")); got != cvn("55") {
		t.Errorf("the miner has %s CVN, want 55", FormatUnits(got))
	}
	if got := vaultNow(t); got != 0 {
		t.Errorf("the vault holds %s CVN after a block with a fee; no part of a fee goes to it", FormatUnits(got))
	}
	// With nothing in the vault there is nothing for it to pay out later.
	*timestamp += 45
	next := extendTestChain(t, *timestamp, testAddress("miner"), nil)
	if got := next.Transactions[0].Outputs[0].Amount.Units(); got != cvn("50") {
		t.Errorf("the block after pays %s CVN, want the reward of 50 and nothing from the vault", FormatUnits(got))
	}
}

func TestABlockMayNotClaimMoreThanItsRewardAndItsFees(t *testing.T) {
	timestamp, payWithFee := feeChain(t)
	transfer := payWithFee(cvn("5"))
	*timestamp += 45

	greedy := mineTestBlock(t, *timestamp, testAddress("miner"), []Transaction{transfer}, cvn("55")+1)
	refusedBothWays(t, greedy, "a block that paid one unit more than the reward and the fees")
	exact := mineTestBlock(t, *timestamp, testAddress("miner"), []Transaction{transfer}, cvn("55"))
	if !accepted(exact) {
		t.Fatal("a block that paid exactly the reward and the fees was refused")
	}
}

// Fees are added up over every transfer in the block, each once.
func TestSeveralFeesInOneBlockAreAllTheMiners(t *testing.T) {
	timestamp, payWithFee := feeChain(t)
	transfers := []Transaction{payWithFee(cvn("3")), payWithFee(cvn("2")), payWithFee(7)}
	*timestamp += 45

	greedy := mineTestBlock(t, *timestamp, testAddress("miner"), transfers, cvn("55")+8)
	refusedBothWays(t, greedy, "a block with fees of 3 CVN, 2 CVN and 7 units that paid 50 CVN, 5 CVN and 8 units")
	exact := mineTestBlock(t, *timestamp, testAddress("miner"), transfers, cvn("55")+7)
	if !accepted(exact) {
		t.Fatal("a block with fees of 3 CVN, 2 CVN and 7 units that paid 50 CVN, 5 CVN and 7 units was refused")
	}
	if got := balanceOf(testAddress("miner")); got != cvn("55")+7 {
		t.Errorf("the miner has %s CVN, want 55.00000007", FormatUnits(got))
	}
	if got := vaultNow(t); got != 0 {
		t.Errorf("the vault holds %s CVN, want nothing", FormatUnits(got))
	}
}

// A block may pay the new reward, what the vault pays out, and the fees: all
// three together, and the fees take nothing from the vault and add nothing
// to it.
func TestFeesAndTheVaultsPayoutTogether(t *testing.T) {
	timestamp, payWithFee := feeChain(t)
	leaveInVault(t, timestamp, cvn("3")) // the next block's payout is a tenth: 0.3 CVN
	transfer := payWithFee(cvn("5"))
	*timestamp += 45

	greedy := mineTestBlock(t, *timestamp, testAddress("miner"), []Transaction{transfer}, cvn("55.3")+1)
	refusedBothWays(t, greedy, "a block that paid one unit more than the reward, the vault's payout and the fee")
	exact := mineTestBlock(t, *timestamp, testAddress("miner"), []Transaction{transfer}, cvn("55.3"))
	if !accepted(exact) {
		t.Fatal("a block that paid exactly the reward, the vault's payout and the fee was refused")
	}
	if got := vaultNow(t); got != cvn("2.7") {
		t.Errorf("the vault holds %s CVN, want 2.7: what it held less its payout, and no part of the fee", FormatUnits(got))
	}
	if got := balanceOf(testAddress("miner")); got != cvn("55.3") {
		t.Errorf("the miner has %s CVN, want 55.3", FormatUnits(got))
	}
}

// What this program's own miner writes into its block: the reward and every
// fee of the transfers it carries.
func TestTheMinersOwnBlockClaimsEveryFee(t *testing.T) {
	timestamp, payWithFee := feeChain(t)
	if err := AdmitTransactionToMempool(payWithFee(cvn("5"))); err != nil {
		t.Fatalf("the transfer was not taken: %v", err)
	}
	tip := GetLatestBlockInternal()
	contents := buildBlockContents(tip, testAddress("miner"), *timestamp+45, "test")
	if len(contents) != 2 {
		t.Fatalf("the miner's block holds %d transactions, want the reward and the transfer", len(contents))
	}
	if outputs := contents[0].Outputs; len(outputs) != 1 || outputs[0].Amount.Units() != cvn("55") {
		t.Errorf("the miner's own block pays it %v, want one output of 55 CVN (the reward and the whole fee)", outputs)
	}
}

// The same with a vault that is paying out and two transfers waiting, and
// the block the miner built is then mined and taken by the rules as it is.
func TestTheMinersOwnBlockWithFeesAndAVaultPayoutIsTaken(t *testing.T) {
	timestamp, payWithFee := feeChain(t)
	leaveInVault(t, timestamp, cvn("3"))
	for _, fee := range []int64{cvn("3"), cvn("2")} {
		if err := AdmitTransactionToMempool(payWithFee(fee)); err != nil {
			t.Fatalf("a transfer was not taken: %v", err)
		}
	}
	*timestamp += 45
	contents := buildBlockContents(GetLatestBlockInternal(), testAddress("miner"), *timestamp, "test")
	if len(contents) != 3 {
		t.Fatalf("the miner's block holds %d transactions, want the reward and two transfers", len(contents))
	}
	if outputs := contents[0].Outputs; len(outputs) != 1 || outputs[0].Amount.Units() != cvn("55.3") {
		t.Fatalf("the miner's own block pays it %v, want one output of 55.3 CVN (50 new, 0.3 from the vault, 5 in fees)", outputs)
	}
	if !accepted(mineTestBlockPaying(t, *timestamp, testAddress("miner"), contents[0].Outputs, contents[1:])) {
		t.Fatal("the block this program's miner built was refused")
	}
	if got := vaultNow(t); got != cvn("2.7") {
		t.Errorf("the vault holds %s CVN, want 2.7", FormatUnits(got))
	}
}

// A reward divided among several addresses, as a pool's is, may pay them the
// fees too, and every output counts toward what the block claimed.
func TestADividedRewardPaysOutTheFeesToo(t *testing.T) {
	timestamp, payWithFee := feeChain(t)
	leaveInVault(t, timestamp, cvn("3"))
	if err := AdmitTransactionToMempool(payWithFee(cvn("5"))); err != nil {
		t.Fatalf("the transfer was not taken: %v", err)
	}
	*timestamp += 45
	var toDivide int64
	contents := buildBlockContentsDivided(GetLatestBlockInternal(), testAddress("pool"), *timestamp, "test", func(totalUnits int64) []UTXOOutput {
		toDivide = totalUnits
		return equalParts(totalUnits, 3)
	})
	if toDivide != cvn("55.3") {
		t.Fatalf("the reward to divide was %s CVN, want 55.3 (50 new, 0.3 from the vault, 5 in fees)", FormatUnits(toDivide))
	}
	if !accepted(mineTestBlockPaying(t, *timestamp, testAddress("pool"), contents[0].Outputs, contents[1:])) {
		t.Fatal("a block whose reward was divided among three addresses, fees and all, was refused")
	}
	// Had only the first output been counted as claimed, the rest would
	// have been put in the vault as well as paid: coins from nowhere.
	if got := vaultNow(t); got != cvn("2.7") {
		t.Errorf("the vault holds %s CVN, want 2.7", FormatUnits(got))
	}
	var paid int64
	for _, output := range contents[0].Outputs {
		if got := balanceOf(output.Recipient); got != output.Amount.Units() {
			t.Errorf("%s has %s CVN, want its part of %s", output.Recipient, FormatUnits(got), FormatUnits(output.Amount.Units()))
		}
		paid += output.Amount.Units()
	}
	if paid != cvn("55.3") {
		t.Errorf("the three parts add up to %s CVN, want 55.3", FormatUnits(paid))
	}
	// One unit more among the three is refused.
	*timestamp += 45
	over := equalParts(cvn("50")+cvn("0.27")+1, 3)
	refusedBothWays(t, mineTestBlockPaying(t, *timestamp, testAddress("pool"), over, nil), "a divided reward one unit over what the block may pay")
}

// A miner who leaves part of a fee unclaimed gives it to the vault. Nothing
// is destroyed.
func TestAnUnclaimedFeeGoesToTheVault(t *testing.T) {
	timestamp, payWithFee := feeChain(t)
	transfer := payWithFee(cvn("5"))
	*timestamp += 45
	modest := mineTestBlock(t, *timestamp, testAddress("miner"), []Transaction{transfer}, cvn("52"))
	if !accepted(modest) {
		t.Fatal("a block that claimed less than it may was refused")
	}
	if got := vaultNow(t); got != cvn("3") {
		t.Errorf("the vault holds %s CVN, want the 3 the miner left", FormatUnits(got))
	}
	if got := balanceOf(testAddress("miner")); got != cvn("52") {
		t.Errorf("the miner has %s CVN, want 52", FormatUnits(got))
	}
}

// Blocks on the same parent, all carrying the same paying transfer. When one
// replaces another, the replaced one's miner keeps nothing and the vault
// holds what the new one left, not what the old one did.
func TestARivalBlockWithAFeeLeavesNothingOfTheBlockItReplaces(t *testing.T) {
	timestamp, payWithFee := feeChain(t)
	transfer := payWithFee(cvn("5"))
	*timestamp += 45

	// Three rivals, mined before any is stored, while the transfer's coin is
	// still there to be checked. Each may pay 55 CVN and each claims a
	// different amount, so each would leave the vault a different figure.
	// Which of them wins is settled by their identifiers, so the test sorts
	// them by the rule itself and does not depend on luck: the middle one is
	// stored first, the best replaces it, and the worst is then refused.
	rivals := []Block{
		mineTestBlock(t, *timestamp, testAddress("miner who claims 55"), []Transaction{transfer}, cvn("55")),
		mineTestBlock(t, *timestamp, testAddress("miner who claims 52"), []Transaction{transfer}, cvn("52")),
		mineTestBlock(t, *timestamp, testAddress("miner who claims 54"), []Transaction{transfer}, cvn("54")),
	}
	sort.Slice(rivals, func(i, j int) bool { return siblingWins(rivals[i], rivals[j]) })
	winner, first, loser := rivals[0], rivals[1], rivals[2]
	minerOf := func(b Block) string { return b.Transactions[0].Witness }
	claimOf := func(b Block) int64 { return b.Transactions[0].Outputs[0].Amount.Units() }

	if !accepted(first) {
		t.Fatal("the first block was refused")
	}
	if balanceOf(minerOf(first)) != claimOf(first) || vaultNow(t) != cvn("55")-claimOf(first) {
		t.Fatalf("after the first block: its miner has %s CVN and the vault %s; want %s and %s",
			FormatUnits(balanceOf(minerOf(first))), FormatUnits(vaultNow(t)), FormatUnits(claimOf(first)), FormatUnits(cvn("55")-claimOf(first)))
	}
	if !accepted(winner) {
		t.Fatal("the rival block that wins was refused")
	}
	if GetLatestBlockInternal().Hash != winner.Hash {
		t.Fatal("the rival block that wins did not become the newest block")
	}
	check := func(when string) {
		t.Helper()
		if got := balanceOf(minerOf(first)); got != 0 {
			t.Errorf("%s: the miner of the replaced block still has %s CVN", when, FormatUnits(got))
		}
		if got := balanceOf(minerOf(winner)); got != claimOf(winner) {
			t.Errorf("%s: the winner's miner has %s CVN, want %s", when, FormatUnits(got), FormatUnits(claimOf(winner)))
		}
		if got := balanceOf(minerOf(loser)); got != 0 {
			t.Errorf("%s: the miner of the block that lost has %s CVN", when, FormatUnits(got))
		}
		if got, want := vaultNow(t), cvn("55")-claimOf(winner); got != want {
			t.Errorf("%s: the vault holds %s CVN, want the %s the winner left", when, FormatUnits(got), FormatUnits(want))
		}
		if got := balanceOf(testAddress("recipient")); got != cvn("20") {
			t.Errorf("%s: the recipient has %s CVN, want 20, paid once", when, FormatUnits(got))
		}
	}
	check("after the rival took its place")
	// A rival that loses changes nothing.
	if SaveBlockToStorageInternal(loser) {
		t.Fatal("a rival block that loses was stored")
	}
	if GetLatestBlockInternal().Hash != winner.Hash {
		t.Fatal("a rival block that loses replaced the newest block")
	}
	check("after a rival that loses was offered")
}

// After the last block with a reward, a block pays its fees and what the
// vault pays out, and nothing else. A fee too small to be a coin is not
// paid to the miner; it goes to the vault.
func TestAfterEmissionABlockPaysItsFeesAndNoMore(t *testing.T) {
	startTestChain(t, "devnet")
	// Two blocks with a reward, then none. The vault here pays out so slowly
	// that its payouts round to nothing, so the sums below are fees alone.
	Params.Economics = &GenesisEconomics{
		BlockSeconds: 45, EpochBlocks: 2, EmissionEpochs: 1, InitialRewardUnits: cvn("50"),
		VaultPayoutBlocks: 1000000000000000, MinOutputUnits: 10000, ReorgLimitBlocks: 100,
	}
	privateKey, publicKey, sender, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 2; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, sender, nil)
	}
	if BlockRewardUnits(3) != 0 {
		t.Fatal("set-up: block 3 should have no reward")
	}
	pay := func(feeUnits int64) Transaction {
		t.Helper()
		transfer, err := BuildSignedTransfer(privateKey, publicKey, testAddress("recipient"), cvn("1"), feeUnits, ListSpendableCoins(sender))
		if err != nil {
			t.Fatal(err)
		}
		return transfer
	}

	// Block 3: a 5 CVN fee and no reward.
	transfer := pay(cvn("5"))
	timestamp += 45
	refusedBothWays(t, mineTestBlock(t, timestamp, testAddress("miner"), []Transaction{transfer}, cvn("5")+1), "a block after emission that paid one unit more than its fee")
	if !accepted(mineTestBlock(t, timestamp, testAddress("miner"), []Transaction{transfer}, cvn("5"))) {
		t.Fatal("a block after emission that paid exactly its fee was refused")
	}
	if balanceOf(testAddress("miner")) != cvn("5") || vaultNow(t) != 0 {
		t.Fatalf("after block 3: the miner has %s CVN and the vault %s; want 5 and 0", FormatUnits(balanceOf(testAddress("miner"))), FormatUnits(vaultNow(t)))
	}

	// Block 4, built by this program's miner: a fee of exactly the smallest
	// coin is paid to the miner as one.
	minimum := minimumOutputUnits()
	if err := AdmitTransactionToMempool(pay(minimum)); err != nil {
		t.Fatalf("a transfer was not taken: %v", err)
	}
	timestamp += 45
	contents := buildBlockContents(GetLatestBlockInternal(), testAddress("miner"), timestamp, "test")
	if len(contents) != 2 || len(contents[0].Outputs) != 1 || contents[0].Outputs[0].Amount.Units() != minimum {
		t.Fatalf("with a fee of exactly the smallest coin the miner's block pays %v, want that one coin", contents[0].Outputs)
	}
	if !accepted(mineTestBlockPaying(t, timestamp, testAddress("miner"), contents[0].Outputs, contents[1:])) {
		t.Fatal("the miner's block with a fee of the smallest coin was refused")
	}
	if got := vaultNow(t); got != 0 {
		t.Fatalf("after block 4 the vault holds %s CVN, want nothing", FormatUnits(got))
	}

	// Block 5: a fee one unit short of the smallest coin cannot be paid out.
	// The block pays nobody and the fee goes to the vault.
	if err := AdmitTransactionToMempool(pay(minimum - 1)); err != nil {
		t.Fatalf("a transfer was not taken: %v", err)
	}
	timestamp += 45
	contents = buildBlockContents(GetLatestBlockInternal(), testAddress("miner"), timestamp, "test")
	if len(contents) != 2 || len(contents[0].Outputs) != 0 {
		t.Fatalf("with a fee smaller than the smallest coin the miner's block pays %v, want nobody", contents[0].Outputs)
	}
	if !accepted(mineTestBlockPaying(t, timestamp, testAddress("miner"), nil, contents[1:])) {
		t.Fatal("a block that pays nobody was refused")
	}
	if got := vaultNow(t); got != minimum-1 {
		t.Errorf("the vault holds %d units, want the %d of the fee that could not be paid", got, minimum-1)
	}
	if got := balanceOf(testAddress("miner")); got != cvn("5")+minimum {
		t.Errorf("the miner has %s CVN, want 5.0001", FormatUnits(got))
	}
}

// The same holds at every height, for small fees and the largest there
// could be, with the vault empty and with it paying out.
func TestFeesAreTheMinersAtEveryHeight(t *testing.T) {
	onNetwork(t, "devnet")
	last := int64(34339200) // the last block with a reward
	if Params.Economics.EmissionEndHeight() != last {
		t.Fatalf("emission ends at %d, and this test was written for %d", Params.Economics.EmissionEndHeight(), last)
	}
	perBlock := int64(9811200) // the vault pays this fraction of itself
	if Params.Economics.VaultPayoutBlocks != perBlock {
		t.Fatalf("the vault pays one part in %d, and this test was written for %d", Params.Economics.VaultPayoutBlocks, perBlock)
	}
	rewards := map[int64]int64{
		1: cvn("50"), 1000: cvn("50"), 4905600: cvn("50"), 4905601: cvn("25"),
		last - 1: cvn("0.78125"), last: cvn("0.78125"), last + 1: 0, last + 1000000: 0,
	}
	for height, reward := range rewards {
		for _, fees := range []int64{0, 1, 4, 5, 9, cvn("10"), cvn("10") + 1, cvn("1000"), Params.Economics.TotalSupplyUnits()} {
			for _, vault := range []int64{0, perBlock - 1, 7 * perBlock, 7*perBlock + 3} {
				payout := vault / perBlock // 0, 0, 7 and 7
				a := accountForBlock(height, fees, vault)
				if a.MinerFeeUnits != fees {
					t.Errorf("height %d, fees %d, vault %d: the miner may take %d of the fees, want all", height, fees, vault, a.MinerFeeUnits)
				}
				if a.VaultPayoutUnits != payout || a.VaultAfterUnits != vault-payout {
					t.Errorf("height %d, fees %d, vault %d: the vault pays %d and keeps %d, want %d and %d", height, fees, vault, a.VaultPayoutUnits, a.VaultAfterUnits, payout, vault-payout)
				}
				if a.MinerAllowanceUnits() != reward+payout+fees {
					t.Errorf("height %d, fees %d, vault %d: the miner may claim %d, want %d (reward %d, payout %d, fees %d)", height, fees, vault, a.MinerAllowanceUnits(), reward+payout+fees, reward, payout, fees)
				}
			}
		}
	}
}

// FuzzABlocksAccountingLosesNoUnit checks, for any height, any fees and any
// vault, that a block's accounting adds up: the miner may take every fee,
// the vault pays exactly its one part and keeps the rest, and what the miner
// may claim and what stays in the vault are together exactly the new
// reward, the fees and what the vault held before.
func FuzzABlocksAccountingLosesNoUnit(f *testing.F) {
	saved := Params
	if err := SelectNetwork("devnet"); err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { Params = saved; applyParams() })
	economics := *Params.Economics
	last, supply, perBlock := economics.EmissionEndHeight(), economics.TotalSupplyUnits(), economics.VaultPayoutBlocks
	f.Add(int64(1), int64(0), int64(0))
	f.Add(int64(1000), int64(1000000000), int64(68678400))
	f.Add(last, int64(9), perBlock-1)
	f.Add(last+1, int64(5), supply)
	// within brings any number the fuzzer makes into 0..limit, so that no
	// input is wasted.
	within := func(n, limit int64) int64 {
		if n < 0 {
			n = -(n + 1)
		}
		return n % (limit + 1)
	}
	f.Fuzz(func(t *testing.T, height, fees, vault int64) {
		// Heights from 1 to well past the end of emission; fees and a vault
		// no larger than every coin there can be, which is all the rules
		// ever hand this function.
		height, fees, vault = 1+within(height, last+10000000), within(fees, supply), within(vault, supply)
		a := accountForBlock(height, fees, vault)
		if a.MinerFeeUnits != fees {
			t.Fatalf("height %d, fees %d, vault %d: the miner may take %d of the fees", height, fees, vault, a.MinerFeeUnits)
		}
		if a.VaultPayoutUnits != vault/perBlock || a.VaultAfterUnits != vault-vault/perBlock {
			t.Fatalf("height %d, fees %d, vault %d: the vault pays %d and is left with %d", height, fees, vault, a.VaultPayoutUnits, a.VaultAfterUnits)
		}
		if a.RewardUnits < 0 || a.RewardUnits > economics.InitialRewardUnits || (height > last && a.RewardUnits != 0) {
			t.Fatalf("height %d: a reward of %d units", height, a.RewardUnits)
		}
		if a.MinerAllowanceUnits()+a.VaultAfterUnits != a.RewardUnits+fees+vault {
			t.Fatalf("height %d, fees %d, vault %d: %d for the miner and %d in the vault do not add up to the reward, the fees and the vault before", height, fees, vault, a.MinerAllowanceUnits(), a.VaultAfterUnits)
		}
	})
}
