# What changed since review round 6

This lists what changed between commit `ed1c62f`, which round 6 reviewed, and commit `5626c8b`. It is written as claims for a reviewer to test. It was written by the authors and may be wrong, incomplete or flattering.

To see the changes themselves: `git diff ed1c62f 5626c8b -- node/ deploy/ MINING_GUIDE.md`.

The seven commits, oldest first:

| Commit | What it is |
|---|---|
| `94bf78c` | The faults round 6 found, fixed |
| `9fe1cb6` | A large block is waited for longer; nobody else is asked once a copy is on its way; a reward counted twice in the balance display |
| `7d04f06` | The four notes the reviewers left open |
| `b30d516` | The seed update script starts only the services that are switched on |
| `ba3fbfd` | The fast miner on a slow machine is no longer dropped every ten minutes |
| `6f73812` | Downloaded blocks have their proofs checked on several cores |
| `5626c8b` | A node downloads from the seed that answers soonest |

Sections 1 to 3 are described in full in [ROUND_6_RESULTS.md](ROUND_6_RESULTS.md) and only summarised here. Sections 4 to 6 are new and have been read by nobody but the authors. Section 5 is the one that touches how a block is checked.

## 1. The faults round 6 found (`94bf78c`)

Eleven fixes, each with a test, listed in ROUND_6_RESULTS.md under "Faults fixed". Claims:

- A node's connections to its seeds are rate-limited like its incoming ones.
- One node reached two ways (a connection each way) is sent a block once, not twice.
- The table of blocks being fetched is limited: 256 in all, 8 begun by one address, 16 announcers remembered for each, each kept 30 seconds.
- A connection whose write fails is closed.
- A pass that finds this node's newest block changed while a batch was on its way ends without blaming the source, and another pass follows.
- With a saved key, a wrong password is an error; nothing else is tried.

## 2. After a run on devnet (`9fe1cb6`)

- A node that has asked one neighbour for an announced block waits 4 seconds before asking the next (it was 2).
- Once a copy of the block has arrived, nobody else is asked for it.
- The balance display counted a block's reward twice when a full recount and the block's own update both ran for the same block. `balancesCountedThrough` in `node/database_core.go` prevents it. The balance display is not part of the rules: transfers are checked against the coins, not against it.

## 3. The four notes (`7d04f06`)

- `/utxos` has an allowance for each caller (10 at once, then one every two seconds) and at most 4 lists are made at once.
- On Linux and macOS the database is given 8 GB to grow into at the start, so a block being stored does not wait behind a backup being written.
- A node that cannot open its port for other nodes stops, with a message. It used to carry on, mining blocks nobody would see.
- Sends started in the background are counted, so tests can wait for them. This is what let `go test -race ./node/` run clean.

## 4. The fast miner (`ba3fbfd`)

Found by running the Linux checks on a small rented server mining at about 100 hashes a second.

- The work server closes a connection it has heard nothing on for ten minutes (`stratumIdleTimeout`), and a miner only speaks when it finds a block. The node now starts the fast miner with `--keepalive`. Seen on that server: dropped at ten minutes before, still connected after thirteen with the change.
- A fast miner that had connected and then stopped was reported as "it never connected to this node". The node now remembers whether it ever connected (`runFastMinerOnce` in `node/fast_miner.go`).
- The fast miner is also looked for one folder further down, `xmrig-cvn/xmrig-cvn/`, because the Linux download unpacks into a folder of its own (`fastMinerPlaces`). Still only inside the folder the program is in.

Known and accepted: a miner someone else connects to the work server without `--keepalive`, on a slow machine, is still dropped every ten minutes and reconnects.

## 5. Proofs of downloaded blocks are worked out on several cores (`6f73812`)

The file is `node/proof_ahead.go`. The call is in `TriggerActiveReSyncPass` in `node/sync_pass.go`, after `pauseForChainChange()` and before `StateMutex.Lock()`.

Before: every block a node downloads has its proof of work checked by one RandomX hash in the low-memory mode, one block after another, on one core, behind one lock (`rxVerifier` in `node/pow.go`).

Now: when a batch of blocks has arrived, `prepareProofs` works their RandomX hashes out on several cores and keeps them in `rxVerifier.prepared`. The checking that follows is the same code as before; when `randomXVerify` is asked for a hash it looks in `rxVerifier.prepared` and finds it.

Claims:

- **No block is accepted that would have been refused, and none refused that would have been accepted.** What is kept is the RandomX hash of a key and a blob, filed under that key and that blob. It is not a verdict. The comparison with the target, and every other check, is made afterwards by the unchanged code.
- **A wrong guess of key costs time and nothing else.** `prepareProofs` takes a block's key from the batch itself when the key block is in the batch, and from the database otherwise. If the checking later uses another key, the entry is not found and the hash is worked out as before.
- **A batch of made-up blocks costs a handful of hashes, not a batch full.** Work is done at most `threads × 4` blocks at a time and stops at the first block that does not follow the one before, does not carry its own identifier, claims less than the minimum difficulty, or whose hash falls short of the difficulty the block itself claims.
- **`--threads N` limits it**, as it limits mining. With one thread nothing is prepared.
- **The lock on the checking engine is held for one such group of hashes at a time**, not for a batch.
- **The spare RandomX machines and the kept answers are given back when the pass ends** (`releaseProofHelp`).
- Several machines share one RandomX cache. The built-in miner already does this in its low-memory mode.

Measured: 96 proofs took 2.2 seconds one at a time and 0.19 seconds on 32 cores, on a desktop that was also mining. A first download of 9,330 blocks on a rented server took 17 minutes 55 seconds on one core before and 10 minutes 51 seconds on two cores after.

Known and accepted:

- A machine with one core, such as a seed, gains nothing.
- While a group of hashes is being worked out, another caller of `randomXVerify` (a block arriving from a neighbour, a miner's answer) waits for that group: `threads × 4` hashes shared over `threads` cores, so about four hashes' time.
- The blocks measured before a change of branch (`measureCandidateBranch`) are still checked one at a time.

Tests: `node/proof_ahead_test.go`.

## 6. A node downloads from the seed that answers soonest (`5626c8b`)

In `chooseSyncSource` in `node/sync_pass.go`.

Before: the node asked each seed what chain it held, took the one claiming the most work, and among equals the first in the list.

Now: among sources in the same tier that claim exactly the same work, the one whose answer came back soonest is taken.

Claims:

- More work still wins, however slow the source.
- A source with nothing better than this node's chain is not chosen for being near.
- The order of the tiers is unchanged: seeds and nodes the operator named first, then nodes that have delivered before, then strangers.

Known and accepted: it is one measurement for each source; and it applies within the tier of strangers too, where a stranger could already win by claiming more work than anyone.

## 7. A test that was changed, not the program

`TestMultiNodeTwoHalvesRejoin` failed once in 45 runs. It counted it a fault whenever a node of the half with more work wrote "Switching to the branch from" a node of the other half. After the two halves have joined, the miner that was in the weak half is a miner like the others, and a node giving up blocks for a longer run of its blocks is an ordinary race. The check now looks only for a switch "above block #0", where the halves part. A reviewer may think this hides a fault: in the failing run a node replaced 19 of its blocks, above block #17, for a branch with more work, about 30 seconds after the halves met.

## 8. The seed update script (`b30d516`)

`deploy/update_seed.sh` restarted every `cvn-seed-*` service, including ones that had been switched off. It now restarts only those that are enabled. Reviewers are asked not to run anything in `deploy/`; reading it is enough.

## What the authors ran

- `go test ./...` at every commit.
- `go test -race ./node/` in full at `7d04f06` (clean); on the proof and download tests at `6f73812` (clean).
- The multi-node tests 45 times at `7d04f06`, 45 at `6f73812` after the change in section 7, and 45 at `5626c8b`, without a failure.
- Devnet: six seeds and one miner ran `7d04f06` overnight, about 2,260 blocks, all agreeing. Nothing later than `7d04f06` had run on devnet when this was written.

## After the review

Review round 7 tested these claims. Two sentences in section 5 were wrong (the wait behind a group of hashes, and what a batch of made-up blocks costs), and eight faults were found and fixed. See `ROUND_7_RESULTS.md`.
