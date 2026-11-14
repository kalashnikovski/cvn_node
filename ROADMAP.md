# Covenant Standard (CVN) Roadmap

Last reviewed against the code and the live test network: 15 November 2026.

A box is ticked only when the feature is built **and** has been seen working on the live test network. Everything else says what exists and what is missing.

## Phase 1: A working chain (test network)

- [x] **45-second blocks with per-block difficulty adjustment.** Live since block 2,500.
- [x] **Uncle rewards.** When two miners solve the same height, the next block pays the loser 20% of the reward.
- [x] **Sending CVN between wallets.** Live since block 6,100. Every transfer is signed by its owner; nodes reject transfers that spend coins that do not exist, belong to someone else, or pay out more than they take in.
- [x] **Block contents locked by proof of work.** The block hash covers every transaction in full.
- [x] **Whole-number amounts.** Live since block 6,600. Every amount is checked as a whole number of units (1 CVN = 100,000,000 units) with no rounding tolerance.
- [x] **Fork recovery and sync from scratch.**
- [ ] **Chain selection by total work.**
  - *Have:* built and running on every node. A node keeps its chain unless a competing branch proves more mining work, and restores its own blocks if the competing branch fails validation.
  - *Missing:* two live nodes genuinely disagreeing has not yet been observed.
- [x] **Desktop wallet (Windows) and text wallet (Linux and Windows).**

## Phase 2: Network

- [x] **Block and transfer gossip** between all nodes.
- [x] **Mining without port forwarding.** Nodes behind a home router receive blocks through an outbound connection; routers that support it are configured automatically.
- [x] **Peers remembered between restarts**, and six seed nodes on three continents, none of which mines.
- [x] **Miner waits when its node is behind**, so it never mines on a stale chain.
- [ ] **Capacity for a large network.**
  - *Have:* comfortable for roughly 100–150 nodes.
  - *Missing:* persistent connections and announce-then-fetch block relay, needed before a large public launch.
- [ ] **Block size limit enforced.** Transfer-count limits are enforced; a byte limit is not.
- [ ] **Automatic router port mapping (UPnP).**
  - *Have:* the node finds the router, maps its ports, renews the mapping and detects carrier-grade NAT.
  - *Missing:* only one machine per router can hold the ports; a second falls back to relay mode (it still mines, but nobody can sync from it). No NAT-PMP/PCP fallback for routers without UPnP.
- [ ] **Peer discovery without the seeds.**
  - *Have:* nodes exchange peer lists and reuse remembered peers on restart.
  - *Missing:* not yet proven with the seeds actually down; a brand-new node still depends on the built-in seed addresses.
- [ ] **Wire protocol.**
  - *Have:* length-prefixed frames with a size limit and a message key.
  - *Missing:* payloads are uncompressed text, and the key is shared by every node, so it filters noise but does not authenticate peers.
- [ ] **Peer banning and scoring.**
  - *Have:* per-address rate limiting, a 5-minute ban after repeated malformed messages, and a 10-minute ban for sending a block whose proof of work is false.
  - *Missing:* no ban for sending other invalid blocks or invalid transfers, no preference for reliable peers when connections are full, and bans are lost on restart.
- [x] **Mempool safeguards.** A transfer is checked for a valid signature and real coins before it is stored or passed on; the mempool has a size limit and a 2-hour expiry.

## Phase 3: Launch rules (in development)

These are being built on a separate development network (`--network devnet`), which has its own key, ports and files and can run beside a test-network node.

- [x] **Separate network identities** and one place for every chain setting.
- [ ] **Amounts stored as whole units.**
  - *Have:* built. Inside the program every amount is a whole number of units, and on devnet that is also how it is written into blocks and hashed. The test network keeps its original decimal format so its existing blocks stay valid. The whole real chain replays through the new code with identical hashes and balances.
  - *Missing:* not yet running on the live test network.
- [ ] **Supply schedule:** 50 CVN per block, halving every 7 years for 49 years; 486,727,500 CVN in total.
  - *Have:* built and enforced on devnet, including what happens after the last reward: a block may then pay its miner only the fees of the transfers it contains.
  - *Missing:* a public testnet of the launch rules.
- [ ] **Exact 45-second average.**
  - *Have:* built on devnet. The difficulty rule applies from block 1 and also steers by how far the chain is from its schedule. In a simulation of 300,000 blocks it averages 45.00 seconds, including through large swings in mining power; the test network's rule averages about 43.5 in the same simulation.
  - *Missing:* a long run on a real network; a realistic starting difficulty for launch.
- [ ] **The vault and the fee split.**
  - *Have:* built on devnet. The vault is a counter kept by the rules; no key can spend from it. While new coins are still being issued, a fifth of every block's fees goes into it and the miner keeps the rest. Every block, the vault pays a fixed fraction of what it holds to that block's miner.
  - *Missing:* a public testnet of the launch rules.
- [ ] **Minimum output size.** Built on devnet: a transfer output must be at least 0.0001 CVN, and wallets leave smaller change to the miner.
- [ ] **Recycling of long-unmoved coins** into the vault.
  - *Have:* built on devnet. Every coin has an age. A coin not spent for 7 years has a fifth of its value moved into the vault at the end of the block that reaches that age, and then starts a new 7-year count; a coin that would be left below the minimum output is moved in full. Spending a coin, even to yourself, resets its age, so an owner who still holds the key loses nothing. The sweep is reversed if its block is removed in a reorganisation. Anything a block could have paid its miner but did not also goes to the vault, so nothing issued is ever destroyed.
  - *Missing:* a public testnet.
- [x] **Combine my coins (wallet).** In the text wallet and the desktop wallet. It sends a wallet's coins back to its own address in groups of up to 200, smallest first, each group becoming one coin; up to 5,000 coins per use. Each group is an ordinary transfer, so it needs no new network rule and works on every network. It removes the 200-coins-per-transfer limit as a practical obstacle and is also how a long-term holder refreshes coins before the 7-year sweep.
- [ ] **RandomX mining algorithm**, tuned for home processors.
  - *Have:* built on devnet. The engine is kept inside the repository and reproduces RandomX's official test values; CVN runs it with its own identifying settings, so mining software built for other coins does not produce valid CVN work, while the memory settings that keep graphics cards and custom chips at bay are untouched. The key changes once a day and always sits beyond the reorganisation limit. The algorithm is chosen by block height, so it can be replaced later without restarting the chain; a test chain switches algorithm part-way and carries on.
  - *Missing:* a public testnet; a realistic starting difficulty for launch.
- [ ] **A miner as fast as the best available.**
  - *Have:* a CVN edition of XMRig, the most widely used RandomX miner, published at https://github.com/kalashnikovski/cvn-xmrig with Windows and Linux builds. It differs from XMRig only in CVN's two RandomX settings and in having no built-in donation. A CVN node on a RandomX network runs a work server that such a miner connects to; the node builds and checks every block the miner finds, and pauses its own miner meanwhile. Measured on one 16-core desktop processor: 15,570 hashes per second with the CVN edition against 9,770 for the miner built into the node. A built-in speed test is included (`--randomx-bench`).
  - *Also have:* the built-in miner now uses large memory pages when the system grants them, which on the same processor raised it from about 12,800 to about 16,400 hashes per second. Miners who do nothing special still get the ordinary speed; those who enable large pages get most of the way to a tuned miner without installing anything else.
  - *Also have:* a choice of miner. `--miner xmrig` (or a line in `miner_config.json`) makes the node start the CVN edition of XMRig itself, feed it work, show its speed on the usual dashboard and stop it on exit; the built-in miner remains the default and takes over if the fast miner is missing or keeps failing. `--threads N` limits either. A guide covers both miners and how to enable large pages: [MINING_GUIDE.md](MINING_GUIDE.md).
  - *Missing:* not yet tried on Linux or across several machines. Antivirus programs may flag a program that starts mining software; this needs checking on testers' machines.
- [ ] **One way to write a block or a transfer.** Built on devnet after checking the specification against the code. The block identifier is the hash of the header's fields each written with its length, so two different headers can never share an identifier (the first test network's format joined text with dashes, and a test shows two headers that collide there). Every header field has one permitted form. A transfer carries nothing its sender did not sign, and what is signed includes the network's name, so a transfer made for one CVN network is invalid on any other. Reward coins obey the same minimum as every other coin. Blocks are limited to 300,000 bytes, and miners fill them accordingly. The uncle share is off on the launch rules: nodes never verified the named uncle, so it was voluntary in practice.
  - *Missing:* a public testnet. To decide: the block size figure, and whether a verified uncle share is wanted later.
- [ ] **How a miner fills a block.** Built: four fifths of a block goes to transfers that pay a fee, highest first; one fifth is kept for fee-less transfers of coins at least a day old, longest-waiting first; space one lane does not use goes to the other. So a fee buys priority, and moving your own coins for free still gets through when blocks are full, while passing coins back and forth to flood the chain does not. This is the published miner's behaviour, not a network rule.
- [ ] **Peers that send bad work are cut off.** Built: a peer that sends a block claiming to extend a node's tip with invalid proof of work is disconnected and refused for 10 minutes. Checking a RandomX proof costs real time, so this stops a peer making nodes do it for nothing. Only what is certain counts: a block from another branch is never held against its sender.
- [ ] **Addresses that catch typing mistakes.** Built: on the launch rules an address ends with a 4-byte check on the rest, and the rules themselves refuse to make a coin out to anything that is not a well-formed address. Every single-character mistake and every swap of two neighbouring characters is caught. An address saved before the check existed is completed automatically for mining; one typed into a wallet is not.
- [ ] **Limit on deep reorganisations.** Built on devnet: a node will not replace more than 100 of its own blocks, however much work a competing chain shows. Not yet seen in action on a real network.
- [ ] **Proof that nobody mined early.**
  - *Have:* built. A launch network's first block is tied to a Bitcoin block whose height is announced before that block is mined. Miners supply that block's 80-byte header at the start; the node checks the header carries Bitcoin's proof of work, writes its hash into the first block and dates the first block with its timestamp. So nobody can mine, or prepare to mine, before the Bitcoin block exists, and nobody can date the first blocks in the past to pull the difficulty down. The software ships with the height fixed and the header open, so its publisher has no head start either.
  - *Also have:* a waiting mode. A node started before the Bitcoin block exists waits and starts by itself when it appears, so everyone who is waiting starts within seconds of each other. It asks public Bitcoin block explorers and uses a header only when two agree; `--no-anchor-lookup` turns that off for an operator who supplies the header by hand. Rehearsed against a real Bitcoin block on the development network.
  - *Missing:* a real launch. If the anchor block itself is replaced in a rare Bitcoin reorganisation just after it appears, nodes could start from two different blocks and the launch would have to be repeated.
- [ ] **The CVN Testnet.** Defined (`--network cvn_testnet`): every launch rule, its own ports, files and message key, the six seed nodes, and a starting difficulty of 1,000,000. Not scheduled: it cannot be started until a Bitcoin height is announced in a release.
- [ ] **Automated tests for every consensus rule.** Started: settings, amounts, chain selection, difficulty and the mining guard are covered and run on every change.
- [ ] **Public testnet of the launch rules** with outside miners.
- [ ] **Independent review.**
- [ ] **Whitepaper rewritten** to match what is built.
- [ ] **Launch at a pre-announced time**, from a new genesis block, with every setting published in advance.

## Phase 5: After launch

These were on the roadmap before the launch rules were designed and are still planned. None has been started.

- [ ] **ASIC-neutral proof of work, long term.** RandomX (Phase 3) is the starting point. The mining check is being built so that the algorithm can be changed at a chosen block height without restarting the chain, so a later algorithm designed specifically for CVN can replace it if specialised hardware ever appears.
- [ ] **Indexed database.** Wallets currently ask a node to scan for their coins; an index would make that fast on a large chain.
- [ ] **Standalone light client (SPV).** A wallet that verifies its own transactions without holding the whole chain.
- [ ] **Scripting engine.** Spending conditions beyond a single signature.
- [ ] **Network alert protocol.** A way to tell every node operator about an urgent upgrade.
