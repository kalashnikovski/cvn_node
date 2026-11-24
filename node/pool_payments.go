package node

import (
	"encoding/json"
	"fmt"
	"sort"

	"go.etcd.io/bbolt"
)

// PAYING OUT: WHAT THE RECORD KEEPS
//
// What an address is owed is paid by an ordinary transfer from the pool's
// address. The record keeps every payment and what stage it has reached, so
// that whatever happens to the program in between, nobody is paid twice and
// nobody's payment is forgotten.
//
// A payment goes through these states:
//
//	prepared  The pool has worked out who is paid what and which of its
//	          coins pay for it, and set both aside: the amounts are no
//	          longer owed but "on their way", and the coins are held. The
//	          transfer is not signed yet. This part of the program holds no
//	          key; it waits for the paying command to sign.
//	sent      The signed transfer came back, matched what was prepared, and
//	          was handed to the network. It is kept here in full and handed
//	          over again from time to time until it is in a block.
//	settled   The transfer is in a block with enough blocks on it that it
//	          cannot be replaced. The amounts are paid.
//	returned  The transfer was never signed (the paying command stopped, or
//	          was told no). The amounts are owed again and the coins free.
//
// A payment that was sent is never returned. The coins it spends are final
// (see below), and only the pool's key can spend them, so the transfer stays
// good until it is mined; making a second payment for the same amounts could
// only ever pay somebody twice. There is one exception, and it needs the
// pool's key used outside the pool: a transfer sent by hand that spends one
// of the same coins and is too deep in the chain to be replaced. The payment
// can then never be mined, and gives its amounts back (pool_chain.go).
//
// THE POOL'S COINS
//
// The record keeps its own list of the coins it may spend: the reward of
// each block that has come to count, and the change of each payment that
// has settled. Both are too deep in the chain to be replaced, so a payment
// built on them cannot be undone by the chain changing. Anything else that
// arrives at the pool's address is left alone.

const (
	poolPaymentPrepared = "prepared"
	poolPaymentSent     = "sent"
	poolPaymentSettled  = "settled"
	poolPaymentReturned = "returned"
)

// poolPaymentsKept is how many finished payments are kept in memory for the
// pool page. All of them stay in the file.
const poolPaymentsKept = 100

// poolPaymentLine is one address in a payment.
type poolPaymentLine struct {
	Address string `json:"address"`
	// Debit is what is taken from what the address was owed; Receives is
	// what reaches it, the debit less its share of the transfer's fee.
	Debit    int64 `json:"debit"`
	Receives int64 `json:"receives"`
	// Beyond is set only in a payment taken into the record from the chain
	// (pool_chain.go): how much of the debit was more than the record said
	// the address was owed.
	Beyond int64 `json:"beyond,omitempty"`
}

// poolPayment is one transfer paying up to 49 addresses.
type poolPayment struct {
	Serial       uint64            `json:"serial"`
	State        string            `json:"state"`
	PreparedAt   int64             `json:"prepared_at"`
	SentAt       int64             `json:"sent_at,omitempty"`
	SentAtHeight int64             `json:"sent_at_height,omitempty"`
	FeeUnits     int64             `json:"fee_units"`
	ChangeUnits  int64             `json:"change_units"`
	Lines        []poolPaymentLine `json:"lines"`
	// The transfer as prepared: the coins it spends and what it pays, one
	// output for each line in order and then the change, if there is any.
	Inputs  []UTXOInput  `json:"inputs"`
	Outputs []UTXOOutput `json:"outputs"`
	// Once signed: the transfer itself, kept to hand to the network again.
	TxID string       `json:"tx_id,omitempty"`
	Tx   *Transaction `json:"tx,omitempty"`
	// Where it was mined, once it was.
	MinedAtHeight   int64  `json:"mined_at_height,omitempty"`
	MinedInBlock    string `json:"mined_in_block,omitempty"`
	DecidedAtHeight int64  `json:"decided_at_height,omitempty"`
	// FromChain marks a payment the record did not make: a transfer from
	// the pool's address that was found in the chain and taken into the
	// record (pool_chain.go).
	FromChain bool `json:"from_chain,omitempty"`
}

// debits is what the payment takes from what its addresses were owed.
func (p poolPayment) debits() int64 {
	var total int64
	for _, line := range p.Lines {
		total += line.Debit
	}
	return total
}

// poolCoin is one coin the record may spend.
type poolCoin struct {
	TxID  string `json:"tx_id"`
	Index int    `json:"index"`
	Units int64  `json:"units"`
	// HeldBy is the serial of the payment that spends it, or 0 if it is free.
	HeldBy uint64 `json:"held_by,omitempty"`
}

func (c poolCoin) key() string { return utxoKey(c.TxID, c.Index) }

func putJSON(bucket *bbolt.Bucket, key []byte, value interface{}) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return bucket.Put(key, raw)
}

// writeAccountsLocked writes changed accounts within a database step.
func writeAccounts(tx *bbolt.Tx, accounts map[string]poolAccount) error {
	bucket := tx.Bucket(poolAccountsBucket)
	for address, account := range accounts {
		if err := putJSON(bucket, []byte(address), account); err != nil {
			return err
		}
	}
	return nil
}

// findPaymentLocked returns the place of a payment held in memory.
func (r *poolRecord) findPaymentLocked(serial uint64) int {
	for i := range r.payments {
		if r.payments[i].Serial == serial {
			return i
		}
	}
	return -1
}

// payment returns one payment.
func (r *poolRecord) payment(serial uint64) (poolPayment, bool) {
	r.Lock()
	defer r.Unlock()
	if i := r.findPaymentLocked(serial); i >= 0 {
		return r.payments[i], true
	}
	return poolPayment{}, false
}

// unsettledPayments are the payments still prepared or sent, oldest first.
func (r *poolRecord) unsettledPayments() []poolPayment {
	r.Lock()
	defer r.Unlock()
	var unsettled []poolPayment
	for _, payment := range r.payments {
		if payment.State == poolPaymentPrepared || payment.State == poolPaymentSent {
			unsettled = append(unsettled, payment)
		}
	}
	return unsettled
}

// recentPayments are the newest payments, newest first.
func (r *poolRecord) recentPayments(limit int) []poolPayment {
	r.Lock()
	defer r.Unlock()
	var recent []poolPayment
	for i := len(r.payments) - 1; i >= 0 && len(recent) < limit; i-- {
		recent = append(recent, r.payments[i])
	}
	return recent
}

// forgetOldPaymentsLocked keeps in memory every payment still under way and
// the newest finished ones.
func (r *poolRecord) forgetOldPaymentsLocked() {
	finished := 0
	for _, payment := range r.payments {
		if payment.State == poolPaymentSettled || payment.State == poolPaymentReturned {
			finished++
		}
	}
	if finished <= poolPaymentsKept {
		return
	}
	drop := finished - poolPaymentsKept
	kept := r.payments[:0:0]
	for _, payment := range r.payments {
		if drop > 0 && (payment.State == poolPaymentSettled || payment.State == poolPaymentReturned) {
			drop--
			continue
		}
		kept = append(kept, payment)
	}
	r.payments = kept
}

// poolPaymentPlan is how one run of payments is to be built.
type poolPaymentPlan struct {
	Now          int64
	PoolAddress  string
	MinimumUnits int64 // an address owed less than this waits
	FeeUnits     int64 // the fee of each transfer, shared among those it pays
	// Usable reports whether a coin the record holds is really there to be
	// spent: on the chain, the pool's, and not already in a waiting transfer.
	Usable func(poolCoin) bool
}

// preparePayments works out a run of payments and sets it aside: every
// address owed at least the minimum, in transfers of up to 49 addresses
// each, the most owed first, for as far as the pool's free coins reach. It
// returns the payments prepared and, if some or all of what is due could
// not be prepared, why.
func (r *poolRecord) preparePayments(plan poolPaymentPlan) ([]poolPayment, string, error) {
	r.Lock()
	defer r.Unlock()
	if r.db == nil {
		return nil, "", fmt.Errorf("the pool's record is closed")
	}
	if plan.FeeUnits < 0 || plan.MinimumUnits <= plan.FeeUnits+minimumOutputUnits() {
		return nil, "", fmt.Errorf("the minimum payment must be more than the fee and the smallest coin together")
	}

	type owing struct {
		address string
		units   int64
	}
	var due []owing
	for address, account := range r.accounts {
		if account.Owed >= plan.MinimumUnits {
			due = append(due, owing{address, account.Owed})
		}
	}
	if len(due) == 0 {
		return nil, fmt.Sprintf("nobody is owed %s CVN or more yet", FormatUnits(plan.MinimumUnits)), nil
	}
	sort.Slice(due, func(i, j int) bool {
		if due[i].units != due[j].units {
			return due[i].units > due[j].units
		}
		return due[i].address < due[j].address
	})

	var free []poolCoin
	for _, coin := range r.coins {
		if coin.HeldBy == 0 && coin.Units > 0 && (plan.Usable == nil || plan.Usable(coin)) {
			free = append(free, coin)
		}
	}
	sort.Slice(free, func(i, j int) bool {
		if free[i].Units != free[j].Units {
			return free[i].Units > free[j].Units
		}
		return free[i].key() < free[j].key()
	})

	const perTransfer = TxMaxOutputs - 1 // one output is kept for the change
	var prepared []poolPayment
	accounts := make(map[string]poolAccount)
	held := make(map[string]poolCoin)
	reason := ""
	serial := r.nextPayment
	for len(due) > 0 {
		group := due
		if len(group) > perTransfer {
			group = group[:perTransfer]
		}
		// Coins for the group, the largest first. If they do not reach, or
		// would leave change too small to be a coin, the address owed least
		// is left for the next run and the rest tried again.
		var chosen []poolCoin
		var need, gathered int64
		for len(group) > 0 {
			need, gathered, chosen = 0, 0, chosen[:0]
			for _, member := range group {
				need += member.units
			}
			for _, coin := range free {
				if gathered >= need && (gathered == need || gathered-need >= minimumOutputUnits()) {
					break
				}
				if len(chosen) >= TxMaxInputs {
					break
				}
				chosen = append(chosen, coin)
				gathered += coin.Units
			}
			if gathered == need || gathered-need >= minimumOutputUnits() {
				break
			}
			group = group[:len(group)-1]
		}
		if len(group) == 0 {
			reason = "the pool's address does not hold enough settled coins for everything that is owed; the rest is paid on a later run"
			break
		}

		payment := poolPayment{Serial: serial, State: poolPaymentPrepared, PreparedAt: plan.Now, FeeUnits: plan.FeeUnits, ChangeUnits: gathered - need}
		serial++
		// The fee is shared equally; the units that do not divide fall on
		// the first addresses, one each.
		share, odd := plan.FeeUnits/int64(len(group)), plan.FeeUnits%int64(len(group))
		for i, member := range group {
			fee := share
			if int64(i) < odd {
				fee++
			}
			payment.Lines = append(payment.Lines, poolPaymentLine{Address: member.address, Debit: member.units, Receives: member.units - fee})
			payment.Outputs = append(payment.Outputs, UTXOOutput{Recipient: member.address, Amount: UnitsAmount(member.units - fee)})
			account, touched := accounts[member.address]
			if !touched {
				account = r.accounts[member.address]
			}
			account.Owed -= member.units
			account.Pending += member.units
			accounts[member.address] = account
		}
		if payment.ChangeUnits > 0 {
			payment.Outputs = append(payment.Outputs, UTXOOutput{Recipient: plan.PoolAddress, Amount: UnitsAmount(payment.ChangeUnits)})
		}
		taken := make(map[string]bool, len(chosen))
		for _, coin := range chosen {
			payment.Inputs = append(payment.Inputs, UTXOInput{SourceTxID: coin.TxID, Index: coin.Index, AmountUnits: coin.Units})
			coin.HeldBy = payment.Serial
			held[coin.key()] = coin
			taken[coin.key()] = true
		}
		prepared = append(prepared, payment)

		remaining := free[:0:0]
		for _, coin := range free {
			if !taken[coin.key()] {
				remaining = append(remaining, coin)
			}
		}
		free = remaining
		due = due[len(group):]
		// Addresses left out of this group for want of coins are not tried
		// again in this run.
		if len(group) < perTransfer && len(due) > 0 && len(free) == 0 {
			reason = "the pool's address does not hold enough settled coins for everything that is owed; the rest is paid on a later run"
			break
		}
	}
	// Nobody could be paid, though the pool holds enough in all: its coins
	// are too many and too small for the 200 a transfer may spend. They are
	// combined first, in a transfer to the pool's own address with no fee
	// and nobody paid, and the payment follows on a later run (review of the
	// pool, round 2, finding 4).
	if len(prepared) == 0 && len(free) > 1 {
		var all int64
		for _, coin := range free {
			all += coin.Units
		}
		smallestDue := due[len(due)-1].units
		if all >= smallestDue {
			chosen := free
			if len(chosen) > TxMaxInputs {
				chosen = chosen[:TxMaxInputs]
			}
			combine := poolPayment{Serial: serial, State: poolPaymentPrepared, PreparedAt: plan.Now}
			serial++
			for _, coin := range chosen {
				combine.Inputs = append(combine.Inputs, UTXOInput{SourceTxID: coin.TxID, Index: coin.Index, AmountUnits: coin.Units})
				combine.ChangeUnits += coin.Units
				coin.HeldBy = combine.Serial
				held[coin.key()] = coin
			}
			combine.Outputs = []UTXOOutput{{Recipient: plan.PoolAddress, Amount: UnitsAmount(combine.ChangeUnits)}}
			prepared = append(prepared, combine)
			reason = "the pool's coins are too small to pay with; this run combines them, and the payments follow on a later run"
		}
	}
	if len(prepared) == 0 {
		return nil, reason, nil
	}

	if err := r.db.Update(func(tx *bbolt.Tx) error {
		payments, coins := tx.Bucket(poolPaymentsBucket), tx.Bucket(poolCoinsBucket)
		for _, payment := range prepared {
			if err := putJSON(payments, poolSeqKey(payment.Serial), payment); err != nil {
				return err
			}
		}
		for key, coin := range held {
			if err := putJSON(coins, []byte(key), coin); err != nil {
				return err
			}
		}
		return writeAccounts(tx, accounts)
	}); err != nil {
		return nil, "", err
	}
	r.nextPayment = serial
	r.payments = append(r.payments, prepared...)
	for key, coin := range held {
		r.coins[key] = coin
	}
	for address, account := range accounts {
		r.accounts[address] = account
	}
	return prepared, reason, nil
}

// returnPayment undoes a payment that was prepared and never sent: its
// amounts are owed again and its coins free. A payment in any other state is
// left as it is.
func (r *poolRecord) returnPayment(serial uint64, chainHeight int64) (bool, error) {
	r.Lock()
	defer r.Unlock()
	return r.returnPaymentLocked(serial, chainHeight)
}

func (r *poolRecord) returnPaymentLocked(serial uint64, chainHeight int64) (bool, error) {
	if r.db == nil {
		return false, fmt.Errorf("the pool's record is closed")
	}
	index := r.findPaymentLocked(serial)
	if index < 0 || r.payments[index].State != poolPaymentPrepared {
		return false, nil
	}
	payment := r.payments[index]
	payment.State, payment.DecidedAtHeight = poolPaymentReturned, chainHeight
	accounts := make(map[string]poolAccount)
	for _, line := range payment.Lines {
		account, touched := accounts[line.Address]
		if !touched {
			account = r.accounts[line.Address]
		}
		account.Pending -= line.Debit
		account.Owed += line.Debit
		accounts[line.Address] = account
	}
	freed := make(map[string]poolCoin)
	for _, input := range payment.Inputs {
		key := utxoKey(input.SourceTxID, input.Index)
		if coin, known := r.coins[key]; known && coin.HeldBy == serial {
			coin.HeldBy = 0
			freed[key] = coin
		}
	}
	if err := r.db.Update(func(tx *bbolt.Tx) error {
		if err := putJSON(tx.Bucket(poolPaymentsBucket), poolSeqKey(serial), payment); err != nil {
			return err
		}
		coins := tx.Bucket(poolCoinsBucket)
		for key, coin := range freed {
			if err := putJSON(coins, []byte(key), coin); err != nil {
				return err
			}
		}
		return writeAccounts(tx, accounts)
	}); err != nil {
		return false, err
	}
	r.payments[index] = payment
	for key, coin := range freed {
		r.coins[key] = coin
	}
	for address, account := range accounts {
		r.accounts[address] = account
	}
	r.forgetOldPaymentsLocked()
	return true, nil
}

// returnStalePayments returns every payment prepared before `before` and
// never signed, and reports their serials.
func (r *poolRecord) returnStalePayments(before, chainHeight int64) []uint64 {
	r.Lock()
	defer r.Unlock()
	var stale []uint64
	for _, payment := range r.payments {
		if payment.State == poolPaymentPrepared && payment.PreparedAt < before {
			stale = append(stale, payment.Serial)
		}
	}
	var returned []uint64
	for _, serial := range stale {
		if done, err := r.returnPaymentLocked(serial, chainHeight); err == nil && done {
			returned = append(returned, serial)
		}
	}
	return returned
}

// markSent records the signed transfer of a prepared payment. From here on
// the payment is never returned. It reports false if the payment was not
// waiting to be signed.
func (r *poolRecord) markSent(serial uint64, signed Transaction, now, chainHeight int64) (poolPayment, bool, error) {
	r.Lock()
	defer r.Unlock()
	if r.db == nil {
		return poolPayment{}, false, fmt.Errorf("the pool's record is closed")
	}
	index := r.findPaymentLocked(serial)
	if index < 0 || r.payments[index].State != poolPaymentPrepared {
		return poolPayment{}, false, nil
	}
	payment := r.payments[index]
	payment.State, payment.SentAt, payment.SentAtHeight = poolPaymentSent, now, chainHeight
	payment.TxID, payment.Tx = signed.ID, &signed
	if err := r.db.Update(func(tx *bbolt.Tx) error {
		return putJSON(tx.Bucket(poolPaymentsBucket), poolSeqKey(serial), payment)
	}); err != nil {
		return poolPayment{}, false, err
	}
	r.payments[index] = payment
	r.paymentTx[signed.ID] = serial
	return payment, true, nil
}

// noteMined records where a sent payment's transfer was mined, or, with a
// height of zero, that it is no longer there.
func (r *poolRecord) noteMined(serial uint64, height int64, blockHash string) error {
	r.Lock()
	defer r.Unlock()
	if r.db == nil {
		return fmt.Errorf("the pool's record is closed")
	}
	index := r.findPaymentLocked(serial)
	if index < 0 || r.payments[index].State != poolPaymentSent {
		return nil
	}
	payment := r.payments[index]
	payment.MinedAtHeight, payment.MinedInBlock = height, blockHash
	if err := r.db.Update(func(tx *bbolt.Tx) error {
		return putJSON(tx.Bucket(poolPaymentsBucket), poolSeqKey(serial), payment)
	}); err != nil {
		return err
	}
	r.payments[index] = payment
	return nil
}

// settlePayment records that a sent payment's transfer is too deep in the
// chain to be replaced. The amounts are paid; the coins it spent are gone
// from the record, and its change, if it left any, is a coin the pool may
// spend. All of it is written together or not at all.
func (r *poolRecord) settlePayment(serial uint64, chainHeight int64) (poolPayment, bool, error) {
	r.Lock()
	defer r.Unlock()
	if r.db == nil {
		return poolPayment{}, false, fmt.Errorf("the pool's record is closed")
	}
	index := r.findPaymentLocked(serial)
	if index < 0 || r.payments[index].State != poolPaymentSent || r.payments[index].MinedAtHeight <= 0 {
		return poolPayment{}, false, nil
	}
	payment := r.payments[index]
	payment.State, payment.DecidedAtHeight = poolPaymentSettled, chainHeight
	payment.Tx = nil // in the chain now; the identifier is enough
	accounts := make(map[string]poolAccount)
	for _, line := range payment.Lines {
		account, touched := accounts[line.Address]
		if !touched {
			account = r.accounts[line.Address]
		}
		account.Pending -= line.Debit
		account.Paid += line.Debit
		accounts[line.Address] = account
	}
	var change *poolCoin
	if payment.ChangeUnits > 0 {
		change = &poolCoin{TxID: payment.TxID, Index: len(payment.Lines), Units: payment.ChangeUnits}
	}
	if err := r.db.Update(func(tx *bbolt.Tx) error {
		if err := putJSON(tx.Bucket(poolPaymentsBucket), poolSeqKey(serial), payment); err != nil {
			return err
		}
		coins := tx.Bucket(poolCoinsBucket)
		for _, input := range payment.Inputs {
			if err := coins.Delete([]byte(utxoKey(input.SourceTxID, input.Index))); err != nil {
				return err
			}
		}
		if change != nil {
			if err := putJSON(coins, []byte(change.key()), *change); err != nil {
				return err
			}
		}
		return writeAccounts(tx, accounts)
	}); err != nil {
		return poolPayment{}, false, err
	}
	r.payments[index] = payment
	for _, input := range payment.Inputs {
		delete(r.coins, utxoKey(input.SourceTxID, input.Index))
	}
	if change != nil {
		r.coins[change.key()] = *change
	}
	for address, account := range accounts {
		r.accounts[address] = account
	}
	r.paymentsSettled++
	r.forgetOldPaymentsLocked()
	return payment, true, nil
}

// coinsHeld reports the pool's coins in the record: how many are free, how
// many held by a payment under way, and what each kind adds up to.
func (r *poolRecord) coinsHeld() (free, held int, freeUnits, heldUnits int64) {
	r.Lock()
	defer r.Unlock()
	for _, coin := range r.coins {
		if coin.HeldBy == 0 {
			free++
			freeUnits += coin.Units
		} else {
			held++
			heldUnits += coin.Units
		}
	}
	return free, held, freeUnits, heldUnits
}
