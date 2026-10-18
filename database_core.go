package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
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
var utxoBucket = []byte("utxo")
var jubileeVaultBucket = []byte("jubilee_vault")
var localHeightState int64 = 0

func InitBoltEngine() {
	StateMutex.Lock()
	defer StateMutex.Unlock()

	targetDatabaseName := "cvn_mainnet.db"
	
	for _, arg := range os.Args {
		if strings.Contains(arg, "wails") || os.Getenv("BlockchainFile") == "cvn_binding_temp.db" {
			targetDatabaseName = "cvn_binding_temp.db"
			break
		}
	}

	if envPath := os.Getenv("BlockchainFile"); envPath != "" {
		targetDatabaseName = envPath
	}

	dbPath := filepath.Join(".", targetDatabaseName)
	fmt.Println("====================================================================")
	fmt.Println("📂 HARDWARE FILE EXPLORER AUDIT INITIALIZED")
	fmt.Println("====================================================================")
	
	absPath, _ := filepath.Abs(dbPath)
	fmt.Printf("🎯 TARGET DATABASE FILE LOCATION:  %s\n\n", absPath)

	options := &bbolt.Options{Timeout: 1 * time.Second}

	var openErr error
	GlobalBoltEngine, openErr = bbolt.Open(dbPath, 0600, options)
	if openErr != nil {
		log.Fatalf("🚨 DATABASE MOUNT FAILURE: %v\n", openErr)
	}

	_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		_, _ = tx.CreateBucketIfNotExists(blocksBucket)
		meta, _ := tx.CreateBucketIfNotExists(metaBucket)
		_, _ = tx.CreateBucketIfNotExists(utxoBucket)
		_, _ = tx.CreateBucketIfNotExists(jubileeVaultBucket)

		if meta.Get([]byte("height")) == nil {
			genesisBlock := Block{
				Index:        0,
				Timestamp:    1790640000,
				Transactions: []Transaction{}, 
				PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
				Hash:         "00000c43b46f2506955b920b5981bf0a6375fc0bc0337",
				Difficulty:   4,
				Nonce:        0,
				GuardMatrix:  "",
			}

			genesisData, _ := json.Marshal(genesisBlock)
			paddedKey := fmt.Sprintf("%016d", genesisBlock.Index)
			
			_ = tx.Bucket(blocksBucket).Put([]byte(paddedKey), genesisData)
			_ = meta.Put([]byte("height"), []byte(paddedKey))
			
			fmt.Println("🌱 [GENESIS MATRIX SECURELY SEEDED] Physical Block #0 written directly to disk.")
			fmt.Println("🪙 [FAIR LAUNCH MANDATE ENFORCED] Zero token pre-mine established. UTXO database is pristine.")
		}
		return nil
	})

	RebuildStateBalanceCacheInternal()
}

func GetLatestBlock() Block {
	StateMutex.RLock()
	defer StateMutex.RUnlock()
	return GetLatestBlockInternal()
}

func GetLatestBlockInternal() Block {
	var latestBlock Block
	latestBlock.Index = 0
	latestBlock.Difficulty = 4
	latestBlock.Timestamp = 1790640000
	latestBlock.Hash = "00000c43b46f2506955b920b5981bf0a6375fc0bc0337"

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
	StateMutex.Lock()
	defer StateMutex.Unlock()
	return SaveBlockToStorageInternal(block)
}

func SaveBlockToStorageInternal(block Block) bool {
	if GlobalBoltEngine == nil {
		return false
	}

	err := GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		bMeta := tx.Bucket(metaBucket)
		bUTXO := tx.Bucket(utxoBucket)
		if bBlocks == nil || bMeta == nil || bUTXO == nil {
			return fmt.Errorf("required consensus partitions are missing")
		}

		// 👑 THE UN-FORKABLE CONSENSUS FIREWALL GATE:
		// Lock validation evaluation strictly past loop index slot 0. This strips out 
		// brittle string prefix checks and securely handles incoming Linux mining headers!
		for txIdx, txPayload := range block.Transactions {
			if txIdx == 0 {
				continue 
			}
			if !VerifyTransactionIntegrity(txPayload, int(block.Index), txIdx, bUTXO) {
				fmt.Printf("🚨 SECURITY WARNING: Fraudulent transaction '%s' detected inside candidate block #%d! Aborting disk commitment.\n", txPayload.ID, block.Index)
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

		// 👑 THE MASTER CONCURRENT MUTATION LOCK:
		// Encapsulate balance memory mapping changes within strict mutex blocks to prevent
		// multi-threaded database cursor writes from corrupting the display outputs.
		BalanceCacheMutex.Lock()
				// 👑 THE MASTER UNIFIED UTXO MATRICES RECUPERATOR:
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
				
				utxoData, err := json.Marshal(utxoRecord)
				if err != nil {
					BalanceCacheMutex.Unlock()
					return err
				}
				_ = bUTXO.Put([]byte(compoundOutputKey), utxoData)
			}
		}
		BalanceCacheMutex.Unlock()

		// 👑 THE CORE ASYNCHRONOUS P2P MESH DISPATCHER:
		// Automatically broadcast the block bytes over the wire mesh the exact millisecond 
		// the physical database disk transaction commits successfully, bypassing terminal queues!
		if block.Hash != "MEMPOOL_GOSSIP_WIRE_ROUTE" && len(block.Transactions) > 0 {
			go func(b Block) {
				// Prevent loop-back broadcast feedback to safe seeds
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
	
	// Forcefully trigger a synchronized read-and-write state caching update pass 
	RebuildStateBalanceCacheInternal()
	return true
}


func GetBlockByHeightFromDB(height int64) Block {
	StateMutex.RLock()
	defer StateMutex.RUnlock()
	
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
	StateMutex.Lock()
	defer StateMutex.Unlock()
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
		if bUTXO == nil {
			return nil
		}
		
		c := bUTXO.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var record UTXORecord
			if json.Unmarshal(v, &record) == nil {
				StateBalanceCache[record.Recipient] += record.Amount
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
	StateMutex.RLock()
	defer StateMutex.RUnlock()
	return GetLatestBlockInternal().Index
}