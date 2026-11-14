package node

import (
	"encoding/json"
	"fmt"

	"go.etcd.io/bbolt"
)

// THE SABBATICAL SWEEP
//
// Every coin (unspent output) has an age: the height of the block that
// created it. A coin that has not been spent for SweepIntervalBlocks (seven
// years) is swept at the end of the block that reaches that age:
//
//   - a fixed share of it (one fifth) moves into the vault;
//   - the coin keeps the rest and its age is reset to that block, so it is
//     next looked at one interval later;
//   - if what would remain is below the minimum output size, the whole coin
//     moves into the vault and the coin ceases to exist.
//
// Spending a coin, to anyone including its own owner, creates new coins with
// a fresh age. So an owner who still holds the key never loses anything by
// moving their coins once per interval; only coins nobody can or does move
// are gradually returned to circulation through the vault.
//
// The sweep happens at the end of the block: a transfer inside that same
// block still spends the coin at its full value.
//
// Which coins are due at a height needs no separate index. A coin due at
// height h was either created by block h-interval or re-aged by the sweep of
// block h-interval, and both of those are already stored.

var sweepBucket = []byte("sweeps")

// SweptCoin records one coin a block's sweep touched, so the sweep can be
// reversed if the block is removed and so the coin can be found again one
// interval later.
type SweptCoin struct {
	Key    string     `json:"key"`
	Before UTXORecord `json:"before"`
	// Kept is true when the coin still exists after the sweep (with a
	// reduced amount and a new age), false when it was swept in full.
	Kept bool `json:"kept"`
}

// sweepEnabled reports whether this network sweeps long-unmoved coins.
func sweepEnabled() bool {
	economics := Params.Economics
	return economics != nil && economics.SweepIntervalBlocks > 0 && economics.SweepDivisor > 0
}

// sweepAmounts splits a coin of `units` into the part swept into the vault
// and the part the coin keeps.
func sweepAmounts(units int64) (swept, kept int64) {
	if units <= 0 {
		return 0, 0
	}
	swept = units / Params.Economics.SweepDivisor
	kept = units - swept
	if kept < minimumOutputUnits() {
		return units, 0
	}
	return swept, kept
}

func loadSweepRecords(dbTx *bbolt.Tx, height int64) []SweptCoin {
	bucket := dbTx.Bucket(sweepBucket)
	if bucket == nil {
		return nil
	}
	raw := bucket.Get(chainWorkKey(height))
	if raw == nil {
		return nil
	}
	var records []SweptCoin
	_ = json.Unmarshal(raw, &records)
	return records
}

func storeSweepRecords(dbTx *bbolt.Tx, height int64, records []SweptCoin) error {
	bucket, err := dbTx.CreateBucketIfNotExists(sweepBucket)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return bucket.Delete(chainWorkKey(height))
	}
	raw, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return bucket.Put(chainWorkKey(height), raw)
}

// applySweep sweeps every coin that reaches the sweep age at `block`. It must
// run in the write transaction that stores the block, after the block's own
// coins have been written. It returns the units moved into the vault.
func applySweep(dbTx *bbolt.Tx, bUTXO *bbolt.Bucket, block Block) (int64, error) {
	if !sweepEnabled() {
		return 0, nil
	}
	bornAt := block.Index - Params.Economics.SweepIntervalBlocks
	if bornAt < 1 {
		return 0, storeSweepRecords(dbTx, block.Index, nil)
	}

	// Candidates: coins created by the block at bornAt, then coins re-aged by
	// that block's sweep. The order is fixed, so every node does the same.
	var candidates []string
	if bBlocks := dbTx.Bucket(blocksBucket); bBlocks != nil {
		if data := bBlocks.Get(chainWorkKey(bornAt)); data != nil {
			var born Block
			if err := json.Unmarshal(data, &born); err != nil {
				return 0, fmt.Errorf("block #%d unreadable during the sweep", bornAt)
			}
			for _, tx := range born.Transactions {
				for outIdx := range tx.Outputs {
					candidates = append(candidates, utxoKey(tx.ID, outIdx))
				}
			}
		}
	}
	for _, earlier := range loadSweepRecords(dbTx, bornAt) {
		if earlier.Kept {
			candidates = append(candidates, earlier.Key)
		}
	}

	var total int64
	var records []SweptCoin
	seen := make(map[string]bool, len(candidates))
	for _, key := range candidates {
		if seen[key] {
			continue
		}
		seen[key] = true
		raw := bUTXO.Get([]byte(key))
		if raw == nil {
			continue // already spent
		}
		var coin UTXORecord
		if json.Unmarshal(raw, &coin) != nil || coin.Age != bornAt {
			continue
		}
		swept, kept := sweepAmounts(coin.Amount.Units())
		if swept == 0 {
			continue
		}
		if kept == 0 {
			if err := bUTXO.Delete([]byte(key)); err != nil {
				return 0, err
			}
		} else {
			after, _ := json.Marshal(UTXORecord{Amount: UnitsAmount(kept), Recipient: coin.Recipient, Age: block.Index})
			if err := bUTXO.Put([]byte(key), after); err != nil {
				return 0, err
			}
		}
		var addErr error
		if total, addErr = addUnits(total, swept); addErr != nil {
			return 0, addErr
		}
		records = append(records, SweptCoin{Key: key, Before: coin, Kept: kept > 0})
	}
	return total, storeSweepRecords(dbTx, block.Index, records)
}

// undoSweep puts back every coin the sweep of the block at `height` touched.
// It must run before the rest of that block's coin changes are undone.
func undoSweep(dbTx *bbolt.Tx, bUTXO *bbolt.Bucket, height int64) {
	for _, record := range loadSweepRecords(dbTx, height) {
		if raw, err := json.Marshal(record.Before); err == nil {
			_ = bUTXO.Put([]byte(record.Key), raw)
		}
	}
	if bucket := dbTx.Bucket(sweepBucket); bucket != nil {
		_ = bucket.Delete(chainWorkKey(height))
	}
}

// nextSweepHeight is the block at whose end a coin of this age will be swept
// if it has not been spent by then; 0 when the network does not sweep.
func nextSweepHeight(age int64) int64 {
	if !sweepEnabled() || age < 1 {
		return 0
	}
	return age + Params.Economics.SweepIntervalBlocks
}
