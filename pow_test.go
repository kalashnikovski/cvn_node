package main

import (
	"testing"
)

// startRandomXChain opens a test chain that really mines with RandomX, at a
// difficulty low enough that each block takes a handful of hashes. The key
// changes every four blocks and trails the tip by two, so a short chain
// crosses several key changes.
func startRandomXChain(t *testing.T, schedule []PowEra) *int64 {
	t.Helper()
	startTestChain(t, "devnet")
	Params.PowSchedule = schedule
	Params.HeartbeatInitialDifficulty = 4
	Params.KeyEpochBlocks = 4
	Params.KeyLagBlocks = 2
	timestamp := Params.GenesisTimestamp + 100
	return &timestamp
}

func TestKeyHeightRule(t *testing.T) {
	onNetwork(t, "devnet")
	cases := map[int64]int64{
		1: 0, 50: 0, 101: 0, // the earliest blocks use the genesis block
		102: 0, 2020: 0, 2021: 1920, // first change: block 1920 becomes the key once it is 100 deep
		2022: 1920, 3940: 1920, 3941: 3840, 10000: 9600,
	}
	for height, want := range cases {
		if got := powKeyHeight(height); got != want {
			t.Errorf("key for block %d is block %d, want %d", height, got, want)
		}
	}
	// The key block is always further back than the reorganisation limit, so
	// it can never be replaced once it is in use.
	for height := int64(102); height < 6000; height++ {
		if height-powKeyHeight(height) <= Params.Economics.ReorgLimitBlocks {
			t.Fatalf("key for block %d is only %d blocks back", height, height-powKeyHeight(height))
		}
	}
}

func TestMinimumDifficultyByEra(t *testing.T) {
	onNetwork(t, "devnet")
	Params.PowSchedule = []PowEra{
		{FromHeight: 1, Algorithm: PowSHA256},
		{FromHeight: 100, Algorithm: PowRandomX, MinDifficulty: 5000, StartDifficulty: 20000},
	}
	if powAlgorithmAt(99) != PowSHA256 || powAlgorithmAt(100) != PowRandomX {
		t.Error("the algorithm must change exactly at the scheduled height")
	}
	if minDifficultyAt(99) != HeartbeatMinDifficulty || minDifficultyAt(100) != 5000 {
		t.Errorf("minimum difficulty: %d before the change, %d after", minDifficultyAt(99), minDifficultyAt(100))
	}
	if powEraStartingAt(100) == nil || powEraStartingAt(99) != nil || powEraStartingAt(101) != nil {
		t.Error("only block 100 starts a new era")
	}
}

func TestChainMinedWithRandomX(t *testing.T) {
	timestamp := startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 4}})

	keysSeen := make(map[int64]bool)
	for height := int64(1); height <= 12; height++ {
		*timestamp += 45
		block := extendTestChain(t, *timestamp, "CVN_miner", nil)
		keysSeen[powKeyHeight(block.Index)] = true

		// The identifier is still a plain SHA-256 hash of the header...
		if block.Hash != CalculateHash(block) {
			t.Fatalf("block #%d: identifier is not the header hash", block.Index)
		}
		// ...and the work is the RandomX hash of the block's mining blob.
		key, ok := powKeyFor(block.Index)
		if !ok {
			t.Fatalf("block #%d: no key", block.Index)
		}
		blob, _ := powBlob(powCommitment(block), uint32(block.Nonce))
		work, ok := randomXVerify(key, blob)
		if !ok || work > HeartbeatTargetHex(block.Difficulty) {
			t.Fatalf("block #%d was stored without a valid RandomX proof", block.Index)
		}
	}
	if len(keysSeen) < 3 {
		t.Errorf("the chain should have crossed at least two key changes; keys used: %v", keysSeen)
	}
	if balanceOf("CVN_miner") != cvn("600") {
		t.Errorf("after 12 RandomX blocks the miner has %s, want 600", FormatUnits(balanceOf("CVN_miner")))
	}
}

func TestBlockWithoutRandomXProofIsRefused(t *testing.T) {
	timestamp := startRandomXChain(t, []PowEra{{FromHeight: 1, Algorithm: PowRandomX, MinDifficulty: 64}})
	Params.HeartbeatInitialDifficulty = 64
	*timestamp += 45
	good := mineTestBlock(t, *timestamp, "CVN_miner", nil, 0)

	// Find a nonce whose identifier is valid but whose RandomX hash misses
	// the target: a block someone did not do the work for.
	lazy := good
	digest := BlockTransactionsDigest(lazy.Index, lazy.Transactions)
	for nonce := int64(0); ; nonce++ {
		lazy.Nonce = nonce
		lazy.Hash = CalculateHashWithTxDigest(lazy, digest)
		if !BlockProofValid(lazy, lazy.Difficulty) {
			break
		}
	}
	if ValidateBlockSignatures(lazy) {
		t.Error("a block without a valid RandomX proof passed validation")
	}
	if SaveBlockToStorageInternal(lazy) {
		t.Fatal("a block without a valid RandomX proof was stored")
	}
	if !candidateBlockProofValid(good) || candidateBlockProofValid(lazy) {
		t.Error("the branch-measuring check disagrees with validation about RandomX proofs")
	}
	// Under SHA-256 that same identifier might well have been enough: the
	// two algorithms must not be confused.
	if !ValidateBlockSignatures(good) || !SaveBlockToStorageInternal(good) {
		t.Fatal("the properly mined block was refused")
	}
}

// The mining algorithm can change at a chosen block, on a chain that is
// already running, without starting again.
func TestAlgorithmChangesAtAHeight(t *testing.T) {
	timestamp := startRandomXChain(t, []PowEra{
		{FromHeight: 1, Algorithm: PowSHA256},
		{FromHeight: 6, Algorithm: PowRandomX, MinDifficulty: 4, StartDifficulty: 8},
	})
	Params.HeartbeatInitialDifficulty = HeartbeatMinDifficulty

	var last Block
	for height := int64(1); height <= 9; height++ {
		*timestamp += 45
		last = extendTestChain(t, *timestamp, "CVN_miner", nil)
		switch {
		case height <= 5:
			if last.Difficulty < int(HeartbeatMinDifficulty) || !HashMeetsDifficulty(last.Hash, last.Index, last.Difficulty) {
				t.Fatalf("block #%d should be a SHA-256 block at SHA-256 difficulty (difficulty %d)", height, last.Difficulty)
			}
		case height == 6:
			if last.Difficulty != 8 {
				t.Fatalf("the first RandomX block should start from the era's own difficulty of 8, got %d", last.Difficulty)
			}
		default:
			if last.Difficulty < 4 || last.Difficulty > 100 {
				t.Fatalf("block #%d: difficulty %d did not carry on from the RandomX era's level", height, last.Difficulty)
			}
		}
	}
	if GetLatestBlockInternal().Index != 9 || balanceOf("CVN_miner") != cvn("450") {
		t.Fatalf("the chain did not carry on across the change: height %d, balance %s", GetLatestBlockInternal().Index, FormatUnits(balanceOf("CVN_miner")))
	}

	// After the change, SHA-256 work alone is no longer accepted: a block
	// whose identifier meets the target but whose RandomX hash does not.
	*timestamp += 45
	candidate := mineTestBlock(t, *timestamp, "CVN_miner", nil, 0)
	digest := BlockTransactionsDigest(candidate.Index, candidate.Transactions)
	for nonce := int64(0); ; nonce++ {
		candidate.Nonce = nonce
		candidate.Hash = CalculateHashWithTxDigest(candidate, digest)
		shaWouldPass := candidate.Hash <= HeartbeatTargetHex(candidate.Difficulty)
		if shaWouldPass && !BlockProofValid(candidate, candidate.Difficulty) {
			break
		}
	}
	if ValidateBlockSignatures(candidate) || SaveBlockToStorageInternal(candidate) {
		t.Error("after the change to RandomX, a block with only SHA-256 work was accepted")
	}
}

func TestHashrateDisplay(t *testing.T) {
	onNetwork(t, "testnet-1")
	if got := formatHashrate(0.47); got != "0.47 MH/s" {
		t.Errorf("testnet-1 shows %q, want 0.47 MH/s", got)
	}
	onNetwork(t, "devnet")
	if got := formatHashrate(0.0012); got != "1200 H/s" {
		t.Errorf("RandomX shows %q, want 1200 H/s", got)
	}
	if got := formatHashrate(0.0185); got != "18.50 kH/s" {
		t.Errorf("RandomX shows %q, want 18.50 kH/s", got)
	}
}

func TestSmallMachinesMineInLowMemoryMode(t *testing.T) {
	t.Setenv("CVN_RANDOMX_FULL", "")
	megabyte := uint64(1) << 20
	if enoughMemoryForFullMining(774*megabyte, true) {
		t.Error("a machine with 774 MB free was sent to full-memory mining")
	}
	if !enoughMemoryForFullMining(2300*megabyte, true) || !enoughMemoryForFullMining(20000*megabyte, true) {
		t.Error("a machine with enough memory was kept out of full-memory mining")
	}
	if !enoughMemoryForFullMining(0, false) {
		t.Error("when the free memory is not known the miner should still try")
	}
	t.Setenv("CVN_RANDOMX_FULL", "1")
	if !enoughMemoryForFullMining(774*megabyte, true) {
		t.Error("CVN_RANDOMX_FULL did not force the attempt")
	}
	// This machine can say how much memory it has free.
	if available, known := availableMemoryBytes(); !known || available < 64*megabyte {
		t.Errorf("free memory read as %d bytes (known: %v)", available, known)
	}
}
