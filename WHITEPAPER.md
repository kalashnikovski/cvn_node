# Covenant Standard (CVN) Protocol
## Layer-1 Ledger Specification & Monetary Constitution
Version 1.0 - Production Launch Specification (October 15, 2026)

1. Purpose and Scope
Covenant Standard (CVN) is a decentralized Layer-1 blockchain operating on a three-node bootstrap cluster (Melbourne, Singapore, Atlanta) with these core pillars:
- Absolute Zero Pre-Mine: The Genesis Block contains 0.00 CVN circulating supply with no developer or VC allocations.
- Equal Rule Enforcement: Consensus rules apply uniformly without administrative exceptions.
- Democratized Hardware: Validation remains accessible to standard consumer desktop CPUs.
- Anti-Capture Architecture: Prevents permanent concentration of validation power.

2. Monetary Rules & Emission
2.1 Fixed Supply Ceiling
The maximum supply is permanently capped at 2,100,000,000.00 CVN.

2.2 Organic Distribution & Year-30 Recycling
Tokens are earned via Proof-of-Diligence. The initial block reward is 50.00 CVN, halving every 7.5 years across four epochs over 30 years. At Year 30 (Block 21,024,000), the network transitions to an algorithmic 7-year recycling model that reclaims 20% from stagnant accounts to sustain ongoing validation incentives.

2.3 Strict UTXO Accounting Core
Uses a disk-enforced UTXO model with 8-decimal precision (%.8f) to ensure exact value conservation and prevent cross-platform serialization drift.
3. Consensus & Bidirectional Validation Firewalls
3.1 Block Hash Commitment
Every block hash is computed over all consensus-critical fields. Any block whose contents do not perfectly match its SHA-256 hash is instantly dropped by the network.

3.2 Dynamic Target Calibration
Blocks are produced via a Proof-of-Diligence leading-zero computational puzzle. The difficulty target dynamically adjusts block-by-block based on an active 45-second calibration window ticker loop to defend against hashrate spikes.

3.3 Bidirectional Validation Symmetry
To prevent local database drift, the exact same VerifyIncomingWireTransaction firewall intercepts transactions on both external network streams and local desktop application submissions before any data can enter the mempool queue. The database gate (SaveBlockToStorageInternal) deeply audits every transaction signature and input reference under an atomic ACID database transaction before disk mutation occurs.

3.4 Race-Proof Non-Destructive Recovery
During fork resolutions, the engine pauses mining and takes a snapshot of local tip index heights. It executes a backward chronological scan across remote endpoints to find the highest common ancestor hash. The engine then executes a targeted rollback transaction, deleting only the blocks mined on the minority fork above that ancestor point, re-instantiating any valid spent inputs back into the live UTXO set before fast-forwarding along the canonical timeline.

4. Decentralized Network Participation
4.1 Open Validation
Any participant may operate a full full node via our zero-configuration PowerShell onboarding script (AutoOnboard_And_Mine.ps1), validate the entire history natively via BoltDB (%016d zero-padded key-value indexing), and mine block rewards using consumer hardware.

4.2 Autonomous Peer Exchange (PEX)
Nodes maintain a dynamic mesh network over raw TCP sockets on Port 8080. When a peer connects, nodes organically exchange active directories (PEER_LIST:), spawning asynchronous background workers to dial unrecognized targets and self-expand the network topography on complete autopilot.

5. Future Evolutionary Roadmap & Upgrade Paths (Design Goals)
5.1 The Q2 2027 ASIC-Neutral 80/20 Soft-Fork Upgrade
To preserve consumer-hardware accessibility, the protocol is architected to undergo a formal consensus soft-fork in Phase 4 (Q2 2027). Once a mathematically proven memory-hard Proof-of-Work algorithm is finalized, it will introduce a dual-channel reward split based on the actual class of work performed. Rigs providing memory-hard CPU solutions will claim an 80% channel reward (40.0 CVN), while specialized industrial hardware running SHA-256 solutions will be clamped to a maximum 20% channel reward (10.0 CVN).

5.2 The Macroeconomic Sabbatical & Jubilee State Loops
The database reserves bucket partitions and naming for future multi-decade balance recycling features. Balances remaining completely stationary for 7-year Sabbatical and 49-year Jubilee scales will be subjected to automated consensus-level decay parameters. The protocol reclaims stagnant wealth, routing it to the jubilee_vault bucket to dynamically fund block rewards for future generations of node operators without diluting the fixed supply ceiling.

6. Upgrade and Governance Discipline
Changes to consensus rules require transparent public specification, broad community node adoption, and explicit activation thresholds. Covenant Standard prioritizes an un-bloated, stable core ledger state over unnecessary software complexity. All upgrades remain strictly subject to transparency and equal rule enforcement across the globe, ensuring the network remains owned entirely by the community from block number one.
