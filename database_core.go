package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
)

// ====================================================================
// 🛰️ GLOBAL CORE DATABASE ENGINE INTERFACE VARIANT FIELDS
// ====================================================================
var GlobalBoltEngine *bbolt.DB
var blocksBucket = []byte("blocks")
var metaBucket = []byte("metadata")
var localHeightState int64 = 0

// InitBoltEngine forces the automatic generation of missing ledger tracking structures on disk
func InitBoltEngine() {
	fmt.Println("====================================================================")
	fmt.Println("📂 HARDWARE FILE EXPLORER AUDIT INITIALIZED")
	fmt.Println("====================================================================")

	if BlockchainFile == "" {
		BlockchainFile = "cvn_mainnet.db"
	}

	workingDir, _ := os.Getwd()
	fmt.Printf("🔍 LOCAL SYSTEM WORKING DIRECTORY: %s\n", workingDir)

	absPath, _ := filepath.Abs(BlockchainFile)
	fmt.Printf("🎯 TARGET DATABASE FILE LOCATION:  %s\n", absPath)

	fileInfo, err := os.Stat(absPath)
	if err != nil {
		fmt.Println("⚠️  FILE NOTICE: cvn_mainnet.db missing. A fresh database will be generated.")
	} else {
		fmt.Printf("📊 PHYSICAL LEDGER FILE WEIGHT:   %d bytes (%.2f MB)\n", fileInfo.Size(), float64(fileInfo.Size())/(1024.0*1024.0))
	}
	fmt.Println("--------------------------------------------------------------------")

	options := &bbolt.Options{Timeout: 2 * time.Second}

	var openErr error
	GlobalBoltEngine, openErr = bbolt.Open(BlockchainFile, 0600, options)
	if openErr != nil {
		log.Fatalf("🚨 DATABASE ENGINE MOUNT FAILURE: Close competing node binaries and retry.\nError details: %v", openErr)
	}

	err = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(blocksBucket)
		if err != nil {
			return fmt.Errorf("failed to build blocks bucket: %v", err)
		}

		meta, err := tx.CreateBucketIfNotExists(metaBucket)
		if err != nil {
			return fmt.Errorf("failed to build metadata bucket: %v", err)
		}

		if meta.Get([]byte("height")) == nil {
			err = meta.Put([]byte("height"), []byte("0"))
			if err != nil {
				return err
			}
			fmt.Println("🌱 [GENESIS MATRIX INITIALIZED] Missing buckets built. Global state reference synchronized to height #0.")
		}
		return nil
	})

	if err != nil {
		log.Fatalf("🚨 CRITICAL LEDGER STRUCTURAL INITIALIZATION FAULT: %v", err)
	}

	tipBlock := GetLatestBlock()
	atomic.StoreInt64(&localHeightState, tipBlock.Index)

	fmt.Printf("⛓️ Ledger Registry Securely Mounted: %s (Starting Height: #%d)\n", BlockchainFile, atomic.LoadInt64(&localHeightState))
}

// GetLatestBlock fetches the highest valid sequential block payload using metadata index loops
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
			if v == nil {
				hIdx, _ := bBlocks.Cursor().Seek(heightBytes)
				if hIdx != nil {
					v = bBlocks.Get(hIdx)
				}
			}

			if v != nil {
				var bData Block
				if json.Unmarshal(v, &bData) == nil {
					latestBlock = bData
					atomic.StoreInt64(&localHeightState, bData.Index)
					return nil
				}
			}
		}

		c := bBlocks.Cursor()
		var internalMaxHeight int64 = -1
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var bData Block
			if json.Unmarshal(v, &bData) == nil {
				if bData.Index > internalMaxHeight {
					internalMaxHeight = bData.Index
					latestBlock = bData
				}
			}
		}

		if internalMaxHeight >= 0 {
			atomic.StoreInt64(&localHeightState, internalMaxHeight)
		}
		return nil
	})

	return latestBlock
}

// SaveBlockToStorage writes a validated consensus block permanently into the local BoltDB sectors
func SaveBlockToStorage(block Block) bool {
	if GlobalBoltEngine == nil {
		return false
	}

	err := GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		bMeta := tx.Bucket(metaBucket)
		if bBlocks == nil || bMeta == nil {
			return fmt.Errorf("target storage bucket arrays missing from database schemas")
		}

		data, err := json.Marshal(block)
		if err != nil {
			return err
		}

		keyStr := fmt.Sprintf("%016d", block.Index)
		keyBytes := []byte(keyStr)
		
		err = bBlocks.Put(keyBytes, data)
		if err != nil {
			return err
		}

		err = bMeta.Put([]byte("height"), []byte(fmt.Sprintf("%d", block.Index)))
		if err != nil {
			return err
		}

		BalanceCacheMutex.Lock()
		for _, txPayload := range block.Transactions {
			for _, out := range txPayload.Outputs {
				StateBalanceCache[out.Recipient] += out.Amount
			}
		}
		StateBalanceCache[CustomMinerAddress] += 50.0
		BalanceCacheMutex.Unlock()

		return nil
	})

	if err != nil {
		log.Printf("🚨 BoltDB write error on block #%d: %v\n", block.Index, err)
		return false
	}

	atomic.StoreInt64(&localHeightState, block.Index)
	return true
}

// GetBlockByHeightFromDB fetches block records using the standardized 16-character padded string key format
func GetBlockByHeightFromDB(height int64) Block {
	var block Block
	
	if GlobalBoltEngine == nil {
		return block
	}
	
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		if bBlocks == nil {
			return nil
		}
		
		// Enforces strict 16-character string padding to fix historical lookup mismatches
		paddedKey := fmt.Sprintf("%016d", height)
		blockData := bBlocks.Get([]byte(paddedKey))
		if blockData != nil {
			_ = json.Unmarshal(blockData, &block)
		}
		return nil
	})
	return block
}

// GetAddressBalanceFromLedger queries cached balance values inside thread-safe configurations
func GetAddressBalanceFromLedger(address string) int64 {
	BalanceCacheMutex.RLock()
	defer BalanceCacheMutex.RUnlock()
	return int64(StateBalanceCache[address])
}

// GetAddressBlockCountFromLedger returns approximate block contributions natively
func GetAddressBlockCountFromLedger(address string) int64 {
	latest := GetLatestBlock()
	if address == CustomMinerAddress {
		return latest.Index
	}
	return 0
}