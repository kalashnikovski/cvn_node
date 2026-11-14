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
- [x] **Peers remembered between restarts**, and three seed nodes on three continents.
- [x] **Miner waits when its node is behind**, so it never mines on a stale chain.
- [ ] **Capacity for a large network.**
  - *Have:* comfortable for roughly 100–150 nodes.
  - *Missing:* persistent connections and announce-then-fetch block relay, needed before a large public launch.
- [ ] **Block size limit enforced.** Transfer-count limits are enforced; a byte limit is not.

## Phase 3: Launch rules (in development)

These are being built on a separate development network (`--network devnet`), which has its own key, ports and files and can run beside a test-network node.

- [x] **Separate network identities** and one place for every chain setting.
- [ ] **Amounts stored as whole units** (today they are checked as units but stored as decimals).
- [ ] **Supply schedule:** 50 CVN per block, halving every 7 years for 49 years; 486,727,500 CVN in total. Written into the settings with tests; not yet used by consensus.
- [ ] **Exact 45-second average.** Test-network blocks run about 6% fast; the launch rule corrects this.
- [ ] **Recycling of long-unmoved coins** to miners. Designed, not built.
- [ ] **RandomX mining algorithm** tuned for home processors. A Go implementation has been verified against the official test values; not yet integrated.
- [ ] **Limit on deep reorganisations.**
- [ ] **Proof that nobody mined early:** the genesis block will contain a value nobody can know before the announced launch time.

## Phase 4: Launch

- [ ] **Automated tests for every consensus rule.** Started: settings, amounts, chain selection, difficulty and the mining guard are covered and run on every change.
- [ ] **Public testnet of the launch rules** with outside miners.
- [ ] **Independent review.**
- [ ] **Whitepaper rewritten** to match what is built.
- [ ] **Launch at a pre-announced time**, from a new genesis block, with every setting published in advance.
