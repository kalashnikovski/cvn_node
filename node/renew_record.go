package node

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A RENEWAL THAT DID NOT GO THROUGH
//
// A wallet renews its coins with transfers that pay no fee. A fifth of every
// block is kept from transfers that pay, so a free transfer normally has
// room. But no rule can make a miner take one, and on a crowded network a
// free renewal may wait. Nodes forget a transfer that has waited two hours
// (MempoolMaxAge). Nothing is lost when that happens: the coins are exactly
// as they were. Their owner is no better off either, and may not know.
//
// So the wallet remembers the renewals it sends free: the address, the
// time, and which coins they spent. When it next looks at that address,
// and three days or more have passed, and any of those coins is still
// unspent, it says so and offers to send the renewal again with a small
// fee. A transfer that pays goes into the larger part of a block, highest
// fee first. A second try is remembered in the same way, with its fee.
//
// The record is a small file in the folder the wallet is started in, where
// a saved key is also kept. It holds nothing secret: an address and the
// names of coins, all of which are on the chain. The wallet works the same
// without it, only without the reminder.
//
// Looking never changes the record. A coin a node does not list may be
// spent, or tied up in a waiting transfer, or the node may be starting up
// or behind; the wallet cannot tell which, so it only leaves such a coin
// out of what it offers. A note is dropped when it is a month old.

// renewalRecordFile is where the wallet keeps its notes of renewals. A
// variable so that tests can keep theirs in a folder of their own.
var renewalRecordFile = "wallet_renewals.json"

const (
	// renewalRetryAfter is how long a renewal is given before the wallet
	// offers to send it again with a fee.
	renewalRetryAfter = 3 * 24 * time.Hour

	// renewalNoteLife is how long a note is kept. After that the wallet
	// stops offering, and the note goes when the record is next written.
	renewalNoteLife = 30 * 24 * time.Hour

	// renewalRetryFeeUnits is the fee the wallet proposes for each transfer
	// of a second try: 0.001 CVN. The owner may name another.
	renewalRetryFeeUnits = int64(100000)

	// The record is kept small whatever happens to it: this many notes and
	// this many coins in all at most (the oldest notes go first), and no
	// more of the file than renewalRecordMaxSize is ever read. A coin's
	// name takes some 75 bytes, so a record within these limits is always
	// small enough to be read back.
	renewalNotesKept     = 200
	renewalCoinsKept     = 40000
	renewalRecordMaxSize = 4 << 20
)

// renewalNote is one renewal a wallet sent.
type renewalNote struct {
	Address string `json:"address"`
	// Sent is when, in seconds since 1970.
	Sent int64 `json:"sent"`
	// FeeUnits is the fee each of its transfers paid: nought for a renewal
	// sent free, which is the usual kind.
	FeeUnits int64 `json:"fee,omitempty"`
	// Coins are the coins the renewal spent, each written as its
	// transaction and its place in it ("TX_..._0").
	Coins []string `json:"coins"`
}

// renewalRecordPath is the record's full path, for messages to the owner.
func renewalRecordPath() string {
	if full, err := filepath.Abs(renewalRecordFile); err == nil {
		return full
	}
	return renewalRecordFile
}

// readRenewalRecord reads the record. `unreadable` is true when a file is
// there and is not a record this program can read: damaged, too large, or
// held by another program.
func readRenewalRecord() (notes []renewalNote, unreadable bool) {
	file, err := os.Open(renewalRecordFile)
	if err != nil {
		return nil, !os.IsNotExist(err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, renewalRecordMaxSize+1))
	if err != nil || len(raw) > renewalRecordMaxSize {
		return nil, true
	}
	if json.Unmarshal(raw, &notes) != nil {
		return nil, true
	}
	kept := notes[:0]
	for _, note := range notes {
		if note.Address != "" && note.Sent > 0 && note.FeeUnits >= 0 && len(note.Coins) > 0 {
			kept = append(kept, note)
		}
	}
	return kept, false
}

// loadRenewalNotes is the notes in the record. A record that is missing or
// cannot be read gives none: the reminder is a convenience, and must never
// stand between an owner and their wallet.
func loadRenewalNotes() []renewalNote {
	notes, _ := readRenewalRecord()
	return notes
}

// saveRenewalNotes writes the record: the newest notes, within the limits
// above. It is written under another name first and then put in place, so
// that a failure half-way leaves the old record whole.
func saveRenewalNotes(notes []renewalNote) error {
	if len(notes) == 0 {
		if err := os.Remove(renewalRecordFile); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].Sent < notes[j].Sent })
	if len(notes) > renewalNotesKept {
		notes = notes[len(notes)-renewalNotesKept:]
	}
	// From the newest back, as many notes as keep the coins within the
	// limit; always the newest note itself.
	first, coins := len(notes)-1, len(notes[len(notes)-1].Coins)
	for first > 0 && coins+len(notes[first-1].Coins) <= renewalCoinsKept {
		first--
		coins += len(notes[first].Coins)
	}
	notes = notes[first:]
	raw, err := json.MarshalIndent(notes, "", " ")
	if err != nil {
		return err
	}
	partial := renewalRecordFile + ".writing"
	if err := os.WriteFile(partial, append(raw, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(partial, renewalRecordFile); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return nil
}

// noteRenewalSent writes down that these transfers, each paying feeUnits,
// were sent for this address just now. Coins named in an earlier note of
// the same address are taken out of it: the newest sending is the one that
// counts. Notes a month old are dropped on the way.
//
// A record that is there and cannot be read is not written over. It is set
// aside under another name first, so that nothing is thrown away without a
// trace; if it cannot be set aside, nothing is written and the error says
// so.
func noteRenewalSent(address string, transfers []Transaction, feeUnits int64, now time.Time) error {
	address = strings.TrimSpace(address)
	spent := make(map[string]bool)
	note := renewalNote{Address: address, Sent: now.Unix(), FeeUnits: feeUnits}
	for _, transfer := range transfers {
		for _, input := range transfer.Inputs {
			key := utxoKey(input.SourceTxID, input.Index)
			if !spent[key] {
				spent[key] = true
				note.Coins = append(note.Coins, key)
			}
		}
	}
	if address == "" || len(note.Coins) == 0 {
		return nil
	}
	earlierNotes, unreadable := readRenewalRecord()
	if unreadable {
		aside := renewalRecordFile + ".unreadable"
		if err := os.Rename(renewalRecordFile, aside); err != nil {
			return fmt.Errorf("the record that is there cannot be read, and could not be set aside as %s: %v", aside, err)
		}
	}
	var notes []renewalNote
	for _, earlier := range earlierNotes {
		if now.Sub(time.Unix(earlier.Sent, 0)) >= renewalNoteLife {
			continue
		}
		if earlier.Address == address {
			earlier.Coins = withoutCoins(earlier.Coins, spent)
			if len(earlier.Coins) == 0 {
				continue
			}
		}
		notes = append(notes, earlier)
	}
	return saveRenewalNotes(append(notes, note))
}

// withoutCoins is the list less the coins named.
func withoutCoins(coins []string, leaveOut map[string]bool) []string {
	var kept []string
	for _, coin := range coins {
		if !leaveOut[coin] {
			kept = append(kept, coin)
		}
	}
	return kept
}

// renewalWaiting is a renewal, or several, that this wallet sent some days
// ago and whose coins are still unspent.
type renewalWaiting struct {
	Coins     []SpendableCoin // the coins, as a node has just listed them
	FirstSent time.Time       // when the earliest of those renewals was sent
	FeeUnits  int64           // the highest fee any of them paid for each transfer
}

// renewalsNotGoneThrough looks through the notes of renewals this wallet
// sent for an address at least renewalRetryAfter ago (and less than
// renewalNoteLife ago), and returns the coins among them that are still
// unspent and can be renewed. `unspent` must be the address's coins as a
// node has just listed them. It reads the record and never changes it.
func renewalsNotGoneThrough(address string, unspent []SpendableCoin, now time.Time) renewalWaiting {
	var waiting renewalWaiting
	address = strings.TrimSpace(address)
	if address == "" {
		return waiting
	}
	held := make(map[string]SpendableCoin, len(unspent))
	for _, coin := range unspent {
		// Only a coin that a renewal can spend: one with an amount and a
		// seven-year date.
		if coin.Amount.Spendable() && coin.SweepAt > 0 {
			held[utxoKey(coin.TxID, coin.Index)] = coin
		}
	}
	listed := make(map[string]bool)
	for _, note := range loadRenewalNotes() {
		age := now.Sub(time.Unix(note.Sent, 0))
		if note.Address != address || age < renewalRetryAfter || age >= renewalNoteLife {
			continue
		}
		for _, key := range note.Coins {
			coin, unspentStill := held[key]
			if !unspentStill || listed[key] {
				continue
			}
			listed[key] = true
			waiting.Coins = append(waiting.Coins, coin)
			if sent := time.Unix(note.Sent, 0); waiting.FirstSent.IsZero() || sent.Before(waiting.FirstSent) {
				waiting.FirstSent = sent
			}
			if note.FeeUnits > waiting.FeeUnits {
				waiting.FeeUnits = note.FeeUnits
			}
		}
	}
	return waiting
}

// any reports whether there is anything to send again.
func (w renewalWaiting) any() bool { return len(w.Coins) > 0 }

// transfers is how many transfers a second try sends at once: one for every
// TxMaxInputs coins, and no more than one renewing sends.
func (w renewalWaiting) transfers() int {
	count := (len(w.Coins) + TxMaxInputs - 1) / TxMaxInputs
	if count > combineMaxTransfers {
		count = combineMaxTransfers
	}
	return count
}

// proposedFeeUnits is the fee the wallet proposes for each transfer of a
// second try: the small one, or twice what the last try paid if that is
// more, since the same fee has already been passed over.
func (w renewalWaiting) proposedFeeUnits() int64 {
	if doubled := 2 * w.FeeUnits; doubled > renewalRetryFeeUnits && doubled <= MaxAmountUnits {
		return doubled
	}
	return renewalRetryFeeUnits
}

// describe says, for the owner, that a renewal has not gone through.
func (w renewalWaiting) describe(now time.Time) string {
	days := int(now.Sub(w.FirstSent) / (24 * time.Hour))
	text := fmt.Sprintf("A renewal of %d coin(s) sent %d days ago, on %s, has not gone through. Nothing is lost: the coins are as they were. ",
		len(w.Coins), days, w.FirstSent.Format("2 January 2006"))
	if w.FeeUnits == 0 {
		return text + "It was sent with no fee, and a miner does not have to take a transfer that pays nothing. It can be sent again with a small fee, which a miner has more reason to take"
	}
	return text + fmt.Sprintf("It was sent with a fee of %s CVN for each transfer, and no miner took it in that time. It can be sent again with a larger fee", FormatUnits(w.FeeUnits))
}

// question asks whether to send it again at a given fee, saying how many
// transfers that is and what they cost together.
func (w renewalWaiting) question(feeUnits int64) string {
	return fmt.Sprintf("Send it again as %d transfer(s) with a fee of %s CVN each, %s CVN in all?",
		w.transfers(), FormatUnits(feeUnits), FormatUnits(feeUnits*int64(w.transfers())))
}

// RenewAgainFromWallet sends again, with a fee, the renewals of this wallet
// that have not gone through after renewalRetryAfter. feeUnits is paid for
// each transfer; nought or less means the fee the wallet proposes (see
// proposedFeeUnits). It fails, sending nothing, if no such renewal is
// waiting. Coins near their date that were in no such renewal are not
// touched: that is what renewing is for.
func RenewAgainFromWallet(privateKeyHex, publicKeyHex string, feeUnits int64, now time.Time) (RenewResult, error) {
	privateKeyHex, publicKeyHex = strings.TrimSpace(privateKeyHex), strings.TrimSpace(publicKeyHex)
	ownerAddress, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		return RenewResult{}, err
	}
	ownerAddress = strings.TrimSpace(ownerAddress)
	coins, gateway, err := FetchSpendableCoinsOverHTTP(ownerAddress)
	if err != nil {
		return RenewResult{}, err
	}
	result := RenewResult{Address: ownerAddress, CoinsHeld: len(coins), EarliestDue: earliestSweepBlock(coins), SentAgain: true}
	waiting := renewalsNotGoneThrough(ownerAddress, coins, now)
	if !waiting.any() {
		return result, fmt.Errorf("no renewal of this wallet is waiting to be sent again")
	}
	if feeUnits <= 0 {
		feeUnits = waiting.proposedFeeUnits()
	}
	// Every such coin, soonest date first, in groups of the most one
	// transfer can spend.
	var stopped error
	for _, group := range planRenewal(waiting.Coins, 0, 0) {
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
	// What was sent is noted afresh, with its fee, so that the wallet asks
	// again only if this sending too has not gone through in three days.
	if len(result.Transfers) > 0 {
		result.NoteProblem = noteRenewalSent(ownerAddress, result.Transfers, feeUnits, now)
	}
	return result, stopped
}
