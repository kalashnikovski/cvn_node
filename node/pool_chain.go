package node

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
)

// THE POOL'S OWN TRANSFERS, READ FROM THE CHAIN
//
// Every payment a pool makes is a transfer from the pool's address, and the
// chain holds them all. The record should know each of them, and there are
// two ways it may not:
//
//   - The record was put back from an older copy (pool_backup.go). It knows
//     nothing of the payments made since that copy.
//   - Somebody with the pool's key sent a transfer by hand, from a wallet.
//
// A record that does not know of a payment still shows its amounts as owed,
// and would pay them a second time. So the pool reads every block for
// transfers sent from its own address, and does two things with one its
// record does not know:
//
//   - At once: it prepares no payment while such a transfer waits.
//   - When the transfer is too deep in the chain to be replaced: it takes
//     the transfer into the record as a payment that has settled. What each
//     address received, with its share of the fee, comes off what the
//     record says that address is owed. The coins the transfer spent leave
//     the record's list, and what it sent back to the pool's address joins
//     it.
//
// An address may have been paid more than the record says it was owed. That
// is what an older copy looks like: the address earned the difference from
// blocks found after the copy was made, and their division is not in the
// copy. What it is owed then becomes nothing, never less than nothing, and
// the difference is written down with the payment ("beyond"). Nobody is
// asked to give anything back, and nobody is paid twice.
//
// What the record cannot learn from the chain is who did the work for a
// block. Blocks found after the copy was made pay the pool's address like
// any other, and the record does not know whose they are. Their rewards
// stay at the pool's address, outside the record's list of coins, for the
// pool's operator to settle by hand.

var (
	poolMetaBucket = []byte("meta")
	poolReadToKey  = []byte("read_to")
)

// poolReadToFrom reads how far the chain had been read from a record's file.
func poolReadToFrom(meta *bbolt.Bucket) int64 {
	height, err := strconv.ParseInt(string(meta.Get(poolReadToKey)), 10, 64)
	if err != nil || height < 0 {
		return 0
	}
	return height
}

// poolUnknownTransfer is a transfer the pool's address sent that is not in
// the record.
type poolUnknownTransfer struct {
	Tx     Transaction
	Height int64
	Block  string // the block it is in
}

// knowsTransfer reports whether a transfer belongs to one of the record's
// payments.
func (r *poolRecord) knowsTransfer(id string) bool {
	r.Lock()
	defer r.Unlock()
	_, known := r.paymentTx[id]
	return known
}

// unknownTransfers returns the transfers that wait to be taken into the
// record, the lowest in the chain first.
func (r *poolRecord) unknownTransfers() []poolUnknownTransfer {
	r.Lock()
	defer r.Unlock()
	waiting := make([]poolUnknownTransfer, 0, len(r.unknown))
	for _, transfer := range r.unknown {
		waiting = append(waiting, transfer)
	}
	sort.Slice(waiting, func(i, j int) bool {
		if waiting[i].Height != waiting[j].Height {
			return waiting[i].Height < waiting[j].Height
		}
		return waiting[i].Tx.ID < waiting[j].Tx.ID
	})
	return waiting
}

// readFrom is the first height the chain is to be read from now: the first
// whose block is not the one read before. `hashAt` gives the hash of the
// block the chain holds at a height. A record opened a moment ago has read
// nothing in this run, and goes back `depth` blocks from where its file says
// it had got to: any of those may have been replaced since, and a transfer
// that was waiting there was kept in memory only.
func (r *poolRecord) readFrom(tip, depth int64, hashAt func(int64) string) int64 {
	r.Lock()
	defer r.Unlock()
	if !r.readBegun {
		from := r.readTo - depth + 1
		// A file that says the chain was read further than the chain goes
		// is wrong, by damage or by somebody's hand, and is not believed:
		// everything since the record's first block is read again (review
		// of the pool, finding 3).
		if r.readTo == 0 || r.readTo > tip {
			// A record that has never read the chain. If it holds blocks
			// it is older than this reading, and everything since its
			// first block is read; a new record starts from now.
			// From the first block in the file, not only the first of those
			// kept in memory (review of the pool, round 3, finding 2).
			from = tip + 1
			if r.firstBlock > 0 && r.firstBlock < from {
				from = r.firstBlock
			}
			for _, block := range r.blocks {
				if block.Height < from {
					from = block.Height
				}
			}
		}
		if from < 1 {
			from = 1
		}
		return from
	}
	height := r.readMemory
	if height > tip {
		height = tip
	}
	for height >= 1 {
		read, remembered := r.readBlocks[height]
		if !remembered || read == hashAt(height) {
			break
		}
		height--
	}
	return height + 1
}

// noteRead writes down what a reading of the chain from `from` to `tip`
// found: the block at each height, and the transfers from the pool's address
// that the record does not know. Whatever was waiting at those heights from
// an earlier reading is dropped first; if it is still in the chain, it has
// just been found again.
func (r *poolRecord) noteRead(from, tip, depth int64, hashes map[int64]string, found []poolUnknownTransfer) error {
	r.Lock()
	defer r.Unlock()
	if r.db == nil {
		return fmt.Errorf("the pool's record is closed")
	}
	if r.unknown == nil {
		r.unknown = make(map[string]poolUnknownTransfer)
	}
	if r.readBlocks == nil {
		r.readBlocks = make(map[int64]string)
	}
	for id, transfer := range r.unknown {
		if transfer.Height >= from {
			delete(r.unknown, id)
		}
	}
	for _, transfer := range found {
		if _, known := r.paymentTx[transfer.Tx.ID]; !known {
			r.unknown[transfer.Tx.ID] = transfer
		}
	}
	for height := range r.readBlocks {
		if height >= from || height < tip-2*depth {
			delete(r.readBlocks, height)
		}
	}
	for height, hash := range hashes {
		r.readBlocks[height] = hash
	}
	r.readBegun, r.readMemory = true, tip
	return r.saveReadToLocked(tip)
}

// saveReadToLocked writes how far the chain has been read into the file. It
// never says more than is safe to start again from: while a transfer waits
// to be taken in, the file says the chain was read to just below it.
func (r *poolRecord) saveReadToLocked(tip int64) error {
	readTo := tip
	for _, transfer := range r.unknown {
		if transfer.Height-1 < readTo {
			readTo = transfer.Height - 1
		}
	}
	if readTo < 0 {
		readTo = 0
	}
	if readTo == r.readTo {
		return nil
	}
	if err := r.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(poolMetaBucket).Put(poolReadToKey, []byte(strconv.FormatInt(readTo, 10)))
	}); err != nil {
		return err
	}
	r.readTo = readTo
	return nil
}

// poolSentBy reports whether a transaction in a block is a transfer sent
// from an address. A valid transfer names its sender in its sender field.
func poolSentBy(transaction Transaction, address string) bool {
	return len(transaction.Inputs) > 0 && strings.TrimSpace(transaction.Witness) == address
}

// poolChainReading lets one reading of the chain run at a time: the pool
// reads it every few seconds, and again whenever payments are asked for.
var poolChainReading sync.Mutex

// poolReadChain reads the blocks the pool has not read yet, takes note of
// transfers from the pool's address that its record does not know, and takes
// into the record those that are deep enough. It returns how many it took
// in, and whether the chain has now been read to its newest block.
func poolReadChain() (takenIn int, readToTip bool) {
	record := poolLedger
	if record == nil || GlobalBoltEngine == nil || atomic.LoadInt32(&IsSyncingActive) == 1 {
		return 0, false
	}
	poolChainReading.Lock()
	defer poolChainReading.Unlock()
	StateMutex.RLock()
	tip := GetLatestBlockInternal().Index
	StateMutex.RUnlock()
	depth := poolConfirmDepth()
	from := record.readFrom(tip, depth, func(height int64) string { return GetBlockByHeightFromDB(height).Hash })
	hashes := make(map[int64]string)
	var found []poolUnknownTransfer
	// Each block read must follow from the one before it. If the chain
	// changes while it is being read, the reading is thrown away and done
	// again next time (review of the pool, round 3, finding 3).
	previous := ""
	if from > 1 {
		previous = GetBlockByHeightFromDB(from - 1).Hash
	}
	for height := from; height <= tip; height++ {
		// A download changing the chain under this reading would leave it
		// half of one chain and half of another. It is done again later.
		if atomic.LoadInt32(&IsSyncingActive) == 1 {
			return 0, false
		}
		block := GetBlockByHeightFromDB(height)
		if block.Hash == "" || block.Index != height {
			return 0, false
		}
		if previous != "" && block.PrevHash != previous {
			return 0, false
		}
		previous = block.Hash
		hashes[height] = block.Hash
		for _, transaction := range block.Transactions {
			if poolSentBy(transaction, poolAddress) && !record.knowsTransfer(transaction.ID) {
				found = append(found, poolUnknownTransfer{Tx: transaction, Height: height, Block: block.Hash})
			}
		}
	}
	before := len(record.unknownTransfers())
	if err := record.noteRead(from, tip, depth, hashes, found); err != nil {
		announceNetworkStatus("⚠️ [POOL] The pool's record could not note how far it has read the chain: " + err.Error())
		return 0, false
	}
	waiting := record.unknownTransfers()
	if len(waiting) > before {
		announceNetworkStatus(fmt.Sprintf("⚠️ [POOL] The pool's address has sent %d transfer(s) that the pool's record does not know of, the first in block #%d. "+
			"No payment is prepared until each is %d blocks deep and has been taken into the record, so that nobody is paid twice.", len(waiting), waiting[0].Height, depth))
	}
	// A block the pool found that is deep enough to count, and has not been
	// counted yet, comes first. A payment found in the chain may have paid
	// for it; counted afterwards, its parts would be owed a second time.
	for _, block := range record.waitingBlocks() {
		if len(waiting) > 0 && tip >= block.Height+depth {
			return 0, true
		}
	}
	for _, transfer := range waiting {
		if tip < transfer.Height+depth || GetBlockByHeightFromDB(transfer.Height).Hash != transfer.Block {
			continue
		}
		payment, taken, err := record.adoptTransfer(transfer, poolAddress, tip, time.Now().Unix())
		if err != nil {
			announceNetworkStatus(fmt.Sprintf("⚠️ [POOL] Transfer %s could not be taken into the pool's record: %v", transfer.Tx.ID, err))
			continue
		}
		if taken {
			takenIn++
			var beyond int64
			for _, line := range payment.Lines {
				beyond += line.Beyond
			}
			message := fmt.Sprintf("📒 [POOL] Transfer %s, sent from the pool's address in block #%d, was not in the pool's record and has been taken into it as payment %d: %s CVN to %d address(es).",
				transfer.Tx.ID, transfer.Height, payment.Serial, FormatUnits(payment.debits()), len(payment.Lines))
			if beyond > 0 {
				message += fmt.Sprintf(" %s CVN of it was more than the record said those addresses were owed.", FormatUnits(beyond))
			}
			announceNetworkStatus(message)
		}
	}
	if takenIn > 0 {
		poolBackupSoon()
	}
	return takenIn, true
}

// adoptTransfer takes into the record a transfer the pool's address sent and
// the record did not know: as a payment that has settled. The transfer must
// be too deep in the chain to be replaced. All of it is written in one step.
func (r *poolRecord) adoptTransfer(found poolUnknownTransfer, poolsAddress string, chainHeight, now int64) (poolPayment, bool, error) {
	r.Lock()
	defer r.Unlock()
	if r.db == nil {
		return poolPayment{}, false, fmt.Errorf("the pool's record is closed")
	}
	transfer := found.Tx
	if _, known := r.paymentTx[transfer.ID]; known {
		delete(r.unknown, transfer.ID)
		return poolPayment{}, false, nil
	}

	accounts := make(map[string]poolAccount)
	account := func(address string) poolAccount {
		if changed, touched := accounts[address]; touched {
			return changed
		}
		return r.accounts[address]
	}
	coins := make(map[string]poolCoin) // coins changed or added
	spent := make(map[string]bool)     // coins this transfer spent
	for _, input := range transfer.Inputs {
		spent[utxoKey(input.SourceTxID, input.Index)] = true
	}

	// Payments under way that this transfer overtakes. One that was
	// prepared and not signed gives back its amounts and coins, as it does
	// when its time runs out. One that was sent and spends a coin this
	// transfer has spent can never be mined now, and gives them back too:
	// the one case in which a payment that was sent is undone.
	var undone []poolPayment
	for _, payment := range r.payments {
		if payment.State != poolPaymentPrepared && payment.State != poolPaymentSent {
			continue
		}
		overtaken := payment.State == poolPaymentPrepared
		for _, input := range payment.Inputs {
			if spent[utxoKey(input.SourceTxID, input.Index)] {
				overtaken = true
			}
		}
		if !overtaken {
			continue
		}
		payment.State, payment.DecidedAtHeight, payment.Tx = poolPaymentReturned, chainHeight, nil
		payment.MinedAtHeight, payment.MinedInBlock = 0, ""
		for _, line := range payment.Lines {
			changed := account(line.Address)
			changed.Pending -= line.Debit
			changed.Owed += line.Debit
			accounts[line.Address] = changed
		}
		for _, input := range payment.Inputs {
			key := utxoKey(input.SourceTxID, input.Index)
			if coin, known := r.coins[key]; known && coin.HeldBy == payment.Serial {
				coin.HeldBy = 0
				coins[key] = coin
			}
		}
		undone = append(undone, payment)
	}

	// The transfer as a payment: one line for each output to another
	// address, in order. What it sent to the pool's own address is the
	// pool's again.
	taken := poolPayment{
		Serial: r.nextPayment, State: poolPaymentSettled, FromChain: true,
		PreparedAt: now, SentAt: now, SentAtHeight: found.Height,
		FeeUnits: transfer.FreeWillOffering.Units(), TxID: transfer.ID,
		MinedAtHeight: found.Height, MinedInBlock: found.Block, DecidedAtHeight: chainHeight,
		Outputs: transfer.Outputs,
	}
	for _, input := range transfer.Inputs {
		taken.Inputs = append(taken.Inputs, UTXOInput{SourceTxID: input.SourceTxID, Index: input.Index, AmountUnits: input.AmountUnits})
	}
	for index, output := range transfer.Outputs {
		if strings.TrimSpace(output.Recipient) == poolsAddress {
			taken.ChangeUnits += output.Amount.Units()
			coin := poolCoin{TxID: transfer.ID, Index: index, Units: output.Amount.Units()}
			coins[coin.key()] = coin
			continue
		}
		taken.Lines = append(taken.Lines, poolPaymentLine{Address: strings.TrimSpace(output.Recipient), Receives: output.Amount.Units()})
	}
	// The fee falls on the addresses paid, as it does in a payment the pool
	// prepares: equally, the units that do not divide on the first.
	if count := int64(len(taken.Lines)); count > 0 {
		share, odd := taken.FeeUnits/count, taken.FeeUnits%count
		for i := range taken.Lines {
			fee := share
			if int64(i) < odd {
				fee++
			}
			line := &taken.Lines[i]
			line.Debit = line.Receives + fee
			changed := account(line.Address)
			comesOff := line.Debit
			if comesOff > changed.Owed {
				comesOff = changed.Owed
			}
			if comesOff < 0 {
				comesOff = 0
			}
			line.Beyond = line.Debit - comesOff
			changed.Owed -= comesOff
			changed.Earned += line.Beyond
			changed.Paid += line.Debit
			accounts[line.Address] = changed
		}
	}

	if err := r.db.Update(func(tx *bbolt.Tx) error {
		payments := tx.Bucket(poolPaymentsBucket)
		for _, payment := range undone {
			if err := putJSON(payments, poolSeqKey(payment.Serial), payment); err != nil {
				return err
			}
		}
		if err := putJSON(payments, poolSeqKey(taken.Serial), taken); err != nil {
			return err
		}
		bucket := tx.Bucket(poolCoinsBucket)
		for key := range spent {
			if err := bucket.Delete([]byte(key)); err != nil {
				return err
			}
		}
		for key, coin := range coins {
			if spent[key] {
				continue
			}
			if err := putJSON(bucket, []byte(key), coin); err != nil {
				return err
			}
		}
		return writeAccounts(tx, accounts)
	}); err != nil {
		return poolPayment{}, false, err
	}

	for _, payment := range undone {
		if index := r.findPaymentLocked(payment.Serial); index >= 0 {
			r.payments[index] = payment
		}
	}
	r.payments = append(r.payments, taken)
	r.nextPayment++
	r.paymentsSettled++
	r.paymentTx[taken.TxID] = taken.Serial
	for key := range spent {
		delete(r.coins, key)
	}
	for key, coin := range coins {
		if !spent[key] {
			r.coins[key] = coin
		}
	}
	for address, changed := range accounts {
		r.accounts[address] = changed
	}
	delete(r.unknown, transfer.ID)
	r.forgetOldPaymentsLocked()
	_ = r.saveReadToLocked(r.readMemory)
	return taken, true, nil
}
