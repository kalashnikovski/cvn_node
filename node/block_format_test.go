package node

import (
	"strings"
	"testing"
)

// An earlier header format was a line of text with its fields joined by
// dashes, so text could be moved from one field to the next without changing
// the line: these two headers had the same identifier. Each field is now
// written with its length, and each header has its own identifier.
func TestHeaderFieldsCannotBleedIntoEachOther(t *testing.T) {
	first := Block{Version: 1, Index: 5, Timestamp: 100, PrevHash: "p", Difficulty: 7, GuardMatrix: "a", Nonce: 1, UncleHash: "h", UncleMiner: "m-2-x-y"}
	second := Block{Version: 1, Index: 5, Timestamp: 100, PrevHash: "p", Difficulty: 7, GuardMatrix: "a-1-h-m", Nonce: 2, UncleHash: "x", UncleMiner: "y"}

	onNetwork(t, "devnet")
	if CalculateHashWithTxDigest(first, "d") == CalculateHashWithTxDigest(second, "d") {
		t.Error("two different headers have the same identifier under the launch format")
	}
	// Every field counts.
	base := Block{Version: 1, Index: 5, Timestamp: 100, PrevHash: "p", Difficulty: 7, Nonce: 1, UncleHash: emptyUncleHash, UncleMiner: emptyUncleMiner}
	identifier := CalculateHashWithTxDigest(base, "d")
	changes := map[string]func(*Block){
		"version": func(b *Block) { b.Version = 2 }, "height": func(b *Block) { b.Index = 6 },
		"date": func(b *Block) { b.Timestamp = 101 }, "parent": func(b *Block) { b.PrevHash = "q" },
		"difficulty": func(b *Block) { b.Difficulty = 8 }, "header text": func(b *Block) { b.GuardMatrix = "x" },
		"nonce": func(b *Block) { b.Nonce = 2 }, "uncle hash": func(b *Block) { b.UncleHash = "x" },
		"uncle miner": func(b *Block) { b.UncleMiner = "x" }, "trailing space": func(b *Block) { b.PrevHash = "p " },
	}
	for name, change := range changes {
		altered := base
		change(&altered)
		if CalculateHashWithTxDigest(altered, "d") == identifier {
			t.Errorf("changing the %s does not change the identifier", name)
		}
	}
	if CalculateHashWithTxDigest(base, "e") == identifier {
		t.Error("changing the transactions does not change the identifier")
	}
}

func TestBlocksOutsideTheLaunchFormatAreRefused(t *testing.T) {
	startTestChain(t, "devnet")
	timestamp := Params.GenesisTimestamp + 60
	good := mineTestBlock(t, timestamp, testAddress("miner"), nil, 0)
	if !ValidateBlockSignatures(good) {
		t.Fatal("a well-formed block was refused")
	}
	remine := func(change func(*Block)) Block {
		block := good
		change(&block)
		digest := BlockTransactionsDigest(block.Index, block.Transactions)
		for nonce := int64(0); ; nonce++ {
			block.Nonce = nonce
			block.Hash = CalculateHashWithTxDigest(block, digest)
			if BlockProofValid(block, block.Difficulty) {
				return block
			}
		}
	}
	bad := map[string]func(*Block){
		"text in the header":       func(b *Block) { b.GuardMatrix = "hello" },
		"an uncle named":           func(b *Block) { b.UncleHash = strings.Repeat("ab", 32); b.UncleMiner = testAddress("friend") },
		"an empty uncle hash":      func(b *Block) { b.UncleHash = "" },
		"an unknown block version": func(b *Block) { b.Version = 2 },
		"a parent with a space":    func(b *Block) { b.PrevHash = b.PrevHash + " " },
	}
	for name, change := range bad {
		if ValidateBlockSignatures(remine(change)) {
			t.Errorf("a block with %s was accepted", name)
		}
	}
	upper := good
	upper.Hash = strings.ToUpper(good.Hash)
	if ValidateBlockSignatures(upper) {
		t.Error("a block whose identifier is written in capitals was accepted")
	}

	// The reward transaction is bare too.
	badReward := map[string]func(*Transaction){
		"extra data":            func(tx *Transaction) { tx.Extra = []byte("hello") },
		"a size field":          func(tx *Transaction) { tx.DataSizeKB = 1 },
		"a fee":                 func(tx *Transaction) { tx.FreeWillOffering = UnitsAmount(1) },
		"another date":          func(tx *Transaction) { tx.Timestamp++ },
		"a long identifier":     func(tx *Transaction) { tx.ID += strings.Repeat("a", 70) },
		"an identifier not hex": func(tx *Transaction) { tx.ID += "zz" },
		"another public key":    func(tx *Transaction) { tx.PublicKey = "x" },
		"two outputs": func(tx *Transaction) {
			tx.Outputs = append(tx.Outputs, UTXOOutput{Recipient: testAddress("friend"), Amount: UnitsAmount(1000000)})
			tx.Outputs[0].Amount = UnitsAmount(cvn("49"))
		},
		"an output below minimum": func(tx *Transaction) { tx.Outputs[0].Amount = UnitsAmount(minimumOutputUnits() - 1) },
	}
	for name, change := range badReward {
		block := remine(func(b *Block) {
			reward := b.Transactions[0]
			reward.Outputs = append([]UTXOOutput{}, reward.Outputs...)
			change(&reward)
			b.Transactions = []Transaction{reward}
		})
		if ValidateBlockSignatures(block) {
			t.Errorf("a reward transaction with %s was accepted", name)
		}
	}
	// A block may still pay its miner nothing at all.
	empty := remine(func(b *Block) {
		reward := b.Transactions[0]
		reward.Outputs = nil
		b.Transactions = []Transaction{reward}
	})
	if !ValidateBlockSignatures(empty) {
		t.Error("a block that claims no reward was refused")
	}
}

func TestTransfersCarryNothingUnsigned(t *testing.T) {
	startTestChain(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	extendTestChain(t, Params.GenesisTimestamp+60, owner, nil)
	transfer, err := BuildSignedTransfer(privateKey, publicKey, testAddress("someone"), cvn("10"), 0, ListSpendableCoins(owner))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CheckTransferStandalone(transfer); err != nil {
		t.Fatalf("the wallet's own transfer was refused: %v", err)
	}
	// None of these touch what the sender signed, which is exactly why they
	// must not be allowed to ride along.
	attach := map[string]func(*Transaction){
		"extra data":   func(tx *Transaction) { tx.Extra = []byte("junk") },
		"a size field": func(tx *Transaction) { tx.DataSizeKB = 1 },
		"a per-input field": func(tx *Transaction) {
			tx.Inputs = []UTXOInput{{SourceTxID: tx.Inputs[0].SourceTxID, Index: tx.Inputs[0].Index, Signature: "junk"}}
		},
		"an unknown version": func(tx *Transaction) { tx.Version = 2 },
	}
	for name, change := range attach {
		altered := transfer
		change(&altered)
		if _, err := CheckTransferStandalone(altered); err == nil {
			t.Errorf("a transfer with %s attached was accepted", name)
		}
	}
}

// A transfer signed for one network is not valid on another, even when the
// same coins exist on both.
func TestTransferIsBoundToItsNetwork(t *testing.T) {
	onNetwork(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	coins := []SpendableCoin{{TxID: "TX_A", Index: 0, Amount: UnitsAmount(cvn("50"))}}
	transfer, err := BuildSignedTransfer(privateKey, publicKey, testAddress("someone"), cvn("10"), 0, coins)
	if err != nil {
		t.Fatal(err)
	}
	if signer, err := CheckTransferStandalone(transfer); err != nil || signer != owner {
		t.Fatalf("refused on its own network: %v", err)
	}
	Params.Name = "another-network"
	if _, err := CheckTransferStandalone(transfer); err == nil {
		t.Error("a transfer signed for one network was accepted on another")
	}
}

func TestBlockSizeLimit(t *testing.T) {
	startTestChain(t, "devnet")
	privateKey, publicKey, owner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := Params.GenesisTimestamp
	for i := 0; i < 6; i++ {
		timestamp += 45
		extendTestChain(t, timestamp, owner, nil)
	}
	coins := ListSpendableCoins(owner)
	var transfers []Transaction
	for _, coin := range coins {
		transfer, err := BuildSignedTransfer(privateKey, publicKey, testAddress("someone"), cvn("1"), cvn("0.01"), []SpendableCoin{coin})
		if err != nil {
			t.Fatal(err)
		}
		transfers = append(transfers, transfer)
	}
	one := transactionSizeBytes(transfers[0])
	if one < 300 || one > 1500 {
		t.Fatalf("a simple transfer is counted as %d bytes; expected a few hundred", one)
	}
	full := mineTestBlock(t, timestamp+45, testAddress("miner"), transfers, 0)
	size := blockSizeBytes(full)

	// One byte under the block's size, the block is refused; at its size, accepted.
	Params.MaxBlockBytes = size - 1
	if ValidateBlockSignatures(full) {
		t.Error("a block over the size limit was accepted")
	}
	Params.MaxBlockBytes = size
	if !ValidateBlockSignatures(full) {
		t.Error("a block exactly at the size limit was refused")
	}

	// A miner fills a block only as far as the limit allows, and what does
	// not fit waits for the next block.
	for _, transfer := range transfers {
		if err := AdmitTransactionToMempool(transfer); err != nil {
			t.Fatalf("mempool refused a transfer: %v", err)
		}
	}
	Params.MaxBlockBytes = blockRewardReserveBytes + 3*one + one/2
	chosen, _ := SelectMempoolTransactionsForBlock(TxMaxPerBlock - 1)
	if len(chosen) != 3 {
		t.Errorf("a miner chose %d transfers for a block with room for 3", len(chosen))
	}
	block := extendTestChain(t, timestamp+45, testAddress("miner"), chosen)
	if blockSizeBytes(block) > Params.MaxBlockBytes {
		t.Error("the miner's own block is over the limit")
	}
	// A transfer too large for any block is turned away at the door.
	Params.MaxBlockBytes = blockRewardReserveBytes + transactionSizeBytes(transfers[5]) - 1
	MempoolMatrix.Lock()
	MempoolMatrix.Transactions = make(map[string]Transaction)
	MempoolMatrix.Unlock()
	if err := AdmitTransactionToMempool(transfers[5]); err == nil || !strings.Contains(err.Error(), "more than a block can hold") {
		t.Errorf("a transfer too large for any block was not refused for that reason: %v", err)
	}
}
