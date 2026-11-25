package node

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// RENEW MY COINS
//
// A coin that has not been spent for seven years has a fifth of its value
// moved to the vault (see sweep.go). An owner who still holds the key loses
// nothing: spending the coin, even to its own address, makes a new coin with
// a new seven-year count. Until now a wallet showed when a coin's date was
// coming and left its owner to work out what to do about it.
//
// Renewing does it in one step. It sends the coins whose date is near back
// to their own address, in groups, each group becoming one new coin. Like
// combining, it is nothing special to the network: every group is an
// ordinary transfer from an address to itself. Unlike combining, a single
// coin can be renewed on its own.

// renewHorizonBlocks is how near a coin's sweep date must be for renewing to
// include it: one year of blocks. A coin further off than that is left
// alone unless the owner asks for everything to be renewed.
const renewHorizonBlocks = int64(365 * 24 * 3600 / 45)

// RenewResult describes what a renew action did.
type RenewResult struct {
	Address      string
	CoinsHeld    int   // spendable coins in the wallet before
	CoinsRenewed int   // coins sent back to their own address
	EarliestDue  int64 // the earliest sweep block among the wallet's coins before (0 if none)
	Transfers    []Transaction
	// NoteProblem is set when the transfers were sent but the wallet could
	// not write its note of them (see renew_record.go). The renewal itself
	// is not affected.
	NoteProblem error
	// SentAgain is true when this was the second try of a renewal that had
	// not gone through, and not an ordinary renewal.
	SentAgain bool
}

// Summary is one sentence for the person using the wallet.
func (r RenewResult) Summary() string {
	text := fmt.Sprintf("Renewed %d coin(s) with %d transfer(s) to %s. Once they confirm in the next blocks each is a new coin with a new seven-year count. Wait for that before sending: until then these coins are not ready to spend.",
		r.CoinsRenewed, len(r.Transfers), r.Address)
	if len(r.Transfers) == combineMaxTransfers {
		text += " There may be more to renew: run it again after that."
	}
	if r.SentAgain {
		text += " Only the renewal that had not gone through was sent again. If other coins are near their date, renew once more for those."
	}
	if r.NoteProblem != nil {
		text += fmt.Sprintf(" (The wallet could not write its note of this renewal in %s: %v. The renewal is not affected; the wallet will only be unable to remind you if it does not go through.)", renewalRecordPath(), r.NoteProblem)
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
// horizonBlocks of tipHeight (every coin with a sweep date when
// horizonBlocks is zero or less), soonest first.
func planRenewal(coins []SpendableCoin, tipHeight, horizonBlocks int64) [][]SpendableCoin {
	due := make([]SpendableCoin, 0, len(coins))
	for _, coin := range coins {
		if !coin.Amount.Spendable() || coin.SweepAt <= 0 {
			continue
		}
		if horizonBlocks > 0 && coin.SweepAt-tipHeight > horizonBlocks {
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
	var tipHeight int64
	if everything {
		horizon = 0
	} else {
		height, ok := walletChainHeight()
		if !ok {
			return result, fmt.Errorf("no node could say what the newest block is, so the wallet cannot tell which coins are near their date")
		}
		tipHeight = height
	}
	groups := planRenewal(coins, tipHeight, horizon)
	if len(groups) == 0 {
		return result, fmt.Errorf("no coin of this wallet is within a year of its date: the earliest is the end of block #%d, %s away. Nothing needs renewing yet",
			result.EarliestDue, describeBlocksAway(result.EarliestDue-tipHeight))
	}
	var stopped error
	for _, group := range groups {
		tx, err := BuildSignedRenewal(privateKeyHex, publicKeyHex, feeUnits, group)
		if err == nil {
			err = SubmitTransferOverHTTP(gateway, tx)
		}
		if err != nil {
			stopped = err
			break
		}
		result.Transfers = append(result.Transfers, tx)
		result.CoinsRenewed += len(group)
	}
	// A renewal sent with no fee is written down, so that the wallet can
	// say so if it has not gone through some days later, and offer to send
	// it again with a small fee (renew_record.go).
	if feeUnits == 0 && len(result.Transfers) > 0 {
		result.NoteProblem = noteRenewalSent(ownerAddress, result.Transfers, 0, time.Now())
	}
	return result, stopped
}
