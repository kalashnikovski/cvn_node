package node

import (
	"strings"
	"testing"
)

func TestBlockAccounting(t *testing.T) {
	onNetwork(t, "devnet")
	e := Params.Economics

	// Early in the chain: 50 CVN reward, and every fee is the miner's.
	a := accountForBlock(1000, cvn("10"), 0)
	if a.RewardUnits != cvn("50") || a.MinerFeeUnits != cvn("10") || a.VaultPayoutUnits != 0 || a.VaultAfterUnits != 0 {
		t.Errorf("block with 10 CVN of fees and an empty vault: %+v", a)
	}
	if a.MinerAllowanceUnits() != cvn("60") {
		t.Errorf("miner may claim %s, want 60", FormatUnits(a.MinerAllowanceUnits()))
	}

	// The vault pays out 1/N of its balance per block. Fees do not enter it.
	vault := e.VaultPayoutBlocks * 7 // pays exactly 7 units
	a = accountForBlock(1000, cvn("10"), vault)
	if a.VaultPayoutUnits != 7 || a.VaultAfterUnits != vault-7 || a.SubsidyUnits() != cvn("50")+7 || a.MinerAllowanceUnits() != cvn("60")+7 {
		t.Errorf("payout from a vault of %d units: %+v", vault, a)
	}
	// Fractions of a unit stay in the vault.
	a = accountForBlock(1000, 0, e.VaultPayoutBlocks-1)
	if a.VaultPayoutUnits != 0 || a.VaultAfterUnits != e.VaultPayoutBlocks-1 {
		t.Errorf("a vault too small to pay a whole unit must pay nothing: %+v", a)
	}
	// No fee is divided, however small: 9 units of fees are 9 for the miner.
	a = accountForBlock(1000, 9, 0)
	if a.MinerFeeUnits != 9 || a.VaultAfterUnits != 0 {
		t.Errorf("9 units of fees: the miner may take %d and the vault holds %d, want 9 and 0", a.MinerFeeUnits, a.VaultAfterUnits)
	}

	// The same at the last block with a reward and at the one after it.
	last := e.EmissionEndHeight()
	if a = accountForBlock(last, cvn("10"), 0); a.MinerFeeUnits != cvn("10") || a.VaultAfterUnits != 0 || a.RewardUnits != cvn("0.78125") {
		t.Errorf("last rewarded block: %+v", a)
	}
	if a = accountForBlock(last+1, cvn("10"), 0); a.MinerFeeUnits != cvn("10") || a.VaultAfterUnits != 0 || a.RewardUnits != 0 {
		t.Errorf("after emission the miner keeps every fee: %+v", a)
	}
	// ...but the vault keeps paying out for as long as it holds anything.
	if a = accountForBlock(last+1, 0, vault); a.VaultPayoutUnits != 7 || a.MinerAllowanceUnits() != 7 {
		t.Errorf("after emission the vault must still pay out: %+v", a)
	}
}

// A small economy with a fast vault, so its behaviour shows within a few blocks.
func startVaultChain(t *testing.T) {
	t.Helper()
	startTestChain(t, "devnet")
	Params.Economics = &GenesisEconomics{
		BlockSeconds: 45, EpochBlocks: 1000, EmissionEpochs: 7, InitialRewardUnits: cvn("50"),
		VaultPayoutBlocks: 10, MinOutputUnits: 10000, ReorgLimitBlocks: 100,
	}
}

func TestVaultOnAChain(t *testing.T) {
	startVaultChain(t)
	privateKey, publicKey, sender, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp + 100
	mine := func(miner string, transfers []Transaction) Block {
		timestamp += 45
		return extendTestChain(t, timestamp, miner, transfers)
	}
	// Everything ever issued must be either in someone's balance or in the vault.
	checkNothingLost := func(when string, addresses ...string) {
		t.Helper()
		total := VaultBalanceUnits()
		for _, address := range addresses {
			total += balanceOf(address)
		}
		issued := GetLatestBlockInternal().Index * cvn("50")
		if total != issued {
			t.Fatalf("%s: balances plus vault are %s, but %s has been issued", when, FormatUnits(total), FormatUnits(issued))
		}
	}

	mine(sender, nil)
	mine(sender, nil)
	if VaultBalanceUnits() != 0 {
		t.Fatalf("vault holds %s before any fee was paid", FormatUnits(VaultBalanceUnits()))
	}

	// A transfer paying a 5 CVN fee. The miner may take the reward and the
	// whole fee, 55 CVN. This miner claims 54 and so leaves 1 CVN to the
	// vault: nothing but a sweep or a miner's own choice puts coins there.
	transfer, err := BuildSignedTransfer(privateKey, publicKey, testAddress("recipient"), cvn("20"), cvn("5"), ListSpendableCoins(sender))
	if err != nil {
		t.Fatal(err)
	}
	timestamp += 45
	modest := mineTestBlock(t, timestamp, testAddress("minerA"), []Transaction{transfer}, cvn("54"))
	if !ValidateBlockSignatures(modest) || !SaveBlockToStorageInternal(modest) {
		t.Fatal("a block that claimed 54 of the 55 CVN it may was refused")
	}
	if balanceOf(testAddress("minerA")) != cvn("54") || VaultBalanceUnits() != cvn("1") {
		t.Fatalf("after the fee block: miner %s, vault %s; want 54 and 1", FormatUnits(balanceOf(testAddress("minerA"))), FormatUnits(VaultBalanceUnits()))
	}
	checkNothingLost("after the fee block", sender, testAddress("recipient"), testAddress("minerA"))

	// From the next block on the vault pays a tenth of what it holds, each
	// block, to that block's miner.
	block := mine(testAddress("minerB"), nil)
	if got := block.Transactions[0].Outputs[0].Amount.Units(); got != cvn("50.1") || VaultBalanceUnits() != cvn("0.9") {
		t.Fatalf("first payout block pays %s and leaves %s; want 50.1 and 0.9", FormatUnits(got), FormatUnits(VaultBalanceUnits()))
	}
	block = mine(testAddress("minerB"), nil)
	if got := block.Transactions[0].Outputs[0].Amount.Units(); got != cvn("50.09") || VaultBalanceUnits() != cvn("0.81") {
		t.Fatalf("second payout block pays %s and leaves %s; want 50.09 and 0.81", FormatUnits(got), FormatUnits(VaultBalanceUnits()))
	}
	checkNothingLost("after two payouts", sender, testAddress("recipient"), testAddress("minerA"), testAddress("minerB"))

	// A miner may not take more than the reward plus the payout.
	timestamp += 45
	greedy := mineTestBlock(t, timestamp, testAddress("minerB"), nil, cvn("50.081")+1)
	if ValidateBlockSignatures(greedy) && SaveBlockToStorageInternal(greedy) {
		t.Fatal("a block paid its miner one unit more than the reward plus the vault payout")
	}
	honest := mineTestBlock(t, timestamp, testAddress("minerB"), nil, 0)
	if got := honest.Transactions[0].Outputs[0].Amount.Units(); got != cvn("50.081") {
		t.Fatalf("third payout should be 50.081, test block pays %s", FormatUnits(got))
	}
	if !ValidateBlockSignatures(honest) || !SaveBlockToStorageInternal(honest) {
		t.Fatal("the correctly paid block was refused")
	}

	// The vault is part of the chain's state: taking blocks off puts it back
	// to what it was, and restoring them brings it forward again.
	vaultAtTip, tip := VaultBalanceUnits(), GetLatestBlockInternal()
	StateMutex.Lock()
	removed := rollbackChainToHeightLocked(tip.Index - 2)
	vaultTwoBack := VaultBalanceUnits()
	restoreChainLocked(tip.Index-2, removed)
	StateMutex.Unlock()
	if vaultTwoBack != cvn("0.9") {
		t.Errorf("two blocks back the vault held %s, want 0.9", FormatUnits(vaultTwoBack))
	}
	if VaultBalanceUnits() != vaultAtTip || GetLatestBlockInternal().Hash != tip.Hash {
		t.Errorf("after putting the blocks back the vault holds %s, want %s", FormatUnits(VaultBalanceUnits()), FormatUnits(vaultAtTip))
	}
	checkNothingLost("at the end", sender, testAddress("recipient"), testAddress("minerA"), testAddress("minerB"))
}

func TestMinimumOutput(t *testing.T) {
	startVaultChain(t)
	privateKey, publicKey, sender, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	extendTestChain(t, Params.GenesisTimestamp+200, sender, nil)
	coins := ListSpendableCoins(sender)
	minimum := minimumOutputUnits()
	if minimum != 10000 {
		t.Fatalf("minimum output is %d units, want 10,000", minimum)
	}

	// The wallet refuses to send less than the minimum.
	if _, err := BuildSignedTransfer(privateKey, publicKey, testAddress("recipient"), minimum-1, 0, coins); err == nil {
		t.Error("the wallet built a transfer below the minimum output")
	}
	// A hand-made transfer with an output below the minimum is refused by the rules.
	small := signedTestTransfer(t, privateKey, publicKey, sender, coins[0].TxID, minimum-1)
	if _, err := CheckTransferStandalone(small); err == nil || !strings.Contains(err.Error(), "at least") {
		t.Errorf("an output below the minimum was accepted (err: %v)", err)
	}
	atMinimum := signedTestTransfer(t, privateKey, publicKey, sender, coins[0].TxID, minimum)
	if _, err := CheckTransferStandalone(atMinimum); err != nil {
		t.Errorf("an output of exactly the minimum was refused: %v", err)
	}

	// Change that would be below the minimum is given to the miner instead.
	amount := cvn("50") - cvn("1") - (minimum - 1) // leaves minimum-1 units over after a 1 CVN fee
	tx, err := BuildSignedTransfer(privateKey, publicKey, testAddress("recipient"), amount, cvn("1"), coins)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Outputs) != 1 || tx.FreeWillOffering.Units() != cvn("1")+minimum-1 {
		t.Errorf("tiny change: %d outputs, fee %s; want one output and the change added to the fee", len(tx.Outputs), tx.FreeWillOffering)
	}
	if _, err := CheckTransferStandalone(tx); err != nil {
		t.Errorf("the wallet's own transfer was refused: %v", err)
	}
	// Change of exactly the minimum is returned to the sender.
	tx, err = BuildSignedTransfer(privateKey, publicKey, testAddress("recipient"), cvn("50")-cvn("1")-minimum, cvn("1"), coins)
	if err != nil || len(tx.Outputs) != 2 || tx.Outputs[1].Amount.Units() != minimum {
		t.Errorf("change of exactly the minimum should be its own output (err %v)", err)
	}
}

func TestReorganisationLimit(t *testing.T) {
	onNetwork(t, "devnet")
	limit := Params.Economics.ReorgLimitBlocks
	if limit != 100 {
		t.Fatalf("devnet reorganisation limit is %d blocks, want 100", limit)
	}
	if !reorgDepthAllowed(1) || !reorgDepthAllowed(limit) {
		t.Error("a replacement within the limit must be allowed")
	}
	if reorgDepthAllowed(limit+1) || reorgDepthAllowed(100000) {
		t.Error("a replacement deeper than the limit must be refused")
	}
}
