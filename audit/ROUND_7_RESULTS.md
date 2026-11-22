# Review round 7: what the reviewers found and what was done

Reviewed commits: `b1a2484` (Google Antigravity) and `c7c7d64` (Claude Code, OpenAI Codex). This was a short confirming review of the nine commits since round 6, set out as claims in `CHANGES_SINCE_ROUND_6.md`.

Every finding was checked against the code. Each one that held has a test that failed before the fix and passes after it. The tests are in `node/round7_fixes_test.go`; most were written by the Claude reviewer and are kept there, turned round to pass.

No finding touches the rules every node enforces. None lets a block be accepted that the rules refuse, and none lets two honest nodes end up disagreeing.

## The reviewers

| Reviewer | Ran the code | Result |
|---|---|---|
| Google Antigravity | Yes | 5 findings. All five hold, at a lower severity than it gave them; one needs no fix. Its list of "claims tested and found true" names seven tests and one constant that do not exist, so that part of its answer was not relied on. |
| Claude Code | Yes | Judged the five, found the cause of the first, and added 3 findings of its own, each with a test. Also measured what the changes file had only estimated. |
| OpenAI Codex | Could not | Its environment refused connections to the same machine, the tests waited, and the run was ended at Go's ten-minute limit. It stopped there, as the prompt told it to. |

## Faults that were real, and fixed

### 1. A block that arrives while chains are compared was taken off and downloaded again (Claude new 1, Medium)

A pass read this node's newest block, compared chains with its source over the network, and only then looked at the newest block again. If the next block of the same chain arrived from a neighbour in between, the point where the chains "part" was one below the newest block, and the pass took that for a rival branch: it took its own good block off, stopped the miner, and downloaded the same block again. The fault was in release candidate 11 as well.

Fixed in `TriggerActiveReSyncPass`: the point found is acted on only if the newest block is still the one the comparison was made against. If it is not, the chains are compared again as they now are, three times at most; after that the pass ends without a verdict and another follows in seconds.

Tests: `TestRound7ABlockThatArrivesWhileChainsAreComparedIsKept`, `TestRound7APassGivesUpComparingWhenBlocksKeepArriving`.

### 2. An honest node changing its own chain was set aside for ten minutes (Antigravity 1, Medium)

This is what finding 1 did to the node's neighbours. While its block was off, the node answered a request for that block with no blocks at all, a second after reporting it. The node that asked ended its pass with nothing gained and set the honest node aside. Seen once in fifteen runs of the multi-node tests.

Fixed twice over. The cause is gone (finding 1). And a source that sends no blocks from a height it reported is now asked its height again: if it still reports the block, the blocks are asked for once more; if it no longer does, or does not answer, the pass is excused, once. A second such pass from the same source within ten minutes, with nothing delivered in between, sets it aside as before. A source that goes on reporting a block and goes on sending nothing is set aside at once, as before.

A request for blocks that fails outright (refused, timed out) is treated the same way. It used to be blamed at once, where a failed request during the comparison was not blamed at all.

Tests: `TestRound7ASourceThatIsChangingBranchIsAskedAgain`, `TestRound7ASourceThatNeverSendsItsBlocksIsExcusedOnlyOnce`, `TestRound7ASourceThatStillClaimsABlockItDoesNotSendIsSetAside`.

### 3. A reply of any length had every proof in it worked out while the node stood still (Antigravity 2, Medium)

Two things together. The receiving side did not limit how many blocks a reply held (the sending side stops at 512, and a reply may be 64 MB: about 177,000 small blocks). And the proofs of the whole reply were worked out, with the miner stopped and blocks from neighbours dropped, before the rules looked at its first block. The reviewer's test: 2,048 made-up blocks claiming difficulty 1 held a two-core node still for 36 seconds, and the same blocks could be sent to every node again and again. Before the several-cores change (`6f73812`) the same reply cost no hashes at all.

Fixed in three places:

- `fetchBlockBatchChecked` cuts a reply to 512 blocks.
- The pass hands blocks over one small group at a time (`threads × 4`). Each group's proofs are worked out, then those blocks are checked and stored by the unchanged code, then the next group. The rules are never more than one group behind.
- `prepareProofs` works out nothing unless the first block stands on this node's newest block, is dated as a block there may be, and claims exactly the difficulty the rules require there.

What a sender can still have worked out for nothing is one group: 8 hashes on a two-core machine, 128 on a 32-thread one. To get that far it needs a block on the newest block of the node it is sending to.

Tests: `TestRound7ABatchTheRulesRefuseIsNotHashed`, `TestRound7ABlockWithoutItsProofCostsOneSmallGroup`.

### 4. A batch could name the RandomX key its proofs were worked out with (Claude new 2, Low)

A batch that began with a made-up "block 0" had nothing about that block checked, and its identifier was then taken as the key for the blocks after it. The node built a 256 MB RandomX cache for a key the sender chose (about 1.6 seconds, with the checking engine locked) and dropped one of the two engines it keeps.

Fixed by the third point of finding 3: the first block must stand on this node's newest block. Every block in a group must also carry its own identifier now, those before RandomX too.

Test: `TestRound7ABatchDoesNotNameItsOwnKey`.

### 5. A fast miner's time to connect ran on while the node waited for the clock (Antigravity 5, Low)

While the newest block is dated ahead of the clock the work server hands out no work, so no miner can log in. The 40 seconds a started fast miner has to connect ran on regardless.

Fixed in `runFastMinerOnce`: that time is not counted, as time spent catching up already was not.

Test: `TestRound7TheTimeToConnectWaitsWhileTheNodeWaitsForTheClock`.

### 6. A fast miner that failed after 61 seconds every time was restarted for ever (Antigravity 4, Low)

Only runs shorter than a minute counted as failures, and the count went back to zero after any longer run. The built-in miner stays idle while the fast miner is expected to mine, so nothing mined in the gaps.

Fixed in `superviseFastMiner`: a fast miner that stops five times within ten minutes is given up on, however long each run lasted, and the built-in miner takes over.

Test: `TestRound7AFastMinerThatKeepsStoppingIsGivenUpOn`.

### 7. Thirty-two addresses could fill the table of announced blocks (Claude new 3, Low)

Each address may begin eight blocks and the table holds 256, so 32 addresses announcing blocks that do not exist filled it. A real block announced by an honest neighbour was then not asked for, and a large block is sent only to nodes that ask; it came by a later pass.

Fixed in `handleBlockAnnouncement`: when the table is full, the oldest block begun by the address holding the most places gives way, as long as that address holds more places than the one announcing now. (Tightened after the fixes were reviewed: see finding 2 under "The fixes, reviewed".)

Test: `TestRound7AFullTableOfAnnouncedBlocksMakesRoom`.

### 8. After the newest block changed while a branch was measured, the next pass waited for a trigger (Claude, from reading; Low)

That way out of a pass did not ask for a pass to follow, and wrote no line in the log. The follow-up came with the next trigger, 30 to 60 seconds later, where every other "the newest block changed" ending is followed in three seconds.

Fixed: it asks for the pass and says so in the log. Test, written by the reviewer of the fixes: `TestRecheckATipChangedWhileABranchIsMeasuredIsFollowedUp`.

## Reported, and left as it is

- **A node kept from catching up by blocks found during every download (Antigravity 3).** Only a block that is valid on this node's newest block and is not on the source's chain ends a pass that way: the node's own miner, or another miner on the same stale block. For a node with half the hashrate and a half-second fetch the chance is 0.6% per pass, and four in a row, as in the reviewer's test, about one in a billion. The suggested fix, pausing the node before blocks are fetched, was rejected in an earlier round on purpose: it lets anyone who claims a better chain stop a node's miner for the length of a download. It stays rejected.
- **A seed that answers first and delivers slowly is chosen every time; each silent seed adds two seconds before a pass begins (Claude, from reading).** Both were true of the first seed in the list before the nearest-seed change. Left for when there are seeds not run by one person.

## Sentences in the changes file that were wrong

- Section 5 said another caller of `randomXVerify` waits "about four hashes' time" behind a group. Measured, it is 7 to 11 hashes' time (about 0.2 seconds on the machine used), because the work begins its next group at once and a waiting caller sits through two.
- Section 5 said "a batch of made-up blocks costs a handful of hashes, not a batch full". That was true only of blocks that fail the checks made before hashing. Blocks that claim the minimum difficulty and meet it passed, a batch full. See finding 3.

## Claims that had no test, and have one now

- A block is remembered as offered by 16 nodes at most, and followed for 30 seconds: `TestRound7AnnouncersAndTimeAreLimited`.
- A connection whose write fails is closed: `TestRound7AConnectionWhoseWriteFailsIsClosed`.
- The largest block the rules allow is a quarter of the largest message a node sends: `TestRound7TheLargestBlockAsSent`.

Still without a test: the 8 GB of room for the database on Linux and macOS, which cannot be run on Windows.

## What was confirmed

- A prepared proof cannot be read back for a check that should use another key (the reviewer prepared a whole other chain, with its own key blocks, and found no answer misused).
- The spare RandomX machines are all given back when a pass ends: about 65 MB per engine at 32 threads while it runs.
- Nothing the program builds comes near the 2 MB message limit of `c7c7d64`: the largest block as sent is 500,038 bytes.
- Go 1.27.4: the RandomX tests pass with the garbage collector running constantly (`GOGC=1`).

## What nobody looked at

Linux and macOS. A run with large pages on. A real XMRig in place of the stand-in. `deploy/update_seed.sh`.

## The fixes, reviewed

Codex (OpenAI) reviewed commit `3f3db0a` on 23 November, on Windows, in a clone of its own: for each fix it took the fix out and ran the test named for it, and then went at eight places where the fixes might have broken something. Its tests are in `node/round7_recheck_test.go`, turned round to pass on the code as it is now.

### Does each test fail without its fix?

| Finding | Fails without the fix |
| --- | --- |
| 1, 4, 5, 6, 7 | Yes |
| 2 | Two of the three tests yes. `TestRound7ASourceThatStillClaimsABlockItDoesNotSendIsSetAside` passes either way: it shows the other side, that such a source is still set aside. |
| 3 | The cut to 512 blocks and the difficulty check: yes. The groups: **no**, see below. |
| 8 | Had no test. The reviewer wrote one, and it fails without the fix. |

### What it found

**1. Sources that had delivered were remembered for good, and all of them were asked at the start of every pass (Medium).** A source that delivers a better chain is put on a list that is asked before the nodes merely connected. Nothing left the list but a source that was chosen and then failed, and one that has gone away is never chosen. Each takes two seconds to give up on, one after another, before a pass looks at anyone else: eight took 16 seconds in the reviewer's test, and the list only grew. Each entry needs a node that really delivered a block, so it grows slowly, with the nodes that come and go.

Fixed in `chooseSyncSource` and `provenSyncSources`: a source is forgotten the first time it does not answer, a day after it last delivered, or when eight more recent ones fill the list. A node that is still about is still asked among those connected. Test: `TestRecheckSourcesThatWentAwayAreForgotten`.

**2. A full table of announced blocks could push out a block being fetched from an honest neighbour (Low).** Round 7's fix made room at the cost of an address holding more places than the one announcing. With the table full of addresses holding one place each, a new address holding none pushed one out. It takes 256 addresses.

Fixed in `handleBlockAnnouncement`: an address gives way only if it would still hold at least as many places afterwards as the one announcing. A table filled by 256 addresses with one place each therefore takes no new block until a place is thirty seconds old, as before round 7; the block then comes by the next pass. Test: `TestRecheckAFullTableKeepsABlockBeingFetched`.

**3. The test for "a group at a time" passed with the groups taken out (Low; a fault in the test).** `TestRound7ABlockWithoutItsProofCostsOneSmallGroup` calls `prepareProofs` itself, which already stopped at the first group whose proofs fall short. What the groups add is the case where every proof is good and the rules refuse the block for another reason.

New test, through a pass: `TestRecheckADownloadWorksProofsOutAGroupAtATime`. One mined block followed by 511 made-up ones that claim difficulty 1. With the groups, 8 proofs are worked out; without, 512.

Each of the three new tests fails with its fix taken out.

### What it confirmed

- Every path that adds or removes a block looks at the sync flag while holding the chain lock, so nothing changes the chain between two groups. `prepareProofs` running while blocks are stored: no race under the race detector.
- A first download of 500 RandomX blocks is not slower than on `c7c7d64`: 11.7 s against 13.8 s on 2 threads, 5.2 s against 6.7 s on 32 (one run each).
- A source that reports a height and sends no blocks is not asked for ever: one excuse, then set aside.
- A rival at the same height arriving during the comparison: the pass ends on the right chain.
- Nothing that fetches blocks relied on a reply longer than 512.
- The leftover-goroutine race in the tests is a fault in the tests, as the authors judged.
- The fast miner's time to connect can be held back about 11 seconds by one block dated as far ahead as allowed. To keep it held back takes a stream of valid blocks a second apart, each harder than the last.

### What it did not reach

A second comparison that ends above the reorganisation limit. A source that goes silent only in the second comparison. A block waiting for a parent whose entry was pushed out of the table. Linux and macOS, large pages, a real XMRig, `deploy/`.
