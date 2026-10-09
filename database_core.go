package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync/atomic" 
	"time"          // ✅ FIXED: Added to handle Genesis block Unix timestamp tracking

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
		b, err := tx.CreateBucketIfNotExists(blocksBucket)
		if err != nil { return err }

		// ✅ AUTOMATIC GENESIS SEED: If the database is brand new, programmatically inject Block #0
		c := b.Cursor()
		k, _ := c.First()
		if k == nil {
			fmt.Println("🌱 SEEDING GENESIS ARCHITECTURE: Injecting primary block parameters into BoltDB...")
			
			genesisTx := Transaction{
				ID: "TX_GENESIS_INITIAL_POOL",
				Outputs: []UTXOOutput{
					{Recipient: "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337", Amount: 100000.00},
				},
				Witness: "GENESIS_VOID_REWARD_POOL",
			}

			genesisBlock := Block{
				Index:        0,
				Timestamp:    time.Now().Unix(),
				Transactions: []Transaction{genesisTx},
				PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
				Hash:         "85632def04401fccf7cbccd78b9ceb4d64c87a9195996921209dd653726b5ebd",
				Difficulty:   4,
			}

			data, _ := json.Marshal(genesisBlock)
			_ = b.Put([]byte("0"), data)
		}
		return nil
	})

	// Pre-load cache tracker sequence indices cleanly from disk
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