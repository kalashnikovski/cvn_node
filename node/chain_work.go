package node

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
)

// TOTAL-WORK CHAIN SELECTION
//
// A node used to prefer whichever chain had more blocks. Difficulty changes on
// every block, and it falls quickly when blocks stop arriving, so a chain can
// be longer and still represent far less mining effort. Preferring it would
// let cheap blocks displace expensive ones.
//
// The node now adds up the work in every block (the average number of hashes
// that block needed) and prefers the chain with the most total work:
//
//   - when choosing whom to sync from, a peer that reports more total work
//     wins; peers on older software, which report only a height, are still
//     judged by height;
//   - before abandoning any of its own blocks for a peer's branch, the node
//     downloads that branch, checks each block's proof of work, and switches
//     only if the branch holds more work than the blocks it would replace;
//   - if the new branch then fails full validation part-way, the node puts its
//     own blocks back;
//   - two blocks competing at the same height on the same parent are decided
//     by work first, and by the lower hash only when the work is equal.
//
// This is a choice each node makes for itself. It changes no rule about which
// blocks are valid, so it needs no activation height.

var chainWorkBucket = []byte("chainwork")

// maxCandidateBranchBlocks bounds how much of a competing branch is examined
// before deciding. Examination stops earlier as soon as the branch has shown
// more work than the blocks it would replace.
const maxCandidateBranchBlocks = 20000

type chainWorkEntry struct {
	Hash string `json:"h"`
	Work string `json:"w"`
}

// BlockWork is the average number of hashes needed to find this block.
func BlockWork(b Block) *big.Int {
	if IsHeartbeatHeight(b.Index) {
		if b.Difficulty < 1 {
			return big.NewInt(1)
		}
		return big.NewInt(int64(b.Difficulty))
	}
	// Older blocks: difficulty counted leading zero hex digits, 16x per digit.
	zeros := b.Difficulty
	if b.Index <= 10 {
		zeros = 2
	}
	if zeros < 0 {
		zeros = 0
	}
	if zeros > 64 {
		zeros = 64
	}
	return new(big.Int).Exp(big.NewInt(16), big.NewInt(int64(zeros)), nil)
}

// siblingWins reports whether block a beats block b when both sit at the same
// height on the same parent: more work first, then the lower hash.
func siblingWins(a, b Block) bool {
	if cmp := BlockWork(a).Cmp(BlockWork(b)); cmp != 0 {
		return cmp > 0
	}
	return a.Hash < b.Hash
}

func chainWorkKey(height int64) []byte {
	return []byte(fmt.Sprintf("%016d", height))
}

// readChainWork returns the total work through `height`, provided the entry
// was written for the block with this hash.
func readChainWork(dbTx *bbolt.Tx, height int64, blockHash string) *big.Int {
	bucket := dbTx.Bucket(chainWorkBucket)
	if bucket == nil {
		return nil
	}
	raw := bucket.Get(chainWorkKey(height))
	if raw == nil {
		return nil
	}
	var entry chainWorkEntry
	if json.Unmarshal(raw, &entry) != nil || entry.Hash != blockHash {
		return nil
	}
	work, ok := new(big.Int).SetString(entry.Work, 10)
	if !ok {
		return nil
	}
	return work
}

// storeChainWork records the total work through `block`, which must be saved
// in the same write transaction. A missing parent entry is rebuilt on demand.
func storeChainWork(dbTx *bbolt.Tx, block Block) error {
	bucket, err := dbTx.CreateBucketIfNotExists(chainWorkBucket)
	if err != nil {
		return err
	}
	total := BlockWork(block)
	if block.Index > 0 {
		parentWork := readChainWork(dbTx, block.Index-1, block.PrevHash)
		if parentWork == nil {
			parentWork = sumChainWorkFromBlocks(dbTx, block.Index-1)
		}
		total.Add(total, parentWork)
	}
	raw, _ := json.Marshal(chainWorkEntry{Hash: block.Hash, Work: total.String()})
	return bucket.Put(chainWorkKey(block.Index), raw)
}

// sumChainWorkFromBlocks adds up the work of every stored block up to and
// including `height` straight from the blocks themselves.
func sumChainWorkFromBlocks(dbTx *bbolt.Tx, height int64) *big.Int {
	total := new(big.Int)
	bBlocks := dbTx.Bucket(blocksBucket)
	if bBlocks == nil {
		return total
	}
	for h := int64(0); h <= height; h++ {
		data := bBlocks.Get(chainWorkKey(h))
		if data == nil {
			continue
		}
		var block Block
		if json.Unmarshal(data, &block) == nil {
			total.Add(total, BlockWork(block))
		}
	}
	return total
}

// EnsureChainWorkIndex rebuilds the total-work records from the stored blocks
// when the record for the current tip is missing or belongs to another block.
func EnsureChainWorkIndex() {
	if GlobalBoltEngine == nil {
		return
	}
	tipHeight := atomic.LoadInt64(&localHeightState)
	rebuilt := false
	_ = GlobalBoltEngine.Update(func(dbTx *bbolt.Tx) error {
		bBlocks := dbTx.Bucket(blocksBucket)
		if bBlocks == nil {
			return nil
		}
		var tip Block
		if data := bBlocks.Get(chainWorkKey(tipHeight)); data == nil || json.Unmarshal(data, &tip) != nil {
			return nil
		}
		if readChainWork(dbTx, tipHeight, tip.Hash) != nil {
			return nil
		}
		bucket, err := dbTx.CreateBucketIfNotExists(chainWorkBucket)
		if err != nil {
			return err
		}
		total := new(big.Int)
		for h := int64(0); h <= tipHeight; h++ {
			data := bBlocks.Get(chainWorkKey(h))
			if data == nil {
				continue
			}
			var block Block
			if json.Unmarshal(data, &block) != nil {
				continue
			}
			total.Add(total, BlockWork(block))
			raw, _ := json.Marshal(chainWorkEntry{Hash: block.Hash, Work: total.String()})
			if err := bucket.Put(chainWorkKey(h), raw); err != nil {
				return err
			}
		}
		rebuilt = true
		return nil
	})
	if rebuilt {
		log.Printf("🧮 [CHAIN WORK] Total-work records rebuilt through block #%d.\n", tipHeight)
	}
}

// chainWorkThrough returns the total work of this node's chain up to and
// including `height`.
func chainWorkThrough(height int64) *big.Int {
	total := new(big.Int)
	if GlobalBoltEngine == nil || height < 0 {
		return total
	}
	_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
		bBlocks := dbTx.Bucket(blocksBucket)
		if bBlocks == nil {
			return nil
		}
		var block Block
		if data := bBlocks.Get(chainWorkKey(height)); data != nil && json.Unmarshal(data, &block) == nil {
			if recorded := readChainWork(dbTx, height, block.Hash); recorded != nil {
				total = recorded
				return nil
			}
		}
		total = sumChainWorkFromBlocks(dbTx, height)
		return nil
	})
	return total
}

// TotalChainWork is the total work of this node's chain at its current tip.
func TotalChainWork() *big.Int {
	return chainWorkThrough(atomic.LoadInt64(&localHeightState))
}

// peerChainStatus asks a node for its height and its total work. work is nil
// when the node runs older software that does not report it; height is 0 when
// the node did not answer.
func peerChainStatus(gateway string) (height int64, work *big.Int) {
	client := &http.Client{Timeout: 2 * time.Second}
	base := strings.TrimSuffix(gateway, "/")
	// The small answer first; nodes that predate it are asked the old way,
	// which sends their whole newest block.
	for _, path := range []string{"/tip", "/block"} {
		resp, err := client.Get(base + path)
		if err != nil {
			return 0, nil
		}
		if resp.StatusCode == http.StatusNotFound && path == "/tip" {
			resp.Body.Close()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return 0, nil
		}
		var data map[string]interface{}
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&data)
		resp.Body.Close()
		if decodeErr != nil {
			return 0, nil
		}
		if h, ok := data["current_height"].(float64); ok {
			height = int64(h)
		}
		if text, ok := data["total_work"].(string); ok && len(text) > 0 && len(text) <= 80 {
			if parsed, valid := new(big.Int).SetString(text, 10); valid && parsed.Sign() > 0 {
				work = parsed
			}
		}
		return height, work
	}
	return 0, nil
}

// peerChainIsBetter decides whether a peer's chain should be preferred to
// ours. Work decides when the peer reports it; otherwise height does.
func peerChainIsBetter(peerHeight int64, peerWork *big.Int, localHeight int64, localWork *big.Int) bool {
	if peerHeight <= 0 {
		return false
	}
	if peerWork != nil {
		return peerWork.Cmp(localWork) > 0
	}
	return peerHeight > localHeight
}

// candidateBlockProofValid checks what can be checked about a block without
// its parent's state: that its hash is really its hash, and that the hash
// meets the difficulty the block claims. Work claimed this way is work done.
func candidateBlockProofValid(block Block) bool {
	claimed := block.Difficulty
	if !IsHeartbeatHeight(block.Index) {
		if block.Index <= 10 {
			claimed = 2
		} else if claimed < 4 {
			return false
		}
	}
	return block.Hash == CalculateHash(block) && BlockProofValid(block, claimed)
}

// measureCandidateBranch downloads a peer's branch above the shared block and
// adds up its proven work. It stops as soon as the branch has more work than
// `workToBeat`, at the first block that does not link or prove its work, or
// at the size limit. It reports the work found and whether it beat the target.
func measureCandidateBranch(fetchBatch func(start int64) []Block, ancestorHeight int64, ancestorHash string, peerTipHeight int64, workToBeat *big.Int) (*big.Int, bool) {
	total := new(big.Int)
	previousHash := ancestorHash
	next := ancestorHeight + 1
	examined := 0
	for next <= peerTipHeight && examined < maxCandidateBranchBlocks {
		batch := fetchBatch(next)
		if len(batch) == 0 {
			break
		}
		advanced := false
		for _, block := range batch {
			if block.Index != next {
				continue
			}
			if strings.TrimSpace(block.PrevHash) != previousHash || !candidateBlockProofValid(block) {
				return total, total.Cmp(workToBeat) > 0
			}
			total.Add(total, BlockWork(block))
			if total.Cmp(workToBeat) > 0 {
				return total, true
			}
			previousHash = block.Hash
			next++
			examined++
			advanced = true
			if next > peerTipHeight || examined >= maxCandidateBranchBlocks {
				break
			}
		}
		if !advanced {
			break
		}
	}
	return total, total.Cmp(workToBeat) > 0
}

// rollbackChainToHeightLocked removes every block above `target`, giving back
// the coins they spent and taking back the ones they created. The caller must
// hold StateMutex. It returns the removed blocks, oldest first, so they can be
// put back if the replacement turns out to be worse.
func rollbackChainToHeightLocked(target int64) []Block {
	var removed []Block
	chainWriteMutex.Lock()
	defer chainWriteMutex.Unlock()
	tip := GetLatestBlockInternal()
	if GlobalBoltEngine == nil || target >= tip.Index {
		return removed
	}
	updateErr := GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		bMeta := tx.Bucket(metaBucket)
		bUTXO := tx.Bucket(utxoBucket)
		if bBlocks == nil || bMeta == nil || bUTXO == nil {
			return fmt.Errorf("required buckets missing")
		}
		for h := tip.Index; h > target; h-- {
			paddedKey := chainWorkKey(h)
			if data := bBlocks.Get(paddedKey); data != nil {
				var block Block
				if json.Unmarshal(data, &block) == nil {
					// Newest block first, so coins spent and re-created across the
					// removed blocks unwind in the right order.
					undoBlockCoinChanges(tx, bUTXO, block)
					if block.UncleHash != "" &&
						block.UncleHash != "0000000000000000000000000000000000000000000000000000000000000000" &&
						cleanAddr(block.UncleMiner) != "NONE" &&
						cleanAddr(block.UncleMiner) != "" {
						deleteUncleUTXO(bUTXO, block.Index, block.UncleMiner)
					}
					removed = append(removed, block)
				}
			}
			_ = bBlocks.Delete(paddedKey)
		}
		return bMeta.Put([]byte("height"), chainWorkKey(target))
	})
	if updateErr != nil {
		// Nothing was removed: the database undid the whole attempt, so the
		// tip is where it was.
		log.Printf("🚨 [CHAIN WORK] Could not roll the chain back to block #%d: %v\n", target, updateErr)
		return nil
	}
	atomic.StoreInt64(&localHeightState, target)
	RebuildStateBalanceCacheInternal()
	// Collected newest first; hand them back oldest first.
	for i, j := 0, len(removed)-1; i < j; i, j = i+1, j-1 {
		removed[i], removed[j] = removed[j], removed[i]
	}
	// The transfers in the removed blocks go back to waiting. Their senders
	// were told they were sent, and unless the blocks that replace these
	// carry them too, they would otherwise be silently lost. Any that the
	// new blocks do carry are dropped again when a miner next fills a block.
	go func(blocks []Block) {
		for _, block := range blocks {
			if len(block.Transactions) > 1 {
				for _, transfer := range block.Transactions[1:] {
					_ = AdmitTransactionToMempool(transfer)
				}
			}
		}
	}(removed)
	return removed
}

// restoreChainLocked puts back blocks taken off by rollbackChainToHeightLocked
// after first removing whatever replaced them. The caller must hold StateMutex.
func restoreChainLocked(ancestorHeight int64, original []Block) {
	rollbackChainToHeightLocked(ancestorHeight)
	for _, block := range original {
		if !SaveBlockToStorageInternal(block) {
			log.Printf("🚨 [CHAIN WORK] Could not put back block #%d; the next sync pass will recover.\n", block.Index)
			return
		}
	}
}
