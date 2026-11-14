package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// AMOUNTS
//
// Inside the program every amount of CVN is an Amount: a whole number of
// units, 100,000,000 to one CVN. All arithmetic and every comparison in the
// rules is done on those whole numbers. Nothing in the rules uses fractions.
//
// How an amount is written into a block depends on the network:
//
//   - On a network with AmountsAsUnits set (devnet, and the launch network),
//     it is written as the whole number of units, in blocks, in the coin
//     database and in what is hashed and signed.
//
//   - testnet-1 began before this and writes amounts as decimal CVN numbers.
//     Its block hashes and transfer IDs were computed from those decimal
//     numbers exactly as written, so an Amount read on testnet-1 also keeps
//     the original decimal number and reproduces it when hashing or writing.
//     That is the only use of the decimal form; the rules never read it.
//
// The testnet-1 handling is confined to this file and can be deleted when
// that network is retired.

// UnitsPerCVN is the number of smallest units in one CVN.
const UnitsPerCVN = int64(100000000)

// MaxAmountUnits is the largest amount any rule will accept or add up.
const MaxAmountUnits = int64(2100000000) * UnitsPerCVN

// Amount is a quantity of CVN held as whole units.
type Amount struct {
	units int64
	// decimal is the number as written on a network that stores decimals.
	decimal float64
	// invalid marks a value that was not a usable amount when read.
	invalid bool
}

// UnitsAmount makes an Amount from a whole number of units.
func UnitsAmount(units int64) Amount {
	return Amount{units: units, decimal: float64(units) / float64(UnitsPerCVN)}
}

// Units is the amount as a whole number of units.
func (a Amount) Units() int64 { return a.units }

// CVN is the amount as a decimal number of CVN, for display only.
func (a Amount) CVN() float64 { return float64(a.units) / float64(UnitsPerCVN) }

// String writes the amount in CVN with up to eight decimals.
func (a Amount) String() string { return FormatUnits(a.units) }

// Spendable reports whether this is a valid amount for an output: at least
// one unit and no more than can exist.
func (a Amount) Spendable() bool {
	return !a.invalid && a.units >= 1 && a.units <= MaxAmountUnits
}

// ValidFee reports whether this is a valid fee: zero or more, and no more
// than can exist.
func (a Amount) ValidFee() bool {
	return !a.invalid && a.units >= 0 && a.units <= MaxAmountUnits
}

// hashText is the form of the amount that goes into hashes and signatures.
func (a Amount) hashText() string {
	if Params.AmountsAsUnits {
		return strconv.FormatInt(a.units, 10)
	}
	return strconv.FormatFloat(a.decimal, 'g', -1, 64)
}

// MarshalJSON writes the amount the way the current network stores it.
func (a Amount) MarshalJSON() ([]byte, error) {
	if Params.AmountsAsUnits {
		return []byte(strconv.FormatInt(a.units, 10)), nil
	}
	if math.IsNaN(a.decimal) || math.IsInf(a.decimal, 0) {
		return []byte("0"), nil
	}
	return json.Marshal(a.decimal)
}

// UnmarshalJSON reads an amount the way the current network stores it. It
// never fails on a badly formed number; it marks the amount invalid, and the
// rules then refuse whatever carries it.
func (a *Amount) UnmarshalJSON(data []byte) error {
	text := string(bytes.TrimSpace(data))
	if Params.AmountsAsUnits {
		units, err := parseWholeUnits(text)
		if err != nil {
			*a = Amount{invalid: true}
			return nil
		}
		*a = UnitsAmount(units)
		return nil
	}
	decimal, err := strconv.ParseFloat(text, 64)
	if err != nil {
		*a = Amount{invalid: true}
		return nil
	}
	units, ok := decimalToUnits(decimal)
	*a = Amount{units: units, decimal: decimal, invalid: !ok}
	return nil
}

// parseWholeUnits accepts only plain digits: no sign, decimal point, exponent
// or quotes.
func parseWholeUnits(text string) (int64, error) {
	if text == "" || len(text) > 19 {
		return 0, fmt.Errorf("not a whole number of units")
	}
	for _, c := range text {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a whole number of units")
		}
	}
	units, err := strconv.ParseInt(text, 10, 64)
	if err != nil || units > MaxAmountUnits {
		return 0, fmt.Errorf("more than can exist")
	}
	return units, nil
}

// decimalToUnits rounds a decimal CVN number to the nearest unit.
func decimalToUnits(decimal float64) (int64, bool) {
	if math.IsNaN(decimal) || math.IsInf(decimal, 0) || decimal < 0 || decimal > float64(MaxAmountUnits/UnitsPerCVN) {
		return 0, false
	}
	return int64(math.Round(decimal * float64(UnitsPerCVN))), true
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
	if Params.Economics != nil {
		return Params.Economics.RewardUnitsAt(height)
	}
	units, _ := decimalToUnits(CalculateBlockReward(height))
	return units
}

// FormatUnits writes a unit amount as a CVN number with up to eight decimals.
func FormatUnits(units int64) string {
	sign := ""
	if units < 0 {
		sign, units = "-", -units
	}
	whole, fraction := units/UnitsPerCVN, units%UnitsPerCVN
	if fraction == 0 {
		return fmt.Sprintf("%s%d", sign, whole)
	}
	text := fmt.Sprintf("%s%d.%08d", sign, whole, fraction)
	return strings.TrimRight(text, "0")
}

// ParseCVN reads an amount typed by a person, such as "1000", "0.5" or
// "12.00000001", into units exactly. It never goes through a floating-point
// number, so what was typed is what is sent.
func ParseCVN(text string) (int64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("no amount given")
	}
	wholeText, fractionText, hasPoint := strings.Cut(text, ".")
	if wholeText == "" {
		wholeText = "0"
	}
	if hasPoint && fractionText == "" {
		fractionText = "0"
	}
	for _, c := range wholeText + fractionText {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("%q is not a number", text)
		}
	}
	if len(fractionText) > 8 {
		if strings.Trim(fractionText[8:], "0") != "" {
			return 0, fmt.Errorf("CVN has at most 8 decimal places")
		}
		fractionText = fractionText[:8]
	}
	if len(wholeText) > 11 {
		return 0, fmt.Errorf("that is more CVN than can exist")
	}
	whole, err := strconv.ParseInt(wholeText, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", text)
	}
	fraction := int64(0)
	if fractionText != "" {
		padded := fractionText + strings.Repeat("0", 8-len(fractionText))
		fraction, _ = strconv.ParseInt(padded, 10, 64)
	}
	units := whole*UnitsPerCVN + fraction
	if units > MaxAmountUnits {
		return 0, fmt.Errorf("that is more CVN than can exist")
	}
	return units, nil
}
