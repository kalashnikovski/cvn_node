package node

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"go.etcd.io/bbolt"
)

// THE JUBILEE VAULT AND BLOCK ACCOUNTING
//
// The vault is a number kept by the rules, not an address. Nobody holds a key
// to it and nothing can spend from it. Coins enter it by rule and leave it by
// rule:
//
//   - In: the swept share of long-unmoved coins (see sweep.go), and
//     anything a block was entitled to pay its miner but did not. Nothing
//     the schedule issues is ever destroyed.
//   - Out: every block pays out vault / VaultPayoutBlocks, added to that
//     block's reward for its miner.
//
// No part of a fee goes to the vault by rule. Every fee in a block is its
// miner's. (A fifth of each fee once went to the vault while new coins were
// being issued. Giving is not to be compelled, and that share was removed.)
// A miner who claims less than a block allows gives the rest to the vault by
// its own choice.
//
// Order within a block, so every node computes the same figures: the payout
// is worked out from the vault as it stood after the previous block and taken
// out; then this block's sweep and anything it left unclaimed are put in.

var vaultBucket = []byte("vault")

type vaultEntry struct {
	Hash  string `json:"h"`
	Units int64  `json:"v"`
}

// blockAccounting is where every unit in one block comes from and goes to.
type blockAccounting struct {
	RewardUnits      int64 // newly issued by the schedule
	VaultPayoutUnits int64 // paid out of the vault to this block's miner
	FeeUnits         int64 // total fees of the block's transfers
	MinerFeeUnits    int64 // the fees the miner may claim: all of them
	VaultAfterUnits  int64 // vault balance once the payout is taken out
}

// SubsidyUnits is what the block pays apart from fees: the new reward plus
// the vault's payout.
func (a blockAccounting) SubsidyUnits() int64 { return a.RewardUnits + a.VaultPayoutUnits }

// MinerAllowanceUnits is the most the block's reward outputs may add up to:
// the new reward, the vault's payout and every fee in the block.
func (a blockAccounting) MinerAllowanceUnits() int64 { return a.SubsidyUnits() + a.MinerFeeUnits }

// vaultEnabled reports whether this network has a vault. Every network does;
// the question remains for a database opened before any network is chosen.
func vaultEnabled() bool { return Params.Economics != nil }

// accountForBlock works out the accounting of a block at `height` whose
// transfers pay feeUnits in total, given the vault balance just before it.
// The fees are the miner's whole, at every height. The vault's balance here
// is before the block's sweep and anything the block leaves unclaimed are
// added, which is done where the block is stored.
func accountForBlock(height, feeUnits, vaultBeforeUnits int64) blockAccounting {
	accounting := blockAccounting{
		RewardUnits:   BlockRewardUnits(height),
		FeeUnits:      feeUnits,
		MinerFeeUnits: feeUnits,
	}
	economics := Params.Economics
	if economics == nil {
		return accounting
	}
	if economics.VaultPayoutBlocks > 0 {
		accounting.VaultPayoutUnits = vaultBeforeUnits / economics.VaultPayoutBlocks
	}
	accounting.VaultAfterUnits = vaultBeforeUnits - accounting.VaultPayoutUnits
	return accounting
}

// minimumOutputUnits is the smallest amount a transfer output may carry.
func minimumOutputUnits() int64 {
	if Params.Economics != nil && Params.Economics.MinOutputUnits > 1 {
		return Params.Economics.MinOutputUnits
	}
	return 1
}

// reorgDepthAllowed reports whether a node may replace `depth` of its own
// most recent blocks with a competing branch. Networks with a limit refuse
// deeper replacements however much work the other branch shows, so settled
// history cannot be rewritten; a node that was genuinely left behind on a
// dead branch for longer than that has to be re-synced by hand.
func reorgDepthAllowed(depth int64) bool {
	if Params.Economics == nil || Params.Economics.ReorgLimitBlocks <= 0 {
		return true
	}
	return depth <= Params.Economics.ReorgLimitBlocks
}

// readVaultUnits returns the vault balance after the block at `height`, which
// must be the block with this hash. The vault is empty after the genesis block.
func readVaultUnits(dbTx *bbolt.Tx, height int64, blockHash string) (int64, error) {
	if !vaultEnabled() || height <= 0 {
		return 0, nil
	}
	bucket := dbTx.Bucket(vaultBucket)
	if bucket == nil {
		return 0, fmt.Errorf("vault record missing for block #%d", height)
	}
	raw := bucket.Get(chainWorkKey(height))
	var entry vaultEntry
	if raw == nil || json.Unmarshal(raw, &entry) != nil || entry.Hash != blockHash || entry.Units < 0 {
		return 0, fmt.Errorf("vault record missing for block #%d", height)
	}
	return entry.Units, nil
}

// storeVaultUnits records the vault balance after `block`, in the same write
// transaction that stores the block.
func storeVaultUnits(dbTx *bbolt.Tx, block Block, units int64) error {
	if !vaultEnabled() {
		return nil
	}
	bucket, err := dbTx.CreateBucketIfNotExists(vaultBucket)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(vaultEntry{Hash: block.Hash, Units: units})
	return bucket.Put(chainWorkKey(block.Index), raw)
}

// VaultBalanceUnits is the vault balance at this node's current tip.
func VaultBalanceUnits() int64 {
	if GlobalBoltEngine == nil || !vaultEnabled() {
		return 0
	}
	var units int64
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		height := atomic.LoadInt64(&localHeightState)
		bBlocks := dbTx.Bucket(blocksBucket)
		if bBlocks == nil {
			return nil
		}
		var tip Block
		if data := bBlocks.Get(chainWorkKey(height)); data != nil && json.Unmarshal(data, &tip) == nil {
			units, _ = readVaultUnits(dbTx, height, tip.Hash)
		}
		return nil
	})
	return units
}

// vaultUnitsAfter returns what the vault held after a given block. A miner
// building on that block uses this, and not the vault of whatever the tip
// has become since, so that the payout it writes into its block is the one
// every node will work out for that parent.
func vaultUnitsAfter(block Block) int64 {
	if GlobalBoltEngine == nil || !vaultEnabled() {
		return 0
	}
	var units int64
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		units, _ = readVaultUnits(dbTx, block.Index, block.Hash)
		return nil
	})
	return units
}
