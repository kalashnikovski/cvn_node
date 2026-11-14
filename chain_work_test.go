package main

import (
	"math/big"
	"testing"

	"go.etcd.io/bbolt"
)

// competingBranches builds a shared history to block 12 and two branches on
// top of it, leaving the chain at block 12:
//
//	heavy: 8 blocks one second apart, so difficulty climbs; its third block
//	       carries a transfer with a fee
//	light: 9 blocks 45 seconds apart, so difficulty stays at the minimum
//
// light is longer; heavy holds more work.
type competingBranches struct {
	ancestor     Block
	heavy, light []Block
}

func buildCompetingBranches(t *testing.T) competingBranches {
	t.Helper()
	privateKey, publicKey, earlyMiner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	ancestor, timestamp := buildSharedHistory(t, earlyMiner, "CVN_minerA")

	heavyTime := timestamp
	for i := 0; i < 8; i++ {
		heavyTime++
		var transfers []Transaction
		if i == 2 {
			transfer, err := BuildSignedTransfer(privateKey, publicKey, "CVN_recipient", 120.25, 0.5, ListSpendableCoins(earlyMiner))
			if err != nil {
				t.Fatalf("build transfer: %v", err)
			}
			transfers = []Transaction{transfer}
		}
		extendTestChain(t, heavyTime, "CVN_minerA", transfers)
	}
	StateMutex.Lock()
	heavy := rollbackChainToHeightLocked(ancestor.Index)
	StateMutex.Unlock()

	lightTime := timestamp
	for i := 0; i < 9; i++ {
		lightTime += 45
		extendTestChain(t, lightTime, "CVN_minerB", nil)
	}
	StateMutex.Lock()
	light := rollbackChainToHeightLocked(ancestor.Index)
	StateMutex.Unlock()

	if len(heavy) != 8 || len(light) != 9 || heavy[0].Index != 13 || light[0].Index != 13 {
		t.Fatalf("branches not built as expected: %d and %d blocks", len(heavy), len(light))
	}
	if sumBlockWork(light).Cmp(sumBlockWork(heavy)) >= 0 {
		t.Fatalf("set-up: the longer branch (%s) must hold less work than the shorter (%s)", sumBlockWork(light), sumBlockWork(heavy))
	}
	if GetLatestBlockInternal().Hash != ancestor.Hash {
		t.Fatal("chain is not back at the shared block")
	}
	return competingBranches{ancestor: ancestor, heavy: heavy, light: light}
}

func putBranchInPlace(branch []Block, ancestor Block) {
	StateMutex.Lock()
	restoreChainLocked(ancestor.Index, branch)
	StateMutex.Unlock()
}

func workAboveAncestor(ancestor Block) *big.Int {
	tip := GetLatestBlockInternal()
	return new(big.Int).Sub(chainWorkThrough(tip.Index), chainWorkThrough(ancestor.Index))
}

func TestBlockWork(t *testing.T) {
	saved := HeartbeatActivationHeight
	HeartbeatActivationHeight = 100
	defer func() { HeartbeatActivationHeight = saved }()

	if got := BlockWork(Block{Index: 100, Difficulty: 90000000}); got.Int64() != 90000000 {
		t.Errorf("heartbeat block work = %s, want its difficulty", got)
	}
	if got := BlockWork(Block{Index: 50, Difficulty: 5}); got.Int64() != 16*16*16*16*16 {
		t.Errorf("five leading zeros = %s hashes, want 16^5", got)
	}
	if got := BlockWork(Block{Index: 3, Difficulty: 9}); got.Int64() != 256 {
		t.Errorf("the first ten blocks count as two leading zeros, got %s", got)
	}
}

func TestTotalWorkIsTheSumOfEveryBlock(t *testing.T) {
	startTestChain(t, "devnet")
	buildSharedHistory(t, "CVN_minerA", "CVN_minerA")
	tip := GetLatestBlockInternal()

	want := new(big.Int)
	for height := int64(0); height <= tip.Index; height++ {
		want.Add(want, BlockWork(GetBlockByHeightFromDB(height)))
	}
	if TotalChainWork().Cmp(want) != 0 {
		t.Fatalf("total work %s, want %s", TotalChainWork(), want)
	}

	// The records can be thrown away and rebuilt from the blocks alone.
	_ = GlobalBoltEngine.Update(func(dbTx *bbolt.Tx) error { return dbTx.DeleteBucket(chainWorkBucket) })
	if TotalChainWork().Cmp(want) != 0 {
		t.Errorf("without records, total work is %s, want %s", TotalChainWork(), want)
	}
	EnsureChainWorkIndex()
	var recorded *big.Int
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		recorded = readChainWork(dbTx, tip.Index, tip.Hash)
		return nil
	})
	if recorded == nil || recorded.Cmp(want) != 0 {
		t.Errorf("rebuilt record is %v, want %s", recorded, want)
	}
}

func TestLongerButLighterChainIsRefused(t *testing.T) {
	startTestChain(t, "devnet")
	b := buildCompetingBranches(t)
	putBranchInPlace(b.heavy, b.ancestor)

	peerTip := b.light[len(b.light)-1].Index
	if peerTip <= GetLatestBlockInternal().Index {
		t.Fatal("set-up: the offered chain must be longer than ours")
	}
	if _, wins := measureCandidateBranch(serveBlocks(b.light), b.ancestor.Index, b.ancestor.Hash, peerTip, workAboveAncestor(b.ancestor)); wins {
		t.Error("a longer chain with less work was preferred to ours")
	}
}

func TestShorterButHeavierChainIsAccepted(t *testing.T) {
	startTestChain(t, "devnet")
	b := buildCompetingBranches(t)
	putBranchInPlace(b.light, b.ancestor)

	peerTip := b.heavy[len(b.heavy)-1].Index
	if _, wins := measureCandidateBranch(serveBlocks(b.heavy), b.ancestor.Index, b.ancestor.Hash, peerTip, workAboveAncestor(b.ancestor)); !wins {
		t.Error("a shorter chain with more work was not preferred")
	}
}

func TestTamperedBranchesEarnNoWork(t *testing.T) {
	startTestChain(t, "devnet")
	b := buildCompetingBranches(t)
	putBranchInPlace(b.light, b.ancestor)
	peerTip := b.heavy[len(b.heavy)-1].Index
	toBeat := workAboveAncestor(b.ancestor)

	inflated := append([]Block{}, b.heavy...)
	inflated[0].Difficulty *= 1000 // claims far more work than its hash shows
	if work, wins := measureCandidateBranch(serveBlocks(inflated), b.ancestor.Index, b.ancestor.Hash, peerTip, toBeat); wins || work.Sign() != 0 {
		t.Errorf("a block claiming a difficulty its hash does not meet was counted (work %s)", work)
	}

	detached := append([]Block{}, b.heavy[1:]...) // first block missing: does not attach
	if work, wins := measureCandidateBranch(serveBlocks(detached), b.ancestor.Index, b.ancestor.Hash, peerTip, toBeat); wins || work.Sign() != 0 {
		t.Errorf("a branch that does not attach to the shared block was counted (work %s)", work)
	}

	edited := append([]Block{}, b.heavy...)
	edited[0].Timestamp++ // contents no longer match the hash
	if work, wins := measureCandidateBranch(serveBlocks(edited), b.ancestor.Index, b.ancestor.Hash, peerTip, toBeat); wins || work.Sign() != 0 {
		t.Errorf("a block whose contents do not match its hash was counted (work %s)", work)
	}
}

func TestSwitchingChainsMovesCoinsCorrectly(t *testing.T) {
	startTestChain(t, "devnet")
	b := buildCompetingBranches(t)

	putBranchInPlace(b.light, b.ancestor)
	if balanceOf("CVN_minerB") != 450 || balanceOf("CVN_recipient") != 0 || balanceOf("CVN_minerA") != 100 {
		t.Fatalf("on the light branch: B=%v recipient=%v A=%v", balanceOf("CVN_minerB"), balanceOf("CVN_recipient"), balanceOf("CVN_minerA"))
	}
	lightWork := TotalChainWork()

	// Switch the way a re-sync does: take our blocks off, apply the peer's.
	StateMutex.Lock()
	removed := rollbackChainToHeightLocked(b.ancestor.Index)
	for _, block := range b.heavy {
		if !ValidateBlockSignatures(block) || !SaveBlockToStorageInternal(block) {
			StateMutex.Unlock()
			t.Fatalf("block #%d of the heavier branch was refused", block.Index)
		}
	}
	StateMutex.Unlock()

	if len(removed) != len(b.light) {
		t.Errorf("%d blocks were taken off, want %d", len(removed), len(b.light))
	}
	if GetLatestBlockInternal().Hash != b.heavy[len(b.heavy)-1].Hash {
		t.Error("tip is not the heavier branch's tip")
	}
	if TotalChainWork().Cmp(lightWork) <= 0 {
		t.Error("total work did not rise after switching to the heavier branch")
	}
	// Miner A: blocks 11 and 12 plus eight more, plus the 0.5 fee.
	if balanceOf("CVN_minerA") != 500.5 || balanceOf("CVN_minerB") != 0 || balanceOf("CVN_recipient") != 120.25 {
		t.Errorf("after the switch: A=%v B=%v recipient=%v; want 500.5, 0, 120.25", balanceOf("CVN_minerA"), balanceOf("CVN_minerB"), balanceOf("CVN_recipient"))
	}
}

func TestFailedSwitchPutsOurBlocksBack(t *testing.T) {
	startTestChain(t, "devnet")
	b := buildCompetingBranches(t)
	putBranchInPlace(b.light, b.ancestor)
	tipBefore, workBefore := GetLatestBlockInternal(), TotalChainWork()

	StateMutex.Lock()
	removed := rollbackChainToHeightLocked(b.ancestor.Index)
	SaveBlockToStorageInternal(b.heavy[0])
	SaveBlockToStorageInternal(b.heavy[1])
	// The third block of the new branch pays its miner 60 CVN: invalid.
	bad := mineTestBlock(t, b.heavy[1].Timestamp+1, "CVN_minerA", nil, 60)
	stored := SaveBlockToStorageInternal(bad)
	worse := TotalChainWork().Cmp(workBefore) < 0
	restoreChainLocked(b.ancestor.Index, removed)
	StateMutex.Unlock()

	if stored {
		t.Fatal("a block paying itself 60 CVN was stored")
	}
	if !worse {
		t.Fatal("set-up: the half-applied branch should hold less work than what we gave up")
	}
	if GetLatestBlockInternal().Hash != tipBefore.Hash || TotalChainWork().Cmp(workBefore) != 0 {
		t.Error("our previous chain was not put back exactly")
	}
	if balanceOf("CVN_minerB") != 450 || balanceOf("CVN_minerA") != 100 || balanceOf("CVN_recipient") != 0 {
		t.Errorf("balances after putting our chain back: B=%v A=%v recipient=%v", balanceOf("CVN_minerB"), balanceOf("CVN_minerA"), balanceOf("CVN_recipient"))
	}
}

func TestPeerComparison(t *testing.T) {
	more, less := big.NewInt(900), big.NewInt(700)
	if !peerChainIsBetter(8, more, 9, less) {
		t.Error("a peer with more work but fewer blocks should be preferred")
	}
	if peerChainIsBetter(9, less, 8, more) {
		t.Error("a peer with more blocks but less work should not be preferred")
	}
	if !peerChainIsBetter(9, nil, 8, more) || peerChainIsBetter(8, nil, 9, less) {
		t.Error("a peer that reports no work (older software) must be judged by height")
	}
	if peerChainIsBetter(0, more, 8, less) {
		t.Error("a peer that did not answer was preferred")
	}
}

func TestSameHeightRace(t *testing.T) {
	saved := HeartbeatActivationHeight
	HeartbeatActivationHeight = 1
	defer func() { HeartbeatActivationHeight = saved }()

	low := Block{Index: 50, Difficulty: 70000, Hash: "0000aaaa"}
	high := Block{Index: 50, Difficulty: 70000, Hash: "0000bbbb"}
	if !siblingWins(low, high) || siblingWins(high, low) {
		t.Error("with equal work the lower hash must win")
	}
	high.Difficulty = 140000
	if !siblingWins(high, low) || siblingWins(low, high) {
		t.Error("more work must win even with the higher hash")
	}
}

// Storing the same block twice is harmless and changes nothing.
func TestStoringTheSameBlockTwice(t *testing.T) {
	startTestChain(t, "devnet")
	block := extendTestChain(t, Params.GenesisTimestamp+200, "CVN_minerA", nil)
	if SaveBlockToStorageInternal(block) {
		t.Error("a block already stored was reported as newly stored")
	}
	if balanceOf("CVN_minerA") != 50 || GetLatestBlockInternal().Hash != block.Hash {
		t.Errorf("state changed after a repeated save: balance %v", balanceOf("CVN_minerA"))
	}
}
