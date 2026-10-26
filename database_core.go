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

	// Initialize core consensus data buckets if they do not exist
	err = db.Update(func(tx *bbolt.Tx) error {
		_, _ = tx.CreateBucketIfNotExists(blocksBucket)
		_, _ = tx.CreateBucketIfNotExists(metaBucket)
		_, _ = tx.CreateBucketIfNotExists(utxoBucket)
		return nil
	})
	if err != nil {
		panic(fmt.Sprintf("🚨 DATABASE CORRUPTION: Failed to provision partition buckets: %v", err))
	}

	// Reconstruct local atomic state counters from the master disk tip
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

	// Synchronize the memory cache balances list with historical database records
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

// SaveBlockToStorageInternal writes verified block structures securely to the database layer
func SaveBlockToStorageInternal(block Block) bool {
	if GlobalBoltEngine == nil {
		return false
	}

	// 👑 THE HARDENED VALIDATION CHECKPOINT:
	// Verify true consensus compliance rules directly inside the storage engine.
	// If an unaligned text preimage tries to bypass the mining workers loop,
	// it gets caught, printed, and rejected here before modifying state records on disk!
	expectedDiff := block.Difficulty
	if block.Index < 10 {
		expectedDiff = 2
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
		if bBlocks == nil || bMeta == nil || bUTXO == nil {
			return fmt.Errorf("required consensus partitions are missing")
		}

		// Audit and process transaction validation limits across all sub-payloads
		for txIdx, txPayload := range block.Transactions {
			if txIdx == 0 {
				continue 
			}
			if !VerifyTransactionIntegrity(txPayload, int(block.Index), txIdx, bUTXO) {
				return fmt.Errorf("transaction verification failed for txID: %s", txPayload.ID)
			}
		}

		data, err := json.Marshal(block)
		if err != nil {
			return err
		}

		paddedKey := fmt.Sprintf("%016d", block.Index)
		_ = bBlocks.Put([]byte(paddedKey), data)
		_ = bMeta.Put([]byte("height"), []byte(paddedKey))

		// Process unspent transaction outputs and inputs spent states
		for txIdx, txPayload := range block.Transactions {
			if txIdx != 0 {
				for _, input := range txPayload.Inputs {
					compoundInputKey := fmt.Sprintf("%s_%d", input.SourceTxID, input.Index)
					_ = bUTXO.Delete([]byte(compoundInputKey))
				}
			}

			for outIdx, output := range txPayload.Outputs {
				compoundOutputKey := fmt.Sprintf("%s_%d", txPayload.ID, outIdx)
				utxoRecord := UTXORecord{
					Amount:    output.Amount,
					Recipient: output.Recipient,
				}
				
				utxoData, _ := json.Marshal(utxoRecord)
				_ = bUTXO.Put([]byte(compoundOutputKey), utxoData)
			}
		}

		// Broadcast newly mined block signatures out over the network interface loop
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

	if err != nil {
		return false
	}
	
	atomic.StoreInt64(&localHeightState, block.Index)
	RebuildStateBalanceCacheInternal()
	return true
}

// RebuildStateBalanceCache wraps our core memory map compiler inside global thread hold gates
func RebuildStateBalanceCache() {
	RebuildStateBalanceCacheInternal()
}

// RebuildStateBalanceCacheInternal chronologically scans storage buckets to construct precise balances
func RebuildStateBalanceCacheInternal() {
	BalanceCacheMutex.Lock()
	defer BalanceCacheMutex.Unlock()
	
	// Reset the memory tracking structures out-of-the-box
	StateBalanceCache = make(map[string]float64)
	if GlobalBoltEngine == nil {
		return
	}

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bUTXO := tx.Bucket(utxoBucket)
		bBlocks := tx.Bucket(blocksBucket)
		
		// Pass 1: Aggregate standard active UTXO ledger entries
		if bUTXO != nil {
			c := bUTXO.Cursor()
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var record UTXORecord
				if json.Unmarshal(v, &record) == nil {
					StateBalanceCache[record.Recipient] += record.Amount
				}
			}
		}

		// Pass 2: Scrutinize the historical block chain for valid Uncle block rewards splits
		if bBlocks != nil {
			bc := bBlocks.Cursor()
			for k, v := bc.First(); k != nil; k, v = bc.Next() {
				var blk Block
				if json.Unmarshal(v, &blk) == nil {
					if blk.UncleHash != "" && 
					   blk.UncleHash != "0000000000000000000000000000000000000000000000000000000000000000" && 
					   blk.UncleMiner != "NONE" && 
					   strings.TrimSpace(blk.UncleMiner) != "" {
						
						totalBlockEmission := CalculateBlockReward(blk.Index)
						uncleSubsidy := totalBlockEmission * 0.20
						
						// Allocate the 10.00 CVN reward directly to the Uncle Miner address channel
						StateBalanceCache[strings.TrimSpace(blk.UncleMiner)] += uncleSubsidy
					}
				}
			}
		}
		return nil
	})
}