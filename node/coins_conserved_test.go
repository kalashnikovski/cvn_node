package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"go.etcd.io/bbolt"
)

// storedCoin is a coin as it is written in the database, read by the names
// of its fields there and not through the program's own type: its amount,
// the fifth a sweep has set aside on it for its owner (nought for most
// coins), its owner and its age.
type storedCoin struct {
	Amount    int64  `json:"amount"`
	Held      int64  `json:"held"`
	Recipient string `json:"recipient"`
	Age       int64  `json:"age"`
}

// worth is what the coin gives whoever spends it: the amount and the fifth
// set aside.
func (c storedCoin) worth() int64 { return c.Amount + c.Held }

// everyStoredCoin reads every coin in the database, by its key.
func everyStoredCoin(t *testing.T) map[string]storedCoin {
	t.Helper()
	coins := make(map[string]storedCoin)
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(utxoBucket).ForEach(func(key, raw []byte) error {
			var coin storedCoin
			if err := json.Unmarshal(raw, &coin); err != nil {
				t.Fatalf("coin %s is unreadable: %v", key, err)
			}
			coins[string(key)] = coin
			return nil
		})
	})
	return coins
}

// coinCount adds up every coin in the database, not just the coins of
// addresses a test knows about: each coin's amount and the fifth set aside
// on it, which is still its owner's. It checks that no coin holds nothing
// or less, and that nothing set aside is below nought.
func coinCount(t *testing.T) int64 {
	t.Helper()
	var total int64
	for key, coin := range everyStoredCoin(t) {
		// The genesis block's output pays nothing and is never spent; it
		// is the one record allowed to hold zero.
		if coin.Amount < 0 || (coin.Amount == 0 && key != "TX_GENESIS_0") {
			t.Fatalf("coin %s holds %d units", key, coin.Amount)
		}
		if coin.Held < 0 {
			t.Fatalf("coin %s has %d units set aside", key, coin.Held)
		}
		total += coin.worth()
	}
	return total
}

// coinSetFingerprint is a hash of every coin record, byte for byte, in the
// database's own key order, and of the vault's balance. Two moments with the
// same fingerprint have exactly the same coins, with the same amounts, the
// same fifths set aside and the same ages.
func coinSetFingerprint() string {
	digest := sha256.New()
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(utxoBucket).ForEach(func(key, raw []byte) error {
			fmt.Fprintf(digest, "%d:%s|%d:%s|", len(key), key, len(raw), raw)
			return nil
		})
	})
	fmt.Fprintf(digest, "vault:%d", VaultBalanceUnits())
	return hex.EncodeToString(digest.Sum(nil))
}

// issuedUpTo is every new coin the schedule has issued from block 1 to height.
func issuedUpTo(height int64) int64 {
	var total int64
	for h := int64(1); h <= height; h++ {
		total += BlockRewardUnits(h)
	}
	return total
}

// TestCoinsAreNeverCreatedOrLost runs a small economy through everything that
// moves coins: transfers with fees (every fee going to the block's miner),
// coins swept at their date, the fifth a sweep sets aside staying on the
// coin for its owner, an owner taking it back by spending the coin, a fifth
// nobody took back going to the vault at the coin's next date, the vault
// paying out, blocks that claim less than they may, the end of emission,
// and reorganisations.
//
// After every block:
//
//   - every coin's amount, plus every fifth set aside, plus the vault, must
//     equal exactly what the schedule has issued;
//   - every coin the block's sweep touched must be worth to its owner what
//     it held before (the fifth is set aside, not taken), and the vault must
//     have received from the sweep exactly the fifths that had been set
//     aside one date earlier, and nothing else;
//   - the coins the sweep touched must be exactly the coins that were due,
//     worked out here from the coins themselves and not from the sweep's own
//     record, each with a fifth rounded down set aside or none; and every
//     other coin must be exactly as it was;
//   - every transfer that spent a coin with a fifth set aside must have
//     been given the amount and the fifth together;
//   - what the node tells wallets and the explorer (each address's balance,
//     and the sum of all fifths set aside) must agree with the coins, as the
//     node left those figures and without a recount.
//
// Every rollback must give back, byte for byte, the coins that were there
// at that height, and putting the blocks back must give back the coins that
// were there before.
func TestCoinsAreNeverCreatedOrLost(t *testing.T) {
	startTestChain(t, "devnet")
	// Emission ends after block 30, so the last part of the run lives on fees
	// and the vault. A coin's date is six blocks after it was made or last
	// swept, and the vault pays a seventh of itself each block, so both
	// happen often.
	Params.Economics = &GenesisEconomics{
		BlockSeconds: 45, EpochBlocks: 10, EmissionEpochs: 3, InitialRewardUnits: cvn("50"),
		SweepIntervalBlocks: 6, SweepDivisor: 5,
		VaultPayoutBlocks: 7, MinOutputUnits: 10000, ReorgLimitBlocks: 100,
	}

	type wallet struct{ private, public, address string }
	newWallet := func() wallet {
		private, public, address, err := GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		return wallet{private, public, address}
	}
	var wallets []wallet
	for i := 0; i < 3; i++ {
		wallets = append(wallets, newWallet())
	}
	// The keeper leaves its coins alone until their dates have passed and
	// then, every tenth block, sends them all back to itself: an owner who
	// comes back for what a sweep set aside. The outsider has no key at
	// all: what is set aside on its coins is never taken back, and goes to
	// the vault at their next date.
	keeper := newWallet()
	miners := []string{wallets[0].address, wallets[1].address, wallets[2].address, keeper.address, testAddress("outsider")}

	// The order of everything below is fixed by this seed, so a failure can
	// be repeated exactly.
	random := rand.New(rand.NewSource(20261125))
	timestamp := Params.GenesisTimestamp + 100
	fingerprints := map[int64]string{0: coinSetFingerprint()}

	conserved := func(when string) {
		t.Helper()
		height := GetLatestBlockInternal().Index
		vault := VaultBalanceUnits()
		if vault < 0 {
			t.Fatalf("%s: the vault holds %d units", when, vault)
		}
		coins := coinCount(t)
		if issued := issuedUpTo(height); coins+vault != issued {
			t.Fatalf("%s (height %d): coins %s (with what is set aside on them) plus vault %s make %s, but %s has been issued",
				when, height, FormatUnits(coins), FormatUnits(vault), FormatUnits(coins+vault), FormatUnits(issued))
		}
		if coins+vault > Params.Economics.TotalSupplyUnits() {
			t.Fatalf("%s: more than the whole supply exists", when)
		}
	}

	transfersMade, underClaimed, vaultPaidOut, rollbacks := 0, 0, false, 0
	// What the run has seen of fifths set aside.
	setAside, takenBack, goneToVault := 0, 0, 0
	takenBackWithAFee, takenBackByTheKeeper, tooSmallForAFifth := 0, 0, 0
	setAsideAt, goneToVaultAt, takenBackAt := map[int64]bool{}, map[int64]bool{}, map[int64]bool{}
	rollbacksOverSettingAside, rollbacksOverExpiry, rollbacksOverTakingBack := 0, 0, 0
	for step := 1; step <= 80; step++ {
		// Up to two transfers between the wallets, each from a different
		// sender so that no coin is spent twice in one block.
		var transfers []Transaction
		for _, i := range random.Perm(len(wallets))[:random.Intn(3)] {
			from := wallets[i]
			coins := ListSpendableCoins(from.address)
			var spendable int64
			for _, coin := range coins {
				spendable += coin.Amount.Units()
			}
			fee := cvn("0.01") * int64(1+random.Intn(50))
			if spendable < fee+cvn("1") {
				continue
			}
			amount := cvn("0.5") + random.Int63n(spendable-fee-cvn("0.5"))
			to := wallets[(i+1+random.Intn(len(wallets)-1))%len(wallets)].address
			transfer, err := BuildSignedTransfer(from.private, from.public, to, amount, fee, coins)
			if err != nil {
				continue // not enough in coins large enough, for instance
			}
			transfers = append(transfers, transfer)
		}
		// Every tenth block the keeper renews everything it holds, at no fee.
		if step%10 == 0 {
			if coins := ListSpendableCoins(keeper.address); len(coins) > 0 && len(coins) <= TxMaxInputs {
				renewal, err := BuildSignedRenewal(keeper.private, keeper.public, 0, coins)
				if err != nil {
					t.Fatalf("step %d: the keeper's renewal could not be built: %v", step, err)
				}
				transfers = append(transfers, renewal)
			}
		}

		before := everyStoredCoin(t)
		vaultBefore := VaultBalanceUnits()
		timestamp += 45
		miner := miners[random.Intn(len(miners))]
		if random.Intn(6) == 0 {
			// A block that claims less than it may: the rest must go to the vault.
			block := mineTestBlock(t, timestamp, miner, transfers, 0)
			allowance := block.Transactions[0].Outputs
			if len(allowance) > 0 && allowance[0].Amount.Units() > 2 {
				modest := mineTestBlock(t, timestamp, miner, transfers, allowance[0].Amount.Units()/2)
				if !ValidateBlockSignatures(modest) || !SaveBlockToStorageInternal(modest) {
					t.Fatalf("step %d: a block claiming half its allowance was refused", step)
				}
				underClaimed++
			} else {
				extendTestChain(t, timestamp, miner, transfers)
			}
		} else {
			extendTestChain(t, timestamp, miner, transfers)
		}
		transfersMade += len(transfers)
		tip := GetLatestBlockInternal()
		after := everyStoredCoin(t)

		// What the node tells a wallet or the explorer, as the node left it
		// after this block (nothing here has asked for a recount): each
		// address's balance is its coins with what is set aside on them,
		// and the figure of all fifths set aside is their sum.
		byAddress, heldInAll := map[string]int64{}, int64(0)
		for _, coin := range after {
			byAddress[coin.Recipient] += coin.worth()
			heldInAll += coin.Held
		}
		BalanceCacheMutex.RLock()
		for address, want := range byAddress {
			if got := StateBalanceCache[address]; got != want {
				t.Errorf("block %d: the node shows %s CVN at %s; its coins, with what is set aside on them, make %s", tip.Index, FormatUnits(got), address, FormatUnits(want))
			}
		}
		for address, got := range StateBalanceCache {
			if _, holds := byAddress[address]; !holds && got != 0 {
				t.Errorf("block %d: the node shows %s CVN at %s, which holds no coin", tip.Index, FormatUnits(got), address)
			}
		}
		BalanceCacheMutex.RUnlock()
		if got := SetAsideTotalUnits(); got != heldInAll {
			t.Fatalf("block %d: the node counts %s set aside on coins; the coins hold %s", tip.Index, FormatUnits(got), FormatUnits(heldInAll))
		}
		if t.Failed() {
			t.FailNow()
		}

		// A transfer that spent a coin with a fifth set aside was given
		// the amount and the fifth together.
		var fees int64
		for _, transfer := range tip.Transactions[1:] {
			fees += transfer.FreeWillOffering.Units()
			for _, input := range transfer.Inputs {
				spent, known := before[utxoKey(input.SourceTxID, input.Index)]
				if !known {
					t.Fatalf("block %d spends a coin that was not there before it", tip.Index)
				}
				if input.AmountUnits != spent.worth() {
					t.Fatalf("block %d: a transfer spent a coin of %s with %s set aside and was given %s; want both together",
						tip.Index, FormatUnits(spent.Amount), FormatUnits(spent.Held), FormatUnits(input.AmountUnits))
				}
				if spent.Held > 0 {
					takenBack++
					takenBackAt[tip.Index] = true
					if transfer.FreeWillOffering.Units() > 0 {
						takenBackWithAFee++
					}
					if spent.Recipient == keeper.address {
						takenBackByTheKeeper++
					}
				}
			}
		}

		// The sweep of this block: every coin it touched is worth to its
		// owner what it held before, and what went to the vault is what
		// had been set aside on those coins one date earlier.
		var swept []SweptCoin
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			swept = mustLoadSweepRecords(dbTx, tip.Index)
			return nil
		})
		var expired int64
		for _, record := range swept {
			was, known := before[record.Key]
			now, still := after[record.Key]
			if !known || !still {
				t.Fatalf("block %d: its sweep names coin %s, which was not there before it or is not there after it", tip.Index, record.Key)
			}
			if now.Amount+now.Held != was.Amount {
				t.Fatalf("block %d: a coin of %s was swept and now holds %s with %s set aside; the two must make what it held, the fifth being set aside for its owner and not taken",
					tip.Index, FormatUnits(was.Amount), FormatUnits(now.Amount), FormatUnits(now.Held))
			}
			if now.Age != tip.Index || now.Recipient != was.Recipient {
				t.Fatalf("block %d: a swept coin has age %d and owner %s; want age %d and its owner unchanged", tip.Index, now.Age, now.Recipient, tip.Index)
			}
			expired += was.Held
			if was.Held > 0 {
				goneToVault++
				goneToVaultAt[tip.Index] = true
			}
			if now.Held > 0 {
				setAside++
				setAsideAt[tip.Index] = true
			}
		}
		// Which coins the sweep must have touched is worked out here from
		// the coins themselves, not from the sweep's own record. A coin that
		// was there before the block, was not spent in it, and was made or
		// last swept one interval ago is due. Of those the sweep changes
		// the ones with something set aside and the ones large enough for
		// a fifth to be taken, and sets aside a fifth rounded down or
		// nothing. Every other coin is exactly as it was.
		recordedBySweep := map[string]bool{}
		for _, record := range swept {
			recordedBySweep[record.Key] = true
		}
		interval, smallest := Params.Economics.SweepIntervalBlocks, minimumOutputUnits()
		for key, was := range before {
			now, still := after[key]
			if !still {
				continue // spent in this block
			}
			newFifth := int64(0)
			if was.Amount-was.Amount/5 >= smallest {
				newFifth = was.Amount / 5
			}
			due := tip.Index-interval >= 1 && was.Age == tip.Index-interval && (was.Held > 0 || newFifth > 0)
			if due != recordedBySweep[key] {
				t.Fatalf("block %d: coin %s (%s with %s set aside, age %d) due: %v, swept: %v", tip.Index, key, FormatUnits(was.Amount), FormatUnits(was.Held), was.Age, due, recordedBySweep[key])
			}
			if !due {
				if now != was {
					t.Fatalf("block %d: coin %s was not due and was not spent, and it changed from %+v to %+v", tip.Index, key, was, now)
				}
				continue
			}
			if now.Held != newFifth || now.Amount != was.Amount-newFifth {
				t.Fatalf("block %d: a coin of %d units was left %d with %d set aside; want %d with %d (a fifth rounded down, or none if less than the smallest coin would be left)",
					tip.Index, was.Amount, now.Amount, now.Held, was.Amount-newFifth, newFifth)
			}
			if newFifth == 0 {
				tooSmallForAFifth++
			}
		}
		// The vault after the block: what it held, less its payout, plus
		// the fifths whose seven years were up, plus what the block did not
		// claim. Nothing else: no part of a fee goes to it.
		accounting := accountForBlock(tip.Index, fees, vaultBefore)
		var claimed int64
		for _, output := range tip.Transactions[0].Outputs {
			claimed += output.Amount.Units()
		}
		if want := accounting.VaultAfterUnits + expired + accounting.MinerAllowanceUnits() - claimed; VaultBalanceUnits() != want {
			t.Fatalf("block %d: the vault holds %s; want %s (%s set aside one date ago went to it, and no fifth set aside at this block)",
				tip.Index, FormatUnits(VaultBalanceUnits()), FormatUnits(want), FormatUnits(expired))
		}
		if accounting.VaultPayoutUnits > 0 {
			vaultPaidOut = true
		}
		conserved(fmt.Sprintf("after block %d", tip.Index))
		fingerprints[tip.Index] = coinSetFingerprint()

		// Now and then, take some blocks off and put them back.
		if step%9 == 0 {
			depth := int64(1 + random.Intn(8))
			if depth > tip.Index {
				depth = tip.Index
			}
			for h := tip.Index - depth + 1; h <= tip.Index; h++ {
				if setAsideAt[h] {
					rollbacksOverSettingAside++
				}
				if goneToVaultAt[h] {
					rollbacksOverExpiry++
				}
				if takenBackAt[h] {
					rollbacksOverTakingBack++
				}
			}
			StateMutex.Lock()
			removed := rollbackChainToHeightLocked(tip.Index - depth)
			StateMutex.Unlock()
			if got := GetLatestBlockInternal().Index; got != tip.Index-depth {
				t.Fatalf("a rollback of %d blocks from %d stopped at %d", depth, tip.Index, got)
			}
			conserved(fmt.Sprintf("after rolling back %d blocks from %d", depth, tip.Index))
			if coinSetFingerprint() != fingerprints[tip.Index-depth] {
				t.Fatalf("rolling back to block %d did not give back the coins that were there at block %d", tip.Index-depth, tip.Index-depth)
			}
			StateMutex.Lock()
			restoreChainLocked(tip.Index-depth, removed)
			StateMutex.Unlock()
			if GetLatestBlockInternal().Hash != tip.Hash {
				t.Fatalf("putting %d blocks back did not reach the same tip", depth)
			}
			if coinSetFingerprint() != fingerprints[tip.Index] {
				t.Fatalf("putting %d blocks back did not give back the same coins", depth)
			}
			rollbacks++
		}
	}

	// The run must really have exercised what it claims to.
	tip := GetLatestBlockInternal()
	if tip.Index <= Params.Economics.EmissionEndHeight() || transfersMade < 10 || underClaimed == 0 || !vaultPaidOut || rollbacks < 5 {
		t.Fatalf("the run did not cover enough: height %d (emission ends %d), %d transfers, %d under-claimed blocks, vault paid out %v, %d rollbacks",
			tip.Index, Params.Economics.EmissionEndHeight(), transfersMade, underClaimed, vaultPaidOut, rollbacks)
	}
	if setAside < 5 || takenBack < 3 || goneToVault < 3 || rollbacksOverSettingAside < 1 || rollbacksOverExpiry < 1 ||
		takenBackWithAFee < 2 || takenBackByTheKeeper < 1 || rollbacksOverTakingBack < 1 {
		t.Fatalf("the run did not cover enough of the fifths set aside: %d set aside, %d taken back by spending the coin (%d of them in a transfer that paid a fee, %d by the keeper's renewals), %d gone to the vault at the next date, %d rollbacks over a block that set one aside, %d over a block that sent one to the vault, %d over a block that took one back",
			setAside, takenBack, takenBackWithAFee, takenBackByTheKeeper, goneToVault, rollbacksOverSettingAside, rollbacksOverExpiry, rollbacksOverTakingBack)
	}
	t.Logf("height %d, %d transfers, %d under-claimed blocks, %d rollbacks, vault %s; fifths: %d set aside, %d taken back (%d with a fee, %d by the keeper), %d gone to the vault, %d sweeps of a coin too small for a new fifth; rollbacks over setting aside %d, over expiry %d, over taking back %d",
		tip.Index, transfersMade, underClaimed, rollbacks, FormatUnits(VaultBalanceUnits()), setAside, takenBack, takenBackWithAFee, takenBackByTheKeeper, goneToVault, tooSmallForAFifth,
		rollbacksOverSettingAside, rollbacksOverExpiry, rollbacksOverTakingBack)
}
