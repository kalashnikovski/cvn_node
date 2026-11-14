package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
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

var BlockchainFile = "cvn_mainnet.db"
var CustomMinerAddress string
var IsSyncingActive int32 = 0

var BalanceCacheMutex sync.RWMutex

// StateBalanceCache holds every address's balance in units.
var StateBalanceCache = make(map[string]int64)
var MempoolMatrix = &MempoolType{Transactions: make(map[string]Transaction)}
var StateMutex sync.RWMutex

func cleanAddr(a string) string {
	return strings.TrimSpace(a)
}

// ValidateBlockByVersion accepts the block versions the first test network
// has always accepted. The launch format is stricter (see block_format.go).
func ValidateBlockByVersion(b Block) bool {
	return b.Version <= 2
}

func CalculateHash(b Block) string {
	return CalculateHashWithTxDigest(b, BlockTransactionsDigest(b.Index, b.Transactions))
}

// CalculateHashWithTxDigest hashes a block header given the digest of its
// transactions. The miner computes that digest once per template instead of
// once per nonce.
func CalculateHashWithTxDigest(b Block, txHashStr string) string {
	if Params.StrictFormat {
		digest := sha256.Sum256([]byte(blockHeaderEncoding(b, txHashStr)))
		return hex.EncodeToString(digest[:])
	}

	uHash := strings.TrimSpace(b.UncleHash)
	if uHash == "" {
		uHash = "0000000000000000000000000000000000000000000000000000000000000000"
	}
	uMiner := strings.TrimSpace(b.UncleMiner)
	if uMiner == "" {
		uMiner = "NONE"
	}
	gMatrix := strings.TrimSpace(b.GuardMatrix)
	if gMatrix == "" {
		gMatrix = "NONE"
	}

	inputStr := fmt.Sprintf("%d-%d-%d-%s-%s-%d-%s-%d-%s-%s",
		int(b.Version),
		int64(b.Index),
		int64(b.Timestamp),
		strings.TrimSpace(b.PrevHash),
		txHashStr,
		int64(b.Difficulty),
		gMatrix,
		int64(b.Nonce),
		uHash,
		uMiner,
	)

	h := sha256.New()
	h.Write([]byte(inputStr))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func CalculateBlockReward(blockIndex int64) float64 {
	if blockIndex < 0 {
		return 0.0
	}
	if blockIndex <= 5256000 {
		return 50.0
	} else if blockIndex <= 10512000 {
		return 25.0
	} else if blockIndex <= 15768000 {
		return 12.5
	} else if blockIndex <= 21024000 {
		return 6.25
	}
	return 5.00
}

func GetGlobalMeshMaxHeight(seedURL string) int64 {
	cleanURL := strings.TrimSuffix(seedURL, "/") + "/block"
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(cleanURL)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0
	}
	if heightVal, exists := data["current_height"]; exists {
		if h, ok := heightVal.(float64); ok {
			return int64(h)
		}
	}
	return 0
}

var DeferredBlockQueue = make(chan Block, 32)

func deleteUncleUTXO(bUTXO *bbolt.Bucket, height int64, uncleMiner string) {
	trimmed := cleanAddr(uncleMiner)
	if trimmed == "" || trimmed == "NONE" {
		return
	}
	key := []byte(fmt.Sprintf("UNCLE_%d_%s", height, trimmed))
	_ = bUTXO.Delete(key)
}

func ProcessIncomingNetworkBlock(incomingBlock Block) {
	StateMutex.Lock()
	currentTip := GetLatestBlockInternal()
	StateMutex.Unlock()

	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		select {
		case DeferredBlockQueue <- incomingBlock:
			log.Printf("📥 [DEFERRED QUEUE] Cached block #%d into bounded memory channel during active re-sync pass.\n", incomingBlock.Index)
		default:
			log.Printf("⚠️ [QUEUE SATURATED] Deferred block buffer capacity (32 slots) reached. Dropping block #%d.\n", incomingBlock.Index)
		}
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
		go TriggerActiveReSyncPass()
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
		go TriggerActiveReSyncPass()
		return
	}
	if incomingBlock.Index == currentTip.Index+1 && incomingBlock.PrevHash != currentTip.Hash {
		go TriggerActiveReSyncPass()
		return
	}
	if !ValidateBlockSignatures(incomingBlock) {
		return
	}
	if incomingBlock.Index == currentTip.Index+1 {
		if SaveBlockToStorageInternal(incomingBlock) {
			atomic.StoreInt64(&localHeightState, incomingBlock.Index)
			go BroadcastNewBlock(incomingBlock)
		}
	}
}
func ProcessIncomingWireBlockWithDelay(incomingBlock Block) {
	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		return
	}
	StateMutex.Lock()
	currentTip := GetLatestBlockInternal()
	StateMutex.Unlock()
	if incomingBlock.Index == currentTip.Index+1 {
		if incomingBlock.PrevHash != currentTip.Hash {
			go TriggerActiveReSyncPass()
			return
		}
		if !ValidateBlockSignatures(incomingBlock) {
			return
		}
		if SaveBlockToStorageInternal(incomingBlock) {
			atomic.StoreInt64(&localHeightState, incomingBlock.Index)
			SignalMinerReset()
			go BroadcastNewBlock(incomingBlock)
		}
		return
	}
	if incomingBlock.Index == currentTip.Index && incomingBlock.Hash != currentTip.Hash &&
		incomingBlock.PrevHash == currentTip.PrevHash {
		var winner, loser Block
		if siblingWins(currentTip, incomingBlock) {
			winner = currentTip
			loser = incomingBlock
		} else {
			winner = incomingBlock
			loser = currentTip
		}
		// Deterministic tie-break: more work wins, then the lower hash. If the incoming
		// block wins, replace our tip with it now instead of staying on our own
		// block until the next height forces a re-sync.
		if winner.Hash == incomingBlock.Hash {
			StateMutex.Lock()
			liveTip := GetLatestBlockInternal()
			adopted := liveTip.Hash == currentTip.Hash &&
				ValidateBlockSignatures(incomingBlock) &&
				SaveBlockToStorageInternal(incomingBlock)
			StateMutex.Unlock()
			if !adopted {
				return
			}
			fmt.Printf("🔄 [TIP SWITCH] Height %d: adopted competing block %.16s (more work or lower hash) over local %.16s\n", incomingBlock.Index, incomingBlock.Hash, currentTip.Hash)
		} else if !ValidateBlockSignatures(incomingBlock) {
			// The losing block earns its miner the uncle share, so it must be a fully
			// valid block, not just something with the right hash prefix.
			return
		}
		RecordUncleCandidate(winner, loser)
		return
	}
	ProcessIncomingNetworkBlock(incomingBlock)
}

// RecordUncleCandidate queues the losing block of a same-height race so the
// miner of the next block can pay its 20% share. It only writes a local note;
// no coins move until a block's coinbase says so. It takes no StateMutex,
// because some callers already hold it.
func RecordUncleCandidate(winner Block, loser Block) {
	if GlobalBoltEngine == nil || winner.Hash == "" || loser.Hash == "" || winner.Hash == loser.Hash {
		return
	}
	var uncleMiner string
	if len(loser.Transactions) > 0 {
		uncleMiner = strings.TrimSpace(loser.Transactions[0].Witness)
	}
	if uncleMiner == "" && len(loser.Transactions) > 0 && len(loser.Transactions[0].Outputs) > 0 {
		uncleMiner = strings.TrimSpace(loser.Transactions[0].Outputs[0].Recipient)
	}
	if uncleMiner == "" || uncleMiner == "GENESIS_VOID_REWARD_POOL" {
		uncleMiner = "NONE"
	}
	if uncleMiner != "NONE" && uncleMiner != "" {
		_ = GlobalBoltEngine.Update(func(tx *bbolt.Tx) error {
			bUncle := tx.Bucket([]byte("uncle_ledger"))
			if bUncle == nil {
				return nil
			}
			uncleKey := []byte(fmt.Sprintf("UNCLE_METADATA_%d", winner.Index))
			// Keep the first uncle seen for a given winner; replace the record if the
			// winner at this height has changed (a lower hash arrived later).
			var existing map[string]string
			if raw := bUncle.Get(uncleKey); raw != nil {
				_ = json.Unmarshal(raw, &existing)
			}
			if existing["winning_hash"] != winner.Hash {
				metadataPayload := map[string]string{
					"winning_hash": winner.Hash,
					"uncle_hash":   loser.Hash,
					"uncle_miner":  uncleMiner,
				}
				metaBytes, _ := json.Marshal(metadataPayload)
				_ = bUncle.Put(uncleKey, metaBytes)
				// Record only. No coins are credited here; the uncle is paid by the next
				// block's coinbase (see GetPendingUncleForTip), which every node validates.
				fmt.Printf("💾 [UNCLE RECORDED] Height %d: losing block %.16s by %s queued for payout in block #%d\n", winner.Index, loser.Hash, uncleMiner, winner.Index+1)
			}
			return nil
		})
	}
}

// GetPendingUncleForTip returns the losing sibling of the current tip, if this
// node witnessed one. The miner of the next block names it in the header and
// pays its miner 20% of the block reward, keeping 80% for itself.
func GetPendingUncleForTip(tip Block) (uncleHash string, uncleMiner string, found bool) {
	if GlobalBoltEngine == nil || tip.Index <= 0 || tip.Hash == "" {
		return "", "", false
	}
	var meta map[string]string
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bUncle := tx.Bucket(uncleLedgerBucket)
		if bUncle == nil {
			return nil
		}
		if raw := bUncle.Get([]byte(fmt.Sprintf("UNCLE_METADATA_%d", tip.Index))); raw != nil {
			_ = json.Unmarshal(raw, &meta)
		}
		return nil
	})
	uncleHash = strings.TrimSpace(meta["uncle_hash"])
	uncleMiner = cleanAddr(meta["uncle_miner"])
	// Only valid while the recorded winner is still our tip.
	if meta["winning_hash"] != tip.Hash || uncleHash == "" || uncleHash == tip.Hash ||
		uncleHash == "0000000000000000000000000000000000000000000000000000000000000000" ||
		uncleMiner == "" || uncleMiner == "NONE" {
		return "", "", false
	}
	return uncleHash, uncleMiner, true
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
func TriggerActiveReSyncPass() {
	if !atomic.CompareAndSwapInt32(&IsSyncingActive, 0, 1) {
		return
	}
	log.Println("⏳ [SYNC GATEWAY ACTIVE] Consensus engine flag set: TRUE. Local mining loops safely PAUSED.")
	syncWatchdogTimer := time.NewTimer(90 * time.Second)
	watchdogDoneChannel := make(chan struct{})
	go func() {
		select {
		case <-syncWatchdogTimer.C:
			log.Println("🚨 [SYNC WATCHDOG TIMEOUT] Catch-up pass exceeded 90-second allocation limit! Enforcing fallback breakout.")
			atomic.StoreInt32(&IsSyncingActive, 0)
			drainAndProcessDeferredQueue()
		case <-watchdogDoneChannel:
			syncWatchdogTimer.Stop()
		}
	}()
	defer func() {
		close(watchdogDoneChannel)
		atomic.StoreInt32(&IsSyncingActive, 0)
		log.Println("🟩 [SYNC GATEWAY RELEASED] Consensus engine flag cleared: FALSE. Mining worker pool RESUMED.")
		drainAndProcessDeferredQueue()
	}()
	activeGlobalRecoveryGateways := append(seedSyncURLs(), Params.ExtraSyncURLs...)
	activeGlobalRecoveryGateways = append(activeGlobalRecoveryGateways, cachedPeerHTTPGateways(activeGlobalRecoveryGateways)...)
	client := &http.Client{Timeout: 3 * time.Second}
	var chosenTargetSeedURL string
	// A peer is a sync source only if its chain is better than ours: more total
	// work when it reports that, otherwise more blocks (older software). Our own
	// address can appear in the peer list; it is never better than itself.
	selectionTip := GetLatestBlockInternal()
	selectionWork := TotalChainWork()
	originalTotalWork := new(big.Int).Set(selectionWork)
	selectedHighestTargetTip := selectionTip.Index
	var bestPeerWork *big.Int
	considerSyncSource := func(gateway string) {
		peerHeight, peerWork := peerChainStatus(gateway)
		if !peerChainIsBetter(peerHeight, peerWork, selectionTip.Index, selectionWork) {
			return
		}
		better := chosenTargetSeedURL == ""
		if peerWork != nil {
			better = better || bestPeerWork == nil || peerWork.Cmp(bestPeerWork) > 0
		} else {
			better = better || (bestPeerWork == nil && peerHeight > selectedHighestTargetTip)
		}
		if better {
			chosenTargetSeedURL = gateway
			selectedHighestTargetTip = peerHeight
			bestPeerWork = peerWork
		}
	}
	PeerMapMutex.RLock()
	var freshPeerAddresses []string
	for activePeerAddress, lastSeenTimestamp := range ActivePeerMap {
		if time.Since(lastSeenTimestamp) <= 30*time.Second {
			freshPeerAddresses = append(freshPeerAddresses, activePeerAddress)
		}
	}
	PeerMapMutex.RUnlock()
	for _, activePeerAddress := range freshPeerAddresses {
		cleanHostIP, _, _ := net.SplitHostPort(activePeerAddress)
		considerSyncSource(Params.syncURL(cleanHostIP))
	}
	if chosenTargetSeedURL == "" {
		for _, endpoint := range activeGlobalRecoveryGateways {
			if skipSeedOnThisMachine(endpoint) && strings.HasSuffix(endpoint, ":"+Params.syncPortText()) {
				continue
			}
			considerSyncSource(endpoint)
		}
	}
	if chosenTargetSeedURL == "" {
		return
	}
	StateMutex.Lock()
	localTip := GetLatestBlockInternal()
	localHashesSnapshot := make(map[int64]string)
	_ = GlobalBoltEngine.View(func(tx *bbolt.Tx) error {
		bBlocks := tx.Bucket(blocksBucket)
		if bBlocks == nil {
			return nil
		}
		for h := localTip.Index; h >= 0; h-- {
			paddedKey := []byte(fmt.Sprintf("%016d", h))
			if data := bBlocks.Get(paddedKey); data != nil {
				var blk Block
				if json.Unmarshal(data, &blk) == nil {
					localHashesSnapshot[h] = blk.Hash
				}
			}
		}
		return nil
	})
	StateMutex.Unlock()
	scanHeight := localTip.Index
	var commonAncestorHeight int64 = -1
	for scanHeight >= 0 {
		localHash := localHashesSnapshot[scanHeight]
		remoteHash, remoteErr := fetchRemoteBlockHeader(client, chosenTargetSeedURL, scanHeight)
		if remoteErr != nil {
			remoteHash = fetchRemoteBlockHeaderViaP2P(scanHeight)
			if remoteHash != "" {
				remoteErr = nil
			}
		}
		if remoteErr == nil && localHash != "" && localHash == remoteHash {
			commonAncestorHeight = scanHeight
			break
		}
		scanHeight--
	}
	if commonAncestorHeight == -1 {
		log.Println("🚨 [SYNC FAULT] Unable to establish common ancestor over HTTP or P2P Mesh channels.")
		return
	}
	StateMutex.Lock()
	shadowTip := GetLatestBlockInternal()
	var removedLocalBlocks []Block
	if commonAncestorHeight < shadowTip.Index {
		if depth := shadowTip.Index - commonAncestorHeight; !reorgDepthAllowed(depth) {
			StateMutex.Unlock()
			log.Printf("🛑 [REORG LIMIT] %s offers a chain that parts from ours %d blocks back. This node does not replace more than %d of its own blocks, so it keeps its chain. If this node was left behind on a dead branch, stop it, delete %s and start again.\n", chosenTargetSeedURL, depth, Params.Economics.ReorgLimitBlocks, Params.DatabaseFile)
			return
		}
		// Switching would discard our blocks above the shared one. Do it only if the
		// peer's branch proves more work than those blocks hold.
		localBranchWork := new(big.Int).Sub(chainWorkThrough(shadowTip.Index), chainWorkThrough(commonAncestorHeight))
		StateMutex.Unlock()
		fetchBatch := func(start int64) []Block {
			var batch []Block
			resp, err := client.Get(fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(chosenTargetSeedURL, "/"), start))
			if err != nil {
				return requestBatchOverTCPMeshNatively(start)
			}
			if resp.StatusCode == http.StatusOK {
				_ = json.NewDecoder(resp.Body).Decode(&batch)
			}
			resp.Body.Close()
			return batch
		}
		candidateWork, candidateWins := measureCandidateBranch(fetchBatch, commonAncestorHeight, localHashesSnapshot[commonAncestorHeight], selectedHighestTargetTip, localBranchWork)
		if !candidateWins {
			log.Printf("⚖️ [CHAIN WORK] Keeping our chain: %s offers work %s above block #%d, ours holds %s.\n", chosenTargetSeedURL, candidateWork.String(), commonAncestorHeight, localBranchWork.String())
			return
		}
		StateMutex.Lock()
		if GetLatestBlockInternal().Hash != shadowTip.Hash {
			// Our tip moved while the peer's branch was being measured; start over
			// on the next pass rather than act on a stale comparison.
			StateMutex.Unlock()
			return
		}
		log.Printf("⚖️ [CHAIN WORK] Switching to the branch from %s above block #%d: it proves more work than our %d block(s) there.\n", chosenTargetSeedURL, commonAncestorHeight, shadowTip.Index-commonAncestorHeight)
		removedLocalBlocks = rollbackChainToHeightLocked(commonAncestorHeight)
	}
	// Release here and take the lock only around each batch below. Previously the
	// lock was still held on several exits from the loop, and the deferred queue
	// drain then blocked on it forever, freezing the miner, dashboard and API.
	StateMutex.Unlock()
	for {
		currentLocalTip := GetLatestBlockInternal()
		nextNeededBlock := currentLocalTip.Index + 1
		if nextNeededBlock > selectedHighestTargetTip {
			break
		}
		requestURL := fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(chosenTargetSeedURL, "/"), nextNeededBlock)
		resp, err := client.Get(requestURL)
		var incomingBatch []Block
		if err != nil {
			log.Printf("🔌 [SYNC] Block download from %s failed. Asking connected nodes for block #%d instead.\n", chosenTargetSeedURL, nextNeededBlock)
			incomingBatch = requestBatchOverTCPMeshNatively(nextNeededBlock)
			if len(incomingBatch) == 0 {
				break
			}
		} else {
			if resp.StatusCode == http.StatusOK {
				_ = json.NewDecoder(resp.Body).Decode(&incomingBatch)
			}
			resp.Body.Close()
			if len(incomingBatch) == 0 {
				break
			}
		}
		StateMutex.Lock()
		syncFilterRejected := false
		for _, block := range incomingBatch {
			if block.Index == GetLatestBlockInternal().Index+1 {
				if ValidateBlockSignatures(block) {
					if block.UncleHash == "" {
						block.UncleHash = "0000000000000000000000000000000000000000000000000000000000000000"
					}
					if block.UncleMiner == "" {
						block.UncleMiner = "NONE"
					}
					SaveBlockToStorageInternal(block)
				} else {
					log.Printf("⚠️ [SYNC EXCLUSION FILTER] Rejected invalid block layout sequence at height #%d\n", block.Index)
					syncFilterRejected = true
					break
				}
			}
		}
		StateMutex.Unlock()
		if syncFilterRejected {
			break
		}
		// Stop if the batch moved us nowhere, rather than re-requesting it forever.
		if GetLatestBlockInternal().Index < nextNeededBlock {
			log.Printf("⚠️ [SYNC STALLED] No block applied at height #%d from %s. Ending this pass.\n", nextNeededBlock, chosenTargetSeedURL)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(removedLocalBlocks) > 0 && TotalChainWork().Cmp(originalTotalWork) < 0 {
		// The peer's branch did not hold up under full validation (or the peer
		// went away), and what we have now is worse than what we gave up.
		log.Printf("↩️ [CHAIN WORK] The new branch ended with less work than our previous chain. Restoring our %d block(s) above #%d.\n", len(removedLocalBlocks), commonAncestorHeight)
		StateMutex.Lock()
		restoreChainLocked(commonAncestorHeight, removedLocalBlocks)
		StateMutex.Unlock()
	}
}
func drainAndProcessDeferredQueue() {
	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		log.Println("⚠️ [RE-ENTRY BLOCK] Sync engine remains active. Deferring block channel queue drainage pass.")
		return
	}
	StateMutex.Lock()
	defer StateMutex.Unlock()
	log.Println("🔄 [DRAIN PHASE INITIALIZED] Sequential block deployment pass streaming from cached memory channel...")
	queueLength := len(DeferredBlockQueue)
	for i := 0; i < queueLength; i++ {
		select {
		case cachedBlock := <-DeferredBlockQueue:
			currentTip := GetLatestBlockInternal()
			if cachedBlock.Index == currentTip.Index+1 {
				if ValidateBlockSignatures(cachedBlock) && cachedBlock.PrevHash == currentTip.Hash {
					if SaveBlockToStorageInternal(cachedBlock) {
						log.Printf("🟩 [QUEUE RECOVERY SUCCESS] Sequentially applied buffered block #%d to disk map storage.\n", cachedBlock.Index)
						atomic.StoreInt64(&localHeightState, cachedBlock.Index)
						go BroadcastNewBlock(cachedBlock)
					}
				}
			}
		default:
			return
		}
	}
}
func GetMedianTimePast(lastBlock Block) int64 {
	var timestamps []int64
	currentScanBlock := lastBlock
	for i := 0; i < 11; i++ {
		timestamps = append(timestamps, currentScanBlock.Timestamp)
		if currentScanBlock.Index == 0 {
			break
		}
		currentScanBlock = GetBlockByHeightFromDB(currentScanBlock.Index - 1)
		if currentScanBlock.Hash == "" {
			break
		}
	}
	for i := 0; i < len(timestamps)-1; i++ {
		for j := 0; j < len(timestamps)-i-1; j++ {
			if timestamps[j] > timestamps[j+1] {
				timestamps[j], timestamps[j+1] = timestamps[j+1], timestamps[j]
			}
		}
	}
	if len(timestamps) == 0 {
		return time.Now().Unix()
	}
	return timestamps[len(timestamps)/2]
}

const (
	RetargetWindowSize    = 15
	DampeningPercentage   = 20
	TargetBlockTimeWindow = 45
)

func CalculateMedianElapsedTime(lastBlock Block) int64 {
	var intervals []int64
	currentScanBlock := lastBlock
	for i := 0; i < RetargetWindowSize; i++ {
		if currentScanBlock.Index == 0 {
			break
		}
		parentBlock := GetBlockByHeightFromDB(currentScanBlock.Index - 1)
		if parentBlock.Hash == "" {
			break
		}
		delta := currentScanBlock.Timestamp - parentBlock.Timestamp
		if delta > 0 {
			intervals = append(intervals, delta)
		}
		currentScanBlock = parentBlock
	}
	if len(intervals) == 0 {
		return TargetBlockTimeWindow
	}
	for i := 0; i < len(intervals)-1; i++ {
		for j := 0; j < len(intervals)-i-1; j++ {
			if intervals[j] > intervals[j+1] {
				intervals[j], intervals[j+1] = intervals[j+1], intervals[j]
			}
		}
	}
	return intervals[len(intervals)/2]
}
func CalculateNextDifficulty(lastBlock Block, nextBlockTimestamp int64) int {
	if IsHeartbeatHeight(lastBlock.Index + 1) {
		return CalculateHeartbeatDifficulty(lastBlock, nextBlockTimestamp)
	}
	if lastBlock.Index < 10 {
		return 2
	}
	currentDiff := int(lastBlock.Difficulty)
	if currentDiff < 4 {
		return 4
	}
	mtpFloor := GetMedianTimePast(lastBlock)
	if nextBlockTimestamp <= mtpFloor {
		return currentDiff + 1
	}
	medianElapsed := CalculateMedianElapsedTime(lastBlock)
	lowThreshold := int64((TargetBlockTimeWindow * (100 - DampeningPercentage)) / 100)
	highThreshold := int64((TargetBlockTimeWindow * (100 + DampeningPercentage)) / 100)
	if medianElapsed < lowThreshold {
		return currentDiff + 1
	} else if medianElapsed > highThreshold {
		decreasedDiff := currentDiff - 1
		if decreasedDiff < 4 {
			return 4
		}
		return decreasedDiff
	}
	return currentDiff
}
func ValidateBlockSignatures(b Block) bool {
	if !ValidateBlockByVersion(b) {
		fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d rejected by version dispatcher gateway rule.\n", b.Index)
		return false
	}
	if err := checkBlockFormat(b); err != nil {
		fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d: %v\n", b.Index, err)
		return false
	}
	if b.Index > 0 {
		parentBlock := GetBlockByHeightFromDB(b.Index - 1)
		if parentBlock.Hash != "" {
			mtpFloor := GetMedianTimePast(parentBlock)
			if b.Timestamp <= mtpFloor {
				fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d timestamp (%d) is behind historical MTP floor (%d). Exploit blocked.\n", b.Index, b.Timestamp, mtpFloor)
				return false
			}
			if b.Timestamp > time.Now().Unix()+7200 {
				fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d timestamp drifted too far into the future. Exploit blocked.\n", b.Index)
				return false
			}
			if IsHeartbeatHeight(b.Index) {
				// Heartbeat blocks must move time forward, and may not be dated far
				// ahead of the clock: a later date is what eases difficulty.
				if b.Timestamp <= parentBlock.Timestamp {
					fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d is not dated after its parent.\n", b.Index)
					return false
				}
				if b.Timestamp > time.Now().Unix()+HeartbeatMaxFutureDriftSeconds {
					fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d is dated more than %d s ahead of this node's clock.\n", b.Index, HeartbeatMaxFutureDriftSeconds)
					return false
				}
			}
		}
	}
	var expectedDiff int
	if b.Index <= 10 && !IsHeartbeatHeight(b.Index) {
		expectedDiff = 2
	} else {
		parentBlock := GetBlockByHeightFromDB(b.Index - 1)
		if parentBlock.Hash == "" {
			if IsHeartbeatHeight(b.Index) {
				// Heartbeat difficulty cannot be derived without the parent.
				return false
			}
			parentBlock = Block{Index: b.Index - 1, Difficulty: b.Difficulty}
		}
		expectedDiff = CalculateNextDifficulty(parentBlock, b.Timestamp)
	}
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
	// Without transfers to check (blocks before the transaction rules), the
	// accounting is the reward alone.
	accounting := accountForBlock(b.Index, 0, 0)
	if IsTxForkHeight(b.Index) {
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
				fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d does not attach to the local tip, so its transfers cannot be checked.\n", b.Index)
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
				fmt.Printf("🚨 CONSENSUS REJECTION: Block #%d: %v\n", b.Index, err)
				isBlockValid = false
				return nil
			}
			accounting = checked
			return nil
		})
	} else if GlobalBoltEngine != nil {
		_ = GlobalBoltEngine.View(func(dbTx *bbolt.Tx) error {
			bUTXO := dbTx.Bucket([]byte("utxo"))
			if bUTXO == nil {
				isBlockValid = false
				return nil
			}
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
	// What the block pays apart from fees: the new reward plus the vault's payout.
	rewardUnits := accounting.SubsidyUnits()
	forkBlockFeeUnits := accounting.MinerFeeUnits
	// When there is nothing to share, no uncle share is paid, and such a block is
	// checked like any other.
	if rewardUnits/5 > 0 && b.UncleHash != "" && b.UncleHash != "0000000000000000000000000000000000000000000000000000000000000000" && cleanAddr(b.UncleMiner) != "NONE" && cleanAddr(b.UncleMiner) != "" {
		if len(coinbaseOutputs) != 2 {
			fmt.Println("🚨 CONSENSUS REJECTION: Uncle transaction layout requires exactly two coinbase outputs.")
			return false
		}
		// An uncle is a sibling of this block's parent, so it can never be the parent itself.
		if strings.TrimSpace(b.UncleHash) == strings.TrimSpace(b.PrevHash) {
			fmt.Println("🚨 CONSENSUS REJECTION: Uncle hash must not equal the parent block hash.")
			return false
		}
		// The uncle's miner receives a fifth of the reward and vault payout; this
		// block's miner receives the rest and may add its share of the fees.
		uncleShareUnits := rewardUnits / 5
		mainShareUnits := rewardUnits - uncleShareUnits
		mainPayoutUnits := coinbaseOutputs[0].Amount.Units()
		unclePayoutUnits := coinbaseOutputs[1].Amount.Units()
		if mainPayoutUnits < mainShareUnits || mainPayoutUnits > mainShareUnits+forkBlockFeeUnits {
			fmt.Printf("🚨 CONSENSUS REJECTION: Block miner's share is %s, expected %s plus fees of at most %s.\n", FormatUnits(mainPayoutUnits), FormatUnits(mainShareUnits), FormatUnits(forkBlockFeeUnits))
			return false
		}
		if unclePayoutUnits != uncleShareUnits {
			fmt.Printf("🚨 CONSENSUS REJECTION: Uncle miner's share is %s, expected %s.\n", FormatUnits(unclePayoutUnits), FormatUnits(uncleShareUnits))
			return false
		}
		if cleanAddr(coinbaseOutputs[1].Recipient) != cleanAddr(b.UncleMiner) {
			fmt.Printf("🚨 CONSENSUS REJECTION: Second reward output pays %s, but the block names %s as the uncle's miner.\n", cleanAddr(coinbaseOutputs[1].Recipient), b.UncleMiner)
			return false
		}
	} else {
		if totalCoinbaseUnits > rewardUnits+forkBlockFeeUnits {
			fmt.Printf("🚨 EMISSION BLOCKED: Block pays %s, allowed %s.\n", FormatUnits(totalCoinbaseUnits), FormatUnits(rewardUnits+forkBlockFeeUnits))
			return false
		}
	}
	return true
}
func requestBatchOverTCPMeshNatively(height int64) []Block {
	PeerMapMutex.RLock()
	var fallbackTargets []string
	for targetIP := range ActivePeerMap {
		fallbackTargets = append(fallbackTargets, targetIP)
	}
	PeerMapMutex.RUnlock()
	for _, endpointHost := range fallbackTargets {
		conn, err := net.DialTimeout("tcp", endpointHost, 3*time.Second)
		if err == nil {
			lengthBuf := make([]byte, 4)
			payloadCommand := fmt.Sprintf("GET_BATCH:%d", height)
			payloadBytes := []byte(payloadCommand)
			binary.BigEndian.PutUint32(lengthBuf, uint32(len(payloadBytes)))
			_, _ = conn.Write(lengthBuf)
			_, _ = conn.Write(payloadBytes)
			_ = conn.SetReadDeadline(time.Now().Add(6 * time.Second))
			var frameLength uint32
			err = binary.Read(conn, binary.BigEndian, &frameLength)
			if err == nil && frameLength <= MaxPayloadFrameSize { // 👑 CHANNELS EXPANDED PARITY PATCH
				payloadBuffer := make([]byte, frameLength)
				_, err = io.ReadFull(conn, payloadBuffer)
				if err == nil {
					var blocks []Block
					if json.Unmarshal(payloadBuffer, &blocks) == nil && len(blocks) > 0 {
						conn.Close()
						return blocks
					}
				}
			}
			conn.Close()
		}
	}
	return nil
}
func fetchRemoteBlockHeaderViaP2P(height int64) string {
	PeerMapMutex.RLock()
	var fallbackTargets []string
	for targetIP := range ActivePeerMap {
		fallbackTargets = append(fallbackTargets, targetIP)
	}
	PeerMapMutex.RUnlock()
	for _, endpointHost := range fallbackTargets {
		conn, err := net.DialTimeout("tcp", endpointHost, 2*time.Second)
		if err == nil {
			lengthBuf := make([]byte, 4)
			payloadCommand := fmt.Sprintf("GET_BATCH:%d", height)
			payloadBytes := []byte(payloadCommand)
			binary.BigEndian.PutUint32(lengthBuf, uint32(len(payloadBytes)))
			_, _ = conn.Write(lengthBuf)
			_, _ = conn.Write(payloadBytes)
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			var frameLength uint32
			err = binary.Read(conn, binary.BigEndian, &frameLength)
			if err == nil && frameLength <= MaxPayloadFrameSize { // 👑 CHANNELS EXPANDED PARITY PATCH
				payloadBuffer := make([]byte, frameLength)
				_, err = io.ReadFull(conn, payloadBuffer)
				if err == nil {
					var blocks []Block
					if json.Unmarshal(payloadBuffer, &blocks) == nil && len(blocks) > 0 {
						conn.Close()
						return blocks[len(blocks)-1].Hash
					}
				}
			}
			conn.Close()
		}
	}
	return ""
}
func fetchRemoteBlockHeader(client *http.Client, seedURL string, height int64) (string, error) {
	requestURL := fmt.Sprintf("%s/blocks/batch?start=%d", strings.TrimSuffix(seedURL, "/"), height)
	resp, err := client.Get(requestURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("invalid response code: %d", resp.StatusCode)
	}
	var batch []Block
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		return "", err
	}
	for _, block := range batch {
		if block.Index == height {
			return block.Hash, nil
		}
	}
	return "", fmt.Errorf("block missing")
}
