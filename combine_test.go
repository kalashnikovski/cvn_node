package main

import (
	"fmt"
	"testing"
)

func testCoins(count int) []SpendableCoin {
	coins := make([]SpendableCoin, count)
	for i := range coins {
		// Amounts fall as i rises, the order a node lists them in.
		coins[i] = SpendableCoin{TxID: fmt.Sprintf("TX_%05d", i), Index: 0, Amount: UnitsAmount(int64(count-i) * 1000000)}
	}
	return coins
}

func TestPlanCombine(t *testing.T) {
	onNetwork(t, "devnet")
	for _, count := range []int{0, 1} {
		if groups := planCombine(testCoins(count)); len(groups) != 0 {
			t.Errorf("%d coin(s) gave %d groups; there is nothing to combine", count, len(groups))
		}
	}
	sizes := func(groups [][]SpendableCoin) []int {
		var out []int
		for _, group := range groups {
			out = append(out, len(group))
		}
		return out
	}
	cases := map[int][]int{
		2:                                     {2},
		TxMaxInputs:                           {TxMaxInputs},
		TxMaxInputs + 1:                       {TxMaxInputs}, // the coin left over is not sent to itself for nothing
		2*TxMaxInputs + 50:                    {TxMaxInputs, TxMaxInputs, 50},
		combineMaxTransfers*TxMaxInputs + 500: nil, // filled in below
	}
	for i := 0; i < combineMaxTransfers; i++ {
		cases[combineMaxTransfers*TxMaxInputs+500] = append(cases[combineMaxTransfers*TxMaxInputs+500], TxMaxInputs)
	}
	for count, want := range cases {
		got := sizes(planCombine(testCoins(count)))
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%d coins were grouped as %v, want %v", count, got, want)
		}
	}

	// The smallest coins are combined first, and no coin is used twice.
	groups := planCombine(testCoins(2*TxMaxInputs + 50))
	seen := make(map[string]bool)
	var previous int64
	for _, group := range groups {
		for _, coin := range group {
			if seen[coin.TxID] {
				t.Fatalf("coin %s is in two groups", coin.TxID)
			}
			seen[coin.TxID] = true
			if coin.Amount.Units() < previous {
				t.Fatal("coins are not taken smallest first")
			}
			previous = coin.Amount.Units()
		}
	}
}

func TestCombineRefusals(t *testing.T) {
	onNetwork(t, "devnet")
	privateKey, publicKey, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildSignedCombine(privateKey, publicKey, 0, testCoins(1)); err == nil {
		t.Error("a single coin was combined")
	}
	if _, err := BuildSignedCombine(privateKey, publicKey, 0, testCoins(TxMaxInputs+1)); err == nil {
		t.Error("more coins than one transfer allows were combined")
	}
	if _, err := BuildSignedCombine(privateKey, publicKey, -1, testCoins(2)); err == nil {
		t.Error("a negative fee was accepted")
	}
	// Two coins of 1 and 2 units cannot pay a fee and still leave a coin.
	tiny := []SpendableCoin{{TxID: "A", Amount: UnitsAmount(1)}, {TxID: "B", Amount: UnitsAmount(2)}}
	if _, err := BuildSignedCombine(privateKey, publicKey, 3, tiny); err == nil {
		t.Error("a fee that swallows every coin was accepted")
	}
}

// Twelve mining rewards become one coin, on the launch rules and on the rules
// of the first test network.
func TestCombineOnAChain(t *testing.T) {
	for _, network := range []string{"devnet", "testnet-1"} {
		t.Run(network, func(t *testing.T) {
			startTestChain(t, network, func() {
				Params.TxForkHeight = 1
				if Params.HeartbeatHeight > 11 {
					Params.HeartbeatHeight = 11
				}
				applyParams()
			})
			privateKey, publicKey, owner, err := GenerateKeyPair()
			if err != nil {
				t.Fatal(err)
			}
			_, timestamp := buildSharedHistory(t, owner, owner)
			coins := ListSpendableCoins(owner)
			if len(coins) != 12 || balanceOf(owner) != cvn("600") {
				t.Fatalf("the miner should hold 12 coins and 600 CVN, has %d coins and %s", len(coins), FormatUnits(balanceOf(owner)))
			}
			groups := planCombine(coins)
			if len(groups) != 1 {
				t.Fatalf("12 coins were split into %d groups", len(groups))
			}
			combine, err := BuildSignedCombine(privateKey, publicKey, cvn("0.5"), groups[0])
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if len(combine.Inputs) != 12 || len(combine.Outputs) != 1 || combine.Outputs[0].Recipient != owner || combine.Outputs[0].Amount.Units() != cvn("599.5") {
				t.Fatalf("the combine transfer is not 12 coins into one of 599.5 CVN: %d in, %+v", len(combine.Inputs), combine.Outputs)
			}

			// While it waits for a block its coins are spoken for, so a second
			// press of the button finds nothing to do.
			if err := AdmitTransactionToMempool(combine); err != nil {
				t.Fatalf("a node refused the combine transfer: %v", err)
			}
			if left := ListSpendableCoins(owner); len(left) != 0 || len(planCombine(left)) != 0 {
				t.Errorf("%d coins are offered again while the combine is waiting", len(left))
			}

			extendTestChain(t, timestamp+45, "CVN_other_miner", []Transaction{combine})
			after := ListSpendableCoins(owner)
			if len(after) != 1 || after[0].Amount.Units() != cvn("599.5") {
				t.Fatalf("after the block the wallet should hold one coin of 599.5 CVN: %+v", after)
			}
			if got := balanceOf(owner); got != cvn("599.5") {
				t.Errorf("the balance is %s, want 599.5", FormatUnits(got))
			}
			// The combined coin spends like any other.
			spend, err := BuildSignedTransfer(privateKey, publicKey, "CVN_someone", cvn("599.5"), 0, after)
			if err != nil {
				t.Fatalf("spend the combined coin: %v", err)
			}
			extendTestChain(t, timestamp+90, "CVN_other_miner", []Transaction{spend})
			if balanceOf(owner) != 0 || balanceOf("CVN_someone") != cvn("599.5") {
				t.Error("the combined coin did not move in full")
			}
		})
	}
}
