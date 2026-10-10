# 💎 Covenant Standard (CVN) Protocol 📡
[![Discord Invite](https://shields.io/badge/Discord-Join%20Server-7289da?logo=discord&logoColor=white)](https://discord.gg/xE2egYbumx)

🚨 TECHNICAL ANNOUNCEMENT / REPOSITORY HOTFIX (v1.0.1)
A critical configuration patch addresses a hardcoded developer address in `CustomMinerAddress`.

Covenant Standard (CVN) is a decentralized Layer-1 blockchain built in Go, utilizing a BoltDB backend (`cvn_mainnet.db`) to provide an immutable ledger optimized for consumer hardware.

---

## 🌐 1. Executive Summary

CVN levels the economic playing field by utilizing an adaptive CPU-optimized cryptographic difficulty loop, allowing home users to run nodes without expensive ASIC hardware.

---

## 🚀 2. Quick-Start One-Click Onboarding

* **OS / Setup:** Windows 11 (Run as Administrator), open ports `8080` (P2P) and `8081` (Web Explorer). Clone the repository and execute `.\AutoOnboard_And_Mine.bat` via Administrator PowerShell.
* **Fast-Boot Routine:** Download the pre-verified ledger snapshot (`cvn_mainnet.db`) to bypass initial syncing.

---

## 📡 3. Live Mainnet Telemetry & Seed Node Gateway

Includes distributed anchor nodes (Atlanta, Singapore, Melbourne) on ports `8080`/`8081`. Refer to the source repository for full addresses and the developer workspace at https://github.com/kalashnikovski/cvn_node

---

## 🤖 4. Deep-Tech Protocol Specifications

This section outlines low-level concurrency and cryptographic constraints. For the complete, unabridged technical specifications and deep-tech parameters, please refer to the referenced source document.

### ⚙️ Consensus Mechanism & Storage
* **Proof-of-Diligence (PoD):** An adaptive computational difficulty ceiling targeting 4 to 6 hexadecimal zero prefixes optimized for consumer hardware while resisting ASIC/GPU centralization.
* **BoltDB Binary Buckets:** Utilizes a low-latency, transactional key-value binary database store for serialized, ACID-compliant ledger transactions.

### 🔄 Concurrency, Economics & P2P Topography
* **Concurrency & Mempool:** Employs `sync.RWMutex` synchronization arrays and a dual-chamber 80/20 mempool layout (fee density vs. FIFO zero-fee allocation).
* **Macroeconomic Laws:** Integrates a 49-Year Sabbatical Jubilee velocity decay mechanism to prevent dead-wallet data bloat.
* **P2P Gossip Mesh:** Operates via a decentralized TCP Port 8080 mesh with semaphore connection gateways capping concurrent peer sockets.