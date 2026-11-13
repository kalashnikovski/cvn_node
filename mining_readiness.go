package main

import (
	"fmt"
	"runtime"
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
	miningHoldUnreachableLimit = int64(300)
	// How long to hold behind a peer that claims to be ahead while our own
	// height does not move.
	miningHoldBehindLimit = int64(600)
)

var (
	miningHoldTrackedHeight int64 = -1
	miningHoldSinceUnix     int64
	miningHoldLastReason    string
)

// decideMiningHold is the rule itself, kept free of clocks and network calls.
// heldSeconds is how long the tip has been stale at its current height.
func decideMiningHold(tipHeight, tipAgeSeconds, bestPeerHeight int64, anyPeerAnswered bool, heldSeconds int64) (hold bool, resync bool, reason string) {
	if tipAgeSeconds < miningStaleTipSeconds {
		return false, false, ""
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
	gateways := []string{"http://64.177.45.153:8081", "http://207.148.67.11:8081"}
	if runtime.GOOS != "windows" {
		gateways = append(gateways, "http://202.137.175.220:8081")
	}
	return append(gateways, cachedPeerHTTPGateways(gateways)...)
}

// probePeerHeights asks every gateway at once and returns the highest height
// reported and whether anyone answered.
func probePeerHeights() (best int64, answered bool) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, gateway := range miningReadinessGateways() {
		wg.Add(1)
		go func(gateway string) {
			defer wg.Done()
			height := GetGlobalMeshMaxHeight(gateway)
			if height <= 0 {
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
	best, answered := probePeerHeights()
	hold, resync, reason := decideMiningHold(tip.Index, tipAge, best, answered, now-miningHoldSinceUnix)
	setMiningHold(hold, reason)
	if resync {
		go TriggerActiveReSyncPass()
	}
}

func setMiningHold(hold bool, reason string) {
	if hold {
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
