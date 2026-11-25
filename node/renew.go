package node

import (
	"fmt"
	"sort"
	"strings"
)

// RENEW MY COINS
//
// A coin that has not been spent for seven years has a fifth of its value
// set aside on it (see sweep.go). The fifth is still its owner's for seven
// more years, and only then goes to the vault. An owner who still holds the
// key loses nothing: spending the coin, even to its own address, makes a new
// coin with a new seven-year count, and takes back whatever was set aside
// on the old one.
//
// Renewing does it in one step. It sends back to their own address the
// coins whose date is near, and every coin that has a fifth set aside, in
// groups, each group becoming one new coin. Like combining, it is nothing
// special to the network: every group is an ordinary transfer from an
// address to itself. Unlike combining, a single coin can be renewed on its
// own. It is sent with no fee unless the owner chooses one: a fifth of every
// block is kept for transfers that pay none.

// renewHorizonBlocks is how near a coin's sweep date must be for renewing to
// include it: one year of blocks. A coin further off than that is left
// alone unless the owner asks for everything to be renewed.
const renewHorizonBlocks = int64(365 * 24 * 3600 / 45)

// renewUrgentBlocks is how near a coin's date counts as close: two days of
// blocks. A renewal with no fee waits its turn when blocks are full, so with
// a date this near the wallet says to check that it went through.
const renewUrgentBlocks = int64(2 * 24 * 3600 / 45)

// RenewResult describes what a renew action did.
type RenewResult struct {
	Address      string
	CoinsHeld    int   // spendable coins in the wallet before
	CoinsRenewed int   // coins sent back to their own address
	EarliestDue  int64 // the earliest sweep block among the wallet's coins before (0 if none)
	Transfers    []Transaction
	// TakenBackUnits is how much of what the renewed coins gave had been
	// set aside on them by a sweep. Once the renewal confirms, it is part
	// of the new coins.
	TakenBackUnits int64
	// CloseToDate is true when a coin was sent for renewal with no fee
	// less than two days before its date.
	CloseToDate bool
}

// Summary is one sentence for the person using the wallet.
func (r RenewResult) Summary() string {
	text := fmt.Sprintf("Renewed %d coin(s) with %d transfer(s) to %s. Once they confirm in the next blocks each is a new coin with a new seven-year count. Wait for that before sending: until then these coins are not ready to spend.",
		r.CoinsRenewed, len(r.Transfers), r.Address)
	if r.TakenBackUnits > 0 {
		text += fmt.Sprintf(" The new coins then hold the %s CVN that had been set aside on the old ones at a seven-year date.", FormatUnits(r.TakenBackUnits))
	}
	if r.CloseToDate {
		text += " One of these coins is less than two days from its date, and a transfer with no fee waits its turn when blocks are full. Look again in a few hours: if it has not confirmed, renew again with a small fee."
	}
	if len(r.Transfers) == combineMaxTransfers {
		text += " There may be more to renew: run it again after that."
	}
	return text
}

// describeBlocksAway says roughly how long a number of blocks is, for a
// person: "about 3 days", "about 2 years".
func describeBlocksAway(blocks int64) string {
	if blocks <= 0 {
		return "now"
	}
	seconds := blocks * 45
	switch {
	case seconds < 2*3600:
		return fmt.Sprintf("about %d minutes", (seconds+59)/60)
	case seconds < 2*86400:
		return fmt.Sprintf("about %d hours", (seconds+1800)/3600)
	case seconds < 60*86400:
		return fmt.Sprintf("about %d days", (seconds+43200)/86400)
	case seconds < 2*365*86400:
		return fmt.Sprintf("about %d months", (seconds+15*86400)/(30*86400))
	default:
		return fmt.Sprintf("about %d years", (seconds+182*86400)/(365*86400))
	}
}

// earliestSweepBlock is the soonest sweep block among a wallet's coins, or 0
// if none of them has one.
func earliestSweepBlock(coins []SpendableCoin) int64 {
	var earliest int64
	for _, coin := range coins {
		if coin.SweepAt > 0 && (earliest == 0 || coin.SweepAt < earliest) {
			earliest = coin.SweepAt
		}
	}
	return earliest
}

// planRenewal picks the coins to renew and sorts them into the groups that
// will each become one new coin: those whose sweep falls within
// horizonBlocks of tipHeight, and every coin with a fifth set aside, however
// far its next date (renewing is how its owner takes the fifth back). With
// horizonBlocks zero or less it is every coin with a sweep date. Soonest
// date first.
func planRenewal(coins []SpendableCoin, tipHeight, horizonBlocks int64) [][]SpendableCoin {
	due := make([]SpendableCoin, 0, len(coins))
	for _, coin := range coins {
		if !coin.Amount.Spendable() || coin.SweepAt <= 0 {
			continue
		}
		if horizonBlocks > 0 && coin.HeldUnits == 0 && coin.SweepAt-tipHeight > horizonBlocks {
			continue
		}
		due = append(due, coin)
	}
	sort.SliceStable(due, func(i, j int) bool {
		if due[i].SweepAt != due[j].SweepAt {
			return due[i].SweepAt < due[j].SweepAt
		}
		if due[i].TxID != due[j].TxID {
			return due[i].TxID < due[j].TxID
		}
		return due[i].Index < due[j].Index
	})
	var groups [][]SpendableCoin
	for len(due) >= 1 && len(groups) < combineMaxTransfers {
		size := TxMaxInputs
		if size > len(due) {
			size = len(due)
		}
		groups = append(groups, due[:size])
		due = due[size:]
	}
	return groups
}

// BuildSignedRenewal builds one transfer that spends every coin given and
// pays their total, less the fee, back to the owner as a single new coin.
// One coin is enough.
func BuildSignedRenewal(privateKeyHex, publicKeyHex string, feeUnits int64, coins []SpendableCoin) (Transaction, error) {
	return buildTransferToSelf(privateKeyHex, publicKeyHex, feeUnits, coins)
}

// RenewCoinsFromWallet fetches a wallet's coins from a node and sends those
// whose sweep date is within a year back to their own address (every coin,
// if everything is true). feeUnits is paid per transfer. If a transfer is
// refused part-way, the result lists the ones already sent.
func RenewCoinsFromWallet(privateKeyHex, publicKeyHex string, feeUnits int64, everything bool) (RenewResult, error) {
	privateKeyHex, publicKeyHex = strings.TrimSpace(privateKeyHex), strings.TrimSpace(publicKeyHex)
	ownerAddress, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		return RenewResult{}, err
	}
	coins, gateway, err := FetchSpendableCoinsOverHTTP(ownerAddress)
	if err != nil {
		return RenewResult{}, err
	}
	result := RenewResult{Address: ownerAddress, CoinsHeld: len(coins), EarliestDue: earliestSweepBlock(coins)}
	if result.EarliestDue == 0 {
		return result, fmt.Errorf("this wallet holds no coin with a sweep date, so there is nothing to renew")
	}
	horizon := renewHorizonBlocks
	tipHeight, heightKnown := walletChainHeight()
	if everything {
		horizon = 0
	} else if !heightKnown {
		return result, fmt.Errorf("no node could say what the newest block is, so the wallet cannot tell which coins are near their date")
	}
	groups := planRenewal(coins, tipHeight, horizon)
	if len(groups) == 0 {
		return result, fmt.Errorf("no coin of this wallet is within a year of its date: the earliest is the end of block #%d, %s away. Nothing needs renewing yet",
			result.EarliestDue, describeBlocksAway(result.EarliestDue-tipHeight))
	}
	for _, group := range groups {
		tx, err := BuildSignedRenewal(privateKeyHex, publicKeyHex, feeUnits, group)
		if err != nil {
			return result, err
		}
		if err := SubmitTransferOverHTTP(gateway, tx); err != nil {
			return result, err
		}
		result.Transfers = append(result.Transfers, tx)
		result.CoinsRenewed += len(group)
		for _, coin := range group {
			result.TakenBackUnits += coin.HeldUnits
			if feeUnits == 0 && heightKnown && coin.SweepAt-tipHeight <= renewUrgentBlocks {
				result.CloseToDate = true
			}
		}
	}
	return result, nil
}
