package node

import (
	"fmt"
	"testing"
	"time"
)

// waiting builds a list of valid waiting transfers for the block-filling
// tests. Each spends one coin of its own, created at height `age`.
func waiting(prefix string, count int, fee func(i int) int64, age int64, size int) []mempoolCandidate {
	base := time.Unix(1_800_000_000, 0)
	list := make([]mempoolCandidate, count)
	for i := range list {
		id := fmt.Sprintf("%s%04d", prefix, i)
		list[i] = mempoolCandidate{
			tx:      Transaction{ID: id},
			fee:     fee(i),
			spent:   []SpentRecord{{Key: "coin-of-" + id, Record: UTXORecord{Age: age}}},
			arrived: base.Add(time.Duration(i) * time.Second), // later entries arrived later
			size:    size,
		}
	}
	return list
}

func noFee(int) int64 { return 0 }

func countByPrefix(chosen []Transaction) map[string]int {
	counts := make(map[string]int)
	for _, tx := range chosen {
		counts[tx.ID[:1]]++
	}
	return counts
}

func TestBlockFillingLanes(t *testing.T) {
	const height = 10000
	const limit = 499 // a block has 500 slots; one is the reward transaction
	settled := int64(height) - freeLaneMinAgeBlocks
	young := settled + 1
	rising := func(i int) int64 { return int64(1000 + i) } // later entries pay more

	// Far more paid and free transfers than fit: four fifths paid, one fifth free.
	crowded := append(waiting("P", 600, rising, young, 400), waiting("F", 300, noFee, settled, 400)...)
	chosen, fees := fillBlockFromCandidates(crowded, limit, 0, height, true)
	counts := countByPrefix(chosen)
	if len(chosen) != limit || counts["P"] != 400 || counts["F"] != 99 {
		t.Fatalf("a crowded block holds %d paid and %d free of %d; want 400 and 99 of 499", counts["P"], counts["F"], len(chosen))
	}
	// The paid ones are the 400 highest fees; the free ones the 99 that waited longest.
	var wantFees int64
	for i := 200; i < 600; i++ {
		wantFees += rising(i)
	}
	if fees != wantFees {
		t.Errorf("the fees in the block are %d; the 400 highest add up to %d", fees, wantFees)
	}
	for _, tx := range chosen {
		if tx.ID[:1] == "F" && tx.ID > "F0098" {
			t.Fatalf("free transfer %s was taken ahead of ones that waited longer", tx.ID)
		}
	}

	// Space the paid lane does not use goes to free transfers...
	quiet := append(waiting("P", 10, rising, young, 400), waiting("F", 600, noFee, settled, 400)...)
	chosen, _ = fillBlockFromCandidates(quiet, limit, 0, height, true)
	if counts = countByPrefix(chosen); counts["P"] != 10 || counts["F"] != 489 {
		t.Errorf("with few paid transfers the block holds %d paid and %d free; want 10 and 489", counts["P"], counts["F"])
	}
	// ...and space the free lane does not use goes to paid ones.
	chosen, _ = fillBlockFromCandidates(waiting("P", 600, rising, young, 400), limit, 0, height, true)
	if len(chosen) != limit {
		t.Errorf("with no free transfers waiting the block holds %d paid; want all %d slots used", len(chosen), limit)
	}

	// Fee-less transfers of young coins have no claim on the free lane: they
	// go in only after everything else, into whatever is left.
	mixed := append(waiting("P", 450, rising, young, 400), waiting("Y", 100, noFee, young, 400)...)
	chosen, _ = fillBlockFromCandidates(mixed, limit, 0, height, true)
	if counts = countByPrefix(chosen); counts["P"] != 450 || counts["Y"] != 49 {
		t.Errorf("young free transfers: the block holds %d paid and %d young free; want 450 and 49", counts["P"], counts["Y"])
	}
	flood := append(waiting("Y", 600, noFee, young, 400), waiting("F", 50, noFee, settled, 400)...)
	chosen, _ = fillBlockFromCandidates(flood, limit, 0, height, true)
	if counts = countByPrefix(chosen); counts["F"] != 50 {
		t.Errorf("a flood of young free transfers pushed out settled ones: only %d of 50 got in", counts["F"])
	}
	// On a quiet network a fee-less transfer of a young coin still confirms at once.
	chosen, _ = fillBlockFromCandidates(waiting("Y", 3, noFee, young, 400), limit, 0, height, true)
	if len(chosen) != 3 {
		t.Errorf("with the block nearly empty, %d of 3 young free transfers went in", len(chosen))
	}

	// The same split applies to the bytes of a block. Room for 10 transfers
	// of 1,000 bytes: eight paid, two free.
	byBytes := append(waiting("P", 50, rising, young, 1000), waiting("F", 50, noFee, settled, 1000)...)
	chosen, _ = fillBlockFromCandidates(byBytes, limit, 10000, height, true)
	if counts = countByPrefix(chosen); counts["P"] != 8 || counts["F"] != 2 {
		t.Errorf("by bytes the block holds %d paid and %d free; want 8 and 2", counts["P"], counts["F"])
	}

	// Two transfers spending the same coin never share a block.
	rivals := waiting("P", 2, rising, young, 400)
	rivals[1].spent = rivals[0].spent
	chosen, _ = fillBlockFromCandidates(rivals, limit, 0, height, true)
	if len(chosen) != 1 || chosen[0].ID != "P0001" {
		t.Errorf("of two transfers spending one coin the block should hold the higher fee only: %v", chosen)
	}
}

// A network that does not record coin ages has no free lane: highest fee first.
func TestBlockFillingWithoutLanes(t *testing.T) {
	list := append(waiting("P", 3, func(i int) int64 { return int64(5 + i) }, 0, 400), waiting("F", 3, noFee, 0, 400)...)
	chosen, fees := fillBlockFromCandidates(list, 4, 0, 10000, false)
	if len(chosen) != 4 || fees != 5+6+7 || chosen[0].ID != "P0002" {
		t.Errorf("without lanes the block should be the three paid transfers, highest first, then one free: %v (fees %d)", chosen, fees)
	}
}

// End to end on the launch rules: fee-less transfers of coins older than the
// free-lane age get in ahead of fee-less transfers of young coins.
func TestFreeLaneOnAChain(t *testing.T) {
	savedAge := freeLaneMinAgeBlocks
	freeLaneMinAgeBlocks = 5
	t.Cleanup(func() { freeLaneMinAgeBlocks = savedAge })
	startTestChain(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp
	for i := 0; i < 8; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, owner, nil)
	}
	for _, coin := range ListSpendableCoins(owner) { // coins created at heights 1 to 8
		transfer, err := BuildSignedTransfer(privateKey, publicKey, "CVN_someone", coin.Amount.Units(), 0, []SpendableCoin{coin})
		if err != nil {
			t.Fatal(err)
		}
		if err := AdmitTransactionToMempool(transfer); err != nil {
			t.Fatalf("mempool refused a transfer: %v", err)
		}
	}
	// The next block is #9, so the coins from heights 1 to 4 are old enough.
	chosen, fees := SelectMempoolTransactionsForBlock(4)
	if len(chosen) != 4 || fees != 0 {
		t.Fatalf("the miner chose %d transfers with fees %d; want 4 fee-less ones", len(chosen), fees)
	}
	height := GetLatestBlockInternal().Index + 1
	created := make(map[string]int64)
	for h := int64(1); h < height; h++ {
		created[GetBlockByHeightFromDB(h).Transactions[0].ID] = h
	}
	for _, transfer := range chosen {
		if from := created[transfer.Inputs[0].SourceTxID]; height-from < freeLaneMinAgeBlocks {
			t.Errorf("a coin made at height %d was taken for block #%d ahead of older ones", from, height)
		}
	}
	block := extendTestChain(t, timestamp+45, "CVN_other", chosen)
	if len(block.Transactions) != 5 {
		t.Errorf("the block holds %d transactions, want the reward and 4 transfers", len(block.Transactions))
	}
}
