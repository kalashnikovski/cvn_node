package main

import (
	"fmt"
	"sort"
	"strings"
)

// COMBINE MY COINS
//
// A wallet's balance is held as separate coins. A miner receives a new one
// for every block won, so a busy miner soon owns hundreds, and one transfer
// may spend at most TxMaxInputs of them. Combining sends coins back to their
// own address in groups, each group becoming a single large coin. Nothing
// about it is special to the network: every group is an ordinary transfer
// from an address to itself.
//
// On a network with the Sabbatical sweep, a combined coin is a new coin, so
// combining is also how a long-term holder shows that coins are still theirs.

// combineMaxTransfers limits how many transfers one combine action sends, so
// that a single wallet does not fill a block. With 200 coins per transfer
// that is 5,000 coins a time; the action can simply be repeated.
const combineMaxTransfers = 25

// CombineResult describes what a combine action did.
type CombineResult struct {
	Address       string
	CoinsBefore   int
	CoinsCombined int
	Transfers     []Transaction
}

// CoinsAfter is how many coins the wallet holds once the transfers confirm.
func (r CombineResult) CoinsAfter() int {
	return r.CoinsBefore - r.CoinsCombined + len(r.Transfers)
}

// Summary is one sentence for the person using the wallet.
func (r CombineResult) Summary() string {
	text := fmt.Sprintf("Combined %d coins into %d with %d transfer(s) to %s. The wallet will hold %d coin(s) once they confirm in the next blocks.",
		r.CoinsCombined, len(r.Transfers), len(r.Transfers), r.Address, r.CoinsAfter())
	if r.CoinsAfter() > len(r.Transfers) && len(r.Transfers) == combineMaxTransfers {
		text += " Run it again after that to combine the rest."
	} else if len(r.Transfers) > 1 {
		text += " Run it once more after that to merge those into one."
	}
	return text
}

// planCombine sorts a wallet's coins into the groups that will each become
// one coin. The smallest coins go first, so the clutter is cleared before
// anything else, and a coin left on its own is not touched.
func planCombine(coins []SpendableCoin) [][]SpendableCoin {
	usable := make([]SpendableCoin, 0, len(coins))
	for _, coin := range coins {
		if coin.Amount.Spendable() {
			usable = append(usable, coin)
		}
	}
	sort.SliceStable(usable, func(i, j int) bool {
		if usable[i].Amount.Units() != usable[j].Amount.Units() {
			return usable[i].Amount.Units() < usable[j].Amount.Units()
		}
		if usable[i].TxID != usable[j].TxID {
			return usable[i].TxID < usable[j].TxID
		}
		return usable[i].Index < usable[j].Index
	})
	var groups [][]SpendableCoin
	for len(usable) >= 2 && len(groups) < combineMaxTransfers {
		size := TxMaxInputs
		if size > len(usable) {
			size = len(usable)
		}
		groups = append(groups, usable[:size])
		usable = usable[size:]
	}
	return groups
}

// BuildSignedCombine builds one transfer that spends every coin given and
// pays their total, less the fee, back to the owner as a single coin.
func BuildSignedCombine(privateKeyHex, publicKeyHex string, feeUnits int64, coins []SpendableCoin) (Transaction, error) {
	if len(coins) < 2 {
		return Transaction{}, fmt.Errorf("combining needs at least two coins")
	}
	if len(coins) > TxMaxInputs {
		return Transaction{}, fmt.Errorf("one transfer can combine at most %d coins", TxMaxInputs)
	}
	if feeUnits < 0 {
		return Transaction{}, fmt.Errorf("fee cannot be negative")
	}
	ownerAddress, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		return Transaction{}, err
	}
	inputs := make([]UTXOInput, 0, len(coins))
	var totalUnits int64
	for _, coin := range coins {
		if !coin.Amount.Spendable() {
			return Transaction{}, fmt.Errorf("coin %s cannot be spent", utxoKey(coin.TxID, coin.Index))
		}
		if totalUnits, err = addUnits(totalUnits, coin.Amount.Units()); err != nil {
			return Transaction{}, err
		}
		inputs = append(inputs, UTXOInput{SourceTxID: coin.TxID, Index: coin.Index})
	}
	if totalUnits-feeUnits < minimumOutputUnits() {
		return Transaction{}, fmt.Errorf("these coins add up to %s CVN, which is not enough to pay a fee of %s CVN and leave a coin", FormatUnits(totalUnits), FormatUnits(feeUnits))
	}
	outputs := []UTXOOutput{{Recipient: ownerAddress, Amount: UnitsAmount(totalUnits - feeUnits)}}
	return signTransfer(privateKeyHex, publicKeyHex, ownerAddress, inputs, outputs, feeUnits)
}

// CombineCoinsFromWallet fetches a wallet's coins from a node, combines them
// in groups and submits each group's transfer. feeUnits is paid per transfer.
// If a transfer is refused part-way, the result lists the ones already sent.
func CombineCoinsFromWallet(privateKeyHex, publicKeyHex string, feeUnits int64) (CombineResult, error) {
	privateKeyHex, publicKeyHex = strings.TrimSpace(privateKeyHex), strings.TrimSpace(publicKeyHex)
	ownerAddress, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		return CombineResult{}, err
	}
	coins, gateway, err := FetchSpendableCoinsOverHTTP(ownerAddress)
	if err != nil {
		return CombineResult{}, err
	}
	result := CombineResult{Address: ownerAddress, CoinsBefore: len(coins)}
	groups := planCombine(coins)
	if len(groups) == 0 {
		return result, fmt.Errorf("this wallet holds %d coin(s) ready to spend, so there is nothing to combine", len(coins))
	}
	for _, group := range groups {
		tx, err := BuildSignedCombine(privateKeyHex, publicKeyHex, feeUnits, group)
		if err != nil {
			return result, err
		}
		if err := SubmitTransferOverHTTP(gateway, tx); err != nil {
			return result, err
		}
		result.Transfers = append(result.Transfers, tx)
		result.CoinsCombined += len(group)
	}
	return result, nil
}
