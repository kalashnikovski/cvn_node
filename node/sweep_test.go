package node

import (
	"encoding/json"
	"strings"
	"testing"

	"go.etcd.io/bbolt"
)

// A small economy where a coin is swept after only five blocks. The vault
// pays out so slowly here that its payouts round to nothing, which keeps the
// sums in these tests easy to follow.
func startSweepChain(t *testing.T) *int64 {
	t.Helper()
	startTestChain(t, "devnet")
	Params.Economics = &GenesisEconomics{
		BlockSeconds: 45, EpochBlocks: 1000000, EmissionEpochs: 7, InitialRewardUnits: cvn("50"),
		SweepIntervalBlocks: 5, SweepDivisor: 5,
		VaultPayoutBlocks: 1000000000000000, FeeVaultDivisor: 5, MinOutputUnits: 10000, ReorgLimitBlocks: 100,
	}
	timestamp := Params.GenesisTimestamp + 100
	return &timestamp
}

func mineNext(t *testing.T, timestamp *int64, miner string, transfers ...Transaction) Block {
	t.Helper()
	*timestamp += 45
	return extendTestChain(t, *timestamp, miner, transfers)
}

// everythingIssuedIsAccountedFor checks that the balances of the given
// addresses plus the vault equal exactly what the schedule has issued.
func everythingIssuedIsAccountedFor(t *testing.T, when string, addresses ...string) {
	t.Helper()
	total := VaultBalanceUnits()
	for _, address := range addresses {
		total += balanceOf(address)
	}
	if issued := GetLatestBlockInternal().Index * cvn("50"); total != issued {
		t.Fatalf("%s: balances plus vault are %s, but %s has been issued", when, FormatUnits(total), FormatUnits(issued))
	}
}

func TestSweepAmounts(t *testing.T) {
	onNetwork(t, "devnet")
	cases := []struct{ coin, swept, kept string }{
		{"100", "20", "80"},
		{"50", "10", "40"},
		{"0.001", "0.0002", "0.0008"},
		{"0.00012501", "0.000025", "0.00010001"}, // a fifth rounds down, in the owner's favour
		{"0.000125", "0.000025", "0.0001"},       // keeps exactly the minimum: stays
		{"0.00012499", "0.00002499", "0.0001"},   // rounding down leaves exactly the minimum: stays
		{"0.00012498", "0", "0.00012498"},        // would keep less than the minimum: left alone
		{"0.0001", "0", "0.0001"},                // the smallest coin there is: left alone
		{"0", "0", "0"},
	}
	for _, c := range cases {
		swept, kept := sweepAmounts(cvn(c.coin))
		if swept != cvn(c.swept) || kept != cvn(c.kept) {
			t.Errorf("coin of %s: swept %s, kept %s; want %s and %s", c.coin, FormatUnits(swept), FormatUnits(kept), c.swept, c.kept)
		}
	}
}

func TestUnmovedCoinsAreSwept(t *testing.T) {
	timestamp := startSweepChain(t)
	mineNext(t, timestamp, testAddress("idle")) // block 1: the coin we watch
	for height := 2; height <= 5; height++ {
		mineNext(t, timestamp, testAddress("other"))
	}
	if balanceOf(testAddress("idle")) != cvn("50") || VaultBalanceUnits() != 0 {
		t.Fatalf("one block before the sweep: balance %s, vault %s; want 50 and 0", FormatUnits(balanceOf(testAddress("idle"))), FormatUnits(VaultBalanceUnits()))
	}

	// Block 6 is five blocks after block 1: a fifth of the coin is set aside
	// on the coin. It is still its owner's, so the address still shows 50,
	// and nothing reaches the vault yet.
	mineNext(t, timestamp, testAddress("other"))
	wantCoin(t, "after the first sweep", testAddress("idle"), cvn("40"), cvn("10"), 6)
	if balanceOf(testAddress("idle")) != cvn("50") || VaultBalanceUnits() != 0 {
		t.Fatalf("after the first sweep: balance %s, vault %s; want 50 (40 and 10 set aside) and nothing in the vault", FormatUnits(balanceOf(testAddress("idle"))), FormatUnits(VaultBalanceUnits()))
	}
	coins := ListSpendableCoins(testAddress("idle"))
	if len(coins) != 1 || coins[0].SweepAt != 11 || coins[0].Amount.Units() != cvn("50") || coins[0].HeldUnits != cvn("10") {
		t.Fatalf("the swept coin should be offered at 50 CVN, 10 of it set aside, and next be due at block 11: %+v", coins)
	}
	everythingIssuedIsAccountedFor(t, "after the first sweep", testAddress("idle"), testAddress("other"))

	// Blocks 7-10 sweep the four coins CVN_other mined in blocks 2-5: a
	// fifth of each is set aside, and the vault still holds nothing.
	for height := 7; height <= 10; height++ {
		mineNext(t, timestamp, testAddress("third"))
	}
	if VaultBalanceUnits() != 0 || SetAsideTotalUnits() != cvn("50") {
		t.Fatalf("after five coins were swept once the vault holds %s and %s is set aside on coins; want nothing and 50", FormatUnits(VaultBalanceUnits()), FormatUnits(SetAsideTotalUnits()))
	}

	// Block 11: the first coin, untouched for another five blocks, is swept
	// again. The 10 set aside at block 6 goes to the vault, and a fifth of
	// the 40 it had left is set aside in its turn.
	mineNext(t, timestamp, testAddress("third"))
	wantCoin(t, "after the second sweep", testAddress("idle"), cvn("32"), cvn("8"), 11)
	if balanceOf(testAddress("idle")) != cvn("40") || VaultBalanceUnits() != cvn("10") {
		t.Fatalf("after the second sweep the idle address shows %s and the vault holds %s; want 40 (32 and 8 set aside) and 10", FormatUnits(balanceOf(testAddress("idle"))), FormatUnits(VaultBalanceUnits()))
	}
	everythingIssuedIsAccountedFor(t, "after the second sweep", testAddress("idle"), testAddress("other"), testAddress("third"))
}

func TestMovingACoinResetsItsAge(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner) // block 1
	mineNext(t, timestamp, testAddress("other"))
	mineNext(t, timestamp, testAddress("other"))

	// Block 4: the owner sends the whole coin to themselves, with no fee.
	move, err := BuildSignedTransfer(privateKey, publicKey, owner, cvn("50"), 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, testAddress("other"), move)
	mineNext(t, timestamp, testAddress("other"))
	mineNext(t, timestamp, testAddress("other")) // block 6: the original coin would have been swept here
	if balanceOf(owner) != cvn("50") {
		t.Fatalf("a coin moved at block 4 was swept at block 6: owner has %s", FormatUnits(balanceOf(owner)))
	}
	if coins := ListSpendableCoins(owner); len(coins) != 1 || coins[0].SweepAt != 9 {
		t.Fatalf("the moved coin should be due at block 9: %+v", coins)
	}

	// Moving it in the very block where it falls due still saves it in full.
	mineNext(t, timestamp, testAddress("other"))
	mineNext(t, timestamp, testAddress("other")) // block 8
	move, err = BuildSignedTransfer(privateKey, publicKey, owner, cvn("50"), 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, testAddress("other"), move) // block 9
	if balanceOf(owner) != cvn("50") {
		t.Fatalf("a coin moved in the block where it fell due lost value: owner has %s", FormatUnits(balanceOf(owner)))
	}
	everythingIssuedIsAccountedFor(t, "at the end", owner, testAddress("other"))
}

// The smallest coins are not swept at all. Taking a fifth would leave less
// than a coin may hold, and an earlier form of the rule then took the whole
// coin: everything from the smallest holdings, a fifth from the rest.
func TestTheSmallestCoinsAreLeftAlone(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner) // block 1
	// Block 2: pay someone exactly the minimum output, 0.0001 CVN.
	tiny, err := BuildSignedTransfer(privateKey, publicKey, testAddress("tiny"), minimumOutputUnits(), 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, testAddress("other"), tiny)
	if balanceOf(testAddress("tiny")) != cvn("0.0001") {
		t.Fatalf("recipient has %s, want 0.0001", FormatUnits(balanceOf(testAddress("tiny"))))
	}
	for height := 3; height <= 14; height++ { // blocks 7 and 12 are five and ten blocks after block 2
		mineNext(t, timestamp, testAddress("other"))
	}
	coins := ListSpendableCoins(testAddress("tiny"))
	if balanceOf(testAddress("tiny")) != cvn("0.0001") || len(coins) != 1 {
		t.Fatalf("a coin at the minimum should be left alone: %s in %d coin(s)", FormatUnits(balanceOf(testAddress("tiny"))), len(coins))
	}
	// It can still be spent like any other coin.
	if !coins[0].Amount.Spendable() {
		t.Error("the small coin that was left alone cannot be spent")
	}
	everythingIssuedIsAccountedFor(t, "after the small coin's dates passed", owner, testAddress("tiny"), testAddress("other"))
}

func TestSweepIsUndoneWithItsBlock(t *testing.T) {
	timestamp := startSweepChain(t)
	mineNext(t, timestamp, testAddress("idle"))
	for height := 2; height <= 5; height++ {
		mineNext(t, timestamp, testAddress("other"))
	}
	sweepBlock := mineNext(t, timestamp, testAddress("other")) // block 6 sweeps the idle coin
	mineNext(t, timestamp, testAddress("other"))               // block 7 sweeps the coin from block 2
	tip, vaultAtTip := GetLatestBlockInternal(), VaultBalanceUnits()
	wantCoin(t, "set-up", testAddress("idle"), cvn("40"), cvn("10"), 6)
	if vaultAtTip != 0 || SetAsideTotalUnits() != cvn("20") {
		t.Fatalf("set-up: vault %s, set aside on coins %s; want nothing and 20", FormatUnits(vaultAtTip), FormatUnits(SetAsideTotalUnits()))
	}

	// A competing block at the same height must see coins as they were
	// before the tip's sweep.
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		bUTXO := dbTx.Bucket(utxoBucket)
		before := lookupWithBlockUndone(dbTx, bUTXO, tip)
		swept := loadSweepRecords(dbTx, tip.Index)
		if len(swept) != 1 {
			t.Fatalf("block 7 swept %d coins, want 1", len(swept))
		}
		if record, ok := before(swept[0].Key); !ok || record.Amount.Units() != cvn("50") || record.HeldUnits != 0 || record.Age != 2 {
			t.Errorf("with block 7 undone its swept coin should read 50 with nothing set aside, age 2; got %s with %d units set aside, age %d", record.Amount, record.HeldUnits, record.Age)
		}
		return nil
	})

	// Take both sweep blocks off: coins and vault return to before any sweep.
	StateMutex.Lock()
	removed := rollbackChainToHeightLocked(sweepBlock.Index - 1)
	StateMutex.Unlock()
	wantCoin(t, "after removing the sweep blocks", testAddress("idle"), cvn("50"), 0, 1)
	if balanceOf(testAddress("idle")) != cvn("50") || VaultBalanceUnits() != 0 || SetAsideTotalUnits() != 0 {
		t.Fatalf("after removing the sweep blocks: idle %s, vault %s, set aside %s; want 50, 0 and 0", FormatUnits(balanceOf(testAddress("idle"))), FormatUnits(VaultBalanceUnits()), FormatUnits(SetAsideTotalUnits()))
	}
	if coins := ListSpendableCoins(testAddress("idle")); len(coins) != 1 || coins[0].SweepAt != 6 || coins[0].HeldUnits != 0 {
		t.Fatalf("the restored coin should be due at block 6 again, with nothing set aside: %+v", coins)
	}
	everythingIssuedIsAccountedFor(t, "after the rollback", testAddress("idle"), testAddress("other"))

	// Put them back: the sweeps happen again, identically.
	StateMutex.Lock()
	restoreChainLocked(sweepBlock.Index-1, removed)
	StateMutex.Unlock()
	wantCoin(t, "after restoring", testAddress("idle"), cvn("40"), cvn("10"), 6)
	if GetLatestBlockInternal().Hash != tip.Hash || VaultBalanceUnits() != vaultAtTip || SetAsideTotalUnits() != cvn("20") {
		t.Fatalf("after restoring: vault %s, set aside %s; want %s and 20", FormatUnits(VaultBalanceUnits()), FormatUnits(SetAsideTotalUnits()), FormatUnits(vaultAtTip))
	}
	everythingIssuedIsAccountedFor(t, "after restoring", testAddress("idle"), testAddress("other"))
}

// A block that pays its miner less than it could puts the rest in the vault.
func TestUnclaimedRewardGoesToTheVault(t *testing.T) {
	timestamp := startSweepChain(t)
	mineNext(t, timestamp, testAddress("miner"))
	*timestamp += 45
	modest := mineTestBlock(t, *timestamp, testAddress("miner"), nil, cvn("49"))
	if !ValidateBlockSignatures(modest) || !SaveBlockToStorageInternal(modest) {
		t.Fatal("a block claiming less than its reward was refused")
	}
	if balanceOf(testAddress("miner")) != cvn("99") || VaultBalanceUnits() != cvn("1") {
		t.Fatalf("miner %s, vault %s; want 99 and 1", FormatUnits(balanceOf(testAddress("miner"))), FormatUnits(VaultBalanceUnits()))
	}
	everythingIssuedIsAccountedFor(t, "after an under-claimed block", testAddress("miner"))
}

func TestWalletWarnsAboutTheSweep(t *testing.T) {
	timestamp := startSweepChain(t)
	mineNext(t, timestamp, testAddress("idle"))
	raw, _ := json.Marshal(ListSpendableCoins(testAddress("idle")))
	if !strings.Contains(string(raw), `"sweep_at":6`) {
		t.Errorf("a wallet asking for coins is not told when they fall due: %s", raw)
	}
}

// The devnet-only shortcuts change devnet's own settings and nothing else.
func TestDevnetShortcuts(t *testing.T) {
	saved := Params
	defer func() { Params = saved; applyParams() }()
	t.Setenv("CVN_DEVNET_SWEEP_BLOCKS", "30")
	t.Setenv("CVN_DEVNET_VAULT_BLOCKS", "100")

	if err := SelectNetwork("devnet"); err != nil {
		t.Fatal(err)
	}
	if Params.Economics.SweepIntervalBlocks != 30 || Params.Economics.VaultPayoutBlocks != 100 {
		t.Errorf("devnet shortcuts not applied: %+v", *Params.Economics)
	}
	if NewGenesisEconomics.SweepIntervalBlocks != 4905600 || NewGenesisEconomics.VaultPayoutBlocks != 9811200 {
		t.Error("the shortcuts changed the launch settings themselves")
	}
	if err := SelectNetwork("cvn_testnet"); err != nil || Params.Economics != &NewGenesisEconomics {
		t.Error("the shortcuts must not affect the CVN Testnet")
	}
}
