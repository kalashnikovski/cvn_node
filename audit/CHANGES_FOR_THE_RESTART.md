# What changed for the restart of the development network

Written 26 November 2026 for the reviewers of the three rules below. Everything here is a claim by the authors. Where the code does not do what this says, that is a finding.

Three rules of the chain were changed together. None has a starting height: each holds from the first block of a new chain. No chain made under the earlier rules is valid under these, and no node may carry an earlier chain file or backup across the change.

## 1. Every fee is the miner's (change 3)

Before: while new coins were still being issued, a fifth of every transaction fee in a block went to the vault and four fifths to the block's miner.

Now: every fee in a block may be claimed by its miner, at every height. No part of a fee goes to the vault by rule. The setting for the share is gone from the code.

Unchanged: a block may pay its miner less than it is allowed (the new coins of its height, its fees, and the vault's payout for that height). Whatever it leaves unclaimed goes to the vault, so nothing issued is ever destroyed.

Where: `accountForBlock` in `node/vault.go`; the vault's new balance in `SaveBlockToStorageInternal` in `node/database_core.go`.

## 2. A fifth of every block is closed to transfers that pay a fee (change 8)

A transfer pays when its fee is more than nought. In any block:

- at most 400 transfers may pay (the limit of 500 transfers less a fifth of it), and
- the transfers that pay may measure at most 240,000 bytes together, by the rules' own measure of a transfer (the block limit of 300,000 bytes less a fifth of it).

Both must hold. The reward transaction is in neither figure. A block that breaks either is not valid. The rule never ends.

What it is for: a miner cannot fill the last fifth of a block with paying transfers, so leaving out transfers that pay nothing earns it nothing there. What it cannot do, and the documents say so: no rule can make a miner include a particular transfer; a sender and a miner who know each other can settle outside the fee; and with more than 400 paying transfers waiting and no free ones, the 401st waits although the block has room.

The program fills its own blocks from the same two figures (`paidTransferLimits`), so it cannot build a block the rule refuses.

Where: `paidTransferLimits`, `ValidateBlockTransactions` and `fillBlockFromCandidates` in `node/tx_rules.go`.

## 3. A swept fifth is kept on the coin for its owner for seven years (change 1A)

Before: a coin not spent for seven years lost a fifth of its amount to the vault at the end of the block where it reached that age, and its age was reset.

Now: every coin record can carry one more figure, what is set aside on it (`held` in the stored record, `HeldUnits` in the code, left out when nought). At the end of the block where a coin reaches its seven-year date:

- whatever was set aside on it at its last date, and has not been taken back, goes to the vault;
- a fifth of its amount, rounded down, is taken off the amount and set aside on the coin, unless that would leave less than the smallest coin allowed, in which case no new fifth is taken;
- its age becomes that block, so it is looked at again one interval later;
- a coin too small for a new fifth and with nothing set aside is left exactly as it is.

A coin is worth its amount and what is set aside on it together (`SpendUnits`). A transfer that spends it must state exactly that sum as the input's amount, and is given exactly that sum. The coins it makes have nothing set aside. So spending a coin, to anyone or to its own address, takes the fifth back, and a transfer signed for a coin before its second date is no longer valid after it.

At every block: the amounts of all coins, plus everything set aside on coins, plus the vault, equal exactly what has been issued.

The sweep happens at the end of the block, after the block's own transfers. A block that competes for the place of the newest block is judged against the coins as they were before the newest block, including before its sweep.

Where: `UTXORecord` in `node/blockchain_core.go`; `applySweep`, `undoSweep`, `sweepAmounts` and `nextSweepHeight` in `node/sweep.go`; `checkTransferAgainstCoins`, `lookupWithBlockUndone` and `undoBlockCoinChanges` in `node/tx_rules.go`; `SaveBlockToStorageInternal` and `RebuildStateBalanceCacheInternal` in `node/database_core.go`; `checkChainFile` in `node/backup.go`.

## Also changed, and not rules

- The wallets show what is set aside and until when, renew every coin with a fifth set aside, and send Renew and Combine with no fee unless the user asks to pay one (`node/renew.go`, `node/wallet.go`, `node/app.go`).
- The wallet notes each renewal it sends free and, if the coins are still unspent three days later, offers to send it again with a small fee (`node/renew_record.go`).
- When sending, a wallet user may give something to a second address in the same transfer: one more output, typed by the sender each time (`node/giving.go`).
- A pool pays an address owed less than 1 CVN whatever it is owed, from 0.002 CVN up, once it has gone twelve hours without a payment (`node/pool_payments.go`, `node/pool_pay.go`).

## After the first outside review of these changes (round 10)

- A damaged record of a sweep, or of the coins a block spent, now stops the rollback or the block that needs it. It used to be passed over (`node/sweep.go`, `node/tx_rules.go`).
- The quick check of a chain file (`checkChainFile` in `node/backup.go`) holds the vault after every block to that block and the vault before it.
- **Putting a backup back now works the chain out again.** `replayChainFile` in `node/backup_replay.go` starts an empty chain in a scratch file, gives it the backup's blocks one by one through the same two functions that take a block from another node (`ValidateBlockSignatures`, then `SaveBlockToStorageInternal`), and compares the result with the backup: every coin, the vault after every block, the total work through every block, and the records of what each block spent and swept. A record of spending or of a sweep that no block makes is refused. `--restore-backup` does this before anything is changed and holds the file it copies to the SHA-256 of the file it checked; `--check-backup-fully` does it by hand. The copy a node makes each day still gets the quick check only. There is no second copy of the rules: the replay borrows the node's one database handle (`GlobalBoltEngine`, `BlockchainFile`) for as long as it runs, which is why it can only run in a command and never inside a running node. That borrowing is the part most worth a reviewer's eye.

## After a security audit of the whole program (November 2026)

Five findings, none in a rule (`audit/out/fingings_securityaudit_codex.md`, not published). All five were real.

1. **Peer messages could exhaust a seed's memory.** Large messages (over 16 KiB) held at one time now share a fixed room, 16 MiB for strangers and 16 MiB for seeds, taken as the bytes arrive and given back when the message has been dealt with (`frameRoom`, `readFrameWithin`, `readLargePayload` in `node/gossip_mesh.go`; used in `serveInboundConnection` and in `node/relay_uplink.go`). A message that finds no room is taken off the connection and not kept; a connection with eight of those is closed. Answers that are always short are read to 64 KiB (`readSmallFrame`). Known limits: the room counts bytes as sent and the node holds several times that while decoding; a decoded block handed on for placing is not counted; whoever keeps the strangers' room full stops large blocks arriving from strangers, though not from seeds.
2. **A wallet read a node's answer without limit.** Every answer is read to a byte limit and a limit on list items (`decodeAnswerWithin` in `node/decode_limits.go`).
3. **The mining work port had no limits when no pool runs.** The pool's limits on connections and on answers without the work now hold for every miner that is not on the node's own machine, and an IPv6 address is counted by its first 64 bits (`handleStratumClient`, `minerAddressKey` in `node/stratum.go`).
4. **The seed's service ran as root.** `deploy/install_seed.sh` now runs it as a user of its own in a folder of its own, with the same limits as the pool's service, and moves an older seed's files across. Not run on a server yet.
5. **The update script could be made to delete a file outside its folder.** `deploy/update_seed.sh` accepts a plain file name only.

## After the second outside look at these changes (round 10 again, on `ce17a9a`)

The reviewer found no way to make or lose a coin with valid blocks, and no altered file that passes the full replay. It found four gaps in what a node does when its own chain file is damaged, and two in the seed scripts. None can be reached from the network.

1. **A record that is missing is damage, like one that cannot be read.** A node used to delete a record that was empty, so it could not tell "this block spent nothing" from "the record is gone".
   - The coins a block spent: the record is now held to the block every time it is used. It must list the coins the block's transfers name, all of them, in order, and no other (`spentRecordsOf` in `node/tx_rules.go`, used by `undoBlockCoinChanges` and `lookupWithBlockUndone`, and by the quick check of a chain file, `checkChainFile`, so the copy a node makes each day is held to it too). Nothing new is stored.
   - A block's sweep: every block now has a record, an empty list when its sweep touched no coin, and a missing one is an error (`storeSweepRecords`, `loadSweepRecords` in `node/sweep.go`). **This changes what a chain file holds.** A file written before it has no empty records and is refused; the restart already forbids carrying a chain file across.
   - The sweep also refuses to go on when the block one interval back is missing or cannot be read, or when a coin that is due cannot be read (`applySweep`). Both used to be passed over.
2. **A block that is missing or cannot be read stops a rollback.** It used to be deleted with its coins left in place. Now nothing is removed (`rollbackChainToHeightLocked` in `node/chain_work.go`). A block offered for a height whose stored block cannot be read was refused before too, but in silence, as a block already held; the node now says what is wrong (`SaveBlockToStorageInternal`).
3. **The full check never deletes the file it was given.** A file named like the check's own working file (`<chain file>.fullcheck-...`) is refused and left where it is (`replayChainFile`).
4. **The seed scripts.** `deploy/update_seed.sh` builds the new program before it stops the seeds, so a build that fails leaves them running. `deploy/install_seed.sh` finishes a move of an older seed's file that was cut off between its two steps. Neither has run on a server.

Known and left as they are, and believed at the time to be, all of them, a node standing still on its own damaged file and none a node going on with wrong coins (the next section shows a case this list missed): a coin that cannot be read is taken for no coin when a transfer is checked, so a sound block that spends it is refused as if the block were at fault (`bucketLookup`); a newest block that cannot be read is reported as the first block (`GetLatestBlockInternal`); a node whose rollback is refused tries the same switch again at every pass, with a line in the log each time; the total work is summed over the blocks that can be read (`sumChainWorkFromBlocks`).

Not changed: whoever keeps the strangers' room for large messages full stops large blocks arriving from strangers. It is the limit named above, and the reviewer's test of it shows the room refusing when it is full, which is what it is for.

What these do not do: a record that is present, fits its block and is wrong in what it says of a coin (its owner, its amount) is still believed by a running node. Only working the file out again from its blocks finds that, which is what putting a backup back does.

## After a third outside look, by another reviewer (on `1ff119f`)

It found no way to make or lose a coin with valid blocks, and could not break the fixes of the round before. It found one case in which a node with a damaged file did go on with wrong coins, which the section above said could not happen, and three smaller things. None can be reached from the network.

1. **The height a chain file records for its newest block is held to the blocks.** A node read a height that was missing or could not be read as "no blocks yet". It then started again from the first block with its old blocks and their coins still in the file, took rival blocks in their place one at a time, and let coins of the old blocks be spent. Now the height must be there, be a height, and be the last block the file holds (`storedTipHeight` in `node/database_core.go`). A file that fails this is not opened (`InitBoltEngine`). A block is stored only on top of the newest block or in its place (`SaveBlockToStorageInternal`). A rollback is refused (`rollbackChainToHeightLocked`). The newest block must itself be readable. And a file that holds coins or a height and no blocks at all is not taken for a new file, which it used to be: it was given a first block and its old coins stayed spendable.
2. **The first block is held to this network's first block in every part.** The checks looked at its identifier only, and the identifier is a field of the stored block. A file whose first block bore the right identifier and another date passed the full check, was put back, and the node then refused the second block every other node takes. Now the quick check requires the first block to be what its identifier says (`checkChainFile`), the full check requires it to be this network's first block in every part (`compareFileWithItsBlocks`), and a node refuses to start on a file where it is missing, unreadable or different (`checkStoredGenesis`).
3. **An amount that is not written as an amount is not passed over.** Such an amount is read without an error, as an amount that is not valid. The sweep took that for a coin too small to sweep. It now stops the block (`applySweep`), and the quick check of a chain file refuses the coin. The same is asked of every coin kept in a record of spent coins or of a sweep before the record is used (`coinRecordSound` in `loadUndoRecords` and `loadSweepRecords`): a rollback used to write such a coin back with an amount of nought.
4. **The seed scripts** (not run on a server). `deploy/update_seed.sh` makes the new program runnable by the seed's user whatever the server gives new files by default. `deploy/install_seed.sh` never deletes a copy left by a run that was cut off: it sets it aside and says so, because after a reboot in between that copy can be the whole chain. It no longer writes through a link put in the seed's folder.

Left as it is, on purpose: a backup can carry the note that its node's download has not yet been vouched for by a node that is not a seed, and while that note stands the limit on deep changes of chain does not hold for the blocks above it. The full check does not look at the note, so a file can be made to carry it when it should not. With the note a node is as open as one that has just joined, which is the safe side. Taking it away from a backup that rightly has it could hold a node for ever to a chain that only a seed vouched for. The reason is written at the top of `node/backup_replay.go`.

The reviewer's tests of the sync pass when a rollback is refused, of a newest block that cannot be read, and of the full check over a chain with a rollback, a sweep and the end of emission are taken into the suite (`node/damaged_file_test.go`).

## What the authors tested

For each rule, tests were written before the code and shown to fail without it. `TestCoinsAreNeverCreatedOrLost` runs a small economy through transfers, fees, sweeps, fifths set aside, taken back and expired, vault payouts, under-claimed blocks, the end of emission and rollbacks, and after every block checks that nothing was created or lost, that the sweep touched exactly the coins that were due, and that every other coin is unchanged. Faults were planted in the rules one at a time and the tests caught each. Real nodes on one machine were run through full blocks under rule 2 and through both sweep dates under rule 3.

All of that was done by the same people who wrote the code. None of it is a reason to believe the rules are right.
