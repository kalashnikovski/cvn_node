package node

import (
	"encoding/json"
	"fmt"
	"math/big"
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

// Outside review, round 10, fourth look. The coins in a file must belong to
// its blocks, a total of work must have every block behind it, and a backup's
// height must be written the way a node writes it.

// aChainOfThree starts a chain on devnet and mines blocks 1 to 3. The node's
// height in memory is put back to nought when the test ends.
func aChainOfThree(t *testing.T) {
	t.Helper()
	startTestChain(t, "devnet")
	t.Cleanup(func() { atomic.StoreInt64(&localHeightState, 0) })
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 3; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, testAddress(fmt.Sprintf("miner%d", i)), nil)
	}
}

// M. Coins that do not belong to the blocks of the file.
func TestANodeThatStartsOnAFileWhoseCoinsDoNotAddUpToTheBlocksStopsAndLetsTheFileGo(t *testing.T) {
	t.Run("a coin of an old block 3 is left after a rollback to block 1", func(t *testing.T) {
		aChainOfThree(t)
		tip := GetLatestBlockInternal()
		coinKey := []byte(utxoKey(tip.Transactions[0].ID, 0))
		var coinRaw []byte
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			coinRaw = append([]byte(nil), dbTx.Bucket(utxoBucket).Get(coinKey)...)
			return nil
		})
		if len(coinRaw) == 0 {
			t.Fatal("set-up: block 3's coin is not there")
		}
		StateMutex.Lock()
		removed := rollbackChainToHeightLocked(1)
		StateMutex.Unlock()
		if len(removed) != 2 {
			t.Fatalf("set-up: %d blocks were removed, want 2", len(removed))
		}
		// The coin of the block that is gone comes back.
		updateDB(t, func(dbTx *bbolt.Tx) error { return dbTx.Bucket(utxoBucket).Put(coinKey, coinRaw) })
		path := BlockchainFile
		closeAfterWork(t)
		mustRefuseToStartOn(t, path)
	})

	t.Run("a coin is deleted from a sound chain", func(t *testing.T) {
		aChainOfThree(t)
		updateDB(t, func(dbTx *bbolt.Tx) error {
			return dbTx.Bucket(utxoBucket).Delete([]byte(coinsWorthSomething(dbTx)[0]))
		})
		path := BlockchainFile
		closeAfterWork(t)
		mustRefuseToStartOn(t, path)
	})

	t.Run("a coin's amount is raised by one unit", func(t *testing.T) {
		aChainOfThree(t)
		updateDB(t, func(dbTx *bbolt.Tx) error {
			key := coinsWorthSomething(dbTx)[0]
			coin := readCoinRecord(t, dbTx, key)
			coin.Amount = UnitsAmount(coin.Amount.Units() + 1)
			return writeCoinRecord(dbTx, key, coin)
		})
		path := BlockchainFile
		closeAfterWork(t)
		mustRefuseToStartOn(t, path)
	})

	// A KNOWN LIMIT, kept here so that it is not forgotten. The start only
	// holds the coins to the sum of what has been issued. A coin given to
	// another address leaves that sum as it was, so the node starts. Only the
	// full check of a chain file (replayChainFile) finds it.
	t.Run("a coin given to another address with the totals unchanged still starts (known limit)", func(t *testing.T) {
		aChainOfThree(t)
		updateDB(t, func(dbTx *bbolt.Tx) error {
			key := coinsWorthSomething(dbTx)[0]
			coin := readCoinRecord(t, dbTx, key)
			coin.Recipient = testAddress("a thief")
			return writeCoinRecord(dbTx, key, coin)
		})
		path := BlockchainFile
		closeAfterWork(t)
		atomic.StoreInt64(&localHeightState, 0)
		BlockchainFile = path
		if message, panicked := openingTheEngine(); panicked {
			t.Fatalf("the node refused a file whose totals are right: %v", message)
		}
	})
}

// workStopAtMiddleBlock is the set-up of the outside reviewer's second proof.
// The heavy branch is in place, a block below the shared ancestor cannot be
// read, and the record of total work for the newest block is gone. It returns
// the lighter chain, which a peer serves.
func workStopAtMiddleBlock(t *testing.T) (lightChain []Block, heavyTip Block) {
	t.Helper()
	startTestChain(t, "devnet")
	b := buildCompetingBranches(t)
	putBranchInPlace(b.heavy, b.ancestor)
	for h := int64(0); h <= b.ancestor.Index; h++ {
		lightChain = append(lightChain, GetBlockByHeightFromDB(h))
	}
	lightChain = append(lightChain, b.light...)
	heavyTip = GetLatestBlockInternal()
	if sumBlockWork(b.light).Cmp(sumBlockWork(b.heavy)) >= 0 {
		t.Fatal("set-up: the light branch must have less work than the heavy one")
	}
	updateDB(t, func(dbTx *bbolt.Tx) error {
		if err := dbTx.Bucket(blocksBucket).Put(chainWorkKey(1), []byte("{corrupt")); err != nil {
			return err
		}
		return dbTx.Bucket(chainWorkBucket).Delete(chainWorkKey(heavyTip.Index))
	})
	return lightChain, heavyTip
}

// N. A total of work that has a block it cannot read behind it.
func TestASyncPassThatCannotWorkOutItsOwnTotalOfWorkKeepsTheNodeOnItsChain(t *testing.T) {
	lightChain, heavyTip := workStopAtMiddleBlock(t)

	srv := serveMockSyncPeer(t, lightChain)
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
	wantNothingChanged(t, before, "the sync pass with a block below the shared one unreadable")
	if tip := GetLatestBlockInternal(); tip.Hash != heavyTip.Hash {
		t.Errorf("the node is at #%d %.12s; want it to stay on its own chain at #%d %.12s", tip.Index, tip.Hash, heavyTip.Index, heavyTip.Hash)
	}
}

func TestATotalOfWorkIsWorkedOutFromEveryBlockOrNotAtAll(t *testing.T) {
	aChainOfThree(t)
	sum := func() (*big.Int, error) {
		var total *big.Int
		var err error
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			total, err = sumChainWorkFromBlocks(dbTx, 3)
			return nil
		})
		return total, err
	}

	t.Run("a sound chain gives the work of every block added", func(t *testing.T) {
		var blocks []Block
		for h := int64(0); h <= 3; h++ {
			blocks = append(blocks, GetBlockByHeightFromDB(h))
		}
		got, err := sum()
		if err != nil {
			t.Fatal(err)
		}
		if want := sumBlockWork(blocks); got.Cmp(want) != 0 {
			t.Errorf("the total is %s, want %s", got, want)
		}
	})
	t.Run("a block in the middle that is gone is an error", func(t *testing.T) {
		updateDB(t, func(dbTx *bbolt.Tx) error { return dbTx.Bucket(blocksBucket).Delete(chainWorkKey(2)) })
		if _, err := sum(); err == nil {
			t.Error("a total was given with block 2 missing")
		}
	})
	t.Run("a block in the middle that cannot be decoded is an error", func(t *testing.T) {
		updateDB(t, func(dbTx *bbolt.Tx) error { return dbTx.Bucket(blocksBucket).Put(chainWorkKey(2), []byte("{")) })
		if _, err := sum(); err == nil {
			t.Error("a total was given with block 2 unreadable")
		}
	})
}

func TestANodeWithNoRecordOfTotalWorkForItsTipAndAnUnreadableBlockStoresNothingAndDoesNotStart(t *testing.T) {
	aChainOfThree(t)
	timestamp := GetLatestBlockInternal().Timestamp + 45
	next := mineTestBlock(t, timestamp, testAddress("m4"), nil, 0)
	updateDB(t, func(dbTx *bbolt.Tx) error {
		if err := dbTx.Bucket(blocksBucket).Put(chainWorkKey(2), []byte("{")); err != nil {
			return err
		}
		return dbTx.Bucket(chainWorkBucket).Delete(chainWorkKey(3))
	})
	before := stateOfChain(t)
	if SaveBlockToStorageInternal(next) {
		t.Error("a block was stored though the total of work behind it cannot be worked out")
	}
	wantNothingChanged(t, before, "after the refused block")

	path := BlockchainFile
	closeAfterWork(t)
	mustRefuseToStartOn(t, path)
}

func TestANodeWithOnlyTheRecordOfTotalWorkForItsTipGoneStartsAndHasTheSameTotal(t *testing.T) {
	aChainOfThree(t)
	before := TotalChainWork()
	if before.Sign() <= 0 {
		t.Fatal("set-up: the total of work is nought")
	}
	updateDB(t, func(dbTx *bbolt.Tx) error { return dbTx.Bucket(chainWorkBucket).Delete(chainWorkKey(3)) })
	path := BlockchainFile
	closeAfterWork(t)
	atomic.StoreInt64(&localHeightState, 0)
	BlockchainFile = path
	if message, panicked := openingTheEngine(); panicked {
		t.Fatalf("the node refused a sound file whose record of total work was gone: %v", message)
	}
	if after := TotalChainWork(); after.Cmp(before) != 0 {
		t.Errorf("the total of work is %s, was %s", after, before)
	}
}

// O. A backup whose height is written as 3 and not as the 16 digits a node
// writes is refused by every check, and a restore leaves the chain file alone.
func TestABackupWhoseHeightIsNotWrittenAsANodeWritesItIsRefusedAndNeverRestored(t *testing.T) {
	startTestChain(t, "devnet")
	t.Cleanup(func() { atomic.StoreInt64(&localHeightState, 0) })
	genesis := buildGenesisBlock().Hash
	timestamp := Params.GenesisTimestamp + 100
	for i := 0; i < 3; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, testAddress("miner"), nil)
	}
	backup, _, err := writeBackup(filepath.Join(t.TempDir(), "backups"), 0)
	if err != nil {
		t.Fatal(err)
	}
	closeAfterWork(t)

	altered := alteredCopyOf(t, backup, "unpadded-height.db", func(tx *bbolt.Tx) error {
		return tx.Bucket(metaBucket).Put([]byte("height"), []byte("3"))
	})
	if _, err := checkChainFile(altered, genesis); err == nil {
		t.Error("the quick check let in a backup whose height is written as 3")
	}
	if _, err := replayChainFile(altered, genesis); err == nil {
		t.Error("the full check let in a backup whose height is written as 3")
	}
	sizeBefore, sumBefore := fileDigest(t, BlockchainFile)
	if _, kept, err := restoreBackup(altered, genesis); err == nil {
		t.Error("a backup whose height is written as 3 was restored")
	} else if kept != "" {
		t.Errorf("the refused restore kept %q", kept)
	}
	if size, sum := fileDigest(t, BlockchainFile); size != sizeBefore || sum != sumBefore {
		t.Error("a refused restore changed the chain file")
	}
}

// P. Two tests taken over from the outside reviewer's second file.

// A record of spending or of a sweep that no block makes is refused, also
// when it sits above the newest block or under a key that is no height.
func TestAFullCheckRefusesARecordOfSpendingOrOfASweepThatNoBlockMakes(t *testing.T) {
	chain := buildChainForFullCheck(t)
	closeAfterWork(t)
	theFileAsTheNodeWroteItPasses(t, chain)

	cases := []struct {
		name   string
		change func(tx *bbolt.Tx) error
	}{
		{"extra undo record above newest block", func(tx *bbolt.Tx) error {
			return tx.Bucket(undoBucket).Put(undoKey(chain.tip.Index+1), []byte("[]"))
		}},
		{"extra sweep record above newest block", func(tx *bbolt.Tx) error {
			return tx.Bucket(sweepBucket).Put(chainWorkKey(chain.tip.Index+1), []byte("[]"))
		}},
		{"extra sweep record under a non-height key", func(tx *bbolt.Tx) error {
			return tx.Bucket(sweepBucket).Put([]byte("not-a-height"), []byte("[]"))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := alteredCopyOf(t, chain.path, "extra-record.db", c.change)
			if _, err := replayChainFile(path, chain.genesis); err == nil {
				t.Fatalf("replayChainFile passed a file with %s", c.name)
			}
		})
	}
}

// A coin that cannot be read is taken for no coin when a transfer is checked,
// so a sound block that spends it is refused, and the chain stays where it is.
func TestASoundBlockThatSpendsACoinThatCannotBeReadIsRefusedAndTheTipDoesNotMove(t *testing.T) {
	startTestChain(t, "devnet")
	priv, pub, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp + 100
	block1 := extendTestChain(t, timestamp, owner, nil)
	transfer, err := BuildSignedTransfer(priv, pub, testAddress("dest"), cvn("49"), cvn("1"), ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	validBlock := mineTestBlock(t, timestamp+45, testAddress("miner"), []Transaction{transfer}, 0)

	// The coin is spoilt in the file; the block is left as it was.
	coinKey := []byte(utxoKey(block1.Transactions[0].ID, 0))
	updateDB(t, func(dbTx *bbolt.Tx) error {
		return dbTx.Bucket(utxoBucket).Put(coinKey, []byte("{corrupt"))
	})

	if ValidateBlockSignatures(validBlock) {
		t.Fatal("ValidateBlockSignatures accepted a block whose input coin cannot be read")
	}
	if SaveBlockToStorageInternal(validBlock) {
		t.Fatal("SaveBlockToStorageInternal stored a block whose input coin cannot be read")
	}
	if got := GetLatestBlockInternal().Index; got != 1 {
		t.Fatalf("the tip moved to #%d; want #1", got)
	}
	// The lookup says there is no such coin, and does not make one up.
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		if _, ok := bucketLookup(dbTx.Bucket(utxoBucket))(string(coinKey)); ok {
			t.Fatal("bucketLookup found a coin whose record cannot be read")
		}
		return nil
	})
}

// The node's own record of total work is there for its tip but not for the
// block it shares with the peer, and a block below that one cannot be read.
// So the sync pass gets past its first look at its own work and stops where
// it works out the work of its own branch. It keeps its chain, and it does
// not blame the peer, which has done nothing wrong.
func TestASyncPassThatCannotWorkOutTheWorkAtTheSharedBlockKeepsTheNodeOnItsChainAndBlamesNoOne(t *testing.T) {
	startTestChain(t, "devnet")
	b := buildCompetingBranches(t)
	putBranchInPlace(b.light, b.ancestor)

	var heavyChain []Block
	for h := int64(0); h <= b.ancestor.Index; h++ {
		heavyChain = append(heavyChain, GetBlockByHeightFromDB(h))
	}
	heavyChain = append(heavyChain, b.heavy...)
	lightTip := GetLatestBlockInternal()

	// Keep chainWorkBucket[lightTip] intact so startWork succeeds and
	// chooseSyncSource selects heavyChain, then delete chainWorkBucket[ancestor]
	// and corrupt block #1 so chainWorkThroughChecked(ancestor) runs at line 950.
	updateDB(t, func(dbTx *bbolt.Tx) error {
		if err := dbTx.Bucket(blocksBucket).Put(chainWorkKey(1), []byte("{corrupt")); err != nil {
			return err
		}
		return dbTx.Bucket(chainWorkBucket).Delete(chainWorkKey(b.ancestor.Index))
	})

	srv := serveMockSyncPeer(t, heavyChain)
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

	TriggerActiveReSyncPass()
	if got := GetLatestBlockInternal(); got.Hash != lightTip.Hash {
		t.Fatalf("switched away from light tip #%d %.12s to #%d %.12s", lightTip.Index, lightTip.Hash, got.Index, got.Hash)
	}
	syncSetAside.Lock()
	_, blamed := syncSetAside.until[srv.URL]
	syncSetAside.Unlock()
	if blamed {
		t.Fatal("sync source was set aside despite local chain-work failure being blameless")
	}
}

// A node writes its file only by storing valid blocks, taking them off again,
// sweeping and the like. A file written so is never refused at start, with
// the records of total work there or wiped.
func TestAChainBuiltOnlyByValidSequencesIsNeverRefusedAtStartOrWhenItsTotalsOfWorkAreRebuilt(t *testing.T) {
	startTestChain(t, "devnet", func() {
		econ := NewGenesisEconomics
		econ.EpochBlocks = 5
		econ.EmissionEpochs = 2 // emission stops after block #10
		econ.SweepIntervalBlocks = 4
		econ.VaultPayoutBlocks = 5
		Params.Economics = &econ
	})
	t.Cleanup(func() { atomic.StoreInt64(&localHeightState, 0) })

	privA, pubA, alice, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	_, _, bob, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	ts := Params.GenesisTimestamp + 100

	reopenAndCheck := func(label string, deleteWorkIndex bool) {
		t.Helper()
		dbPath := BlockchainFile
		beforeTip := GetLatestBlockInternal()
		beforeWork := TotalChainWork()
		beforeVault := VaultBalanceUnits()
		if deleteWorkIndex {
			updateDB(t, func(tx *bbolt.Tx) error {
				if tx.Bucket(chainWorkBucket) != nil {
					return tx.DeleteBucket(chainWorkBucket)
				}
				return nil
			})
		}
		closeAfterWork(t)

		atomic.StoreInt64(&localHeightState, 0)
		BlockchainFile = dbPath
		if msg, panicked := openingTheEngine(); panicked {
			t.Fatalf("%s (deleteWork=%v): InitBoltEngine falsely refused valid chain: %s", label, deleteWorkIndex, msg)
		}
		if err := checkStoredGenesis(); err != nil {
			t.Fatalf("%s (deleteWork=%v): checkStoredGenesis failed: %v", label, deleteWorkIndex, err)
		}
		afterTip := GetLatestBlockInternal()
		afterWork := TotalChainWork()
		if afterTip.Hash != beforeTip.Hash || afterTip.Index != beforeTip.Index {
			t.Fatalf("%s: tip mismatch after reopen: got #%d %.12s, want #%d %.12s", label, afterTip.Index, afterTip.Hash, beforeTip.Index, beforeTip.Hash)
		}
		if afterWork.Cmp(beforeWork) != 0 {
			t.Fatalf("%s: work mismatch after reopen: got %s, want %s", label, afterWork, beforeWork)
		}
		if VaultBalanceUnits() != beforeVault {
			t.Fatalf("%s: vault mismatch after reopen", label)
		}
	}

	// Block #1: alice gets 50 CVN.
	extendTestChain(t, ts, alice, nil)

	// Block #2: miner claims only 10 CVN (leaving 40 CVN in the vault) and alice creates
	// both a minimum-dust coin (10000) and a boundary coin (12500 -> 10000+2500 at sweep 1,
	// then 10000+0 at sweep 2) sent to bob while keeping her remaining coin.
	ts += 45
	txSplit, err := BuildSignedTransfer(privA, pubA, bob, 12500, 10000, ListSpendableCoins(alice))
	if err != nil {
		t.Fatal(err)
	}
	ts2 := ts
	// Replace outputs to hold 10000 dust, 12500 boundary, and change to alice with 10000 fee.
	coinsA := ListSpendableCoins(alice)
	_ = txSplit
	signedSplit, err := signTransfer(privA, pubA, alice, []UTXOInput{walletInput(coinsA[0])}, []UTXOOutput{
		{Recipient: bob, Amount: UnitsAmount(10000)},
		{Recipient: bob, Amount: UnitsAmount(12500)},
		{Recipient: alice, Amount: UnitsAmount(coinsA[0].Amount.Units() - 32500)},
	}, 10000)
	if err != nil {
		t.Fatal(err)
	}
	b2 := mineTestBlock(t, ts2, testAddress("underclaim"), []Transaction{signedSplit}, cvn("10"))
	if !ValidateBlockSignatures(b2) || !SaveBlockToStorageInternal(b2) {
		t.Fatal("failed storing underclaimed block #2")
	}

	// Blocks #3..#6: triggers sweep 1 at #5 (for #1) and #6 (for #2, touching 12500 + alice change, leaving 10000 dust alone).
	for h := int64(3); h <= 6; h++ {
		ts += 45
		extendTestChain(t, ts, testAddress(fmt.Sprintf("m%d", h)), nil)
	}
	reopenAndCheck("after sweep 1 at #6", false)
	reopenAndCheck("after sweep 1 at #6 with chainwork deleted", true)

	// Spend alice's swept coin (with HeldUnits > 0) in block #7 to reclaim her held fifth.
	var aliceHeld int64
	for _, c := range ListSpendableCoins(alice) {
		aliceHeld += c.HeldUnits
	}
	if aliceHeld <= 0 {
		t.Fatal("set-up: expected alice to hold swept fifth after block #6")
	}
	ts += 45
	reclaimTx, err := BuildSignedRenewal(privA, pubA, cvn("0.01"), ListSpendableCoins(alice))
	if err != nil {
		t.Fatal(err)
	}
	extendTestChain(t, ts, testAddress("m7"), []Transaction{reclaimTx})

	// Blocks #8..#12: crosses sweep 2 for block #2 (#10) and emission end (#10 -> #11/#12 have 0 new emission).
	for h := int64(8); h <= 12; h++ {
		ts += 45
		extendTestChain(t, ts, testAddress(fmt.Sprintf("m%d", h)), nil)
	}
	// Sibling tip replacement at #12: build two blocks at height 12 on parent #11,
	// ensure the second beats the first (lower hash at same work), store loser then winner.
	StateMutex.Lock()
	rollbackChainToHeightLocked(11)
	StateMutex.Unlock()
	reopenAndCheck("immediately after rollback to #11 across post-emission", false)

	cand1 := mineTestBlock(t, ts, testAddress("sib1"), nil, 0)
	var cand2 Block
	for nonceTry := 0; ; nonceTry++ {
		c := mineTestBlock(t, ts, testAddress(fmt.Sprintf("sib2-%d", nonceTry)), nil, 0)
		if siblingWins(c, cand1) {
			cand2 = c
			break
		}
	}
	if !ValidateBlockSignatures(cand1) || !SaveBlockToStorageInternal(cand1) {
		t.Fatal("failed storing sibling candidate 1")
	}
	if !ValidateBlockSignatures(cand2) || !SaveBlockToStorageInternal(cand2) {
		t.Fatal("failed storing winning sibling candidate 2")
	}
	reopenAndCheck("after sibling tip replacement at #12", true)

	// Advance until vault shrinks below VaultPayoutBlocks so allowance == 0 (empty coinbase outputs).
	for h := int64(13); h <= 25; h++ {
		ts += 45
		b := mineTestBlock(t, ts, testAddress("late"), nil, 0)
		if !ValidateBlockSignatures(b) || !SaveBlockToStorageInternal(b) {
			t.Fatalf("failed post-emission block #%d", h)
		}
		if len(b.Transactions[0].Outputs) == 0 {
			break
		}
	}
	reopenAndCheck("post-emission", false)
	reopenAndCheck("post-emission with chainwork wiped", true)

	// Full backup check + quick check must also pass on this chain.
	backupDir := filepath.Join(t.TempDir(), "backups")
	backupPath, _, err := writeBackup(backupDir, 0)
	if err != nil {
		t.Fatalf("writeBackup failed on healthy complex chain: %v", err)
	}
	genesis := buildGenesisBlock().Hash
	closeAfterWork(t)
	if _, err := checkChainFile(backupPath, genesis); err != nil {
		t.Fatalf("checkChainFile failed on healthy complex chain: %v", err)
	}
	if _, err := replayChainFile(backupPath, genesis); err != nil {
		t.Fatalf("replayChainFile failed on healthy complex chain: %v", err)
	}
}

// At start the coins and the vault must add up to what has been issued, and a
// coin must be readable.
func TestANodeThatStartsOnAFileWhoseVaultOrSetAsideFigureOrCoinIsWrongStopsAndLetsTheFileGo(t *testing.T) {
	setupChainWithSweepAndVault := func(t *testing.T) string {
		t.Helper()
		ts := startSweepChain(t)
		tsVal := *ts
		tsVal += 45
		b1 := mineTestBlock(t, tsVal, testAddress("m1"), nil, cvn("40")) // 10 CVN to vault
		if !ValidateBlockSignatures(b1) || !SaveBlockToStorageInternal(b1) {
			t.Fatal("failed storing b1")
		}
		for h := int64(2); h <= 6; h++ {
			tsVal += 45
			extendTestChain(t, tsVal, testAddress("m"), nil)
		}
		return BlockchainFile
	}

	t.Run("deleted vault entry at tip", func(t *testing.T) {
		path := setupChainWithSweepAndVault(t)
		tip := GetLatestBlockInternal()
		updateDB(t, func(tx *bbolt.Tx) error {
			return tx.Bucket(vaultBucket).Delete(chainWorkKey(tip.Index))
		})
		closeAfterWork(t)
		mustRefuseToStartOn(t, path)
	})

	t.Run("altered vault balance at tip", func(t *testing.T) {
		path := setupChainWithSweepAndVault(t)
		tip := GetLatestBlockInternal()
		updateDB(t, func(tx *bbolt.Tx) error {
			return storeVaultUnits(tx, tip, VaultBalanceUnits()+1)
		})
		closeAfterWork(t)
		mustRefuseToStartOn(t, path)
	})

	t.Run("held_units raised by one unit", func(t *testing.T) {
		path := setupChainWithSweepAndVault(t)
		updateDB(t, func(tx *bbolt.Tx) error {
			key := coinsWorthSomething(tx)[0]
			coin := readCoinRecord(t, tx, key)
			coin.HeldUnits++
			return writeCoinRecord(tx, key, coin)
		})
		closeAfterWork(t)
		mustRefuseToStartOn(t, path)
	})

	t.Run("corrupt utxo JSON record", func(t *testing.T) {
		path := setupChainWithSweepAndVault(t)
		updateDB(t, func(tx *bbolt.Tx) error {
			key := coinsWorthSomething(tx)[0]
			return tx.Bucket(utxoBucket).Put([]byte(key), []byte("{corrupt"))
		})
		closeAfterWork(t)
		mustRefuseToStartOn(t, path)
	})
}
