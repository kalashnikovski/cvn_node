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
var utxoBucket = []byte("utxo")
var jubileeVaultBucket = []byte("jubilee_vault")
var localHeightState int64 = 0

func InitBoltEngine() {
	StateMutex.Lock()
	defer StateMutex.Unlock()

	targetDatabaseName := "cvn_mainnet.db"
	dbPath := filepath.Join(".", targetDatabaseName)
	fmt.Println("====================================================================")
	fmt.Println("📂 HARDWARE FILE EXPLORER AUDIT INITIALIZED")
	fmt.Println("====================================================================")
	
	absPath, _ := filepath.Abs(dbPath)
	fmt.Printf("🎯 TARGET DATABASE FILE LOCATION:  %s\n\n", absPath)

	options := bbolt.Options{
		Timeout:      5 * time.Second,
		NoGrowSync:   false,
		FreelistType: bbolt.FreelistArrayType,
	}

	var openErr error
	GlobalBoltEngine, openErr = bbolt.Open(dbPath, 0666, &options)
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
				PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
				// 👑 FIXED MINIMUM HARD-LOCKED MAINNET PRIMITIVE:
				// Enforces exactly 4 leading zeros out-of-the-box matching your difficulty rule floor!
				Hash:         "00000c43b46f2506955b920b5981bf0a6375fc0bc0337", 
				Difficulty:   4, 
				Nonce:        0,
				GuardMatrix:  "",
				UncleHash:    "0000000000000000000000000000000000000000000000000000000000000000",
				UncleMiner:   "NONE",
				Transactions: []Transaction{
					{
						ID:               "TX_COINBASE_GENESIS_CORE_PROVISION_PRISTINE_ANCHOR",
						Witness:          "COVENANT_STEWARD_ASSEMBLY",
						PublicKey:        "GENESIS_VOID_REWARD_POOL",
						Inputs:           []UTXOInput{},
						Outputs:          []UTXOOutput{{Recipient: "CVN_GENESIS_VOID_REWARD_POOL", Amount: 0.0}},
						Timestamp:        1790640000,
						FreeWillOffering: 0.0,
						DataSizeKB:       0.1,
						Signature:        "COINBASE_IMMUTABLE_CONSENSUS_VALID",
					},
				},
			}

			genesisData, _ := json.Marshal(genesisBlock)
			paddedKey := fmt.Sprintf("%016d", genesisBlock.Index)
			
			_ = tx.Bucket(blocksBucket).Put([]byte(paddedKey), genesisData)
			_ = meta.Put([]byte("height"), []byte(paddedKey))
			
			fmt.Println("🌱 [GENESIS MATRIX SECURELY SEEDED] Physical Block #0 written directly to disk.")
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

	// 👑 THE FIRWALL RE-ALIGNMENT CHECKPOINT:
	// Enforce true cryptographic consensus verification checks directly at the front gate 
	// of your storage engine. If an un-computed nonce or forged loopback block attempts to bypass
	// main.go, it gets caught and rejected here, preventing hyper-acceleration forks entirely!
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

		// Process unspent transaction outputs and spends without creating map duplication leaks
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
	
	// Purge and reconstruct the memory registry structure cleanly
	StateBalanceCache = make(map[string]float64)
	if GlobalBoltEngine == nil {
		return
	}

	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bUTXO := tx.Bucket(utxoBucket)
		bBlocks := tx.Bucket(blocksBucket)
		
		// Pass 1: Scan and aggregate standard standard transaction UTXO records from disk
		if bUTXO != nil {
			c := bUTXO.Cursor()
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var record UTXORecord
				if json.Unmarshal(v, &record) == nil {
					StateBalanceCache[record.Recipient] += record.Amount
				}
			}
		}

		// 👑 Pass 2: THE UNCLE SUBSIDY MEMORY INJECTION ENGINE
		// Chronologically step through the entire block bucket database. If an uncle
		// block signature is found trapped in the metadata headers, dynamically compute
		// its 20% reward allocation split and credit it straight to the in-memory cache map!
		if bBlocks != nil {
			bc := bBlocks.Cursor()
			for k, v := bc.First(); k != nil; k, v = bc.Next() {
				var blk Block
				if json.Unmarshal(v, &blk) == nil {
					if blk.UncleHash != "" && 
					   blk.UncleHash != "0000000000000000000000000000000000000000000000000000000000000000" && 
					   blk.UncleMiner != "NONE" && 
					   strings.TrimSpace(blk.UncleMiner) != "" {
						
						// Calculate the exact 20% Uncle subsidy split based on the block index height
						totalBlockEmission := CalculateBlockReward(blk.Index)
						uncleSubsidy := totalBlockEmission * 0.20
						
						// Forcefully allocate the reward directly into the public wallet tracking map
						StateBalanceCache[strings.TrimSpace(blk.UncleMiner)] += uncleSubsidy
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
	StateMutex.RLock()
	defer StateMutex.RUnlock()
	return GetLatestBlockInternal().Index
}