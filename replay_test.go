package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// TestReplayRealChain re-validates a real chain from its first block, the way
// a new node does when it syncs, and checks that every block is accepted and
// that the balances at the end match the source database.
//
// It is the safety net for changes to how blocks, amounts or hashes are
// handled: if a change would make a node reject real history or compute a
// different balance, this fails.
//
// It needs a chain database, so it only runs when asked:
//
//	CVN_REPLAY_DB=path/to/cvn_mainnet.db go test -run TestReplayRealChain .
//
// The file is copied first; the original is never opened for writing, and it
// may belong to a running node. Set CVN_REPLAY_NETWORK for a network other
// than testnet-1.
func TestReplayRealChain(t *testing.T) {
	sourcePath := os.Getenv("CVN_REPLAY_DB")
	if sourcePath == "" {
		t.Skip("set CVN_REPLAY_DB to a chain database to run the replay")
	}
	network := os.Getenv("CVN_REPLAY_NETWORK")

	copyPath := filepath.Join(t.TempDir(), "source_copy.db")
	if err := copyFileForTest(sourcePath, copyPath); err != nil {
		t.Fatalf("copy %s: %v", sourcePath, err)
	}
	source, err := bbolt.Open(copyPath, 0600, &bbolt.Options{ReadOnly: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("open copy: %v", err)
	}
	defer source.Close()

	var blocks []Block
	sourceBalances := make(map[string]int64)
	err = source.View(func(tx *bbolt.Tx) error {
		bBlocks, bUTXO := tx.Bucket(blocksBucket), tx.Bucket(utxoBucket)
		if bBlocks == nil || bUTXO == nil {
			return fmt.Errorf("not a chain database")
		}
		cursor := bBlocks.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var block Block
			if err := json.Unmarshal(v, &block); err != nil {
				return fmt.Errorf("block %s unreadable: %w", k, err)
			}
			blocks = append(blocks, block)
		}
		coins := bUTXO.Cursor()
		for k, v := coins.First(); k != nil; k, v = coins.Next() {
			var record UTXORecord
			if json.Unmarshal(v, &record) == nil {
				sourceBalances[strings.TrimSpace(record.Recipient)] += record.Amount.Units()
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) < 2 {
		t.Fatalf("source holds %d block(s); nothing to replay", len(blocks))
	}

	startTestChain(t, network)
	genesis := GetBlockByHeightFromDB(0)
	if genesis.Hash != blocks[0].Hash {
		t.Fatalf("genesis mismatch: this build makes %s, the source has %s", genesis.Hash, blocks[0].Hash)
	}

	started := time.Now()
	for _, block := range blocks[1:] {
		if block.Index != GetLatestBlockInternal().Index+1 {
			t.Fatalf("source is not continuous at block #%d", block.Index)
		}
		if CalculateHash(block) != block.Hash {
			t.Fatalf("block #%d: this build computes hash %s, the source has %s", block.Index, CalculateHash(block), block.Hash)
		}
		if !ValidateBlockSignatures(block) {
			t.Fatalf("block #%d was rejected by validation", block.Index)
		}
		if !SaveBlockToStorageInternal(block) {
			t.Fatalf("block #%d was refused by storage", block.Index)
		}
	}
	tip := GetLatestBlockInternal()
	if tip.Hash != blocks[len(blocks)-1].Hash {
		t.Fatalf("tip after replay is %s, source tip is %s", tip.Hash, blocks[len(blocks)-1].Hash)
	}

	replayed := make(map[string]int64)
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		coins := tx.Bucket(utxoBucket).Cursor()
		for k, v := coins.First(); k != nil; k, v = coins.Next() {
			var record UTXORecord
			if json.Unmarshal(v, &record) == nil {
				replayed[strings.TrimSpace(record.Recipient)] += record.Amount.Units()
			}
		}
		return nil
	})
	var addresses []string
	for address := range sourceBalances {
		addresses = append(addresses, address)
	}
	for address := range replayed {
		if _, known := sourceBalances[address]; !known {
			addresses = append(addresses, address)
		}
	}
	sort.Strings(addresses)
	var total int64
	for _, address := range addresses {
		if replayed[address] != sourceBalances[address] {
			t.Errorf("balance of %s: replay gives %s, source has %s", address, FormatUnits(replayed[address]), FormatUnits(sourceBalances[address]))
		}
		total += replayed[address]
	}
	t.Logf("replayed %d blocks in %v; tip #%d; %d addresses; %s CVN in total; total work %s",
		len(blocks)-1, time.Since(started).Round(time.Millisecond), tip.Index, len(addresses), FormatUnits(total), TotalChainWork())
}

func copyFileForTest(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
