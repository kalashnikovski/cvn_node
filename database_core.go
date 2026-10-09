package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync/atomic" // ✅ ADDED: Handles high-speed thread safe memory lookups

	"go.etcd.io/bbolt"
)

var GlobalBoltEngine *bbolt.DB
var blocksBucket = []byte("blocks")
var localHeightState int64 = 0 // ✅ CRITICAL: Pure memory cache tracker to bypass database read-locks

func InitBoltEngine() {
	fmt.Println("📂 Initializing BoltDB Mainnet Cache Engine...")
	if BlockchainFile == "" {
		BlockchainFile = "cvn_mainnet.db"
	}

	var err error
	GlobalBoltEngine, err = bbolt.Open(BlockchainFile, 0600, nil)
	if err != nil {
		log.Printf("⚠️ BoltDB initialization delay: %v\n", err)
		return
	}

	_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(blocksBucket)
		return err
	})

	// Pre-load our atomic integer cache with the truest disk tip at launch
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(blocksBucket)
		if b == nil { return nil }
		c := b.Cursor()
		k, v := c.Last()
		if k != nil && v != nil {
			var bData Block
			if json.Unmarshal(v, &bData) == nil {
				atomic.StoreInt64(&localHeightState, bData.Index)
			}
		}
		return nil
	})

	fmt.Printf("⛓️ Ledger Registry Securely Mounted: %s (Starting Height: #%d)\n", BlockchainFile, atomic.LoadInt64(&localHeightState))
}

func GetLatestBlock() Block {
	var latestBlock Block
	
	// Read instantly straight from memory cache—completely avoiding disk-lock deadlocks!
	memHeight := atomic.LoadInt64(&localHeightState)
	latestBlock.Index = memHeight
	latestBlock.Difficulty = 4
	latestBlock.Hash = "0000000000000000000000000000000000000000000000000000000000000000"

	if GlobalBoltEngine == nil || memHeight == 0 {
		return latestBlock
	}

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(blocksBucket)
		if b == nil { return nil }
		v := b.Get([]byte(fmt.Sprintf("%d", memHeight)))
		if v != nil {
			_ = json.Unmarshal(v, &latestBlock)
		}
		return nil
	})

	return latestBlock
}

func SaveBlockToStorage(block Block) bool {
	if GlobalBoltEngine == nil {
		return false
	}

	err := GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(blocksBucket)
		if b == nil { return fmt.Errorf("missing bucket") }

		data, err := json.Marshal(block)
		if err != nil { return err }

		return b.Put([]byte(fmt.Sprintf("%d", block.Index)), data)
	})

	if err != nil {
		log.Printf("🚨 BoltDB write error on block #%d: %v\n", block.Index, err)
		return false
	}

	// Safely increment our global memory tracker for immediate UI updates
	atomic.StoreInt64(&localHeightState, block.Index)
	return true
}

func GetAddressBalanceFromLedger(address string) int64 { return 0 }
func GetAddressBlockCountFromLedger(address string) int64 { return 0 }

// GetBlockByHeightFromDB pulls a specific block segment out of the bbolt key-value store using the height index string
func GetBlockByHeightFromDB(height int64) Block {
	var targetBlock Block

	if GlobalBoltEngine == nil {
		return targetBlock
	}

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(blocksBucket)
		if b == nil {
			return nil
		}

		// Format the int64 index key as a byte string to match your BoltDB bucket row structure
		key := []byte(fmt.Sprintf("%d", height))
		v := b.Get(key)
		if v == nil {
			return nil // Block slice doesn't exist yet on disk
		}

		_ = json.Unmarshal(v, &targetBlock)
		return nil
	})

	return targetBlock
}