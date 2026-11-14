package node

import (
	"math"
	"math/rand"
	"testing"
)

func TestDevnetUsesLaunchRules(t *testing.T) {
	onNetwork(t, "devnet")
	if !IsHeartbeatHeight(1) || !IsTxForkHeight(1) {
		t.Error("on devnet the difficulty rule and transfer validation must apply from block 1")
	}
	if !Params.ScheduleAnchored || Params.Economics == nil || !Params.AmountsAsUnits {
		t.Error("devnet must run the launch rules: anchored schedule, launch rewards, whole-unit amounts")
	}
	if powAlgorithmAt(1) != PowRandomX || powAlgorithmAt(5000000) != PowRandomX || minDifficultyAt(1) != 5000 {
		t.Error("devnet must mine with RandomX from block 1, with a minimum difficulty of 5,000")
	}
	if Params.KeyEpochBlocks != 1920 || Params.KeyLagBlocks != Params.Economics.ReorgLimitBlocks {
		t.Error("the RandomX key must change daily and sit beyond the reorganisation limit")
	}
	if BlockRewardUnits(0) != 0 || BlockRewardUnits(1) != cvn("50") || BlockRewardUnits(4905600) != cvn("50") || BlockRewardUnits(4905601) != cvn("25") {
		t.Error("devnet rewards do not follow the launch schedule")
	}
	if BlockRewardUnits(34339200) != cvn("0.78125") || BlockRewardUnits(34339201) != 0 {
		t.Error("the launch schedule must end after block 34,339,200")
	}
}

func TestTestnet1KeepsItsOriginalRules(t *testing.T) {
	onNetwork(t, "testnet-1")
	if Params.ScheduleAnchored || Params.Economics != nil {
		t.Fatal("testnet-1 must keep its original difficulty rule and reward schedule")
	}
	if BlockRewardUnits(7000) != cvn("50") || BlockRewardUnits(5256001) != cvn("25") {
		t.Error("testnet-1 reward schedule changed")
	}
	if IsHeartbeatHeight(2499) || !IsHeartbeatHeight(2500) {
		t.Error("testnet-1 heartbeat height changed")
	}
	if powAlgorithmAt(1) != PowSHA256 || powAlgorithmAt(7000) != PowSHA256 || minDifficultyAt(7000) != HeartbeatMinDifficulty {
		t.Error("testnet-1 must keep mining with SHA-256 at its original minimum difficulty")
	}
}

// A whole emission, in miniature: three epochs of four blocks, then nothing.
func TestRewardScheduleOnAChain(t *testing.T) {
	startTestChain(t, "devnet")
	Params.Economics = &GenesisEconomics{BlockSeconds: 45, EpochBlocks: 4, EmissionEpochs: 3, InitialRewardUnits: cvn("50")}
	privateKey, publicKey, miner, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	timestamp := Params.GenesisTimestamp + 100
	wantRewards := []string{"50", "50", "50", "50", "25", "25", "25", "25", "12.5", "12.5", "12.5", "12.5"}
	for i, want := range wantRewards {
		timestamp += 45
		block := extendTestChain(t, timestamp, miner, nil)
		if got := block.Transactions[0].Outputs[0].Amount.Units(); got != cvn(want) {
			t.Fatalf("block #%d pays %s, want %s", i+1, FormatUnits(got), want)
		}
	}
	if got, want := balanceOf(miner), Params.Economics.TotalSupplyUnits(); got != want || got != cvn("350") {
		t.Fatalf("after the last rewarded block %s CVN exist, want %s", FormatUnits(got), FormatUnits(want))
	}

	// Emission is over. A block may not create anything...
	timestamp += 45
	greedy := mineTestBlock(t, timestamp, miner, nil, 1)
	if ValidateBlockSignatures(greedy) && SaveBlockToStorageInternal(greedy) {
		t.Fatal("a block created one unit after emission ended")
	}
	// ...a block with no reward and no fees pays nothing and is still valid...
	empty := extendTestChain(t, timestamp, miner, nil)
	if len(empty.Transactions[0].Outputs) != 0 {
		t.Errorf("a block with nothing to pay has %d outputs", len(empty.Transactions[0].Outputs))
	}
	// ...and a block with transfers pays its miner exactly their fees.
	transfer, err := BuildSignedTransfer(privateKey, publicKey, "CVN_someone", cvn("10"), cvn("0.3"), ListSpendableCoins(miner))
	if err != nil {
		t.Fatal(err)
	}
	timestamp += 45
	feeBlock := extendTestChain(t, timestamp, "CVN_fee_miner", []Transaction{transfer})
	if got := feeBlock.Transactions[0].Outputs[0].Amount.Units(); got != cvn("0.3") {
		t.Errorf("after emission the miner received %s, want exactly the 0.3 fee", FormatUnits(got))
	}
	total := balanceOf(miner) + balanceOf("CVN_someone") + balanceOf("CVN_fee_miner")
	if total != cvn("350") {
		t.Errorf("total supply is %s after emission ended, want 350", FormatUnits(total))
	}
}

func TestAnchoredRetarget(t *testing.T) {
	const d = int64(90000000)
	target := HeartbeatTargetSeconds
	onSchedule := heartbeatRetargetAnchored(d, target, 10, 0, HeartbeatMinDifficulty)
	if onSchedule != d {
		t.Errorf("on target and on schedule, difficulty moved to %d", onSchedule)
	}
	if heartbeatRetargetAnchored(d, target, 10, 0, HeartbeatMinDifficulty) != heartbeatRetarget(d, target, 10) {
		t.Error("with no drift the anchored rule must agree with the plain rule")
	}
	ahead := heartbeatRetargetAnchored(d, target, 10, -900, HeartbeatMinDifficulty) // 20 blocks ahead of schedule
	behind := heartbeatRetargetAnchored(d, target, 10, 900, HeartbeatMinDifficulty)
	if ahead <= d || behind >= d {
		t.Errorf("ahead of schedule must raise difficulty (%d) and behind must lower it (%d)", ahead, behind)
	}
	// The correction is capped at 1% per block however large the drift.
	farAhead := heartbeatRetargetAnchored(d, target, 10, -100000000, HeartbeatMinDifficulty)
	farBehind := heartbeatRetargetAnchored(d, target, 10, 100000000, HeartbeatMinDifficulty)
	if farAhead != d+d/100 || farBehind != d-d/100 {
		t.Errorf("capped correction gives %d and %d, want %d and %d", farAhead, farBehind, d+d/100, d-d/100)
	}
	// It still works at the lowest difficulty, where rounding could hide it.
	if got := heartbeatRetargetAnchored(HeartbeatMinDifficulty*2, 1, 10, 0, HeartbeatMinDifficulty); got <= HeartbeatMinDifficulty*2 {
		t.Errorf("a fast block at low difficulty did not raise it (%d)", got)
	}
	// Limits and stall relief behave as in the plain rule.
	if got := heartbeatRetargetAnchored(d, target, HeartbeatStallAfterSeconds, 0, HeartbeatMinDifficulty); got != d/2 {
		t.Errorf("at the stall threshold difficulty should halve, got %d", got)
	}
	if got := heartbeatRetargetAnchored(d, 100000, 100000000, 100000000, HeartbeatMinDifficulty); got != HeartbeatMinDifficulty {
		t.Errorf("difficulty fell below the minimum: %d", got)
	}
	if got := heartbeatRetargetAnchored(HeartbeatMaxDifficulty, 1, 10, -100000000, HeartbeatMinDifficulty); got != HeartbeatMaxDifficulty {
		t.Errorf("difficulty rose above the maximum: %d", got)
	}
}

// simulateChain mines `blocks` blocks against a hashrate (hashes per second,
// which may change over time) and returns the average seconds per block and
// how far the chain ended from its schedule. Finding a block is random, as in
// real mining, and difficulty eases during a long wait exactly as the rule
// says. The seed makes every run identical.
func simulateChain(anchored bool, blocks int, hashrate func(block int) float64, seed int64) (averageSeconds float64, finalDriftSeconds int64) {
	random := rand.New(rand.NewSource(seed))
	difficulty := int64(hashrate(0) * float64(HeartbeatTargetSeconds))
	parentSolve := HeartbeatTargetSeconds
	var elapsed int64 // seconds since block 1

	next := func(gap, drift int64) int64 {
		if anchored {
			return heartbeatRetargetAnchored(difficulty, parentSolve, gap, drift, HeartbeatMinDifficulty)
		}
		return heartbeatRetarget(difficulty, parentSolve, gap)
	}
	for i := 1; i < blocks; i++ {
		drift := elapsed - int64(i-1)*HeartbeatTargetSeconds
		rate := hashrate(i)
		// Work left to do, in units of "expected hashes at difficulty 1".
		remaining := random.ExpFloat64()
		var waited float64
		segmentStart, segmentEnd := int64(0), HeartbeatStallAfterSeconds
		for {
			segmentDifficulty := float64(next(segmentStart, drift))
			needed := remaining * segmentDifficulty / rate
			if needed < float64(segmentEnd-segmentStart) {
				waited = float64(segmentStart) + needed
				break
			}
			remaining -= float64(segmentEnd-segmentStart) * rate / segmentDifficulty
			segmentStart, segmentEnd = segmentEnd, segmentEnd+HeartbeatStallStepSeconds
		}
		gap := int64(math.Ceil(waited))
		if gap < 1 {
			gap = 1
		}
		difficulty = next(gap, drift)
		parentSolve = gap
		elapsed += gap
	}
	return float64(elapsed) / float64(blocks-1), elapsed - int64(blocks-1)*HeartbeatTargetSeconds
}

func TestBlockTimeAveragesExactlyTheTarget(t *testing.T) {
	const blocks = 300000 // about five months of blocks
	steady := func(int) float64 { return 2000000 }

	plainAverage, plainDrift := simulateChain(false, blocks, steady, 1)
	anchoredAverage, anchoredDrift := simulateChain(true, blocks, steady, 1)
	t.Logf("steady hashrate: plain rule %.2f s per block (ends %.1f days ahead of schedule); anchored rule %.3f s (ends %d s from schedule)",
		plainAverage, -float64(plainDrift)/86400, anchoredAverage, anchoredDrift)

	if plainAverage > 44.5 {
		t.Errorf("the plain rule was expected to run fast (as on testnet-1); it averaged %.2f s", plainAverage)
	}
	if math.Abs(anchoredAverage-45) > 0.05 {
		t.Errorf("anchored rule averaged %.3f s per block, want 45", anchoredAverage)
	}
	if anchoredDrift > 3600 || anchoredDrift < -3600 {
		t.Errorf("anchored chain ended %d s from its schedule, want within an hour", anchoredDrift)
	}
}

func TestBlockTimeRecoversFromHashrateSwings(t *testing.T) {
	const blocks = 300000
	// Hashrate quadruples a third of the way in, then falls to a quarter of
	// its starting level.
	swings := func(block int) float64 {
		switch {
		case block < blocks/3:
			return 2000000
		case block < 2*blocks/3:
			return 8000000
		default:
			return 500000
		}
	}
	average, drift := simulateChain(true, blocks, swings, 2)
	t.Logf("hashrate x4 then /16: %.3f s per block, ends %d s from schedule", average, drift)
	if math.Abs(average-45) > 0.05 {
		t.Errorf("averaged %.3f s per block across hashrate swings, want 45", average)
	}
	if drift > 3600 || drift < -3600 {
		t.Errorf("ended %d s from schedule after hashrate swings, want within an hour", drift)
	}

	// Several different random runs all land on the target.
	for seed := int64(10); seed < 15; seed++ {
		if got, _ := simulateChain(true, 100000, func(int) float64 { return 2000000 }, seed); math.Abs(got-45) > 0.1 {
			t.Errorf("seed %d: averaged %.3f s per block, want 45", seed, got)
		}
	}
}
