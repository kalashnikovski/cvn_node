package main

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
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
		_, _ = tx.CreateBucketIfNotExists(blocksBucket)
		meta, _ := tx.CreateBucketIfNotExists(metaBucket)

		if meta.Get([]byte("height")) == nil {
			_ = meta.Put([]byte("height"), []byte("0"))
			fmt.Println("🌱 [GENESIS MATRIX INITIALIZED] Height synchronized to #0.")
		}
		return nil
	})

	RebuildStateBalanceCache()
}

func GetLatestBlock() Block {
	var latestBlock Block
	latestBlock.Index = 0
	latestBlock.Difficulty = 4
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

		keyStr := fmt.Sprintf("%016d", block.Index)
		_ = bBlocks.Put([]byte(keyStr), data)
		_ = bMeta.Put([]byte("height"), []byte(fmt.Sprintf("%d", block.Index)))

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