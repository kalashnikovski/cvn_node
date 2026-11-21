package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
)

type Block struct {
	Version      uint32        `json:"version"`
	Index        int64         `json:"current_height"`
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     string        `json:"prev_hash"`
	Hash         string        `json:"block_hash"`
	Difficulty   int           `json:"difficulty"`
	Nonce        int64         `json:"nonce"`
	GuardMatrix  string        `json:"guard_matrix"`
	UncleHash    string        `json:"uncle_hash"`
	UncleMiner   string        `json:"uncle_miner"`
}

type Transaction struct {
	Version          uint32       `json:"version"`
	ID               string       `json:"tx_id"`
	Inputs           []UTXOInput  `json:"inputs"`
	Outputs          []UTXOOutput `json:"outputs"`
	Signature        string       `json:"signature"`
	PublicKey        string       `json:"public_key"`
	Timestamp        int64        `json:"timestamp"`
	FreeWillOffering Amount       `json:"free_will_offering"`
	DataSizeKB       float64      `json:"data_size_kb"`
	Witness          string       `json:"witness"`
	Extra            []byte       `json:"extra,omitempty"`
}

type UTXOInput struct {
	SourceTxID string `json:"source_tx_id"`
	Index      int    `json:"output_index"`
	Signature  string `json:"signature"`
	// AmountUnits is the amount of the coin being spent, as the sender
	// believed it to be when signing. It is part of what is signed and must
	// match the coin exactly, so a wallet that was told the wrong amount
	// cannot be made to sign away the difference as a fee.
	AmountUnits int64 `json:"amount_units,omitempty"`
}

type UTXOOutput struct {
	Recipient string `json:"recipient"`
	Amount    Amount `json:"amount"`
}

type UTXORecord struct {
	Amount    Amount `json:"amount"`
	Recipient string `json:"recipient"`
	// Age is the height of the block that created the coin, or that last
	// swept it. Only kept on networks that sweep long-unmoved coins.
	Age int64 `json:"age,omitempty"`
}

type MempoolType struct {
	sync.RWMutex
	Transactions map[string]Transaction
}

var BlockchainFile = "cvn_testnet.db"
var CustomMinerAddress string
var IsSyncingActive int32 = 0

var BalanceCacheMutex sync.RWMutex

// StateBalanceCache holds every address's balance in units.
var StateBalanceCache = make(map[string]int64)
var MempoolMatrix = &MempoolType{Transactions: make(map[string]Transaction)}
var StateMutex sync.RWMutex

func CalculateHash(b Block) string {
	return CalculateHashWithTxDigest(b, BlockTransactionsDigest(b.Index, b.Transactions))
}

// CalculateHashWithTxDigest hashes a block header given the digest of its
// transactions. The miner computes that digest once per template instead of
// once per nonce.
func CalculateHashWithTxDigest(b Block, txHashStr string) string {
	digest := sha256.Sum256([]byte(blockHeaderEncoding(b, txHashStr)))
	return hex.EncodeToString(digest[:])
}

// blockAlreadyStored reports whether this exact block is the one stored at
// its height, as seen by the open database view.
func blockAlreadyStored(dbTx *bbolt.Tx, b Block) bool {
	bBlocks := dbTx.Bucket(blocksBucket)
	if bBlocks == nil || b.Hash == "" {
		return false
	}
	var stored Block
	if raw := bBlocks.Get([]byte(fmt.Sprintf("%016d", b.Index))); raw == nil || json.Unmarshal(raw, &stored) != nil {
		return false
	}
	return stored.Hash == b.Hash
}

func ProcessIncomingNetworkBlock(incomingBlock Block) {
	if incomingBlock.Index <= 0 {
		return // the genesis block is never taken from the network
	}
	StateMutex.Lock()
	currentTip := GetLatestBlockInternal()
	StateMutex.Unlock()

	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		// A pass is comparing chains; it will fetch whatever this block
		// belongs to.
		return
	}

	StateMutex.Lock()
	currentTip = GetLatestBlockInternal()
	StateMutex.Unlock()

	// A different block at our own height that does not share our tip's
	// parent belongs to another branch. (One that does share it is settled
	// by the sibling rule before this function is reached.) Which branch to
	// follow is decided by total work, in the re-sync, never by which block
	// arrived or was dated first; our own tip is not touched here.
	if incomingBlock.Index == currentTip.Index && incomingBlock.Hash != currentTip.Hash {
		requestSyncPassForBlock()
		return
	}
	if incomingBlock.Index < currentTip.Index {
		return
	}
	// A block that does not attach to our tip cannot be fully validated here:
	// its difficulty depends on a parent we do not have (or, on a fork, on a
	// different parent than the one we hold). It has already passed the
	// proof-of-work pre-check on the wire, so treat it as a sign that we are
	// behind or on a side branch and let the re-sync compare chains with peers.
	// Validating first made a node that fell two or more blocks behind reject
	// every later block and stay on its own branch for good.
	if incomingBlock.Index > currentTip.Index+1 {
		requestSyncPassForBlock()
		return
	}
	if incomingBlock.Index == currentTip.Index+1 && incomingBlock.PrevHash != currentTip.Hash {
		requestSyncPassForBlock()
		return
	}
	if incomingBlock.Index == currentTip.Index+1 && placeOnTip(incomingBlock) {
		go BroadcastNewBlock(incomingBlock)
	}
}

// placeOnTip checks a block that is to follow the newest block and stores
// it. Every block that is added one at a time comes through here: a block
// from a neighbour, a block this node's own miner found, and a block the
// fast miner handed in.
//
// It holds the chain still while it does: those three and a pass that is
// comparing chains (sync_pass.go) all add blocks, and each used to look at
// the newest block, then check, then store, with nothing to stop another
// doing the same in between. Two of them adding the same
// block at once could leave the pass believing its source had sent a bad
// block, and the source was then set aside for ten minutes. Under the lock
// each sees the chain as the other left it.
func placeOnTip(block Block) bool {
	StateMutex.Lock()
	defer StateMutex.Unlock()
	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		atomic.StoreInt32(&blockMissedDuringPass, 1)
		return false
	}
	tip := GetLatestBlockInternal()
	if block.Index != tip.Index+1 || block.PrevHash != tip.Hash {
		return false
	}
	return ValidateBlockSignatures(block) && SaveBlockToStorageInternal(block)
}
func ProcessIncomingWireBlockWithDelay(incomingBlock Block) {
	if incomingBlock.Index <= 0 {
		return // the genesis block is never taken from the network
	}
	// For a block this node asked for after an announcement, the log says
	// how long it took to come and how long to check and store. Without
	// those two figures a slow block cannot be told from a slow node.
	sinceAsked, askedFor := noteAnnouncedBlockArrived(incomingBlock.Hash)
	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		// The chain is being changed and this block cannot be placed now.
		// It is not kept, but it is not forgotten either: the pass asks
		// again as soon as it ends (see catchUpAfterMissedBlock).
		atomic.StoreInt32(&blockMissedDuringPass, 1)
		if askedFor {
			log.Printf("📦 [RELAY] Block #%d arrived %.1f seconds after it was asked for. The chain is being changed, so the pass will fetch it.\n", incomingBlock.Index, sinceAsked.Seconds())
		}
		return
	}
	// A block whose parent this node has asked a neighbour for, and which
	// has not come yet, is kept for a moment and placed once the parent is
	// here (see placeAfterItsParent).
	if placeAfterItsParent(incomingBlock) {
		if askedFor {
			log.Printf("📦 [RELAY] Block #%d arrived %.1f seconds after it was asked for. It is kept until its parent is in place.\n", incomingBlock.Index, sinceAsked.Seconds())
		}
		return
	}
	began := time.Now()
	placeWireBlock(incomingBlock)
	if askedFor {
		outcome := "It was not placed on top of the chain."
		if _, tip := latestTipID(); tip == incomingBlock.Hash {
			outcome = fmt.Sprintf("Checking and storing it took %.1f seconds.", time.Since(began).Seconds())
		}
		log.Printf("📦 [RELAY] Block #%d (%d bytes) arrived %.1f seconds after it was asked for. %s\n", incomingBlock.Index, blockSizeBytes(incomingBlock), sinceAsked.Seconds(), outcome)
	}
}

// placeWireBlock puts a block that arrived from a neighbour where it belongs:
// on top of the chain, beside the newest block as its rival, or nowhere, in
// which case the node compares chains with others.
func placeWireBlock(incomingBlock Block) {
	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		atomic.StoreInt32(&blockMissedDuringPass, 1)
		return
	}
	StateMutex.Lock()
	currentTip := GetLatestBlockInternal()
	StateMutex.Unlock()
	if incomingBlock.Index == currentTip.Index+1 {
		if incomingBlock.PrevHash != currentTip.Hash {
			requestSyncPassForBlock()
			return
		}
		if placeOnTip(incomingBlock) {
			go BroadcastNewBlock(incomingBlock)
		}
		return
	}
	if incomingBlock.Index == currentTip.Index && incomingBlock.Hash != currentTip.Hash &&
		incomingBlock.PrevHash == currentTip.PrevHash {
		// Two blocks on the same parent. More work wins, then the lower hash;
		// every node applies the same rule, so they all settle on one block.
		// If the incoming block wins, replace our tip with it now instead of
		// staying on our own block until the next height forces a re-sync.
		// The loser is simply dropped: there is no uncle share.
		// The gossip handler has already checked this block's proof of work
		// against the parent the two share (see blockCarriesBadWork), so it
		// is a real rival and the height is counted as a race.
		if siblingWins(currentTip, incomingBlock) {
			noteBlockRace(incomingBlock.Index, currentTip.Index, currentTip.Hash, incomingBlock.Hash)
			return
		}
		StateMutex.Lock()
		if atomic.LoadInt32(&IsSyncingActive) == 1 {
			// A pass began changing the chain since the check above. The
			// newest block is the pass's to change now.
			StateMutex.Unlock()
			atomic.StoreInt32(&blockMissedDuringPass, 1)
			return
		}
		liveTip := GetLatestBlockInternal()
		adopted := liveTip.Hash == currentTip.Hash &&
			ValidateBlockSignatures(incomingBlock) &&
			SaveBlockToStorageInternal(incomingBlock)
		StateMutex.Unlock()
		if adopted {
			noteBlockRace(incomingBlock.Index, currentTip.Index, incomingBlock.Hash, currentTip.Hash)
			fmt.Printf("🔄 [TIP SWITCH] Height %d: adopted competing block %.16s (more work or lower hash) over local %.16s\n", incomingBlock.Index, incomingBlock.Hash, currentTip.Hash)
		}
		return
	}
	ProcessIncomingNetworkBlock(incomingBlock)
}

// runChainLagWatchdog is a safety net for a node that has silently fallen
// behind or onto a side branch (for example blocks missed while it was
// starting up). Once a minute it asks a few peers for their height and starts
// a re-sync if any is well ahead.
func runChainLagWatchdog() {
	const lagThresholdBlocks = 3
	for {
		time.Sleep(60 * time.Second)
		if atomic.LoadInt32(&IsSyncingActive) == 1 {
			continue
		}
		gateways := reachableSeedSyncURLs()
		gateways = append(gateways, cachedPeerHTTPGateways(gateways)...)
		StateMutex.RLock()
		localHeight := GetLatestBlockInternal().Index
		StateMutex.RUnlock()
		localWork := TotalChainWork()
		for _, gateway := range gateways {
			remoteHeight, remoteWork := peerChainStatus(gateway)
			if remoteHeight >= localHeight+lagThresholdBlocks {
				log.Printf("🩺 [LAG WATCHDOG] %s is at height #%d, we are at #%d. Starting catch-up.\n", gateway, remoteHeight, localHeight)
				go TriggerActiveReSyncPass()
				break
			}
			// A chain can hold more work without being longer. A peer one or two
			// blocks ahead is normal propagation and is left to gossip.
			if remoteWork != nil && remoteHeight > 0 && remoteHeight <= localHeight && remoteWork.Cmp(localWork) > 0 {
				log.Printf("🩺 [LAG WATCHDOG] %s holds more total work at height #%d than our chain at #%d. Comparing chains.\n", gateway, remoteHeight, localHeight)
				go TriggerActiveReSyncPass()
				break
			}
		}
	}
}

// CalculateNextDifficulty returns the difficulty required of the block that
// follows lastBlock and is dated nextBlockTimestamp.
func CalculateNextDifficulty(lastBlock Block, nextBlockTimestamp int64) int {
	return CalculateHeartbeatDifficulty(lastBlock, nextBlockTimestamp)
}

func ValidateBlockSignatures(b Block) bool {
	// There is exactly one genesis block, and the program builds it itself
	// (see genesis.go). Nothing else at height 0, or below it, is a block.
	if b.Index <= 0 {
		return b.Index == 0 && b.Hash == buildGenesisBlock().Hash && b.Hash == CalculateHash(b)
	}
	if err := checkBlockFormat(b); err != nil {
		fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d: %v\n", b.Index, err)
		return false
	}
	// A block is judged against its parent, which must be the block we hold
	// at the height below.
	parentBlock := GetBlockByHeightFromDB(b.Index - 1)
	if parentBlock.Hash == "" {
		return false
	}
	// A block must move time forward, and may not be dated far ahead of the
	// clock: a later date is what eases difficulty. (Dates that always move
	// forward also keep every block later than the median of those before
	// it, the rule older chains state separately.)
	if b.Timestamp <= parentBlock.Timestamp {
		fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d is not dated after its parent.\n", b.Index)
		return false
	}
	if b.Timestamp > time.Now().Unix()+HeartbeatMaxFutureDriftSeconds {
		fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d is dated more than %d s ahead of this node's clock.\n", b.Index, HeartbeatMaxFutureDriftSeconds)
		return false
	}
	expectedDiff := CalculateNextDifficulty(parentBlock, b.Timestamp)
	if b.Difficulty != expectedDiff {
		fmt.Printf("🚨 REJECTION: Incoming block #%d uses invalid difficulty %d (Expected exactly: %d)\n", b.Index, b.Difficulty, expectedDiff)
		return false
	}
	// The cheap check first: the identifier must be the block's own. Only then
	// the proof of work, which under RandomX costs real time.
	if b.Hash != CalculateHash(b) || !BlockProofValid(b, expectedDiff) {
		return false
	}
	if len(b.Transactions) == 0 {
		return false
	}
	isBlockValid := true
	var accounting blockAccounting
	{
		// Transfers can only be judged against the coin set just before this
		// block. That is known when the block sits on our tip, or competes with
		// our tip for the same parent; anything else is settled by a re-sync.
		if GlobalBoltEngine == nil {
			return false
		}
		tipBlock := GetLatestBlockInternal()
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			bUTXO := dbTx.Bucket(utxoBucket)
			if bUTXO == nil {
				isBlockValid = false
				return nil
			}
			var lookup utxoLookup
			switch {
			case b.Index == tipBlock.Index+1 && b.PrevHash == tipBlock.Hash:
				lookup = bucketLookup(bUTXO)
			case b.Index == tipBlock.Index && b.PrevHash == tipBlock.PrevHash:
				lookup = lookupWithBlockUndone(dbTx, bUTXO, tipBlock)
			default:
				// A second copy of a block we already hold, arriving after the
				// chain has moved on past it, lands here too. That is not a
				// bad block and is not reported as one.
				if !blockAlreadyStored(dbTx, b) {
					fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d does not attach to the local tip, so its transfers cannot be checked.\n", b.Index)
				}
				isBlockValid = false
				return nil
			}
			// In both cases above the block's parent is stored at the height below it.
			vaultBefore, vaultErr := readVaultUnits(dbTx, b.Index-1, b.PrevHash)
			if vaultErr != nil {
				fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d: %v\n", b.Index, vaultErr)
				isBlockValid = false
				return nil
			}
			checked, _, err := ValidateBlockTransactions(b, lookup, vaultBefore)
			if err != nil {
				// The same block often arrives from several peers at once. If
				// another copy was stored between our reading the tip and
				// opening this view, this copy is judged against a coin set
				// that already holds its own coins and fails. That is a
				// duplicate, not a bad block, and is not reported as one.
				if !blockAlreadyStored(dbTx, b) {
					fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d: %v\n", b.Index, err)
				}
				isBlockValid = false
				return nil
			}
			accounting = checked
			return nil
		})
	}
	if !isBlockValid {
		return false
	}
	// Reward check, in whole units with no tolerance.
	if len(b.Transactions) == 0 {
		return false
	}
	coinbaseOutputs := b.Transactions[0].Outputs
	var totalCoinbaseUnits int64
	for _, out := range coinbaseOutputs {
		if out.Amount.Units() < 0 || out.Amount.Units() > MaxAmountUnits {
			return false
		}
		totalCoinbaseUnits += out.Amount.Units()
	}
	// What the block pays apart from fees: the new reward plus the vault's
	// payout. Its miner may add the miner's share of the fees.
	rewardUnits := accounting.SubsidyUnits()
	minerFeeUnits := accounting.MinerFeeUnits
	if totalCoinbaseUnits > rewardUnits+minerFeeUnits {
		fmt.Printf("🚨 EMISSION BLOCKED: Block pays %s, allowed %s.\n", FormatUnits(totalCoinbaseUnits), FormatUnits(rewardUnits+minerFeeUnits))
		return false
	}
	return true
}
