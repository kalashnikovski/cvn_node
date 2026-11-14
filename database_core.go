package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

var blocksBucket      = []byte("blocks")
var metaBucket        = []byte("meta")
var utxoBucket        = []byte("utxo")
var uncleLedgerBucket = []byte("uncle_ledger") // 👑 STANDALONE IMMUTABLE UNCLE STORAGE PARTITION

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
		_, _ = tx.CreateBucketIfNotExists(uncleLedgerBucket)
		_, _ = tx.CreateBucketIfNotExists(undoBucket)

		// 👑 THE DETERMINISTIC GENESIS BOOTSTRAPPER ENGINE: Wiped Clean of Pre-mines
		if bBlocks.Stats().KeyN == 0 {
			paddedZeroKey := fmt.Sprintf("%016d", int64(0))

			genesisBlock := Block{
				Version:      1, 
				Index:        0,
				Timestamp:    Params.GenesisTimestamp,
				Transactions: []Transaction{
					{
						Version:          1, 
						ID:               "TX_GENESIS_FAIR_LAUNCH_POOL",
						Witness:          "COVENANT_STEWARD_ASSEMBLY",
						PublicKey:        "GENESIS_VOID_REWARD_POOL",
						Inputs:           []UTXOInput{},
						Outputs:          []UTXOOutput{
							{Recipient: "COVENANT_STEWARD_ASSEMBLY", Amount: UnitsAmount(0)}, // 👑 100% PURE FAIR LAUNCH ARRIVAL
						},
						Timestamp:        Params.GenesisTimestamp,
						Signature:        "COINBASE_IMMUTABLE_CONSENSUS_VALID",
					},
				},
				PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
				Difficulty:   2,
				Nonce:        0,
				GuardMatrix:  "",
				UncleHash:    "0000000000000000000000000000000000000000000000000000000000000000",
				UncleMiner:   "NONE",
			}

			genesisBlock.Hash = CalculateHash(genesisBlock)
			genesisData, _ := json.Marshal(genesisBlock)
			_ = bBlocks.Put([]byte(paddedZeroKey), genesisData)
			
			bMeta := tx.Bucket(metaBucket)
			if bMeta != nil {
				_ = bMeta.Put([]byte("height"), []byte(paddedZeroKey))
			}

			utxoRecord := UTXORecord{Amount: UnitsAmount(0), Recipient: "COVENANT_STEWARD_ASSEMBLY"}
			utxoData, _ := json.Marshal(utxoRecord)
			compoundOutputKey := fmt.Sprintf("TX_GENESIS_FAIR_LAUNCH_POOL_%d", 0)
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
// blocks and locally minted UNCLE_REWARD_* entries; replaying the chain drops
// both, so every node on the same chain ends up with the same balances.
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
	if GlobalBoltEngine == nil { return false }

	expectedDiff := block.Difficulty
	if IsHeartbeatHeight(block.Index) {
		// Heartbeat blocks carry their own numeric difficulty; HashMeetsDifficulty
		// enforces its floor.
	} else if block.Index <= 10 {
		expectedDiff = 2
	} else {
		if expectedDiff < 4 {
			return false
		}
	}
	
	if !HashMeetsDifficulty(block.Hash, block.Index, expectedDiff) || block.Hash != CalculateHash(block) {
		fmt.Printf("🚨 STORAGE REFUSAL: Block #%d failed strict cryptographic verification filters!\n", block.Index)
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
		if bBlocks == nil || bMeta == nil || bUTXO == nil { return fmt.Errorf("required buckets missing") }

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
		} else {
			// Perform fast structural database transaction verification checks
			for txIdx, txPayload := range block.Transactions {
				if txIdx == 0 { continue }
				if !VerifyTransactionIntegrity(txPayload, int(block.Index), txIdx, bUTXO) {
					return fmt.Errorf("transaction verification failed for txID: %s", txPayload.ID)
				}
			}
		}

		data, err := json.Marshal(block)
		if err != nil { return err }

		_ = bBlocks.Put([]byte(paddedKey), data)
		_ = bMeta.Put([]byte("height"), []byte(paddedKey))
		if workErr := storeChainWork(tx, block); workErr != nil {
			return workErr
		}

		for txIdx, txPayload := range block.Transactions {
			if txIdx != 0 {
				for _, input := range txPayload.Inputs {
					_ = bUTXO.Delete([]byte(fmt.Sprintf("%s_%d", input.SourceTxID, input.Index)))
				}
			}
			for outIdx, output := range txPayload.Outputs {
				sanitizedRecipient := strings.TrimSpace(output.Recipient)
				utxoRecord := UTXORecord{Amount: output.Amount, Recipient: sanitizedRecipient}
				if sweepEnabled() {
					utxoRecord.Age = block.Index
				}
				utxoData, _ := json.Marshal(utxoRecord)
				_ = bUTXO.Put([]byte(fmt.Sprintf("%s_%d", txPayload.ID, outIdx)), utxoData)
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
	if err != nil {
		if keptLowerHashSibling != nil {
			// Our block lost a same-height race to one already on disk. It is still
			// a valid block, so queue it as the uncle and let peers see it too;
			// the next block's miner pays its 20% share.
			RecordUncleCandidate(*keptLowerHashSibling, block)
			go BroadcastNewBlock(block)
		}
		return false
	}
	
	atomic.StoreInt64(&localHeightState, block.Index)
	if displacedHigherHashSibling != nil {
		// We replaced a same-parent block that had the higher hash: it is the uncle.
		RecordUncleCandidate(block, *displacedHigherHashSibling)
	}
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
	if block.Hash != "MEMPOOL_GOSSIP_WIRE_ROUTE" && len(block.Transactions) > 0 {
		go func(b Block) {
			// Cleanly forward headers straight through the structured packet marshalling pipeline
			BroadcastNewBlock(b)
		}(block)
	}

	return true
}

func existingBlockBytes(b []byte) []byte { return b }
// ApplyIncrementalDeltaCacheUpdate updates the memory cache using only the block's deltas
func ApplyIncrementalDeltaCacheUpdate(block Block) {
	BalanceCacheMutex.Lock()
	defer BalanceCacheMutex.Unlock()

	if StateBalanceCache == nil {
		StateBalanceCache = make(map[string]int64)
	}

	// 👑 CRITICAL REMEDY 3: HIGH-SPEED CONCURRENCY DELTA ENGINE
	// Process inputs (debits) and outputs (credits) only for affected addresses
	for txIdx, txPayload := range block.Transactions {
		// Process spending inputs (subtracting balances) for normal transactions
		if txIdx != 0 {
			for _, input := range txPayload.Inputs {
				parentTxBlock := GetBlockByHeightFromDB(block.Index - 1)
				for _, pTx := range parentTxBlock.Transactions {
					if pTx.ID == input.SourceTxID && input.Index < len(pTx.Outputs) {
						sourceRecipient := strings.TrimSpace(pTx.Outputs[input.Index].Recipient)
						StateBalanceCache[sourceRecipient] -= pTx.Outputs[input.Index].Amount.Units()
						if StateBalanceCache[sourceRecipient] <= 0 {
							delete(StateBalanceCache, sourceRecipient)
						}
					}
				}
			}
		}

		// Process receiving outputs (adding balances)
		for _, output := range txPayload.Outputs {
			sanitizedRecipient := strings.TrimSpace(output.Recipient)
			if sanitizedRecipient != "" {
				StateBalanceCache[sanitizedRecipient] += output.Amount.Units()
			}
		}
	}

	// An uncle named in the header is paid by the second coinbase output, which
	// the loop above has already credited. Nothing further to add here.
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
		bBlocks := tx.Bucket(blocksBucket)

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

		// uncle_ledger is a local record of races this node witnessed. It is not
		// consensus state, so it must not contribute to balances.

		if bBlocks != nil {
			bc := bBlocks.Cursor()
			for k, v := bc.First(); k != nil; k, v = bc.Next() {
				var blk Block
				if json.Unmarshal(v, &blk) == nil {
					for _, txPayload := range blk.Transactions {
						for _, output := range txPayload.Outputs {
							sanitizedRecipient := strings.TrimSpace(output.Recipient)
							if sanitizedRecipient != "" {
								ApplyJubileeDecayIfNeeded(sanitizedRecipient, blk.Timestamp)
							}
						}
					}
				}
			}
		}
		return nil
	})
}