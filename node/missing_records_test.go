package node

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"go.etcd.io/bbolt"
)

// Outside review, round 10, second look. A record that has gone MISSING used
// to be read as "this block spent nothing" or "this sweep touched nothing".
// These tests take a record away, or make it not fit its block, and check
// that the node gives the work up and changes nothing.

// chainState is everything a refused change must leave as it was: every coin
// (key and bytes), the newest block, and the stored height.
type chainState struct {
	coins  map[string]string
	tip    string
	stored string
	memory int64
}

func stateOfChain(t *testing.T) chainState {
	t.Helper()
	state := chainState{coins: map[string]string{}, tip: GetLatestBlockInternal().Hash}
	state.memory = int64(GetLatestBlockInternal().Index)
	if err := GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		state.stored = string(dbTx.Bucket(metaBucket).Get([]byte("height")))
		return dbTx.Bucket(utxoBucket).ForEach(func(key, raw []byte) error {
			state.coins[string(key)] = string(raw)
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	return state
}

// wantNothingChanged fails the test unless the chain is as `before` says.
func wantNothingChanged(t *testing.T, before chainState, what string) {
	t.Helper()
	if after := stateOfChain(t); !reflect.DeepEqual(before, after) {
		t.Fatalf("%s: the coins, the newest block or the stored height changed (tip %.12s to %.12s, height %s to %s, %d coins to %d)",
			what, before.tip, after.tip, before.stored, after.stored, len(before.coins), len(after.coins))
	}
}

// wantRollbackRefused rolls back to `target` and fails unless nothing came
// off and nothing changed.
func wantRollbackRefused(t *testing.T, target int64, what string) {
	t.Helper()
	before := stateOfChain(t)
	StateMutex.Lock()
	removed := rollbackChainToHeightLocked(target)
	StateMutex.Unlock()
	if len(removed) != 0 {
		t.Fatalf("%s: %d block(s) were removed; want the rollback refused", what, len(removed))
	}
	wantNothingChanged(t, before, what)
}

func updateDB(t *testing.T, change func(dbTx *bbolt.Tx) error) {
	t.Helper()
	if err := GlobalBoltEngine.Update(change); err != nil {
		t.Fatal(err)
	}
}

// chainWithOneTransfer: block 1 pays the owner, block 2 spends that coin, and
// the chain goes on to `height`.
func chainWithOneTransfer(t *testing.T, height int64) *int64 {
	t.Helper()
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner)
	pay, err := BuildSignedTransfer(privateKey, publicKey, testAddress("shop"), cvn("10"), 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, testAddress("other"), pay)
	mineTo(t, timestamp, height, testAddress("other"))
	return timestamp
}

// chainWithATransferOfTwoCoins: blocks 1 and 2 pay the owner, block 3 spends
// both coins in one transfer. The tip is block 3.
func chainWithATransferOfTwoCoins(t *testing.T) {
	t.Helper()
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner)
	mineNext(t, timestamp, owner)
	pay, err := BuildSignedTransfer(privateKey, publicKey, testAddress("shop"), cvn("70"), 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	if len(pay.Inputs) != 2 {
		t.Fatalf("set-up: the transfer names %d coins, want 2", len(pay.Inputs))
	}
	mineNext(t, timestamp, testAddress("other"), pay)
}

// A. The record of the coins a block spent is gone: the block is not removed,
// since its coins could never be put back.
func TestABlockWhoseRecordOfSpentCoinsIsMissingIsNotRemoved(t *testing.T) {
	chainWithOneTransfer(t, 2)
	updateDB(t, func(dbTx *bbolt.Tx) error {
		if dbTx.Bucket(undoBucket).Get(undoKey(2)) == nil {
			t.Fatal("set-up: block 2 has no record of the coins it spent")
		}
		return dbTx.Bucket(undoBucket).Delete(undoKey(2))
	})
	wantRollbackRefused(t, 1, "with block 2's record of spent coins missing")
}

// B. A record of spent coins that does not fit its block.
func TestARecordOfSpentCoinsThatDoesNotFitItsBlockStopsTheRollback(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, dbTx *bbolt.Tx, records []SpentRecord) []SpentRecord
	}{
		{"one coin of two left out", func(t *testing.T, dbTx *bbolt.Tx, records []SpentRecord) []SpentRecord {
			return records[:1]
		}},
		{"a coin named by another key", func(t *testing.T, dbTx *bbolt.Tx, records []SpentRecord) []SpentRecord {
			records[1].Key = utxoKey("TX_made_up", 0)
			return records
		}},
		{"two coins in the other order", func(t *testing.T, dbTx *bbolt.Tx, records []SpentRecord) []SpentRecord {
			records[0], records[1] = records[1], records[0]
			return records
		}},
		{"a coin added that the block did not spend", func(t *testing.T, dbTx *bbolt.Tx, records []SpentRecord) []SpentRecord {
			return append(records, SpentRecord{Key: utxoKey("TX_made_up", 0), Record: records[0].Record})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chainWithATransferOfTwoCoins(t)
			updateDB(t, func(dbTx *bbolt.Tx) error {
				records, err := loadUndoRecords(dbTx, 3)
				if err != nil || len(records) != 2 {
					t.Fatalf("set-up: block 3 has %d spent coins on record (%v), want 2", len(records), err)
				}
				return storeUndoRecords(dbTx, 3, c.change(t, dbTx, records))
			})
			wantRollbackRefused(t, 1, c.name)
		})
	}

	// A record present for a block that spent nothing.
	t.Run("a record under a block that spent nothing", func(t *testing.T) {
		chainWithATransferOfTwoCoins(t)
		updateDB(t, func(dbTx *bbolt.Tx) error {
			raw := dbTx.Bucket(undoBucket).Get(undoKey(3))
			return dbTx.Bucket(undoBucket).Put(undoKey(2), append([]byte(nil), raw...))
		})
		wantRollbackRefused(t, 1, "with a record of spent coins under block 2, which spent nothing")
	})
}

// C. A block that cannot be read, or is gone, stops the whole rollback.
func TestABlockThatCannotBeReadOrIsMissingStopsTheRollback(t *testing.T) {
	t.Run("a block that cannot be decoded", func(t *testing.T) {
		chainWithOneTransfer(t, 3)
		updateDB(t, func(dbTx *bbolt.Tx) error {
			return dbTx.Bucket(blocksBucket).Put(chainWorkKey(2), []byte("{"))
		})
		wantRollbackRefused(t, 1, "with block 2 unreadable")
	})
	t.Run("a block missing in the middle", func(t *testing.T) {
		chainWithOneTransfer(t, 3)
		updateDB(t, func(dbTx *bbolt.Tx) error {
			return dbTx.Bucket(blocksBucket).Delete(chainWorkKey(2))
		})
		wantRollbackRefused(t, 0, "with block 2 missing")
		// Block 3 is still stored, and its coins are still there.
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			if dbTx.Bucket(blocksBucket).Get(chainWorkKey(3)) == nil {
				t.Error("block 3 was taken off though the rollback was refused")
			}
			return nil
		})
		if tip := GetLatestBlockInternal(); tip.Index != 3 {
			t.Errorf("the newest block is #%d, want #3", tip.Index)
		}
	})
}

// D. On a network that sweeps, every block has a sweep record, an empty list
// where the sweep touched no coin.
func TestEveryBlockOfASweepingChainHasASweepRecordAndAnEmptyOneIsAnEmptyList(t *testing.T) {
	timestamp := startSweepChain(t)
	mineNext(t, timestamp, testAddress("idle"))
	mineTo(t, timestamp, 8, testAddress("other"))
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		bucket := dbTx.Bucket(sweepBucket)
		for h := int64(1); h <= 8; h++ {
			raw := bucket.Get(chainWorkKey(h))
			if raw == nil {
				t.Errorf("block %d has no sweep record", h)
				continue
			}
			touched := h >= 6 // coins are first due five blocks after they are made
			if touched && bytes.Equal(raw, []byte("[]")) {
				t.Errorf("block %d swept a coin but its record is an empty list", h)
			}
			if !touched && !bytes.Equal(raw, []byte("[]")) {
				t.Errorf("block %d swept nothing, and its record is %s, want []", h, raw)
			}
		}
		return nil
	})
}

// E. A sweep record that is missing stops the rollback, whether the sweep
// touched a coin or not.
func TestABlockWhoseSweepRecordIsMissingIsNotRemoved(t *testing.T) {
	for _, c := range []struct {
		name         string
		missing, to  int64
		touchedCoins bool
	}{
		{"a sweep that touched a coin", 6, 5, true},
		{"a sweep that touched nothing", 3, 2, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			timestamp := startSweepChain(t)
			idle := testAddress("idle")
			mineNext(t, timestamp, idle)
			mineTo(t, timestamp, 6, testAddress("other"))
			updateDB(t, func(dbTx *bbolt.Tx) error {
				raw := dbTx.Bucket(sweepBucket).Get(chainWorkKey(c.missing))
				if raw == nil || bytes.Equal(raw, []byte("[]")) == c.touchedCoins {
					t.Fatalf("set-up: the record at %d is %q", c.missing, raw)
				}
				return dbTx.Bucket(sweepBucket).Delete(chainWorkKey(c.missing))
			})
			wantRollbackRefused(t, c.to, "with the sweep record of block "+strconv.FormatInt(c.missing, 10)+" missing")
		})
	}
}

// F. Going forward: the next block sweeps what the block one interval back
// made, and needs that block and its sweep record.
func TestABlockIsRefusedWhenWhatItsSweepNeedsIsMissing(t *testing.T) {
	for _, c := range []struct {
		name   string
		remove func(dbTx *bbolt.Tx) error
	}{
		{"the sweep record of the block one interval back is missing", func(dbTx *bbolt.Tx) error {
			return dbTx.Bucket(sweepBucket).Delete(chainWorkKey(2))
		}},
		{"the block one interval back is missing", func(dbTx *bbolt.Tx) error {
			return dbTx.Bucket(blocksBucket).Delete(chainWorkKey(2))
		}},
		{"the block one interval back cannot be read", func(dbTx *bbolt.Tx) error {
			return dbTx.Bucket(blocksBucket).Put(chainWorkKey(2), []byte("{"))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			timestamp := startSweepChain(t)
			mineNext(t, timestamp, testAddress("idle"))
			mineTo(t, timestamp, 6, testAddress("other"))
			// Block 7 is due to sweep what block 2 made.
			*timestamp += 45
			next := mineTestBlock(t, *timestamp, testAddress("other"), nil, 0)
			updateDB(t, c.remove)
			before := stateOfChain(t)
			StateMutex.Lock()
			valid := ValidateBlockSignatures(next)
			if !valid {
				StateMutex.Unlock()
				t.Fatal("block 7 fails ValidateBlockSignatures on its own, so this test would prove nothing about the save")
			}
			stored := SaveBlockToStorageInternal(next)
			StateMutex.Unlock()
			if stored {
				t.Fatal("block 7 was stored though what its sweep needs is missing")
			}
			wantNothingChanged(t, before, "after the refused block")
		})
	}
}

// G. A rival for the newest block's place is judged against the coins as they
// were before that block, which the record of its spent coins gives.
func TestARivalBlockIsRefusedWhenTheTipsRecordOfSpentCoinsIsMissing(t *testing.T) {
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, owner)
	// The rival is built while block 1 is the tip: same parent as block 2.
	rival := mineTestBlock(t, *timestamp+46, testAddress("rival"), nil, 0)
	pay, err := BuildSignedTransfer(privateKey, publicKey, testAddress("shop"), cvn("10"), 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, testAddress("other"), pay)

	valid := func() bool {
		StateMutex.Lock()
		defer StateMutex.Unlock()
		return ValidateBlockSignatures(rival)
	}
	if !valid() {
		t.Fatal("set-up: a sound rival for block 2 was refused with every record in place")
	}
	updateDB(t, func(dbTx *bbolt.Tx) error { return dbTx.Bucket(undoBucket).Delete(undoKey(2)) })
	before := stateOfChain(t)
	if valid() {
		t.Fatal("a rival was accepted though the record of what block 2 spent is missing")
	}
	StateMutex.Lock()
	stored := SaveBlockToStorageInternal(rival)
	StateMutex.Unlock()
	if stored {
		t.Error("the rival was stored")
	}
	wantNothingChanged(t, before, "after the refused rival")
}

// G, directly: the coins as they were before the newest block cannot be
// worked out from a record that is missing.
func TestTheCoinsBeforeABlockCannotBeWorkedOutFromAMissingRecord(t *testing.T) {
	t.Run("the record of spent coins", func(t *testing.T) {
		chainWithOneTransfer(t, 2)
		tip := GetLatestBlockInternal()
		updateDB(t, func(dbTx *bbolt.Tx) error { return dbTx.Bucket(undoBucket).Delete(undoKey(2)) })
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			if _, err := lookupWithBlockUndone(dbTx, dbTx.Bucket(utxoBucket), tip); err == nil {
				t.Error("the coins before block 2 were worked out without its record of spent coins")
			}
			return nil
		})
	})
	t.Run("a record of spent coins that names another coin", func(t *testing.T) {
		chainWithOneTransfer(t, 2)
		tip := GetLatestBlockInternal()
		updateDB(t, func(dbTx *bbolt.Tx) error {
			records, err := loadUndoRecords(dbTx, 2)
			if err != nil || len(records) == 0 {
				t.Fatalf("set-up: block 2 has no record of spent coins (%v)", err)
			}
			records[0].Key = utxoKey("TX_made_up", 0)
			return storeUndoRecords(dbTx, 2, records)
		})
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			if _, err := lookupWithBlockUndone(dbTx, dbTx.Bucket(utxoBucket), tip); err == nil {
				t.Error("the coins before block 2 were worked out from a record naming another coin")
			}
			return nil
		})
	})
	t.Run("the record of a sweep", func(t *testing.T) {
		chainWithOneTransfer(t, 6)
		tip := GetLatestBlockInternal()
		updateDB(t, func(dbTx *bbolt.Tx) error { return dbTx.Bucket(sweepBucket).Delete(chainWorkKey(6)) })
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			if _, err := lookupWithBlockUndone(dbTx, dbTx.Bucket(utxoBucket), tip); err == nil {
				t.Error("the coins before block 6 were worked out without its sweep record")
			}
			return nil
		})
	})
}

// H. If the block already stored at a height cannot be read, nothing says
// whether a block for that height is "the same again", so it is not saved.
func TestASaveAtAHeightWhoseStoredBlockCannotBeReadIsRefused(t *testing.T) {
	timestamp := startSweepChain(t)
	mineNext(t, timestamp, testAddress("miner"))
	// A rival for block 2, built while block 1 is the tip.
	rival := mineTestBlock(t, *timestamp+46, testAddress("rival"), nil, 0)
	mineNext(t, timestamp, testAddress("other")) // the block that is stored at 2
	updateDB(t, func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(blocksBucket).Put(chainWorkKey(2), []byte("{"))
	})
	before := stateOfChain(t)
	StateMutex.Lock()
	stored := SaveBlockToStorageInternal(rival)
	StateMutex.Unlock()
	if stored {
		t.Fatal("a block was saved at a height whose stored block cannot be read")
	}
	wantNothingChanged(t, before, "after the refused save")
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		if got := dbTx.Bucket(blocksBucket).Get(chainWorkKey(2)); !bytes.Equal(got, []byte("{")) {
			t.Errorf("the block stored at 2 is now %.40q, want it left as it was", got)
		}
		return nil
	})
}

// A coin that is due at a block's sweep and cannot be read is not passed
// over as if it were not there: the block is refused.
func TestABlockIsRefusedWhenACoinDueAtItsSweepCannotBeRead(t *testing.T) {
	timestamp := startSweepChain(t)
	mineNext(t, timestamp, testAddress("idle"))
	mineTo(t, timestamp, 6, testAddress("other"))
	// Block 7 is due to sweep the coin that block 2 made.
	*timestamp += 45
	next := mineTestBlock(t, *timestamp, testAddress("other"), nil, 0)
	var key string
	updateDB(t, func(dbTx *bbolt.Tx) error {
		var block Block
		if err := json.Unmarshal(dbTx.Bucket(blocksBucket).Get(chainWorkKey(2)), &block); err != nil {
			return err
		}
		key = utxoKey(block.Transactions[0].ID, 0)
		if dbTx.Bucket(utxoBucket).Get([]byte(key)) == nil {
			t.Fatal("set-up: block 2's reward coin is not there")
		}
		return dbTx.Bucket(utxoBucket).Put([]byte(key), []byte("{"))
	})
	before := stateOfChain(t)
	StateMutex.Lock()
	valid := ValidateBlockSignatures(next)
	if !valid {
		StateMutex.Unlock()
		t.Fatal("block 7 fails ValidateBlockSignatures on its own, so this test would prove nothing about the save")
	}
	stored := SaveBlockToStorageInternal(next)
	StateMutex.Unlock()
	if stored {
		t.Fatal("block 7 was stored though a coin due at its sweep cannot be read")
	}
	wantNothingChanged(t, before, "after the refused block")
}
