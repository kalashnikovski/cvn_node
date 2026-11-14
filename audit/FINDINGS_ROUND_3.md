# Audit round 3: findings and what was done

(Run as "round 2.5".) Two reviewers (Claude, Grok) examined the code after the round-2 fixes, at commit `5e85d3b`. One found the problems below; the other, which had found nothing serious alone, then confirmed each of them against the code. Both again confirmed that the earlier fixes hold, with the gaps listed here. Tests named below fail on the old code and pass on the new.

## Fixed

| # | Finding | Severity | What was wrong | Fix | Test |
|---|---|---|---|---|---|
| A | **A remembered node could keep a restarting node from ever mining** | High | At start-up the node believed the highest height any seed or remembered node claimed, and did not start mining until it reached it. The protections added in rounds 1 and 2 applied to later catching-up only. A node claiming a billion blocks and feeding real ones slowly held the start-up loop open for ever. | Start-up is now a series of ordinary passes, with everything they do to avoid being misled. It continues only while a seed claims a better chain and each pass brings the node closer. | `TestStartUpDoesNotWaitOnAStrangersClaim`, `TestStartUpCatchesUpFromASeed` |
| B | **Updating on Windows as the guide said made the miner pay the maintainer** | High | The repository contained a `miner_config.json` holding the maintainer's address. Unpacking a new download over an existing folder replaced the user's own, and the launcher, having been told once that the address was confirmed, started mining to it without asking. | The file is no longer in the repository. The launcher's confirmation now holds a copy of what was confirmed, and asks again if the saved address is anything else. | Launcher dry run |
| C | **Miners would have waited five minutes at launch** | Medium | A node at height 0 was read as a node that had not answered. At a launch every node is there, so each miner, six minutes after the anchor block, would have held off for five more. Any remembered node could also hold a miner back by claiming to be ahead. | A node at height 0 has answered. Whether to hold the miner is decided on the word of the seeds alone. | `TestANodeAtTheStartCountsAsAnAnswer` |
| D | **The launch window ended in a cliff** | Medium | Relief for a late block returned in full on block 21. Whoever mined the first 20 blocks could date them close together, let the real clock run on, and have block 21 read as hours late: the burst the window prevents, one block later. | The relief returns gradually: one halving allowed on the first block after the window, two on the second, and so on over 40 blocks. | `TestReliefReturnsGraduallyAfterTheLaunchWindow` |
| E | **One unit of fee emptied every pool of fee-less transfers** | Medium | With the pool full, any fee at all pushed out a fee-less transfer. 5,000 transfers paying the smallest possible fee removed them all, everywhere, and a sender could not then replace their own with one that paid. | A fifth of the pool is kept for fee-less transfers of settled coins; a paying transfer displaces them only above that. An owner may replace their own waiting transfer with one paying more. | `TestTheSmallestFeeDoesNotSweepOutFreeTransfers`, `TestFreeTransfersKeepTheirShareAndNoMore`, `TestAnOwnerCanReplaceTheirWaitingTransferByPayingMore` |
| F | **75 addresses could close the seeds to everyone** | Medium | A node took 100 connections in all, 4 per address, kept for as long as the holder kept talking. | A seed is always let in. Others get 2 per address out of 200. This raises the cost of the attack; it does not end it (see below). | |
| G | **An old request for blocks still answered without a size limit** | Medium | Round 1 removed the code that used it but left the code that answered it: 64 blocks of any size, five times a second. | Removed. | |
| H | **A seed that missed one request was set aside as a liar** | Medium | "No answer" was read as "has a different chain". Blocks that could not be placed, which cost nothing to make, could start a pass each, and every pass asked each seed for its whole newest block. | A lost request ends the pass without a verdict. Passes started by unplaceable blocks are limited to one per 30 seconds. Chains are compared through a small request that carries no transactions. | `TestASourceThatStopsAnsweringIsNotBlamed` |
| I | **Every mining thread checked the whole pool for itself** | Medium | On each new block every thread, one after another, re-checked every waiting transfer. With a full pool the last of 32 threads had barely started before the next block. | The choice is made once per tip and shared. | `TestTransfersAreChosenOncePerTip` |
| J | **The built-in miner kept one block date for minutes** | Low | A template was kept for 100,000 hashes, which under RandomX is minutes. | It is renewed every 10 seconds. | |
| K | **The fast miner was looked for in the start folder** | Low | A file of the right name in whatever folder the program was started from would have been run. | It is looked for beside the program only. | `TestFastMinerIsNotTakenFromTheStartFolder` |
| L | **Smaller ones** | Low | A peer could be judged while the chain was half-changed. Transfers in removed blocks were dropped. Every block of a catch-up was announced. A failed rollback still moved the height in memory. The desktop wallet prefilled a fee, did not ask before sending, and did not say that combining charges the fee per transfer. The guides gave one port for every network, said five minutes where the code allows ten, and did not say that the Windows text wallet shows the key as it is typed. | Each fixed. | `TestNoPeerIsJudgedWhileTheChainIsChanging`, `TestTransfersInRemovedBlocksGoBackToWaiting` |

## Not changed, and why

| Finding | Severity | Decision |
|---|---|---|
| **Enough addresses can still fill a node's connections** | Medium | Open. With 2 per address and 200 places it takes 100 addresses, and seeds are always let in, but the places are still first come, first kept. The real answer is to prefer peers that have delivered valid blocks, which belongs with the planned work on node-to-node connections. |
| **A backdated block 20 leans on difficulty by up to 1% a block until the lag is made up** | Low | Accepted. It is bounded, and it is the price of counting the schedule from a block inside the chain at all. |
| **A slow start if the starting difficulty is set far too high** | Low | Accepted, as in round 2. The value is ours to set. |
| **The fast miner is identified by what it says about itself** | Low | Open. Checking it against published file hashes needs those hashes to be published with each release of the miner. The guide now says plainly that the check is against mistakes, not forgery. |
| **A miner connected to the work server and doing nothing pauses the built-in miner** | Low | Open. It applies only when the work server is opened to other machines, which is off unless asked for. |
| **32-bit builds** | Low | Not supported. Difficulty is held in a number that is too small on 32-bit systems. No 32-bit build is published. |
| **Many database write results are not checked** | Not assessed | Open. What a full disk does part-way through a change has not been traced. |

## What the reviewers checked and found sound

One block worked through by hand, with the vault payout, the fee split and the totals all exact. The supply schedule and its total. A sweep across the removal and re-application of its block. Replacing one block by its rival in one database step. The block identifier covering every field. Reward identifiers not colliding. Nobody but the owner able to invalidate a signed transfer. The mining rule being the same in the miner, the work server and the checker. A lying answer in the halving search not producing a worse chain. The wait for the Bitcoin anchor.

## What this round could not settle

- How long a full pool really delays mining threads, and how far Bitcoin block timestamps lag: neither reviewer could run the code.
- The RandomX engine and the CVN build of XMRig are not in the pack.
- One reviewer did not read the test files in full.
