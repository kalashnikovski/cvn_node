# Audit round 2: findings and what was done

Two reviewers (Google AI, Grok) examined the code after the round-1 fixes, at commit `8432084`, then each tested the other's findings. Both confirmed that every round-1 fix closes the hole it was made for. This is what they found beyond that. Tests named below fail on the old code and pass on the new.

## Fixed

| # | Finding | Severity | What was wrong | Fix | Test |
|---|---|---|---|---|---|
| 1 | **A node could lose track of its own newest block** | High | The height kept in memory was written after each save, outside the order in which the saves were made, and by callers as well as by the save. Two saves finishing together could record their heights the wrong way round, leaving the node working from the block below its real tip. | Everything that changes the tip (storing a block, rolling back) is done one at a time, and the height is recorded as part of that. Nothing else writes it. | `TestTipIsRightAfterManySavesAtOnce` |
| 2 | **Strangers were asked before the seeds** | High | A node catching up asked nodes that had merely connected to it first, and the seeds only if none of those claimed a better chain. A few addresses, each claiming enormous work, could take turns being asked and keep a node from ever asking a seed. | Sources are asked in order of trust: the seeds, then nodes that have delivered before, then strangers. A later group is asked only when nobody in an earlier one claims a better chain. | `TestSeedsAreAskedBeforeStrangers` |
| 3 | **Comparing chains paused the node** | High | A pass stopped mining, and made the node ignore blocks from its peers, from its first moment. A node that merely claimed a better chain could idle and deafen another at will. | A pass leaves the node alone until it has blocks to add or remove. | `TestComparingChainsDoesNotPauseTheNode`, `TestCatchingUpDoesPauseTheNode` |
| 4 | **The work server accepted answers while the chain was changing** | High | An external miner's block could be stored on a chain that was part-way through being replaced. (The database itself refused anything that did not attach, so no invalid state resulted, but the result of the pass could be decided by the interleaving.) | Answers are turned away as stale while the chain is changing. | `TestWorkServerPaysOnlyARealAddress` |
| 5 | **Mining could be paid to text that is not an address** | Medium | The miner took any text starting with `CVN_` as the address to pay, and the work server did the same, falling back to the node owner's address when the text was unusable. Rewards paid to text that is not an address can never be spent, and a miner who mistyped was silently made to mine for someone else. | Both accept only `CVN_` and 40 hex digits. The work server refuses any other login outright. The text wallet checks the recipient before asking for a key. | `TestMiningIsOnlyPaidToARealAddress`, `TestWorkServerPaysOnlyARealAddress` |
| 6 | **A miner read the vault of the wrong block** | Medium | A miner worked out its payout from the vault as it stood at whatever the tip had become, not at the block it was building on. If a block arrived in between, it mined a block that would be refused. | The vault is read for the block being built on. | `TestMinerReadsTheVaultOfTheBlockItBuildsOn` |
| 7 | **An identifier was not checked to be the block's own (first test network's rules)** | Medium | Under SHA-256 mining a block far ahead with an identifier of all zeros passed the first check and set off a sync pass. A relay connection that sent malformed blocks was not penalised. | The identifier is checked on arrival under both mining rules. Malformed blocks over a relay connection count against the sender. | `TestAnIdentifierThatIsNotTheBlocksOwnIsRefusedOnArrival` |
| 8 | **Dates on the launch blocks leaned on every later difficulty** | Low | The schedule the difficulty rule steers by was counted from block 1, whose date its miner chooses. | It is counted from the last block of the launch window. | `TestLaunchBlockDatesDoNotLeanOnLaterDifficulty` |
| 9 | **Tables that only grew** | Low | Fees of waiting transfers, refused addresses, malformed-message counts and rate limits were never emptied. | Fee entries go when their transfer does. The others are pruned every ten minutes. | `TestOldEntriesAreForgotten` |

## Not a problem

| Finding | Why |
|---|---|
| **Reward transaction identifiers colliding and overwriting coins** | An identifier must name its block's height, and a chain has one block at each height. When a block at a height is replaced, the coins of the block it replaces are removed first. Both reviewers' scenarios need two blocks at one height in one chain, which cannot exist. A block that pays nothing creates no coins to collide with. |
| **A node forced onto a worse chain by a source that delivers what the search expects** | Checked by the reviewers themselves: a competing branch is measured before anything is removed, and if what is finally applied holds less work than what was given up, the node's own blocks are put back. |

## Not changed, and why

| Finding | Severity | Decision |
|---|---|---|
| **A Windows node whose download port was taken listens on a second port that others do not try** | Medium | Open. It only happens when another program already holds the port. Asking every peer on two ports doubles the cost of every pass. To revisit with the capacity work on node-to-node connections. |
| **If the starting difficulty is far above the mining power at launch, the first 20 blocks are slow** | Low | Accepted. It is the other side of not letting dates talk the difficulty down. The starting value is ours to set, and is set near the low end of what is expected. |
| **Several small database reads per block when checking dates** | Low | Open. Slower first sync on a long chain; no effect on what is valid. |

## What this round could not settle

- No reviewer could run the code. The simultaneous-save problem in particular was found by reading; the test for it races eight savers and passes, which shows the fix holds under that load, not that no other ordering problem exists.
- Nothing has been run between machines since round 1.
