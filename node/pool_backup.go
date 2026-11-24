package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"go.etcd.io/bbolt"
)

// COPIES OF THE POOL'S RECORD
//
// The pool's record is one file, and the only place that says what each
// address is owed for work it has done. A node that loses its chain
// downloads it again; a pool that loses its record cannot ask anybody. So
// the pool keeps copies of it, made the way a node makes backups of its
// chain (backup.go), and in the same folder:
//
//   - A snapshot is taken through the database itself, which gives the
//     record exactly as it stood at one moment. Copying the file while the
//     pool runs would not.
//   - Every snapshot is checked before it is kept (checkPoolRecordFile): all
//     of it can be read, and it adds up. A snapshot that fails is thrown
//     away and the fault is reported.
//   - One is made every hour, and one straight after payments have been
//     sent, so that the newest copy always knows of every payment made. The
//     newest 48 are kept. `--no-backups` switches them off with the chain's.
//
// `--check-pool-backup FILE` runs the same check by hand on any copy, and
// `--restore-pool-backup FILE` puts a checked copy in place of the record,
// with the pool stopped, keeping the file it replaces.
//
// A copy is the record as it stood when the copy was made. Put back later,
// it knows nothing of what happened since. The payments made since are on
// the chain, and the pool takes them into the record by itself when it
// starts (pool_chain.go), so nobody is paid twice. The work done since, and
// the division of blocks found since, are nowhere else and are lost.
//
// A copy holds no key. It holds what the pool page shows.

// How often a copy is made, how many are kept, and how long the pool waits
// after a payment is sent before it makes one: payments are handed in one
// after another, and one copy should hold them all. Variables so tests need
// not wait.
var (
	poolBackupEvery     = time.Hour
	poolBackupsKept     = 48
	poolBackupSettleFor = 20 * time.Second
	// How long to wait for a record's file before taking it to be in use
	// by a running pool.
	poolRecordOpenWait = 3 * time.Second
)

// poolRecordReport is what checkPoolRecordFile found in a file that passed.
type poolRecordReport struct {
	Shares                           int
	Blocks, Waiting, Confirmed, Lost int
	NewestBlock                      int64
	Addresses                        int
	EarnedUnits, OwedUnits           int64
	PendingUnits, PaidUnits          int64
	Payments, PaymentsUnderWay       int
	Coins                            int
	CoinUnits                        int64
	// ReadTo is how far the record had read the chain for the pool's own
	// transfers (pool_chain.go), or zero if it does not say.
	ReadTo int64
}

func (r poolRecordReport) String() string {
	return fmt.Sprintf("%d address(es) owed %s CVN, with %s CVN on its way and %s CVN paid; %d block(s) found, of which %d count, %d are waiting and %d were lost; %d payment(s); %d recent share(s)",
		r.Addresses, FormatUnits(r.OwedUnits), FormatUnits(r.PendingUnits), FormatUnits(r.PaidUnits),
		r.Blocks, r.Confirmed, r.Waiting, r.Lost, r.Payments, r.Shares)
}

// checkPoolRecordFile opens a pool's record without changing it and checks
// that it is whole and that it adds up:
//
//   - every share, block, account, payment and coin in it can be read;
//   - the parts of every block that counts add up to its reward;
//   - what each address has earned is its parts of the blocks that count,
//     and is what it is owed, has on its way and has been paid together;
//   - what is on its way and what has been paid are what the payments say;
//   - every coin set aside is set aside for a payment that is under way,
//     and each such payment has exactly the coins it spends;
//   - the coins the record holds are what it owes and has on its way.
//
// It does not compare the record with the chain: a copy may be checked on a
// machine that has no chain.
func checkPoolRecordFile(path string) (report poolRecordReport, err error) {
	// A file cut short can be opened and then fail under the reading of
	// it. That is a damaged copy, said so, and not the end of the program.
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		if fault := recover(); fault != nil {
			err = fmt.Errorf("%s is damaged and cannot be read through (%v)", path, fault)
		}
	}()
	if info, err := os.Stat(path); err != nil {
		return report, fmt.Errorf("%s cannot be read: %v", path, err)
	} else if info.Size() == 0 {
		return report, fmt.Errorf("%s is empty", path)
	}
	db, err := bbolt.Open(path, 0400, &bbolt.Options{ReadOnly: true, Timeout: poolRecordOpenWait})
	if err != nil {
		if strings.Contains(err.Error(), "timeout") {
			return report, fmt.Errorf("%s is in use by a running pool. Check one of its copies in the %s folder, or stop the pool first", path, backupFolder)
		}
		return report, fmt.Errorf("%s is not a pool's record, or is damaged: %v", path, err)
	}
	defer db.Close()

	err = db.View(func(tx *bbolt.Tx) error {
		bShares, bBlocks, bAccounts := tx.Bucket(poolSharesBucket), tx.Bucket(poolBlocksBucket), tx.Bucket(poolAccountsBucket)
		bPayments, bCoins := tx.Bucket(poolPaymentsBucket), tx.Bucket(poolCoinsBucket)
		if bShares == nil || bBlocks == nil || bAccounts == nil || bPayments == nil || bCoins == nil {
			return fmt.Errorf("parts of the record are missing from the file (shares, blocks, accounts, payments or coins). It is not a usable copy")
		}

		if err := bShares.ForEach(func(_, raw []byte) error {
			var share poolShare
			if json.Unmarshal(raw, &share) != nil || share.Weight <= 0 || share.Address == "" {
				return fmt.Errorf("a share cannot be read")
			}
			report.Shares++
			return nil
		}); err != nil {
			return err
		}

		// What the blocks and the payments say each address should have.
		earned, pending, paid := make(map[string]int64), make(map[string]int64), make(map[string]int64)
		if err := bBlocks.ForEach(func(_, raw []byte) error {
			var block poolBlock
			if json.Unmarshal(raw, &block) != nil || block.Hash == "" || block.Height < 1 {
				return fmt.Errorf("a block the pool found cannot be read")
			}
			report.Blocks++
			if block.Height > report.NewestBlock {
				report.NewestBlock = block.Height
			}
			switch block.State {
			case poolBlockWaiting:
				report.Waiting++
			case poolBlockLost:
				report.Lost++
			case poolBlockConfirmed:
				report.Confirmed++
				var parts int64
				for _, part := range block.Parts {
					if part.Units > 0 {
						parts += part.Units
						earned[part.Address] += part.Units
					}
				}
				if parts != block.RewardUnits {
					return fmt.Errorf("the parts of block #%d add up to %s CVN and its reward was %s", block.Height, FormatUnits(parts), FormatUnits(block.RewardUnits))
				}
			default:
				return fmt.Errorf("block #%d is neither waiting, counted nor lost", block.Height)
			}
			return nil
		}); err != nil {
			return err
		}

		underWay := make(map[uint64]poolPayment)
		takenFromChain := false
		if err := bPayments.ForEach(func(_, raw []byte) error {
			var payment poolPayment
			if json.Unmarshal(raw, &payment) != nil || payment.Serial == 0 {
				return fmt.Errorf("a payment cannot be read")
			}
			report.Payments++
			for _, line := range payment.Lines {
				if line.Address == "" || line.Receives < 0 || line.Debit < line.Receives || line.Beyond < 0 || line.Beyond > line.Debit {
					return fmt.Errorf("payment %d has a line that does not hang together", payment.Serial)
				}
			}
			switch payment.State {
			case poolPaymentPrepared, poolPaymentSent:
				report.PaymentsUnderWay++
				underWay[payment.Serial] = payment
				for _, line := range payment.Lines {
					pending[line.Address] += line.Debit
				}
			case poolPaymentSettled:
				takenFromChain = takenFromChain || payment.FromChain
				for _, line := range payment.Lines {
					paid[line.Address] += line.Debit
					earned[line.Address] += line.Beyond
				}
			case poolPaymentReturned:
			default:
				return fmt.Errorf("payment %d is in no known state", payment.Serial)
			}
			return nil
		}); err != nil {
			return err
		}

		accounted := make(map[string]bool)
		if err := bAccounts.ForEach(func(key, raw []byte) error {
			address := string(key)
			var account poolAccount
			if json.Unmarshal(raw, &account) != nil {
				return fmt.Errorf("the account of %s cannot be read", address)
			}
			if account.Owed < 0 || account.Pending < 0 || account.Paid < 0 || account.Earned != account.Owed+account.Pending+account.Paid {
				return fmt.Errorf("the account of %s does not add up: earned %s CVN, owed %s, on its way %s, paid %s",
					address, FormatUnits(account.Earned), FormatUnits(account.Owed), FormatUnits(account.Pending), FormatUnits(account.Paid))
			}
			if account.Earned != earned[address] {
				return fmt.Errorf("the record says %s has earned %s CVN, and the blocks that count give it %s", address, FormatUnits(account.Earned), FormatUnits(earned[address]))
			}
			if account.Pending != pending[address] {
				return fmt.Errorf("the record says %s has %s CVN on its way, and the payments under way hold %s for it", address, FormatUnits(account.Pending), FormatUnits(pending[address]))
			}
			if account.Paid != paid[address] {
				return fmt.Errorf("the record says %s has been paid %s CVN, and the settled payments paid it %s", address, FormatUnits(account.Paid), FormatUnits(paid[address]))
			}
			accounted[address] = true
			report.Addresses++
			report.EarnedUnits += account.Earned
			report.OwedUnits += account.Owed
			report.PendingUnits += account.Pending
			report.PaidUnits += account.Paid
			return nil
		}); err != nil {
			return err
		}
		for _, figures := range []map[string]int64{earned, pending, paid} {
			for address, units := range figures {
				if units != 0 && !accounted[address] {
					return fmt.Errorf("%s has a part in a block or a payment and no account", address)
				}
			}
		}

		held := make(map[uint64]int64)
		if err := bCoins.ForEach(func(key, raw []byte) error {
			var coin poolCoin
			if json.Unmarshal(raw, &coin) != nil || coin.Units <= 0 || coin.key() != string(key) {
				return fmt.Errorf("one of the pool's coins cannot be read")
			}
			if coin.HeldBy != 0 {
				if _, known := underWay[coin.HeldBy]; !known {
					return fmt.Errorf("a coin is set aside for payment %d, which is not under way", coin.HeldBy)
				}
				held[coin.HeldBy] += coin.Units
			}
			report.Coins++
			report.CoinUnits += coin.Units
			return nil
		}); err != nil {
			return err
		}
		for serial, payment := range underWay {
			if spends := payment.debits() + payment.ChangeUnits; held[serial] != spends {
				return fmt.Errorf("payment %d spends %s CVN and has coins of %s set aside for it", serial, FormatUnits(spends), FormatUnits(held[serial]))
			}
		}
		// A record that has taken a payment in from the chain may hold more
		// or less than it owes: such a payment may have spent coins the
		// record never knew (pool_chain.go).
		if !takenFromChain && report.CoinUnits != report.OwedUnits+report.PendingUnits {
			return fmt.Errorf("the record holds coins of %s CVN, and owes %s with %s on its way", FormatUnits(report.CoinUnits), FormatUnits(report.OwedUnits), FormatUnits(report.PendingUnits))
		}
		if meta := tx.Bucket(poolMetaBucket); meta != nil {
			report.ReadTo = poolReadToFrom(meta)
		}
		return nil
	})
	if err != nil {
		return report, fmt.Errorf("%s: %v", path, err)
	}
	return report, nil
}

// poolBackupNamePrefix is what every copy of this network's pool record is
// called, before the time it was made: "pool_devnet_".
func poolBackupNamePrefix() string {
	return "pool_" + Params.Name + "_"
}

// A copy is named after the moment it was made, in universal time, with a
// number for copies made within one second: pool_devnet_20261124-101500-00.db.
// The names alone put the copies in order, whatever the file system says of
// when each file was written.
const poolBackupTimeForm = "20060102-150405"

// listPoolBackups returns this network's copies in the folder, newest first.
func listPoolBackups(directory string) []string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, poolBackupNamePrefix()) || !strings.HasSuffix(name, ".db") {
			continue
		}
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	paths := make([]string, len(names))
	for i, name := range names {
		paths[i] = filepath.Join(directory, name)
	}
	return paths
}

// poolBackupMade is when a copy was made, by its name.
func poolBackupMade(path string) (time.Time, bool) {
	name := strings.TrimPrefix(filepath.Base(path), poolBackupNamePrefix())
	if len(name) < len(poolBackupTimeForm) {
		return time.Time{}, false
	}
	made, err := time.ParseInLocation(poolBackupTimeForm, name[:len(poolBackupTimeForm)], time.UTC)
	return made, err == nil
}

// writeBackup takes a snapshot of the record into the folder, checks it, and
// keeps the newest `keep` copies. It returns the new file.
func (r *poolRecord) writeBackup(directory string, keep int) (string, poolRecordReport, error) {
	var report poolRecordReport
	r.Lock()
	db := r.db
	r.Unlock()
	if db == nil {
		return "", report, fmt.Errorf("the pool's record is closed")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", report, err
	}
	// Written under another name first: a file with a copy's name is always
	// a whole, checked copy, even if the power fails half-way.
	partial := filepath.Join(directory, poolBackupNamePrefix()+"writing")
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return "", report, err
	}
	err = db.View(func(tx *bbolt.Tx) error {
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
	if report, err = checkPoolRecordFile(partial); err != nil {
		// Kept under a name that says what it is: a record that does not
		// add up is something its operator will want to look at.
		failed := filepath.Join(directory, poolBackupNamePrefix()+"failed-check")
		_ = os.Remove(failed)
		if os.Rename(partial, failed) != nil {
			_ = os.Remove(partial)
		}
		return "", report, fmt.Errorf("the snapshot did not pass its check and was not kept as a copy: %v", err)
	}
	made := time.Now().UTC().Format(poolBackupTimeForm)
	final := ""
	for n := 0; n < 100; n++ {
		final = filepath.Join(directory, fmt.Sprintf("%s%s-%02d.db", poolBackupNamePrefix(), made, n))
		if _, err := os.Stat(final); err != nil {
			break
		}
	}
	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(partial)
		return "", report, err
	}
	prunePoolBackups(directory, keep)
	return final, report, nil
}

// prunePoolBackups removes all but the newest `keep` copies. One that cannot
// be removed just now, because another program is reading it, is left for
// the next time.
func prunePoolBackups(directory string, keep int) {
	if keep <= 0 {
		return
	}
	for i, old := range listPoolBackups(directory) {
		if i >= keep {
			_ = os.Remove(old)
		}
	}
}

// poolBackupWanted is told when a copy should be made now and not at the
// next hour: payments have just been sent.
var poolBackupWanted = make(chan struct{}, 1)

// poolBackupSoon asks for a copy of the record to be made shortly.
func poolBackupSoon() {
	select {
	case poolBackupWanted <- struct{}{}:
	default:
	}
}

// poolBackupIsDue reports whether the newest copy in the folder is older
// than the interval, or there is none.
func poolBackupIsDue(directory string, every time.Duration, now time.Time) bool {
	backups := listPoolBackups(directory)
	if len(backups) == 0 {
		return true
	}
	made, ok := poolBackupMade(backups[0])
	return !ok || now.Sub(made) >= every
}

// runPoolBackupSchedule makes a copy of the pool's record whenever one is
// due or asked for, for as long as the pool runs.
func runPoolBackupSchedule(every time.Duration, keep int) {
	directory := backupDirectory()
	for {
		asked := false
		select {
		case <-poolBackupWanted:
			asked = true
			time.Sleep(poolBackupSettleFor)
			select {
			case <-poolBackupWanted:
			default:
			}
		case <-time.After(time.Minute):
		}
		record := poolLedger
		if record == nil || (!asked && !poolBackupIsDue(directory, every, time.Now())) {
			continue
		}
		if path, report, err := record.writeBackup(directory, keep); err != nil {
			announceNetworkStatus("⚠️ [POOL] No copy of the pool's record was made: " + err.Error())
		} else {
			noteNetworkEvent(fmt.Sprintf("💾 [POOL] The pool's record was saved and checked (%s CVN owed to %d address(es)): %s", FormatUnits(report.OwedUnits), report.Addresses, path))
		}
	}
}

// runPoolBackupCommand handles `--check-pool-backup FILE` and
// `--restore-pool-backup FILE`. It reports whether one of them was given, in
// which case the program is done.
func runPoolBackupCommand(args []string) bool {
	if path := argumentValue(args, "--check-pool-backup"); path != "" {
		report, err := checkPoolRecordFile(path)
		if err != nil {
			fmt.Println("🚨 NOT USABLE. " + err.Error())
			os.Exit(1)
		}
		fmt.Println("✅ This file is a whole pool's record, and it adds up: " + report.String() + ".")
		if made, ok := poolBackupMade(path); ok {
			fmt.Println("   It was made on " + made.Local().Format("2 January 2006 at 15:04") + ". It knows nothing of what the pool did after that.")
		}
		return true
	}
	if path := argumentValue(args, "--restore-pool-backup"); path != "" {
		report, kept, err := restorePoolBackup(path)
		if err != nil {
			fmt.Println("🚨 " + err.Error())
			os.Exit(1)
		}
		fmt.Println("✅ Restored " + poolLedgerPath() + " from the copy: " + report.String() + ".")
		if kept != "" {
			fmt.Println("   The file it replaced was kept as " + kept + ". Delete it once you are sure.")
		}
		fmt.Println("   Start the pool as usual. Before it pays anybody it reads the chain for every payment the")
		fmt.Println("   pool's address has made since this copy, and takes them into the record, so that nobody is")
		fmt.Println("   paid twice. Work done after the copy was made, and the division of any block found after it,")
		fmt.Println("   are not in the copy and cannot be got back: POOL_GUIDE.md says what to do about those.")
		return true
	}
	return false
}

// restorePoolBackup puts a copy in place of this network's pool record. The
// pool must be stopped. The copy is checked first; the file it replaces is
// kept beside it under another name, and that name is returned.
func restorePoolBackup(backupPath string) (report poolRecordReport, kept string, err error) {
	if report, err = checkPoolRecordFile(backupPath); err != nil {
		return report, "", fmt.Errorf("the copy did not pass its check, so nothing was changed. %v", err)
	}
	record := poolLedgerPath()
	if _, statErr := os.Stat(record); statErr == nil {
		// Opening it for writing fails while a pool holds it.
		db, openErr := bbolt.Open(record, 0600, &bbolt.Options{Timeout: poolRecordOpenWait})
		if openErr != nil && strings.Contains(openErr.Error(), "timeout") {
			return report, "", fmt.Errorf("a pool is running on %s. Stop it first; nothing was changed", record)
		}
		if openErr == nil {
			_ = db.Close()
		}
		kept = record + ".replaced-" + time.Now().UTC().Format("20060102-150405")
		if err := os.Rename(record, kept); err != nil {
			return report, "", fmt.Errorf("the present record could not be set aside, so nothing was changed: %v", err)
		}
	}
	partial := record + ".restoring"
	if err = copyFile(backupPath, partial); err == nil {
		err = os.Rename(partial, record)
	}
	if err != nil {
		_ = os.Remove(partial)
		if kept != "" {
			_ = os.Rename(kept, record)
		}
		return report, "", fmt.Errorf("the copy could not be put in place; the record is as it was: %v", err)
	}
	return report, kept, nil
}
