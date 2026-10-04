# Covenant Standard (CVN) Protocol 📡
[![Discord Invite](https://shields.io/badge/Discord-Join%20Server-7289da?logo=discord&logoColor=white)](https://discord.gg/xE2egYbumx)

A lightweight, high-performance Layer-1 blockchain consensus engine engineered completely from scratch in Go, running on a hyper-efficient transactional binary **BoltDB key-value store** backend. This project features a fair-launch Proof-of-Diligence (PoD) network designed to ensure decentralized scalability and state integrity natively without relying on bloated external frameworks.

## 🏛️ Core Architectural Features

* **CPU-Centric Democratization:** Implements an adaptive difficulty ceiling clamp (targeting 4 to 6 hexadecimal zero prefixes) [INDEX]. This barrier prevents industrial ASIC/GPU mining setups from monopolizing block production, ensuring consumer-grade home processors can mine fairly (optimized for AMD Ryzen 9 7950X3D hardware).
* **Dual-Chamber Mempool Matrix (v4.0):** Closes transaction stranding loopholes using an isolated 80/20 mempool layout. While 80% of block allocations are prioritized by voluntary fee density, a hardlocked 20% chamber processes zero-fee consumer transaction payloads chronologically (FIFO) protected by a strict RAM spam shield.
* **The 49-Year Jubilee Decay Loop:** Trims the global UTXO tracking footprint natively. Accounts remaining completely stagnant with zero transactional velocity for a continuous 49-year window automatically face systematic reward decrements to 0%, preventing dead weight from bloating active node memory states.

---

## 🛠️ Quick-Start One-Click Onboarding

### Prerequisites
* **Operating System:** Windows 11 (Run with Administrator privileges for raw TCP port binding)
* **Network Profile:** Router ports `8080` (P2P Consensus) and `8081` (Visual Web Explorer) open and routed to your machine

### 🚀 Launching Your Standalone Node Client

You no longer need to have Go, Git, or a compiler toolchain installed. To connect your computer to our live distributed network grid, follow these simple steps:

1. Download the latest release asset folder from this repository.
2. Open an **Administrator PowerShell** window in the folder directory and run:
   ```powershell
   .\AutoOnboard_And_Mine.bat
   ```
3. **Select Option 1** if you are a new user. The standalone binary will automatically generate a fresh, unique cryptographic wallet key for you, write your profile config to disk, sync historical blocks from the master seed node, and immediately start mining block rewards to your private keys!

---

## 📡 Live Mainnet Telemetry & Seed Node Gateway

To ensure absolute network redundancy and high-throughput data replication, the Covenant Standard mainnet relies on a distributed multi-anchor blueprint. Miners and full nodes can synchronize their ledger states through either of the live bootstrap entry gates below:

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

* **Developer Workspace / Core Source Repositories:** https://github.com/kalashnikovski/cvn_node
