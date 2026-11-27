package node

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// Outside review, round 10, third look. A node's file can be damaged or
// altered in ways that used to be taken for "nothing there": a stored height
// that is gone or wrong, a first block that bears the right identifier and
// nothing else right, an amount that is not written as an amount. These tests
// make each of them and check that the node stops, or refuses, and changes
// nothing.

// heightEntryCases are the four ways the stored height can be wrong on a
// chain whose blocks go to 3.
var heightEntryCases = []struct {
	name   string
	change func(bMeta *bbolt.Bucket) error
}{
	{"the entry is deleted", func(bMeta *bbolt.Bucket) error { return bMeta.Delete([]byte("height")) }},
	{"the entry is not a number", func(bMeta *bbolt.Bucket) error { return bMeta.Put([]byte("height"), []byte("damaged")) }},
	{"the entry is lowered to block 1", func(bMeta *bbolt.Bucket) error { return bMeta.Put([]byte("height"), chainWorkKey(1)) }},
	{"the entry is raised to block 9", func(bMeta *bbolt.Bucket) error { return bMeta.Put([]byte("height"), chainWorkKey(9)) }},
}

// openingTheEngine calls InitBoltEngine and says whether it panicked, and
// with what.
func openingTheEngine() (message string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			message, panicked = fmt.Sprint(r), true
		}
	}()
	InitBoltEngine()
	return "", false
}

// A. At start, a height that is missing or wrong stops the node and the file
// is let go.
func TestANodeThatStartsOnAFileWithAMissingOrWrongHeightStopsAndLetsTheFileGo(t *testing.T) {
	for _, c := range heightEntryCases {
		t.Run(c.name, func(t *testing.T) {
			startTestChain(t, "devnet")
			t.Cleanup(func() { atomic.StoreInt64(&localHeightState, 0) })
			timestamp := Params.GenesisTimestamp + 100
			for i := 0; i < 3; i++ {
				timestamp += 45
				extendTestChain(t, timestamp, testAddress("miner"), nil)
			}
			updateDB(t, func(dbTx *bbolt.Tx) error { return c.change(dbTx.Bucket(metaBucket)) })

			// The node stops and starts again on the same file.
			path := BlockchainFile
			closeAfterWork(t)
			atomic.StoreInt64(&localHeightState, 0)
			BlockchainFile = path

			message, panicked := openingTheEngine()
			if !panicked {
				t.Fatal("the node started on a file whose stored height cannot be trusted")
			}
			if !strings.Contains(message, "DATABASE DAMAGED") {
				t.Errorf("the node stopped with %q; want it to say the database is damaged", message)
			}
			if GlobalBoltEngine != nil {
				t.Error("the engine is left open after the node stopped")
				closeAfterWork(t)
			}
			// The file is not held: it can be opened again.
			db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 2 * time.Second})
			if err != nil {
				t.Fatalf("the file is still held after the node stopped: %v", err)
			}
			_ = db.Close()
		})
	}
}

// closeAfterWork waits for the work handed to goroutines, then closes the
// engine.
func closeAfterWork(t *testing.T) {
	t.Helper()
	backgroundWork.Wait()
	closeTheEngine(t)
}

// B. While the node runs, a height changed underneath it is refused, not
// followed.
func TestARunningNodeWhoseStoredHeightIsChangedRefusesBlocksAndRollbacksAndChangesNothing(t *testing.T) {
	// The entry raised to block 9 is tried at start only (test A).
	for _, c := range heightEntryCases[:3] {
		t.Run(c.name, func(t *testing.T) {
			startTestChain(t, "devnet")
			timestamp := Params.GenesisTimestamp + 100

			// Block 1 is mined first and a rival for it that has a lower
			// identifier, both while the first block is the tip.
			timestamp += 45
			block1 := mineTestBlock(t, timestamp, testAddress("m1"), nil, 0)
			var rival Block
			for attempt := int64(0); ; attempt++ {
				candidate := mineTestBlock(t, Params.GenesisTimestamp+10+attempt, testAddress("rival"), nil, 0)
				if strings.Compare(candidate.Hash, block1.Hash) < 0 {
					rival = candidate
					break
				}
			}
			if !SaveBlockToStorageInternal(block1) {
				t.Fatal("set-up: block 1 was not stored")
			}
			timestamp += 45
			extendTestChain(t, timestamp, testAddress("m2"), nil)
			timestamp += 45
			extendTestChain(t, timestamp, testAddress("m3"), nil)
			timestamp += 45
			next := mineTestBlock(t, timestamp, testAddress("m4"), nil, 0)
			if !ValidateBlockSignatures(next) {
				t.Fatal("set-up: the next block does not pass validation on the sound chain")
			}

			updateDB(t, func(dbTx *bbolt.Tx) error { return c.change(dbTx.Bucket(metaBucket)) })
			before := stateOfChain(t)

			if SaveBlockToStorageInternal(next) {
				t.Error("the next block on the tip was stored though the stored height cannot be trusted")
			}
			wantNothingChanged(t, before, "after the next block was offered")

			if SaveBlockToStorageInternal(rival) {
				t.Error("a rival for block 1 was stored though the stored height cannot be trusted")
			}
			wantNothingChanged(t, before, "after the rival for block 1 was offered")

			wantRollbackRefused(t, 1, "the rollback to block 1")
		})
	}
}

// C. A first block in a chain file that bears the right identifier but is not
// this network's first block.
func TestAChainFileWhoseFirstBlockHasAnAlteredDateIsRefusedAndNeverRestored(t *testing.T) {
	chain := buildChainForFullCheck(t)
	closeAfterWork(t)
	theFileAsTheNodeWroteItPasses(t, chain)

	altered := alteredCopyOf(t, chain.path, "altered-first-block.db", func(tx *bbolt.Tx) error {
		var first Block
		if err := json.Unmarshal(tx.Bucket(blocksBucket).Get(chainWorkKey(0)), &first); err != nil {
			return err
		}
		first.Timestamp += 1000 // the identifier is left as it was
		raw, err := json.Marshal(first)
		if err != nil {
			return err
		}
		return tx.Bucket(blocksBucket).Put(chainWorkKey(0), raw)
	})

	if _, err := checkChainFile(altered, chain.genesis); err == nil {
		t.Error("the quick check let in a file whose first block has an altered date")
	}
	if _, err := replayChainFile(altered, chain.genesis); err == nil {
		t.Error("the full check let in a file whose first block has an altered date")
	}

	sizeBefore, sumBefore := fileDigest(t, BlockchainFile)
	if _, kept, err := restoreBackup(altered, chain.genesis); err == nil {
		t.Fatal("a backup whose first block has an altered date was restored")
	} else if kept != "" {
		t.Errorf("the refused restore kept %q", kept)
	}
	if size, sum := fileDigest(t, BlockchainFile); size != sizeBefore || sum != sumBefore {
		t.Error("a refused restore changed the chain file")
	}
}

// The identifier of a block does not cover everything in it. The first block's
// identifier is made from its one transaction's ID alone, so a change to that
// transaction's date leaves the identifier as it was and passes the quick
// check. The full check compares the whole block.
func TestAChainFileWhoseFirstBlockIsAlteredWhereItsIdentifierDoesNotReachFailsTheFullCheck(t *testing.T) {
	chain := buildChainForFullCheck(t)
	closeAfterWork(t)
	theFileAsTheNodeWroteItPasses(t, chain)

	altered := alteredCopyOf(t, chain.path, "altered-first-payment.db", func(tx *bbolt.Tx) error {
		var first Block
		if err := json.Unmarshal(tx.Bucket(blocksBucket).Get(chainWorkKey(0)), &first); err != nil {
			return err
		}
		first.Transactions[0].Timestamp += 1000 // not part of the identifier
		if CalculateHash(first) != first.Hash {
			t.Fatal("set-up: the change reached the identifier; this test needs one that does not")
		}
		raw, err := json.Marshal(first)
		if err != nil {
			return err
		}
		return tx.Bucket(blocksBucket).Put(chainWorkKey(0), raw)
	})

	if _, err := checkChainFile(altered, chain.genesis); err != nil {
		t.Logf("the quick check already refuses this file: %v", err)
	}
	if _, err := replayChainFile(altered, chain.genesis); err == nil {
		t.Error("the full check let in a file whose first block is not this network's in every part")
	}
}

// D. checkStoredGenesis on a chain that is open.
func TestTheStoredFirstBlockIsHeldToThisNetworksInEveryPart(t *testing.T) {
	cases := []struct {
		name    string
		change  func(t *testing.T, bBlocks *bbolt.Bucket) error
		refused bool
	}{
		{"a sound chain passes", func(t *testing.T, bBlocks *bbolt.Bucket) error { return nil }, false},
		{"a first block that cannot be decoded", func(t *testing.T, bBlocks *bbolt.Bucket) error {
			return bBlocks.Put(chainWorkKey(0), []byte("{"))
		}, true},
		{"a first block that is gone", func(t *testing.T, bBlocks *bbolt.Bucket) error {
			return bBlocks.Delete(chainWorkKey(0))
		}, true},
		{"a first block with the right identifier and an altered date", func(t *testing.T, bBlocks *bbolt.Bucket) error {
			var first Block
			if err := json.Unmarshal(bBlocks.Get(chainWorkKey(0)), &first); err != nil {
				return err
			}
			first.Timestamp += 1000
			raw, err := json.Marshal(first)
			if err != nil {
				return err
			}
			return bBlocks.Put(chainWorkKey(0), raw)
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			startTestChain(t, "devnet")
			extendTestChain(t, Params.GenesisTimestamp+145, testAddress("miner"), nil)
			if err := checkStoredGenesis(); err != nil {
				t.Fatalf("set-up: a sound chain fails the check: %v", err)
			}
			updateDB(t, func(dbTx *bbolt.Tx) error { return c.change(t, dbTx.Bucket(blocksBucket)) })
			err := checkStoredGenesis()
			if c.refused && err == nil {
				t.Error("the check passed a first block that is not this network's")
			}
			if !c.refused && err != nil {
				t.Errorf("the check refused a sound chain: %v", err)
			}
		})
	}
}

// E. A coin whose amount is not written as an amount.
func TestABlockThatReachesACoinWithAnUnreadableAmountIsNotStoredAndChangesNothing(t *testing.T) {
	timestamp := startSweepChain(t)
	alice := testAddress("alice-sweep")
	block1 := mineNext(t, timestamp, alice) // alice's coin is due at height 6
	mineTo(t, timestamp, 5, testAddress("miner"))
	coinKey := utxoKey(block1.Transactions[0].ID, 0)

	// Only the amount is spoilt, so that the record still decodes.
	bad := fmt.Sprintf(`{"amount":"not-a-number","recipient":%q,"age":1}`, alice)
	updateDB(t, func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(utxoBucket).Put([]byte(coinKey), []byte(bad))
	})

	// Block 6 reaches alice's sweep date.
	*timestamp += 45
	block6 := mineTestBlock(t, *timestamp, testAddress("miner"), nil, 0)
	if !ValidateBlockSignatures(block6) {
		t.Fatal("set-up: block 6 failed validation")
	}
	before := stateOfChain(t)
	if SaveBlockToStorageInternal(block6) {
		t.Fatal("block 6 was stored though a coin due at it has an unreadable amount")
	}
	wantNothingChanged(t, before, "after block 6 was refused")
}

func TestAChainFileWithACoinWhoseAmountIsNotANumberIsRefused(t *testing.T) {
	chain := buildChainForFullCheck(t)
	closeAfterWork(t)
	theFileAsTheNodeWroteItPasses(t, chain)

	altered := alteredCopyOf(t, chain.path, "altered-amount.db", func(tx *bbolt.Tx) error {
		key := coinsWorthSomething(tx)[0]
		coin := readCoinRecord(t, tx, key)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(tx.Bucket(utxoBucket).Get([]byte(key)), &fields); err != nil {
			return err
		}
		// An amount that is not a number is read as nought. What the coin
		// held is put under "held", so that the coins still add up and
		// only the amount itself is wrong.
		fields["amount"] = json.RawMessage(`"not-a-number"`)
		fields["held"] = json.RawMessage(strconv.FormatInt(coin.SpendUnits(), 10))
		raw, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		return tx.Bucket(utxoBucket).Put([]byte(key), raw)
	})
	if _, err := checkChainFile(altered, chain.genesis); err == nil {
		t.Error("the quick check let in a file with a coin whose amount is not a number")
	}
}

// F. The note of an unfinished download is kept on purpose (see the top of
// backup_replay.go). A backup that carries it passes, and a restore keeps it.
func TestABackupThatCarriesTheNoteOfAnUnfinishedDownloadPassesAndKeepsItWhenRestored(t *testing.T) {
	startTestChain(t, "devnet")
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 3; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, testAddress("miner"), nil)
	}
	genesis := buildGenesisBlock().Hash
	backup, _, err := writeBackup(filepath.Join(t.TempDir(), "backups"), 0)
	if err != nil {
		t.Fatal(err)
	}
	closeAfterWork(t)

	withNote := alteredCopyOf(t, backup, "with-note.db", func(tx *bbolt.Tx) error {
		return tx.Bucket(metaBucket).Put(unsettledDownloadKey, []byte("0"))
	})
	if _, err := replayChainFile(withNote, genesis); err != nil {
		t.Fatalf("the full check refused a backup for carrying the note: %v", err)
	}
	if _, _, err := restoreBackup(withNote, genesis); err != nil {
		t.Fatalf("the restore refused a backup for carrying the note: %v", err)
	}

	db, err := bbolt.Open(BlockchainFile, 0400, &bbolt.Options{ReadOnly: true, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_ = db.View(func(tx *bbolt.Tx) error {
		if note := tx.Bucket(metaBucket).Get(unsettledDownloadKey); string(note) != "0" {
			t.Errorf("the restored file carries the note %q; want it kept as it was, \"0\"", note)
		}
		return nil
	})
}

// G. Three tests taken over from the outside reviewer's file.

// serveMockSyncPeer serves a whole chain over /tip, /blockhash and
// /blocks/batch, the way a peer does, for TriggerActiveReSyncPass.
func serveMockSyncPeer(t *testing.T, chain []Block) *httptest.Server {
	t.Helper()
	totalWork := sumBlockWork(chain[1:])
	mux := http.NewServeMux()
	mux.HandleFunc("/tip", func(w http.ResponseWriter, r *http.Request) {
		tip := chain[len(chain)-1]
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"current_height": tip.Index,
			"block_hash":     tip.Hash,
			"timestamp":      tip.Timestamp,
			"total_work":     totalWork.String(),
		})
	})
	mux.HandleFunc("/blockhash", func(w http.ResponseWriter, r *http.Request) {
		h, err := strconv.ParseInt(r.URL.Query().Get("height"), 10, 64)
		if err != nil || h < 0 || int(h) >= len(chain) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"height": h, "hash": chain[h].Hash})
	})
	mux.HandleFunc("/blocks/batch", func(w http.ResponseWriter, r *http.Request) {
		start, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		var batch []Block
		for _, b := range chain {
			if b.Index >= start {
				batch = append(batch, b)
			}
		}
		_ = json.NewEncoder(w).Encode(batch)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A peer proves more work on another branch, the rollback to the shared block
// is refused because a sweep record is missing, and the node stands still.
func TestASyncPassWhoseRollbackIsRefusedLeavesTheChainAsItWas(t *testing.T) {
	// The longer branch is built first, in a database of its own.
	startTestChain(t, "devnet")
	genesis := GetBlockByHeightFromDB(0)
	var peerChain []Block
	peerChain = append(peerChain, genesis)
	peerTime := Params.GenesisTimestamp + 100
	for h := 1; h <= 4; h++ {
		peerTime += 45
		peerChain = append(peerChain, extendTestChain(t, peerTime, testAddress(fmt.Sprintf("peer%d", h)), nil))
	}
	closeAfterWork(t)

	// This node's own shorter chain, blocks 1 and 2.
	startTestChain(t, "devnet")
	timestamp := Params.GenesisTimestamp + 110
	extendTestChain(t, timestamp, testAddress("ours1"), nil)
	timestamp += 45
	extendTestChain(t, timestamp, testAddress("ours2"), nil)

	// The sweep record of block 2 goes, so a rollback to block 0 is refused.
	updateDB(t, func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(sweepBucket).Delete(chainWorkKey(2))
	})

	srv := serveMockSyncPeer(t, peerChain)
	savedExtras := Params.ExtraSyncURLs
	Params.ExtraSyncURLs = []string{srv.URL}
	t.Cleanup(func() {
		Params.ExtraSyncURLs = savedExtras
		syncSetAside.Lock()
		delete(syncSetAside.until, srv.URL)
		syncSetAside.Unlock()
		syncExcused.Lock()
		delete(syncExcused.at, srv.URL)
		syncExcused.Unlock()
		syncDelivered.Lock()
		delete(syncDelivered.at, srv.URL)
		syncDelivered.Unlock()
	})

	before := stateOfChain(t)
	TriggerActiveReSyncPass()
	wantNothingChanged(t, before, "the sync pass whose rollback is refused")
}

// When the newest block cannot be read, no block is taken, and a sync pass
// changes nothing.
func TestANewestBlockThatCannotBeReadStopsEveryBlockAndASyncPass(t *testing.T) {
	startTestChain(t, "devnet")
	genesis := GetBlockByHeightFromDB(0)
	timestamp := Params.GenesisTimestamp + 100
	extendTestChain(t, timestamp, testAddress("m1"), nil)
	timestamp += 45
	extendTestChain(t, timestamp, testAddress("m2"), nil)

	nextOnBlock2 := mineTestBlock(t, timestamp+45, testAddress("m3"), nil, 0)

	// A longer branch is built in another database for the sync pass.
	savedEngine, savedFile, savedHeight := GlobalBoltEngine, BlockchainFile, atomic.LoadInt64(&localHeightState)
	GlobalBoltEngine = nil
	BlockchainFile = filepath.Join(t.TempDir(), "peer.db")
	atomic.StoreInt64(&localHeightState, 0)
	InitBoltEngine()
	peerChain := []Block{genesis}
	peerTime := Params.GenesisTimestamp + 120
	for h := 1; h <= 4; h++ {
		peerTime += 45
		peerChain = append(peerChain, extendTestChain(t, peerTime, testAddress(fmt.Sprintf("p%d", h)), nil))
	}
	backgroundWork.Wait()
	_ = GlobalBoltEngine.Close()
	GlobalBoltEngine, BlockchainFile = savedEngine, savedFile
	atomic.StoreInt64(&localHeightState, savedHeight)

	// Block 2 cannot be read, and the stored height still says 2.
	updateDB(t, func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(blocksBucket).Put(chainWorkKey(2), []byte("{corrupt"))
	})

	if tip := GetLatestBlockInternal(); tip.Index != 0 || tip.Hash != "" {
		t.Fatalf("with the newest block unreadable the tip is #%d %q; want an empty block", tip.Index, tip.Hash)
	}
	if ValidateBlockSignatures(nextOnBlock2) {
		t.Fatal("block 3 passed validation though block 2 cannot be read")
	}
	if ValidateBlockSignatures(peerChain[1]) {
		t.Fatal("a sound block 1 passed validation though block 2 cannot be read")
	}

	srv := serveMockSyncPeer(t, peerChain)
	savedExtras := Params.ExtraSyncURLs
	Params.ExtraSyncURLs = []string{srv.URL}
	t.Cleanup(func() {
		Params.ExtraSyncURLs = savedExtras
		syncSetAside.Lock()
		delete(syncSetAside.until, srv.URL)
		syncSetAside.Unlock()
		syncExcused.Lock()
		delete(syncExcused.at, srv.URL)
		syncExcused.Unlock()
	})
	before := stateOfChain(t)
	TriggerActiveReSyncPass()
	wantNothingChanged(t, before, "the sync pass with the newest block unreadable")
}

// A backup with a reorg, a sweep and blocks past the end of emission passes
// the full check.
func TestAFullCheckOfAChainWithAReorgASweepAndBlocksPastTheEndOfEmissionPasses(t *testing.T) {
	savedParams := Params
	t.Cleanup(func() { Params = savedParams; applyParams() })

	startTestChain(t, "devnet", func() {
		econ := NewGenesisEconomics
		econ.EpochBlocks = 2
		econ.EmissionEpochs = 2 // emission ends after height 4
		econ.SweepIntervalBlocks = 3
		econ.VaultPayoutBlocks = 10
		Params.Economics = &econ
	})

	_, _, alice, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp + 100
	extendTestChain(t, timestamp, alice, nil)
	timestamp += 45
	extendTestChain(t, timestamp, testAddress("b"), nil)
	timestamp += 45
	extendTestChain(t, timestamp, testAddress("c"), nil)

	StateMutex.Lock()
	removed := rollbackChainToHeightLocked(2)
	StateMutex.Unlock()
	if len(removed) != 1 {
		t.Fatalf("expected 1 block removed, got %d", len(removed))
	}
	for h := int64(3); h <= 8; h++ {
		timestamp += 45
		extendTestChain(t, timestamp, testAddress("miner"), nil)
	}

	backupPath, _, err := writeBackup(filepath.Join(t.TempDir(), "backups"), 0)
	if err != nil {
		t.Fatalf("writeBackup failed: %v", err)
	}
	closeAfterWork(t)

	report, err := replayChainFile(backupPath, buildGenesisBlock().Hash)
	if err != nil {
		t.Fatalf("the full check failed after a reorg, a sweep and blocks past emission: %v", err)
	}
	if report.Height != 8 {
		t.Fatalf("the report says block #%d, want #8", report.Height)
	}
}

// mustRefuseToStartOn opens the file at `path` as a node does at start. It
// fails the test unless the node stops with "DATABASE DAMAGED", leaves no
// engine open, and lets the file go.
func mustRefuseToStartOn(t *testing.T, path string) {
	t.Helper()
	atomic.StoreInt64(&localHeightState, 0)
	BlockchainFile = path
	message, panicked := openingTheEngine()
	if !panicked {
		closeAfterWork(t)
		t.Fatal("the node started on a file that has lost what it needs")
	}
	if !strings.Contains(message, "DATABASE DAMAGED") {
		t.Errorf("the node stopped with %q; want it to say the database is damaged", message)
	}
	if GlobalBoltEngine != nil {
		t.Error("the engine is left open after the node stopped")
		closeAfterWork(t)
	}
	db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("the file is still held after the node stopped: %v", err)
	}
	_ = db.Close()
}

// H. A file that has lost its blocks is not given a new first block.
func TestANodeThatStartsOnAFileWithNoBlocksButCoinsOrAHeightStopsAndLetsTheFileGo(t *testing.T) {
	t.Run("a chain of 3 whose blocks are all gone and whose coins are left", func(t *testing.T) {
		startTestChain(t, "devnet")
		timestamp := Params.GenesisTimestamp + 100
		for i := 0; i < 3; i++ {
			timestamp += 45
			extendTestChain(t, timestamp, testAddress("miner"), nil)
		}
		updateDB(t, func(dbTx *bbolt.Tx) error {
			bBlocks := dbTx.Bucket(blocksBucket)
			var keys [][]byte
			_ = bBlocks.ForEach(func(key, _ []byte) error {
				keys = append(keys, append([]byte(nil), key...))
				return nil
			})
			for _, key := range keys {
				if err := bBlocks.Delete(key); err != nil {
					return err
				}
			}
			return nil
		})
		path := BlockchainFile
		closeAfterWork(t)
		mustRefuseToStartOn(t, path)
	})

	t.Run("a file with no blocks and no coins and only a height", func(t *testing.T) {
		startTestChain(t, "devnet")
		closeAfterWork(t)
		path := filepath.Join(t.TempDir(), "height-only.db")
		db, err := bbolt.Open(path, 0600, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Update(func(tx *bbolt.Tx) error {
			for _, name := range [][]byte{blocksBucket, utxoBucket} {
				if _, err := tx.CreateBucketIfNotExists(name); err != nil {
					return err
				}
			}
			bMeta, err := tx.CreateBucketIfNotExists(metaBucket)
			if err != nil {
				return err
			}
			return bMeta.Put([]byte("height"), chainWorkKey(3))
		}); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		mustRefuseToStartOn(t, path)
	})

	t.Run("a new empty file starts and holds the first block at height 0", func(t *testing.T) {
		startTestChain(t, "devnet")
		closeAfterWork(t)
		path := filepath.Join(t.TempDir(), "new.db")
		db, err := bbolt.Open(path, 0600, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		atomic.StoreInt64(&localHeightState, 0)
		BlockchainFile = path
		if message, panicked := openingTheEngine(); panicked {
			t.Fatalf("a new empty file did not start: %v", message)
		}
		if height := atomic.LoadInt64(&localHeightState); height != 0 {
			t.Errorf("the height is %d, want 0", height)
		}
		if first := GetBlockByHeightFromDB(0); first.Hash != buildGenesisBlock().Hash {
			t.Errorf("block 0 is %.12s, want this network's first block", first.Hash)
		}
	})
}

// I. The newest block cannot be read.
func TestANodeThatStartsOnAFileWhoseNewestBlockCannotBeReadStopsAndLetsTheFileGo(t *testing.T) {
	startTestChain(t, "devnet")
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 3; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, testAddress("miner"), nil)
	}
	updateDB(t, func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(blocksBucket).Put(chainWorkKey(3), []byte("{"))
	})
	path := BlockchainFile
	closeAfterWork(t)
	mustRefuseToStartOn(t, path)
}

// While the node runs, an unreadable newest block stops the next block and a
// rival for the newest height alike.
func TestARunningNodeWhoseNewestBlockCannotBeReadStoresNeitherTheNextBlockNorARival(t *testing.T) {
	startTestChain(t, "devnet")
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 2; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, testAddress("miner"), nil)
	}
	// Block 3, and a rival for it with a lower identifier, both built while
	// block 2 is the tip.
	timestamp += 45
	block3 := mineTestBlock(t, timestamp, testAddress("m3"), nil, 0)
	var rival Block
	for attempt := int64(1); ; attempt++ {
		candidate := mineTestBlock(t, timestamp+attempt, testAddress("rival"), nil, 0)
		if strings.Compare(candidate.Hash, block3.Hash) < 0 {
			rival = candidate
			break
		}
	}
	if !SaveBlockToStorageInternal(block3) {
		t.Fatal("set-up: block 3 was not stored")
	}
	next := mineTestBlock(t, timestamp+45, testAddress("m4"), nil, 0)

	updateDB(t, func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(blocksBucket).Put(chainWorkKey(3), []byte("{"))
	})
	before := stateOfChain(t)
	if SaveBlockToStorageInternal(next) {
		t.Error("the next block was stored on a newest block that cannot be read")
	}
	wantNothingChanged(t, before, "after the next block was offered")
	if SaveBlockToStorageInternal(rival) {
		t.Error("a rival was stored in place of a newest block that cannot be read")
	}
	wantNothingChanged(t, before, "after the rival was offered")
}

// spoilAmountIn rewrites the "amount" of the coin that sits under `field` in
// the first element of a JSON list (a record of spent or swept coins) as text
// that is not a number.
func spoilAmountIn(t *testing.T, raw []byte, field string) []byte {
	t.Helper()
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil || len(list) == 0 {
		t.Fatalf("set-up: the record cannot be changed (%v, %d entries)", err, len(list))
	}
	var coin map[string]json.RawMessage
	if err := json.Unmarshal(list[0][field], &coin); err != nil {
		t.Fatal(err)
	}
	coin["amount"] = json.RawMessage(`"not-a-number"`)
	var err error
	if list[0][field], err = json.Marshal(coin); err != nil {
		t.Fatal(err)
	}
	spoilt, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	return spoilt
}

// sweepChainWithASweepAt6 is a chain on the network that sweeps every 5
// blocks, with a coin made at block 1 and swept at block 6. The tip is block
// `height` (6 or more).
func sweepChainWithASweepAt6(t *testing.T, height int64) *int64 {
	t.Helper()
	timestamp := startSweepChain(t)
	mineNext(t, timestamp, testAddress("idle"))
	mineTo(t, timestamp, height, testAddress("other"))
	updateDB(t, func(dbTx *bbolt.Tx) error {
		if records, err := loadSweepRecords(dbTx, 6); err != nil || len(records) == 0 {
			t.Fatalf("set-up: block 6 swept nothing (%v)", err)
		}
		return nil
	})
	return timestamp
}

// J. A record of coins with an amount that is not an amount.
func TestARollbackIsRefusedWhenARecordOfSpentCoinsHoldsAnUnreadableAmount(t *testing.T) {
	chainWithOneTransfer(t, 2)
	updateDB(t, func(dbTx *bbolt.Tx) error {
		raw := dbTx.Bucket(undoBucket).Get(undoKey(2))
		if raw == nil {
			t.Fatal("set-up: block 2 has no record of the coins it spent")
		}
		return dbTx.Bucket(undoBucket).Put(undoKey(2), spoilAmountIn(t, raw, "record"))
	})
	wantRollbackRefused(t, 1, "with a spent coin's amount unreadable in block 2's record")
}

func TestARollbackIsRefusedWhenASweepRecordHoldsAnUnreadableAmount(t *testing.T) {
	sweepChainWithASweepAt6(t, 6)
	updateDB(t, func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(sweepBucket).Put(chainWorkKey(6), spoilAmountIn(t, dbTx.Bucket(sweepBucket).Get(chainWorkKey(6)), "before"))
	})
	wantRollbackRefused(t, 5, "with a swept coin's amount unreadable in block 6's record")
}

func TestABlockThatNeedsASweepRecordWithAnUnreadableAmountIsNotStored(t *testing.T) {
	timestamp := sweepChainWithASweepAt6(t, 10)
	updateDB(t, func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(sweepBucket).Put(chainWorkKey(6), spoilAmountIn(t, dbTx.Bucket(sweepBucket).Get(chainWorkKey(6)), "before"))
	})
	// Block 11 sweeps again the coins that block 6 swept.
	*timestamp += 45
	block11 := mineTestBlock(t, *timestamp, testAddress("other"), nil, 0)
	if !ValidateBlockSignatures(block11) {
		t.Fatal("set-up: block 11 fails ValidateBlockSignatures on its own, so this test would prove nothing about the save")
	}
	before := stateOfChain(t)
	if SaveBlockToStorageInternal(block11) {
		t.Fatal("block 11 was stored though the record of the sweep it follows holds an unreadable amount")
	}
	wantNothingChanged(t, before, "after the refused block")
}

// K. A first block whose payment has an amount that is not an amount. Its
// identifier does not change, since an unreadable amount is hashed as nought.
func spoilFirstBlocksAmount(t *testing.T, raw []byte) []byte {
	t.Helper()
	var block map[string]json.RawMessage
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatal(err)
	}
	var transactions []map[string]json.RawMessage
	if err := json.Unmarshal(block["transactions"], &transactions); err != nil || len(transactions) == 0 {
		t.Fatalf("set-up: the first block's transactions cannot be read (%v)", err)
	}
	var outputs []map[string]json.RawMessage
	if err := json.Unmarshal(transactions[0]["outputs"], &outputs); err != nil || len(outputs) == 0 {
		t.Fatal("set-up: the first block's transaction has no output to change")
	}
	outputs[0]["amount"] = json.RawMessage(`"x"`)
	var err error
	if transactions[0]["outputs"], err = json.Marshal(outputs); err != nil {
		t.Fatal(err)
	}
	if block["transactions"], err = json.Marshal(transactions); err != nil {
		t.Fatal(err)
	}
	spoilt, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	return spoilt
}

func TestAFirstBlockWithAnUnreadableAmountIsNotThisNetworksFirstBlock(t *testing.T) {
	t.Run("the stored first block of an open chain", func(t *testing.T) {
		startTestChain(t, "devnet")
		extendTestChain(t, Params.GenesisTimestamp+145, testAddress("miner"), nil)
		updateDB(t, func(dbTx *bbolt.Tx) error {
			bBlocks := dbTx.Bucket(blocksBucket)
			return bBlocks.Put(chainWorkKey(0), spoilFirstBlocksAmount(t, bBlocks.Get(chainWorkKey(0))))
		})
		var first Block
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			return json.Unmarshal(dbTx.Bucket(blocksBucket).Get(chainWorkKey(0)), &first)
		})
		if CalculateHash(first) != first.Hash {
			t.Fatal("set-up: the identifier changed; this test needs it to stay")
		}
		if checkStoredGenesis() == nil {
			t.Error("the check passed a first block with an amount that is not an amount")
		}
	})
	t.Run("a chain file", func(t *testing.T) {
		chain := buildChainForFullCheck(t)
		closeAfterWork(t)
		theFileAsTheNodeWroteItPasses(t, chain)
		altered := alteredCopyOf(t, chain.path, "altered-first-amount.db", func(tx *bbolt.Tx) error {
			bBlocks := tx.Bucket(blocksBucket)
			return bBlocks.Put(chainWorkKey(0), spoilFirstBlocksAmount(t, bBlocks.Get(chainWorkKey(0))))
		})
		if _, err := replayChainFile(altered, chain.genesis); err == nil {
			t.Error("the full check let in a file whose first block has an amount that is not an amount")
		}
	})
}

// L. The node's memory says the newest block is 2 and the file says 3.
func TestARollbackIsRefusedWhenTheNodesMemoryAndTheFileDisagreeAboutTheNewestBlock(t *testing.T) {
	startTestChain(t, "devnet")
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 3; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, testAddress("miner"), nil)
	}
	before := stateOfChain(t) // taken while the memory still says 3
	t.Cleanup(func() { atomic.StoreInt64(&localHeightState, 3) })

	atomic.StoreInt64(&localHeightState, 2)
	StateMutex.Lock()
	removed := rollbackChainToHeightLocked(1)
	StateMutex.Unlock()
	atomic.StoreInt64(&localHeightState, 3)

	if len(removed) != 0 {
		t.Fatalf("%d block(s) were removed; want the rollback refused", len(removed))
	}
	wantNothingChanged(t, before, "after the refused rollback")
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		if dbTx.Bucket(blocksBucket).Get(chainWorkKey(3)) == nil {
			t.Error("block 3 was taken off though the rollback was refused")
		}
		return nil
	})
}
