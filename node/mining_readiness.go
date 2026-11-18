package node

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MINING READINESS
//
// The miner must not work on top of an old block while the rest of the network
// has moved on. The heartbeat rule halves difficulty when no block has arrived
// for several minutes, so a node whose start-up sync broke off hours behind
// would otherwise mine dozens of minimum-difficulty blocks per minute on a
// private branch, show them as solved, and lose them all at the next re-sync.
//
// While the tip is fresh the miner runs normally. Once the tip is older than
// the stall threshold, the node first asks its peers how far they are:
//
//   - a peer is ahead: hold the miner and catch up;
//   - peers answer and none is ahead: the whole network has stalled, so mine
//     (this is what the stall rule exists for);
//   - nobody answers: hold for a while, then mine anyway, so a lone node or
//     the last miner on the network is never locked out.
//
// A hold that makes no progress is released after a limit, so a peer that
// reports a height it cannot actually serve cannot stop this node for good.

// MiningHoldActive is 1 while the miner is being held back. Mining workers
// check it the same way they check IsSyncingActive.
var MiningHoldActive int32

const (
	miningReadinessInterval = 5 * time.Second
	// A tip at least this old counts as stale. Same point at which the
	// heartbeat rule starts easing difficulty.
	miningStaleTipSeconds = HeartbeatStallAfterSeconds
	// How long to hold with no peer answering before mining anyway.
	miningHoldUnreachableLimit = int64(120)
	// How long to hold behind a peer that claims to be ahead while our own
	// height does not move. A node that really is ahead delivers blocks
	// within seconds of being asked, and each one starts this count again.
	// One that only says so, seed or not, costs a miner two minutes and no
	// more: nobody should be able to keep others from mining by claiming.
	miningHoldBehindLimit = int64(120)
)

var (
	miningHoldTrackedHeight int64 = -1
	miningHoldSinceUnix     int64
	miningHoldLastReason    string
)

// networkHeightHeard is the block the seeds were last heard to be at. It is
// only shown to the person at the miner window; no decision rests on it.
var networkHeightHeard int64

// miningHoldReasonShown is the reason for the current hold, for the miner
// window, which reads it from another goroutine.
var miningHoldReasonShown atomic.Value

// progressBar draws done out of total as a bar of the given width.
func progressBar(done, total int64, width int) string {
	filled := 0
	if total > 0 && done > 0 {
		filled = int(done * int64(width) / total)
	}
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat(".", width-filled) + "]"
}

// minerStatus says in a few words what the node is doing, for the miner
// window: mining, catching up (with how far it has got), or waiting and why.
// working is false while no mining is being done.
func minerStatus(localHeight int64) (text string, working bool) {
	syncing := atomic.LoadInt32(&IsSyncingActive) == 1
	held := atomic.LoadInt32(&MiningHoldActive) == 1
	if !syncing && !held {
		return "UP TO DATE", true
	}
	if target := atomic.LoadInt64(&networkHeightHeard); target > localHeight {
		return fmt.Sprintf("CATCHING UP: block #%d of #%d %s %d%%", localHeight, target,
			progressBar(localHeight, target, 20), localHeight*100/target), false
	}
	if syncing {
		return "CATCHING UP with the network", false
	}
	reason, _ := miningHoldReasonShown.Load().(string)
	if reason == "" {
		return "WAITING before mining", false
	}
	return "WAITING: " + reason, false
}

// decideMiningHold is the rule itself, kept free of clocks and network calls.
// heldSeconds is how long the tip has been stale at its current height.
func decideMiningHold(tipHeight, tipAgeSeconds, bestPeerHeight int64, anyPeerAnswered bool, heldSeconds int64) (hold bool, resync bool, reason string) {
	if tipAgeSeconds < miningStaleTipSeconds {
		return false, false, ""
	}
	// In the first blocks of a chain the difficulty does not fall when
	// blocks are slow (see heartbeat_difficulty.go), so there is no flood of
	// cheap blocks to guard against, and a slow start is exactly when holding
	// miners back on anyone's word would matter most.
	if tipHeight < HeartbeatLaunchBlocks {
		return false, anyPeerAnswered && bestPeerHeight > tipHeight, ""
	}
	if anyPeerAnswered {
		if bestPeerHeight <= tipHeight {
			return false, false, ""
		}
		if heldSeconds >= miningHoldBehindLimit {
			return false, true, ""
		}
		return true, true, fmt.Sprintf("the network is at block #%d and this node is at #%d", bestPeerHeight, tipHeight)
	}
	if heldSeconds >= miningHoldUnreachableLimit {
		return false, false, ""
	}
	return true, false, "no other node could be reached to confirm this node is up to date"
}

// miningReadinessGateways lists the nodes asked for their height.
func miningReadinessGateways() []string {
	// Whether to hold the miner back is decided on the word of the seeds
	// alone. A node that has merely been reached before could otherwise
	// stop this one mining by claiming to be ahead. Only a network with no
	// seeds falls back on remembered nodes.
	if gateways := reachableSeedSyncURLs(); len(gateways) > 0 {
		return gateways
	}
	return cachedPeerHTTPGateways(nil)
}

// probePeerHeights asks every gateway at once and returns the highest height
// reported and whether anyone answered.
func probePeerHeights() (best int64, answered bool) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, gateway := range miningReadinessGateways() {
		if syncSourceIsSetAside(gateway) {
			continue
		}
		wg.Add(1)
		go func(gateway string) {
			defer wg.Done()
			height, work := probeChain(gateway)
			// A node at height 0 has answered. At a launch every node is
			// there, and reading that as silence held every miner back for
			// five minutes.
			if height <= 0 && work == nil {
				return
			}
			mu.Lock()
			answered = true
			if height > best {
				best = height
			}
			mu.Unlock()
		}(gateway)
	}
	wg.Wait()
	return best, answered
}

// evaluateMiningReadiness runs one check and sets or clears the hold.
func evaluateMiningReadiness() {
	if atomic.LoadInt32(&IsSyncingActive) == 1 {
		return
	}
	StateMutex.RLock()
	tip := GetLatestBlockInternal()
	StateMutex.RUnlock()
	now := time.Now().Unix()
	tipAge := now - tip.Timestamp

	if tipAge < miningStaleTipSeconds {
		miningHoldTrackedHeight = -1
		setMiningHold(false, "")
		return
	}
	if miningHoldTrackedHeight != tip.Index {
		miningHoldTrackedHeight = tip.Index
		miningHoldSinceUnix = now
	}
	if len(miningReadinessGateways()) == 0 {
		// A network with no seeds and no remembered peers: there is nobody to
		// ask, so there is nothing to wait for.
		setMiningHold(false, "")
		return
	}
	best, answered := probePeerHeights()
	if answered {
		atomic.StoreInt64(&networkHeightHeard, best)
	}
	hold, resync, reason := decideMiningHold(tip.Index, tipAge, best, answered, now-miningHoldSinceUnix)
	setMiningHold(hold, reason)
	if resync {
		go TriggerActiveReSyncPass()
	}
}

func setMiningHold(hold bool, reason string) {
	if hold {
		miningHoldReasonShown.Store(reason)
		if atomic.SwapInt32(&MiningHoldActive, 1) == 0 || reason != miningHoldLastReason {
			announceNetworkStatus("⏸️ [MINER] Waiting before mining: " + reason + ".")
		}
		miningHoldLastReason = reason
		return
	}
	if atomic.SwapInt32(&MiningHoldActive, 0) == 1 {
		announceNetworkStatus("▶️ [MINER] This node is up to date. Mining resumed.")
	}
	miningHoldLastReason = ""
}

// runMiningReadinessMonitor repeats the check for as long as the node runs.
func runMiningReadinessMonitor() {
	for {
		time.Sleep(miningReadinessInterval)
		evaluateMiningReadiness()
	}
}

// minerClockMargin is how close to the limit on future-dated blocks a miner
// will go. Other nodes refuse a block dated more than
// HeartbeatMaxFutureDriftSeconds ahead of their own clock, and clocks differ
// by a few seconds, so a miner stays this far inside the limit.
const minerClockMargin = int64(10)

// mustWaitForClock reports whether a block with this date would be refused
// by other nodes as too far ahead, so that a miner has to wait before
// working on it. A block has to be dated after its parent, so this happens
// when the parent itself was dated ahead.
//
// A miner waits only as long as it must. It used to wait whenever the date
// was more than a minute ahead, which let anyone who dated a block two
// minutes ahead idle every honest miner for a minute and mine alone.
func mustWaitForClock(blockTimestamp, now int64) bool {
	return blockTimestamp > now+HeartbeatMaxFutureDriftSeconds-minerClockMargin
}

// minerBlockDate is the date a miner gives the block it is working on, on
// top of `tip`, when its clock reads `now`.
//
// Ordinarily that is the time by its clock (or one second after the tip, if
// the tip is dated ahead). There is one exception. A block dated six minutes
// or more after its parent is mined at a halved difficulty, and a block may
// be dated up to two minutes ahead of the clock. So anyone may claim that
// halving two minutes before six minutes have really passed, simply by
// dating the block ahead. A miner who did so, while everyone else dated
// their blocks truthfully, mined at half the difficulty of the others for
// those two minutes and took about 1% more than its share (audit round 5,
// finding 1).
//
// The answer is for every miner to claim it at the same moment. So when
// dating the block ahead, by no more than a miner will go (see
// minerClockMargin), reaches a halving that the true time does not, the
// block is dated at exactly the moment that halving begins. Nobody then has
// a head start. This is a choice each miner makes and not a rule nodes
// check: the rules already allow it.
func minerBlockDate(tip Block, now int64) int64 {
	plain := now
	if plain <= tip.Timestamp {
		plain = tip.Timestamp + 1
	}
	claim := dateClaimingRelief(tip.Timestamp, plain, now+HeartbeatMaxFutureDriftSeconds-minerClockMargin)
	// Only where it really lowers the difficulty: in the first blocks of a
	// chain no relief is given, and a later date would gain nothing.
	if claim > plain && CalculateNextDifficulty(tip, claim) < CalculateNextDifficulty(tip, plain) {
		return claim
	}
	return plain
}

// dateClaimingRelief returns `plain`, or the latest moment at which a further
// late-block halving begins if that lies after `plain` and no later than
// `latest`.
func dateClaimingRelief(parentDate, plain, latest int64) int64 {
	if latest-parentDate < HeartbeatStallAfterSeconds {
		return plain
	}
	steps := (latest - parentDate - HeartbeatStallAfterSeconds) / HeartbeatStallStepSeconds
	if claim := parentDate + HeartbeatStallAfterSeconds + steps*HeartbeatStallStepSeconds; claim > plain {
		return claim
	}
	return plain
}

// minerTemplateLife is how long the built-in miner works on one block
// template before building a fresh one with the current date and transfers.
const minerTemplateLife = 10 * time.Second
