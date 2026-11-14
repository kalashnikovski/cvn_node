package node

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

// formatFloatExact writes a number so that the same number always gives the
// same text. It is used for the one non-amount number in a transfer.
func formatFloatExact(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
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
	if Params.StrictFormat {
		// The network's name is part of what is signed, so a transfer made
		// for one CVN network is worthless on every other.
		writeField(&builder, "CVN_TX_V2")
		writeField(&builder, Params.Name)
	} else {
		writeField(&builder, "CVN_TX_V1")
	}
	writeField(&builder, strconv.FormatUint(uint64(tx.Version), 10))
	writeField(&builder, tx.PublicKey)
	writeField(&builder, strconv.FormatInt(tx.Timestamp, 10))
	writeField(&builder, tx.FreeWillOffering.hashText())
	writeField(&builder, tx.Witness)
	writeField(&builder, strconv.Itoa(len(tx.Inputs)))
	for _, input := range tx.Inputs {
		writeField(&builder, input.SourceTxID)
		writeField(&builder, strconv.Itoa(input.Index))
		if Params.StrictFormat {
			writeField(&builder, strconv.FormatInt(input.AmountUnits, 10))
		}
	}
	writeField(&builder, strconv.Itoa(len(tx.Outputs)))
	for _, output := range tx.Outputs {
		writeField(&builder, output.Recipient)
		writeField(&builder, output.Amount.hashText())
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
	if Params.StrictFormat {
		// Under the launch format a transaction has no other fields.
		digest := sha256.Sum256([]byte(builder.String()))
		return hex.EncodeToString(digest[:])
	}
	for _, input := range tx.Inputs {
		writeField(&builder, input.Signature)
	}
	writeField(&builder, formatFloatExact(tx.DataSizeKB))
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
	if !tx.FreeWillOffering.ValidFee() {
		return "", fmt.Errorf("invalid fee field")
	}
	if math.IsNaN(tx.DataSizeKB) || math.IsInf(tx.DataSizeKB, 0) {
		return "", fmt.Errorf("invalid size field")
	}
	if len(tx.Extra) > 1024 || len(tx.Witness) > TxMaxAddressLength {
		return "", fmt.Errorf("oversized transfer fields")
	}
	if err := checkTransferFormat(tx); err != nil {
		return "", err
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
		if !output.Amount.Spendable() || output.Amount.Units() < minimumOutputUnits() {
			return "", fmt.Errorf("an output must be at least %s CVN and no more than can exist", FormatUnits(minimumOutputUnits()))
		}
	}
	if len(tx.PublicKey) != 128 {
		return "", fmt.Errorf("public key must be 128 hex characters")
	}
	senderAddress, err := DeriveAddressFromPublicKey(tx.PublicKey)
	if err != nil {
		return "", err
	}
	if Params.StrictFormat && tx.Witness != senderAddress {
		return "", fmt.Errorf("the sender field must hold the sender's address and nothing else")
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
// All amounts are whole units and the comparison is exact. It returns the fee
// in units and the coins consumed.
func checkTransferAgainstCoins(tx Transaction, lookup utxoLookup, alreadySpent map[string]bool) (int64, []SpentRecord, error) {
	senderAddress, err := CheckTransferStandalone(tx)
	if err != nil {
		return 0, nil, err
	}
	if _, exists := lookup(utxoKey(tx.ID, 0)); exists {
		return 0, nil, fmt.Errorf("transfer %s is already on the chain", tx.ID)
	}
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
		if !record.Amount.ValidFee() {
			return 0, nil, fmt.Errorf("coin %s has an unusable amount", key)
		}
		if Params.StrictFormat && input.AmountUnits != record.Amount.Units() {
			return 0, nil, fmt.Errorf("coin %s holds %s CVN, not the %s CVN the sender signed for", key, FormatUnits(record.Amount.Units()), FormatUnits(input.AmountUnits))
		}
		if inputUnits, err = addUnits(inputUnits, record.Amount.Units()); err != nil {
			return 0, nil, err
		}
		consumed = append(consumed, SpentRecord{Key: key, Record: record})
	}
	var outputUnits int64
	for _, output := range tx.Outputs {
		if outputUnits, err = addUnits(outputUnits, output.Amount.Units()); err != nil {
			return 0, nil, err
		}
	}
	if outputUnits > inputUnits {
		return 0, nil, fmt.Errorf("transfer pays out %s but only takes in %s", FormatUnits(outputUnits), FormatUnits(inputUnits))
	}
	// On the launch rules the fee a sender signs is the fee that is taken:
	// no more can leave the sender's coins than they agreed to.
	if Params.StrictFormat && tx.FreeWillOffering.Units() != inputUnits-outputUnits {
		return 0, nil, fmt.Errorf("transfer states a fee of %s CVN but leaves %s CVN unaccounted for", FormatUnits(tx.FreeWillOffering.Units()), FormatUnits(inputUnits-outputUnits))
	}
	return inputUnits - outputUnits, consumed, nil
}

// ValidateBlockTransactions applies the transaction rules to every
// transaction in a block. lookup must describe the coin set as it is just
// before this block, and vaultBeforeUnits the vault balance at that point. It
// returns the block's accounting and the coins the block consumes.
func ValidateBlockTransactions(b Block, lookup utxoLookup, vaultBeforeUnits int64) (blockAccounting, []SpentRecord, error) {
	var none blockAccounting
	if len(b.Transactions) == 0 {
		return none, nil, fmt.Errorf("block has no reward transaction")
	}
	if len(b.Transactions) > TxMaxPerBlock {
		return none, nil, fmt.Errorf("block has %d transactions (limit %d)", len(b.Transactions), TxMaxPerBlock)
	}

	coinbase := b.Transactions[0]
	if len(coinbase.Inputs) != 0 {
		return none, nil, fmt.Errorf("reward transaction must not spend coins")
	}
	if !strings.HasPrefix(coinbase.ID, fmt.Sprintf("TX_COINBASE_H%d_", b.Index)) || len(coinbase.ID) > 200 {
		return none, nil, fmt.Errorf("reward transaction ID does not name height %d", b.Index)
	}
	// Under the launch schedule the reward eventually reaches zero, and a block
	// with no fees then has nothing to pay out, so no outputs is allowed there.
	if len(coinbase.Outputs) > 2 || (len(coinbase.Outputs) == 0 && Params.Economics == nil) {
		return none, nil, fmt.Errorf("reward transaction must have one or two outputs")
	}
	if err := checkRewardFormat(coinbase, b); err != nil {
		return none, nil, err
	}
	if Params.MaxBlockBytes > 0 {
		if size := blockSizeBytes(b); size > Params.MaxBlockBytes {
			return none, nil, fmt.Errorf("block is %d bytes (limit %d)", size, Params.MaxBlockBytes)
		}
	}
	var coinbaseUnits int64
	var err error
	for _, output := range coinbase.Outputs {
		if !validRecipient(output.Recipient) || !output.Amount.Spendable() {
			return none, nil, fmt.Errorf("invalid reward output")
		}
		// A reward coin obeys the same minimum as every other coin. What a
		// block cannot pay because of it stays in the vault.
		if output.Amount.Units() < minimumOutputUnits() {
			return none, nil, fmt.Errorf("a reward output must be at least %s CVN", FormatUnits(minimumOutputUnits()))
		}
		if coinbaseUnits, err = addUnits(coinbaseUnits, output.Amount.Units()); err != nil {
			return none, nil, err
		}
	}
	if _, exists := lookup(utxoKey(coinbase.ID, 0)); exists {
		return none, nil, fmt.Errorf("reward transaction ID already used")
	}

	seenIDs := map[string]bool{coinbase.ID: true}
	spentInBlock := make(map[string]bool)
	var totalFeeUnits int64
	var consumed []SpentRecord
	for _, tx := range b.Transactions[1:] {
		if seenIDs[tx.ID] {
			return none, nil, fmt.Errorf("transaction %s appears twice", tx.ID)
		}
		seenIDs[tx.ID] = true
		feeUnits, spent, err := checkTransferAgainstCoins(tx, lookup, spentInBlock)
		if err != nil {
			return none, nil, fmt.Errorf("transfer %.20s rejected: %w", tx.ID, err)
		}
		for _, record := range spent {
			spentInBlock[record.Key] = true
		}
		if totalFeeUnits, err = addUnits(totalFeeUnits, feeUnits); err != nil {
			return none, nil, err
		}
		consumed = append(consumed, spent...)
	}

	// The reward outputs may pay at most the newly issued reward, the vault's
	// payout for this block, and the miner's share of the transfers' fees.
	// Exact, with no tolerance.
	accounting := accountForBlock(b.Index, totalFeeUnits, vaultBeforeUnits)
	if coinbaseUnits > accounting.MinerAllowanceUnits() {
		return none, nil, fmt.Errorf("reward of %s exceeds the allowed %s", FormatUnits(coinbaseUnits), FormatUnits(accounting.MinerAllowanceUnits()))
	}
	return accounting, consumed, nil
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
	// Coins its sweep reduced or removed are seen as they were before it.
	for _, swept := range loadSweepRecords(dbTx, applied.Index) {
		restored[swept.Key] = swept.Before
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
// coins its sweep touched are restored, the coins it created are removed and
// the coins it spent are put back.
// Must run inside the write transaction that removes or replaces the block.
func undoBlockCoinChanges(dbTx *bbolt.Tx, bUTXO *bbolt.Bucket, block Block) {
	// The sweep was the last thing the block did, so it is undone first.
	undoSweep(dbTx, bUTXO, block.Index)
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

// MempoolMaxTransactions is how many transfers a node keeps waiting. A
// variable so tests can make the pool small.
var MempoolMaxTransactions = 5000

// MempoolMaxAge is how long a transfer may wait before it is dropped.
const MempoolMaxAge = 2 * time.Hour

var mempoolArrival = make(map[string]time.Time) // guarded by MempoolMatrix's lock

// mempoolFees holds the fee of each waiting transfer, in units. Guarded by
// MempoolMatrix's lock. An entry may outlive its transfer; it is only ever
// read for transfers still waiting, and the leftovers are swept out in
// makeRoomInMempool.
var mempoolFees = make(map[string]int64)

// makeRoomInMempool removes one waiting transfer to make room for one paying
// feeUnits, and reports whether it did. Transfers that have waited too long
// go first; otherwise the lowest fee goes, and among equals the one that
// arrived last. Nothing is removed for a transfer that pays no more than the
// lowest fee waiting. The caller holds MempoolMatrix's lock.
func makeRoomInMempool(feeUnits int64) bool {
	if len(mempoolFees) > 2*len(MempoolMatrix.Transactions)+1000 {
		for id := range mempoolFees {
			if _, waiting := MempoolMatrix.Transactions[id]; !waiting {
				delete(mempoolFees, id)
			}
		}
	}
	victim, found := "", false
	var victimFee int64
	var victimArrived time.Time
	for id := range MempoolMatrix.Transactions {
		arrived := mempoolArrival[id]
		if time.Since(arrived) > MempoolMaxAge {
			victim, victimFee, found = id, -1, true
			break
		}
		fee := mempoolFees[id]
		if !found || fee < victimFee || (fee == victimFee && (arrived.After(victimArrived) || (arrived.Equal(victimArrived) && id > victim))) {
			victim, victimFee, victimArrived, found = id, fee, arrived, true
		}
	}
	if !found || victimFee >= feeUnits {
		return false
	}
	delete(MempoolMatrix.Transactions, victim)
	delete(mempoolArrival, victim)
	delete(mempoolFees, victim)
	return true
}

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
	if budget := blockTransferBudgetBytes(); budget > 0 && transactionSizeBytes(tx) > budget {
		return fmt.Errorf("transfer is %d bytes, more than a block can hold (%d)", transactionSizeBytes(tx), budget)
	}
	pendingSpends := make(map[string]bool)
	for _, waiting := range MempoolMatrix.Transactions {
		for _, input := range waiting.Inputs {
			pendingSpends[utxoKey(input.SourceTxID, input.Index)] = true
		}
	}
	var checkErr error
	var feeUnits int64
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		bUTXO := dbTx.Bucket(utxoBucket)
		if bUTXO == nil {
			checkErr = fmt.Errorf("coin database missing")
			return nil
		}
		feeUnits, _, checkErr = checkTransferAgainstCoins(tx, bucketLookup(bUTXO), pendingSpends)
		return nil
	})
	if checkErr != nil {
		return checkErr
	}
	// A full pool does not turn everything away: that would let anyone keep
	// it full of fee-less transfers and shut everyone else out for nothing.
	// A transfer that pays more than the cheapest one waiting takes its place.
	if len(MempoolMatrix.Transactions) >= MempoolMaxTransactions {
		if !makeRoomInMempool(feeUnits) {
			return fmt.Errorf("mempool is full; a transfer paying a higher fee than the lowest one waiting would be taken")
		}
	}
	mempoolFees[tx.ID] = feeUnits
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

// HOW A MINER FILLS A BLOCK
//
// This is the behaviour of this program's miner, not a rule of the network:
// which transfers go into a block is the miner's choice and cannot be
// checked from the block.
//
// A block's transfer slots, and its bytes, are split into two lanes.
//
//   - The paid lane, four fifths of the block: transfers that pay a fee,
//     highest fee first.
//   - The free lane, one fifth: transfers that pay no fee, longest-waiting
//     first, and only if every coin they spend is at least a day old
//     (freeLaneMinAgeBlocks). The age condition stops the free lane being
//     used to flood the chain by passing the same coins back and forth.
//
// Space one lane does not use goes to the other. Whatever is still free after
// that takes fee-less transfers of younger coins, so that on a quiet network
// every transfer confirms in the next block, as it always has.
//
// The free lane needs coin ages, which only networks with the Sabbatical
// sweep record. On other networks blocks are filled highest fee first.

// freeLaneShare is the part of a block kept for fee-less transfers: one in
// this many slots and bytes.
const freeLaneShare = 5

// freeLaneMinAgeBlocks is how old every coin a fee-less transfer spends must
// be for the free lane: one day of blocks. A variable so tests can shorten it.
var freeLaneMinAgeBlocks = int64(1920)

// mempoolCandidate is a waiting transfer that is valid on the current chain.
type mempoolCandidate struct {
	tx      Transaction
	fee     int64
	spent   []SpentRecord
	arrived time.Time
	size    int
}

// spendsOnlySettledCoins reports whether every coin the transfer spends is
// at least freeLaneMinAgeBlocks old at the given height.
func (c mempoolCandidate) spendsOnlySettledCoins(height int64) bool {
	for _, record := range c.spent {
		if height-record.Record.Age < freeLaneMinAgeBlocks {
			return false
		}
	}
	return true
}

// SelectMempoolTransactionsForBlock returns the waiting transfers a miner
// puts in its next block, at most `limit` of them, with their total fee in
// units. Transfers that have become invalid (confirmed, double-spent or too
// old) are dropped from the mempool on the way.
func SelectMempoolTransactionsForBlock(limit int) ([]Transaction, int64) {
	if GlobalBoltEngine == nil {
		return nil, 0
	}
	nextHeight := GetLatestBlockInternal().Index + 1
	MempoolMatrix.Lock()
	defer MempoolMatrix.Unlock()
	if len(MempoolMatrix.Transactions) == 0 {
		return nil, 0
	}

	var candidates []mempoolCandidate
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
			fee, spent, err := checkTransferAgainstCoins(tx, lookup, nil)
			if err != nil {
				delete(MempoolMatrix.Transactions, id)
				delete(mempoolArrival, id)
				continue
			}
			candidates = append(candidates, mempoolCandidate{tx: tx, fee: fee, spent: spent, arrived: mempoolArrival[id], size: transactionSizeBytes(tx)})
		}
		return nil
	})
	return fillBlockFromCandidates(candidates, limit, blockTransferBudgetBytes(), nextHeight, sweepEnabled())
}

// fillBlockFromCandidates chooses which of the valid waiting transfers go
// into a block of at most `limit` transfers and `budgetBytes` bytes (zero
// meaning no byte limit). With `lanes` off it is highest fee first.
func fillBlockFromCandidates(candidates []mempoolCandidate, limit, budgetBytes int, nextHeight int64, lanes bool) ([]Transaction, int64) {
	var paid, freeSettled, freeYoung []mempoolCandidate
	for _, entry := range candidates {
		switch {
		case entry.fee > 0 || !lanes:
			paid = append(paid, entry)
		case entry.spendsOnlySettledCoins(nextHeight):
			freeSettled = append(freeSettled, entry)
		default:
			freeYoung = append(freeYoung, entry)
		}
	}
	sort.Slice(paid, func(i, j int) bool {
		if paid[i].fee != paid[j].fee {
			return paid[i].fee > paid[j].fee
		}
		return paid[i].tx.ID < paid[j].tx.ID
	})
	longestWaitingFirst := func(list []mempoolCandidate) {
		sort.Slice(list, func(i, j int) bool {
			if !list[i].arrived.Equal(list[j].arrived) {
				return list[i].arrived.Before(list[j].arrived)
			}
			return list[i].tx.ID < list[j].tx.ID
		})
	}
	longestWaitingFirst(freeSettled)
	longestWaitingFirst(freeYoung)

	var selected []Transaction
	var totalFeeUnits int64
	usedBytes := 0
	chosen := make(map[string]bool)
	taken := make(map[string]bool)
	// fill adds transfers from a list until the block holds countLimit
	// transfers; one that would take it past byteLimit is passed over, since
	// a smaller one further down may still fit.
	fill := func(list []mempoolCandidate, countLimit, byteLimit int) {
		for _, entry := range list {
			if len(selected) >= countLimit {
				return
			}
			if chosen[entry.tx.ID] || (byteLimit > 0 && usedBytes+entry.size > byteLimit) {
				continue
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
			chosen[entry.tx.ID] = true
			selected = append(selected, entry.tx)
			totalFeeUnits += entry.fee
			usedBytes += entry.size
		}
	}
	if !lanes {
		fill(paid, limit, budgetBytes)
		return selected, totalFeeUnits
	}
	fill(paid, limit-limit/freeLaneShare, budgetBytes-budgetBytes/freeLaneShare) // the paid lane
	fill(freeSettled, limit, budgetBytes)                                        // the free lane, plus what the paid lane left
	fill(paid, limit, budgetBytes)                                               // paid transfers into what the free lane left
	fill(freeYoung, limit, budgetBytes)                                          // anything still free
	return selected, totalFeeUnits
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
	TxID   string `json:"tx_id"`
	Index  int    `json:"output_index"`
	Amount Amount `json:"amount"`
	// SweepAt is the block at whose end this coin will be swept if it has
	// not been spent by then (0 on a network that does not sweep).
	SweepAt int64 `json:"sweep_at,omitempty"`
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
			if json.Unmarshal(v, &record) != nil || strings.TrimSpace(record.Recipient) != address || !record.Amount.Spendable() {
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
			coins = append(coins, SpendableCoin{TxID: key[:separator], Index: index, Amount: record.Amount, SweepAt: nextSweepHeight(record.Age)})
		}
		return nil
	})
	sort.Slice(coins, func(i, j int) bool {
		if coins[i].Amount.Units() != coins[j].Amount.Units() {
			return coins[i].Amount.Units() > coins[j].Amount.Units()
		}
		return coins[i].TxID < coins[j].TxID
	})
	return coins
}

// BuildSignedTransfer assembles and signs a transfer that pays amountUnits to
// `recipient` plus feeUnits to the miner, returning change to the sender. All
// amounts are whole units.
func BuildSignedTransfer(privateKeyHex, publicKeyHex, recipient string, amountUnits, feeUnits int64, coins []SpendableCoin) (Transaction, error) {
	recipient = strings.TrimSpace(recipient)
	if !validRecipient(recipient) {
		return Transaction{}, fmt.Errorf("invalid recipient address")
	}
	if amountUnits < minimumOutputUnits() {
		return Transaction{}, fmt.Errorf("amount must be at least %s CVN", FormatUnits(minimumOutputUnits()))
	}
	if feeUnits < 0 {
		return Transaction{}, fmt.Errorf("fee cannot be negative")
	}
	senderAddress, err := DeriveAddressFromPublicKey(publicKeyHex)
	if err != nil {
		return Transaction{}, err
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
		if !coin.Amount.Spendable() {
			continue
		}
		inputs = append(inputs, walletInput(coin))
		gatheredUnits += coin.Amount.Units()
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
	outputs := []UTXOOutput{{Recipient: recipient, Amount: UnitsAmount(amountUnits)}}
	// Change too small to be a valid output is left for the miner as part of
	// the fee, so the wallet never builds a transfer the network would refuse.
	if changeUnits := gatheredUnits - requiredUnits; changeUnits >= minimumOutputUnits() {
		outputs = append(outputs, UTXOOutput{Recipient: senderAddress, Amount: UnitsAmount(changeUnits)})
	} else {
		feeUnits += changeUnits
	}
	return signTransfer(privateKeyHex, publicKeyHex, senderAddress, inputs, outputs, feeUnits)
}

// looksLikeWalletAddress reports whether text has the form of an address
// made from a key: "CVN_" and 40 lower-case hex digits.
func looksLikeWalletAddress(text string) bool {
	return strings.HasPrefix(text, "CVN_") && isLowerHex(strings.TrimPrefix(text, "CVN_"), 40)
}

// walletInput names a coin to spend. On the launch rules it also states the
// coin's amount, which the sender signs.
func walletInput(coin SpendableCoin) UTXOInput {
	input := UTXOInput{SourceTxID: coin.TxID, Index: coin.Index}
	if Params.StrictFormat {
		input.AmountUnits = coin.Amount.Units()
	}
	return input
}

// signTransfer turns chosen inputs and outputs into a signed transfer.
func signTransfer(privateKeyHex, publicKeyHex, senderAddress string, inputs []UTXOInput, outputs []UTXOOutput, feeUnits int64) (Transaction, error) {
	tx := Transaction{
		Version:          1,
		Inputs:           inputs,
		Outputs:          outputs,
		PublicKey:        publicKeyHex,
		Timestamp:        time.Now().Unix(),
		FreeWillOffering: UnitsAmount(feeUnits),
		DataSizeKB:       1.0,
		Witness:          senderAddress,
	}
	if Params.StrictFormat {
		tx.DataSizeKB = 0 // the launch format has no such field
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
func SendTransferFromWallet(privateKeyHex, publicKeyHex, recipient string, amountUnits, feeUnits int64) (Transaction, error) {
	// The rules let coins be sent to any text at all, and coins sent to
	// something that is not a wallet address are gone for good. A wallet
	// does not let its user do that by accident.
	if recipient = strings.TrimSpace(recipient); !looksLikeWalletAddress(recipient) {
		return Transaction{}, fmt.Errorf("%q is not a CVN wallet address. An address is CVN_ followed by 40 characters, each a digit or a letter from a to f. Nothing was sent", recipient)
	}
	senderAddress, err := DeriveAddressFromPublicKey(strings.TrimSpace(publicKeyHex))
	if err != nil {
		return Transaction{}, err
	}
	coins, gateway, err := FetchSpendableCoinsOverHTTP(senderAddress)
	if err != nil {
		return Transaction{}, err
	}
	tx, err := BuildSignedTransfer(strings.TrimSpace(privateKeyHex), strings.TrimSpace(publicKeyHex), recipient, amountUnits, feeUnits, coins)
	if err != nil {
		return Transaction{}, err
	}
	if err := SubmitTransferOverHTTP(gateway, tx); err != nil {
		return Transaction{}, err
	}
	return tx, nil
}
