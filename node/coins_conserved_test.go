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

// coinCount adds up every coin in the database, not just the coins of
// addresses a test knows about, and checks that none is zero or less.
func coinCount(t *testing.T) int64 {
	t.Helper()
	var total int64
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(utxoBucket).ForEach(func(key, raw []byte) error {
			var coin UTXORecord
			if err := json.Unmarshal(raw, &coin); err != nil {
				t.Fatalf("coin %s is unreadable: %v", key, err)
			}
			// The genesis block's output pays nothing and is never spent; it
			// is the one record allowed to hold zero.
			if coin.Amount.Units() < 0 || (coin.Amount.Units() == 0 && string(key) != "TX_GENESIS_0") {
				t.Fatalf("coin %s holds %d units", key, coin.Amount.Units())
			}
			total += coin.Amount.Units()
			return nil
		})
	})
	return total
}

// coinSetFingerprint is a hash of every coin record, byte for byte, in the
// database's own key order, and of the vault's balance. Two moments with the
// same fingerprint have exactly the same coins.
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
// moves coins: transfers with fees (part of each fee going to the vault),
// coins swept into the vault, the vault paying out, blocks that claim less
// than they may, the end of emission, and reorganisations. After every block,
// every coin there is plus the vault must equal exactly what the schedule has
// issued. Every rollback must give back, byte for byte, the coins that were
// there at that height, and putting the blocks back must give back the coins
// that were there before.
func TestCoinsAreNeverCreatedOrLost(t *testing.T) {
	startTestChain(t, "devnet")
	// Emission ends after block 30, so the last part of the run lives on fees
	// and the vault. Coins are swept after six blocks, and the vault pays a
	// seventh of itself each block, so both happen often.
	Params.Economics = &GenesisEconomics{
		BlockSeconds: 45, EpochBlocks: 10, EmissionEpochs: 3, InitialRewardUnits: cvn("50"),
		SweepIntervalBlocks: 6, SweepDivisor: 5,
		VaultPayoutBlocks: 7, FeeVaultDivisor: 5, MinOutputUnits: 10000, ReorgLimitBlocks: 100,
	}

	type wallet struct{ private, public, address string }
	var wallets []wallet
	for i := 0; i < 3; i++ {
		private, public, address, err := GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		wallets = append(wallets, wallet{private, public, address})
	}
	miners := []string{wallets[0].address, wallets[1].address, wallets[2].address, testAddress("outsider")}

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
			t.Fatalf("%s (height %d): coins %s plus vault %s make %s, but %s has been issued",
				when, height, FormatUnits(coins), FormatUnits(vault), FormatUnits(coins+vault), FormatUnits(issued))
		}
		if coins+vault > Params.Economics.TotalSupplyUnits() {
			t.Fatalf("%s: more than the whole supply exists", when)
		}
	}

	transfersMade, underClaimed, sweptAtSomePoint, vaultPaidOut, rollbacks := 0, 0, false, false, 0
	for step := 1; step <= 60; step++ {
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
		if accountForBlock(tip.Index, 0, vaultBefore).VaultPayoutUnits > 0 {
			vaultPaidOut = true
		}
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			if len(loadSweepRecords(dbTx, tip.Index)) > 0 {
				sweptAtSomePoint = true
			}
			return nil
		})
		conserved(fmt.Sprintf("after block %d", tip.Index))
		fingerprints[tip.Index] = coinSetFingerprint()

		// Now and then, take some blocks off and put them back.
		if step%9 == 0 {
			depth := int64(1 + random.Intn(8))
			if depth > tip.Index {
				depth = tip.Index
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
	if tip.Index <= Params.Economics.EmissionEndHeight() || transfersMade < 10 || underClaimed == 0 || !sweptAtSomePoint || !vaultPaidOut || rollbacks < 5 {
		t.Fatalf("the run did not cover enough: height %d (emission ends %d), %d transfers, %d under-claimed blocks, swept %v, vault paid out %v, %d rollbacks",
			tip.Index, Params.Economics.EmissionEndHeight(), transfersMade, underClaimed, sweptAtSomePoint, vaultPaidOut, rollbacks)
	}
	t.Logf("height %d, %d transfers, %d under-claimed blocks, %d rollbacks, vault %s", tip.Index, transfersMade, underClaimed, rollbacks, FormatUnits(VaultBalanceUnits()))
}
