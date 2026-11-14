package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
)

// TRANSACTION RULES (hard fork)
//
// Before this fork a block was checked for its proof of work and its reward
// amount only. Transfers inside it were accepted unseen, and the block hash
// covered transaction IDs but not their contents.
//
// From TxForkActivationHeight on:
//
//  1. The block hash commits to the full contents of every transaction, so
//     nothing in a block (including who the reward pays) can be changed
//     without redoing the proof of work.
//  2. Every transfer must be signed by the key that owns the coins it spends,
//     may only spend coins that exist and are unspent, and may not pay out
//     more than it takes in.
//  3. A transfer's ID is the hash of its contents, so it cannot be forged or
//     replayed under another ID.
//  4. The miner may add the transfers' fees to its reward.
//
// Nodes that have not updated by the activation height will reject the new
// blocks and stay behind until they update.

// TxForkActivationHeight is the first block that must follow the rules above.
// A variable so tests can activate it early.
var TxForkActivationHeight = int64(6100)

const (
	TxMaxPerBlock      = 500
	TxMaxInputs        = 200
	TxMaxOutputs       = 50
	TxMaxAddressLength = 128
	TxAmountTolerance  = 0.00000001
	TxMaxAmount        = 2100000000.0

	CoinbasePublicKeyMarker = "GENESIS_VOID_REWARD_POOL"
	CoinbaseSignatureMarker = "COINBASE_IMMUTABLE_CONSENSUS_VALID"
)

var undoBucket = []byte("undo") // height -> coins that block spent, for rollbacks

// IsTxForkHeight reports whether a block at this height follows the
// transaction rules.
func IsTxForkHeight(height int64) bool {
	return height >= TxForkActivationHeight
}

// SpentRecord is one coin a block consumed. It is kept so that removing the
// block (a reorg) can put the coin back.
type SpentRecord struct {
	Key    string     `json:"key"`
	Record UTXORecord `json:"record"`
}

// utxoLookup answers "does this coin exist, and who owns it?".
type utxoLookup func(key string) (UTXORecord, bool)

func utxoKey(txID string, index int) string {
	return fmt.Sprintf("%s_%d", txID, index)
}

func formatAmountExact(amount float64) string {
	return strconv.FormatFloat(amount, 'g', -1, 64)
}

// writeField appends a length-prefixed field, so no field's content can be
// mistaken for a separator or for part of a neighbouring field.
func writeField(builder *strings.Builder, value string) {
	builder.WriteString(strconv.Itoa(len(value)))
	builder.WriteByte(':')
	builder.WriteString(value)
	builder.WriteByte('|')
}

// transactionSigningBody is everything a sender commits to: who pays, which
// coins are spent, who receives how much. It excludes the ID and signatures.
func transactionSigningBody(tx Transaction) string {
	var builder strings.Builder
	writeField(&builder, "CVN_TX_V1")
	writeField(&builder, strconv.FormatUint(uint64(tx.Version), 10))
	writeField(&builder, tx.PublicKey)
	writeField(&builder, strconv.FormatInt(tx.Timestamp, 10))
	writeField(&builder, formatAmountExact(tx.FreeWillOffering))
	writeField(&builder, tx.Witness)
	writeField(&builder, strconv.Itoa(len(tx.Inputs)))
	for _, input := range tx.Inputs {
		writeField(&builder, input.SourceTxID)
		writeField(&builder, strconv.Itoa(input.Index))
	}
	writeField(&builder, strconv.Itoa(len(tx.Outputs)))
	for _, output := range tx.Outputs {
		writeField(&builder, output.Recipient)
		writeField(&builder, formatAmountExact(output.Amount))
	}
	return builder.String()
}

// ComputeTransactionID is the only valid ID for a transfer after the fork.
func ComputeTransactionID(tx Transaction) string {
	digest := sha256.Sum256([]byte(transactionSigningBody(tx)))
	return "TX_" + hex.EncodeToString(digest[:])
}

// transactionContentDigest covers every stored field of a transaction,
// signatures included. The block hash is built from these.
func transactionContentDigest(tx Transaction) string {
	var builder strings.Builder
	writeField(&builder, tx.ID)
	writeField(&builder, transactionSigningBody(tx))
	writeField(&builder, tx.Signature)
	for _, input := range tx.Inputs {
		writeField(&builder, input.Signature)
	}
	writeField(&builder, formatAmountExact(tx.DataSizeKB))
	writeField(&builder, hex.EncodeToString(tx.Extra))
	digest := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(digest[:])
}

// BlockTransactionsDigest is the transaction part of the block hash. Before
// the fork it covers IDs only (unchanged, so old blocks keep their hashes);
// from the fork on it covers full contents.
func BlockTransactionsDigest(height int64, transactions []Transaction) string {
	var builder strings.Builder
	if IsTxForkHeight(height) {
		builder.WriteString("CVN_BLOCK_TXS_V2|")
		for _, tx := range transactions {
			builder.WriteString(transactionContentDigest(tx))
			builder.WriteByte('-')
		}
	} else {
		for _, tx := range transactions {
			builder.WriteString(tx.ID + "-")
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(builder.String())))
}

func validAmount(amount float64) bool {
	return !math.IsNaN(amount) && !math.IsInf(amount, 0) && amount > 0 && amount <= TxMaxAmount
}

func validRecipient(recipient string) bool {
	if recipient == "" || len(recipient) > TxMaxAddressLength {
		return false
	}
	// Stored balances are keyed by the trimmed address, so an address that
	// changes when trimmed could never be spent by its owner.
	return recipient == strings.TrimSpace(recipient) && !strings.ContainsAny(recipient, " \t\r\n")
}

// CheckTransferStandalone checks everything about a transfer that does not
// need the coin database: shape, ID and signature. It returns the address
// that signed it.
func CheckTransferStandalone(tx Transaction) (string, error) {
	if len(tx.Inputs) == 0 {
		return "", fmt.Errorf("transfer spends no coins")
	}
	if len(tx.Inputs) > TxMaxInputs {
		return "", fmt.Errorf("transfer has %d inputs (limit %d)", len(tx.Inputs), TxMaxInputs)
	}
	if len(tx.Outputs) == 0 || len(tx.Outputs) > TxMaxOutputs {
		return "", fmt.Errorf("transfer has %d outputs (allowed 1-%d)", len(tx.Outputs), TxMaxOutputs)
	}
	if math.IsNaN(tx.FreeWillOffering) || math.IsInf(tx.FreeWillOffering, 0) || tx.FreeWillOffering < 0 {
		return "", fmt.Errorf("invalid fee field")
	}
	if math.IsNaN(tx.DataSizeKB) || math.IsInf(tx.DataSizeKB, 0) {
		return "", fmt.Errorf("invalid size field")
	}
	if len(tx.Extra) > 1024 || len(tx.Witness) > TxMaxAddressLength {
		return "", fmt.Errorf("oversized transfer fields")
	}
	seenInputs := make(map[string]bool, len(tx.Inputs))
	for _, input := range tx.Inputs {
		if input.SourceTxID == "" || input.Index < 0 || len(input.SourceTxID) > 200 || len(input.Signature) > 300 {
			return "", fmt.Errorf("malformed input")
		}
		key := utxoKey(input.SourceTxID, input.Index)
		if seenInputs[key] {
			return "", fmt.Errorf("coin %s listed twice", key)
		}
		seenInputs[key] = true
	}
	for _, output := range tx.Outputs {
		if !validRecipient(output.Recipient) {
			return "", fmt.Errorf("invalid recipient address")
		}
		if !validAmount(output.Amount) {
			return "", fmt.Errorf("invalid output amount")
		}
	}
	if len(tx.PublicKey) != 128 {
		return "", fmt.Errorf("public key must be 128 hex characters")
	}
	senderAddress, err := DeriveAddressFromPublicKey(tx.PublicKey)
	if err != nil {
		return "", err
	}
	if tx.ID != ComputeTransactionID(tx) {
		return "", fmt.Errorf("transfer ID does not match its contents")
	}
	signatureParts := strings.Split(tx.Signature, "|")
	if len(signatureParts) != 2 || len(tx.Signature) > 300 {
		return "", fmt.Errorf("malformed signature")
	}
	if !VerifyTransactionSignature(tx.PublicKey, tx.ID, signatureParts[0], signatureParts[1]) {
		return "", fmt.Errorf("signature does not match the sender's key")
	}
	return senderAddress, nil
}

// checkTransferAgainstCoins checks a transfer against the coin set: every
// input must exist, be unspent (also within this block or mempool, via
// alreadySpent) and belong to the signer, and outputs may not exceed inputs.
// It returns the fee and the coins consumed.
//
// With exactUnits set (blocks from the units fork on) the comparison is done
// in whole units with no tolerance; see units.go.
func checkTransferAgainstCoins(tx Transaction, lookup utxoLookup, alreadySpent map[string]bool, exactUnits bool) (float64, []SpentRecord, error) {
	senderAddress, err := CheckTransferStandalone(tx)
	if err != nil {
		return 0, nil, err
	}
	if _, exists := lookup(utxoKey(tx.ID, 0)); exists {
		return 0, nil, fmt.Errorf("transfer %s is already on the chain", tx.ID)
	}
	if exactUnits {
		return checkTransferAgainstCoinsInUnits(tx, senderAddress, lookup, alreadySpent)
	}
	var inputTotal float64
	consumed := make([]SpentRecord, 0, len(tx.Inputs))
	for _, input := range tx.Inputs {
		key := utxoKey(input.SourceTxID, input.Index)
		if alreadySpent[key] {
			return 0, nil, fmt.Errorf("coin %s is already being spent", key)
		}
		record, exists := lookup(key)
		if !exists {
			return 0, nil, fmt.Errorf("coin %s does not exist or is already spent", key)
		}
		if strings.TrimSpace(record.Recipient) != senderAddress {
			return 0, nil, fmt.Errorf("coin %s does not belong to the signer", key)
		}
		inputTotal += record.Amount
		consumed = append(consumed, SpentRecord{Key: key, Record: record})
	}
	var outputTotal float64
	for _, output := range tx.Outputs {
		outputTotal += output.Amount
	}
	if outputTotal > inputTotal+TxAmountTolerance {
		return 0, nil, fmt.Errorf("transfer pays out %s but only takes in %s", formatAmountExact(outputTotal), formatAmountExact(inputTotal))
	}
	fee := inputTotal - outputTotal
	if fee < 0 {
		fee = 0
	}
	return fee, consumed, nil
}

// checkTransferAgainstCoinsInUnits is the whole-unit form of the check above.
// The fee it returns is an exact number of units, expressed in CVN.
func checkTransferAgainstCoinsInUnits(tx Transaction, senderAddress string, lookup utxoLookup, alreadySpent map[string]bool) (float64, []SpentRecord, error) {
	var inputUnits int64
	consumed := make([]SpentRecord, 0, len(tx.Inputs))
	for _, input := range tx.Inputs {
		key := utxoKey(input.SourceTxID, input.Index)
		if alreadySpent[key] {
			return 0, nil, fmt.Errorf("coin %s is already being spent", key)
		}
		record, exists := lookup(key)
		if !exists {
			return 0, nil, fmt.Errorf("coin %s does not exist or is already spent", key)
		}
		if strings.TrimSpace(record.Recipient) != senderAddress {
			return 0, nil, fmt.Errorf("coin %s does not belong to the signer", key)
		}
		coinUnits, ok := AmountToUnits(record.Amount)
		if !ok {
			return 0, nil, fmt.Errorf("coin %s has an unusable amount", key)
		}
		var addErr error
		if inputUnits, addErr = addUnits(inputUnits, coinUnits); addErr != nil {
			return 0, nil, addErr
		}
		consumed = append(consumed, SpentRecord{Key: key, Record: record})
	}
	var outputUnits int64
	for _, output := range tx.Outputs {
		units, ok := AmountToUnits(output.Amount)
		if !ok || units < 1 {
			return 0, nil, fmt.Errorf("an output is smaller than the smallest unit (0.00000001 CVN)")
		}
		var addErr error
		if outputUnits, addErr = addUnits(outputUnits, units); addErr != nil {
			return 0, nil, addErr
		}
	}
	if outputUnits > inputUnits {
		return 0, nil, fmt.Errorf("transfer pays out %s but only takes in %s", FormatUnits(outputUnits), FormatUnits(inputUnits))
	}
	return UnitsToAmount(inputUnits - outputUnits), consumed, nil
}

// ValidateBlockTransactions applies the fork rules to every transaction in a
// block. lookup must describe the coin set as it is just before this block.
// It returns the total fees and the coins the block consumes.
func ValidateBlockTransactions(b Block, lookup utxoLookup) (float64, []SpentRecord, error) {
	if len(b.Transactions) == 0 {
		return 0, nil, fmt.Errorf("block has no reward transaction")
	}
	if len(b.Transactions) > TxMaxPerBlock {
		return 0, nil, fmt.Errorf("block has %d transactions (limit %d)", len(b.Transactions), TxMaxPerBlock)
	}

	coinbase := b.Transactions[0]
	if len(coinbase.Inputs) != 0 {
		return 0, nil, fmt.Errorf("reward transaction must not spend coins")
	}
	if !strings.HasPrefix(coinbase.ID, fmt.Sprintf("TX_COINBASE_H%d_", b.Index)) || len(coinbase.ID) > 200 {
		return 0, nil, fmt.Errorf("reward transaction ID does not name height %d", b.Index)
	}
	if len(coinbase.Outputs) == 0 || len(coinbase.Outputs) > 2 {
		return 0, nil, fmt.Errorf("reward transaction must have one or two outputs")
	}
	var coinbaseTotal float64
	for _, output := range coinbase.Outputs {
		if !validRecipient(output.Recipient) || !validAmount(output.Amount) {
			return 0, nil, fmt.Errorf("invalid reward output")
		}
		coinbaseTotal += output.Amount
	}
	if _, exists := lookup(utxoKey(coinbase.ID, 0)); exists {
		return 0, nil, fmt.Errorf("reward transaction ID already used")
	}

	exactUnits := IsUnitsForkHeight(b.Index)
	seenIDs := map[string]bool{coinbase.ID: true}
	spentInBlock := make(map[string]bool)
	var totalFees float64
	var totalFeeUnits int64
	var consumed []SpentRecord
	for _, tx := range b.Transactions[1:] {
		if seenIDs[tx.ID] {
			return 0, nil, fmt.Errorf("transaction %s appears twice", tx.ID)
		}
		seenIDs[tx.ID] = true
		fee, spent, err := checkTransferAgainstCoins(tx, lookup, spentInBlock, exactUnits)
		if err != nil {
			return 0, nil, fmt.Errorf("transfer %.20s rejected: %w", tx.ID, err)
		}
		for _, record := range spent {
			spentInBlock[record.Key] = true
		}
		totalFees += fee
		if exactUnits {
			feeUnits, _ := AmountToUnits(fee)
			var addErr error
			if totalFeeUnits, addErr = addUnits(totalFeeUnits, feeUnits); addErr != nil {
				return 0, nil, addErr
			}
		}
		consumed = append(consumed, spent...)
	}

	if exactUnits {
		// Whole units, no tolerance: the reward outputs may pay at most the
		// newly issued reward plus the fees of this block's transfers.
		var coinbaseUnits int64
		for _, output := range coinbase.Outputs {
			units, ok := AmountToUnits(output.Amount)
			if !ok || units < 1 {
				return 0, nil, fmt.Errorf("a reward output is smaller than the smallest unit")
			}
			var addErr error
			if coinbaseUnits, addErr = addUnits(coinbaseUnits, units); addErr != nil {
				return 0, nil, addErr
			}
		}
		allowedUnits := BlockRewardUnits(b.Index) + totalFeeUnits
		if coinbaseUnits > allowedUnits {
			return 0, nil, fmt.Errorf("reward of %s exceeds the allowed %s", FormatUnits(coinbaseUnits), FormatUnits(allowedUnits))
		}
		return UnitsToAmount(totalFeeUnits), consumed, nil
	}

	allowed := CalculateBlockReward(b.Index) + totalFees
	if coinbaseTotal > allowed+0.00001 {
		return 0, nil, fmt.Errorf("reward of %s exceeds the allowed %s", formatAmountExact(coinbaseTotal), formatAmountExact(allowed))
	}
	return totalFees, consumed, nil
}

// bucketLookup reads coins straight from the utxo bucket.
func bucketLookup(bUTXO *bbolt.Bucket) utxoLookup {
	return func(key string) (UTXORecord, bool) {
		raw := bUTXO.Get([]byte(key))
		if raw == nil {
			return UTXORecord{}, false
		}
		var record UTXORecord
		if json.Unmarshal(raw, &record) != nil {
			return UTXORecord{}, false
		}
		return record, true
	}
}

// lookupWithBlockUndone describes the coin set as it was before `applied`
// (the block currently at the tip) was stored: coins it created are hidden
// and coins it spent are visible again. Used to judge a competing block at
// the same height.
func lookupWithBlockUndone(dbTx *bbolt.Tx, bUTXO *bbolt.Bucket, applied Block) utxoLookup {
	created := make(map[string]bool)
	for _, tx := range applied.Transactions {
		for outIdx := range tx.Outputs {
			created[utxoKey(tx.ID, outIdx)] = true
		}
	}
	restored := make(map[string]UTXORecord)
	for _, record := range loadUndoRecords(dbTx, applied.Index) {
		restored[record.Key] = record.Record
	}
	direct := bucketLookup(bUTXO)
	return func(key string) (UTXORecord, bool) {
		if record, ok := restored[key]; ok {
			return record, true
		}
		if created[key] {
			return UTXORecord{}, false
		}
		return direct(key)
	}
}

func undoKey(height int64) []byte {
	return []byte(fmt.Sprintf("%016d", height))
}

func loadUndoRecords(dbTx *bbolt.Tx, height int64) []SpentRecord {
	bUndo := dbTx.Bucket(undoBucket)
	if bUndo == nil {
		return nil
	}
	raw := bUndo.Get(undoKey(height))
	if raw == nil {
		return nil
	}
	var records []SpentRecord
	_ = json.Unmarshal(raw, &records)
	return records
}

func storeUndoRecords(dbTx *bbolt.Tx, height int64, records []SpentRecord) error {
	bUndo, err := dbTx.CreateBucketIfNotExists(undoBucket)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return bUndo.Delete(undoKey(height))
	}
	raw, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return bUndo.Put(undoKey(height), raw)
}

// undoBlockCoinChanges reverses what storing `block` did to the coin set:
// the coins it created are removed and the coins it spent are put back.
// Must run inside the write transaction that removes or replaces the block.
func undoBlockCoinChanges(dbTx *bbolt.Tx, bUTXO *bbolt.Bucket, block Block) {
	for _, tx := range block.Transactions {
		for outIdx := range tx.Outputs {
			_ = bUTXO.Delete([]byte(utxoKey(tx.ID, outIdx)))
		}
	}
	for _, record := range loadUndoRecords(dbTx, block.Index) {
		if raw, err := json.Marshal(record.Record); err == nil {
			_ = bUTXO.Put([]byte(record.Key), raw)
		}
	}
	if bUndo := dbTx.Bucket(undoBucket); bUndo != nil {
		_ = bUndo.Delete(undoKey(block.Index))
	}
}

// ---------------------------------------------------------------------------
// Mempool
// ---------------------------------------------------------------------------

const (
	MempoolMaxTransactions = 5000
	MempoolMaxAge          = 2 * time.Hour
)

var mempoolArrival = make(map[string]time.Time) // guarded by MempoolMatrix's lock

// AdmitTransactionToMempool is the only way a transfer enters the mempool.
// It applies the same checks a block would, against the current coin set and
// against transfers already waiting, so junk never gets stored or gossiped.
func AdmitTransactionToMempool(tx Transaction) error {
	if _, err := CheckTransferStandalone(tx); err != nil {
		return err
	}
	if GlobalBoltEngine == nil {
		return fmt.Errorf("node is not ready")
	}

	MempoolMatrix.Lock()
	defer MempoolMatrix.Unlock()
	if MempoolMatrix.Transactions == nil {
		MempoolMatrix.Transactions = make(map[string]Transaction)
	}
	if _, known := MempoolMatrix.Transactions[tx.ID]; known {
		return fmt.Errorf("already known")
	}
	if len(MempoolMatrix.Transactions) >= MempoolMaxTransactions {
		return fmt.Errorf("mempool is full")
	}
	pendingSpends := make(map[string]bool)
	for _, waiting := range MempoolMatrix.Transactions {
		for _, input := range waiting.Inputs {
			pendingSpends[utxoKey(input.SourceTxID, input.Index)] = true
		}
	}
	var checkErr error
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		bUTXO := dbTx.Bucket(utxoBucket)
		if bUTXO == nil {
			checkErr = fmt.Errorf("coin database missing")
			return nil
		}
		_, _, checkErr = checkTransferAgainstCoins(tx, bucketLookup(bUTXO), pendingSpends, IsUnitsForkHeight(atomic.LoadInt64(&localHeightState)+1))
		return nil
	})
	if checkErr != nil {
		return checkErr
	}
	MempoolMatrix.Transactions[tx.ID] = tx
	mempoolArrival[tx.ID] = time.Now()
	return nil
}

// MempoolPendingSpends lists coins that waiting transfers are about to spend.
func MempoolPendingSpends() map[string]bool {
	pending := make(map[string]bool)
	MempoolMatrix.RLock()
	for _, waiting := range MempoolMatrix.Transactions {
		for _, input := range waiting.Inputs {
			pending[utxoKey(input.SourceTxID, input.Index)] = true
		}
	}
	MempoolMatrix.RUnlock()
	return pending
}

// SelectMempoolTransactionsForBlock returns the waiting transfers that are
// valid on top of the current chain, highest fee first, with their total fee.
// Transfers that have become invalid (confirmed, double-spent or too old) are
// dropped from the mempool on the way.
func SelectMempoolTransactionsForBlock(limit int) ([]Transaction, float64) {
	if GlobalBoltEngine == nil {
		return nil, 0
	}
	MempoolMatrix.Lock()
	defer MempoolMatrix.Unlock()
	if len(MempoolMatrix.Transactions) == 0 {
		return nil, 0
	}

	type candidate struct {
		tx    Transaction
		fee   float64
		spent []SpentRecord
	}
	exactUnits := IsUnitsForkHeight(atomic.LoadInt64(&localHeightState) + 1)
	var candidates []candidate
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		bUTXO := dbTx.Bucket(utxoBucket)
		if bUTXO == nil {
			return nil
		}
		lookup := bucketLookup(bUTXO)
		for id, tx := range MempoolMatrix.Transactions {
			if time.Since(mempoolArrival[id]) > MempoolMaxAge {
				delete(MempoolMatrix.Transactions, id)
				delete(mempoolArrival, id)
				continue
			}
			fee, spent, err := checkTransferAgainstCoins(tx, lookup, nil, exactUnits)
			if err != nil {
				delete(MempoolMatrix.Transactions, id)
				delete(mempoolArrival, id)
				continue
			}
			candidates = append(candidates, candidate{tx: tx, fee: fee, spent: spent})
		}
		return nil
	})
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].fee != candidates[j].fee {
			return candidates[i].fee > candidates[j].fee
		}
		return candidates[i].tx.ID < candidates[j].tx.ID
	})

	var selected []Transaction
	var totalFees float64
	var totalFeeUnits int64
	taken := make(map[string]bool)
	for _, entry := range candidates {
		if len(selected) >= limit {
			break
		}
		conflict := false
		for _, record := range entry.spent {
			if taken[record.Key] {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		for _, record := range entry.spent {
			taken[record.Key] = true
		}
		selected = append(selected, entry.tx)
		totalFees += entry.fee
		if feeUnits, ok := AmountToUnits(entry.fee); ok {
			totalFeeUnits += feeUnits
		}
	}
	if exactUnits {
		// An exact number of units, so the miner's reward output matches what
		// every node will compute when it checks the block.
		return selected, UnitsToAmount(totalFeeUnits)
	}
	return selected, totalFees
}

// RemoveConfirmedFromMempool drops transfers a stored block has included.
func RemoveConfirmedFromMempool(block Block) {
	if len(block.Transactions) <= 1 {
		return
	}
	MempoolMatrix.Lock()
	for _, tx := range block.Transactions[1:] {
		delete(MempoolMatrix.Transactions, tx.ID)
		delete(mempoolArrival, tx.ID)
	}
	MempoolMatrix.Unlock()
}

// ---------------------------------------------------------------------------
// Wallet helpers
// ---------------------------------------------------------------------------

// SpendableCoin is one unspent output owned by an address.
type SpendableCoin struct {
	TxID   string  `json:"tx_id"`
	Index  int     `json:"output_index"`
	Amount float64 `json:"amount"`
}

// ListSpendableCoins returns the unspent coins of an address, leaving out
// any that a waiting transfer is already spending.
func ListSpendableCoins(address string) []SpendableCoin {
	address = strings.TrimSpace(address)
	coins := make([]SpendableCoin, 0)
	if GlobalBoltEngine == nil || address == "" {
		return coins
	}
	pending := MempoolPendingSpends()
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		bUTXO := dbTx.Bucket(utxoBucket)
		if bUTXO == nil {
			return nil
		}
		cursor := bUTXO.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var record UTXORecord
			if json.Unmarshal(v, &record) != nil || strings.TrimSpace(record.Recipient) != address || record.Amount <= 0 {
				continue
			}
			key := string(k)
			if pending[key] {
				continue
			}
			separator := strings.LastIndex(key, "_")
			if separator <= 0 {
				continue
			}
			index, err := strconv.Atoi(key[separator+1:])
			if err != nil {
				continue
			}
			coins = append(coins, SpendableCoin{TxID: key[:separator], Index: index, Amount: record.Amount})
		}
		return nil
	})
	sort.Slice(coins, func(i, j int) bool {
		if coins[i].Amount != coins[j].Amount {
			return coins[i].Amount > coins[j].Amount
		}
		return coins[i].TxID < coins[j].TxID
	})
	return coins
}

// BuildSignedTransfer assembles and signs a transfer that pays `amount` to
// `recipient` plus `fee` to the miner, returning change to the sender.
func BuildSignedTransfer(privateKeyHex, publicKeyHex, recipient string, amount, fee float64, coins []SpendableCoin) (Transaction, error) {
	recipient = strings.TrimSpace(recipient)
	if !validRecipient(recipient) {
		return Transaction{}, fmt.Errorf("invalid recipient address")
	}
	if !validAmount(amount) {
		return Transaction{}, fmt.Errorf("amount must be greater than zero")
	}
	if math.IsNaN(fee) || math.IsInf(fee, 0) || fee < 0 {
		return Transaction{}, fmt.Errorf("fee cannot be negative")
	}
	senderAddress, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		return Transaction{}, err
	}
	// Everything is worked out in whole units, so the change is exact and the
	// transfer is valid under the whole-unit rules as well as the older ones.
	amountUnits, _ := AmountToUnits(amount)
	feeUnits, feeOK := AmountToUnits(fee)
	if amountUnits < 1 {
		return Transaction{}, fmt.Errorf("amount is smaller than the smallest unit (0.00000001 CVN)")
	}
	if !feeOK {
		return Transaction{}, fmt.Errorf("fee is too large")
	}
	requiredUnits, err := addUnits(amountUnits, feeUnits)
	if err != nil {
		return Transaction{}, err
	}
	var inputs []UTXOInput
	var gatheredUnits int64
	for _, coin := range coins {
		if len(inputs) >= TxMaxInputs {
			break
		}
		coinUnits, ok := AmountToUnits(coin.Amount)
		if !ok || coinUnits < 1 {
			continue
		}
		inputs = append(inputs, UTXOInput{SourceTxID: coin.TxID, Index: coin.Index})
		gatheredUnits += coinUnits
		if gatheredUnits >= requiredUnits {
			break
		}
	}
	if gatheredUnits < requiredUnits {
		if len(inputs) >= TxMaxInputs {
			return Transaction{}, fmt.Errorf("this amount needs more than %d coins in one transfer; the largest %d add up to %s CVN, so send it in smaller parts", TxMaxInputs, TxMaxInputs, FormatUnits(gatheredUnits))
		}
		return Transaction{}, fmt.Errorf("insufficient spendable balance: need %s, have %s", FormatUnits(requiredUnits), FormatUnits(gatheredUnits))
	}
	outputs := []UTXOOutput{{Recipient: recipient, Amount: UnitsToAmount(amountUnits)}}
	if changeUnits := gatheredUnits - requiredUnits; changeUnits > 0 {
		outputs = append(outputs, UTXOOutput{Recipient: senderAddress, Amount: UnitsToAmount(changeUnits)})
	}
	fee = UnitsToAmount(feeUnits)
	tx := Transaction{
		Version:          1,
		Inputs:           inputs,
		Outputs:          outputs,
		PublicKey:        publicKeyHex,
		Timestamp:        time.Now().Unix(),
		FreeWillOffering: fee,
		DataSizeKB:       1.0,
		Witness:          senderAddress,
	}
	tx.ID = ComputeTransactionID(tx)
	r, s, err := SignTransactionPayload(privateKeyHex, tx.ID)
	if err != nil {
		return Transaction{}, err
	}
	tx.Signature = r + "|" + s
	if _, err := CheckTransferStandalone(tx); err != nil {
		return Transaction{}, fmt.Errorf("keys do not produce a valid signature: %w", err)
	}
	return tx, nil
}

// walletNodeGateways are the nodes a wallet asks, in order: the node on this
// machine first, then the public seeds.
func walletNodeGateways() []string {
	return append(localSyncURLs(), seedSyncURLs()...)
}

// FetchSpendableCoinsOverHTTP asks a node for an address's unspent coins and
// returns them with the node that answered.
func FetchSpendableCoinsOverHTTP(address string) ([]SpendableCoin, string, error) {
	client := &http.Client{Timeout: 6 * time.Second}
	for _, gateway := range walletNodeGateways() {
		resp, err := client.Get(gateway + "/utxos?address=" + strings.TrimSpace(address))
		if err != nil {
			continue
		}
		var coins []SpendableCoin
		decodeErr := json.NewDecoder(resp.Body).Decode(&coins)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || decodeErr != nil {
			// A node that has not updated yet does not offer this lookup.
			continue
		}
		return coins, gateway, nil
	}
	return nil, "", fmt.Errorf("no updated node answered; start your node or check your connection")
}

// SubmitTransferOverHTTP hands a signed transfer to a node's mempool.
func SubmitTransferOverHTTP(gateway string, tx Transaction) error {
	payload, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Post(gateway+"/transaction", "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("could not reach the node: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	var reply map[string]string
	if json.Unmarshal(body, &reply) == nil && reply["error"] != "" {
		return fmt.Errorf("node refused the transfer: %s", reply["error"])
	}
	return fmt.Errorf("node refused the transfer (status %d)", resp.StatusCode)
}

// SendTransferFromWallet builds, signs and submits a transfer in one step.
func SendTransferFromWallet(privateKeyHex, publicKeyHex, recipient string, amount, fee float64) (Transaction, error) {
	senderAddress, err := DeriveAddressFromPublicKey(strings.TrimSpace(publicKeyHex))
	if err != nil {
		return Transaction{}, err
	}
	coins, gateway, err := FetchSpendableCoinsOverHTTP(senderAddress)
	if err != nil {
		return Transaction{}, err
	}
	tx, err := BuildSignedTransfer(strings.TrimSpace(privateKeyHex), strings.TrimSpace(publicKeyHex), recipient, amount, fee, coins)
	if err != nil {
		return Transaction{}, err
	}
	if err := SubmitTransferOverHTTP(gateway, tx); err != nil {
		return Transaction{}, err
	}
	return tx, nil
}
