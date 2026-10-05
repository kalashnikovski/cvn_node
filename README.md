# 💎 Covenant Standard (CVN) Protocol 📡
[![Discord Invite](https://shields.io/badge/Discord-Join%20Server-7289da?logo=discord&logoColor=white)](https://discord.gg/xE2egYbumx)

Covenant Standard (CVN) is an independent, decentralized Layer-1 blockchain network built completely from scratch in the Go programming language, running natively on a hyper-efficient transactional binary **BoltDB key-value store** backend (`cvn_mainnet.db`). By decoupling state consensus from corporate infrastructure, CVN establishes a highly secure, immutable ledger optimized specifically for consumer computer hardware without relying on bloated external frameworks.

---

## 🌐 1. Executive Summary

Most modern cryptocurrency networks are dominated by massive, industrial server farms using expensive, specialized ASIC hardware. This creates a highly centralized system where regular home users are completely squeezed out of the network.

**Covenant Standard changes the rules.** The protocol is intentionally engineered to level the economic playing field. By enforcing an adaptive cryptographic difficulty loop optimized for standard consumer CPUs, any sovereign individual can run a node from their own home office or desktop computer. CVN features a hardlocked, un-manipulable maximum token supply cap and an automated architectural health engine designed to protect the integrity of the ledger over decades.

---

## 🚀 2. Quick-Start One-Click Onboarding

Follow this simple sequence to spin up your local hardware threads, connect to the live global mainnet mesh, and begin validating block rewards natively on your computer without needing to install Go, Git, or a compiler toolchain.

### 📋 Prerequisites
* **Operating System:** Windows 11 (Run with Administrator privileges for raw TCP port binding).
* **Network Profile:** Router ports `8080` (P2P Consensus) and `8081` (Visual Web Explorer) open and routed to your machine.

### 🛠️ Standalone Node Client Installation Steps
1. Download the latest release asset folder from this repository or clone the source tree directly:
   ```powershell
   git clone https://github.com/kalashnikovski/cvn_node
   cd cvn_node
   ```
2. Open an **Administrator PowerShell** window inside your local project directory folder and run:
   ```powershell
   .\AutoOnboard_And_Mine.bat
   ```
3. **Select Option 1** from the interface menu prompt if you are a new user. 

*BOOM! The standalone binary will automatically initialize your unique cryptographic public/private key-generation pair, write your profile config to disk, safely clear environment variables using sequential execution jumps, and instantly lock hands with our bootstrap entry gates to stream the full historical ledger block height dynamically to your private keys!*

---

## 📡 3. Live Mainnet Telemetry & Seed Node Gateway

To ensure absolute network redundancy, automated failover protection, and high-throughput data replication, the Covenant Standard mainnet relies on a distributed multi-anchor blueprint. Miners and full nodes can synchronize their ledger states through either of the live bootstrap entry gates below:

### 🇸🇬 1. Cloud Master Seed Node (Singapore Hub)
* **Visual Blockchain Explorer Dashboard:** http://207.148.67.11:8081
* **Public Discovery Peer Roster Index:** http://207.148.67.11:8081/peers
* **Unique Network Addresses Tracker:** http://207.148.67.11:8081/addresses
* **P2P Mesh Consensus Connection Gateway:** `207.148.67.11:8080`

### 🇦🇺 2. Anchor Seed Node Rig (Melbourne Hub)
* **Visual Blockchain Explorer Dashboard:** http://202.137.175.220:8081/
* **Public Discovery Peer Roster Index:** http://202.137.175.220:8081/peers
* **Unique Network Addresses Tracker:** http://202.137.175.220:8081/addresses
* **P2P Mesh Consensus Connection Gateway:** `202.137.175.220:8080`

👉 **Developer Workspace / Core Source Repositories:** https://github.com/kalashnikovski/cvn_node

---

## 🤖 4. Deep-Tech Protocol Specifications

This section maps out the low-level asynchronous concurrency models and cryptographic constraints governing the Covenant Standard state machine.

### ⚙️ Consensus Mechanism: Proof-of-Diligence (PoD)
Covenant Standard does not utilize energy-centralized Proof-of-Work (PoW) hashes like SHA-256, nor does it rely on capital-centralized Proof-of-Stake (PoS) slashing layers. Instead, it enforces a proprietary **Proof-of-Diligence (PoD)** algorithm. PoD implements an adaptive computational difficulty ceiling clamp (targeting 4 to 6 hexadecimal zero prefixes) that rewards sustained thread alignment. The difficulty matrix calculates a dynamic diligence score mapping consumer CPU clock velocities, ensuring optimized efficiency on consumer processors like the **AMD Ryzen 9 7950X3D hardware** while remaining naturally resilient against ASIC/GPU centralization.

### 📦 Hard Disk Storage Model: BoltDB Binary Buckets
The ledger bypasses heavy, bloated relational database layers entirely. Instead, the CVN engine interacts natively with a low-latency, transactional key-value binary database store via **BoltDB** on disk tracks. Blockchain transactions are serialized into tight binary payloads and committed inside ACID-compliant write transactions using a single-writer, multi-reader architecture.

### 🔄 Concurrency Isolation & The Dual-Chamber Mempool Matrix
To prevent memory race conditions, deadlocks, and write-lock freezes during intense block production, the core Go engine implements strict asynchronous concurrency tracking:
* **`sync.RWMutex` Synchronization Arrays:** Read/Write mutex boundaries safely decouple the lightweight wallet client API queries from the heavy background mining thread loops.
* **Dual-Chamber Mempool Matrix (v4.0):** Closes transaction stranding loopholes using an isolated 80/20 mempool layout. While 80% of block allocations are prioritized by voluntary fee density, a hardlocked 20% chamber processes zero-fee consumer transaction payloads chronologically (FIFO) protected by a strict RAM spam shield.

### ⏳ Macroeconomic Ledger Laws: The 49-Year Sabbatical Jubilee
To counter generational wealth hoarding, dead-wallet data bloat, and Unspent Transaction Output (UTXO) expansion, CVN integrates an automated velocity decay mechanism tracking a programmatic **49-Year Sabbatical Jubilee** frequency curve. Inactive, stagnant cryptographic accounts remaining completely stagnant with zero transactional velocity for a continuous 49-year window automatically face systematic reward decrements to 0%, preventing dead weight from bloating active node memory states and preserving absolute ledger velocity.

### 📡 Distributed P2P Topography & Semaphore Shields
The network routes block and transaction propagation horizontally via an active, decentralized **Continuous Roster P2P Gossip Mesh network** over TCP Port 8080.
* **Gossip Daemons:** Nodes continuously cross-gossip validated peer identity arrays, bypassing centralized directory lookups.
* **Semaphore Connection Gateways:** To protect seed nodes against multi-threaded resource exhaustion attacks and hosting throttling thresholds, the ingestion server enforces a channel-based semaphore gate, capping maximum active concurrent peer sockets to 15 parallel threads and instantly dropping excess socket overflows at the interface level via a non-blocking Go `select-default` structure.