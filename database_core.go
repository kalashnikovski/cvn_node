package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"             // ✅ Handles physical file metadata tracking
	"path/filepath"  // ✅ Handles absolute path calculations
	"sync/atomic"

	"go.etcd.io/bbolt"
)

var GlobalBoltEngine *bbolt.DB
var blocksBucket = []byte("blocks")
var localHeightState int64 = 0 

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

	var openErr error
	GlobalBoltEngine, openErr = bbolt.Open(BlockchainFile, 0600, nil)
	if openErr != nil {
		log.Printf("🚨 BoltDB initialization fault: %v\n", openErr)
		return
	}

	_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(blocksBucket)
		return err
	})

	// Pre-load our cache tracker index using our newly repaired, adaptive scanner
	tipBlock := GetLatestBlock()
	atomic.StoreInt64(&localHeightState, tipBlock.Index)

	fmt.Printf("⛓️ Ledger Registry Securely Mounted: %s (Starting Height: #%d)\n", BlockchainFile, atomic.LoadInt64(&localHeightState))
}

func GetLatestBlock() Block {
	var latestBlock Block
	
	// Default baseline safely
	latestBlock.Index = 0
	latestBlock.Difficulty = 4
	latestBlock.Hash = "85632def04401fccf7cbccd78b9ceb4d64c87a9195996921209dd653726b5ebd"

	if GlobalBoltEngine == nil {
		return latestBlock
	}

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(blocksBucket)
		if b == nil { return nil }
		
		c := b.Cursor()
		var internalMaxHeight int64 = -1

		// ✅ ADAPTIVE SCANNER ROAD: Iterates through keys to handle both padded and unpadded string structures safely
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var bData Block
			if json.Unmarshal(v, &bData) == nil {
				// Safely parse the true numerical index value
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

func SaveBlockToStorage(block Block) bool {
	if GlobalBoltEngine == nil {
		return false
	}

	err := GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(blocksBucket)
		if b == nil { return fmt.Errorf("missing bucket") }

		data, err := json.Marshal(block)
		if err != nil { return err }

		// Write using padded layout going forward to guarantee high-velocity lexicographical sorting
		key := []byte(fmt.Sprintf("%016d", block.Index))
		return b.Put(key, data)
	})

	if err != nil {
		log.Printf("🚨 BoltDB write error on block #%d: %v\n", block.Index, err)
		return false
	}

	atomic.StoreInt64(&localHeightState, block.Index)
	return true
}

func GetBlockByHeightFromDB(height int64) Block {
	var targetBlock Block
	if GlobalBoltEngine == nil { return targetBlock }

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(blocksBucket)
		if b == nil { return nil }
		
		// Dual compatibility lookup pass
		keyPadded := []byte(fmt.Sprintf("%016d", height))
		v := b.Get(keyPadded)
		if v == nil {
			keyUnpadded := []byte(fmt.Sprintf("%d", height))
			v = b.Get(keyUnpadded)
		}
		
		if v != nil {
			_ = json.Unmarshal(v, &targetBlock)
		}
		return nil
	})
	return targetBlock
}

func GetAddressBalanceFromLedger(address string) int64 { return 0 }
func GetAddressBlockCountFromLedger(address string) int64 { return 0 }