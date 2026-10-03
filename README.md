# Covenant Standard (CVN) Protocol 📡
[![Discord Invite](https://shields.io/badge/Discord-Join%20Server-7289da?logo=discord&logoColor=white)](https://discord.gg/xE2egYbumx)

A lightweight, high-performance Layer-1 blockchain consensus engine engineered completely from scratch in Go, running on a hyper-efficient transactional binary **BoltDB key-value store** backend. This project features a fair-launch Proof-of-Diligence (PoD) network designed to ensure decentralized scalability and state integrity natively without relying on bloated external frameworks.

## 🏛️ Core Architectural Features

* **CPU-Centric Democratization:** Implements an adaptive difficulty ceiling clamp (targeting 4 to 6 hexadecimal zero prefixes) [INDEX]. This barrier prevents industrial ASIC/GPU mining setups from monopolizing block production, ensuring consumer-grade home processors can mine fairly (optimized for AMD Ryzen 9 7950X3D hardware) [INDEX].
* **Dual-Chamber Mempool Matrix (v4.0):** Closes transaction stranding loopholes using an isolated 80/20 mempool layout. While 80% of block allocations are prioritized by voluntary fee density, a hardlocked 20% chamber processes zero-fee consumer transaction payloads chronologically (FIFO) protected by a strict RAM spam shield [INDEX].
* **The 49-Year Jubilee Decay Loop:** Trims the global UTXO tracking footprint natively. Accounts remaining completely stagnant with zero transactional velocity for a continuous 49-year window automatically face systematic reward decrements to 0%, preventing dead weight from bloating active node memory states [INDEX].

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

## 📡 Live Mainnet Telemetry

* **Visual Blockchain Explorer Dashboard:** http://202.137.175.220:8081/
* **Developer Workspace / Core Repositories:** https://github.com/kalashnikovski/cvn_node