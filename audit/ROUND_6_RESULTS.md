# Audit round 6: what the reviewers found and what was done

Reviewed commit: `ed1c62f`. Five reviewers were given the prompts in `AUDIT_PROMPT.md` (7A, 7B, 7C). Three could run the code (Claude Code, Google Antigravity, OpenAI Codex); two read the pack (Google AI, Grok).

Every finding was checked against the code. Each one that held has a test that failed before the fix and passes after it. The reviewer's own tests are kept in `node/round6_findings_test.go` and `node/multinode_flood_test.go`; tests written for the fixes are in `node/round6_fixes_test.go`.

No finding touches the rules every node enforces (section 1 of `CHANGES_FOR_ROUND_6.md`). All three reviewers that examined the rounding, the sweep and the plain names found them as claimed.

## The reviewers

| Reviewer | Ran the code | Result |
|---|---|---|
| Claude Code | Yes | 7 findings, all demonstrated with tests. 30 repetitions of the multi-node tests, two extra arrangements with release candidate 11, race detector on real nodes. |
| Google Antigravity | Yes | 4 findings. Two hold (one is the same as one of Claude's), one is a clash between two test runs, one is in the tests only. |
| Google AI | No (pack) | 4 findings. Two hold, two are kept as designed. Also two small points. |
| Grok | No (pack) | No findings. |
| OpenAI Codex | Could not | Its environment refused connections to the same machine, so the baseline failed and it stopped, as the prompt told it to. |

## Faults that were real, and fixed

### 1. A connection this node opened had no message allowance (Claude 1, High)

A node calls up any node that says hello to it. What then arrived on that connection was not counted against any allowance. A stranger could send announcements as fast as the line carried them: 5,000 were acted on in under half a second, each a read of the database, a table entry and a log line.

Fixed in `runRelayUplink`: the two allowances that apply to a connection opened to this node now apply to one it opened. The table of blocks being fetched is also limited: 256 in all, 8 begun by any one address, each kept 30 seconds.

Tests: `TestRound6AnUplinkHasNoMessageAllowance`, `TestMultiNodeRound6AStrangerIsCalledAndFloodsTheUplink`, `TestOneAddressCannotFillTheTableOfAnnouncedBlocks`.

### 2. A wrong password made of digits was taken as a private key (Claude 2, Google AI 1; Medium to High)

With a key saved under a password such as `19841206`, typing `19841207` did not give "wrong password". The wallet took what was typed as a short private key and carried on with some other, empty address. "Show the 24 recovery words" would then show the words of that wrong key for the owner to write down.

Fixed in `signingKeyFrom`: with a key saved, anything that is not 24 words or a whole key is the password, and a wrong password is refused. The recovery-words screen now shows the address the words belong to and asks the owner to check it.

Test: `TestRound6AWrongPasswordOfDigitsIsTakenAsAPrivateKey`.

### 3. Two nodes behind one public address: each block went to only one (Claude 3, Medium)

Blocks were sent "once to each address". Two nodes in one house or office, or behind an address shared by a mobile carrier, look like one address from outside, so each block reached only one of them and the other waited for a pass.

Fixed in `sendOverOpenConnections`: two connections count as one node only when they are with the same address and the far end gave the same name on both (the random name each node makes up at start and already sends when it subscribes or acknowledges).

Test: `TestRound6TwoNodesBehindOneAddressAreBothSentABlock`.

### 4. A pass blamed an honest source when the newest block changed during the download (Claude 4, Antigravity 1; Medium)

While a pass was fetching blocks, the node's own miner could find a block, or a rival of the newest block could take its place. The fetched blocks then no longer fitted, the first was called "not valid", and the source was set aside for ten minutes (or, when the node's own block had added work, wrongly counted as having delivered while the node stayed on the shorter chain).

Fixed in `TriggerActiveReSyncPass`: the pass notes the newest block when it asks for a batch. If it is different when the batch arrives and is not one of the blocks fetched, the pass ends without a verdict and another follows. The rule for rivals in `placeWireBlock` now checks, under the lock, that no pass has begun changing the chain.

Antigravity's suggested fix, pausing the node before every download, was not used: a node would then stop mining whenever anyone claimed a better chain.

Tests: `TestRound6HonestSourceIsSetAsideWhenARivalBlockIsAdoptedDuringTheDownload`, `TestABlockFoundDuringADownloadDoesNotBlameTheSource`.

### 5. A block that arrived during a pass could still be forgotten (Claude 5, Low)

Two paths. The note that a block was dropped was cleared before the 3-second spacing was checked, so when the spacing said "not yet" the block was forgotten. And a block that did not fit, arriving while a pass was already running, used up the 30-second allowance without starting anything.

Fixed in `catchUpAfterMissedBlock` and `requestSyncPassForBlock`: the pass for a missed block is put off, not dropped; a block that does not fit during a running pass is noted as missed, so the next pass fetches it.

Tests: `TestRound6ASecondBlockMissedInARowIsForgotten`, `TestRound6ARequestForAPassIsSpentWhenNoPassStarts`. The second had one line added: the test ended its pretended pass without the call every real pass makes when it ends.

### 6. A node that announced a block and never sent it could keep the honest announcer from being asked (Claude 6, Low)

Nodes that announced the same block drew for the next turn every two seconds. A node announcing over and over held nearly every turn.

Fixed in `handleBlockAnnouncement`: each address is asked once for a block, and those that announced it are asked in the order they did.

Test: `TestRound6TheHonestAnnouncerIsNeverAsked`.

### 7. A large block was sent whole to nodes called up afresh (Claude 7, Low)

Claim 2 of section 2 ("no block over 16,384 bytes is sent without being asked for") held only on open connections. Each node also called up to eight others afresh and sent them the whole block.

Fixed in `callAndSend`: on a fresh call the caller first says it understands announcements. A node of this version says so back, is sent the announcement, and is handed the block only if it asks. A node of an older version says nothing and after one second is sent the whole block, as before.

Tests: `TestRound6ALargeBlockIsSentWholeToANodeWithNoOpenConnection`, `TestALargeBlockOnAFreshCallIsAnnouncedToANodeThatUnderstands`.

### 8. A rival of the newest block could be kept waiting four seconds (Antigravity 3, Claude in passing; Medium)

A block that overtakes its parent is kept until the parent is in place. The check was "the parent is the newest block", which is never true of a rival, so a rival waited out the full four seconds when its parent had been fetched a moment before.

Fixed in `placeAfterItsParent`: the wait ends as soon as a block stands at the parent's height.

Test: `TestARivalOfTheNewestBlockIsNotKeptWaiting`.

### 9. A list of nodes that was not signed was still read (Google AI 3, Low)

`askForPeers` checked the signature only to decide whether to list the sender, then read the list anyway. It now stops at a bad signature. (The signature is the same for every node of a network, so this is tidiness more than protection.)

### 10. A write that failed part-way left the connection open (Claude, not demonstrated)

`writeFrame` gave each message five seconds. If the time ran out with part of a frame written, the next message on that connection would have been read as the rest of it. The connection is now closed when a write fails.

### 11. The window was cleared with a Windows command on Linux (Google AI, cosmetic)

Harmless; now done on Windows only.

## Findings that were not faults in the program

- **A newcomer left at block 0 in `TestMultiNodeLimitOnReplacingBlocks` (Antigravity 2).** Two causes, both in the test and both now removed. Antigravity and Claude Code each ran thirty repetitions of the multi-node tests on the same machine at the same time; two test networks could draw the same range of addresses and find each other (Claude's report shows this from its side). The tests now hold a port on their range for as long as they run. Separately, the test itself had a race, seen again here in 1 run of 45: the stray node tells the newcomer of the network's nodes, and now and then the newcomer had taken the stray chain and already left it for the network's before the test looked. The test now accepts a log that shows both steps.
- **A data race on the network settings (Antigravity 4, Claude).** Between a test putting the settings back and a goroutine left over from a block stored earlier in the same test. Nothing in the program writes the settings after start. Real nodes built with the race detector reported nothing. The unit tests cannot yet pass under `-race`; that is noted as work to do.
- **A node with a chain stops if the node named with `--connect` is on another chain while every seed is silent (Google AI 4).** Kept. The operator named that node; being told plainly that it is on a different chain is the purpose of the check.
- **Transfers crowded out by a flood of invalid ones (Google AI 2).** Already listed as known. The estimate assumed 5 to 10 thousandths of a second to check a transfer; a signature check takes a small fraction of that. A dropped transfer is not lost. Left as it is, and still listed.

## Changed afterwards, from a run on devnet

Five full blocks were sent across devnet on the fixed code (Melbourne and six seeds). All arrived well inside a block interval, but Amsterdam asked all six other nodes in turn for two of them, and was sent each block several times.

- **The wait before asking a second node is 4 seconds, not 2.** A full block from Australia to Europe over an idle connection takes a little over two seconds, so the first copy was still on its way when the second node was asked. The wait matters only when the first node does not deliver.
- **Nobody else is asked once a copy has arrived**, however long checking and storing it takes. Test: `TestNobodyElseIsAskedOnceACopyHasArrived`.
- **The log says how long a requested block took to arrive and how long to check and store**, so a slow line can be told from a slow node.

## Found while repeating the tests

- **A block reward could be counted twice in the balances a node shows.** A fresh count of all balances (anyone opening the address page can cause one) that ran between a block being written and its reward being added to the running balances already included the block; the reward was then added again. The node showed 50 coins too many at the miner's address until the next fresh count. It affected only what is shown: blocks are checked against the coins themselves, never against these totals. Seen in 1 run of 45. A fresh count now notes the block it reaches and that block is not added again. Test: `TestARewardIsNotCountedTwiceAfterAFreshCount`.

## Noted, not changed

- `/utxos` reads every coin on each request, with no limit. Older than this round.
- A backup holds one read of the database for as long as the copy takes, and a block that makes the file grow waits for it.
- A node that cannot open its port carries on with no connections instead of stopping.
- The unit tests under the race detector (above).
