# Audit round 4: findings and what was done

Three reviewers (Claude, Google AI, Grok) examined commit `35a596a` with the prompts in `audit/AUDIT_PROMPT.md` (Prompt 5). The round was called for two changes to the rules: late-block relief no longer inherited ("rule B"), and the removal of the first test network with all of its rules. What those changes were is in `audit/CHANGES_FOR_ROUND_4.md`.

Each finding below was checked against the code before anything was done about it. The reviewers have not yet tested each other's findings (Prompt 2).

## What all three agreed on

- No way was found for two honest nodes to compute different difficulties for the same block under rule B.
- Removing the median-of-eleven date check changed nothing: dates that always move forward along a chain already imply it.
- The genesis block's special cases can be reached only by the program's own genesis block.
- The two fixed "uncle" fields in the block header are enforced on every path by which a block is stored today.

## Fixed by a rule change

| # | Finding | Severity | Status |
|---|---|---|---|
| 1 | **A miner with most of the hashrate gains by choosing the dates of its blocks** (Claude) | Medium | **Confirmed and fixed** (`ExponentialSteps`). Not yet reviewed by anyone else. |

The miner dates a run of blocks one second after their parents, which raises difficulty by about 5% a block while the chain's dates fall behind the clock, then dates one block 495 seconds after its parent, which halves difficulty in one step. The rises and the halving cancel, but the dates of those blocks span less time than that many block intervals, so blocks come faster than the rule believes.

Reproduced against the real arithmetic (`node/chosen_dates_sim_test.go`), with honest miners dating truthfully:

| Miner's share of hashrate | Seconds per block | Coins against steady mining |
|---|---|---|
| 100% | 39.7 | +13.5% |
| 94% | 41.2 | +9.0% |
| 85% | 43.3 | +3.6% |
| 75% or less | 44.9 | none |

It does not come from rule B and was there before it. The cause is that each block's step is applied as `1 + x`: many small rises and one large fall multiply to less than their sum suggests.

**The fix.** Each step is now applied as `e^x` (`heartbeatRetargetAnchored`, setting `ExponentialSteps`, on for both networks). Steps then multiply to something that depends only on how much time the blocks' dates span.

- `e^x` is worked out in whole numbers (`expStepFactor`): the series 1 + x + x²/2 + ... in 64-bit fixed point, each term rounded toward zero, until a term rounds to nothing. Seven values are pinned by a test; they were checked against a separate implementation of the same series and lie within six parts in 2^64 of the true value.
- Against the real arithmetic, the patterns that paid before pay nothing (`TestChoosingDatesDoesNotPayTheLargestMiner`): no gain above 2% at 100%, 94% or 85% of the hashrate, and the chain holds 45 seconds. A float copy tried 29 patterns (`node/dates_candidates_sim_test.go`): 0.3% at most.
- Elsewhere it behaves as before: stop-start mining still pays a dominant miner nothing extra; after hashrate falls to a tenth, the next 30 blocks take about 52 minutes (49 before).
- A slow block now lowers difficulty by about 39% at most, where it was 50%.

It is a change to a rule every node enforces. A development chain mined before it is not valid under it.

## Fixed

| # | Finding | Severity | What was wrong | Fix | Test |
|---|---|---|---|---|---|
| 2 | **A rewrite of stored history during the walk through late blocks could leave a wrong remembered difficulty** (Claude) | Low | The walk reads several blocks one after another. A reorganisation in between could mix blocks of two chains, and the result was remembered as sound until restart. Not reproduced; a narrow race. | Rewrites of history (blocks removed, or a stored block replaced) are counted. An answer worked out while one was under way, or across one, is used once and not remembered. | `TestNothingIsRememberedWhileHistoryIsBeingRewritten` |
| 3 | **The built-in miner stored its own block without the full check** (Google AI, Claude) | Low | Storing a block checks its proof and its transfers, not its date or difficulty. The miner relied on having built the block correctly. No way to trigger a wrong block was found. | The miner validates its own block exactly as a peer's before storing it. | |
| 4 | **The walk through late blocks held whole blocks in memory** (Claude) | Low | On a chain stalled for days, the first difficulty question after a restart would hold every late block with its transfers. | Only heights, dates, difficulties and identifiers are kept. | |
| 5 | **Wrong port in the installation guide; stale port numbers in two comments** (Google AI) | Low | `http://NODE:8082/status` was the retired network's port. | Corrected to 18082 (28082 on devnet). | |
| 6 | **Left behind by the removal** (Claude) | Low | Unreachable code after the removal in the check on arriving blocks and in the sync pass; a reward check and message that still spoke of two outputs; an old routine that rebuilds the coin set without coin ages or sweeps, which ran only on an empty database but was not prevented from running on a full one; two stale comments; the whitepaper still stating the median rule as a rule. | Removed or corrected. The rebuild routine now refuses to run over a chain that has blocks. | `TestTheCoinSetIsNotRebuiltOverAChainWithBlocks` |

Tests added for cases the reviewers found missing: a late block during the forty blocks after the launch window (`TestALateBlockInTheRampAfterTheLaunchWindow`), and a late block replaced by a rival that was on time (`TestALateBlockReplacedByOneThatWasOnTime`).

## Not changed, and why

| Finding | Severity | Decision |
|---|---|---|
| **A Bitcoin anchor block dated ahead of real time makes every early block harder, with no relief** (Claude) | Low | Real, bounded, and it heals once the clock passes the anchor block's own time. The suggested fix (count "no time passed" as an on-time block instead of a fast one) was not taken: blocks dated before the anchor time would then never raise difficulty, and whoever mined first could hold it at its starting value for as long as they kept dating blocks there. To watch for at launch: how far the anchor block's timestamp is ahead of the true time. |
| **External mining software is refused work for a moment when the newest block is dated far ahead** (Google AI) | Low | True for the few seconds until the clock catches up, the same wait the built-in miner makes. The reviewer's "40 seconds" was not borne out. |
| **Mining only when difficulty dips still pays 10% to 30% more per hash** | Known | As stated in `CHANGES_FOR_ROUND_4.md`. All three reviewers judged leaving it defensible for a launch. |

## Not covered by this round

- One reviewer did not read the networking or wallet files.
- Whether a date pattern exists, beyond those tried, that still pays under exponential steps. The argument that none can is that steps now depend only on their sum; the late-block halving, which is separate from the step, is the place to look.
- The tests still mostly check the rule against its own arithmetic; the simulations are the only check from outside it.
