package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"go.etcd.io/bbolt"
)

// The full check of a chain file (backup_replay.go) works the chain out again
// from the file's blocks. These tests build one chain that has a block that
// claims less than it may, an ordinary transfer, and sweeps, close it, and
// alter copies of its file in ways the quick check (checkChainFile) cannot
// see.

// fullCheckChain is a chain built for these tests, closed, with what it was.
type fullCheckChain struct {
	path, genesis string
	tip           Block
	fingerprint   string
	// The height of the block with the transfer in it, and the first height
	// at which a sweep took something.
	transferHeight, sweepHeight int64
}

// buildChainForFullCheck mines a chain on a network that sweeps every five
// blocks. Block 1 claims 40 of its 50 CVN, so the vault is not empty. Block 2
// holds a signed transfer, so it has a record of the coin it spent. The chain
// goes on to block 14, so that coins are swept, and swept a second time. The
// engine is left open.
func buildChainForFullCheck(t *testing.T) fullCheckChain {
	t.Helper()
	timestamp := startSweepChain(t)
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	*timestamp += 45
	first := mineTestBlock(t, *timestamp, owner, nil, cvn("40"))
	if !ValidateBlockSignatures(first) || !SaveBlockToStorageInternal(first) {
		t.Fatal("set-up: the block that claims less than it may was refused")
	}
	pay, err := BuildSignedTransfer(privateKey, publicKey, testAddress("shop"), cvn("10"), 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	mineNext(t, timestamp, testAddress("other"), pay)
	mineTo(t, timestamp, 14, testAddress("other"))

	chain := fullCheckChain{
		path: BlockchainFile, genesis: buildGenesisBlock().Hash,
		tip: GetLatestBlockInternal(), fingerprint: coinSetFingerprint(), transferHeight: 2,
	}
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		if records, err := loadUndoRecords(dbTx, chain.transferHeight); err != nil || len(records) == 0 {
			t.Fatalf("set-up: block %d has no record of the coins it spent (%v)", chain.transferHeight, err)
		}
		for h := int64(1); h <= chain.tip.Index; h++ {
			if records, _ := loadSweepRecords(dbTx, h); len(records) > 0 {
				chain.sweepHeight = h
				break
			}
		}
		return nil
	})
	if chain.sweepHeight == 0 {
		t.Fatal("set-up: no block swept anything")
	}
	if vaultRecord(t) <= 0 {
		t.Fatal("set-up: the vault is empty")
	}
	return chain
}

// closeTheEngine closes the open chain, as a program that is not a running
// node has it.
func closeTheEngine(t *testing.T) {
	t.Helper()
	if GlobalBoltEngine == nil {
		return
	}
	if err := GlobalBoltEngine.Close(); err != nil {
		t.Fatal(err)
	}
	GlobalBoltEngine = nil
}

// alteredCopyOf copies a closed chain file and makes a change inside the copy.
func alteredCopyOf(t *testing.T, good, name string, change func(tx *bbolt.Tx) error) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
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
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// coinsWorthSomething is the keys of the coins that hold an amount, in the
// order the database keeps them.
func coinsWorthSomething(tx *bbolt.Tx) []string {
	var keys []string
	_ = tx.Bucket(utxoBucket).ForEach(func(key, raw []byte) error {
		var coin UTXORecord
		if json.Unmarshal(raw, &coin) == nil && coin.Amount.Units() > 1 {
			keys = append(keys, string(key))
		}
		return nil
	})
	return keys
}

func readCoinRecord(t *testing.T, tx *bbolt.Tx, key string) UTXORecord {
	t.Helper()
	var coin UTXORecord
	if err := json.Unmarshal(tx.Bucket(utxoBucket).Get([]byte(key)), &coin); err != nil {
		t.Fatalf("coin %s cannot be read: %v", key, err)
	}
	return coin
}

func writeCoinRecord(tx *bbolt.Tx, key string, coin UTXORecord) error {
	raw, err := json.Marshal(coin)
	if err != nil {
		return err
	}
	return tx.Bucket(utxoBucket).Put([]byte(key), raw)
}

// theFileAsTheNodeWroteItPasses fails the test unless the closed chain file
// passes both checks. It is the guard that makes a failure after an
// alteration mean something.
func theFileAsTheNodeWroteItPasses(t *testing.T, chain fullCheckChain) {
	t.Helper()
	if _, err := replayChainFile(chain.path, chain.genesis); err != nil {
		t.Fatalf("set-up: the chain file as the node wrote it fails its full check: %v", err)
	}
}

func TestAChainFileAsTheNodeWroteItPassesTheFullCheck(t *testing.T) {
	chain := buildChainForFullCheck(t)
	fileBefore := BlockchainFile
	syncingBefore := atomic.LoadInt32(&IsSyncingActive)
	heightBefore := atomic.LoadInt64(&localHeightState)
	closeTheEngine(t)

	report, err := replayChainFile(chain.path, chain.genesis)
	if err != nil {
		t.Fatalf("the chain file as the node wrote it failed its full check: %v", err)
	}
	if report.Height != chain.tip.Index || report.TipHash != chain.tip.Hash {
		t.Errorf("the report says block #%d (%.12s), the chain is at #%d (%.12s)", report.Height, report.TipHash, chain.tip.Index, chain.tip.Hash)
	}
	// Everything the check borrowed is put back.
	if BlockchainFile != fileBefore {
		t.Errorf("the chain file is now %s, was %s", BlockchainFile, fileBefore)
	}
	if GlobalBoltEngine != nil {
		t.Error("a chain is left open by the full check")
		closeTheEngine(t)
	}
	if now := atomic.LoadInt32(&IsSyncingActive); now != syncingBefore {
		t.Errorf("IsSyncingActive is %d after the check, was %d", now, syncingBefore)
	}
	if now := atomic.LoadInt64(&localHeightState); now != heightBefore {
		t.Errorf("the node's height is %d after the check, was %d", now, heightBefore)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(chain.path), "*.fullcheck-*")); len(left) != 0 {
		t.Errorf("the check left its scratch file behind: %v", left)
	}

	// The node's own chain is as it was.
	InitBoltEngine()
	if tip := GetLatestBlockInternal(); tip.Hash != chain.tip.Hash {
		t.Errorf("after the check the node is at block #%d, want #%d", tip.Index, chain.tip.Index)
	}
	if coinSetFingerprint() != chain.fingerprint {
		t.Error("the coins are not what they were before the check")
	}
}

// The quick check holds the records of a file to each other, and every
// record here is still held to the others. The coin is what it was in total,
// and only whose it is has changed. (Outside review, round 10, finding 1.)
func TestAChainFileWithACoinGivenToAnotherAddressPassesTheQuickCheckAndFailsTheFullOne(t *testing.T) {
	chain := buildChainForFullCheck(t)
	closeTheEngine(t)
	theFileAsTheNodeWroteItPasses(t, chain)

	var key string
	path := alteredCopyOf(t, chain.path, "given-away.db", func(tx *bbolt.Tx) error {
		key = coinsWorthSomething(tx)[0]
		coin := readCoinRecord(t, tx, key)
		coin.Recipient = testAddress("a thief")
		return writeCoinRecord(tx, key, coin)
	})
	if _, err := checkChainFile(path, chain.genesis); err != nil {
		t.Fatalf("the quick check refused the file: %v. This test is about what the quick check cannot see", err)
	}
	if _, err := replayChainFile(path, chain.genesis); err == nil || !strings.Contains(err.Error(), key) {
		t.Fatalf("a file with coin %s given to another address passed the full check, or failed without naming it: %v", key, err)
	}
}

func TestAChainFileWithRecordsAlteredButTheTotalsKeptFailsTheFullCheck(t *testing.T) {
	chain := buildChainForFullCheck(t)
	closeTheEngine(t)
	theFileAsTheNodeWroteItPasses(t, chain)

	// A middle block, and the newest one, for the total work recorded.
	middle, newest := chain.tip.Index/2, chain.tip.Index
	// A height with a record of spent coins and a height with a record of a
	// sweep, and the same records copied to heights no block has.
	beyond := chain.tip.Index + 5
	var spentOnNothing int64
	if db, err := bbolt.Open(chain.path, 0400, &bbolt.Options{ReadOnly: true}); err != nil {
		t.Fatal(err)
	} else {
		_ = db.View(func(tx *bbolt.Tx) error {
			for h := int64(1); h <= chain.tip.Index; h++ {
				if records, _ := loadUndoRecords(tx, h); len(records) == 0 {
					spentOnNothing = h
					break
				}
			}
			return nil
		})
		db.Close()
	}
	if spentOnNothing == 0 {
		t.Fatal("set-up: every block spent something")
	}
	// raiseWork keeps the entry's block and makes its total work far larger.
	raiseWork := func(tx *bbolt.Tx, height int64) error {
		bucket := tx.Bucket(chainWorkBucket)
		var entry chainWorkEntry
		if err := json.Unmarshal(bucket.Get(chainWorkKey(height)), &entry); err != nil {
			return err
		}
		entry.Work = "9" + strings.Repeat("9", 40)
		raw, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return bucket.Put(chainWorkKey(height), raw)
	}

	cases := []struct {
		name   string
		says   string // what the full check names
		change func(t *testing.T, tx *bbolt.Tx) error
	}{
		{"the total work recorded for a middle block raised", "total work recorded through block #" + strconv.FormatInt(middle, 10), func(t *testing.T, tx *bbolt.Tx) error {
			return raiseWork(tx, middle)
		}},
		{"the total work recorded for the newest block raised", "total work recorded through block #" + strconv.FormatInt(newest, 10), func(t *testing.T, tx *bbolt.Tx) error {
			return raiseWork(tx, newest)
		}},
		{"a record of spent coins copied to a height above the newest block", "that none of its blocks makes", func(t *testing.T, tx *bbolt.Tx) error {
			bucket := tx.Bucket(undoBucket)
			raw := bucket.Get(undoKey(chain.transferHeight))
			if raw == nil {
				t.Fatalf("set-up: no record of the coins block %d spent", chain.transferHeight)
			}
			return bucket.Put(undoKey(beyond), append([]byte(nil), raw...))
		}},
		{"a record of a sweep copied to a height above the newest block", "that none of its blocks makes", func(t *testing.T, tx *bbolt.Tx) error {
			bucket := tx.Bucket(sweepBucket)
			raw := bucket.Get(chainWorkKey(chain.sweepHeight))
			if raw == nil {
				t.Fatalf("set-up: no sweep record at block %d", chain.sweepHeight)
			}
			return bucket.Put(chainWorkKey(beyond), append([]byte(nil), raw...))
		}},
		{"a record of spent coins put under a block that spent nothing", "altered or damaged", func(t *testing.T, tx *bbolt.Tx) error {
			bucket := tx.Bucket(undoBucket)
			raw := bucket.Get(undoKey(chain.transferHeight))
			if raw == nil {
				t.Fatalf("set-up: no record of the coins block %d spent", chain.transferHeight)
			}
			return bucket.Put(undoKey(spentOnNothing), append([]byte(nil), raw...))
		}},
		{"one coin lowered and another raised by the same amount", "is not recorded as its blocks make it", func(t *testing.T, tx *bbolt.Tx) error {
			keys := coinsWorthSomething(tx)
			a, b := readCoinRecord(t, tx, keys[0]), readCoinRecord(t, tx, keys[1])
			a.Amount = UnitsAmount(a.Amount.Units() - 12345)
			b.Amount = UnitsAmount(b.Amount.Units() + 12345)
			if err := writeCoinRecord(tx, keys[0], a); err != nil {
				return err
			}
			return writeCoinRecord(tx, keys[1], b)
		}},
		{"one coin deleted and its amount added to another", "is not recorded as its blocks make it", func(t *testing.T, tx *bbolt.Tx) error {
			keys := coinsWorthSomething(tx)
			gone, other := readCoinRecord(t, tx, keys[0]), readCoinRecord(t, tx, keys[1])
			other.Amount = UnitsAmount(other.Amount.Units() + gone.SpendUnits())
			if err := writeCoinRecord(tx, keys[1], other); err != nil {
				return err
			}
			return tx.Bucket(utxoBucket).Delete([]byte(keys[0]))
		}},
		{"an extra coin added and the same amount taken off another", "altered or damaged", func(t *testing.T, tx *bbolt.Tx) error {
			keys := coinsWorthSomething(tx)
			other := readCoinRecord(t, tx, keys[0])
			other.Amount = UnitsAmount(other.Amount.Units() - 5000)
			if err := writeCoinRecord(tx, keys[0], other); err != nil {
				return err
			}
			return writeCoinRecord(tx, utxoKey("TX_made_up", 0), UTXORecord{Amount: UnitsAmount(5000), Recipient: testAddress("nobody"), Age: 3})
		}},
		{"the record of the coins a block spent altered", "the record of the coins block #2 spent", func(t *testing.T, tx *bbolt.Tx) error {
			bucket := tx.Bucket(undoBucket)
			var records []SpentRecord
			if err := json.Unmarshal(bucket.Get(undoKey(chain.transferHeight)), &records); err != nil || len(records) == 0 {
				t.Fatalf("set-up: no record of the coins block %d spent (%v)", chain.transferHeight, err)
			}
			records[0].Record.Recipient = testAddress("somebody else")
			raw, err := json.Marshal(records)
			if err != nil {
				return err
			}
			return bucket.Put(undoKey(chain.transferHeight), raw)
		}},
		{"the record of a sweep altered", "the record of the sweep of block", func(t *testing.T, tx *bbolt.Tx) error {
			records, err := loadSweepRecords(tx, chain.sweepHeight)
			if err != nil || len(records) == 0 {
				t.Fatalf("set-up: no sweep record at block %d (%v)", chain.sweepHeight, err)
			}
			// What is set aside on the coin is left alone, so the vault's
			// figure, which is worked out from it, still agrees.
			records[0].Before.Recipient = testAddress("somebody else")
			raw, err := json.Marshal(records)
			if err != nil {
				return err
			}
			return tx.Bucket(sweepBucket).Put(chainWorkKey(chain.sweepHeight), raw)
		}},
		{"the record of the coins a block spent deleted", "the record of the coins block #2 spent", func(t *testing.T, tx *bbolt.Tx) error {
			return tx.Bucket(undoBucket).Delete(undoKey(chain.transferHeight))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := alteredCopyOf(t, chain.path, "altered.db", func(tx *bbolt.Tx) error { return c.change(t, tx) })
			if _, quick := checkChainFile(path, chain.genesis); quick != nil {
				t.Fatalf("the quick check refused the file: %v. This test is about what the quick check cannot see", quick)
			}
			if _, err := replayChainFile(path, chain.genesis); err == nil {
				t.Error("the full check passed a file whose records were altered")
			} else if !strings.Contains(err.Error(), c.says) {
				t.Errorf("the full check refused the file with: %v; want it to say %q", err, c.says)
			}
		})
	}
}

// A file can hold a block the rules refuse and still have every record agree
// with every other, if the one who altered it fixed the block's identifier and
// the vault's record for it. Only working the chain out again finds it.
func TestAChainFileHoldingABlockTheRulesRefuseFailsTheFullCheck(t *testing.T) {
	chain := buildChainForFullCheck(t)
	closeTheEngine(t)
	theFileAsTheNodeWroteItPasses(t, chain)

	path := alteredCopyOf(t, chain.path, "refused-block.db", func(tx *bbolt.Tx) error {
		blocks, vaults := tx.Bucket(blocksBucket), tx.Bucket(vaultBucket)
		key := chainWorkKey(chain.tip.Index)
		var block Block
		if err := json.Unmarshal(blocks.Get(key), &block); err != nil {
			return err
		}
		var entry vaultEntry
		if err := json.Unmarshal(vaults.Get(key), &entry); err != nil {
			return err
		}
		// First a different nonce, so that the proof of work no longer holds.
		found := false
		for tries := 0; tries < 100000 && !found; tries++ {
			block.Nonce++
			block.Hash = CalculateHash(block)
			found = !BlockProofValid(block, block.Difficulty)
		}
		// If the nonce is never wrong, the difficulty is raised.
		for tries := 0; !found && tries < 100000; tries++ {
			block.Difficulty++
			block.Hash = CalculateHash(block)
			found = !BlockProofValid(block, block.Difficulty)
		}
		if !found {
			t.Fatal("set-up: no change to the newest block broke its proof of work")
		}
		raw, err := json.Marshal(block)
		if err != nil {
			return err
		}
		if err := blocks.Put(key, raw); err != nil {
			return err
		}
		entry.Hash = block.Hash
		raw, err = json.Marshal(entry)
		if err != nil {
			return err
		}
		return vaults.Put(key, raw)
	})
	if _, err := checkChainFile(path, chain.genesis); err != nil {
		t.Fatalf("the quick check refused the file: %v. This test is about what the quick check cannot see", err)
	}
	want := "block #" + strconv.FormatInt(chain.tip.Index, 10)
	if _, err := replayChainFile(path, chain.genesis); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("a file holding a block that breaks the rules gave: %v; want it refused, naming %s", err, want)
	}
}

func TestTheFullCheckNeedsAProgramOfItsOwnAndAKnownFirstBlock(t *testing.T) {
	chain := buildChainForFullCheck(t)
	copyOfFile, _, err := writeBackup(t.TempDir(), 7)
	if err != nil {
		t.Fatal(err)
	}

	// With a chain open in this program, the check is refused and the open
	// chain is not touched.
	if _, err := replayChainFile(copyOfFile, chain.genesis); err == nil || !strings.Contains(err.Error(), "a chain is open in this program") {
		t.Fatalf("with a chain open the full check gave: %v; want it refused for that reason", err)
	}
	if GlobalBoltEngine == nil || BlockchainFile != chain.path {
		t.Fatal("the refused check closed or moved the open chain")
	}
	if GetLatestBlockInternal().Hash != chain.tip.Hash || coinSetFingerprint() != chain.fingerprint {
		t.Error("the refused check changed the open chain")
	}

	// With the first block not known, there is nothing to hold the file's
	// first block to, and the check is refused.
	closeTheEngine(t)
	if _, err := replayChainFile(copyOfFile, ""); err == nil || !strings.Contains(err.Error(), "first block is not known") {
		t.Fatalf("with no first block known the full check gave: %v; want it refused for that reason", err)
	}
	if GlobalBoltEngine != nil || BlockchainFile != chain.path {
		t.Error("the refused check left a chain open or moved the chain file")
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(chain.path), "*.fullcheck-*")); len(left) != 0 {
		t.Errorf("the refused check left a scratch file behind: %v", left)
	}
}

// fileDigest is the size and SHA-256 of a file.
func fileDigest(t *testing.T, path string) (int64, string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return int64(len(raw)), hex.EncodeToString(sum[:])
}

func TestRestoringABackupWhoseCoinsWereAlteredChangesNothing(t *testing.T) {
	buildChainForBackup(t)
	directory := t.TempDir()
	backup, _, err := writeBackup(directory, 7)
	if err != nil {
		t.Fatal(err)
	}
	genesis := buildGenesisBlock().Hash
	extendTestChain(t, GetLatestBlockInternal().Timestamp+45, testAddress("later"), nil)
	closeTheEngine(t)

	// A coin in the backup is given to another address. The quick check
	// would let the file in.
	altered := alteredCopyOf(t, backup, "altered-backup.db", func(tx *bbolt.Tx) error {
		key := coinsWorthSomething(tx)[0]
		coin := readCoinRecord(t, tx, key)
		coin.Recipient = testAddress("a thief")
		return writeCoinRecord(tx, key, coin)
	})
	if _, err := checkChainFile(altered, genesis); err != nil {
		t.Fatalf("the quick check refused the altered backup: %v. This test is about what the quick check cannot see", err)
	}
	sizeBefore, sumBefore := fileDigest(t, BlockchainFile)

	if _, kept, err := restoreBackup(altered, genesis); err == nil {
		t.Fatal("a backup whose coins were altered was restored")
	} else if kept != "" || !strings.Contains(err.Error(), "nothing was changed") {
		t.Errorf("the refused restore said: %v (kept %q); want it to say nothing was changed", err, kept)
	}
	if size, sum := fileDigest(t, BlockchainFile); size != sizeBefore || sum != sumBefore {
		t.Error("a refused restore changed the chain file")
	}
	for _, mark := range []string{".replaced-", ".restoring", ".fullcheck-"} {
		if left, _ := filepath.Glob(filepath.Join(filepath.Dir(BlockchainFile), "*"+mark+"*")); len(left) != 0 {
			t.Errorf("a refused restore left %v beside the chain file", left)
		}
	}
}

// A node works a missing total of work out again, and reads an entry only for
// the block it names. So neither of these files is damaged, and the full check
// lets them through.
func TestAChainFileWithATotalOfWorkMissingOrNamingAnotherBlockStillPassesTheFullCheck(t *testing.T) {
	chain := buildChainForFullCheck(t)
	closeTheEngine(t)
	theFileAsTheNodeWroteItPasses(t, chain)
	middle := chain.tip.Index / 2

	cases := []struct {
		name   string
		change func(tx *bbolt.Tx) error
	}{
		{"the total work for a middle block is missing", func(tx *bbolt.Tx) error {
			return tx.Bucket(chainWorkBucket).Delete(chainWorkKey(middle))
		}},
		{"the total work for a middle block names another block", func(tx *bbolt.Tx) error {
			bucket := tx.Bucket(chainWorkBucket)
			var entry chainWorkEntry
			if err := json.Unmarshal(bucket.Get(chainWorkKey(middle)), &entry); err != nil {
				return err
			}
			entry.Hash = strings.Repeat("0", 64)
			entry.Work = "9" + strings.Repeat("9", 40)
			raw, err := json.Marshal(entry)
			if err != nil {
				return err
			}
			return bucket.Put(chainWorkKey(middle), raw)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := alteredCopyOf(t, chain.path, "work.db", c.change)
			if _, err := replayChainFile(path, chain.genesis); err != nil {
				t.Errorf("the full check refused a file where %s: %v; want it passed", c.name, err)
			}
		})
	}
}

func TestTheFingerprintOfAFileIsItsSHA256(t *testing.T) {
	content := []byte("a small file with known content\n")
	path := filepath.Join(t.TempDir(), "small.bin")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])
	if got, err := fileSHA256(path); err != nil || got != want {
		t.Errorf("the fingerprint of the file is %q (%v), want %q", got, err, want)
	}
	if got, err := fileSHA256(filepath.Join(t.TempDir(), "missing.bin")); err == nil {
		t.Errorf("the fingerprint of a missing file is %q, want an error", got)
	}
}
