package main

import (
	"fmt"
	"math/big"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

// Helpers shared by the tests. Every test chain lives in a temporary database
// that is deleted afterwards; no test opens the real database or contacts the
// network.

// testCoinbaseCounter keeps every test reward transaction's ID unique.
var testCoinbaseCounter int64

// startTestChain opens an empty chain in a temporary database on the named
// network and restores the previous settings when the test ends.
func startTestChain(t *testing.T, network string) {
	t.Helper()
	savedParams := Params
	if err := SelectNetwork(network); err != nil {
		t.Fatalf("select network: %v", err)
	}
	BlockchainFile = filepath.Join(t.TempDir(), "chain.db")
	InitBoltEngine()
	t.Cleanup(func() {
		if GlobalBoltEngine != nil {
			GlobalBoltEngine.Close()
			GlobalBoltEngine = nil
		}
		MempoolMatrix.Lock()
		MempoolMatrix.Transactions = make(map[string]Transaction)
		MempoolMatrix.Unlock()
		Params = savedParams
		applyParams()
	})
}

// mineTestBlock builds a valid block on the current tip without saving it.
// rewardOverrideUnits, when above zero, replaces the correct reward (to
// build blocks that must be refused).
func mineTestBlock(t *testing.T, timestamp int64, miner string, transfers []Transaction, rewardOverrideUnits int64) Block {
	t.Helper()
	tip := GetLatestBlockInternal()
	index := tip.Index + 1
	difficulty := 2
	if index > 10 {
		difficulty = CalculateNextDifficulty(tip, timestamp)
	}
	var feeUnits int64
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		for _, tx := range transfers {
			fee, _, err := checkTransferAgainstCoins(tx, bucketLookup(dbTx.Bucket(utxoBucket)), nil)
			if err != nil {
				t.Fatalf("test transfer is not valid: %v", err)
			}
			feeUnits += fee
		}
		return nil
	})
	reward := BlockRewardUnits(index) + feeUnits
	if rewardOverrideUnits > 0 {
		reward = rewardOverrideUnits
	}
	testCoinbaseCounter++
	coinbase := Transaction{
		Version: 1, ID: fmt.Sprintf("TX_COINBASE_H%d_%d", index, testCoinbaseCounter), Witness: miner,
		PublicKey: CoinbasePublicKeyMarker, Signature: CoinbaseSignatureMarker, Timestamp: timestamp,
		Inputs: []UTXOInput{}, Outputs: []UTXOOutput{{Recipient: miner, Amount: UnitsAmount(reward)}},
	}
	block := Block{
		Version: 1, Index: index, Timestamp: timestamp, PrevHash: tip.Hash, Difficulty: difficulty,
		Transactions: append([]Transaction{coinbase}, transfers...),
		UncleHash:    "0000000000000000000000000000000000000000000000000000000000000000", UncleMiner: "NONE",
	}
	digest := BlockTransactionsDigest(index, block.Transactions)
	for nonce := int64(0); ; nonce++ {
		block.Nonce = nonce
		hash := CalculateHashWithTxDigest(block, digest)
		if HashMeetsDifficulty(hash, index, difficulty) {
			block.Hash = hash
			return block
		}
	}
}

// extendTestChain mines a block and adds it to the chain the way a node does.
func extendTestChain(t *testing.T, timestamp int64, miner string, transfers []Transaction) Block {
	t.Helper()
	block := mineTestBlock(t, timestamp, miner, transfers, 0)
	if !ValidateBlockSignatures(block) {
		t.Fatalf("block #%d failed validation", block.Index)
	}
	if !SaveBlockToStorageInternal(block) {
		t.Fatalf("block #%d was not stored", block.Index)
	}
	return block
}

// buildSharedHistory mines blocks 1 to 12 and returns the last one and its
// timestamp. Block 11 is the first heartbeat block on a chain whose heartbeat
// starts at 11; the long gap before it brings its difficulty down to the
// minimum so tests run quickly.
func buildSharedHistory(t *testing.T, earlyMiner, lateMiner string) (Block, int64) {
	t.Helper()
	timestamp := Params.GenesisTimestamp + 100
	for i := 1; i <= 10; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, earlyMiner, nil)
	}
	timestamp += HeartbeatStallAfterSeconds + HeartbeatStallStepSeconds*12
	extendTestChain(t, timestamp, lateMiner, nil)
	timestamp += 45
	return extendTestChain(t, timestamp, lateMiner, nil), timestamp
}

func sumBlockWork(blocks []Block) *big.Int {
	total := new(big.Int)
	for _, block := range blocks {
		total.Add(total, BlockWork(block))
	}
	return total
}

// serveBlocks returns a batch fetcher that hands out the given blocks, the
// way a peer's block-download endpoint would.
func serveBlocks(blocks []Block) func(int64) []Block {
	return func(start int64) []Block {
		var batch []Block
		for _, block := range blocks {
			if block.Index >= start {
				batch = append(batch, block)
			}
		}
		return batch
	}
}

// cvn turns a figure written in CVN, such as "120.25", into units.
func cvn(text string) int64 {
	units, err := ParseCVN(text)
	if err != nil {
		panic(err)
	}
	return units
}

// balanceOf returns an address's balance in units after a full recount from
// the coins.
func balanceOf(address string) int64 {
	RebuildStateBalanceCacheInternal()
	BalanceCacheMutex.RLock()
	defer BalanceCacheMutex.RUnlock()
	return StateBalanceCache[address]
}
