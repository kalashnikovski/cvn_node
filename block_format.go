package main

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
)

// THE LAUNCH FORMAT
//
// These rules apply on networks whose settings have StrictFormat on. They do
// not change what a block or a transfer means; they make sure that each one
// can be written in exactly one way, and that nothing rides along that its
// author did not sign.
//
//   - The block identifier is the hash of the header's fields, each written
//     with its length in front, so no field's content can pass for a
//     separator or for part of its neighbour.
//   - Every header field has one permitted form. Text fields that have no
//     use after the genesis block must be empty.
//   - A transfer has no fields beyond the ones its sender signs, and the
//     signature covers the network's name.
//   - A reward transaction is likewise bare.
//
// The first test network (testnet-1) keeps its original, looser format.

const (
	emptyUncleHash  = "0000000000000000000000000000000000000000000000000000000000000000"
	emptyUncleMiner = "NONE"

	// rewardIDSuffixMax bounds the miner-chosen part of a reward
	// transaction's identifier.
	rewardIDSuffixMax = 64

	// blockRewardReserveBytes is the room a miner leaves in a block for its
	// header and reward transaction when choosing transfers.
	blockRewardReserveBytes = 2000
)

// blockHeaderEncoding is the exact text whose hash is a block's identifier
// under the launch format.
func blockHeaderEncoding(b Block, transactionsDigest string) string {
	var builder strings.Builder
	writeField(&builder, "CVN_BLOCK_V2")
	writeField(&builder, strconv.FormatUint(uint64(b.Version), 10))
	writeField(&builder, strconv.FormatInt(b.Index, 10))
	writeField(&builder, strconv.FormatInt(b.Timestamp, 10))
	writeField(&builder, b.PrevHash)
	writeField(&builder, transactionsDigest)
	writeField(&builder, strconv.Itoa(b.Difficulty))
	writeField(&builder, b.GuardMatrix)
	writeField(&builder, strconv.FormatInt(b.Nonce, 10))
	writeField(&builder, b.UncleHash)
	writeField(&builder, b.UncleMiner)
	return builder.String()
}

// isLowerHex reports whether text is exactly `length` lower-case hex digits.
func isLowerHex(text string, length int) bool {
	if len(text) != length {
		return false
	}
	for _, c := range text {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// checkBlockFormat checks the shape of a block's header under the launch
// format. The genesis block is built by the program itself and is exempt.
func checkBlockFormat(b Block) error {
	if !Params.StrictFormat || b.Index == 0 {
		return nil
	}
	if b.Version != 1 {
		return fmt.Errorf("block version %d is not known", b.Version)
	}
	if !isLowerHex(b.PrevHash, 64) || !isLowerHex(b.Hash, 64) {
		return fmt.Errorf("block identifiers must be 64 lower-case hex digits")
	}
	if b.Nonce < 0 {
		return fmt.Errorf("negative nonce")
	}
	if b.GuardMatrix != "" {
		return fmt.Errorf("the header text field is used by the genesis block only")
	}
	if Params.NoUncles {
		if b.UncleHash != emptyUncleHash || b.UncleMiner != emptyUncleMiner {
			return fmt.Errorf("this network has no uncle share; the uncle fields must be empty")
		}
	} else {
		if !isLowerHex(b.UncleHash, 64) || !validRecipient(b.UncleMiner) {
			return fmt.Errorf("malformed uncle fields")
		}
	}
	return nil
}

// checkTransferFormat refuses a transfer that carries anything its sender
// did not sign.
func checkTransferFormat(tx Transaction) error {
	if !Params.StrictFormat {
		return nil
	}
	if tx.Version != 1 {
		return fmt.Errorf("transfer version %d is not known", tx.Version)
	}
	if tx.DataSizeKB != 0 || len(tx.Extra) != 0 {
		return fmt.Errorf("transfer carries fields that are not part of the format")
	}
	for _, input := range tx.Inputs {
		if input.Signature != "" {
			return fmt.Errorf("transfer carries fields that are not part of the format")
		}
	}
	return nil
}

// rewardTransactionID builds the identifier of a block's reward transaction.
// It must name the block's height, which is what keeps it from ever being
// used twice in one chain; `uniqueness` lets separate miners and mining
// threads work on different blocks.
func rewardTransactionID(height int64, uniqueness string) string {
	return fmt.Sprintf("TX_COINBASE_H%d_%x", height, sha256.Sum256([]byte(fmt.Sprintf("%d-%s", height, uniqueness))))
}

// checkRewardFormat checks the shape of a block's reward transaction under
// the launch format.
func checkRewardFormat(coinbase Transaction, b Block) error {
	if !Params.StrictFormat {
		return nil
	}
	suffix := strings.TrimPrefix(coinbase.ID, fmt.Sprintf("TX_COINBASE_H%d_", b.Index))
	if len(suffix) == 0 || len(suffix) > rewardIDSuffixMax || !isLowerHex(suffix, len(suffix)) {
		return fmt.Errorf("reward transaction ID must end in 1 to %d lower-case hex digits", rewardIDSuffixMax)
	}
	if coinbase.Version != 1 || coinbase.PublicKey != CoinbasePublicKeyMarker || coinbase.Signature != CoinbaseSignatureMarker {
		return fmt.Errorf("malformed reward transaction")
	}
	if coinbase.DataSizeKB != 0 || len(coinbase.Extra) != 0 || coinbase.FreeWillOffering.Units() != 0 {
		return fmt.Errorf("reward transaction carries fields that are not part of the format")
	}
	if coinbase.Timestamp != b.Timestamp {
		return fmt.Errorf("reward transaction must carry its block's date")
	}
	if !validRecipient(coinbase.Witness) {
		return fmt.Errorf("reward transaction names no valid miner")
	}
	if Params.NoUncles && len(coinbase.Outputs) > 1 {
		return fmt.Errorf("this network has no uncle share; a block pays one reward output at most")
	}
	return nil
}

// transactionSizeBytes is the size of a transaction for the block size
// limit: the length of everything in it that the block identifier covers.
// It is counted from the format's own encoding, not from how a program
// happens to store or send the transaction.
func transactionSizeBytes(tx Transaction) int {
	size := len(tx.ID) + len(transactionSigningBody(tx)) + len(tx.Signature) + len(tx.Extra)
	for _, input := range tx.Inputs {
		size += len(input.Signature)
	}
	return size
}

// blockSizeBytes is the size of a block for the block size limit: its header
// plus every transaction in it.
func blockSizeBytes(b Block) int {
	size := len(blockHeaderEncoding(b, emptyUncleHash))
	for _, tx := range b.Transactions {
		size += transactionSizeBytes(tx)
	}
	return size
}

// blockTransferBudgetBytes is how many bytes of transfers a miner puts in a
// block, leaving room for the header and reward. Zero means no limit.
func blockTransferBudgetBytes() int {
	if Params.MaxBlockBytes <= 0 {
		return 0
	}
	return Params.MaxBlockBytes - blockRewardReserveBytes
}
