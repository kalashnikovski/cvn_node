package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"go.etcd.io/bbolt"
)

// Global database file handles and structural bucket identifiers
var GlobalBoltEngine *bbolt.DB
var localHeightState int64

var blocksBucket      = []byte("blocks")
var metaBucket        = []byte("meta")
var utxoBucket        = []byte("utxo")
var uncleLedgerBucket = []byte("uncle_ledger") // 👑 NEW EXCLUSIVE GHOST LEDGER PARTITION

// InitBoltEngine mounts the local physical bbolt storage files safely on startup
func InitBoltEngine() {
	db, err := bbolt.Open(BlockchainFile, 0600, nil)
	if err != nil {
		panic(fmt.Sprintf("🚨 CRITICAL STORAGE FAULT: Unable to allocate BoltDB file handles: %v", err))
	}
	GlobalBoltEngine = db

	err = db.Update(func(tx *bbolt.Tx) error {
		bBlocks, _ := tx.CreateBucketIfNotExists(blocksBucket)
		_, _ = tx.CreateBucketIfNotExists(metaBucket)
		bUTXO, _ := tx.CreateBucketIfNotExists(utxoBucket)
		_, _ = tx.CreateBucketIfNotExists(uncleLedgerBucket) // 👑 INITIALIZE THE UNCLE BUCKET

		// 👑 THE DETERMINISTIC GENESIS BOOTSTRAPPER ENGINE: Wiped Clean of Pre-mines
		if bBlocks.Stats().KeyN == 0 {
			paddedZeroKey := fmt.Sprintf("%016d", int64(0))

			genesisBlock := Block{
				Version:      1, // 👑 Establish baseline protocol version parameter
				Index:        0,
				Timestamp:    1790640000, 
				Transactions: []Transaction{
					{
						Version:          1, // Establish baseline transaction version parameter
						ID:               "TX_GENESIS_FAIR_LAUNCH_POOL",
						Witness:          "COVENANT_STEWARD_ASSEMBLY",
						PublicKey:        "GENESIS_VOID_REWARD_POOL",
						Inputs:           []UTXOInput{},
						Outputs:          []UTXOOutput{
							{Recipient: "COVENANT_STEWARD_ASSEMBLY", Amount: 0.0}, // 👑 100% PURE FAIR LAUNCH ARRIVAL
						},
						Timestamp:        1790640000,
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

			utxoRecord := UTXORecord{Amount: 0.0, Recipient: "COVENANT_STEWARD_ASSEMBLY"}
			utxoData, _ := json.Marshal(utxoRecord)
			compoundOutputKey := fmt.Sprintf("TX_GENESIS_FAIR_LAUNCH_POOL_%d", 0)
			_ = bUTXO.Put([]byte(compoundOutputKey), utxoData)
		}
		return nil
	})
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

	RebuildStateBalanceCacheInternal()
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

func SaveBlockToStorageInternal(block Block) bool {
	if GlobalBoltEngine == nil { return false }

	expectedDiff := block.Difficulty
	if block.Index <= 10 {
		expectedDiff = 2
	} else {
		if expectedDiff < 4 {
			return false
		}
	}
	
	targetPrefix := strings.Repeat("0", expectedDiff)
	if !strings.HasPrefix(block.Hash, targetPrefix) || block.Hash != CalculateHash(block) {
		fmt.Printf("🚨 STORAGE REFUSAL: Block #%d failed strict cryptographic verification filters inside database_core!\n", block.Index)
		return false
	}

	err := GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		bMeta := tx.Bucket(metaBucket)
		bUTXO := tx.Bucket(utxoBucket)
		if bBlocks == nil || bMeta == nil || bUTXO == nil { return fmt.Errorf("required buckets are missing") }

		// --- 👑 CRITICAL REORG UTXO SELF-HEALING ENGINE ---
		// Detect if we are overwriting or replacing an existing block tip layout at this height.
		// If an old block exists here, we must restore its consumed inputs back to the unspent pool
		// before validating the new competing block to eliminate reorg deadlock conditions!
		paddedKey := fmt.Sprintf("%016d", block.Index)
		existingBlockBytes := bBlocks.Get([]byte(paddedKey))
		
		if existingBlockBytes != nil {
			var historicalOldBlock Block
			if json.Unmarshal(existingBlockBytes, &historicalOldBlock) == nil {
				for txIdx, oldTx := range historicalOldBlock.Transactions {
					if txIdx == 0 { continue } // Skip coinbase
					
					for _, oldIn := range oldTx.Inputs {
						parentBlk := GetBlockByHeightFromDB(block.Index - 1)
						for _, parentTx := range parentBlk.Transactions {
							if parentTx.ID == oldIn.SourceTxID && oldIn.Index < len(parentTx.Outputs) {
								outTarget := parentTx.Outputs[oldIn.Index]
								restoredRecord := UTXORecord{
									Amount:    outTarget.Amount,
									Recipient: strings.TrimSpace(outTarget.Recipient),
								}
								rebuildBytes, _ := json.Marshal(restoredRecord)
								compoundInputKey := fmt.Sprintf("%s_%d", oldIn.SourceTxID, oldIn.Index)
								_ = bUTXO.Put([]byte(compoundInputKey), rebuildBytes)
							}
						}
					}
					for outIdx := range oldTx.Outputs {
						staleOutputKey := fmt.Sprintf("%s_%d", oldTx.ID, outIdx)
						_ = bUTXO.Delete([]byte(staleOutputKey))
					}
				}
			}
		}

		for txIdx, txPayload := range block.Transactions {
			if txIdx == 0 { continue }
			if !VerifyTransactionIntegrity(txPayload, int(block.Index), txIdx, bUTXO) {
				return fmt.Errorf("transaction verification failed for txID: %s", txPayload.ID)
			}
		}

		data, err := json.Marshal(block)
		if err != nil { return err }

		_ = bBlocks.Put([]byte(paddedKey), data)
		_ = bMeta.Put([]byte("height"), []byte(paddedKey))

		for txIdx, txPayload := range block.Transactions {
			if txIdx != 0 {
				for _, input := range txPayload.Inputs {
					compoundInputKey := fmt.Sprintf("%s_%d", input.SourceTxID, input.Index)
					_ = bUTXO.Delete([]byte(compoundInputKey))
				}
			}
			for outIdx, output := range txPayload.Outputs {
				compoundOutputKey := fmt.Sprintf("%s_%d", txPayload.ID, outIdx)
				sanitizedRecipient := strings.TrimSpace(output.Recipient)
				utxoRecord := UTXORecord{Amount: output.Amount, Recipient: sanitizedRecipient}
				utxoData, _ := json.Marshal(utxoRecord)
				_ = bUTXO.Put([]byte(compoundOutputKey), utxoData)
			}
		}

		if block.Hash != "MEMPOOL_GOSSIP_WIRE_ROUTE" && len(block.Transactions) > 0 {
			go func(b Block) {
				jsonBytes, err := json.Marshal(b)
				if err == nil {
					payload := fmt.Sprintf("BLOCK_BROADCAST:%s\n", string(jsonBytes))
					transmitPayloadToNetworkBoundary(payload)
				}
			}(block)
		}
		return nil
	})

	if err != nil { return false }
	atomic.StoreInt64(&localHeightState, block.Index)
	RebuildStateBalanceCacheInternal()
	return true
}

// RebuildStateBalanceCache wraps our core memory map compiler inside global thread hold gates
func RebuildStateBalanceCache() {
	RebuildStateBalanceCacheInternal()
}

func RebuildStateBalanceCacheInternal() {
	BalanceCacheMutex.Lock()
	defer BalanceCacheMutex.Unlock()
	
	StateBalanceCache = make(map[string]float64)
	if GlobalBoltEngine == nil {
		return
	}

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bUTXO := tx.Bucket(utxoBucket)
		bBlocks := tx.Bucket(blocksBucket)
		bUncle := tx.Bucket(uncleLedgerBucket) // 👑 OPEN THE UNCLE ACCOUNTING PARTITION
		
		if bUTXO != nil {
			c := bUTXO.Cursor()
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var record UTXORecord
				if json.Unmarshal(v, &record) == nil {
					sanitizedRecipient := strings.TrimSpace(record.Recipient)
					StateBalanceCache[sanitizedRecipient] += record.Amount
				}
			}
		}

		// 👑 PARSE UNCLE REWARD BALANCES DIRECTLY FROM THE INDEPENDENT STORAGE PARTITIONS
		if bUncle != nil {
			uc := bUncle.Cursor()
			for k, v := uc.First(); k != nil; k, v = uc.Next() {
				var meta map[string]string
				if json.Unmarshal(v, &meta) == nil {
					uncleMinerAddress := strings.TrimSpace(meta["uncle_miner"])
					var heightIndex int64
					_, err := fmt.Sscanf(string(k), "UNCLE_METADATA_%d", &heightIndex)
					if err == nil && uncleMinerAddress != "" && uncleMinerAddress != "NONE" {
						totalBlockEmission := CalculateBlockReward(heightIndex)
						uncleSubsidy := totalBlockEmission * 0.20
						StateBalanceCache[uncleMinerAddress] += uncleSubsidy
					}
				}
			}
		}

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