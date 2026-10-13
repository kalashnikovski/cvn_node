package main

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
)

var GlobalBoltEngine *bbolt.DB
var blocksBucket = []byte("blocks")
var metaBucket = []byte("metadata")
var localHeightState int64 = 0

func InitBoltEngine() {
	fmt.Println("====================================================================")
	fmt.Println("📂 HARDWARE FILE EXPLORER AUDIT INITIALIZED")
	fmt.Println("====================================================================")

	if BlockchainFile == "" {
		BlockchainFile = "cvn_mainnet.db"
	}

	absPath, _ := filepath.Abs(BlockchainFile)
	fmt.Printf("🎯 TARGET DATABASE FILE LOCATION:  %s\n", absPath)

	options := &bbolt.Options{Timeout: 2 * time.Second}

	var openErr error
	GlobalBoltEngine, openErr = bbolt.Open(BlockchainFile, 0600, options)
	if openErr != nil {
		log.Fatalf("🚨 DATABASE MOUNT FAILURE: %v", openErr)
	}

	_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		bBlocks, _ := tx.CreateBucketIfNotExists(blocksBucket)
		meta, _ := tx.CreateBucketIfNotExists(metaBucket)

		// Seed a physical Block #0 if the database is pristine
		if meta.Get([]byte("height")) == nil {
			genesisBlock := Block{
				Index:     0,
				Timestamp: 1790640000,
				Transactions: []Transaction{
					{
						ID:        "TX_GENESIS_INITIAL_POOL",
						Witness:   "COVENANT_STEWARD_ASSEMBLY",
						PublicKey: "GENESIS_VOID_REWARD_POOL",
						Outputs: []UTXOOutput{
							{Recipient: "CVN_c43b46f2506955b920b5981bf0a6375fc0bc0337", Amount: 2100000000.0},
						},
					},
				},
				PrevHash:   "0000000000000000000000000000000000000000000000000000000000000000",
				Hash:       "85632def04401fccf7cbccd78b9ceb4d64c87a9195996921209dd653726b5ebd",
				Difficulty: 4,
				Nonce:      0,
			}

			genesisData, _ := json.Marshal(genesisBlock)
			paddedKey := fmt.Sprintf("%016d", genesisBlock.Index)
			
			_ = bBlocks.Put([]byte(paddedKey), genesisData)
			
			// Write metadata height keys using strict 16-character padding alignment!
			_ = meta.Put([]byte("height"), []byte(paddedKey))
			
			fmt.Println("🌱 [GENESIS MATRIX SECURELY SEEDED] Physical Block #0 written directly to disk.")
		}
		return nil
	})

	RebuildStateBalanceCache()
}

func GetLatestBlock() Block {
	var latestBlock Block
	latestBlock.Index = 0
	latestBlock.Difficulty = 4
	latestBlock.Timestamp = 1790640000
	latestBlock.Hash = "85632def04401fccf7cbccd78b9ceb4d64c87a9195996921209dd653726b5ebd"

	if GlobalBoltEngine == nil {
		return latestBlock
	}

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bMeta := tx.Bucket(metaBucket)
		bBlocks := tx.Bucket(blocksBucket)
		if bMeta == nil || bBlocks == nil {
			return nil
		}

		heightBytes := bMeta.Get([]byte("height"))
		if heightBytes != nil {
			v := bBlocks.Get(heightBytes)
			if v == nil {
				hStr := string(heightBytes)
				hIdx, err := strconv.ParseInt(hStr, 10, 64)
				if err == nil {
					paddedKey := fmt.Sprintf("%016d", hIdx)
					v = bBlocks.Get([]byte(paddedKey))
				}
			}

			if v != nil {
				var bData Block
				if json.Unmarshal(v, &bData) == nil {
					latestBlock = bData
					atomic.StoreInt64(&localHeightState, bData.Index)
				}
			}
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
		bBlocks := tx.Bucket(blocksBucket)
		bMeta := tx.Bucket(metaBucket)
		if bBlocks == nil || bMeta == nil {
			return fmt.Errorf("buckets missing")
		}

		data, err := json.Marshal(block)
		if err != nil {
			return err
		}

		paddedKey := fmt.Sprintf("%016d", block.Index)
		_ = bBlocks.Put([]byte(paddedKey), data)
		
		_ = bMeta.Put([]byte("height"), []byte(paddedKey))

		BalanceCacheMutex.Lock()
		for _, txPayload := range block.Transactions {
			if !strings.HasPrefix(txPayload.ID, "TX_COINBASE_") && txPayload.Witness != "" {
				var totalOutput float64 = 0.0
				for _, out := range txPayload.Outputs {
					totalOutput += out.Amount
				}
				StateBalanceCache[txPayload.Witness] -= (totalOutput + txPayload.FreeWillOffering)
			}
			for _, out := range txPayload.Outputs {
				StateBalanceCache[out.Recipient] += out.Amount
			}
		}
		BalanceCacheMutex.Unlock()

		return nil
	})

	if err != nil {
		return false
	}
	atomic.StoreInt64(&localHeightState, block.Index)
	return true
}

func GetBlockByHeightFromDB(height int64) Block {
	var block Block
	if GlobalBoltEngine == nil {
		return block
	}
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		if bBlocks != nil {
			paddedKey := fmt.Sprintf("%016d", height)
			blockData := bBlocks.Get([]byte(paddedKey))
			if blockData != nil {
				_ = json.Unmarshal(blockData, &block)
			}
		}
		return nil
	})
	return block
}

func RebuildStateBalanceCache() {
	BalanceCacheMutex.Lock()
	defer BalanceCacheMutex.Unlock()
	
	StateBalanceCache = make(map[string]float64)
	if GlobalBoltEngine == nil {
		return
	}

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		if bBlocks == nil {
			return nil
		}
		c := bBlocks.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var block Block
			if json.Unmarshal(v, &block) == nil {
				for _, txPayload := range block.Transactions {
					if !strings.HasPrefix(txPayload.ID, "TX_COINBASE_") && txPayload.Witness != "" {
						var totalOutput float64 = 0.0
						for _, out := range txPayload.Outputs {
							totalOutput += out.Amount
						}
						StateBalanceCache[txPayload.Witness] -= (totalOutput + txPayload.FreeWillOffering)
					}
					for _, out := range txPayload.Outputs {
						StateBalanceCache[out.Recipient] += out.Amount
					}
				}
			}
		}
		return nil
	})
}

func GetAddressBalanceFromLedger(address string) int64 {
	BalanceCacheMutex.RLock()
	defer BalanceCacheMutex.RUnlock()
	return int64(StateBalanceCache[address])
}

func GetAddressBlockCountFromLedger(address string) int64 {
	return GetLatestBlock().Index
}