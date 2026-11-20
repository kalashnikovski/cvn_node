package node

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// THE BUILT-IN MINER
//
// The miner that is part of this program: one worker for each processor
// thread it is allowed, each building a block on the newest one and trying
// nonces until one of them, or somebody else, finds the next block. The
// separate fast miner (fast_miner.go) does the same job quicker; when it is
// running these workers stand by.

// startBuiltInMiner starts one worker for each thread the miner may use.
func startBuiltInMiner() {
	miningMutex.Lock()
	miningContext = context.Background()
	miningMutex.Unlock()
	threads := miningThreads(os.Args)
	for i := 0; i < threads; i++ {
		miningWg.Add(1)
		go miningWorker(miningContext, int64(i))
	}
}

// miningWorker is one worker. It builds a block on the newest one, tries
// nonces until one has the work or the newest block changes, and begins
// again. A worker needs no telling when a block arrives from elsewhere: it
// looks at the newest block as it goes.
func miningWorker(ctx context.Context, workerID int64) {
	defer miningWg.Done()
	var nonce int64 = workerID * 100000000
	pinned := false
	for {
		if atomic.LoadInt32(&IsSyncingActive) == 1 || atomic.LoadInt32(&MiningHoldActive) == 1 || atomic.LoadInt32(&externalMinersConnected) > 0 || atomic.LoadInt32(&fastMinerActive) == 1 {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		StateMutex.RLock()
		parent := GetLatestBlockInternal()
		StateMutex.RUnlock()
		nextIndex := parent.Index + 1
		parentHash := parent.Hash
		// Blocks are spaced by proof of work, not by waiting. The tip read
		// above is the one mined on; it is not read again here.
		blockDate := minerBlockDate(parent, time.Now().Unix())
		// Peers reject blocks dated too far ahead of their clock. If the tip is
		// already dated ahead, wait for real time to catch up instead of stacking
		// another future-dated block on it.
		if mustWaitForClock(blockDate, time.Now().Unix()) {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		blockDifficulty := CalculateNextDifficulty(parent, blockDate)
		targetHex := HeartbeatTargetHex(blockDifficulty)
		// The free text in a block's header is empty in every block but the first.
		const headerText = ""
		contents := buildBlockContents(parent, CustomMinerAddress, blockDate, fmt.Sprintf("%d", nonce))
		// The transactions are fixed for this template, so digest them once.
		contentsDigest := BlockTransactionsDigest(nextIndex, contents)
		usesRandomX := powAlgorithmAt(nextIndex) == PowRandomX
		if usesRandomX && !pinned {
			pinned = true
			pinMiningThread(int(workerID))
		}
		var randomXKey []byte
		randomXCommitment := ""
		if usesRandomX {
			var keyKnown bool
			if randomXKey, keyKnown = powKeyFor(nextIndex); !keyKnown {
				time.Sleep(250 * time.Millisecond)
				continue
			}
			// Everything in the block except the nonce is fixed for this template,
			// so it is committed to once; only the nonce changes per attempt.
			randomXCommitment = CalculateHashWithTxDigest(Block{
				Version: 1, Index: nextIndex, Timestamp: blockDate, PrevHash: parentHash,
				Difficulty: blockDifficulty, GuardMatrix: headerText,
				UncleHash: emptyUncleHash, UncleMiner: emptyUncleMiner,
			}, contentsDigest)
		}
		templateBuilt := time.Now()
		for i := 0; i < 100000; i++ {
			// A RandomX hash takes milliseconds, so a hundred thousand of
			// them is minutes. The block's date, and the transfers in it,
			// are renewed at least this often.
			if usesRandomX && i%64 == 0 && time.Since(templateBuilt) > minerTemplateLife {
				break
			}
			nonce++
			atomic.AddUint64(&globalHashCount, 1)
			// Abandon the template if the tip moved or was replaced by a competing
			// block at the same height; otherwise we would mine on a stale parent.
			// A RandomX hash takes milliseconds, so there the tip is looked at every
			// few hashes: often enough to react at once, without a database read
			// per hash.
			if !usesRandomX || i%4 == 0 {
				// Asked by identifier alone, without reading the block (see
				// latestTipID). Only if that says the tip has moved is the
				// block itself read, to be sure before the work is dropped.
				StateMutex.RLock()
				tipHeight, tipHash := latestTipID()
				StateMutex.RUnlock()
				if tipHeight != (nextIndex-1) || tipHash != parentHash {
					StateMutex.RLock()
					tipNow := GetLatestBlockInternal()
					StateMutex.RUnlock()
					if tipNow.Index != (nextIndex-1) || tipNow.Hash != parentHash {
						break
					}
				}
			}
			// Under RandomX the nonce is four bytes.
			templateNonce := nonce
			if usesRandomX {
				templateNonce = int64(uint32(nonce))
			}
			candidate := Block{
				Version:      1,
				Index:        nextIndex,
				Timestamp:    blockDate,
				Transactions: contents,
				PrevHash:     parentHash,
				Hash:         "",
				Difficulty:   blockDifficulty,
				Nonce:        templateNonce,
				GuardMatrix:  headerText,
				UncleHash:    emptyUncleHash,
				UncleMiner:   emptyUncleMiner,
			}
			// Under RandomX the identifier is only needed for a nonce that has the
			// work, so it is computed then; under SHA-256 it is the work itself.
			identifier := ""
			if !usesRandomX {
				identifier = CalculateHashWithTxDigest(candidate, contentsDigest)
			}
			solved := false
			if usesRandomX {
				// The work is the RandomX hash of the mining blob under this height's key.
				blob, blobOK := powBlob(randomXCommitment, uint32(templateNonce))
				if workHex, ok := randomXMine(workerID, randomXKey, blob); ok && blobOK {
					solved = workHex <= targetHex
					if solved {
						identifier = CalculateHashWithTxDigest(candidate, contentsDigest)
					}
				} else {
					time.Sleep(200 * time.Millisecond)
				}
			} else {
				solved = identifier <= targetHex
			}
			if solved {
				StateMutex.RLock()
				storedTip := GetLatestBlockInternal()
				StateMutex.RUnlock()
				storedHeight := storedTip.Index
				if storedHeight == nextIndex-1 && storedTip.Hash != parentHash {
					break
				}
				// Several workers can find a block at the same height within a
				// moment of each other; only one of them may store it. Each
				// claims the height first, and a worker that finds the height
				// already claimed drops its block.
				//
				// The claim is first brought up to the height of the stored
				// chain (and never lowered), so that heights reached through
				// blocks from other nodes count; otherwise no worker could
				// claim the next height on a node that did not mine the last.
				for {
					cur := atomic.LoadUint64(&heightClaimedByMiner)
					if storedHeight < 0 || cur >= uint64(storedHeight) {
						break
					}
					if atomic.CompareAndSwapUint64(&heightClaimedByMiner, cur, uint64(storedHeight)) {
						break
					}
				}
				claimed := atomic.LoadUint64(&heightClaimedByMiner)
				if claimed >= uint64(nextIndex) || storedHeight >= nextIndex {
					break
				}
				// Claim this height: one worker succeeds.
				if !atomic.CompareAndSwapUint64(&heightClaimedByMiner, uint64(nextIndex-1), uint64(nextIndex)) {
					break
				}
				candidate.Hash = identifier
				fmt.Printf("\n🟩 [SUCCESS] Block #%d Mined by Worker %d | Hash: %s\n", candidate.Index, workerID, candidate.Hash[:16])
				// Our own block is checked exactly as a peer's would be before
				// it is stored: storing alone does not check its date or its
				// difficulty, and a block peers would refuse must not become
				// our tip. It is also placed the way a peer's is, with the
				// chain held still (placeOnTip), so that a block arriving
				// from elsewhere or a comparison of chains cannot change the
				// newest block between the check and the storing.
				if placeOnTip(candidate) {
					logMutex.Lock()
					logLine := fmt.Sprintf("[%s] 🎉 [SUCCESS] Solved valid block #%-3d | Hash: %s | Performance: %s", time.Now().Format("15:04:05"), candidate.Index, candidate.Hash[:16], formatHashrate(currentLiveHashrateTrack))
					liveTerminalEventLogs = append(liveTerminalEventLogs, logLine)
					logMutex.Unlock()
				} else {
					// The block was not stored: give the height back.
					atomic.StoreUint64(&heightClaimedByMiner, uint64(nextIndex-1))
				}
				break
			}
		}
	}
}

// buildBlockContents assembles what goes into a new block on top of `tip`:
// the reward transaction paying minerAddress, followed by the waiting transfers. It is used by the built-in
// miner and by the work server for external miners, so both build identical
// blocks. `uniqueness` only has to differ between attempts; it keeps reward
// transaction IDs distinct.
func buildBlockContents(tip Block, minerAddress string, timestamp int64, uniqueness string) []Transaction {
	nextIndex := tip.Index + 1
	coinbaseTxID := rewardTransactionID(nextIndex, uniqueness)
	var coinbaseOutputs []UTXOOutput
	// From the transaction fork on, waiting transfers that are valid on top of
	// this tip go into the block, and their fees are added to the miner's reward.
	var templateTransfers []Transaction
	templateTransfers, feeUnits := selectionForTip(tip.Hash, TxMaxPerBlock-1)
	// Everything is whole units, so the amounts written into the block are
	// exact. rewardUnits is the new reward plus the vault's payout for this
	// block; feeUnits becomes the miner's share of the fees.
	accounting := accountForBlock(nextIndex, feeUnits, vaultUnitsAfter(tip))
	rewardUnits := accounting.SubsidyUnits()
	feeUnits = accounting.MinerFeeUnits
	if rewardUnits+feeUnits >= minimumOutputUnits() {
		// An amount too small to be a coin is not paid; it stays in the vault.
		coinbaseOutputs = append(coinbaseOutputs, UTXOOutput{Recipient: minerAddress, Amount: UnitsAmount(rewardUnits + feeUnits)})
	}
	contents := []Transaction{
		{
			Version:   1,
			ID:        coinbaseTxID,
			Witness:   minerAddress,
			PublicKey: CoinbasePublicKeyMarker,
			Inputs:    []UTXOInput{},
			Outputs:   coinbaseOutputs,
			Timestamp: timestamp,
			Signature: CoinbaseSignatureMarker,
		},
	}
	contents = append(contents, templateTransfers...)
	return contents
}
