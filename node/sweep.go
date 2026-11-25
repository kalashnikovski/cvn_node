package node

import (
	"encoding/json"
	"fmt"

	"go.etcd.io/bbolt"
)

// THE SABBATICAL SWEEP
//
// Every coin (unspent output) has an age: the height of the block that
// created it, or that last swept it. A coin that has not been spent for
// SweepIntervalBlocks (seven years) is swept at the end of the block that
// reaches that age:
//
//   - a fifth that was set aside on the coin at its last sweep, one
//     interval ago, and that its owner has not taken back, goes to the
//     vault. Its seven years are up;
//   - a fixed share of the coin's amount (one fifth) is taken off the
//     amount and set aside on the coin itself, for its owner. It does not
//     go to the vault;
//   - the coin's age is reset to that block, so it is next looked at one
//     interval later;
//   - a coin so small that what remained would be below the minimum output
//     size has no new fifth taken from it. If nothing was set aside on it
//     either, it is left exactly as it is, then and later.
//
// Spending a coin, to anyone including its own owner, spends its amount and
// whatever is set aside on it together, and creates new coins with a fresh
// age and nothing set aside. So an owner who still holds the key never
// loses anything by moving their coins once per interval, and one who
// missed a date has a whole further interval in which spending the coin
// takes the fifth back. Only what nobody comes for is returned to
// circulation through the vault, one interval after it was set aside.
//
// At every sweep a coin's amount is what it would be if the fifth went
// straight to the vault, as it once did. What differs is when the vault
// receives it, and that the owner can take it back meanwhile.
//
// The sweep happens at the end of the block: a transfer inside that same
// block still spends the coin as it stood, with anything set aside on it.
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
	// Kept is true when the coin still exists after the sweep, with a
	// reduced amount and a new age. It always does: no coin is swept whole.
	Kept bool `json:"kept"`
}

// sweepEnabled reports whether this network sweeps long-unmoved coins.
func sweepEnabled() bool {
	economics := Params.Economics
	return economics != nil && economics.SweepIntervalBlocks > 0 && economics.SweepDivisor > 0
}

// sweepAmounts splits a coin of `units` into the part swept into the vault
// and the part the coin keeps.
//
// A coin so small that what it kept would be less than the smallest coin
// allowed is left alone: nothing is taken from it, and it is not looked at
// again. An earlier form of the rule took such a coin whole, which took
// everything from the smallest holdings while taking a fifth from the rest.
func sweepAmounts(units int64) (swept, kept int64) {
	if units <= 0 {
		return 0, 0
	}
	swept = units / Params.Economics.SweepDivisor
	kept = units - swept
	if kept < minimumOutputUnits() {
		return 0, units
	}
	return swept, kept
}

// loadSweepRecords returns what the sweep of the block at `height` recorded:
// nothing if it touched no coin. A record that is there and cannot be read
// is an error and never "nothing": the record is what finds a swept coin
// again at its next date and what puts a sweep back when its block is
// removed, so a node that passed over a damaged one would go on with coins
// that no other node has (outside review, round 10, finding 2).
func loadSweepRecords(dbTx *bbolt.Tx, height int64) ([]SweptCoin, error) {
	bucket := dbTx.Bucket(sweepBucket)
	if bucket == nil {
		return nil, nil
	}
	raw := bucket.Get(chainWorkKey(height))
	if raw == nil {
		return nil, nil
	}
	var records []SweptCoin
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("the record of the sweep of block #%d cannot be read: this node's database is damaged", height)
	}
	return records, nil
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
// coins have been written. It returns the units that go to the vault (the
// fifths set aside one interval ago and not taken back; never the fifths it
// sets aside now) and whether it changed any coin.
func applySweep(dbTx *bbolt.Tx, bUTXO *bbolt.Bucket, block Block) (toVaultUnits int64, changedCoins bool, err error) {
	if !sweepEnabled() {
		return 0, false, nil
	}
	bornAt := block.Index - Params.Economics.SweepIntervalBlocks
	if bornAt < 1 {
		return 0, false, storeSweepRecords(dbTx, block.Index, nil)
	}

	// Candidates: coins created by the block at bornAt, then coins re-aged by
	// that block's sweep. The order is fixed, so every node does the same.
	var candidates []string
	if bBlocks := dbTx.Bucket(blocksBucket); bBlocks != nil {
		if data := bBlocks.Get(chainWorkKey(bornAt)); data != nil {
			var born Block
			if err := json.Unmarshal(data, &born); err != nil {
				return 0, false, fmt.Errorf("block #%d unreadable during the sweep", bornAt)
			}
			for _, tx := range born.Transactions {
				for outIdx := range tx.Outputs {
					candidates = append(candidates, utxoKey(tx.ID, outIdx))
				}
			}
		}
	}
	earlierSweep, err := loadSweepRecords(dbTx, bornAt)
	if err != nil {
		return 0, false, err
	}
	for _, earlier := range earlierSweep {
		if earlier.Kept {
			candidates = append(candidates, earlier.Key)
		}
	}

	var records []SweptCoin
	seen := make(map[string]bool, len(candidates))
	for _, key := range candidates {
		if seen[key] {
			continue
		}
		seen[key] = true
		raw := bUTXO.Get([]byte(key))
		if raw == nil {
			continue // already spent, and anything set aside on it with it
		}
		var coin UTXORecord
		if json.Unmarshal(raw, &coin) != nil || coin.Age != bornAt {
			continue
		}
		if coin.HeldUnits < 0 {
			// Cannot arise: only this function writes the figure.
			return 0, false, fmt.Errorf("coin %s is recorded with %d units set aside, which cannot be: this node's database is damaged", key, coin.HeldUnits)
		}
		// What was set aside one interval ago was not taken back: it is the
		// vault's now.
		expired := coin.HeldUnits
		// A new fifth is set aside, unless the coin is too small for one
		// (swept is then nought and kept is the whole amount).
		swept, kept := sweepAmounts(coin.Amount.Units())
		if expired == 0 && swept == 0 {
			continue // too small, and nothing set aside: left as it is
		}
		after, marshalErr := json.Marshal(UTXORecord{Amount: UnitsAmount(kept), Recipient: coin.Recipient, Age: block.Index, HeldUnits: swept})
		if marshalErr != nil {
			return 0, false, marshalErr
		}
		if err := bUTXO.Put([]byte(key), after); err != nil {
			return 0, false, err
		}
		if toVaultUnits, err = addUnits(toVaultUnits, expired); err != nil {
			return 0, false, err
		}
		// Kept is what brings the coin up again one interval later; the
		// whole coin as it was is what lets the sweep be undone.
		records = append(records, SweptCoin{Key: key, Before: coin, Kept: true})
	}
	return toVaultUnits, len(records) > 0, storeSweepRecords(dbTx, block.Index, records)
}

// undoSweep puts back every coin the sweep of the block at `height` touched.
// It must run before the rest of that block's coin changes are undone. If
// the sweep's record cannot be read, or a coin cannot be written back, it
// returns the error and the caller must give up the whole change: a sweep
// half undone leaves coins that belong to no chain.
func undoSweep(dbTx *bbolt.Tx, bUTXO *bbolt.Bucket, height int64) error {
	records, err := loadSweepRecords(dbTx, height)
	if err != nil {
		return err
	}
	for _, record := range records {
		raw, err := json.Marshal(record.Before)
		if err != nil {
			return err
		}
		if err := bUTXO.Put([]byte(record.Key), raw); err != nil {
			return err
		}
	}
	if bucket := dbTx.Bucket(sweepBucket); bucket != nil {
		return bucket.Delete(chainWorkKey(height))
	}
	return nil
}

// nextSweepHeight is the block at whose end the sweep will next change a
// coin if it has not been spent by then: a fifth of it set aside, or the
// fifth already set aside sent to the vault, or both. It is 0 when nothing
// will happen to the coin: on a network that does not sweep, and for a coin
// that has nothing set aside and is too small for a fifth to be taken.
func nextSweepHeight(coin UTXORecord) int64 {
	if !sweepEnabled() || coin.Age < 1 {
		return 0
	}
	if swept, _ := sweepAmounts(coin.Amount.Units()); swept == 0 && coin.HeldUnits == 0 {
		return 0
	}
	return coin.Age + Params.Economics.SweepIntervalBlocks
}
