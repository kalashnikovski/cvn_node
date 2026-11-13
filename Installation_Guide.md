
🏛️ Covenant Standard (CVN) Protocol


Layer-1 Core Network Onboarding Specification

Established: October 13, 2026
The Covenant Standard (CVN) Layer-1 network protocol was birthed under a strict, uncorrupted Fair Launch Mandate. The network coordinator and the Assembly of Stewards hereby declare that the network adheres to the following immutable economic pillars:
1. Absolute Zero Pre-Mine: The Genesis Block (Block #0) contains exactly 0.00 CVN in circulating supply. There are no developer allocations, no founder reserve pools, and no venture capital token buckets baked into the code layout.
2. Organic Distribution Only: Every single CVN token in existence must be earned openly and transparently. New tokens only enter circulation when an honest node processor provides computational work to solve a Proof-of-Diligence (PoD) block reward block (+50 CVN).
3. No Private Pre-Sales or ICOs: No cheap tokens were sold to private insiders or early investors before the mainnet went live. Every participant starts their wallet balance at absolute zero on their very first boot.
4. Democratized Hardware Access: The network's difficulty constants are tuned exclusively for consumer-grade desktop processors (CPUs). This prevents centralized ASIC mining factories or millionaire staking pools from monopolizing the ledger, keeping validation accessible to ordinary household hardware slots forever.
Covenant Standard is an open-source, public-validated, decentralized ecosystem owned entirely by the community from block number one.

💎 Covenant Standard (CVN) Onboarding Guide


Zero-Configuration Household Hardware Setup

Covenant Standard is an open-source Layer-1 network optimized for standard consumer processors (CPUs). By following this guide, you will initialize a sovereign validation rig on your computer to secure the global ledger and collect transaction fees.

🪟 SECTION A: Microsoft Windows Installation Track


📂 Step 1: Create and Populate Your Workspace Folder

To ensure the network files run securely on your computer, we must create a dedicated home directory and populate it with the core software suite.
1. Open your internet browser and visit the official repository release page: `https://github.com/kalashnikovski/cvn_node`
2. Click the green Code button near the top right of the file grid, then click Download ZIP.
3. Open your File Explorer (the folder icon on your taskbar).
4. Click on This PC in the left sidebar, then double-click your primary hard drive (usually labeled Local Disk (C:)).
5. Right-click inside any empty white space, hover over New, select Folder, and name it exactly: cvn_node
6. Go to your Downloads folder, double-click the cvn_node-main.zip file you just grabbed from GitHub, and drag all the contents directly out of the zip file and drop them straight inside your newly created C:\cvn_node directory folder.

📦 Step 2: Install the Framework Runtime Engine

The network requires a foundational, open-source programming runtime environment to run natively on Windows hardware slots.
1. Open your internet browser and visit the official download page: https://go.dev/dl/
2. Locate the Featured Downloads section and click the link for Microsoft Windows (it will end in .msi).
3. Once the installer finishes downloading, double-click the file to run it.
4. Click Next on all prompts, accept the standard license terms, and click Install.
5. When the installation completes, click Finish. Your operating system is now fully upgraded.

🚀 Step 3: Build and Launch Your Miner
1. Run **`build_clean.bat`** inside `C:\cvn_node` to compile `cvn_miner.exe` and `cvn_node.exe` 
2. Right-click **`LaunchMiner.bat`** and run as administrator, or double-click **`LaunchWallet.bat`** for the desktop UI.

**About the desktop wallet (`LaunchWallet.bat`):**
* It can run at the same time as the miner. It shows the balance of the address in `miner_config.json`.
* To send CVN, paste the private key of the sending wallet into the form. The transfer is signed on your computer; the key is not saved or sent anywhere.
* It needs Microsoft's WebView2 runtime. Windows 11 includes it. On Windows 10, install the "Evergreen" runtime from `https://developer.microsoft.com/microsoft-edge/webview2/` if the wallet window does not open.
* You do **not** need Node.js. `setup_wallet_deps.bat` is only for developers who want to change the wallet's screens.

🪪 Step 4: Profile Routing & Consensus Sync
* **New Users:** Missing configuration triggers automatic wallet generation (`--generate-wallet`) via CSPRNG, displaying your private key directly in the terminal.
* **Returning Miners:** Existing configurations lock onto your saved public address in `miner_config.json` 
* **Sync & Mining:** The node handles bulk chain sync (`⏳ [BULK SYNC]`) before starting active CPU Proof-of-Diligence mining.

⚠️ **Security Note:** Store your Secret Private Key offline immediately upon generation; plaintext keys are no longer written to `cvn_secret_backup.txt`
---


🐧 SECTION B: Headless Linux Installation Track


📂 Step 1: Create and Populate Your Workspace Folder

To ensure the network files run securely on your Linux environment, we must provision a clean repository directory.
1. Open your terminal application window.
2. Clone the official repository branches directly into your local home space using git, or download the archived tree layout using curl:
cd ~
git clone https://github.com/kalashnikovski/cvn_node

cd cvn_node


📦 Step 2: Install the Framework Runtime Engine

The compilation mechanics require an active, open-source Go compiler framework on your environment's binary path.
1. Update your localized package managers and install the foundational runtime utilities:
sudo apt update
sudo apt install golang-go git build-essential -y

2. Verify that your runtime profile meets the platform specifications by checking the tool version (should register v1.21 or higher):
go version

🚀 Step 3: Initialize Keys and Build Targets

1. Clean out any legacy developer database artifacts or configurations from your directory workspace to prepare for a clean mainnet launch:
rm -f cvn_mainnet.db miner_config.json cvn_secret_backup.txt

2. Fire your secure, clock-isolated CSPRNG Elliptic Curve Factory Module to derive your unique asymmetric cryptography signature parameters natively on your storage card:
go run . --generate-wallet

3. Compile your high-performance, un-throttled proof-of-work consensus hashing core executable block target:
go build -o cvn_node .

4. Generate your wallet address and private keys:
./cvn_node --generate-wallet 

⚡ Step 4: Core Execution & Multi-Threaded Resource Fencing

1. Launch your compiled validator binary core on autopilot and hook directly into the primary mainnet sync gateways: bash
./cvn_node --miner-address YOUR_CVN_ADDRESS --connect 202.137.175.220:8081

2. Hardware Micro-Throttling (Optional): By default, the mining thread will process nonces over all available CPU execution frames. To restrict your hardware footprint to a specific thread frame ceiling so your machine stays quiet and cool, prefix your launch sequence with the native resource gate variable (e.g., limiting execution to exactly 4 cores): bash
GOMAXPROCS=4 ./cvn_node --miner-address YOUR_CVN_ADDRESS --connect 202.137.175.220:8081

⚠️ **Security Note:** Store your Secret Private Key offline immediately upon generation; plaintext keys are no longer written to `cvn_secret_backup.txt`
