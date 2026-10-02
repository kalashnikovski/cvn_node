# Covenant Standard (CVN) Protocol 📡
[![Discord Invite](https://shields.io)](https://discord.gg/xE2egYbumx)

A lightweight, high-performance Layer-1 blockchain consensus engine engineered completely from scratch in Go. This project is built as a pure, fair-launch Proof-of-Diligence (PoD) network designed to optimize transaction routing and state synchronization without relying on massive, bloated external frameworks.

## 🏛️ Core Architectural Blueprint

* **CPU-Centric Democratization:** The consensus engine implements a strict, adaptive mining difficulty ceiling clamp (capping target prefixes strictly between 4 to 7 hexadecimal zeros). This structural barrier guarantees that elite industrial ASIC/GPU mining setups cannot monopolize block production, ensuring a permanent meritocracy for standard consumer-grade home processors (benchmarked heavily on AMD Ryzen 9 7950X3D hardware).
* **Dual-Chamber Mempool Matrix:** Transaction prioritization balances user volition with system access equity via an 80/20 mempool split. While 80% of block payload allocation favors high-density fee offerings, a dedicated 20% room is permanently hardlocked to process zero-fee consumer transaction payloads sequentially by chronological arrival time (FIFO).
* **The Sabbatical Jubilee State Loop:** Grounded in timeless macroeconomic equilibrium principles, the protocol monitors asset velocity fields. Accounts remaining stagnant for a continuous 49-year virtual window undergo a systematic transaction weight decrement down to 0%, preventing long-term ledger ossification.
* **Low-Overhead Background Snapshot Daemon:** Features a robust background worker architecture that executes automated state-integrity sweeps and asynchronous transaction validation logging without interfering with high-throughput P2P socket handles.

---

## 🛠️ Quick-Start Infrastructure Setup

### Prerequisites
* **Operating System:** Windows 11 (with Administrator privileges for raw TCP port binding)
* **Development Environment:** Go (Golang) 1.21+ installed and configured in system PATH variables
* **Network Profile:** Ports `8080` (P2P Consensus) and `8081` (Visual Explorer UI) routed openly on your local router gateway

### 🚀 Launching Your Sovereign Node Client

To initialize your local database ledger environment, connect to the primary master seed anchor node, and activate your computational mining threads, open an **Administrator PowerShell** session and execute this single command string:

```powershell
Set-Location C:\; git clone https://github.com/kalashnikovski/cvn_node; Set-Location .\cvn_node; .\AutoOnboard_And_Mine.bat
```

1. The automated onboarding launcher will initialize your client and cleanly write your unique `miner_config.json` profile to disk.
2. It will output your fresh public address (Format: `CVN_...`). Copy it down safely!
3. Select **Option 1** to spin up the core mining loop. Paste your address when prompted.

Your workstation will instantly connect to the live network grid, download historical block segments, and start banking block rewards straight to your keys!

---

## 📡 Live Public Telemetry

* **Visual Blockchain Explorer Dashboard:** http://202.137.175.220:8081/
* **Developer Workspace / Core Repositories:** https://github.com/kalashnikovski/cvn_node
