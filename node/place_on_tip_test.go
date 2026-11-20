package node

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func addressForPlacingTests(t *testing.T) string {
	t.Helper()
	_, _, address, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(address)
}

// Every block added one at a time is checked and stored with the chain held
// still. These are the cases that matter: the block that fits, the block
// whose parent is no longer the newest, and the block that arrives while
// chains are being compared.
func TestABlockIsPlacedOnlyOnTheNewestBlock(t *testing.T) {
	startTestChain(t, "devnet")
	miner := addressForPlacingTests(t)
	date := Params.GenesisTimestamp + 45

	first := mineTestBlock(t, date, miner, nil, 0)
	if !placeOnTip(first) {
		t.Fatal("a valid block on the newest block was refused")
	}
	if GetLatestBlockInternal().Hash != first.Hash {
		t.Fatal("the block was accepted and is not the newest block")
	}
	// The same block again has nowhere to go: its parent is one back.
	if placeOnTip(first) {
		t.Error("a block was placed a second time")
	}

	// Two blocks made on the same parent, as two miners (or a miner and a
	// neighbour) make them: the first to arrive is placed, and the second,
	// which was checked against a newest block that has since changed, is
	// not stored on top of it.
	second := mineTestBlock(t, date+45, miner, nil, 0)
	rival := mineTestBlock(t, date+46, miner, nil, 0)
	if !placeOnTip(second) {
		t.Fatal("the first of two blocks on one parent was refused")
	}
	if placeOnTip(rival) {
		t.Error("a block was stored on a parent that is no longer the newest block")
	}
	if tip := GetLatestBlockInternal(); tip.Index != 2 || tip.Hash != second.Hash {
		t.Errorf("the newest block is #%d %.16s; it should be #2 %.16s", tip.Index, tip.Hash, second.Hash)
	}

	// While chains are being compared nothing is placed, and the node
	// remembers that a block came, so that it looks again afterwards.
	third := mineTestBlock(t, date+90, miner, nil, 0)
	atomic.StoreInt32(&blockMissedDuringPass, 0)
	atomic.StoreInt32(&IsSyncingActive, 1)
	placed := placeOnTip(third)
	atomic.StoreInt32(&IsSyncingActive, 0)
	if placed {
		t.Error("a block was placed while chains were being compared")
	}
	if atomic.LoadInt32(&blockMissedDuringPass) != 1 {
		t.Error("a block that came during a comparison was not remembered")
	}
	atomic.StoreInt32(&blockMissedDuringPass, 0)
	if !placeOnTip(third) {
		t.Error("the block was refused once the comparison had ended")
	}
}

// Many blocks offered for the same place at the same moment: exactly one is
// stored, and what the chain holds afterwards adds up.
func TestOnlyOneOfManyBlocksOfferedAtOnceIsPlaced(t *testing.T) {
	startTestChain(t, "devnet")
	miner := addressForPlacingTests(t)
	date := Params.GenesisTimestamp + 45

	const offered = 16
	blocks := make([]Block, offered)
	for i := range blocks {
		blocks[i] = mineTestBlock(t, date+int64(i), miner, nil, 0)
	}
	var placed int32
	var together sync.WaitGroup
	begin := make(chan struct{})
	for i := range blocks {
		together.Add(1)
		go func(block Block) {
			defer together.Done()
			<-begin
			if placeOnTip(block) {
				atomic.AddInt32(&placed, 1)
			}
		}(blocks[i])
	}
	close(begin)
	together.Wait()
	if placed != 1 {
		t.Fatalf("%d of %d blocks offered for one place were stored; exactly one should be", placed, offered)
	}
	if height := GetLatestBlockInternal().Index; height != 1 {
		t.Errorf("the chain is at block #%d after one block was placed", height)
	}
	RebuildStateBalanceCacheInternal()
	BalanceCacheMutex.RLock()
	held := StateBalanceCache[miner]
	BalanceCacheMutex.RUnlock()
	if held != cvn("50") {
		t.Errorf("the miner is counted %s CVN after one block; it should be 50", FormatUnits(held))
	}
}
