# The CVN pool: design

How the mining pool built into the CVN program works, and why it works that way. For someone who wants to check it, review it or run one. To mine at a pool, read section 5 of [MINING_GUIDE.md](MINING_GUIDE.md). To run one, read [POOL_GUIDE.md](POOL_GUIDE.md).

**The aim.** From the day a CVN network starts, anyone can mine through a pool, and anyone can run one. A machine that mines alone may wait days for a block; at a pool it is paid a little, often. The project runs a pool that takes no fee, and the same program lets anyone else run their own.

**A pool's block pays the miners itself.** The reward of a block a pool finds is paid, in that block, to the addresses that did the work. Those coins go from the network straight to the miners. The pool never holds them, so nobody who runs the pool, or breaks into it, can keep them. Only parts too small for a block, or more than a block has room for, pass through the pool's address.

**It rests on one rule of the chain:** a block's reward may pay up to 50 addresses, each once (section 3). That rule was changed for this, before any public network started, because it is the one thing about pools that cannot be added afterwards. Everything else here is what one node does for the miners connected to it. A node started without `--pool` does what it always did, and a miner working alone still pays one address, its own.

---

## 1. The parts

| Part | What it is | File |
|---|---|---|
| The pool | A node started with `--pool CVN_ADDRESS`. It takes miners in, hands out work and checks answers | `node/pool.go`, `node/stratum.go` |
| Paying in the block | Each piece of work carries a reward divided among the miners who did the recent work. A block found on it pays them itself | `node/pool_inblock.go` |
| The record | One file beside the chain, `pool_NETWORK.db`: every share, every block the pool found and how its reward was divided, what each address was paid by the blocks and is owed by the pool, every payment, and the coins the pool may spend | `node/pool_ledger.go`, `node/pool_payments.go` |
| Paying what is owed | For the parts a block could not pay itself. The pool pays by itself every five hours. A timer on the pool's server starts a separate program, `--pool-pay`, that signs the payments the pool's node has prepared. The node never holds the key | `node/pool_pay.go` |
| The chain reader | The pool reads every block for transfers sent from its own address, so that its record cannot fall behind what has really been paid | `node/pool_chain.go` |
| Copies | The pool copies its record every hour and after every payment, and checks each copy | `node/pool_backup.go` |
| The page | `/pool` for people and `/pool.json` for programs: the whole record, in public | `node/pool_page.go` |
| The miner's side | `--pool-mine HOST` starts the fast miner pointed at a pool. No node, no chain | `node/pool_mine.go` |

## 2. Shares

**Easy work.** A miner logs in with its own CVN address. The pool gives it work at a difficulty of its own, far below the network's, chosen so that its machine finds an answer about every 30 seconds. Each answer that meets that difficulty is a share. A share that also meets the network's difficulty is a block.

**The difficulty follows the machine.** It starts at 50,000 and moves as answers arrive, by at most four times up or down at a step, never below 2,500 and never above the network's own. A fast machine and a slow one both hand in about two shares a minute. A share counts for the difficulty it was found at, so each is credited for the work it did.

**Different work for every miner.** Each piece of work carries its own reward transaction, so no two miners are ever given the same block to search.

**What is refused:** the same answer twice; an answer to work on a block that is no longer the newest; an answer that does not meet its difficulty (thirty of those and the connection is closed). One internet address may hold 32 connections and one pool 2,000 miners. A pool that has fallen behind the network takes no shares until it has caught up.

**What checking costs.** Each share costs the pool one RandomX hash in the low-memory mode. The lowest difficulty the pool hands out is high enough that making a share costs a miner thousands of times what checking it costs the pool.

## 3. Dividing a block's reward, and paying it in the block

**The rule of the chain.** A block's reward may pay as many as 50 addresses, each of them once, and together no more than the block may pay. Each part must be a coin in its own right, at least 0.0001 CVN.

**The division.** A block's reward is divided among the most recent shares, counted back until their work adds up to **twice the work one block takes**. Each address gets the part of the reward that matches its part of that work. The oldest share counted is cut to fit, so the window is exact.

This is the usual "pay per last N shares". Someone who mines steadily has a part in every block found while their shares are in the window. Someone who joins for five minutes when they think a block is near gains nothing by it.

**When it is fixed.** A block's reward is part of the block, so the division has to be written into the work before anybody searches it. Each piece of work the pool hands out carries the division as it stood at that moment, and every miner is sent fresh work when the chain moves and at least every fifteen seconds. The share that finds a block was handed in after that block's work was made, so it counts towards the blocks that follow, like every share after it. Before any share has been handed in at all, a miner's work pays that miner.

**Which parts the block pays.** A reward has room for 50 addresses.

- A part of **0.01 CVN or more** is paid in the block, to its address. If more than 50 addresses have such a part, the 49 with the most work are paid and the fiftieth output is the pool's own.
- A part under 0.01 CVN is not paid in the block. A miner paid a hundredth of a coin in every block would hold hundreds of coins too small to be worth spending.
- Everything the block does not pay to a miner goes to the pool's address in one output, and each such part is owed to its address in the pool's record (section 5).

With 50 miners or fewer, each doing at least a five-thousandth of the pool's work, the pool's address receives nothing at all.

**Exact amounts.** Everything is whole units, as everywhere in CVN. Parts are rounded down, and the few units left over go to the address with the most work in the window (the lowest address, if two are equal), so the parts always add up to exactly the reward. If what is left for the pool would itself be too small to be a coin, it goes to the largest part.

**No fee.** The pool keeps nothing. There is no setting for a fee.

**What a miner cannot check from its own machine.** The fast miner searches the work it is given without reading it, so a pool could hand out work that pays other addresses. It would show at once and for good: every block's reward is on the chain, and the pool page shows the division beside it. This is a far smaller thing than holding the coins, but it is not nothing.

## 4. When a block counts

The coins a block pays are on the chain as soon as the block is. A miner can see them at once. They are sure once the block is too deep to be replaced.

In the pool's record a block is **waiting** until 100 more blocks stand on it, which is the number of blocks a node will never replace. Then the pool looks at its own chain:

- if the block is still there, it **counts**: each address is credited with its part, as paid by the block or as owed by the pool, and what the block paid the pool's address becomes a coin the pool may spend;
- if another block stands at that height, it is **lost**: its payments went with it, and nobody is owed anything for it.

The block's new state, what it adds to each address and the new coin are written in one step. A program stopped half-way can neither credit a block twice nor count one without crediting it.

Each address has five figures, and the first is always the other four together: **earned**, **paid in blocks**, **owed**, **on its way** (in a payment that has been sent and is not yet settled) and **paid by the pool**.

## 5. Paying what the blocks could not

This section is about the parts a block did not pay to a miner itself: those under 0.01 CVN, and those of miners beyond the fifty a block has room for. They are the only coins a pool holds for anybody. For a pool with few miners there may be none.

**A pool pays them by itself, every five hours.** Nobody runs a command to pay. A timer on the pool's server starts a payment run five hours after the last one, and each run pays every address owed 1 CVN or more. The operator does one thing by hand, once: saves the pool's key on the server, locked with a password that only the administrator can read. [POOL_GUIDE.md](POOL_GUIDE.md) has the steps, what keeping the key there costs in safety, and how an operator who will not keep it there pays by hand instead.

**The paying is done by two programs, and only one of them has the key.** The pool's node, which listens to miners, knows what each address is owed and which of the pool's coins are free. It prepares the payments and holds no key. The timer starts a second, short-lived program on the same machine that has the key and listens to nobody:

```
cvn_node --network NAME --pool-pay --password-file FILE
```

In each run:

1. The paying program asks the pool's node to prepare the payments: every address owed 1 CVN or more, the most owed first, in transfers of up to 49 addresses, with the coins that pay for each. Those amounts are no longer "owed" but "on their way", and the coins are set aside.
2. The paying program checks that each payment hangs together (the figures add up, the fee is the small one, the change comes back to the pool), signs it, and hands it back.
3. The node checks that the signed transfer is exactly the payment it prepared and is signed by the pool's address, writes it into the record as sent, and only then hands it to the network.
4. When the transfer is in a block with 100 more on it, the payment is settled.

The two talk over a port that answers on the server's own address only (the work port plus one). A request passed on by a web server in front of the node is refused, and so is anything a web page could make a browser send.

**Which coins the pool spends.** Only what blocks that count paid to the pool's address, and the change of payments that have settled. Both are too deep to be replaced, so a payment cannot be undone by the chain changing. Anything else that arrives at the pool's address is left alone.

**The fee** is 0.001 CVN for each transfer, shared equally among the addresses it pays. Units that do not divide fall one each on the first addresses.

**A payment that has been sent is never made a second time.** Its transfer is kept in the record and handed to the network again every five minutes until it is mined. Only a payment that was prepared and never signed is undone, after ten minutes or at once if the command gives it back. (Paying again after a wait would not be safe: the first transfer stays valid for as long as its coins are unspent, and could be mined after the second.)

**Why two programs and not one.** The node that listens to miners is the part of a pool that strangers can reach. If it held the key, a fault in it would be the loss of every coin at the pool's address. As built, the node runs as a user that can read neither the saved key nor its password, and the key is open only for the seconds a run takes.

**Why five hours.** A shorter interval pays miners sooner and leaves less at the pool's address between runs. A longer one makes fewer transfers, so less goes in fees. Five hours is the figure the installer sets; an operator may name another.

## 6. A record that is behind the chain

Every payment is a transfer from the pool's address, and the chain holds them all. The record should know each of them. It may not, in two ways: the record was put back from an older copy, or somebody with the pool's key sent a transfer by hand. A record that does not know of a payment still shows its amounts as owed, and would pay them again.

So the pool reads every block for transfers sent from its own address.

- **One the record does not know stops all paying at once.** A payment run also has the chain read to its newest block before anything is prepared, so there is no moment after a restart in which an old record can pay.
- **When that transfer is 100 blocks deep it is taken into the record** as a payment that has settled. What each address received, with its share of the fee, comes off what the record says it is owed. The coins it spent leave the record's list, and what it sent back to the pool's address joins it.
- **Blocks that are deep enough to count are always counted first.** Otherwise a block that payment had paid for would be credited afterwards and owed a second time.
- **An address paid more than the record says it was owed is owed nothing, never less than nothing,** and the difference is written down with the payment. That is what an older copy looks like: the difference was earned from blocks found after the copy was made.
- **A payment of the pool's own that such a transfer overtakes is undone:** one prepared and not signed, always; one signed and sent, only if the transfer spent one of its coins, since it can then never be mined. This is the one case in which a sent payment gives its amounts back, and it needs the pool's key to have been used outside the pool.

**The rule this gives.** After an older copy is put back, nobody is paid twice and nobody is asked to give anything back. What cannot be recovered is who earned the blocks found after the copy was made: that division was in the lost record and nowhere else. Their rewards stay at the pool's address, outside what the record counts, for the operator to settle by hand.

## 7. Copies of the record

The record is the only place that says what each address is owed, and it cannot be downloaded again as a chain can. Every hour, and straight after payments have been sent, the pool takes a snapshot of it through the database, checks it, and keeps it beside the record. The newest 48 are kept.

**The check.** Everything in the file can be read. The parts of every block that counts add up to its reward. What each address has earned is its parts of those blocks, and is what it is owed, has on its way and has been paid together. What is on its way and what has been paid are what the payments say. Every coin set aside is set aside for a payment under way, which has exactly the coins it spends. The coins the record holds are what it owes and has on its way.

`--check-pool-backup FILE` runs that check on any copy. `--restore-pool-backup FILE` puts a checked copy in place of the record, with the pool stopped, and keeps the file it replaces.

## 8. The page

`/pool` and `/pool.json`, on the port every node answers questions on: the pool's speed and miners, every block it found and what became of it, how each block's reward was divided address by address, what each address is owed, has on its way and has been paid, and every payment with its transfer.

Addresses and amounts are public on this chain in any case. Showing the whole record means a miner need not take the pool's word for anything it can add up.

## 9. For a miner

```
LaunchMiner.bat --pool-mine POOL_NAME        (Windows)
./cvn_node --pool-mine POOL_NAME             (Linux)
```

starts the fast miner (the CVN edition of XMRig) pointed at the pool, with the machine's own mining address as its name there. No node is started and no chain kept. The program reads the pool's page first and refuses a pool on another network. Once a minute it prints the machine's speed, its shares, and what the pool's page says the address is owed, has on its way and has been paid. The miner built into the CVN program cannot mine at a pool.

## 10. What can go wrong, and what is done about it

| Risk | What is done |
|---|---|
| The operator runs off with the miners' coins, or the pool's key is stolen | A block pays the miners itself: those coins never pass through the pool. What the pool's address holds is only the parts too small for a block, or beyond the fifty a block has room for. The node that listens to miners never has the key and runs as a user that cannot read it |
| The pool hands out work that pays the wrong addresses | A miner's machine does not read the work it searches. It shows at once on the chain and on the pool page, block by block, and cannot be undone or hidden. It cannot be prevented from inside the pool |
| The operator does not pay the small parts it holds | The record is public and every payment is a transfer anyone can see on the chain. It cannot be prevented, only seen |
| The pool's node is taken over | It could ask the paying command to sign a wrong payment, up to what the address holds. It could not learn the key |
| The record is lost | What the blocks paid is on the chain and is not touched. Copies every hour and after every payment hold what is owed. A copy put back cannot pay twice (section 6). For blocks found since the copy, who is owed the small parts is lost |
| A miner floods the pool with useless answers | Each costs the pool one hash and the miner far more; thirty in a row closes the connection; connections from one address are limited |
| A miner hands in shares but keeps back a block | No pool can detect it. It costs the attacker their own part of that block |
| The pool becomes most of the network | The program is public so that others run pools; mining alone stays fully supported; both guides say so |
| A miner's connection is tampered with on the way | Not encrypted in this version. Work is not secret, but someone in the path could put their own address in place of the miner's |

## 11. Considered

**Paying miners inside the block** was at first set aside, because it needs a rule of the chain changed, and then done, for two reasons. A pool that receives the reward and pays it on holds other people's coins, and nothing in a program can stop its operator keeping them. And the rule is the one thing here that cannot be added after a network has started. It costs each miner at a pool many small coins, which have to be combined before they can be spent in one transfer; the floor of 0.01 CVN is there to keep them from being too small.

**Paying again when a payment is slow.** Unsafe, as section 5 says.

**A fee.** None, and no setting for one. A pool that wants one has to change the program and should say so.

**A higher limit than fifty.** A reward of fifty outputs adds about 4,000 bytes to a block of 300,000. More would let a large pool pay more of its miners in the block, at the cost of every pool block being larger. Fifty is the limit an ordinary transfer has.

## 12. Not in this version

- Encrypted connections for miners.
- Names and figures for each of one person's machines.
- More than one pool server sharing one record.
- The built-in miner mining at a pool.
- Sending copies of the record off the server by itself.
- Working out who is owed the small parts of blocks found after an older copy of the record was made.
- A check, on the miner's own machine, that the work it is given pays its address.
- A pool with no operator at all, where miners keep the record between them.

## 13. How it was tested

- **The rule:** a reward of fifty outputs accepted and stored, with every address holding its part; fifty-one, one address twice, a unit too much and a part too small to be a coin all refused; coins from a reward in parts spent like any others, and a checked copy of the chain adding up to what was issued.
- **Paying in the block:** a division turned into a reward's outputs for three, fifty and sixty miners, for miners too small, for a remainder too small to be a coin, and before any share; a pool whose block pays its two miners, with nothing at the pool's address and nothing to pay; a pool with one miner too small for the block, paid by transfer from what the block paid the pool.
- **Inside the test program:** shares, duplicates and answers without the work; a block divided to the unit; the difficulty following a machine; blocks coming to count and being lost; a payment prepared, signed, sent, mined and settled, with every forged or altered transfer refused; sixty addresses paid in transfers of 49 and 11; the record checked at every stage of a payment and seven kinds of damaged copy refused; an older copy put back without anybody being paid twice, checked on the chain; a transfer sent by hand; the miner's side with a stand-in fast miner.
- **With real nodes on one machine:** a pool among two seeds whose blocks pay its three miners themselves: every seed holds for each miner exactly what the record says the blocks paid it, and nothing at the pool's address. Then, with a pool set to pay nothing in the block so that all of it is owed: a pool among two seeds with three miners of different speeds, paid by the real paying command, every unit accounted for in the record and on every node's chain; a pool that mined cut off from the network and lost its blocks; a pool whose record was replaced, with the real command, by a copy made before it paid anybody.
- **Not yet done:** a run on a rented server with real miners over days, and with it how many miners one server carries. The installer `deploy/install_pool.sh` has had no run on a real server. The real fast miner has been run against a pool for half a minute only.
