package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"go.etcd.io/bbolt"
)

// Block structural copy to parse the incoming byte blocks natively
type Block struct {
	Index int64 `json:"current_height"`
}

func main() {
	dbFile := "cvn_mainnet.db"
	fmt.Println("🛠️ Covenant Standard Ledger Metadata Alignment Tool Initiating...")

	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		log.Fatalf("🚨 CRITICAL ERROR: %s not found in current execution directory!", dbFile)
	}

	db, err := bbolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Fatalf("🚨 BoltDB Error opening database: %v", err)
	}
	defer db.Close()

	var calculatedTip int64 = 0

	// 1. Audit the blocks bucket sequentially to discover the absolute highest block index key
	err = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("blocks"))
		if b == nil {
			return fmt.Errorf("blocks bucket is missing completely")
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var blk Block
			if err := json.Unmarshal(v, &blk); err == nil {
				if blk.Index > calculatedTip {
					calculatedTip = blk.Index
				}
			}
		}
		return nil
	})

	if err != nil {
		log.Fatalf("🚨 Error auditing block bucket indices: %v", err)
	}

	fmt.Printf("📊 Physical Scan complete. True ledger index block depth found: #%d\n", calculatedTip)

	// 2. Forcefully write the metadata bucket height key string value to disk
	err = db.Update(func(tx *bbolt.Tx) error {
		// Initialize the metadata tracking bucket space if dropped by legacy loops
		meta, err := tx.CreateBucketIfNotExists([]byte("metadata"))
		if err != nil {
			return err
		}

		// Convert height numerical index integer to explicit byte strings matching consensus specs
		heightKey := []byte("height")
		heightVal := []byte(fmt.Sprintf("%d", calculatedTip))

		err = meta.Put(heightKey, heightVal)
		if err != nil {
			return err
		}

		fmt.Printf("✅ METADATA OVERRIDE SUCCESSFUL: Key 'height' is now reading string value: '%s'\n", string(heightVal))
		return nil
	})

	if err != nil {
		log.Fatalf("🚨 CRITICAL FAULT: Failed to inject metadata parameters to BoltDB: %v", err)
	}

	fmt.Println("✨ Maintenance complete. Safe to unchain production daemons.")
}