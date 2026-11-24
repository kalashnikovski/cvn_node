# Covenant Standard (CVN)

## Design paper

November 2026. This paper describes the rules Covenant Standard is intended to launch with.

**Status.** These rules are built and run on a development network. They have been reviewed in several rounds by AI models, whose findings and what was done about each are recorded in the repository's `audit` folder. They have not been audited by an independent firm or person, and have not yet run as a public network. The first test network (`testnet-1`) used an earlier, simpler rule set; it has been retired and this software no longer follows it. Nothing here is an offer or a promise of value. [ROADMAP.md](ROADMAP.md) records what is finished and what is not.

---

## 1. What it is

Covenant Standard is a proof-of-work chain for a single thing: a coin that ordinary people can mine on ordinary computers, with a fixed supply and no one given a head start.

It does not have smart contracts, staking, a treasury, a foundation allocation or an on-chain vote. Each of those is a place where a small group ends up with a lasting advantage, and leaving them out is a design decision, not an omission.

## 2. Principles

1. **No pre-mine.** The first block pays nothing. No coins are set aside for founders, developers or investors.
2. **A hard cap.** 486,727,500 CVN. There is no tail emission.
3. **No charge for holding.** Coins their owner still controls are never taxed or decayed.
4. **Mining on home processors.** The mining algorithm favours general-purpose processors and large memory.
5. **Lost coins return.** Coins that nobody has moved for seven years are slowly returned to the people securing the network.
6. **One rule for everyone.** Every rule is enforced by every node, in the same way, with no privileged keys.

## 3. Units and time

- The smallest unit is 0.00000001 CVN; 1 CVN is 100,000,000 units.
- Every amount in the rules is a whole number of units. No rule uses decimal arithmetic.
- The target is one block every 45 seconds: 1,920 a day, 700,800 a year.
- Rules are stated in block heights, never dates. "Seven years" means 4,905,600 blocks.

## 4. Supply

The block reward halves every 4,905,600 blocks and stops after the seventh period.

| Period | Years | Blocks | Reward per block (CVN) | Issued in period (CVN) |
|---|---|---|---|---|
| 1 | 0–7 | 1 – 4,905,600 | 50 | 245,280,000 |
| 2 | 7–14 | 4,905,601 – 9,811,200 | 25 | 122,640,000 |
| 3 | 14–21 | 9,811,201 – 14,716,800 | 12.5 | 61,320,000 |
| 4 | 21–28 | 14,716,801 – 19,622,400 | 6.25 | 30,660,000 |
| 5 | 28–35 | 19,622,401 – 24,528,000 | 3.125 | 15,330,000 |
| 6 | 35–42 | 24,528,001 – 29,433,600 | 1.5625 | 7,665,000 |
| 7 | 42–49 | 29,433,601 – 34,339,200 | 0.78125 | 3,832,500 |
| — | 49 on | 34,339,201 onward | 0 | 0 |

Total: **486,727,500 CVN**. Every reward is a whole number of units, so the total is exact.

A block may pay its miner less than it is allowed to. Whatever it does not pay is not destroyed: it goes to the vault (section 6). Nothing issued ever leaves the supply.

## 5. The Sabbatical sweep

Every chain loses coins: keys are lost, owners die, small amounts are forgotten. On most chains those coins are gone for good, and the people securing the network are paid less and less as issuance falls.

Covenant Standard returns part of them.

- Every coin records the block at which it was created.
- A coin that has not been spent for 4,905,600 blocks (seven years) has **one fifth** of its value moved to the vault. It keeps the rest and starts a new seven-year count.
- A coin so small that what remained would be below the minimum coin size is left alone: nothing is taken from it. The rule takes a fifth from a coin or nothing; it never takes a coin whole.
- This repeats every seven years for as long as the coin stays unspent.

An owner who still holds their key loses nothing. Spending a coin, including sending it to your own address, creates a new coin with a new count. One transfer every seven years is enough. The wallet shows when a coin's date is approaching, and its "Renew my coins" action makes that transfer in one step for every coin within a year of its date.

A coin that is truly lost keeps 80% after 7 years, 64% after 14, about a third after 35 and about a tenth after 70.

The sweep is a rule every node applies at the same block. No one can skip it and no one decides whom it applies to.

## 6. The vault

The vault is a number kept by the rules. It has no address and no key. Nobody can spend from it.

**What goes in:** swept coins, a share of transaction fees (section 7), and anything a block could have paid its miner but did not.

**What comes out:** with every block, the vault pays out its balance divided by 9,811,200 (fourteen years of blocks), rounded down to whole units. That amount is added to the block's reward.

The divisor only sets the pace. Over time the vault pays out everything that enters it. With this value, half of any amount entering the vault has reached miners within about ten years.

Within a block, the payout is taken from the vault as it stood after the previous block; that block's own fees and sweeps are added afterwards.

After year 49 the block reward is zero, and miners are paid by fees and the vault.

## 7. Fees

- A fee is whatever a transfer's inputs exceed its outputs by. It is chosen by the sender. **Zero is allowed.**
- There is no percentage fee. The chain cannot tell a payment from a person moving their own coins, so a percentage would tax people for rearranging what they own.
- Until block 34,339,200, **80% of a block's fees go to its miner and 20% to the vault**. After that the miner receives all of them.
- No fee is ever destroyed.

The vault's share is deliberately small. A miner can avoid it by including their own transfers at no fee, and a sender can pay a miner privately. At a fifth, neither is worth the trouble.

## 8. Transfers and blocks

**Ownership.** A coin belongs to an address. An address is `CVN_`, then the first 20 bytes of the SHA-256 hash of a public key (the key as it is written in a transfer: 128 lower-case hex characters), then a 4-byte check on those 20, all in hex: 52 characters in all. The check means a mistyped address is not an address at all, and the rules refuse to make a coin out to anything that is not one. So a slip of the hand cannot send coins where nobody can spend them; about one mistake in four billion would still pass. A transfer is signed with the matching private key (ECDSA on the P-256 curve).

**A transfer** spends up to 200 coins and creates up to 50. It carries nothing its sender did not sign, and what is signed includes the name of the network, so a transfer made for one CVN network is invalid on any other.

**Minimum coin size.** No coin smaller than 0.0001 CVN can be created, by a transfer or as a mining reward. This keeps the set of unspent coins from filling with dust.

**A block** holds at most 500 transactions, one of which is its miner's reward, so at most 499 transfers; and at most 300,000 bytes.

**A block's reward** may be paid to as many as 50 addresses, each of them once, and together no more than the block may pay. A miner working alone pays one address, its own. A pool pays the miners who did the work in the block itself, and so never has to hold their coins.

**One way to write everything.** A block's identifier is the SHA-256 hash of its header fields, each written with its length in front, so no two different blocks can share an identifier. Every header field has exactly one permitted form. The identifier covers the full contents of every transaction in the block.

**Time.** A block must be dated later than its parent, and no more than two minutes ahead of the clock of the node checking it. Because dates always move forward along a chain, every block is also later than the median of those before it, the rule older chains state separately.

## 9. Mining

### 9.1 RandomX

Blocks are mined with RandomX, the algorithm Monero has used since 2019. It runs randomly generated programs against a 2 GB table in memory, which is work general-purpose processors are good at and graphics cards and custom chips are not.

Covenant Standard uses RandomX with two of its settings changed, so that software and hardware prepared for another coin do not produce valid CVN work:

| Setting | Standard | CVN |
|---|---|---|
| Identifying text | `RandomX\x03` | `RandomCVN\x01` |
| Extra table size | 33,554,368 bytes | 33,424,704 bytes |

Everything that makes RandomX resistant to specialised hardware, and everything its security reviews covered, is unchanged: the memory size, the program size and the instruction mix.

The engine is kept inside the project's repository. It is a published Go implementation of RandomX with those two settings changed and with two additions that do not affect what it computes: it can place its memory in large pages on Windows as well as Linux, and it does the same for each mining thread's working memory. Built with the standard settings it reproduces RandomX's published test values, and the project's automatic tests check that on every change.

**The key.** RandomX is seeded by a key that changes once a day. For a block at height *h*, the key is the identifier of the block at height ⌊(h − 101) ÷ 1,920⌋ × 1,920. That is the genesis block for every block up to height 2,020, the first day or so; the key first changes at block 2,021. The key block is always more than 100 blocks deep, beyond the reach of any reorganisation (section 10).

**The proof.** What RandomX hashes is a 43-byte message made from the block: one byte with the value 1, six zero bytes, the block's identifier as it would be with the nonce set to zero (32 bytes), and the nonce as four bytes, lowest first. A block is valid when the RandomX hash of that message, read as a number with its last byte the most significant, is at most (2²⁵⁶ − 1) ÷ difficulty. This is the layout that widely used RandomX mining software expects.

### 9.2 Replacing the algorithm

The mining algorithm is chosen by block height from a schedule. If RandomX is ever broken, or hardware arrives that defeats its purpose, a different algorithm can take over from an announced height without restarting the chain. This has been exercised: a test chain changes algorithm part-way and continues.

### 9.3 Difficulty

Difficulty is adjusted on every block.

- Each block's difficulty moves according to how long the previous block took compared with 45 seconds: up by at most 5% for a fast block, and down by more for a slow one, to at most about 39% for a block that took eight minutes or longer.
- The move is applied as a multiplication by e^x, where x is the step (for example +0.049 for the fastest block, -0.5 for the slowest). Applied that way, any run of blocks moves difficulty by an amount that depends only on how much time their dates span, not on how that time is shared out between them. An earlier form applied the step as 1 + x, and a miner holding most of the mining power could then gain by choosing dates: a run of blocks dated one second apart, then one dated eight minutes later, repeated, left difficulty level while blocks really came every 40 seconds, about 13% more coins. With e^x the same pattern gains nothing. The arithmetic is done in whole numbers, so every node gets the same result to the last digit.
- It also leans against how far the whole chain is ahead of or behind its schedule since the first block, by at most 1% per block. Without this term blocks arrive a few percent fast and the seven-year periods would end early; with it, the long-run average is 45 seconds. In a simulation of 300,000 blocks, including large swings in mining power, the average was 45.00 seconds.
- If no block arrives for six minutes, the difficulty of the block being mined halves, and halves again every three minutes after that. A chain that loses most of its miners overnight keeps moving. "Six minutes" is read from the block's own date, and a block may be dated up to two minutes ahead of the clock, so the halving can be reached a little under two minutes early. The program's miner does exactly that, so that every miner reaches it at the same moment and none has a head start; it dates a block ahead for no other reason.
- That relief belongs to the late block alone. The block after it is worked out from the difficulty the late block would have had without relief, lowered in the ordinary way for the late block having been slow (by at most half). If the miners really have gone, difficulty comes down by half per block, with relief again for each block that is late: after mining power falls to a tenth, the next 30 blocks take about 52 minutes in simulation, against 22.5 on schedule. If a miner only paused, the difficulty is still close to where it was when they return. Without this, one long gap left dozens of nearly free blocks for whoever held the most mining power: in simulation, a miner with 94% of it who mined in bursts on purpose took about a third more coins than by mining steadily, and blocks came every 32 seconds. With it, burst mining pays that miner nothing extra and the chain holds 45 seconds.
- The first blocks are treated differently, so that the start cannot be gamed. For the first 20 blocks the difficulty never goes below its starting value and there is no relief for a late block. Over the next 40 blocks that relief returns one halving at a time. The chain's schedule is counted from block 20. And for this rule a block dated before the launch itself (the Bitcoin anchor block's own time, section 11) counts as dated at the launch, so no time is taken to have passed between such blocks and difficulty can only rise. Without these, whoever mined the first blocks in a row could date them to make the following ones nearly free. A miner with most of the network's power keeps the latitude any such miner has on any chain: dates within two minutes of the true time.

### 9.4 Mining software

Two miners are published, both open source:

- a miner built into the node, which needs nothing else installed;
- a CVN edition of XMRig, the most widely used RandomX miner, which is somewhat faster. It differs from XMRig only in the two settings above and in having no built-in donation.

Publishing the faster one openly is a fairness measure. Without it, the few people able to adapt XMRig themselves would out-mine everyone else.

A machine needs about 2.3 GB of free memory to mine at full speed. With less, the miner runs in a low-memory mode that is many times slower. A small machine makes a good node but a poor miner.

## 10. Which chain is the real one

- Nodes follow the chain with the **most total work**, not the longest one. Each block's work is its difficulty.
- Between two blocks at the same height on the same parent, the one with more work wins; if equal, the one with the lower identifier.
- A node **will not replace more than 100 of its own blocks** (about 75 minutes), however much work a competing chain shows.

The last rule trades one risk for another. It stops a long secret chain from rewriting settled history, which matters most while the network is small. In return, a node that was cut off for longer than that and ended up on a dead branch has to be re-synced by its operator.

## 11. The first block

A fair launch has to be able to show that nobody mined before everybody could.

The first block of a launch network is tied to one particular Bitcoin block, named by its height when the launch is announced and not yet mined at that time. Nobody can know a future Bitcoin block, so nobody can build the first block, or anything on top of it, early.

The Bitcoin block gives the first block two things:

- **Its identity.** The Bitcoin block's hash is written into the first block, so it decides the first block's identifier. The first RandomX key is that identifier, so nobody can even prepare to mine early.
- **Its date.** The first block is dated two hours before the Bitcoin block's own timestamp. Bitcoin lets a block's time run up to two hours ahead of the true time, and no node accepts a block dated more than two minutes ahead of its own clock; a first block dated in the future would leave every honest miner waiting while an altered miner worked ahead. Two hours back, it can never be in the future. The two hours of earlier dates this opens up buy nothing: the difficulty rule counts any date before the Bitcoin block's own time as that time (section 9.3). The chain's clock therefore starts at the launch, not at a date written into the software weeks before. Without this, the first miner could date the first blocks far in the past; the difficulty rule would see days passing between blocks and fall to its minimum, handing that miner a long run of easy blocks.

Both come from the Bitcoin block's 80-byte header, which is what a miner supplies at the start. The header proves that it is a real Bitcoin block: its hash has to meet the difficulty it states, and that difficulty has to be at least a quarter of Bitcoin's at the time the software was written. Nobody can produce such a header without a large part of Bitcoin's own mining power, and no smaller chain that shares Bitcoin's header format comes near it. A header names its block by hash, not by height, so by itself it cannot prove which height it stands at. A node therefore counts. The software carries one Bitcoin header that was already deeply buried when it was written. From that block the node collects every Bitcoin header up to the announced height and checks each one as a Bitcoin node does: it names the header before it, its hash meets the target it states, that target is the one Bitcoin's rules give at that height, and its date is in order. The header the node arrives at is the block at that height, and nobody's word is taken for it. The headers come from other computers on the Bitcoin network, or from public block explorers when none of those can be reached; a wrong header fails the checks, and a header held back only makes the node wait. A network also fixes the earliest date the block may carry, which rules out any block mined before the announcement.

The software is published with the Bitcoin height fixed and the header still unknown. A node started before that block exists waits for it. When the block appears, every waiting node, including those of the people who wrote the software, takes its header from the public Bitcoin chain and starts within seconds of the others. A node finds the header by following Bitcoin's headers to that height, as described above. An operator who prefers that the program contact nobody can supply the header by hand, from their own Bitcoin node or anywhere else, and gains little by it: getting the header a few seconds early is worth a few seconds of mining.

About one Bitcoin block in several hundred is replaced moments after it appears. If that happened to the anchor block, nodes could start from two different headers and form two networks. The rule for that case is fixed in advance and is nobody's to decide: **the network is the one that begins from the Bitcoin block still in Bitcoin's chain six blocks later, and blocks mined on the other are void.** Until the anchor is six blocks deep every node keeps following Bitcoin's headers to see whether its block is still the one at that height, and every node compares its first block with the seeds'. A node that finds itself on the wrong side says so plainly, and its operator starts again from the right block.

Anyone can check the anchor against Bitcoin afterwards: `--show-genesis` prints it.

## 12. The network

A node keeps the whole chain, checks every block and transfer itself, and relays what it accepts. Nodes find each other through a short list of seed addresses and by exchanging the addresses of nodes they know. A seed is a convenience and has no authority: it is asked first because it is likely to be online, every block it sends is checked like any other, and a node follows whichever chain proves the most work, whoever it came from. Nothing a new node has taken on the seeds' word is protected by the limit on replacing one's own blocks (section 10): until a node that is not a seed has shown that it holds the same chain, a newcomer follows whichever chain proves the most work, however far back the two part, across restarts as well. So the seeds alone cannot hold newcomers on a chain of their own. An operator can name other nodes to start from with `--connect`. The seeds are at present all run by the project's maintainer; seeds run by others will be added as they are offered. A node behind a home router needs no configuration: it opens its own ports where the router allows, and otherwise receives blocks over connections it makes outward.

A node passes a block on over the connections it has open, once, and to a few other nodes besides. A block near the size limit is first announced by its height and identifier and handed over only to the nodes that ask for it; a block of ordinary size is simply sent. How a block reaches a node is not a rule of the chain: a block is judged the same however it arrived.

A node never signs anything on a user's behalf. Wallets build and sign transfers on the user's own computer and hand the finished transfer to a node.

Which transfers a miner puts in a block is the miner's choice and is not a network rule.

## 13. What this design does not do, and its known limits

- **A small proof-of-work network can be attacked cheaply.** Anyone with more mining power than everyone else combined can rewrite recent blocks. Total-work selection and the reorganisation limit reduce the damage; they do not remove the risk. It shrinks as honest mining grows.
- **RandomX favours processors today.** That is an observation about current hardware, not a guarantee. Section 9.2 is the remedy if it changes.
- **Capacity is modest.** At most 499 transfers every 45 seconds. Block relay between nodes is simple and has not been tested with thousands of nodes.
- **A node stores every block.** There is no pruning yet.
- **No privacy features.** Every transfer and balance is public.
- **A lost key is a lost wallet.** There is no recovery. The sweep returns only a part of lost coins, slowly, and to miners, not to the person who lost them.
- **Not audited.** The rules and the code have been tested by their authors and by a small test network. Independent review is planned before any launch.

## 14. Changing the rules

A rule that decides whether a block is valid can only change at a block height announced in advance, in software published before that height, with the change described in plain terms. Nodes that do not adopt it stay on the old rules. There is no key, vote or authority that can change the rules for anyone else.

## 15. Considered and rejected

| Idea | Why it was dropped |
|---|---|
| A yearly charge on all balances | It charges people who have done nothing wrong for holding what they earned |
| Tail emission | It breaks the hard cap |
| Locking the vault for 50 years | Coins would sit unused for decades; continuous payout was chosen |
| A fee as a percentage of the amount | Moving your own coins cannot be told apart from paying someone |
| Sending most fees to the vault | Too easy, and too tempting, for miners and senders to bypass |
| A dividend to every person | Nothing stops one person claiming under many identities |
| A larger sweep (25–33%) | The same income in the long run, but harsher on people who miss the date |
| A share of each reward for the miner of a losing block | Nodes could not verify the losing block without much more machinery, so the share was voluntary in practice |
