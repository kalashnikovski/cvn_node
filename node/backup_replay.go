package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
)

// A FULL CHECK OF A CHAIN FILE
//
// checkChainFile holds a chain file's records to each other: the blocks
// follow one another, the vault after every block is what that block makes
// it, and the coins add up. It does not work the coins out again, so a file
// in which one address's coins were moved to another, with the totals left
// alone, passes it (outside review, round 10, finding 1).
//
// replayChainFile does the whole job. It starts an empty chain beside the
// node's own, gives it the file's blocks one by one through the same two
// steps that take a block from another node (ValidateBlockSignatures, then
// SaveBlockToStorageInternal: every rule, every signature, every proof of
// work), and then compares what those blocks made with what the file holds:
// every coin, the vault after every block, the records that put a block's
// spending and its sweep back when the block is removed, and the total work
// recorded for every block.
//
// There is no second copy of the rules here. The price is time: the proof of
// work of every block is repeated, which is minutes on a short chain and
// hours on a long one. So it is done when a backup is put back
// (`--restore-backup`), and by hand with `--check-backup-fully`; the copy a
// node makes each day gets the quick check only.
//
// It can only run in a program that has no chain open: the commands, never
// a running node. A node may be running beside it in another program; the
// scratch chain is a file of its own and is removed at the end. While it
// runs it borrows the settings a node keeps for its one chain. The file
// name, the height and the catching-up mark are put back at the end; the
// balances and the waiting transfers held in memory are not, which is of no
// account to a command that ends when the check does.
//
// What it does not compare, because a node does not rely on it: a vault
// figure or a total of work recorded for a block that is not on the chain
// (a node reads those only for the block they name), the node's own notes
// in the file (what it was downloading, which one-time repairs it has
// done), and the exact bytes of a record where the record means the same.

// replayProgressEvery is how often a long replay says how far it has got.
const replayProgressEvery = 30 * time.Second

// replayChainFile is the full check described above. The report is the one
// checkChainFile gives.
func replayChainFile(path, wantGenesis string) (report chainFileReport, err error) {
	if report, err = checkChainFile(path, wantGenesis); err != nil {
		return report, err
	}
	if wantGenesis == "" {
		return report, fmt.Errorf("%s: this network's first block is not known on this machine yet, so the file cannot be worked out again from its blocks. Start the node once so that it learns the first block, stop it, and try again", path)
	}
	if GlobalBoltEngine != nil {
		return report, fmt.Errorf("%s: a chain is open in this program, and the full check needs a program of its own", path)
	}

	// Scratch chains left by a check that was cut off are cleared away. One
	// that cannot be removed must not be built upon: its old blocks would
	// make a good file look bad.
	chainFile, heightBefore := BlockchainFile, atomic.LoadInt64(&localHeightState)
	scratch := fmt.Sprintf("%s.fullcheck-%d", chainFile, os.Getpid())
	stale, _ := filepath.Glob(chainFile + ".fullcheck-*")
	// The file to be checked must not be one of them: it would be cleared
	// away before it was read (outside review, round 10, second look,
	// finding 4). Told by the file itself and not by how its name is
	// written.
	given, givenErr := os.Stat(path)
	if givenErr != nil {
		return report, fmt.Errorf("%s cannot be read: %v", path, givenErr)
	}
	for _, old := range stale {
		if info, statErr := os.Stat(old); statErr == nil && os.SameFile(info, given) {
			return report, fmt.Errorf("%s has the kind of name the full check gives its own working file and clears away when it starts. Nothing was done to it. Give it another name and try again", path)
		}
	}
	for _, old := range stale {
		_ = os.Remove(old)
	}
	if _, statErr := os.Stat(scratch); statErr == nil {
		return report, fmt.Errorf("%s: the full check could not be run: %s is in the way and cannot be removed", path, scratch)
	}

	// The scratch chain takes the place of the node's own for as long as
	// this runs, and everything is put back whatever happens.
	BlockchainFile = scratch
	// Blocks stored while the node is catching up are not passed on to the
	// network; these must not be either.
	wasCatchingUp := atomic.SwapInt32(&IsSyncingActive, 1)
	defer func() {
		// A fault while the check runs is reported, with where it happened,
		// and never thrown: the caller must be told the file did not pass.
		if fault := recover(); fault != nil {
			err = fmt.Errorf("%s: the full check could not be run: %v\n%s", path, fault, debug.Stack())
		}
		if GlobalBoltEngine != nil {
			_ = GlobalBoltEngine.Close()
			GlobalBoltEngine = nil
		}
		BlockchainFile = chainFile
		atomic.StoreInt64(&localHeightState, heightBefore)
		atomic.StoreInt32(&IsSyncingActive, wasCatchingUp)
		if removeErr := os.Remove(scratch); removeErr != nil && !os.IsNotExist(removeErr) {
			fmt.Printf("   The check's scratch file %s could not be removed (%v). It is safe to delete.\n", scratch, removeErr)
		}
	}()
	InitBoltEngine()

	source, err := bbolt.Open(path, 0400, &bbolt.Options{ReadOnly: true, Timeout: 3 * time.Second})
	if err != nil {
		return report, fmt.Errorf("%s cannot be opened: %v", path, err)
	}
	defer source.Close()

	hashes := make([]string, report.Height+1)
	hashes[0] = GetLatestBlockInternal().Hash
	lastSaid := time.Now()
	for h := int64(1); h <= report.Height; h++ {
		var block Block
		if err := source.View(func(tx *bbolt.Tx) error {
			raw := tx.Bucket(blocksBucket).Get([]byte(fmt.Sprintf("%016d", h)))
			if raw == nil {
				return fmt.Errorf("missing")
			}
			return json.Unmarshal(raw, &block)
		}); err != nil {
			return report, fmt.Errorf("%s: block #%d cannot be read", path, h)
		}
		if block.Index != h || !placeReplayedBlock(block) {
			return report, fmt.Errorf("%s: block #%d is not a block this network's rules allow on top of the blocks before it. The file is not a chain a node could have built", path, h)
		}
		hashes[h] = block.Hash
		if time.Since(lastSaid) >= replayProgressEvery {
			lastSaid = time.Now()
			fmt.Printf("   ...block #%d of #%d checked.\n", h, report.Height)
		}
	}
	if err := compareFileWithItsBlocks(source, hashes); err != nil {
		return report, fmt.Errorf("%s: %v", path, err)
	}
	return report, nil
}

// placeReplayedBlock places one block of the file on the scratch chain as a
// block from another node is placed (placeOnTip, and the catching-up pass):
// on top of the newest block and nowhere else, checked in full, then stored.
func placeReplayedBlock(block Block) bool {
	StateMutex.Lock()
	defer StateMutex.Unlock()
	tip := GetLatestBlockInternal()
	return block.Index == tip.Index+1 && block.PrevHash == tip.Hash &&
		ValidateBlockSignatures(block) && SaveBlockToStorageInternal(block) &&
		GetLatestBlockInternal().Hash == block.Hash
}

// compareFileWithItsBlocks holds what a chain file records to what its
// blocks made in the scratch chain (GlobalBoltEngine): the coins, and for
// every block the vault after it, the total work through it, the coins it
// spent and the coins its sweep touched. hashes[h] is the identifier of
// block h.
func compareFileWithItsBlocks(source *bbolt.DB, hashes []string) error {
	return source.View(func(theirs *bbolt.Tx) error {
		return GlobalBoltEngine.View(func(ours *bbolt.Tx) error {
			theirCoins, ourCoins := theirs.Bucket(utxoBucket), ours.Bucket(utxoBucket)
			if theirCoins == nil || ourCoins == nil {
				return fmt.Errorf("the coins are missing")
			}
			inFile := 0
			if err := theirCoins.ForEach(func(key, raw []byte) error {
				inFile++
				var recorded, made UTXORecord
				ourRaw := ourCoins.Get(key)
				if ourRaw == nil {
					return fmt.Errorf("the file holds a coin, %s, that its blocks never made or have already spent. It has been altered or damaged", key)
				}
				if json.Unmarshal(raw, &recorded) != nil || json.Unmarshal(ourRaw, &made) != nil || recorded != made {
					return fmt.Errorf("the coin %s is not recorded as its blocks make it (its owner, its amount, its date or what is set aside on it). The file has been altered or damaged", key)
				}
				return nil
			}); err != nil {
				return err
			}
			made := 0
			_ = ourCoins.ForEach(func(_, _ []byte) error { made++; return nil })
			if made != inFile {
				return fmt.Errorf("the file holds %d coins and its blocks make %d: coins are missing from it. It has been altered or damaged", inFile, made)
			}

			for h := int64(0); h < int64(len(hashes)); h++ {
				// The total work a node records for a block is what it goes
				// by when it chooses between two chains. A figure that is
				// missing is worked out again from the blocks; one that is
				// there must be the right one.
				if recorded := readChainWork(theirs, h, hashes[h]); recorded != nil {
					if made := readChainWork(ours, h, hashes[h]); made == nil || recorded.Cmp(made) != 0 {
						return fmt.Errorf("the total work recorded through block #%d is not what the blocks make it. The file has been altered or damaged", h)
					}
				}
				if h == 0 {
					continue
				}
				theirVault, theirErr := readVaultUnits(theirs, h, hashes[h])
				ourVault, ourErr := readVaultUnits(ours, h, hashes[h])
				if theirErr != nil || ourErr != nil || theirVault != ourVault {
					return fmt.Errorf("the vault's balance recorded after block #%d is not what the blocks make it. The file has been altered or damaged", h)
				}
				theirSpent, theirErr := loadUndoRecords(theirs, h)
				ourSpent, ourErr := loadUndoRecords(ours, h)
				if theirErr != nil || ourErr != nil || !sameRecords(theirSpent, ourSpent) {
					return fmt.Errorf("the record of the coins block #%d spent is not what the block makes it. The file has been altered or damaged", h)
				}
				theirSwept, theirErr := loadSweepRecords(theirs, h)
				ourSwept, ourErr := loadSweepRecords(ours, h)
				if theirErr != nil || ourErr != nil || !sameRecords(theirSwept, ourSwept) {
					return fmt.Errorf("the record of the sweep of block #%d is not what the block makes it. The file has been altered or damaged", h)
				}
			}
			// A record of spending or of a sweep is read by its height alone,
			// so one that belongs to no block of the chain (above the newest
			// block, or under a name that is no height) is not let through.
			// A node removes these records with the block they belong to.
			for _, table := range []struct {
				name   []byte
				called string
			}{{undoBucket, "a record of spent coins"}, {sweepBucket, "a record of a sweep"}} {
				theirRecords, ourRecords := theirs.Bucket(table.name), ours.Bucket(table.name)
				if theirRecords == nil {
					continue
				}
				if err := theirRecords.ForEach(func(key, _ []byte) error {
					if ourRecords == nil || ourRecords.Get(key) == nil {
						return fmt.Errorf("the file holds %s, under %q, that none of its blocks makes. It has been altered or damaged", table.called, key)
					}
					return nil
				}); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// sameRecords reports whether two lists of records are the same, in the same
// order. No records and an empty list are the same thing.
func sameRecords[T any](a, b []T) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}
