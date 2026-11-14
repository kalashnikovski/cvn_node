# Covenant Standard (CVN)

## Design paper

November 2026. This paper describes the rules Covenant Standard is intended to launch with.

**Status.** These rules are built and run on a development network. They have not been independently audited and have not yet run as a public network. The public test network running today (`testnet-1`) uses an earlier, simpler rule set and will be reset. Nothing here is an offer or a promise of value. [ROADMAP.md](ROADMAP.md) records what is finished and what is not.

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
- If what would remain is below the minimum coin size, the whole coin is moved.
- This repeats every seven years for as long as the coin stays unspent.

An owner who still holds their key loses nothing. Spending a coin, including sending it to your own address, creates a new coin with a new count. One transfer every seven years is enough, and the wallet shows when a coin's date is approaching.

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

**Ownership.** A coin belongs to an address. An address is `CVN_` followed by the first 20 bytes of the SHA-256 hash of a public key. A transfer is signed with the matching private key (ECDSA on the P-256 curve).

**A transfer** spends up to 200 coins and creates up to 50. It carries nothing its sender did not sign, and what is signed includes the name of the network, so a transfer made for one CVN network is invalid on any other.

**Minimum coin size.** No coin smaller than 0.0001 CVN can be created, by a transfer or as a mining reward. This keeps the set of unspent coins from filling with dust.

**A block** holds at most 500 transfers and at most 300,000 bytes.

**One way to write everything.** A block's identifier is the SHA-256 hash of its header fields, each written with its length in front, so no two different blocks can share an identifier. Every header field has exactly one permitted form. The identifier covers the full contents of every transaction in the block.

**Time.** A block must be dated later than its parent and later than the median of the last eleven blocks, and no more than two minutes ahead of the clock of the node checking it.

## 9. Mining

### 9.1 RandomX

Blocks are mined with RandomX, the algorithm Monero has used since 2019. It runs randomly generated programs against a 2 GB table in memory, which is work general-purpose processors are good at and graphics cards and custom chips are not.

Covenant Standard uses RandomX with two of its settings changed, so that software and hardware prepared for another coin do not produce valid CVN work:

| Setting | Standard | CVN |
|---|---|---|
| Identifying text | `RandomX\x03` | `RandomCVN\x01` |
| Extra table size | 33,554,368 bytes | 33,424,704 bytes |

Everything that makes RandomX resistant to specialised hardware, and everything its security reviews covered, is unchanged: the memory size, the program size and the instruction mix.

The engine is kept inside the project's repository and, built with the standard settings, reproduces RandomX's published test values.

**The key.** RandomX is seeded by a key that changes once a day. For a block at height *h*, the key is the identifier of the block at height ⌊(h − 101) ÷ 1,920⌋ × 1,920; for the first 101 blocks it is the genesis block. The key block is always more than 100 blocks deep, beyond the reach of any reorganisation (section 10).

**The proof.** A block is valid when the RandomX hash of its identifier, read as a number, is at most (2²⁵⁶ − 1) ÷ difficulty.

### 9.2 Replacing the algorithm

The mining algorithm is chosen by block height from a schedule. If RandomX is ever broken, or hardware arrives that defeats its purpose, a different algorithm can take over from an announced height without restarting the chain. This has been exercised: a test chain changes algorithm part-way and continues.

### 9.3 Difficulty

Difficulty is adjusted on every block.

- Each block's difficulty moves up or down a little according to how long the previous block took compared with 45 seconds.
- It also leans against how far the whole chain is ahead of or behind its schedule since the first block, by at most 1% per block. Without this term blocks arrive a few percent fast and the seven-year periods would end early; with it, the long-run average is 45 seconds. In a simulation of 300,000 blocks, including large swings in mining power, the average was 45.00 seconds.
- If no block arrives for six minutes, difficulty halves, and halves again every three minutes after that. A chain that loses most of its miners overnight recovers within minutes, not days.

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
- **Its date.** The first block is dated with the Bitcoin block's own timestamp. The chain's clock therefore starts at the launch, not at a date written into the software weeks before. Without this, the first miner could date the first blocks far in the past; the difficulty rule would see days passing between blocks and fall to its minimum, handing that miner a long run of easy blocks.

Both come from the Bitcoin block's 80-byte header, which is what a miner supplies at the start. The header proves itself: its hash has to meet the difficulty it states, which nobody can fake without Bitcoin's own mining power.

The software is published with the Bitcoin height fixed and the header still unknown. A node started before that block exists waits for it. When the block appears, every waiting node, including those of the people who wrote the software, takes its header from the public Bitcoin chain and starts within seconds of the others. A node finds the header by asking public Bitcoin block explorers and uses it only when two independent ones agree; an operator who prefers not to rely on them can supply it by hand.

Anyone can check the anchor against Bitcoin afterwards: `--show-genesis` prints it.

## 12. The network

A node keeps the whole chain, checks every block and transfer itself, and relays what it accepts. Nodes find each other through a short list of seed addresses and by exchanging the addresses of nodes they know. A node behind a home router needs no configuration: it opens its own ports where the router allows, and otherwise receives blocks over connections it makes outward.

A node never signs anything on a user's behalf. Wallets build and sign transfers on the user's own computer and hand the finished transfer to a node.

Which transfers a miner puts in a block is the miner's choice and is not a network rule.

## 13. What this design does not do, and its known limits

- **A small proof-of-work network can be attacked cheaply.** Anyone with more mining power than everyone else combined can rewrite recent blocks. Total-work selection and the reorganisation limit reduce the damage; they do not remove the risk. It shrinks as honest mining grows.
- **RandomX favours processors today.** That is an observation about current hardware, not a guarantee. Section 9.2 is the remedy if it changes.
- **Capacity is modest.** At most 500 transfers every 45 seconds. Block relay between nodes is simple and has not been tested with thousands of nodes.
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
