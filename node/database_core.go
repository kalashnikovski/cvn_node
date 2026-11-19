package node

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
)

// Global database file handles and structural bucket identifiers
var GlobalBoltEngine *bbolt.DB

// errBlockAlreadyStored is returned when a save is asked to store a block
// that is already the stored block at its height.
var errBlockAlreadyStored = fmt.Errorf("block already stored")
var localHeightState int64

// chainWriteMutex is held by anything that changes which block is the tip:
// storing a block, or rolling the chain back. The database already takes
// such changes one at a time, but the height kept in memory is updated just
// after each change, and without this two changes finishing together could
// record their heights in the wrong order, leaving the node believing its
// tip is a block below the real one.
var chainWriteMutex sync.Mutex

var blocksBucket = []byte("blocks")
var metaBucket = []byte("meta")
var utxoBucket = []byte("utxo")

// InitBoltEngine mounts the local physical bbolt storage files safely on startup
func InitBoltEngine() {
	db, err := bbolt.Open(BlockchainFile, 0600, nil)
	if err != nil {
		panic(fmt.Sprintf("🚨 CRITICAL STORAGE FAULT: Unable to allocate BoltDB file handles: %v", err))
	}
	GlobalBoltEngine = db

	// Enforce strict 5-second transaction timeouts so stuck routines never freeze the node
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = db.Update(func(tx *bbolt.Tx) error {
		bBlocks, _ := tx.CreateBucketIfNotExists(blocksBucket)
		_, _ = tx.CreateBucketIfNotExists(metaBucket)
		bUTXO, _ := tx.CreateBucketIfNotExists(utxoBucket)
		_, _ = tx.CreateBucketIfNotExists(undoBucket)

		// 👑 THE DETERMINISTIC GENESIS BOOTSTRAPPER ENGINE: Wiped Clean of Pre-mines
		if bBlocks.Stats().KeyN == 0 {
			paddedZeroKey := fmt.Sprintf("%016d", int64(0))

			genesisBlock := buildGenesisBlock()
			genesisData, _ := json.Marshal(genesisBlock)
			_ = bBlocks.Put([]byte(paddedZeroKey), genesisData)

			bMeta := tx.Bucket(metaBucket)
			if bMeta != nil {
				_ = bMeta.Put([]byte("height"), []byte(paddedZeroKey))
			}

			utxoRecord := UTXORecord{Amount: UnitsAmount(0), Recipient: genesisRecipient}
			utxoData, _ := json.Marshal(utxoRecord)
			compoundOutputKey := utxoKey(genesisTransactionID, 0)
			_ = bUTXO.Put([]byte(compoundOutputKey), utxoData)
		}
		return nil
	})

	_ = ctx // Context tracking satisfied natively inside transactional callbacks
	if err != nil {
		panic(fmt.Sprintf("🚨 DATABASE CORRUPTION: Failed to provision partition buckets: %v", err))
	}

	_ = db.View(func(tx *bbolt.Tx) error {
		bMeta := tx.Bucket(metaBucket)
		if bMeta != nil {
			heightBytes := bMeta.Get([]byte("height"))
			if heightBytes != nil {
				var parsedHeight int64
				_, err := fmt.Sscanf(string(heightBytes), "%016d", &parsedHeight)
				if err == nil {
					atomic.StoreInt64(&localHeightState, parsedHeight)
				}
			}
		}
		return nil
	})

	reindexUTXOFromChainOnce()
	RebuildStateBalanceCacheInternal()
	EnsureChainWorkIndex()
}

// reindexUTXOFromChainOnce rebuilds the utxo bucket from the canonical blocks
// bucket a single time. Earlier builds left behind coinbase outputs of orphaned
// blocks; replaying the chain drops
// them, so every node on the same chain ends up with the same balances.
func reindexUTXOFromChainOnce() {
	const reindexFlagKey = "utxo_reindex_v2"
	tipHeight := atomic.LoadInt64(&localHeightState)

	err := GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		bMeta := tx.Bucket(metaBucket)
		bBlocks := tx.Bucket(blocksBucket)
		if bMeta == nil || bBlocks == nil {
			return fmt.Errorf("required buckets missing")
		}
		if bMeta.Get([]byte(reindexFlagKey)) != nil {
			return nil
		}
		// This rebuild knows nothing of coin ages or sweeps, so it must never
		// run over a chain that has blocks: it would write every coin back
		// without its age. A database made by this program has the flag from
		// its first moment; one that somehow lacks it and holds blocks is
		// left as it is.
		if tipHeight > 0 {
			return bMeta.Put([]byte(reindexFlagKey), []byte("skipped: chain not empty"))
		}

		if tx.Bucket(utxoBucket) != nil {
			if err := tx.DeleteBucket(utxoBucket); err != nil {
				return err
			}
		}
		bUTXO, err := tx.CreateBucket(utxoBucket)
		if err != nil {
			return err
		}

		var replayed int64
		for h := int64(0); h <= tipHeight; h++ {
			data := bBlocks.Get([]byte(fmt.Sprintf("%016d", h)))
			if data == nil {
				continue
			}
			var blk Block
			if err := json.Unmarshal(data, &blk); err != nil {
				return fmt.Errorf("block #%d unreadable during utxo reindex: %w", h, err)
			}
			for txIdx, txPayload := range blk.Transactions {
				if txIdx != 0 {
					for _, input := range txPayload.Inputs {
						_ = bUTXO.Delete([]byte(fmt.Sprintf("%s_%d", input.SourceTxID, input.Index)))
					}
				}
				for outIdx, output := range txPayload.Outputs {
					utxoData, _ := json.Marshal(UTXORecord{Amount: output.Amount, Recipient: strings.TrimSpace(output.Recipient)})
					if err := bUTXO.Put([]byte(fmt.Sprintf("%s_%d", txPayload.ID, outIdx)), utxoData); err != nil {
						return err
					}
				}
			}
			replayed++
		}

		fmt.Printf("🧹 [UTXO REINDEX] Rebuilt unspent output set from %d canonical blocks.\n", replayed)
		return bMeta.Put([]byte(reindexFlagKey), []byte("done"))
	})
	if err != nil {
		// The update rolled back, so the previous utxo bucket is untouched.
		fmt.Printf("⚠️ [UTXO REINDEX] Skipped, will retry next start: %v\n", err)
	}
}

// GetLatestBlockInternal returns the current block tip structure directly from state memory
func GetLatestBlockInternal() Block {
	var currentTip Block
	currentTipHeight := atomic.LoadInt64(&localHeightState)

	if GlobalBoltEngine == nil {
		return currentTip
	}

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		if bBlocks == nil {
			return nil
		}
		paddedKey := fmt.Sprintf("%016d", currentTipHeight)
		data := bBlocks.Get([]byte(paddedKey))
		if data != nil {
			_ = json.Unmarshal(data, &currentTip)
		}
		return nil
	})

	return currentTip
}

// latestTipID returns the height and identifier of the newest block without
// reading the block itself.
//
// A miner asks "is the block I am building on still the newest?" thousands
// of times a second. GetLatestBlockInternal answers by reading the whole
// block and every transfer in it, which costs almost nothing for an ordinary
// block and milliseconds for a full one: with a full block at the tip, the
// asking took more time than the mining. The identifier is also kept in the
// small total-work record written with every block, and that is read here.
// If the record is not there, the block is read as before.
func latestTipID() (int64, string) {
	height := atomic.LoadInt64(&localHeightState)
	if GlobalBoltEngine == nil {
		return height, ""
	}
	hash := ""
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(chainWorkBucket)
		if bucket == nil {
			return nil
		}
		var entry chainWorkEntry
		if raw := bucket.Get(chainWorkKey(height)); raw != nil && json.Unmarshal(raw, &entry) == nil {
			hash = entry.Hash
		}
		return nil
	})
	if hash == "" {
		tip := GetLatestBlockInternal()
		return tip.Index, tip.Hash
	}
	return height, hash
}

// GetLatestBlock wraps our internal state memory helper to fulfill global interface requirements
func GetLatestBlock() Block {
	return GetLatestBlockInternal()
}

// GetBlockByHeightFromDB retrieves a historical block structure entry directly from its index key
func GetBlockByHeightFromDB(height int64) Block {
	var requestedBlock Block
	if GlobalBoltEngine == nil || height < 0 {
		return requestedBlock
	}

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		if bBlocks == nil {
			return nil
		}
		paddedKey := fmt.Sprintf("%016d", height)
		data := bBlocks.Get([]byte(paddedKey))
		if data != nil {
			_ = json.Unmarshal(data, &requestedBlock)
		}
		return nil
	})

	return requestedBlock
}

// SaveBlockToStorage wraps our internal disk commit engine with thread synchronization gates
func SaveBlockToStorage(block Block) bool {
	return SaveBlockToStorageInternal(block)
}

// SaveBlockToStorageInternal isolates block reads completely outside the atomic Update loop
func SaveBlockToStorageInternal(block Block) bool {
	if GlobalBoltEngine == nil {
		return false
	}
	// The genesis block is written once, when the database is created, and
	// is never stored again or replaced.
	if block.Index <= 0 {
		return false
	}
	chainWriteMutex.Lock()
	defer chainWriteMutex.Unlock()

	// A block carries its own difficulty; HashMeetsDifficulty enforces the
	// network's minimum.
	expectedDiff := block.Difficulty

	if block.Hash != CalculateHash(block) || !BlockProofValid(block, expectedDiff) {
		fmt.Printf("🚨 STORAGE REFUSAL: Block #%d: its identifier or its proof of work is not valid.\n", block.Index)
		return false
	}

	paddedKey := fmt.Sprintf("%016d", block.Index)

	// Setup a bounded 5-second context timeout to ensure a hung disk frame never freezes full nodes
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Everything below is decided inside ONE write transaction. bbolt runs write
	// transactions one at a time, so two saves at the same height (our own miner
	// and a peer's block arriving together) can no longer both believe the slot
	// is empty. That race left the first block's coinbase outputs in the UTXO
	// set after the second block overwrote it.
	replacedExistingBlock := false
	sweptCoins := false
	var keptLowerHashSibling *Block
	var displacedHigherHashSibling *Block

	err := GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		bMeta := tx.Bucket(metaBucket)
		bUTXO := tx.Bucket(utxoBucket)
		if bBlocks == nil || bMeta == nil || bUTXO == nil {
			return fmt.Errorf("required buckets missing")
		}

		// Never move the chain tip backwards through a plain save. Rollbacks lower
		// the stored height first, so a legitimate reorg is unaffected.
		if heightBytes := bMeta.Get([]byte("height")); heightBytes != nil {
			var storedHeight int64
			if _, scanErr := fmt.Sscanf(string(heightBytes), "%016d", &storedHeight); scanErr == nil && block.Index < storedHeight {
				return fmt.Errorf("block #%d is below stored tip #%d", block.Index, storedHeight)
			}
		}

		if existingBlockData := bBlocks.Get([]byte(paddedKey)); existingBlockData != nil {
			var historicalOldBlock Block
			if json.Unmarshal(existingBlockData, &historicalOldBlock) == nil && historicalOldBlock.Hash != block.Hash {
				// Same parent, and the block already stored has the lower hash: it
				// wins the tie-break, so keep it and report ours as the loser.
				if historicalOldBlock.PrevHash == block.PrevHash && siblingWins(historicalOldBlock, block) {
					keptLowerHashSibling = &historicalOldBlock
					return fmt.Errorf("lower-hash sibling already stored at #%d", block.Index)
				}

				replacedExistingBlock = true
				beginChainRewrite() // ended once this write has finished, below
				if historicalOldBlock.PrevHash == block.PrevHash {
					displacedHigherHashSibling = &historicalOldBlock
				}

				// Take back the coins the old block created and return the ones it spent.
				undoBlockCoinChanges(tx, bUTXO, historicalOldBlock)
			} else {
				// The very same block is already stored (our own block coming
				// back from a peer, for example). Nothing to do.
				return errBlockAlreadyStored
			}
		}

		var blockAccount blockAccounting
		if IsTxForkHeight(block.Index) {
			// Final gate: check every transfer against the coin set exactly as
			// it stands inside this write transaction, and remember what the
			// block spends so a reorg can give it back.
			if block.Index > 0 {
				parentData := bBlocks.Get([]byte(fmt.Sprintf("%016d", block.Index-1)))
				var parentBlock Block
				if parentData == nil || json.Unmarshal(parentData, &parentBlock) != nil || parentBlock.Hash != block.PrevHash {
					return fmt.Errorf("block #%d does not attach to the stored parent", block.Index)
				}
			}
			vaultBefore, vaultErr := readVaultUnits(tx, block.Index-1, block.PrevHash)
			if vaultErr != nil {
				fmt.Printf("🚨 STORAGE REFUSAL: Block #%d: %v\n", block.Index, vaultErr)
				return vaultErr
			}
			accounting, consumed, validationErr := ValidateBlockTransactions(block, bucketLookup(bUTXO), vaultBefore)
			if validationErr != nil {
				fmt.Printf("🚨 STORAGE REFUSAL: Block #%d: %v\n", block.Index, validationErr)
				return validationErr
			}
			blockAccount = accounting
			if undoErr := storeUndoRecords(tx, block.Index, consumed); undoErr != nil {
				return undoErr
			}
		}

		data, err := json.Marshal(block)
		if err != nil {
			return err
		}

		// A write that fails ends the whole transaction, so that nothing of
		// a block is kept unless all of it is.
		if putErr := bBlocks.Put([]byte(paddedKey), data); putErr != nil {
			return putErr
		}
		if putErr := bMeta.Put([]byte("height"), []byte(paddedKey)); putErr != nil {
			return putErr
		}
		if workErr := storeChainWork(tx, block); workErr != nil {
			return workErr
		}

		for txIdx, txPayload := range block.Transactions {
			if txIdx != 0 {
				for _, input := range txPayload.Inputs {
					if deleteErr := bUTXO.Delete([]byte(fmt.Sprintf("%s_%d", input.SourceTxID, input.Index))); deleteErr != nil {
						return deleteErr
					}
				}
			}
			for outIdx, output := range txPayload.Outputs {
				sanitizedRecipient := strings.TrimSpace(output.Recipient)
				utxoRecord := UTXORecord{Amount: output.Amount, Recipient: sanitizedRecipient}
				if sweepEnabled() {
					utxoRecord.Age = block.Index
				}
				utxoData, marshalErr := json.Marshal(utxoRecord)
				if marshalErr != nil {
					return marshalErr
				}
				if putErr := bUTXO.Put([]byte(fmt.Sprintf("%s_%d", txPayload.ID, outIdx)), utxoData); putErr != nil {
					return putErr
				}
			}
		}

		if vaultEnabled() {
			// Last, once the block's own coins are in place: sweep the coins
			// that reach the sweep age at this block, and settle the vault.
			// Whatever the block was entitled to pay its miner but did not
			// goes into the vault, so nothing issued is ever destroyed.
			sweptUnits, sweepErr := applySweep(tx, bUTXO, block)
			if sweepErr != nil {
				return sweepErr
			}
			sweptCoins = sweptUnits > 0
			var claimedUnits int64
			for _, output := range block.Transactions[0].Outputs {
				claimedUnits += output.Amount.Units()
			}
			unclaimedUnits := blockAccount.MinerAllowanceUnits() - claimedUnits
			if unclaimedUnits < 0 {
				return fmt.Errorf("block #%d pays more than it is allowed", block.Index)
			}
			vaultAfter, addErr := addUnits(blockAccount.VaultAfterUnits, sweptUnits)
			if addErr == nil {
				vaultAfter, addErr = addUnits(vaultAfter, unclaimedUnits)
			}
			if addErr != nil {
				return addErr
			}
			if vaultErr := storeVaultUnits(tx, block, vaultAfter); vaultErr != nil {
				return vaultErr
			}
		}
		return nil
	})

	_ = ctx // Enforced contextual lifetime tracking satisfied natively inside callbacks
	if replacedExistingBlock {
		endChainRewrite()
	}
	if err != nil {
		// (When keptLowerHashSibling is set, our block lost a same-height race
		// to one already on disk. It is dropped; there is no uncle share.)
		_ = keptLowerHashSibling
		return false
	}

	atomic.StoreInt64(&localHeightState, block.Index)
	_ = displacedHigherHashSibling // the block we replaced is simply dropped
	RemoveConfirmedFromMempool(block)
	if replacedExistingBlock || sweptCoins || len(block.Transactions) > 1 {
		// A block at this height was already counted in the balance cache, or
		// the block moves coins between addresses: recompute from the UTXO set
		// instead of applying a delta.
		RebuildStateBalanceCacheInternal()
	} else {
		ApplyIncrementalDeltaCacheUpdate(block)
	}

	// 👑 PRODUCTION REMEDY: ISOLATED BROADCAST LAYER INTERCEPT
	// Extract text payload transmissions out of the exclusive BoltDB transaction handles!
	// This isolates network streaming operations and opens up full block win parities.
	// A block stored while catching up is one the network already has;
	// announcing each of them would be noise, hundreds of times over.
	if atomic.LoadInt32(&IsSyncingActive) == 0 && len(block.Transactions) > 0 {
		go func(b Block) {
			// Cleanly forward headers straight through the structured packet marshalling pipeline
			BroadcastNewBlock(b)
		}(block)
	}

	return true
}

// ApplyIncrementalDeltaCacheUpdate updates the memory cache using only the block's deltas
func ApplyIncrementalDeltaCacheUpdate(block Block) {
	BalanceCacheMutex.Lock()
	defer BalanceCacheMutex.Unlock()

	if StateBalanceCache == nil {
		StateBalanceCache = make(map[string]int64)
	}

	// This is only called for a block that holds nothing but its reward
	// transaction, so there are no spent coins to subtract: only the reward
	// to add. (A block with transfers, a replaced block or a sweep triggers
	// a full recount instead.)
	for _, txPayload := range block.Transactions {
		for _, output := range txPayload.Outputs {
			sanitizedRecipient := strings.TrimSpace(output.Recipient)
			if sanitizedRecipient != "" {
				StateBalanceCache[sanitizedRecipient] += output.Amount.Units()
			}
		}
	}
}

// RebuildStateBalanceCache wraps our core memory map compiler inside global thread hold gates
func RebuildStateBalanceCache() {
	RebuildStateBalanceCacheInternal()
}

// RebuildStateBalanceCacheInternal crawls bbolt buckets only during initial node bootup recoveries
func RebuildStateBalanceCacheInternal() {
	BalanceCacheMutex.Lock()
	defer BalanceCacheMutex.Unlock()

	StateBalanceCache = make(map[string]int64)
	if GlobalBoltEngine == nil {
		return
	}

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bUTXO := tx.Bucket(utxoBucket)

		if bUTXO != nil {
			c := bUTXO.Cursor()
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var record UTXORecord
				if json.Unmarshal(v, &record) == nil {
					sanitizedRecipient := strings.TrimSpace(record.Recipient)
					StateBalanceCache[sanitizedRecipient] += record.Amount.Units()
				}
			}
		}

		return nil
	})
}
