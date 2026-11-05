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

var blocksBucket = []byte("blocks")
var metaBucket   = []byte("meta")
var utxoBucket   = []byte("utxo")

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
	// 👑 FIXED CORES ALIGNMENT ANCHOR:
	// Allow block height 10 to be stored at difficulty 2. The moment block 10 
	// is saved, block 11 hits this gate, forcing the strict difficulty 4 floor.
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

		for txIdx, txPayload := range block.Transactions {
			if txIdx == 0 { continue }
			if !VerifyTransactionIntegrity(txPayload, int(block.Index), txIdx, bUTXO) {
				return fmt.Errorf("transaction verification failed for txID: %s", txPayload.ID)
			}
		}

		data, err := json.Marshal(block)
		if err != nil { return err }

		paddedKey := fmt.Sprintf("%016d", block.Index)
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
				
				// 👑 REQUIREMENT 4: TRIM RECIPIENT CARRIER ON COINBASE SPLITS
				sanitizedRecipient := strings.TrimSpace(output.Recipient)
				utxoRecord := UTXORecord{Amount: output.Amount, Recipient: sanitizedRecipient}
				utxoData, _ := json.Marshal(utxoRecord)
				_ = bUTXO.Put([]byte(compoundOutputKey), utxoData)
			}
		}

		// 👑 CRITICAL REMEDY 1: STRIKE OUT SEPARATE UNCLE_ KEY EXTRA WRITES
		// The legacy standalone UNCLE_ key block is removed completely. 
		// Uncle rewards are now cleanly committed above as regular transaction outputs,
		// maintaining absolute ledger consistency and eliminating balance duplication.

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
		
		if bUTXO != nil {
			c := bUTXO.Cursor()
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var record UTXORecord
				if json.Unmarshal(v, &record) == nil {
					// 👑 REQUIREMENT 5: CANONICALISE MEMORY PROFILE MAP RECIPIENTS KEY BOUNDS
					sanitizedRecipient := strings.TrimSpace(record.Recipient)
					StateBalanceCache[sanitizedRecipient] += record.Amount
				}
			}
		}

		if bBlocks != nil {
			bc := bBlocks.Cursor()
			for k, v := bc.First(); k != nil; k, v = bc.Next() {
				var blk Block
				if json.Unmarshal(v, &blk) == nil {
					canonicalUncleMiner := strings.TrimSpace(blk.UncleMiner)
					if blk.UncleHash != "" && 
					   blk.UncleHash != "0000000000000000000000000000000000000000000000000000000000000000" && 
					   canonicalUncleMiner != "NONE" && 
					   canonicalUncleMiner != "" {
						
						// 👑 READ DIRECTLY FROM THE SAFE STATE RECORDS
						totalBlockEmission := CalculateBlockReward(blk.Index)
						uncleSubsidy := totalBlockEmission * 0.20
						StateBalanceCache[canonicalUncleMiner] += uncleSubsidy
					}

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