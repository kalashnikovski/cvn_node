# Audit round 1: findings and what was done

Three reviewers (Claude, Google AI, Grok) examined the code at commit `4e775ec`, then each tested the others' findings. This is every finding that survived, what was checked, and the outcome. Tests named below fail on the old code and pass on the new.

## Fixed

| # | Finding | Severity | What was wrong | Fix | Test |
|---|---|---|---|---|---|
| 1 | **Any peer could replace the genesis block** | Critical | A block at height 0 was judged by the first test network's rules for its earliest blocks, which check almost nothing. It was treated as a rival to the genesis block, won on a lower identifier, and every output of every transaction in it was stored unchecked: coins from nothing. It affected every network, the live one included. | There is one genesis block and the program builds it. Nothing at height 0 is accepted from the network, judged valid unless it is that block, or stored. | `TestGenesisBlockCannotBeReplaced` |
| 2 | **A peer that lied about its chain became the only one asked** | High | A node believed a peer's claim of total work, chose the highest claim as its only source, and after it delivered nothing chose it again. A watchdog cleared the "busy" flag while the pass carried on, so passes overlapped. | A source that claims a better chain and does not deliver one is set aside for 10 minutes. A pass keeps its own time limit and alone clears the flag it set. | `TestALyingSourceIsSetAside`, `TestAnHonestSourceIsFollowed` |
| 3 | **Nodes could not catch up when blocks were full** | High | A request for blocks was answered with 512 of them whatever their size, over 100 MB when full, under a 3-second limit. The fallback over node connections sent unsigned requests to nodes that require signed ones and could never have worked. | Replies stop at 4 MB. Downloads are allowed 30 seconds. The dead fallbacks are removed. | `TestBlockBatchesAreLimitedBySize` |
| 4 | **A wallet could be made to sign away its change** | Medium–High | A wallet works out change from the coin amounts a node reports, and nothing signed stated those amounts or the fee. A node that understated them turned the difference into a fee. | On the launch rules the sender signs the amount of every coin spent and the fee; both must match exactly or the transfer is invalid. | `TestALyingNodeCannotTurnChangeIntoAFee`, `TestTheFeeASenderSignsIsTheFeeTaken` |
| 5 | **A block dated two minutes ahead idled honest miners** | High | Miners waited whenever the newest block was dated more than a minute ahead. Anyone dating a block at the allowed limit idled everyone else for a minute and mined alone. | A miner waits only when its own block would be refused: about 11 seconds at most. | `TestMinerDoesNotIdleForFutureDatedBlocks` |
| 6 | **The first blocks' difficulty followed the dates written on them** | Medium | The genesis block is dated by a Bitcoin miner's clock, and the first miner chooses the dates of the first blocks. Either could make the next block look late and drop its difficulty to the minimum. | For the first 20 blocks no relief is given for a late block and difficulty does not go below the starting value. It can still rise. | `TestLaunchDifficultyCannotBeTalkedDown` |
| 7 | **Bad work beside the tip cost a proof check and no ban** | Low–Medium | A rival to a node's newest block with an invalid proof was checked in full and not held against its sender. | Such a block now gets its sender refused for 10 minutes, as one extending the tip already did. | `TestBadWorkBesideOurTipIsHeldAgainstThePeer` |
| 9 | **Work that grew with the whole chain** | Medium | Every request for the list of balances recounted every coin. Every sync pass read every block while holding the main lock, and found where chains part by walking back one block at a time, downloading 512 blocks at each step. The web server had no time limits. | Recounts for web requests are at most every 10 seconds. The parting point is found by halving the range with small requests, and no further back than the reorganisation limit. The web server has time limits. | `TestCommonAncestorIsFoundQuickly`, `TestBalanceRecountsAreSpacedOut` |
| 10 | **Wallets sent to anything that was not blank** | Medium | Coins sent to text that is not an address can never be spent. | Both wallets refuse a recipient that is not `CVN_` and 40 hex digits, before asking any node. | `TestWalletRefusesToSendToSomethingThatIsNotAnAddress` |
| N1 | **The mempool could be held full for nothing** | Medium | A full pool refused everything, so fee-less transfers of dust could shut everyone out. | A transfer paying more than the cheapest one waiting takes its place. | `TestAFeeGetsIntoAFullMempool` |
| G1 | **A signature could be rewritten into another valid one** | Low | Signatures were accepted in several written forms, and in their mirror image, so one transfer could appear in a block in more than one form. | On the launch rules a key and a signature have one form: fixed-length lower-case hex, and the lower of the two mirror images. | `TestATransferHasOneForm` |
| G6 | **A free-text field in every transfer** | Low | The "sender" field was signed but could hold any 128 characters. | On the launch rules it must be the sender's address. | `TestATransferHasOneForm` |
| G5 | **The work server checked any number of bad answers** | Low | Each cost a proof check. | A miner is disconnected after 30 answers without the work. Good answers that arrived late are not counted. | |
| G2 | **Malformed blocks cost their sender nothing** | Low | A block that failed the first check was dropped silently. | Under the heartbeat rule it counts against the sender, as other malformed messages do. | |
| G3 | **A balance shortcut that would be wrong if ever reached** | Low | Code for subtracting spent coins looked only in the previous block. It was unreachable. | Removed. | |
| — | **Whitepaper said 500 transfers per block** | Note | The 500 includes the miner's reward. | Corrected to 499. | |

## Not changed, and why

| # | Finding | Severity | Decision |
|---|---|---|---|
| 8 | **Between two blocks with equal work, the lower identifier wins, even over a block already accepted** | Low–Medium | Kept for now. It gives a miner who withholds a block a small edge. The usual alternative, keeping the block seen first, was how the first test network behaved when nodes split and did not rejoin. Changing which block a node prefers is the kind of change that needs a multi-machine test of its own. To revisit. |
| 11 | **Relief for a late block is measured from the block's own date, which may run two minutes ahead** | Low–Medium | Kept. A miner can sometimes claim relief about two minutes early, on the few blocks a day that are already four minutes late. Removing it means removing the relief, which is what lets a chain that has lost its miners recover. The idle-miner fix (5) removes the larger gain that went with it. |
| 12 | **Addresses are a hash of the key written as text; the anchor check accepts less work than a real Bitcoin block has; transfers only expire when a block is being filled** | Low | The first is a fixed convention with no weakness found. The second cannot hurt: a header for a different block gives a different, lonely network. Expiry is now also applied when the pool is full. |
| N2 | **A competing branch is measured by the difficulty its blocks claim** | Low | The branch is still checked in full before anything is kept, and a source that does not deliver is now set aside (2). What remains is wasted work, not a wrong chain. |
| — | **No checksum in an address** | Medium | Open. A mistyped address that still has the right form is not caught. Adding a checksum changes what an address looks like, for everyone, and is a decision for the project. |
| — | **Messages of up to 2 MB are parsed before being checked** | Medium | Open. A valid full block needs most of that. Rate limits per sender exist. Lowering the limit safely needs measuring real block sizes first. |
| — | **Finding a wallet's coins reads every coin** | Medium | Open. It needs an index in the database, which is already on the roadmap. |
| — | **Write errors to peers are ignored; many sends at once** | Low | Open. No effect on what is valid. |

## What the reviewers checked and found sound

The supply schedule and its total; the RandomX key rule; how coins are named; a coin cannot be spent twice inside one block.

## What this round could not settle

- Whether a node behind a home router can be reached by the lying-source attack. The fix does not depend on the answer.
- How often Bitcoin block timestamps are far enough from the real time to matter. The fix does not depend on the answer.
- No reviewer could run the code. Everything here was found by reading, and every fix is confirmed by a test, not by a reviewer.
