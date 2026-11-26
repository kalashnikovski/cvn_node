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

## What the authors tested

For each rule, tests were written before the code and shown to fail without it. `TestCoinsAreNeverCreatedOrLost` runs a small economy through transfers, fees, sweeps, fifths set aside, taken back and expired, vault payouts, under-claimed blocks, the end of emission and rollbacks, and after every block checks that nothing was created or lost, that the sweep touched exactly the coins that were due, and that every other coin is unchanged. Faults were planted in the rules one at a time and the tests caught each. Real nodes on one machine were run through full blocks under rule 2 and through both sweep dates under rule 3.

All of that was done by the same people who wrote the code. None of it is a reason to believe the rules are right.
