# 💎 The Covenant Standard (CVN) Protocol v3

An open-source, multi-threaded Layer-1 sovereign state engine featuring automated macroeconomic velocity controls, a randomized 21-Witness validation pass tier, and an ASIC-neutral consensus model built entirely in Go.

---

## 📜 Core Architectural & Scriptural Tokenomics

The CVN network enforces an un-alterable, algorithmic token framework translating ancestral socioeconomic protection parameters directly into hardened, functional software mechanics:

1. **The Sabbatical Jubilee State Loop:** To prevent wealth ossification and resource hoarding, the ledger scans unspent outputs using a **49-Year Canonical Boundary**. Balances that remain stagnant without network velocity or transactional movement for 49 years lose consensus validation weight (decremented to 0%), and future transaction offerings are programmatically routed into a permanent cryptographic burn void (`0x0000000000000000000000000000000000000000_BURN_VOID`).
2. **Model B Free-Will Offerings:** Network transactions bypass rigid, mandatory minimum baseline gas structures. The priority engine evaluates a voluntary score metric (`FreeWillOffering / DataSizeKB`), naturally incentivizing distributed desktop operators via genuine corporate volition rather than centralized enforcement.
3. **The 21-Witness Guard Matrix:** Block generation requires verification confirmations from 21 randomly selected active network node identities before permanent local database storage updates execute, driving the mathematical probability of double-spends to absolute zero.

---

## 🧬 Structural Migration: Account Model vs. Bitcoin-Style UTXO

The Covenant Standard Protocol v3 operates on a hardened **Unspent Transaction Output (UTXO) data graph structure** to match the foundational cryptographic integrity of early cypherpunk architecture.

Use code with caution.
┌──────────────────────────────────────┐     ┌──────────────────────────────────────┐
│        LEGACY ACCOUNT MODEL          │     │        MODERN UTXO CASH GRAPH        │
├──────────────────────────────────────┤     ├──────────────────────────────────────┤
│ Address: CVN_c43b46...               │     │ TxID_A: [Output 0: 50.00 CVN] (Unspent)
│ Total Balance: 200.00 CVN            │ ──> │ TxID_B: [Output 1: 15.50 CVN] (Unspent)
│ (Blind state addition/subtraction)   │     │ TxID_C: [Output 0: 10.00 CVN] (Spent)
└──────────────────────────────────────┘     └──────────────────────────────────────┘

#### Key Architectural Distinctions for Core Contributors:

1. **Digital Cash Clumps vs. Account Balances:** Coins do not sit statically under an account address string. Instead, they exist as individual, atomic cryptographic outputs (`UTXOOutput`) floating on the public ledger. To spend tokens, a transaction must explicitly reference and consume a historical, unspent output as an `UTXOInput`, proving legal ownership via ECDSA signature hashes.
2. **Deterministic Coin Fracturing & Change Routing:** Because UTXO outputs cannot be partially spent or broken in half inside the database vault, they must be consumed in their entirety. If a user holds a `50.00 CVN` unspent coinbase chunk and wishes to transfer `10.00 CVN` to a peer, the engine executes a protocol-enforced fracturing split:
   * **Output 0:** `10.00 CVN` routed cleanly to the recipient's target address.
   * **Output 1 (The Change):** `40.00 CVN` cryptographically routed straight **back to the sender's own public address** as a brand-new, unspent cash profile.
3. **High-Speed Non-Blocking Balance Sweeps:** The node and visual client interface (`wallet.go`) bypass raw state table calculations entirely. Current address balances are derived natively via a full ledger scan—accumulating all generated outputs matching the user's address and deleting any that appear in a subsequent transaction's input list.
4. **Hardened Supply-Cap Immutability:** Initial token allocation limits are permanently locked inside a neutral, un-spendable system identifier (`RESERVE_POOL_UNALLOCATED_SUPPLY`). Tokens cannot be created out of thin air or manipulated via state injection; new spendable supply is strictly introduced into circulation via verified CPU Proof-of-Diligence (PoD) block mining rewards.

---

## 🛠️ Workspace Installation & Compilation Guide

### Prerequisites
- Go Runtime environment toolchain installed locally (`v1.20+` recommended)
- CGO Compiler environment tools enabled (`gcc` C-compiler context parameters)

### 1. Clone the Workspace
Clone the repository workspace folder directory down to your machine:
```bash
git clone https://github.com
cd cvn_node
```

### 2. Fetch Dependencies
Initialize local tracking checksum signatures and fetch layout module components:
```bash
go mod tidy
```

### 3. Compile the High-Performance Native Binary
Compile your source code files into a single, high-speed standalone binary executable file before starting nodes:
```powershell
# Windows PowerShell Build Command
$env:CGO_ENABLED="1"; go build .
```

---

## 🚀 Running the Universal Core Node Matrix

The ecosystem includes automated desktop scripts to handle user onboarding instantly with zero command line configuration friction.

### ⚒️ Launching the Mining Node Engine
Simply execute the universal batch script file inside your project directory:
```powershell
.\LaunchMiner.bat
```
- **Automated First-Time Onboarding:** If the script detects no pre-existing setup, it will automatically call the cryptographic key generator, output a fresh, unique **Private Key (Hex)** and **Wallet Address (`CVN_...`)** onto your screen, save a local profile configuration (`miner_config.json`), and start mining instantly.
- **Interactive Multi-Profile Routing:** On subsequent launches, the script will show your active profile address. You can press `[ENTER]` directly to resume mining, or type `NEW` to safely clear the file and register a completely different address signature target.

### 🎨 Spanning the Visual Desktop Wallet
To spin up your Fyne graphical interface transaction management panel, run the wallet script inside a separate terminal window:
```powershell
.\LaunchWallet.bat
```
- Paste your secret **Private Key (Hex)** generated during your node onboarding into the key entry box to load your identity coordinates and track your **Current Balance** updates live every 3 seconds via full unspent history sweeps.

### 🌐 Live On-Chain Metric Auditing
Open a standard browser window on your machine to monitor active validated block generation data heights, network velocity, and Jubilee burn void accumulation:
- **Local Dashboard UI:** `http://localhost:8081`
- **Mempool Queue Endpoint:** `http://localhost:8081/req_mempool`
- **Chain Architecture Telemetry:** `http://localhost:8081/req_chain`
- **Isolated Account Statement Sheets:** `http://localhost:8081/audit?address=YOUR_CVN_ADDRESS`

---

**Author:** Nikola Trajanovski (`kalashnikovski`)  
**License:** Open-Source MIT Permissions Framework Core