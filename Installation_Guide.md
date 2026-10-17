
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

🚀 Step 3: Launch on Autopilot

1. Open your C:\cvn_node folder.
2. Locate the file named AutoOnboard_And_Mine.ps1.
3. Right-click the file and choose "Run with PowerShell" (or run inside an Administrator PowerShell terminal window).

🪪 Step 4: The Infallible Dual-Profile Routing Rule

The automation script automatically executes a pre-flight identity check to manage your node credentials with absolute state-safety:
• Scenario A (Brand New Users): If you are joining the ecosystem for the first time, the script will immediately detect that your workspace lacks a configuration profile. It will instantly launch our secure, clock-isolated CSPRNG Elliptic Curve Factory Module. This derives a mathematically unique private key passphrase, constructs your public identity key parameters, and locks your credentials natively onto your hard drive.
• Scenario B (Returning Alpha Testers / Pre-Existing Wallets): If you are an active miner who already owns a wallet or has a configuration sheet sitting inside your directory, the script automatically recognizes your pre-existing profile variables. It bypasses the generation sequence entirely and safely locks your hardware into your verified public identity. Your accumulated block mining balances can never be accidentally overwritten, dropped, or reset.

⚡ Step 5: Global Consensus Sync and Active Validation

Once your wallet profile is securely aligned, the onboarding script automatically fires up your un-throttled core execution path:
• Network Alignment Phase: The terminal will instantly load the Master Telemetry Dashboard at the top of your screen.
	• If your ledger is trailing: The system engages a strict, synchronized multi-seed chunk pipeline. It will display a live ⏳ \[BULK SYNC] loading tracking bar across row 11 while cloning historical blocks from the cross-continental cloud gateways in compressed batch blocks.
	• If your ledger is aligned: The consensus sync gate automatically disengages. Your processor instantly initiates full bare-metal computational work, solving Proof-of-Diligence block puzzles directly to your wallet address and broadcasting new hashes across our global mesh network on total autopilot.
🪪 Finding Your Public Address & Backups
• Inside C:\cvn_node, you will see a text file named cvn_secret_backup.txt. Open it to find your public WALLET ADDRESS (which you use to receive coins) and your secret PRIVATE KEY.
• ⚠️ WARNING: Never share your Private Key with anyone. It controls your funds.

------------------------


🐧 SECTION B: Headless Linux Installation Track


📂 Step 1: Create and Populate Your Workspace Folder

To ensure the network files run securely on your Linux environment, we must provision a clean repository directory.
1. Open your terminal application window.
2. Clone the official repository branches directly into your local home space using git, or download the archived tree layout using curl:bash
cd ~
git clone https://github.com/kalashnikovski/cvn_node
cd cvn_node


📦 Step 2: Install the Framework Runtime Engine

The compilation mechanics require an active, open-source Go compiler framework on your environment's binary path.
1. Update your localized package managers and install the foundational runtime utilities:bash
sudo apt update
sudo apt install golang-go git build-essential -y
Use code with caution.
2. Verify that your runtime profile meets the platform specifications by checking the tool version (should register v1.21 or higher):bash
go version

🚀 Step 3: Initialize Keys and Build Targets

1. Clean out any legacy developer database artifacts or configurations from your directory workspace to prepare for a clean mainnet launch:bash
rm -f cvn_mainnet.db miner_config.json cvn_secret_backup.txt
Use code with caution.
2. Fire your secure, clock-isolated CSPRNG Elliptic Curve Factory Module to derive your unique asymmetric cryptography signature parameters natively on your storage card:bash
go run . --generate-wallet
Use code with caution.
3. Compile your high-performance, un-throttled proof-of-work consensus hashing core executable block target:bash
go build -o cvn_miner .

⚡ Step 4: Core Execution & Multi-Threaded Resource Fencing

1. Launch your compiled validator binary core on autopilot and hook directly into the primary mainnet sync gateways:bash
./cvn_miner --connect 202.137.175.220:8081
Use code with caution.
2. Hardware Micro-Throttling (Optional): By default, the mining thread will process nonces over all available CPU execution frames. To restrict your hardware footprint to a specific thread frame ceiling so your machine stays quiet and cool, prefix your launch sequence with the native resource gate variable (e.g., limiting execution to exactly 4 cores):bash
GOMAXPROCS=4 ./cvn_miner --connect 202.137.175.220:8081

🪪 Finding Your Public Address & Backups
• Inside your ~/cvn_node folder, locate the file named cvn_secret_backup.txt. Run cat cvn_secret_backup.txt to display your public WALLET ADDRESS and secret PRIVATE KEY passphrase.
• ⚠️ WARNING: Never share your Private Key with anyone. It controls your funds.
