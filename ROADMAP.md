# Covenant Standard (CVN) Development Roadmap

Last reviewed against the code and the live network: 12 November 2026.

A box is ticked only when the feature is fully built **and** has been seen working on the live network. Everything else carries a short note: what exists today, and what is still missing.

## Phase 1: Stabilization (in progress)

- [x] **Heartbeat difficulty (45-second blocks):** Live since block 2500. Difficulty retargets every block; blocks have averaged 42–46 seconds per 100-block window, and a miner can no longer submit a burst of blocks seconds apart.
- [x] **Uncle rewards:** Live. When two miners solve the same height, the lower hash wins and the next block pays the loser 20% of the reward.
- [x] **Balance cache:** Live. Balances are computed from the on-disk UTXO set and updated per block.
- [ ] **Transactions (sending CVN between wallets):**
  - *Have:* built, activates at block 6100 (transaction hard fork). Wallets build and sign a transfer locally; nodes check it before it enters the mempool; miners include waiting transfers in blocks and collect their fees. Passed local tests, including a run of the real mining loop.
  - *Missing:* not yet seen on the live network (nothing can confirm before block 6100). The desktop and command-line wallets have not been tried by hand against a live node.
- [ ] **Transaction and block-content validation (consensus):**
  - *Have:* built, activates at block 6100. Every transfer must be signed by the owner of the coins it spends, spend only coins that exist, and not pay out more than it takes in. A transfer's ID is the hash of its contents. The block hash covers full transaction contents, so the reward recipient is locked in by the proof of work. The open endpoint that built transfers on a caller's behalf is removed. Local tests reject forged, stolen, double-spent, replayed and inflated transfers.
  - *Missing:* not yet live. No independent review. Amounts are still floating-point numbers.
- [ ] **Fork recovery:**
  - *Have:* a node that falls onto a losing branch finds the common ancestor, rolls back and re-syncs, including during startup. Seen working on the live network. From block 6100, removing a block also returns the coins it spent.
  - *Missing:* the winning chain is chosen by height, not by total work, which is no longer correct now that difficulty varies per block.
- [ ] **Sabbatical and Jubilee recycling:**
  - *Have:* a standalone simulator, and a 49-year check in the node that only writes a log line.
  - *Missing:* everything in consensus. No coins move and there is no `jubilee_vault`. Design is still open: whether it starts at year 7 or year 30, 20% of the remaining balance or of the original, per coin or per wallet, and how the vault pays miners.
- [ ] **Supply schedule agreed and documented:**
  - *Have:* 50 → 25 → 12.5 → 6.25 CVN over four 7.5-year epochs, then 5 CVN per block indefinitely.
  - *Missing:* the whitepaper says the 2.1 billion supply is mined in 30 years; the code mints about 492.75 million by year 30. The cap, the post-year-30 reward and the whitepaper need to agree. Whitepaper section 3.2 also still describes the old leading-zero puzzle.

## Phase 2: Mesh Expansion (in progress)

- [x] **Block and transaction gossip on port 8080:** Live between all nodes.
- [x] **Outbound relay (receive blocks without port forwarding):** Live. Every node keeps long-lived outbound connections to seeds and public peers and receives blocks down them. Confirmed by a home miner running in relay-only mode with no port forwarding.
- [ ] **Automatic router port mapping (UPnP):**
  - *Have:* the node finds the router, maps ports 8080 and 8081, renews the mapping and detects carrier-grade NAT. Confirmed on the Melbourne router and on a tester's home router.
  - *Missing:* only one machine per router can hold ports 8080-8081; a second machine is refused and falls back to relay mode (it still mines, but nobody can sync from it). No NAT-PMP/PCP fallback for routers without UPnP.
- [ ] **Peer discovery:**
  - *Have:* nodes exchange peer lists and connect to peers they learn about. Peers a node has reached itself are saved to `peers.json` and reused on the next start for discovery, live block feeds and sync, so a restarted node can find the network with every seed offline. Seeds can be overridden in `miner_config.json`.
  - *Missing:* not yet proven on the live network with the seeds actually down. A brand-new node with no `peers.json` still depends on the three built-in seed IPs.
- [ ] **Wire protocol optimization:**
  - *Have:* length-prefixed frames with a size limit and an HMAC tag.
  - *Missing:* payloads are uncompressed JSON. The HMAC key is a shared default built into the binary, so it filters noise but does not authenticate peers.

## Phase 3: Hardening (in progress)

- [ ] **Peer banning and scoring:**
  - *Have:* per-IP rate limiting, connection tracking, and a 5-minute ban after 10 malformed frames.
  - *Missing:* no ban for sending invalid blocks or transactions, no peer scoring or preference for reliable peers, and bans are lost on restart.
- [ ] **Mempool flooding safeguards:**
  - *Have:* per-IP rate limiting on gossip, and a basic shape check (transaction has an ID and outputs).
  - *Missing:* no signature or balance check before a transaction enters the mempool, no size limit, no expiry, and no fee-based eviction. The local `/transaction` HTTP endpoint is not rate limited.
- [ ] **Isolated stack scripting engine:** Scheduled (Q1 2027). Not started.
- [ ] **Master alert gateway protocol:** Scheduled (Q1 2027). Not started.

## Phase 4: Industrial Scale (Q2 2027)

- [ ] **Indexed database migration:** Scheduled. Not started. (The wallet currently downloads blocks to find its coins.)
- [ ] **Standalone SPV light client:** Scheduled. Not started.
- [ ] **ASIC-neutral proof-of-work optimization:** Scheduled. Not started.
