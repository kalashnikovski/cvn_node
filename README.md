[![Discord Invite](https://shields.io/badge/Discord-Join%20Server-7289da?logo=discord&logoColor=white)](https://discord.gg/xE2egYbumx)

# 💎 Covenant Standard (CVN) Protocol 📡

Covenant Standard (CVN) is a production-grade, built-from-scratch Layer-1 blockchain protocol engineered in Go. Utilizing an embedded key-value BoltDB engine (`cvn_mainnet.db`), the network is mathematically structured to enforce honest, non-dilutable accounting and decentralized consensus optimized for consumer-grade household hardware.

## 🌐 1. Executive Summary

CVN preserves public access to token distribution by utilizing an adaptive CPU-optimized computational difficulty loop. This prevents centralized ASIC mining warehouses or massive capital pools from monopolizing the ledger, keeping validation accessible to ordinary household processors forever. The protocol operates under an immutable Fair Launch mandate with absolute zero pre-mine or venture capital token allocations.

---

## 🚀 2. Quick-Start Onboarding Framework

To protect local data layers from timeline drift, the protocol enforces absolute validation symmetry. Full block synchronization must conclude prior to thread engagement.

*   **OS / Setup Environment:** Windows 11 (Run terminal as Administrator). Open port **8080** for P2P gossip mesh traffic and port **8081** for the web explorer dashboard.
*   **One-Click Autopilot Launch:** Open an Administrator PowerShell window inside your root directory and execute the native automation framework script:
    ```powershell
    .\AutoOnboard_And_Mine.ps1
    ```
*   ⚠️ **DEPRECATION NOTICE:** The legacy `AutoOnboard_And_Mine.bat` configuration engine has been permanently deprecated and removed from the active core files. Do not attempt to run old batch scripts, as they do not support the unified binary execution tracks and will fail to mount asymmetric cryptographic key parameters.

---

🐧 Linux Setup (Ubuntu / Debian Baseline Path)

• OS / Setup Environment: Xubuntu / Ubuntu / Debian Linux distribution layouts. Ensure your home gateway or server security policies allow ingress/egress traffic on port 8080 for the P2P gossip mesh and port 8081 for the HTTP API bulk sync pipeline.
• Core Dependencies: Ensure Go (v1.21+) is installed natively on your path (sudo apt install golang or from official repositories).
• Headless Terminal Onboarding Pass: Open your terminal inside your root /cvn_node directory workspace, clean out legacy state files, compile your custom binary engine, and ignite your validation threads:bash
# 1. Clean the execution cache space out-of-the-box
rm -f cvn_mainnet.db miner_config.json cvn_secret_backup.txt

# 2. Derive your secure cryptographic key-pair coordinates over CSPRNG
go run . --generate-wallet

# 3. Compile your production-ready, bare-metal ledger core target
go build -o cvn_miner .

# 4. Launch on autopilot and connect directly to the Melbourne Master truth anchor
./cvn_miner --connect 202.137.175.220:8081

• Hardware Resource Control: By default, the mining loop will execute on all available system processor threads. To limit the hardware mining footprint to a specific thread frame allocation (e.g., exactly 4 CPU cores), prepend your launch command with the native environment routing variable gate:bash

GOMAXPROCS=4 ./cvn_miner --connect 202.137.175.220:8081

## 📡 3. Live Mainnet Telemetry & Seed Node Gateway

The network initializes natively from an absolute zero-state Genesis Block #0 containing 0.00 CVN tokens and empty transaction arrays. New tokens only enter existence when an honest CPU processor solves a valid block reward. Nodes dynamically construct a self-healing network web by routing data through our active cross-continental bootstrap anchor cluster:

*   **Melbourne Master Rig:** `202.137.175.220` (Ports 8080/8081)
*   **Singapore Cloud VPS:** `207.148.67.11` (Ports 8080/8081)
*   **Atlanta Cloud VPS:** `64.177.45.153` (Ports 8080/8081)

---

## 🤖 4. Deep-Tech Protocol Specifications

### ⚙️ Consensus Mechanism & Storage Primitives
*   **Proof-of-Diligence (PoD):** An adaptive computational leading-zero target ceiling that recalibrates block-by-block using a responsive 45-second calibration window ticker loop to smooth difficulty charts under changing global hashrates.
*   **BoltDB Binary Buckets:** Low-latency, ACID-compliant key-value disk storage indexing block structures under a uniform zero-padded keyspace layout (`%016d`). The storage commitment gate (`SaveBlockToStorageInternal`) loops through and deeply audits every transaction signature and input reference under an active database transaction before disk mutation can occur.

### 🔄 Concurrency, Serialization & P2P Topography
*   **Bidirectional Mempool Firewall:** Employs explicit `sync.RWMutex` synchronization tracking arrays. To ensure absolute data consistency, the exact same `VerifyIncomingWireTransaction` firewall intercepts payloads across both external network socket arrivals and local desktop application submissions *before* any data can enter the mempool queue.
*   **Serialized Wire Protocol:** To eliminate floating-point drift across multi-platform daemons, all transaction preimage template strings are hard-locked to an explicit 8-decimal precision length (`%.8f`).
*   **Autonomous Peer Exchange (PEX):** Sockets on Port 8080 handle continuous data propagation. When a peer connects, nodes organically exchange active directories (`PEER_LIST:`), spawning asynchronous background workers to dial unrecognized targets and self-expand the network topography completely on autopilot.

---

## 🏛️ 5. Long-Horizon Economic Policy (Roadmap Design Goals)

*   **ASIC-Neutral PoD Optimization (Q2 2027 Upgrade):** A future consensus soft-fork will introduce a verified memory-hard hashing layer to segment block production based on work verification. Memory-hard CPU solutions will claim an 80% channel reward (40.0 CVN), while SHA-256 ASIC solutions will be capped by consensus law to a 20% reward pool (10.0 CVN).
*   **The Macroeconomic Sabbatical Jubilee Loops:** Balances remaining completely stationary for extended scriptural horizons (7-year Sabbatical and 49-year Jubilee scales) are subjected to automated consensus-level decay parameters. The protocol reclaims 20% of stagnant wealth, routing it to the `jubilee_vault` bucket to dynamically fund block rewards for future generations of full node operators without diluting the fixed supply ceiling.