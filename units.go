package main

import (
	"fmt"
	"math"
)

// WHOLE-NUMBER UNITS
//
// Amounts travel and are stored as decimal CVN numbers (floating point). Adding
// and comparing those directly can leave tiny rounding leftovers, and in rare
// cases two machines could disagree by one such leftover about whether a
// transfer or a reward is valid.
//
// From UnitsForkActivationHeight on, every rule that adds or compares amounts
// first turns each amount into a whole number of units (1 CVN = 100,000,000
// units) and does all arithmetic on those whole numbers, with no tolerance:
//
//   - an output must be at least 1 unit;
//   - a transfer's outputs may not exceed its inputs, exactly;
//   - a block's reward outputs may not exceed reward plus fees, exactly.
//
// The stored and transmitted format does not change here. Every block that is
// valid under these rules is also valid under the older ones, so nodes that
// have not updated keep following the chain; they simply check less strictly.
// (The move to storing units directly belongs to the new genesis.)
//
// Limit of this approach: a decimal number holds a unit amount exactly only up
// to about 20,000,000 CVN in a single amount. Every node still computes the
// same result above that, but a single output that large could be off by one
// unit from what its sender meant. This chain's whole supply is far below
// that, and the new genesis stores units directly, which removes the limit.

// UnitsPerCVN is the number of smallest units in one CVN.
const UnitsPerCVN = int64(100000000)

// MaxAmountUnits is the largest amount any rule will add up.
const MaxAmountUnits = int64(2100000000) * UnitsPerCVN

// UnitsForkActivationHeight is the first block checked in whole units.
// A variable so tests can activate it early; treat it as a constant.
var UnitsForkActivationHeight = int64(6600)

// IsUnitsForkHeight reports whether a block at this height is checked in
// whole units.
func IsUnitsForkHeight(height int64) bool {
	return height >= UnitsForkActivationHeight
}

// AmountToUnits converts a decimal CVN amount to whole units, rounding to the
// nearest unit. ok is false for anything that is not a usable amount.
func AmountToUnits(amount float64) (units int64, ok bool) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 || amount > TxMaxAmount {
		return 0, false
	}
	return int64(math.Round(amount * float64(UnitsPerCVN))), true
}

// UnitsToAmount converts whole units back to the decimal CVN number that is
// stored and transmitted. The result is the closest representable number, the
// same one any machine gets when it reads the decimal text back.
func UnitsToAmount(units int64) float64 {
	return float64(units) / float64(UnitsPerCVN)
}

// addUnits adds two unit amounts and refuses totals no real balance can reach.
func addUnits(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > MaxAmountUnits || b > MaxAmountUnits || a+b > MaxAmountUnits {
		return 0, fmt.Errorf("amounts add up to more than can exist")
	}
	return a + b, nil
}

// BlockRewardUnits is the newly issued reward for a height, in units.
func BlockRewardUnits(height int64) int64 {
	units, _ := AmountToUnits(CalculateBlockReward(height))
	return units
}

// FormatUnits writes a unit amount as a CVN number with up to eight decimals.
func FormatUnits(units int64) string {
	whole, fraction := units/UnitsPerCVN, units%UnitsPerCVN
	if fraction == 0 {
		return fmt.Sprintf("%d", whole)
	}
	text := fmt.Sprintf("%d.%08d", whole, fraction)
	for text[len(text)-1] == '0' {
		text = text[:len(text)-1]
	}
	return text
}
