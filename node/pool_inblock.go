package node

// PAYING IN THE BLOCK
//
// A block's reward may pay up to RewardMaxOutputs addresses. A pool uses
// that to pay the miners who did the work in the block itself. Their coins
// go straight from the network to them; the pool never holds them, so
// nobody who runs the pool, or breaks into it, can keep them.
//
// The division is the same as before: the most recent shares, counted back
// until their work adds up to twice the work one block takes. What is new is
// when it is fixed. A block's reward is part of the block, so the division
// has to be written into the work before anybody searches it. Each piece of
// work the pool hands out carries the division as it stood at that moment,
// and a miner is sent fresh work at least every fifteen seconds. The share
// that finds a block was handed in after that block's work was made, so it
// counts towards the blocks that follow, like every share after it.
//
// Not every part can be paid in the block:
//
//   - A reward has room for fifty addresses. The largest parts are paid in
//     the block. If there are more than fifty addresses, forty-nine are
//     paid and the fiftieth output is the pool's own, carrying the rest.
//   - A part smaller than poolInBlockFloorUnits is not paid in the block
//     either. A miner paid a hundredth of a coin in every block would hold
//     hundreds of coins too small to be worth spending.
//
// Those parts go to the pool's address in one output, and each is owed to
// its address in the pool's record, to be paid by an ordinary transfer once
// it has grown to the smallest payment (pool_payments.go). That is the only
// part of anybody's coins the pool holds.
//
// Before any share has been handed in there is nothing to divide by, and
// the work pays the miner it was made for.

// poolInBlockFloorUnits is the smallest part of a reward that is paid in
// the block itself: 0.01 CVN. Smaller parts are kept by the pool and owed.
// A variable so that tests can try a pool whose miners are all below it.
var poolInBlockFloorUnits = int64(1000000)

// poolRewardOutputs turns a division of `total` units into the outputs of a
// block's reward. It returns the outputs, the division with each part marked
// as paid in the block or not, and the place and size of the pool's own
// output (poolUnits is zero when there is none).
//
// `parts` must add up to `total` and come most work first, as poolSplit
// gives them. An empty division pays everything to `madeFor`, the miner the
// work is for.
func poolRewardOutputs(parts []poolPart, poolsAddress, madeFor string, total int64) (outputs []UTXOOutput, division []poolPart, poolIndex int, poolUnits int64) {
	if total < minimumOutputUnits() {
		return nil, nil, 0, 0 // too little to be a coin: the block pays nobody
	}
	if len(parts) == 0 {
		parts = []poolPart{{Address: madeFor, Units: total}}
	}
	division = make([]poolPart, len(parts))
	copy(division, parts)

	// Which parts could be paid in the block: large enough, and not the
	// pool's own address, which has an output of its own.
	fits := func(part poolPart) bool {
		return part.Units >= poolInBlockFloorUnits && part.Units >= minimumOutputUnits() && part.Address != poolsAddress
	}
	eligible := 0
	for _, part := range division {
		if fits(part) {
			eligible++
		}
	}
	room := RewardMaxOutputs - 1 // one output is kept for the pool
	if eligible == len(division) && eligible <= RewardMaxOutputs {
		room = RewardMaxOutputs // every part fits: the pool needs none
	}
	var inBlock int64
	for i := range division {
		if room > 0 && fits(division[i]) {
			division[i].InBlock = true
			inBlock += division[i].Units
			room--
		}
	}
	poolUnits = total - inBlock

	// What is left for the pool must itself be a coin. If it is too small
	// to be one, it cannot be paid to anybody: the parts it was made of are
	// dropped, and the units go to the largest part in the block. This is a
	// few thousandths of a coin at the very most.
	if poolUnits > 0 && poolUnits < minimumOutputUnits() {
		kept := division[:0]
		largest := -1
		for _, part := range division {
			if part.InBlock {
				kept = append(kept, part)
				if largest < 0 {
					largest = len(kept) - 1
				}
			}
		}
		if largest >= 0 {
			kept[largest].Units += poolUnits
			division, poolUnits = kept, 0
		}
	}

	for _, part := range division {
		if part.InBlock {
			outputs = append(outputs, UTXOOutput{Recipient: part.Address, Amount: UnitsAmount(part.Units)})
		}
	}
	if poolUnits > 0 {
		poolIndex = len(outputs)
		outputs = append(outputs, UTXOOutput{Recipient: poolsAddress, Amount: UnitsAmount(poolUnits)})
	}
	return outputs, division, poolIndex, poolUnits
}

// division is the reward of a block found now, divided among the shares
// held: the most recent, counted back until their work is `windowWork`. It
// is worked out once for each new share and kept, since every miner's work
// is made from it.
func (r *poolRecord) division(windowWork, rewardUnits int64) ([]poolPart, int64) {
	r.Lock()
	defer r.Unlock()
	if r.divided.parts != nil && r.divided.seq == r.nextSeq && r.divided.windowWork == windowWork && r.divided.rewardUnits == rewardUnits && r.divided.shares == len(r.shares) {
		return r.divided.parts, r.divided.total
	}
	var newest uint64
	if r.nextSeq > 0 {
		newest = r.nextSeq - 1
	}
	parts, total := poolSplit(r.shares, newest, windowWork, rewardUnits)
	if parts == nil {
		parts = []poolPart{}
	}
	r.divided.parts, r.divided.total = parts, total
	r.divided.seq, r.divided.shares, r.divided.windowWork, r.divided.rewardUnits = r.nextSeq, len(r.shares), windowWork, rewardUnits
	return parts, total
}
