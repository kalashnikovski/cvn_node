# Changes for audit round 4

What changed in the code after the last reviewed commit (`d61ec5e`, 15 November 2026), stated as claims for a reviewer to test. Nothing in this file has been checked by anyone other than the authors.

Two of these changes touch the rules every node enforces. They are the reason for this round.

## 1. Late-block relief is no longer inherited ("rule B")

**Where:** `node/heartbeat_difficulty.go` (`heartbeatDifficultyWithGap`, `heartbeatDifficultyFromBase`, `unrelievedDifficulty`, `reliefGapSeconds`), switched on by `ReliefNotInherited` in `node/chain_params.go`. Tests: `node/relief_not_inherited_test.go`. Simulations: `node/hopping_sim_test.go`, `node/difficulty_candidates_sim_test.go`.

**The fault it answers.** A block dated six minutes or more after its parent is mined at a halved difficulty, halved again for every further three minutes. Before this change the next block started from that halved figure and was then cut again, by up to half, because its parent had been slow. One 14-minute gap left the following blocks at a sixteenth of the difficulty, and difficulty rises by at most 5% a block, so about seventy-five cheap blocks followed. Whoever holds most of the hashrate collects them.

- Seen on the development network on 16 November 2026: the largest miner (about 94% of the hashrate) was off for 49 minutes, then won 67 blocks in 25 minutes; over the episode the chain ran at 34.8 seconds a block.
- In simulation with the real arithmetic, a miner with 94% of the hashrate who mined only while difficulty was at or below half its fair level took about 34% more coins a day than by mining steadily, and blocks came every 32 seconds. The term that steers the chain back to its schedule is capped at 1% a block and could not hold it.

**The change.** The relief belongs to the late block alone. The next block is worked out from the difficulty its parent would have had with no relief (its "unrelieved" difficulty), and is then lowered in the ordinary way for the parent having been slow. The unrelieved difficulty of a late block cannot be read off the block: halving discards low bits and stops at the network minimum. So it is recomputed: the rule applied to that block's parent with no gap, which needs the parent's own unrelieved difficulty, and so on back through consecutive late blocks to the first that was on time. Answers are remembered by block identifier in a table that is emptied when it reaches 4,096 entries or the network settings change.

**What the authors claim.**

1. With the real arithmetic, burst mining pays a 94% miner nothing extra at any of six thresholds, and the chain holds 45.0 seconds (`TestBurstMiningDoesNotPayTheLargestMiner`).
2. Blocks that arrive on time are judged exactly as before, as are all blocks of the launch window (`TestOnTimeBlocksAreUnchanged`).
3. Every node computes the same unrelieved difficulty for the same block, whatever is or is not in its table (`TestSeveralLateBlocksInARow`, `TestAChainWithALateBlockValidates`).
4. Near the network minimum the figure is exact (`TestUnrelievedDifficultyNearTheMinimum`).

**What it costs, and what it does not fix.**

- After hashrate really falls to a tenth, the next 30 blocks take about 49 minutes in simulation; before the change, about 37; on schedule, 22.5.
- After a long real absence the difficulty has still come down by half per late block, so a returning large miner still finds cheap blocks. What changes is that going away on purpose now costs about as much as it gains.
- A miner of any size who mines only when difficulty dips below its average still earns 10% to 30% more per hash than one who mines steadily, while earning fewer coins in total. This comes from the rule reacting block by block and was not addressed. A rule driven only by the chain's distance from its schedule would cut it, at the price of stalls of an hour or more after a real loss of hashrate; the comparison is in `difficulty_candidates_sim_test.go`.
- The simulations assume an attacker who switches instantly with perfect knowledge, and honest block dates. They do not model a miner who also chooses dates.

**Things a reviewer might try.** Make two honest nodes compute different difficulties for one block (through the table, a restart, a reorganisation, a rival block at the same height, or a missing ancestor: see the `exact` flag in `unrelievedDifficulty`). Make the walk back through late blocks expensive. Find a pattern of block dates, within the 120 seconds a block may be dated ahead, that brings the old burst back. Check the interaction with the launch window (blocks 1 to 20, no relief) and the 40 blocks after it, where relief returns one halving at a time (`launchRampGap`).

## 2. The first test network and all of its rules are removed

**Where:** most files in `node/`. Commit `a3c8c9f`.

The program used to carry two rule sets side by side, chosen by six network settings. It now carries one. `--network testnet-1` is refused. Two networks remain, `cvn_testnet` and `devnet`, differing only in names, keys, ports, files, seeds and starting difficulty.

**Removed:** the first difficulty rule and the 45-second wait in the miner; decimal amounts; the looser block and transfer format; addresses without a checksum; the old reward schedule; the uncle share (recording, paying and validating it); the settings `StrictFormat`, `NoUncles`, `AmountsAsUnits`, `ScheduleAnchored`, `HeartbeatHeight`, `TxForkHeight`.

**Changed in passing, on purpose:**

- `ValidateBlockSignatures` no longer checks a block's date against the median of the last eleven, or against two hours ahead of the clock. It still requires a date later than the parent's and no more than 120 seconds ahead. The authors' claim: with dates that always move forward the median rule can never be the one that refuses a block, and 120 seconds is stricter than two hours.
- A block whose parent is not held at the height below is refused outright. Before, that was so only from the first block of the newer difficulty rule, which on the launch networks was block 1.
- When two blocks compete at one height, the loser is dropped and is no longer broadcast (it was broadcast so that others could pay its miner the uncle share).

**Kept on purpose:** the two uncle fields in the block header, which are part of every block's identifier and must hold their empty values; SHA-256 as a mining algorithm, which the tests use and the algorithm schedule can name; the genesis block's own cases (it is not mined, its work counts as a fixed 256, and its transaction digest is built from its one transaction's ID).

**What the authors claim.** No rule of the launch networks changed. The evidence: a snapshot of the live development chain (2,944 RandomX blocks, with transfers and transfers that combine many coins into one) replays through the new code with identical block identifiers and identical balances, and the development network's plain genesis identifier is unchanged.

**What that evidence does not cover.** The replayed chain holds no block that was refused, so it says nothing about blocks the old code refused and the new code might accept, or the reverse. It holds no sweep, no reward halving, no reorganisation and no block near the size limit.

**Things a reviewer might try.** Find a block or transfer the old launch rules refused that these accept. Find a place where a removed setting was the only thing standing between launch-rule data and code that assumed the older format. Find anything left behind that still behaves as the old rules did.

## 3. Smaller changes

- **The CVN Testnet is the default network** (`78b1ba4`): a program started with no network named runs on `cvn_testnet`. It cannot start until its Bitcoin anchor is scheduled; wallets still open.
- **The fast miner is used whenever its folder is present** beside the program (`f4cd336`), without any setting. A `miner_config.json` that cannot be read is refused with the line at fault, where before it was silently ignored.
- **The node shows the fast miner's processor-tuning lines** (`af6792e`), success included.
- **Two log lines no longer appear** (`dc8c27f`): a second copy of a block already stored is not reported as a rejection (`blockAlreadyStored`), and no uncle note is kept.
- **Wording:** the desktop wallet no longer says "mainnet"; an error after combining coins explains that the coins are tied up until the next block.

## 4. Measured, not changed

- **Graphics cards.** The published CVN edition of XMRig cannot mine CVN on a graphics card: its OpenCL kernel holds standard RandomX's dataset size. With a private patch, never published, an RTX 5070 Ti reached about 772 hashes a second against about 18,000 for a 16-core processor. Anyone can make that patch; it is not worth running.
- **Processor tuning.** XMRig's MSR change, applied on Windows as administrator with virtualization switched off, gave about 19,200 hashes a second against about 17,900: roughly 7%. It is documented in `MINING_GUIDE.md` section 3.

## Open, known to the authors

- The per-hash advantage of mining only when difficulty dips (section 1).
- Enough addresses can still fill a node's connections; the fast miner is identified by what it says about itself (both from round 3).
- A node cannot prove that a Bitcoin header is the block at the announced height; it relies on five public explorers, any two agreeing, and on the earliest date fixed in the release.
- None of this has run on a live network. The development network will be wiped and restarted on these rules before the CVN Testnet is scheduled.
