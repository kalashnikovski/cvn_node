package node

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// partsOf makes a division, most first, from amounts in CVN.
func partsOf(amounts ...string) ([]poolPart, int64) {
	var parts []poolPart
	var total int64
	for i, amount := range amounts {
		units := cvn(amount)
		parts = append(parts, poolPart{Address: testAddress(fmt.Sprintf("miner %02d", i)), Units: units, Work: units})
		total += units
	}
	return parts, total
}

// A division becomes the outputs of a block's reward: the parts that fit
// paid to their addresses, the rest to the pool's.
func TestADivisionBecomesTheOutputsOfAReward(t *testing.T) {
	onNetwork(t, "devnet")
	pool, finder := testAddress("the pool"), testAddress("the finder")

	check := func(what string, parts []poolPart, total int64, wantInBlock int, wantPoolUnits int64) ([]UTXOOutput, []poolPart) {
		t.Helper()
		outputs, division, poolIndex, poolUnits := poolRewardOutputs(parts, pool, finder, total)
		var paid, inBlock, kept int64
		seen := map[string]bool{}
		for _, output := range outputs {
			if seen[output.Recipient] {
				t.Errorf("%s: %s is paid twice", what, output.Recipient)
			}
			seen[output.Recipient] = true
			paid += output.Amount.Units()
		}
		marked := 0
		for _, part := range division {
			if part.InBlock {
				marked++
				inBlock += part.Units
			} else {
				kept += part.Units
			}
		}
		if paid != total || inBlock+kept != total || kept != poolUnits {
			t.Errorf("%s: the outputs pay %s, the parts are %s in the block and %s kept, the pool's output is %s; the reward is %s",
				what, FormatUnits(paid), FormatUnits(inBlock), FormatUnits(kept), FormatUnits(poolUnits), FormatUnits(total))
		}
		if marked != wantInBlock || poolUnits != wantPoolUnits || len(outputs) > RewardMaxOutputs {
			t.Errorf("%s: %d parts paid in the block and %s to the pool, in %d outputs; want %d and %s",
				what, marked, FormatUnits(poolUnits), len(outputs), wantInBlock, FormatUnits(wantPoolUnits))
		}
		if poolUnits > 0 && (poolIndex != len(outputs)-1 || outputs[poolIndex].Recipient != pool || outputs[poolIndex].Amount.Units() != poolUnits) {
			t.Errorf("%s: the pool's output is not the last, or not what was kept", what)
		}
		// And a block with that reward is one the rules accept.
		reward := Transaction{Version: 1, ID: "TX_COINBASE_H100_0a", Inputs: []UTXOInput{}, Witness: pool,
			PublicKey: CoinbasePublicKeyMarker, Signature: CoinbaseSignatureMarker, Outputs: outputs}
		nothing := func(string) (UTXORecord, bool) { return UTXORecord{}, false }
		if _, _, err := ValidateBlockTransactions(Block{Index: 100, Transactions: []Transaction{reward}}, nothing, 0); err != nil {
			t.Errorf("%s: a block with this reward is refused: %v", what, err)
		}
		return outputs, division
	}

	// Three miners: all three paid by the block, and the pool holds nothing.
	parts, total := partsOf("30", "15", "5")
	outputs, _ := check("three miners", parts, total, 3, 0)
	if len(outputs) != 3 || outputs[0].Recipient != parts[0].Address || outputs[0].Amount.Units() != cvn("30") {
		t.Errorf("three miners: the outputs are %+v", outputs)
	}

	// Fifty fit exactly. Sixty do not: forty-nine are paid and the pool
	// keeps the parts of the eleven smallest.
	fifty := make([]string, 50)
	for i := range fifty {
		fifty[i] = "1"
	}
	parts, total = partsOf(fifty...)
	check("fifty miners", parts, total, 50, 0)
	sixty := make([]string, 60)
	for i := range sixty {
		sixty[i] = fmt.Sprintf("%.2f", float64(60-i)/100) // most first, as a division comes
	}
	parts, total = partsOf(sixty...)
	var smallest int64
	for _, part := range parts[49:] {
		smallest += part.Units
	}
	_, division := check("sixty miners", parts, total, 49, smallest)
	for i, part := range division {
		if part.InBlock != (i < 49) {
			t.Errorf("sixty miners: part %d is marked as paid in the block: %v", i, part.InBlock)
		}
	}

	// A part under a hundredth of a coin is kept by the pool and owed.
	parts, total = partsOf("49.985", "0.009", "0.006")
	check("two small miners", parts, total, 1, cvn("0.015"))

	// What would be left for the pool is too small to be a coin: it cannot
	// be paid to anybody, and goes to the largest part.
	parts, total = partsOf("49.99995", "0.00005")
	outputs, division = check("a crumb", parts, total, 1, 0)
	if len(outputs) != 1 || outputs[0].Amount.Units() != cvn("50") || len(division) != 1 {
		t.Errorf("a crumb: the outputs are %+v and the division %+v", outputs, division)
	}

	// Before anybody has handed in a share, the work pays the miner it is for.
	outputs, division = check("no shares yet", nil, cvn("50"), 1, 0)
	if len(outputs) != 1 || outputs[0].Recipient != finder || len(division) != 1 || division[0].Address != finder {
		t.Errorf("no shares yet: the outputs are %+v", outputs)
	}

	// Somebody mining to the pool's own address is not paid twice over in
	// one reward: that part is in the pool's output.
	parts, total = partsOf("30", "20")
	parts[0].Address = pool
	check("mining to the pool's address", parts, total, 1, cvn("30"))

	// A pool whose miners are all too small: one output, the pool's.
	saved := poolInBlockFloorUnits
	poolInBlockFloorUnits = MaxAmountUnits
	parts, total = partsOf("30", "15", "5")
	outputs, _ = check("nothing paid in the block", parts, total, 0, cvn("50"))
	poolInBlockFloorUnits = saved
	if len(outputs) != 1 || outputs[0].Recipient != pool {
		t.Errorf("nothing paid in the block: the outputs are %+v", outputs)
	}

	// A reward too small to be a coin pays nobody.
	if outputs, _, _, _ := poolRewardOutputs(nil, pool, finder, minimumOutputUnits()-1); len(outputs) != 0 {
		t.Errorf("a reward too small to be a coin was paid: %+v", outputs)
	}
}

// coinsFree lists the coins the record may spend that no payment has set aside.
func (r *poolRecord) coinsFree() []poolCoin {
	r.Lock()
	defer r.Unlock()
	var free []poolCoin
	for _, coin := range r.coins {
		if coin.HeldBy == 0 {
			free = append(free, coin)
		}
	}
	return free
}

// paidByBlocks is what an address has been paid by the blocks themselves, as
// the record has them: every block that has not been lost.
func paidByBlocks(record *poolRecord, address string) int64 {
	record.Lock()
	defer record.Unlock()
	var units int64
	for _, block := range record.blocks {
		if block.State == poolBlockLost {
			continue
		}
		for _, part := range block.Parts {
			if part.InBlock && part.Address == address {
				units += part.Units
			}
		}
	}
	return units
}

// A pool with two miners. The block pays them both itself, by the work each
// did. The pool's address receives nothing, owes nothing, and has nothing to
// pay.
func TestAPoolPaysItsMinersInTheBlock(t *testing.T) {
	pool := startPayingPoolAt(t, 16, cvn("0.01"))
	first := GetBlockByHeightFromDB(1)
	reward := first.Transactions[0]
	if len(reward.Outputs) != 2 || reward.Outputs[0].Recipient != pool.alice || reward.Outputs[0].Amount.Units() != cvn("37.5") ||
		reward.Outputs[1].Recipient != pool.bob || reward.Outputs[1].Amount.Units() != cvn("12.5") || reward.Witness != pool.address {
		t.Fatalf("block 1's reward pays %+v", reward.Outputs)
	}
	if balanceOf(pool.address) != 0 {
		t.Errorf("the pool's address holds %s CVN; its blocks paid the miners", FormatUnits(balanceOf(pool.address)))
	}
	// Each holds on the chain what the blocks paid it, block 1 and since.
	if got, want := balanceOf(pool.alice), paidByBlocks(poolLedger, pool.alice); got != want || got < cvn("37.5") {
		t.Errorf("Alice holds %s CVN on the chain; the blocks paid her %s", FormatUnits(got), FormatUnits(want))
	}
	if got, want := balanceOf(pool.bob), paidByBlocks(poolLedger, pool.bob); got != want || got < cvn("12.5") {
		t.Errorf("Bob holds %s CVN on the chain; the blocks paid him %s", FormatUnits(got), FormatUnits(want))
	}

	// In the record: block 1 counts, both were paid in it, nothing is owed.
	alice, _ := poolLedger.account(pool.alice, 16)
	bob, _ := poolLedger.account(pool.bob, 16)
	if alice.Earned != cvn("37.5") || alice.InBlock != cvn("37.5") || alice.Owed != 0 || bob.InBlock != cvn("12.5") || bob.Owed != 0 {
		t.Errorf("after block 1 counts: Alice %+v, Bob %+v", alice, bob)
	}
	view := poolLedger.view(16, time.Now().Unix(), 10)
	for _, block := range view.Blocks {
		if block.PoolUnits != 0 {
			t.Errorf("block %d paid the pool's address %s CVN", block.Height, FormatUnits(block.PoolUnits))
		}
		for _, part := range block.Parts {
			if !part.InBlock {
				t.Errorf("a part of block %d was not paid in the block: %+v", block.Height, part)
			}
		}
	}
	if view.Owed != 0 || view.InBlock != cvn("50") || len(poolLedger.coinsFree()) != 0 {
		t.Errorf("the pool owes %s CVN, the blocks paid %s, and the record holds %d coins", FormatUnits(view.Owed), FormatUnits(view.InBlock), len(poolLedger.coinsFree()))
	}
	// Asked to pay, there is nothing to pay.
	if payments, reason, err := poolPrepare(time.Now()); err != nil || len(payments) != 0 || reason == "" {
		t.Errorf("a pool that owes nothing prepared %d payments (%q): %v", len(payments), reason, err)
	}

	// The page says who was paid by the blocks, and a copy of the record adds up.
	pages := http.NewServeMux()
	addPoolPages(pages)
	reply := httptest.NewRecorder()
	pages.ServeHTTP(reply, httptest.NewRequest("GET", "/pool.json", nil))
	var page poolPage
	if err := json.Unmarshal(reply.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.InBlockUnits == 0 || page.OwedUnits != 0 || page.InBlockFloor != "0.01" || len(page.Blocks) == 0 || !page.Blocks[len(page.Blocks)-1].Parts[0].InBlock {
		t.Errorf("pool.json shows %s paid by blocks, %s owed, floor %s", page.InBlock, page.Owed, page.InBlockFloor)
	}
	if _, report := takeCopy(t, filepath.Join(t.TempDir(), "backups"), "a pool that pays in its blocks"); report.InBlockUnits == 0 || report.OwedUnits != 0 || report.CoinUnits != 0 {
		t.Errorf("the copy says: %s", report)
	}
}

// A pool with one large miner and one too small to be paid in the block.
// The block pays the large one itself and the pool's address the rest, which
// is owed to the small one and paid to it by a transfer.
func TestAPoolKeepsOnlyThePartsTooSmallForTheBlock(t *testing.T) {
	pool := startPayingPoolAt(t, 16, cvn("20"))
	reward := GetBlockByHeightFromDB(1).Transactions[0]
	if len(reward.Outputs) != 2 || reward.Outputs[0].Recipient != pool.alice || reward.Outputs[0].Amount.Units() != cvn("37.5") ||
		reward.Outputs[1].Recipient != pool.address || reward.Outputs[1].Amount.Units() != cvn("12.5") {
		t.Fatalf("block 1's reward pays %+v", reward.Outputs)
	}
	alice, _ := poolLedger.account(pool.alice, 16)
	bob, _ := poolLedger.account(pool.bob, 16)
	if alice.InBlock != cvn("37.5") || alice.Owed != 0 || bob.InBlock != 0 || bob.Owed != cvn("12.5") || bob.Earned != cvn("12.5") {
		t.Fatalf("after block 1 counts: Alice %+v, Bob %+v", alice, bob)
	}
	// The pool may spend what the block paid its address, and that is all.
	free := poolLedger.coinsFree()
	if len(free) != 1 || free[0].TxID != reward.ID || free[0].Index != 1 || free[0].Units != cvn("12.5") {
		t.Fatalf("the record's coins are %+v", free)
	}

	// It pays Bob, from that coin.
	payments := pool.payEverything(t)
	if len(payments) != 1 || len(payments[0].Lines) != 1 || payments[0].Lines[0].Address != pool.bob || payments[0].Lines[0].Debit != cvn("12.5") ||
		len(payments[0].Inputs) != 1 || payments[0].Inputs[0].SourceTxID != reward.ID || payments[0].Inputs[0].Index != 1 {
		t.Fatalf("the payment is %+v", payments)
	}
	pool.mineAndSettle(t, payments)
	bob, _ = poolLedger.account(pool.bob, 16)
	if bob.Paid != cvn("12.5") || bob.Pending != 0 || bob.Earned != bob.InBlock+bob.Owed+bob.Pending+bob.Paid {
		t.Errorf("after the payment: Bob %+v", bob)
	}
	// On the chain each holds what the blocks paid it and what the pool sent.
	if got, want := balanceOf(pool.bob), paidByBlocks(poolLedger, pool.bob)+cvn("12.499"); got != want {
		t.Errorf("Bob holds %s CVN on the chain; the blocks and the payment gave him %s", FormatUnits(got), FormatUnits(want))
	}
	if got, want := balanceOf(pool.alice), paidByBlocks(poolLedger, pool.alice); got != want {
		t.Errorf("Alice holds %s CVN on the chain; the blocks paid her %s", FormatUnits(got), FormatUnits(want))
	}
	takeCopy(t, filepath.Join(t.TempDir(), "backups"), "a pool that keeps the small parts")
}
