package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// buildChainForBackup makes a short chain with a transfer and a fee in it,
// so that the coins, the vault and what was issued are three different sums.
func buildChainForBackup(t *testing.T) {
	t.Helper()
	startTestChain(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 5; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, owner, nil)
	}
	transfer, err := BuildSignedTransfer(privateKey, publicKey, testAddress("someone"), cvn("10"), cvn("5"), ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	extendTestChain(t, timestamp+45, testAddress("other"), []Transaction{transfer})
	extendTestChain(t, timestamp+90, owner, nil)
}

// In October 2026 the servers lost their chains and none of the backups
// could be used: they were copies of a file taken while it was being written,
// and opened with parts missing. A backup is now a snapshot taken through the
// database and checked before it is kept.
func TestABackupIsAWholeCheckedChain(t *testing.T) {
	buildChainForBackup(t)
	tip := GetLatestBlockInternal()
	directory := filepath.Join(t.TempDir(), "backups")

	path, report, err := writeBackup(directory, 7)
	if err != nil {
		t.Fatalf("the backup failed: %v", err)
	}
	if report.Height != tip.Index || report.TipHash != tip.Hash {
		t.Errorf("the backup is of block #%d, and the chain is at #%d", report.Height, tip.Index)
	}
	// A fifth of the 5 CVN fee went in; the block after paid a sliver out.
	if report.VaultUnits <= cvn("0.99") || report.VaultUnits >= cvn("1") {
		t.Errorf("the backup's vault holds %s CVN, want a little under 1", FormatUnits(report.VaultUnits))
	}
	if want := 7 * cvn("50"); report.CoinsUnits+report.VaultUnits != want {
		t.Errorf("the backup holds %s CVN in all, want %s", FormatUnits(report.CoinsUnits+report.VaultUnits), FormatUnits(want))
	}
	if !strings.Contains(filepath.Base(path), "chain_block00000007_") {
		t.Errorf("the backup is called %s", filepath.Base(path))
	}
	// The file checks by hand too, and no half-written file is left behind.
	if _, err := checkChainFile(path, buildGenesisBlock().Hash); err != nil {
		t.Errorf("the kept backup does not pass its own check: %v", err)
	}
	if entries, _ := os.ReadDir(directory); len(entries) != 1 {
		t.Errorf("the backup folder holds %d files, want the one backup", len(entries))
	}
	// A backup of another network's chain, or another start, is refused.
	if _, err := checkChainFile(path, strings.Repeat("ab", 32)); err == nil || !strings.Contains(err.Error(), "another network") {
		t.Errorf("a chain with a different first block gave: %v", err)
	}
}

func TestADamagedBackupIsFoundOut(t *testing.T) {
	buildChainForBackup(t)
	directory := t.TempDir()
	good, _, err := writeBackup(directory, 7)
	if err != nil {
		t.Fatal(err)
	}
	genesis := buildGenesisBlock().Hash

	damage := func(name string, change func(tx *bbolt.Tx) error) string {
		path := filepath.Join(directory, name)
		if err := copyFile(good, path); err != nil {
			t.Fatal(err)
		}
		db, err := bbolt.Open(path, 0600, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Update(change); err != nil {
			t.Fatal(err)
		}
		db.Close()
		return path
	}
	key := func(h int) []byte { return chainWorkKey(int64(h)) }

	cases := []struct {
		name, want string
		change     func(tx *bbolt.Tx) error
	}{
		// What happened in October: the part holding the coins was not there.
		{"no-coins.db", "parts of the chain are missing", func(tx *bbolt.Tx) error { return tx.DeleteBucket(utxoBucket) }},
		{"no-blocks.db", "parts of the chain are missing", func(tx *bbolt.Tx) error { return tx.DeleteBucket(blocksBucket) }},
		{"hole.db", "block #3 is missing", func(tx *bbolt.Tx) error { return tx.Bucket(blocksBucket).Delete(key(3)) }},
		{"cut-short.db", "newest block is #7 but its last stored block is #6", func(tx *bbolt.Tx) error { return tx.Bucket(blocksBucket).Delete(key(7)) }},
		{"altered.db", "block #4 is not what its identifier says", func(tx *bbolt.Tx) error {
			b := tx.Bucket(blocksBucket)
			raw := strings.Replace(string(b.Get(key(4))), `"amount":5000000000`, `"amount":5000000001`, 1)
			return b.Put(key(4), []byte(raw))
		}},
		{"coin-gone.db", "the coins do not add up", func(tx *bbolt.Tx) error {
			// Any coin worth something: the first block's coin is worth nothing.
			b := tx.Bucket(utxoBucket)
			cursor := b.Cursor()
			for key, raw := cursor.First(); key != nil; key, raw = cursor.Next() {
				var coin UTXORecord
				if json.Unmarshal(raw, &coin) == nil && coin.Amount.Units() > 0 {
					return b.Delete(key)
				}
			}
			return fmt.Errorf("no coin found to remove")
		}},
	}
	for _, c := range cases {
		path := damage(c.name, c.change)
		if _, err := checkChainFile(path, genesis); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: the check said %v, want it to say %q", c.name, err, c.want)
		}
	}
	// Not a chain file at all, an empty file, and no file.
	rubbish := filepath.Join(directory, "rubbish.db")
	_ = os.WriteFile(rubbish, []byte(strings.Repeat("not a database ", 500)), 0600)
	empty := filepath.Join(directory, "empty.db")
	_ = os.WriteFile(empty, nil, 0600)
	for _, path := range []string{rubbish, empty, filepath.Join(directory, "absent.db")} {
		if _, err := checkChainFile(path, genesis); err == nil {
			t.Errorf("%s passed the check", filepath.Base(path))
		}
	}
	// And the good one still passes: the check changes nothing.
	if _, err := checkChainFile(good, genesis); err != nil {
		t.Errorf("the good backup stopped passing: %v", err)
	}
}

func TestRestoringABackup(t *testing.T) {
	buildChainForBackup(t)
	directory := t.TempDir()
	backup, saved, err := writeBackup(directory, 7)
	if err != nil {
		t.Fatal(err)
	}
	genesis := buildGenesisBlock().Hash
	// The chain goes on after the backup.
	extendTestChain(t, GetLatestBlockInternal().Timestamp+45, testAddress("later"), nil)

	// With the node running, nothing is touched.
	if _, _, err := restoreBackup(backup, genesis); err == nil || !strings.Contains(err.Error(), "Stop it first") {
		t.Fatalf("restoring over a running node gave: %v", err)
	}
	if GetLatestBlockInternal().Index != saved.Height+1 {
		t.Fatal("the running chain was disturbed by a refused restore")
	}

	// Stopped: a damaged backup is refused and the chain file is left alone.
	GlobalBoltEngine.Close()
	GlobalBoltEngine = nil
	before, _ := os.Stat(BlockchainFile)
	rubbish := filepath.Join(directory, "rubbish.db")
	_ = os.WriteFile(rubbish, []byte(strings.Repeat("x", 9000)), 0600)
	if _, _, err := restoreBackup(rubbish, genesis); err == nil {
		t.Fatal("a file that is not a chain was restored")
	}
	if after, err := os.Stat(BlockchainFile); err != nil || after.Size() != before.Size() {
		t.Fatal("a refused restore changed the chain file")
	}

	// A good backup goes in, and the file it replaced is kept.
	report, kept, err := restoreBackup(backup, genesis)
	if err != nil {
		t.Fatalf("restoring a good backup: %v", err)
	}
	if report.Height != saved.Height || kept == "" {
		t.Errorf("restored to block #%d (kept: %q), want #%d and the old file kept", report.Height, kept, saved.Height)
	}
	if old, err := checkChainFile(kept, genesis); err != nil || old.Height != saved.Height+1 {
		t.Errorf("the file set aside is not the chain as it was: %v", err)
	}

	// The node opens the restored chain and carries on from it.
	InitBoltEngine()
	tip := GetLatestBlockInternal()
	if tip.Index != saved.Height || tip.Hash != saved.TipHash {
		t.Fatalf("after the restore the node is at block #%d, want #%d", tip.Index, saved.Height)
	}
	if VaultBalanceUnits() != saved.VaultUnits {
		t.Error("the vault did not come back with the chain")
	}
	extendTestChain(t, tip.Timestamp+45, testAddress("after"), nil)
	if GetLatestBlockInternal().Index != saved.Height+1 {
		t.Error("the restored chain would not take a new block")
	}
}

func TestOneBackupADayAndTheNewestKept(t *testing.T) {
	buildChainForBackup(t)
	directory := t.TempDir()
	now := time.Now()
	if !backupIsDue(directory, 24*time.Hour, now) {
		t.Error("with no backup yet, none was due")
	}
	var made []string
	for i := 0; i < 5; i++ {
		path, _, err := writeBackup(directory, 3)
		if err != nil {
			t.Fatal(err)
		}
		// Each a day older than the next, oldest first.
		age := now.Add(-time.Duration(5-i) * 24 * time.Hour)
		_ = os.Chtimes(path, age, age)
		made = append(made, path)
		extendTestChain(t, GetLatestBlockInternal().Timestamp+45, testAddress("m"), nil)
	}
	kept := listBackups(directory)
	if len(kept) != 3 || kept[0] != made[4] || kept[2] != made[2] {
		t.Errorf("kept %d backups, newest %s; want the newest three", len(kept), filepath.Base(kept[0]))
	}
	// The newest is a day old: one is due. Just made: none is.
	if !backupIsDue(directory, 24*time.Hour, now) {
		t.Error("a day after the last backup, none was due")
	}
	fresh := time.Now()
	_ = os.Chtimes(kept[0], fresh, fresh)
	if backupIsDue(directory, 24*time.Hour, fresh.Add(time.Hour)) {
		t.Error("an hour after a backup, another was due: a node restarted often would fill the disk")
	}
	// Another network's backups in the same folder are not counted or removed.
	other := filepath.Join(directory, "cvn_testnet_block00000009_20260101-000000.db")
	_ = os.WriteFile(other, []byte("x"), 0600)
	if len(listBackups(directory)) != 3 {
		t.Error("another network's backup was counted as this one's")
	}
}

func TestBackupSettings(t *testing.T) {
	if every, keep, on := backupSettings([]string{"cvn"}); !on || every != 24*time.Hour || keep != 7 {
		t.Errorf("by default: every %v, keep %d, on %v", every, keep, on)
	}
	if every, keep, on := backupSettings([]string{"cvn", "--backup-hours", "6", "--backup-keep", "30"}); !on || every != 6*time.Hour || keep != 30 {
		t.Errorf("as asked: every %v, keep %d, on %v", every, keep, on)
	}
	if _, _, on := backupSettings([]string{"cvn", "--no-backups"}); on {
		t.Error("--no-backups did not switch backups off")
	}
	// Nonsense falls back to the defaults.
	if every, keep, _ := backupSettings([]string{"cvn", "--backup-hours", "0", "--backup-keep", "-2"}); every != 24*time.Hour || keep != 7 {
		t.Errorf("nonsense gave every %v, keep %d", every, keep)
	}
}
