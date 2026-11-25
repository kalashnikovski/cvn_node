package node

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
)

// BACKUPS OF THE CHAIN
//
// The chain is one file (Params.DatabaseFile). A node that loses it downloads
// the chain again from the others, so on a network with other nodes a backup
// only saves time. It matters when there are no others: a network of one
// machine, or a fault that takes every node at once.
//
// Copying the file while the node runs does not make a backup. The node may
// be half-way through writing, and the copy then holds part of one state and
// part of another; it opens as damaged, or with parts missing, and is found
// to be useless only on the day it is needed. So the node makes its own:
//
//   - A snapshot is taken through the database itself, which gives the file
//     exactly as it stood at one moment, however much is being written.
//   - Every snapshot is checked before it is kept (checkChainFile): every
//     block is there and follows from the one before, each is what its
//     identifier says it is, and the coins add up to what has been issued. A
//     snapshot that fails is thrown away and the fault is reported.
//   - One is made a day, and the newest seven are kept.
//
// `--check-backup FILE` runs the same check by hand on any chain file, and
// `--restore-backup FILE` puts a checked backup in place of the chain, with
// the node stopped, keeping the file it replaces.
//
// A backup is a copy of public data. It holds no private key.

const (
	backupFolder       = "backups"
	backupDefaultHours = 24
	backupDefaultKeep  = 7
)

// chainFileReport is what checkChainFile found in a file that passed.
type chainFileReport struct {
	Height  int64
	TipHash string
	TipTime int64
	// CoinsUnits is what every coin gives when spent: its amount and
	// whatever a sweep has set aside on it. SetAsideUnits is the part of it
	// that is set aside.
	CoinsUnits    int64
	SetAsideUnits int64
	VaultUnits    int64
}

// checkChainFile opens a chain file without changing it and checks that it
// is a whole chain of this network:
//
//   - it has blocks, and the height it records is its newest block;
//   - its first block is this network's (when wantGenesis is given);
//   - every block from the first to the newest is present, is what its
//     identifier says, and names the block before it as its parent;
//   - the coins it holds (with what sweeps have set aside on them for their
//     owners), with the vault, are exactly what has been issued.
//
// It does not repeat the proof-of-work of every block: that takes hours on a
// long chain, and this is a check that a copy is whole, not that a stranger's
// chain is honest.
func checkChainFile(path, wantGenesis string) (chainFileReport, error) {
	var report chainFileReport
	if info, err := os.Stat(path); err != nil {
		return report, fmt.Errorf("%s cannot be read: %v", path, err)
	} else if info.Size() == 0 {
		return report, fmt.Errorf("%s is empty", path)
	}
	db, err := bbolt.Open(path, 0400, &bbolt.Options{ReadOnly: true, Timeout: 3 * time.Second})
	if err != nil {
		if strings.Contains(err.Error(), "timeout") {
			return report, fmt.Errorf("%s is in use by a running node. Stop the node first", path)
		}
		return report, fmt.Errorf("%s is not a chain file, or is damaged: %v", path, err)
	}
	defer db.Close()

	err = db.View(func(tx *bbolt.Tx) error {
		bBlocks, bMeta, bUTXO := tx.Bucket(blocksBucket), tx.Bucket(metaBucket), tx.Bucket(utxoBucket)
		if bBlocks == nil || bMeta == nil || bUTXO == nil {
			return fmt.Errorf("parts of the chain are missing from the file (blocks, records or coins). It is not a usable copy")
		}
		height, err := strconv.ParseInt(string(bMeta.Get([]byte("height"))), 10, 64)
		if err != nil || height < 0 {
			return fmt.Errorf("the file does not say what its newest block is")
		}
		if lastKey, _ := bBlocks.Cursor().Last(); lastKey == nil {
			return fmt.Errorf("the file holds no blocks")
		} else if string(lastKey) != fmt.Sprintf("%016d", height) {
			return fmt.Errorf("the file says its newest block is #%d but its last stored block is #%s", height, strings.TrimLeft(string(lastKey), "0"))
		}

		var previous Block
		var issued int64
		for h := int64(0); h <= height; h++ {
			raw := bBlocks.Get([]byte(fmt.Sprintf("%016d", h)))
			if raw == nil {
				return fmt.Errorf("block #%d is missing", h)
			}
			var block Block
			if err := json.Unmarshal(raw, &block); err != nil || block.Index != h {
				return fmt.Errorf("block #%d cannot be read", h)
			}
			if h == 0 {
				if wantGenesis != "" && block.Hash != wantGenesis {
					return fmt.Errorf("the file's first block is %.16s..., and this network's is %.16s...: it belongs to another network or another start", block.Hash, wantGenesis)
				}
			} else {
				if block.PrevHash != previous.Hash {
					return fmt.Errorf("block #%d does not follow block #%d", h, h-1)
				}
				if CalculateHash(block) != block.Hash {
					return fmt.Errorf("block #%d is not what its identifier says: its contents have been altered or damaged", h)
				}
				issued += BlockRewardUnits(h)
			}
			previous = block
		}
		report.Height, report.TipHash, report.TipTime = height, previous.Hash, previous.Timestamp

		if err := bUTXO.ForEach(func(_, raw []byte) error {
			var coin UTXORecord
			if err := json.Unmarshal(raw, &coin); err != nil || coin.HeldUnits < 0 || coin.HeldUnits > MaxAmountUnits ||
				coin.Amount.Units() < 0 || coin.Amount.Units() > MaxAmountUnits {
				return fmt.Errorf("a coin record cannot be read")
			}
			// Added up with a check at every step: a plain sum of figures
			// too large to be real could come round to the right total.
			var sumErr error
			if report.CoinsUnits, sumErr = addUnits(report.CoinsUnits, coin.SpendUnits()); sumErr != nil {
				return fmt.Errorf("the coins add up to more than can exist")
			}
			report.SetAsideUnits += coin.HeldUnits
			return nil
		}); err != nil {
			return err
		}
		if report.VaultUnits, err = readVaultUnits(tx, height, previous.Hash); err != nil {
			return fmt.Errorf("the vault's balance is missing for the newest block")
		}
		if report.CoinsUnits+report.VaultUnits != issued {
			return fmt.Errorf("the coins do not add up: %s CVN held and %s in the vault, but %s issued by block #%d",
				FormatUnits(report.CoinsUnits), FormatUnits(report.VaultUnits), FormatUnits(issued), height)
		}
		return nil
	})
	if err != nil {
		return report, fmt.Errorf("%s: %v", path, err)
	}
	return report, nil
}

func (r chainFileReport) String() string {
	setAside := ""
	if r.SetAsideUnits > 0 {
		setAside = fmt.Sprintf(" (of which %s CVN set aside at a seven-year date)", FormatUnits(r.SetAsideUnits))
	}
	return fmt.Sprintf("block #%d (%s), %s CVN held by addresses%s, %s CVN in the vault",
		r.Height, time.Unix(r.TipTime, 0).Format("2 January 2006 15:04"), FormatUnits(r.CoinsUnits), setAside, FormatUnits(r.VaultUnits))
}

// backupNamePrefix is what every backup of this network's chain is called,
// before the block number: "cvn_devnet_block".
func backupNamePrefix() string {
	return strings.TrimSuffix(filepath.Base(BlockchainFile), ".db") + "_block"
}

func backupDirectory() string {
	return filepath.Join(filepath.Dir(BlockchainFile), backupFolder)
}

// listBackups returns this network's backups in the folder, newest first.
func listBackups(directory string) []string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	type backup struct {
		path string
		made time.Time
	}
	var found []backup
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, backupNamePrefix()) || !strings.HasSuffix(name, ".db") {
			continue
		}
		if info, err := entry.Info(); err == nil {
			found = append(found, backup{filepath.Join(directory, name), info.ModTime()})
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].made.After(found[j].made) })
	paths := make([]string, len(found))
	for i, b := range found {
		paths[i] = b.path
	}
	return paths
}

// writeBackup takes a snapshot of the running node's chain into the folder,
// checks it, and keeps the newest `keep` backups. It returns the new file.
func writeBackup(directory string, keep int) (string, chainFileReport, error) {
	var report chainFileReport
	if GlobalBoltEngine == nil {
		return "", report, fmt.Errorf("the chain is not open")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", report, err
	}
	// Written under another name first: a file with a backup's name is
	// always a whole, checked backup, even if the power fails half-way.
	partial := filepath.Join(directory, backupNamePrefix()+".writing")
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return "", report, err
	}
	err = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		_, err := tx.WriteTo(file)
		return err
	})
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(partial)
		return "", report, fmt.Errorf("the snapshot could not be written: %v", err)
	}
	report, err = checkChainFile(partial, buildGenesisBlock().Hash)
	if err != nil {
		_ = os.Remove(partial)
		return "", report, fmt.Errorf("the snapshot did not pass its check and was not kept: %v", err)
	}
	final := filepath.Join(directory, fmt.Sprintf("%s%08d_%s.db", backupNamePrefix(), report.Height, time.Now().UTC().Format("20060102-150405")))
	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(partial)
		return "", report, err
	}
	if keep > 0 {
		for i, old := range listBackups(directory) {
			if i >= keep && old != final {
				_ = os.Remove(old)
			}
		}
	}
	return final, report, nil
}

// backupSettings reads the backup options from the command line:
//
//	--no-backups        make none
//	--backup-hours N    one every N hours (default 24)
//	--backup-keep N     keep the newest N (default 7)
func backupSettings(args []string) (every time.Duration, keep int, on bool) {
	for _, arg := range args {
		if arg == "--no-backups" {
			return 0, 0, false
		}
	}
	hours, keep := backupDefaultHours, backupDefaultKeep
	if n, err := strconv.Atoi(argumentValue(args, "--backup-hours")); err == nil && n >= 1 {
		hours = n
	}
	if n, err := strconv.Atoi(argumentValue(args, "--backup-keep")); err == nil && n >= 1 {
		keep = n
	}
	return time.Duration(hours) * time.Hour, keep, true
}

// backupIsDue reports whether the newest backup in the folder is older than
// the interval, or there is none. Going by the files and not by a timer
// means a node that is restarted often still makes one a day and no more.
func backupIsDue(directory string, every time.Duration, now time.Time) bool {
	backups := listBackups(directory)
	if len(backups) == 0 {
		return true
	}
	info, err := os.Stat(backups[0])
	return err != nil || now.Sub(info.ModTime()) >= every
}

// runBackupSchedule makes a backup whenever one is due, for as long as the
// node runs. It waits while the chain is being changed by a download.
func runBackupSchedule(every time.Duration, keep int) {
	directory := backupDirectory()
	time.Sleep(2 * time.Minute)
	for {
		if backupIsDue(directory, every, time.Now()) && atomic.LoadInt32(&IsSyncingActive) == 0 {
			if path, report, err := writeBackup(directory, keep); err != nil {
				announceNetworkStatus("⚠️ [BACKUP] No backup was made: " + err.Error())
			} else {
				announceNetworkStatus(fmt.Sprintf("💾 [BACKUP] The chain up to block #%d was saved and checked: %s", report.Height, path))
			}
		}
		time.Sleep(10 * time.Minute)
	}
}

// restoreBackup puts a backup in place of this network's chain file. The
// node must be stopped. The backup is checked first; the file it replaces is
// kept beside it under another name and its name is returned.
func restoreBackup(backupPath, wantGenesis string) (report chainFileReport, kept string, err error) {
	if report, err = checkChainFile(backupPath, wantGenesis); err != nil {
		return report, "", fmt.Errorf("the backup did not pass its check, so nothing was changed. %v", err)
	}
	if _, statErr := os.Stat(BlockchainFile); statErr == nil {
		// Opening it for writing fails while a node holds it.
		db, openErr := bbolt.Open(BlockchainFile, 0600, &bbolt.Options{Timeout: 2 * time.Second})
		if openErr != nil && strings.Contains(openErr.Error(), "timeout") {
			return report, "", fmt.Errorf("a node is running on %s. Stop it first; nothing was changed", BlockchainFile)
		}
		if openErr == nil {
			_ = db.Close()
		}
		kept = BlockchainFile + ".replaced-" + time.Now().UTC().Format("20060102-150405")
		if err := os.Rename(BlockchainFile, kept); err != nil {
			return report, "", fmt.Errorf("the present chain file could not be set aside, so nothing was changed: %v", err)
		}
	}
	partial := BlockchainFile + ".restoring"
	if err = copyFile(backupPath, partial); err == nil {
		err = os.Rename(partial, BlockchainFile)
	}
	if err != nil {
		_ = os.Remove(partial)
		if kept != "" {
			_ = os.Rename(kept, BlockchainFile)
		}
		return report, "", fmt.Errorf("the backup could not be copied into place; the chain file is as it was: %v", err)
	}
	return report, kept, nil
}

func copyFile(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(target, source); err == nil {
		err = target.Sync()
	}
	if closeErr := target.Close(); err == nil {
		err = closeErr
	}
	return err
}

// runBackupCommand handles `--check-backup FILE` and `--restore-backup FILE`.
// It reports whether one of them was given, in which case the program is done.
func runBackupCommand(args []string, genesisKnown bool) bool {
	wantGenesis := ""
	if genesisKnown {
		wantGenesis = buildGenesisBlock().Hash
	}
	if path := argumentValue(args, "--check-backup"); path != "" {
		report, err := checkChainFile(path, wantGenesis)
		if err != nil {
			fmt.Println("🚨 NOT USABLE. " + err.Error())
			os.Exit(1)
		}
		fmt.Println("✅ This file is a whole chain of the " + Params.Name + " network: " + report.String() + ".")
		if !genesisKnown {
			fmt.Println("   Its first block was not compared with the network's, which is not known on this machine yet.")
		}
		return true
	}
	if path := argumentValue(args, "--restore-backup"); path != "" {
		report, kept, err := restoreBackup(path, wantGenesis)
		if err != nil {
			fmt.Println("🚨 " + err.Error())
			os.Exit(1)
		}
		fmt.Println("✅ Restored " + BlockchainFile + " from the backup: " + report.String() + ".")
		if kept != "" {
			fmt.Println("   The file it replaced was kept as " + kept + ". Delete it once you are sure.")
		}
		fmt.Println("   Start the node as usual. It will fetch any later blocks from the network.")
		return true
	}
	return false
}
