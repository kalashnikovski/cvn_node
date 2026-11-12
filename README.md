# 💎 Covenant Standard (CVN) Protocol 📡

Covenant Standard (CVN) is a production-grade Layer-1 blockchain protocol engineered in Go using an embedded BoltDB engine (`cvn_mainnet.db`), designed for consumer hardware with a Fair Launch mandate.

## 🌐 1. Executive Summary
CVN uses an adaptive CPU-optimized difficulty loop to keep validation accessible to household processors and prevent ASIC/capital monopolies.

## 🚀 2. Quick-Start Onboarding Framework

### 🪟 Windows Setup (Windows 11)
* **OS / Setup Environment:** Windows 11 
* **Headless Miner Launch:** Run `LaunchMiner.bat` as Administrator to generate a wallet, sync, and mine: `.\LaunchMiner.bat`
* **Desktop Wallet GUI Launch:** Run `LaunchWallet.bat` to open the graphical wallet interface: `.\LaunchWallet.bat`

### 🐧 Linux Setup (Ubuntu / Debian Baseline Path)
* **OS / Setup Environment:** Xubuntu/Ubuntu/Debian. Allow ingress/egress on port **8080** (P2P) and **8081–8082** (HTTP API).
* **Core Dependencies:** Ensure Go (v1.21+) is installed (`sudo apt install golang`).
* **Headless Terminal Onboarding:** In your `/cvn_node` directory:
  ```bash
  rm -f cvn_mainnet.db miner_config.json cvn_secret_backup.txt
  go run . --generate-wallet
  go build -o cvn_miner .
  ./cvn_miner --connect 202.137.175.220:8081
  ```
* **Hardware Resource Control:** Limit CPU core usage via environment variable:
  ```bash
  GOMAXPROCS=4 ./cvn_miner --connect 202.137.175.220:8081
  ```

## 📡 3. Live Mainnet Telemetry & Seed Node Gateway
Connects via cross-continental bootstrap anchors:
* **Melbourne Master Rig:** `202.137.175.220` (Ports 8080/8081)
* **Singapore Cloud VPS:** `207.148.67.11` (Ports 8080/8081)
* **Atlanta Cloud VPS:** `64.177.45.153` (Ports 8080/8081)

## 🤖 4. Deep-Tech Protocol Specifications
* **Consensus & Storage:** Proof-of-Diligence (PoD) with a 45-second calibration window and ACID-compliant BoltDB storage (`SaveBlockToStorageInternal`).
* **Concurrency & P2P:** Bidirectional mempool firewall (`sync.RWMutex`), 8-decimal precision transaction templates (`%.8f`), and autonomous peer exchange (`PEER_LIST:` on Port 8080).

## 🏛️ 5. Long-Horizon Economic Policy
* **ASIC-Neutral PoD Optimization (Q2 2027):** Software fork splitting rewards (80% memory-hard CPU / 20% SHA-256 ASIC).
* **Sabbatical & Jubilee Loops:** Automated decay parameters on stationary balances over 7-year and 49-year scales, routing 25% of stagnant wealth to the `jubilee_vault`.