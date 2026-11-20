# Changes for audit round 6

What changed in the code after the last reviewed commit (`ab485df`, release candidate 7, 17 November 2026), stated as claims for a reviewer to test. Nothing in this file has been checked by anyone other than the authors.

Round 5 looked at one rule only (the exponential difficulty step). The last review of the whole program was round 4. Since then the program has gained about 5,200 lines outside its tests, most of them in how nodes talk to each other. This round is therefore of the whole program again, with the weight on sections 1 to 4 below.

How the sections are ordered: what could split the network or create coins comes first, what could only inconvenience a user comes last.

## 1. Three changes to the rules every node enforces

None of these has been reviewed by anyone but the authors. Nodes before and after each of them do not agree about every block; the development network was restarted on them (release candidate 9).

### 1.1 A difficulty step rounds to the nearest whole number

**Where:** `node/heartbeat_difficulty.go` (the product of the difficulty and `e^x`). Commit `a6fe85d`. It answers finding 5 of `audit/FINDINGS_ROUND_5.md`.

Before, the product was cut down to a whole number, so at the minimum difficulty a very small step up was lost and a small step down was not. Now a half rounds up.

**Claims.** Every machine gets the same whole number. The eight pinned values of the whole step in `node/exponential_steps_test.go` are right. No pattern of dates turns the rounding into more blocks for a miner.

### 1.2 The seven-year sweep leaves the smallest coins alone

**Where:** `node/sweep.go`. Commit `ee4a319`.

A coin unmoved for 4,905,600 blocks has a fifth moved to the vault. Before, a coin so small that four fifths of it would be below the minimum output (0.0001 CVN) was taken whole. Now nothing is taken from such a coin, then or later, and it is not looked at again.

**Claims.** No coin is ever swept whole. Coins plus vault still equal what was issued, across a sweep and across a reorganisation that undoes and redoes one. A coin left alone cannot be made to pay later by anything short of being spent. Leaving them alone opens no way to avoid the sweep that is cheaper than moving the coin: splitting a balance into coins that small costs more in block space than the sweep would take. (That last claim is an argument, not a test.)

### 1.3 Plain names inside a block

**Where:** `node/genesis.go`, `node/block_format.go`, `node/blockchain_core.go`. Commit `092cf2c`.

The reward transaction's fixed markers are `NO_KEY_BLOCK_REWARD` and `NO_SIGNATURE_BLOCK_REWARD`. The first block pays nothing to `NOBODY` in a transaction named `TX_GENESIS`. These texts are part of what is hashed, so every block identifier changed.

**Claims.** Nothing but the texts changed. No transfer can be made that carries either marker and is accepted as anything but a block's reward. `NOBODY` is not an address and nothing can be sent to it or spent from it.

## 2. How a block travels

**Where:** `node/block_relay.go` (new), `node/gossip_mesh.go`, `node/relay_uplink.go`, `node/blockchain_core.go`, `node/sync_pass.go`. Commits `cdc77fd`, `2318335`, and parts of `d6c5c7a`. Tests: `node/block_relay_test.go`, `node/place_on_tip_test.go`, and the tests with real nodes in `node/multinode_test.go` and `node/multinode_split_test.go` (built only with `-tags multinode`).

No rule of the chain changes here. A mistake here cannot make a bad block valid. It can stop good blocks arriving, which on a proof-of-work chain is how a network splits.

**What it was.** A node opened a new connection to every node it knew, for every block and every transfer, and sent the whole thing.

**What it is.**

- A node keeps up to 8 connections open to other nodes and serves up to 256 opened to it. Blocks and transfers go down all of them, in whichever direction each was opened. Up to 8 nodes with no open connection either way are also called up afresh, chosen at random each time.
- A block over 16,384 bytes is announced (`BLOCK_ANNOUNCE:` with height and identifier). A node that does not hold it asks (`BLOCK_REQUEST:`) and is sent it. It asks the first that announced, and another after 2 seconds if the block has not come. A node hands over only blocks it passed on lately (the last 32).
- A node announces only down a connection whose other end has said it understands (`RELAY_FEATURES:announce`). Any other is sent the whole block, as before.
- A small block that arrives before its large parent (which takes one exchange longer) is kept for up to 4 seconds and placed when the parent lands (`placeAfterItsParent`).
- Transfers have a message allowance per address separate from everything else (each: 50 at once, 20 a second).
- Transfers from other nodes are put in a queue of 1,024 and checked by two workers, away from the connection they came in on. A transfer that finds the queue full is dropped.
- A block is passed on once. It used to leave a node twice.
- **Every block added one at a time goes through `placeOnTip`**, which holds the chain's lock while it reads the newest block, checks the new one and stores it: a block from a neighbour, a block the built-in miner found, a block the fast miner handed in. A pass that compares chains (`TriggerActiveReSyncPass`) holds the same lock while it adds its blocks.
- A block that arrives while such a pass runs is not kept, but the node remembers that one came and runs another pass as soon as the first ends (`catchUpAfterMissedBlock`).
- A pass that finds a block "not valid" which the node in fact already holds at that height carries on, and does not blame its source.

**Faults the authors found here, by running the tests with real nodes dozens of times.** Each failed about one run in thirty to fifty and could not be produced on demand, so the evidence that they are fixed is a count of clean runs, not a proof.

1. A block that arrived during a pass was dropped and not asked for again for up to 30 seconds.
2. A small block overtook its large parent and set off a needless comparison of chains.
3. A burst of large transfers held up the block behind them on the same connection for seconds (created by this change, then fixed by the queue).
4. A block from a neighbour and a pass added the same block at the same moment. The pass then judged the block not valid, or its source not to have delivered, and set an honest node aside for ten minutes. **This fault is in release candidate 11 and earlier.**
5. The built-in miner and the work server stored a mined block with no lock, the same gap as 4.

**Claims.**

1. A node running this version and a node running release candidate 11 pass each other every block and transfer (`TestMultiNodeOldAndNewVersionsTogether`).
2. Among nodes of this version, no block over 16,384 bytes is sent without being asked for.
3. No message a stranger can send makes a node do work out of proportion to the message: read a block from disk, verify signatures, send a large reply, or hold memory, beyond what the two allowances permit.
4. With `placeOnTip` and the pass under one lock, no two paths can store a block on the same parent, and no path stores a block checked against a newest block that has since changed (`TestOnlyOneOfManyBlocksOfferedAtOnceIsPlaced`).
5. Nothing takes the chain's lock while already holding it, and nothing waits on the network while holding it.
6. A node that is announced a block and never sent it is not left behind: it asks elsewhere, and failing that the ordinary comparison of chains still runs.
7. Two halves of a network that each mined a chain end on the one with more work, with the same coins counted everywhere (`TestMultiNodeTwoHalvesRejoin`).

**Known and not fixed.**

- An announcement makes the receiver look up the block at that height to see whether it holds it, which reads a whole block. A node sending false announcements costs the receiver that much per message, within the allowance.
- The table of blocks asked for is pruned only past 256 entries, and then only of entries over a minute old.
- A node that holds 256 inbound connections open to a seed denies them to others. Seeds are always let in; nobody else is.
- How many nodes the network carries is not measured. Nothing here has run on more than six machines or with real distance between more than seven.
- The message key (`getNetworkAuthSecret`) is in the source. It separates networks; it authenticates nobody.

**Things a reviewer might try.** Find a sequence of messages that leaves a node a block behind for good. Make a node of this version and one of release candidate 11 disagree about what they hold. Make `placeAfterItsParent` hold a block that should have been placed, or place one that should not. Find a deadlock: `placeOnTip`, the pass, `ProcessIncomingWireBlockWithDelay`, the miner and the HTTP handlers all touch `StateMutex`. Starve block delivery with anything other than blocks. Use `BLOCK_REQUEST` or `BLOCK_ANNOUNCE` to learn or cause something they should not.

## 3. A node counts Bitcoin's blocks to its starting block itself

**Where:** `node/bitcoin_headers.go`, `node/bitcoin_peers.go` (both new), `node/anchor_wait.go`, `node/genesis_watch.go`. Commit `2fc200a`. Tests: `node/bitcoin_headers_test.go`; against real Bitcoin, `node/bitcoin_live_test.go` (runs only with `CVN_LIVE_BITCOIN=1`).

A launch network starts from the Bitcoin block at an announced height. A Bitcoin header proves real work but not its height. A waiting node used to take the block at that height on the word of two block explorers out of five.

It now carries one Bitcoin header fixed in the source (`bitcoinStart`, block 975,744, the first block of a 2,016-block period) and follows every header from there to the height, checking each: it names the header before it; its hash meets the target it states; that target is the one Bitcoin's rule gives at that height (`nextBitcoinBits`, worked out afresh every 2,016 blocks); its date is later than the middle one of up to eleven before it and not more than two hours ahead of the clock. Of two branches it keeps the one with more work. Headers come from computers on the Bitcoin network (found through eight DNS names, port 8333, the `getheaders` exchange only) and from the five explorers whenever those bring nothing new.

**Claims.**

1. `nextBitcoinBits` and `compactFromTarget` give what Bitcoin gives, bit for bit. They match four cases from Bitcoin's own test suite, and, started from block 973,728, the real change of target at block 975,744.
2. No source can make a node take a header that is not at the announced height on a branch that cost real Bitcoin work to make.
3. A source that sends wrong headers, or none, can delay a node and nothing more.
4. Nothing read from a Bitcoin peer or an explorer can crash the node or make it hold memory without limit (messages over 1 MB are refused; a reply holds at most 2,000 headers).
5. A network scheduled at or before the fixed block is refused by a test (`TestScheduledNetworksStartAfterTheFixedBitcoinBlock`); such a height would fall back to the explorers.

**Known and not fixed.**

- A header supplied by hand (`--genesis-anchor`, or the anchor file) is checked for real work and its date and is not counted up to. A wrong one puts the node on a network of its own, which the check in section 4 then reports.
- The date check uses fewer than eleven earlier blocks for the first ten headers after the fixed block.
- Two real Bitcoin blocks at the announced height (about one height in several hundred) can start nodes from two headers. The rule for that case is fixed in advance (the block still in Bitcoin's chain six blocks later) and the node warns; it does not move over by itself.
- A node waiting through the Bitcoin block arriving, and counting up to it, has run only in tests.

**Things a reviewer might try.** Find a header chain that passes and that Bitcoin would refuse, or the reverse (Bitcoin's rule that the block version must not be too old, and its limits on the first blocks of a period, are not applied: does that matter here?). Make the follower switch to a branch with less work. Make `offer` loop, panic or keep a half-applied branch. Find what an attacker who controls the node's DNS and its route to the explorers can do, and what it costs.

## 4. A new node joins the network that is there

**Where:** `node/join_check.go` (new), `node/chain_params.go` (`applySeedOverride`), `node/own_address.go` (new). Commits `539e28b`, `fb56801`.

A mining node that was given seeds asks them for their first block before it mines. One agrees: it carries on. They answer and none agrees: it stops ("DIFFERENT CHAIN"). None answers and it holds no blocks: it waits, mines nothing, and asks again every 10 seconds. None answers and it holds blocks: it carries on. `--start-alone` switches the check off. A seed address that cannot be read stops the program. A node knows its own addresses and does not list, call or download from itself.

**Claims.** A node never begins a chain of its own because its seeds were unreachable or mistyped. A node that already holds a chain is never stopped from running by seeds that are down. Nobody but a seed the operator named can cause the stop.

**Known.** One seed on another chain is enough to stop a new node if no seed agrees; if any seed agrees, the node carries on.

## 5. The miner

- **The built-in miner no longer re-reads the whole newest block on every hash** (`latestTipID` in `node/database_core.go`, commit `d6c5c7a`). On top of a full block that read cost more than the hashing. Claim: the identifier read from the total-work record is always that of the block `GetLatestBlockInternal` would return.
- **A mined block is placed under the chain's lock** (section 2).
- **A count of races** (`node/block_races.go`): heights at which a node saw two valid blocks on one parent. It changes nothing about which block is kept.

## 6. Wallets and keys

**Where:** `node/recovery_words.go`, `node/recovery_wordlist.go`, `node/wallet.go`, `node/renew.go`, `node/secret_input_windows.go`, `node/secret_input_other.go`, `node/app.go`. Commits `d87fa9b`, `a170263`, `68f9010`.

- **Recovery words.** A private key is also shown as 24 words: the key's 256 bits and 8 bits of its SHA-256, in groups of 11, from the BIP39 English list. The words are the key written another way; nothing is derived. Claims: the encoding matches BIP39's published test values; a wrong word is caught by the check 255 times in 256 and otherwise gives a different key, never the right one; the word list is the published one (SHA-256 in the file's header).
- **A saved key.** The text wallet can keep the key in `wallet_key.json`, encrypted with AES-256-GCM under a key made from a password by PBKDF2-SHA256, with a random salt. Claims: a wrong password is refused and reveals nothing; the file holds nothing else of use; the number of rounds is adequate for a key that controls real value (judge this).
- **Renew my coins.** One step sends coins near their seven-year date back to their own address. An ordinary transfer; no rule involved.
- **A key is not shown as it is typed**, on Windows as on Linux.

## 7. Backups of the chain

**Where:** `node/backup.go`. Commit `9e87614`.

A node copies its chain through the database two minutes after starting and then once a day, checks the copy (every block present and linked, each matching its identifier, the first block this network's, coins plus vault equal to what was issued) and keeps seven. `--check-backup` and `--restore-backup` work on a file by hand.

**Claims.** A backup is never taken from a file being written. A restore never leaves the node without a chain: the file replaced is kept. Nothing reachable from the network can start a backup or a restore, or name the file.

## 8. Programs that exist only for testing

`node/localnet_multinode.go` (a network on loopback addresses with a fast hash) is built only with `-tags multinode`. `node/fill_blocks_devtool.go` (a tool that fills blocks with a wallet's own coins) only with `-tags multinode` or `-tags devtools`.

**Claim.** A program built the way releases are built contains neither, refuses the network's name, and reads none of the `CVN_LOCALNET_*` settings (`node/release_build_test.go`). In particular `CVN_LOCALNET_REORG_LIMIT`, which lowers the limit on replacing blocks, cannot reach a released program.

## 9. The code was tidied

Commit `f4b1f39`. `Run` in `node/main.go` was 586 lines; the pages a node answers (`node/http_api.go`), the miner window (`node/dashboard.go`) and the built-in miner (`node/builtin_miner.go`) were moved out whole. Three copies of the code that reads a message became one (`readFrame`). Peer discovery became two functions. Names were changed; none on the wire.

**Claim.** Nothing a node does changed, apart from the wording of three log lines. Removed as dead: `SignalMinerReset`, and the miner window asking its own node over HTTP for a balance it held.

Not tidied, on purpose: `SaveBlockToStorageInternal`, `TriggerActiveReSyncPass`, `ValidateBlockSignatures`.

## 10. Outside the program

- `deploy/install_https.sh` puts Caddy in front of a seed's own port, with a certificate from Let's Encrypt. It runs as root on the seeds.
- `site/` is two static pages; one asks each seed from the reader's browser what it holds.
- The release workflow signs each file (build provenance) and the builds are reproducible.

## What has never run outside tests

A launch from a future Bitcoin block with nodes waiting and counting. More than seven machines. The limit on replacing blocks at its real figure of 100 (the test sets it to 6). The seven-year sweep and the first halving at their real heights. A second mining algorithm. The saved key and recovery words in anyone's hands but the authors'.
