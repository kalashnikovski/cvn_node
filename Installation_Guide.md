# Installation guide

How to run a Covenant Standard (CVN) node, mine, and use the wallet, on Windows and Linux.

> **These are test networks.** The CVN Testnet, the public test of the launch rules, has not started yet; its start will be announced as a Bitcoin block height. Coins on any test network have no value and carry over to nothing. See [README.md](README.md) for the current status.

One program does everything: it is the node, the miner and the text wallet. On Windows there is also a desktop wallet with a window.

**Which download to use.** Trial packages of the current program are on the GitHub Releases page, with names ending in `-rc` (for example `v5.0.0-rc12`). Use the newest of those. The packages there from October 2026 (names beginning `v4`) are too old to follow the current chain. The packages meant for general use will be published with the public test network. Section 11 shows how to check any package you download.

**New to this?** On Windows you need three parts of this guide and nothing else: section 1, then "Install" and "Mine" in section 2. If you were invited to the development network, section 7 has the one command to use in place of a double-click.

---

## 1. Before you start

- A 64-bit computer running Windows 10 or 11, or Linux.
- About 1 GB of free disk space.
- An internet connection. No port forwarding is needed.
- **Your private key is your wallet.** It is shown once, when the wallet is created, and saved nowhere. Anyone who has it can spend your coins, and without it nobody can, including you. It is shown both as 64 characters and as **24 recovery words**, which are the same key written another way. Write the 24 words down on paper, in order, before you start mining.

---

## 2. Windows

### Install

There are two ways to get the program, A and B. **You do only one of them.** Almost everyone should do A.

#### Way A (recommended): download the ready-made package

1. Go to https://github.com/kalashnikovski/cvn_node/releases. The page lists the releases one under another, newest at the top. The newest is the first one you see; its name looks like `v5.0.0-rc12`. You do not need to click its name or any link in its text. Those links lead to other things, such as the fast miner, which is optional and comes later.
2. Scroll down to the end of that first release, to the word **Assets**. If no list of files shows under it, click **Assets** to open the list. In the list, click **`cvn_node_windows.zip`**. Your browser downloads it. That one file is all you need; leave the others.
3. Open your Downloads folder. Right-click `cvn_node_windows.zip`, choose **Extract All**, and pick a place of your own, for example `C:\`. You get a folder named `cvn_node`. This is your **CVN folder**; the rest of this guide refers to it by that name.

**That is the whole installation. Skip Way B and go straight to "Mine" below.**

What you now have: the folder contains `cvn_miner.exe` (the node, miner and text wallet), `cvn_node.exe` (the desktop wallet) and two files to start them with, `LaunchMiner.bat` and `LaunchWallet.bat`. Nothing is installed anywhere else on your computer. Checking the download is optional; section 11 shows how.

Windows may show "Windows protected your PC" the first time you start a program from the folder, because the programs are not signed by a company. Click **More info**, then **Run anyway**.

#### Way B (instead of A): build it yourself from the source

**If you did Way A, do not do this.** It is for people who would rather make the program from its source code than download it ready-made. The result is the same program.

1. Install Go from https://go.dev/dl/ (the Windows installer, a file ending in `.msi`). Accept the defaults.
2. On https://github.com/kalashnikovski/cvn_node click **Code**, then **Download ZIP**.
3. Unpack the ZIP into a folder of your own, for example `C:\cvn_node`. This is your CVN folder.
4. In that folder, double-click **`build_clean.bat`**. It builds `cvn_miner.exe` and `cvn_node.exe`.

### Mine

Double-click **`LaunchMiner.bat`**. (If you were invited to the development network, do not double-click: use the command in section 7, "Joining the development network". A double-click starts the CVN Testnet, which has not begun yet.)

The first time, it asks about your mining address:

- If the folder has no address yet, it offers to create a wallet.
- If the folder has an address saved that you have not confirmed on this machine, or that has changed since you confirmed it, it shows that address and asks whether it is yours. **Unless you put it there, choose "Create a new wallet"**, or everything you mine is paid to someone else.

Creating a wallet prints your address and your private key. **Write the private key down**, then press a key to start mining. You are not asked again.

The miner downloads the chain first (a progress bar), then starts mining. The dashboard shows the network height, your balance and your mining speed.

To stop the miner, press `Ctrl+C` or close its window.

### Typing a command

Double-clicking is enough for ordinary use. A few things in this guide need an option added, such as `--threads 8`, and that means typing a command. This is how:

1. Open your CVN folder in File Explorer.
2. Click the address bar at the top (where the folder's path is shown), type `cmd` and press Enter. A black window opens, already in your CVN folder. It is called the Command Prompt.
3. Type the command exactly as this guide shows it and press Enter.

| To do this | Type |
|---|---|
| Mine to a wallet you already have | `LaunchMiner.bat --miner-address CVN_your_address` |
| Use only some processor threads | `LaunchMiner.bat --threads 8` |
| Mine on another network | `LaunchMiner.bat --network devnet` |
| Several at once | `LaunchMiner.bat --network devnet --threads 8` |
| Mine at a pool, not by yourself | `LaunchMiner.bat --pool-mine pool.example.org` (section 5 of [MINING_GUIDE.md](MINING_GUIDE.md)) |

Commands in this guide that begin with `cvn_miner.exe` are typed the same way. Where one is shown beginning with `cvn_miner.exe` and you normally start the miner with `LaunchMiner.bat`, you can use `LaunchMiner.bat` in its place: it passes everything after its name on to the program.

**If you use PowerShell** (the blue or black window Windows 11 opens with "Open in Terminal"), put `.\` in front of the file name: `.\LaunchMiner.bat --threads 8`, `.\cvn_miner.exe --wallet`. PowerShell does not run a program from the current folder without it.

### The wallet

The wallet runs alongside the miner; leave the miner running.

- **Desktop wallet:** double-click **`LaunchWallet.bat`**. If the window does not open on Windows 10, install Microsoft's WebView2 runtime ("Evergreen") from https://developer.microsoft.com/microsoft-edge/webview2/ . Windows 11 already has it.
- **Text wallet:** type `cvn_miner.exe --wallet` (see "Typing a command" above).

---

## 3. Linux

### Install

```bash
sudo apt update && sudo apt install -y git golang-go
```

The project needs one exact version of Go, named in its file `go.mod`. The Go that Ubuntu and Debian install is usually older, and is set up not to fetch another version by itself. The build command below therefore starts with `GOTOOLCHAIN=auto`, which lets it fetch the right one the first time you build (this needs Go 1.21 or newer to begin with). If your system's Go is older than 1.21, install a current one from https://go.dev/dl/ instead.

```bash
git clone https://github.com/kalashnikovski/cvn_node
```

```bash
cd cvn_node && GOTOOLCHAIN=auto go build -o cvn_node .
```

### Create a wallet

```bash
./cvn_node --generate-wallet
```

This prints your address, your private key, and the same key as 24 recovery words, and saves the address as this machine's mining address. **Write the 24 words down on paper.** They are shown this once and saved nowhere.

### Mine

```bash
./cvn_node
```

To mine to a different wallet, or to limit the processor threads used:

```bash
./cvn_node --miner-address CVN_your_address --threads 4
```

To keep the miner running after you close the terminal, start it inside `screen` or `tmux`.

### The wallet

While the miner runs in one terminal, open a second one:

```bash
cd cvn_node && ./cvn_node --wallet
```

The desktop wallet with a window exists on Windows only. On Linux the text wallet does everything it does.

### Running a node that does not mine

A node does not have to mine. Started with `--no-mining` it keeps the whole chain, checks every block and transfer, and passes them on to other nodes, and that is all. It needs no wallet and no mining address, and very little memory.

```bash
./cvn_node --no-mining
```

On a server you will want it to start by itself when the server boots and to restart if it stops. One command sets that up on a fresh Ubuntu server (24.04 or newer), run as root:

```bash
curl -fsSL https://raw.githubusercontent.com/kalashnikovski/cvn_node/main/deploy/install_seed.sh | bash
```

It installs what is needed, builds the program in `/opt/cvn_node`, opens the network's two ports and installs a service. The seed does not run as root: it runs as a user of its own, `cvn-seed`, and keeps its chain in `/var/lib/cvn_seed/NETWORK`. A seed set up by an older version of the script ran as root from `/opt/cvn_node`; running the script again moves its files across. To update the server later: `bash /opt/cvn_node/deploy/update_seed.sh`.

This is the right way to run a small rented server. On one shared processor, mining would earn next to nothing and get in the way of the server's real use to the network, which is being online and reachable. Open the network's two ports in the server's firewall so that other nodes can reach it (18080 and 18081 on the CVN Testnet, 28080 and 28081 on `devnet`).

---

## 4. Using the wallet

Both wallets do the same things.

| Action | What it does |
|---|---|
| Show a balance | The balance of any address. The text wallet also shows how many separate coins it is held in and, where it applies, when the oldest coin reaches its seven-year date and how much has been set aside at such a date. |
| Send CVN | Sends an amount to an address, with an optional fee. If you wish, it gives something to a second address in the same transfer (see "Giving" below) |
| Combine my coins | Merges many small coins into a few large ones in your own address |
| Renew my coins | Gives coins that are within a year of their seven-year date a new seven-year count, and takes back anything set aside on a coin at such a date, by sending them back to your own address. No fee is needed. If no coin needs it, it says how far off the earliest date is and offers to renew them all. If a renewal you sent with no fee has not gone through after three days, it says so and offers to send it again with a small fee |
| Create a new wallet | Makes a new address and private key, shown also as 24 recovery words (text wallet) |
| Recovery words and saved key | Shows the 24 words of a key, checks words you have written down, and saves or removes a key locked with a password (text wallet) |

**Sending.** You enter the recipient's address, the amount, an optional fee, and the private key of the wallet you are sending from. In the text wallet on Windows the key is visible on screen as you type or paste it, so make sure nobody is watching and close the window afterwards; on Linux it is hidden. The transfer is signed on your own computer. The key is used for that one transfer and is not saved or sent anywhere. A transfer normally confirms in the next block.

**Recovery words.** A private key is 64 characters, and one copied wrongly by hand gives no warning until the coins cannot be spent. The 24 recovery words are the same key written as ordinary English words, with a check built in: a misspelt, missing or misplaced word is refused at once, with a message saying what is wrong. Wherever a wallet asks for the private key you can give the 24 words instead, with spaces between them; capitals do not matter. The words are the key itself and nothing is derived from them, so a wallet made before this existed has its words too: "Recovery words and saved key" in the text wallet shows them. The word list is the one Bitcoin wallets use, so a card or steel plate made for recovery words fits. They are for CVN only: typing them into another coin's wallet does not open this one.

**A saved key.** So that you need not paste the key for every transfer, the text wallet can save it on this computer in the file `wallet_key.json`, locked with a password of your choosing (at least 8 characters; longer is better). After that, wherever either wallet asks for the private key you can type the password. The file cannot be opened without the password, but it is a convenience and not a backup: if the computer is lost or the password forgotten, only the words on paper bring the coins back. Anyone who gets both the file and the password has the coins, so never send the file to anyone, and remove it (same menu) from a computer you are giving away.

**The seven-year date.** A coin that has not been spent for seven years has a fifth of its value set aside. Nothing has left your wallet: the fifth stays on the coin, the balance shown still includes it, and it is yours for seven more years. Spending the coin in that time, to anyone or to your own address, gives you all of it. If the coin is still unspent seven years after that, the fifth goes to the vault, which pays it out to miners, and a new fifth is set aside. So one "Renew my coins" every seven years keeps everything, and if you miss a date you have seven years to renew and take the fifth back. "Show a balance" in the text wallet says how much is set aside and by which block to renew. Renewing and combining are sent with no fee unless you choose one; in the window wallet, tick the box under the two buttons if you want to pay the fee you typed. A transfer with no fee waits its turn when blocks are full, so do not leave a renewal to the last days before a date: if a date is less than two days away the wallet says so, and a small fee then makes sure the renewal is mined in time.

**Fees, and transfers without one.** A fee is yours to choose, and none is needed: a fifth of every block is closed by rule to transfers that pay a fee, so a transfer that pays nothing has room even when blocks are full. A fee buys a place among the paying transfers, highest fee first. Renewing and combining are sent with no fee unless you choose one. No rule can make a miner take a free transfer, though, so on a crowded network one may wait, and nodes forget a transfer that has waited two hours. Nothing is lost when that happens: the coins are as they were. The wallet keeps a note of each free renewal it sends, in the file `wallet_renewals.json` in the folder it is started in, where a saved key is also kept (an address and the names of coins, nothing secret). If three days later those coins are still unspent, the wallet tells you when you look at the balance, and "Renew my coins" offers to send the renewal again with a fee of 0.001 CVN for each transfer, or another fee you name. It shows how many transfers that is and what they cost together, and sends nothing until you say yes. It goes on reminding you for a month. If the node the wallet asks is still starting up or catching up, it may not list your coins; the wallet then says nothing, and reminds you the next time.

**Giving.** When you send CVN you may give something to a second address in the same transfer: a church, a charity, a friend, any address you choose. In the text wallet the question comes after you have typed your key: answer it with that address, and then with what to give, either an amount in CVN or a share of the amount you are sending, such as `10%`. In the window wallet, fill in the two fields under the fee. The gift is in addition to the amount, so the person you are paying receives all of it. Before anything is sent, both wallets show the gift as a figure in CVN (a share worked out) and what leaves your wallet in all, and send only if you agree. Giving is yours to choose each time. The wallet suggests no address and no amount and remembers neither, the rules of the network know nothing of it (it is an ordinary transfer to two addresses), and with the gift left empty a transfer is exactly what it always was. If you fill in only the address or only the amount, nothing is sent. Take care what you put where the address goes: if it is not an address the wallet refuses it and does not show it back.

**Why combine?** A miner receives one separate coin for every block won, and one transfer can spend at most 200 coins. A wallet holding hundreds of mining rewards therefore cannot send them all at once. Combining sends them back to your own address as a few large coins. Your balance does not change, apart from any fee you choose to pay.

**After combining or sending, wait for the next block before sending again.** The coins involved are tied up until a block confirms the transfer, usually within a minute or two. A transfer attempted before that is refused with "not enough coins are ready to spend". Nothing is lost: the balance shown is still yours, and the same transfer goes through after the next block.

---

## 5. Mining faster

Two things make a large difference, and both are covered step by step in [MINING_GUIDE.md](MINING_GUIDE.md):

- **Large pages**, a one-time system setting: about a quarter faster.
- **The fast miner**, an optional separate program: about a tenth faster again. You download one folder and put it inside your CVN folder; the CVN program then uses it by itself.

A third, smaller gain is there for those who want it: with the fast miner, a processor tuning adds a few percent more (about 8% on the project's test machine). It needs the miner started as administrator and lowers some of the system's protections, so read section 3 of the mining guide before deciding.

---

## 6. Updating

Stop the miner first.

**Windows:** download the newest `cvn_node_windows.zip` and unpack it over your CVN folder, replacing the files it asks about. (If you build from the source: download the source ZIP again, unpack it over your folder, and run `build_clean.bat`.) The download does not contain a `miner_config.json` or a chain database, so yours are kept. If `LaunchMiner.bat` then shows you a mining address and asks whether it is yours, check it before answering: it asks whenever the saved address is not the one you confirmed.

**Linux:**

```bash
cd cvn_node && git pull && GOTOOLCHAIN=auto go build -o cvn_node .
```

Then start the miner again.

---

## 7. Networks

The program runs on one network at a time.

| Network | How to choose it | What it is |
|---|---|---|
| `cvn_testnet` | nothing; it is the default | The CVN Testnet: the public test of the launch rules. **Not started yet.** Its start will be announced as a Bitcoin block height. Once it is announced, start your miner on it any time beforehand: it waits, and begins by itself at the same moment as everyone else. |
| `devnet` | add `--network devnet` | The development network for the launch rules. It has no public seeds and may be wiped at any time. |

The same option works for the wallet, for example `./cvn_node --network devnet --wallet` or `LaunchWallet.bat --network devnet`. Each network keeps its own database file and uses its own ports, so they do not interfere.

**For any network other than the CVN Testnet, give the option every time, including when you create a wallet.** Every command in this guide that has no `--network` runs on the CVN Testnet, and so does a double-click on `LaunchMiner.bat` or `LaunchWallet.bat`. Each network has its own wallet file and its own chain. The program refuses a network name it does not know. In particular there is no network called `mainnet` or `testnet`: the CVN Testnet is `cvn_testnet`.

### Joining the development network

The development network (`devnet`) is run by invitation. Whoever runs it gives you two things:

- **the Bitcoin block it started from**, a number such as `976283`. Below it is written `HEIGHT`;
- **the addresses of its seeds**, the machines a new node asks for the chain. Below they are written `SEEDS`. Type them as numbers and dots with commas between them and nothing else: no spaces, no port numbers, no `http://`. For example `203.0.113.5,203.0.113.9`.

**Windows.** Open your CVN folder in File Explorer, right-click an empty part of it and choose **Open in Terminal**. A window opens, already in your CVN folder. Paste this one line, with your own two values in it, and press Enter:

```
$env:CVN_DEVNET_ANCHOR_HEIGHT="HEIGHT"; .\LaunchMiner.bat --network devnet --seeds SEEDS
```

If your window is the Command Prompt instead (you opened it by typing `cmd`), that line does not work there. Type these two lines, pressing Enter after each:

```
set CVN_DEVNET_ANCHOR_HEIGHT=HEIGHT
LaunchMiner.bat --network devnet --seeds SEEDS
```

**Linux:**

```bash
CVN_DEVNET_ANCHOR_HEIGHT=HEIGHT ./cvn_node --network devnet --seeds SEEDS
```

Use the same line every time you start the miner. The height is forgotten when the window is closed.

**How to tell it worked.** After the wallet questions, the window shows `Comparing our chain with ...`, then a progress bar counting up to the network's height, then the dashboard with a network height in the thousands and `UP TO DATE`. If it shows `Starting at block #0` and a height that begins at 1, you are not on the network: stop it and see section 9.

The program does not start a chain of its own when it was given seeds. If none of them answers it waits and says so, and if they are on a different chain it stops and says so. Both messages are explained in section 9. (Whoever starts a network's very first blocks adds `--start-alone`.)

### On the day a network starts

A launch network starts when one particular Bitcoin block is mined. Its height is announced in advance and written into the program.

**Checklist, to finish before that day** (`NETWORK` is the network's name, for example `cvn_testnet`):

1. **Get the release that names the height.** Download its package, or build from its tag, and check it against the published sums (section 11).
2. **Create a wallet for that network** and write its private key down on paper:
   - Windows: `LaunchMiner.bat --network NETWORK` and choose "Create a new wallet". For the CVN Testnet a double-click on `LaunchMiner.bat` does the same.
   - Linux: `./cvn_node --network NETWORK --generate-wallet`
3. **Optional, for more speed:** switch on large pages and add the fast miner. Both are in [MINING_GUIDE.md](MINING_GUIDE.md), and both can be done later without losing anything.
4. **Start the miner** on that network and leave it running:
   - Windows: `LaunchMiner.bat --network NETWORK`, or for the CVN Testnet just double-click `LaunchMiner.bat`.
   - Linux: `./cvn_node --network NETWORK`

   It shows how many Bitcoin blocks are left and waits.

**At the moment itself** there is nothing to do. The program follows Bitcoin's own block headers up to that height, checking every one as it goes, and starts as soon as the block at that height appears. It takes nobody's word for which block that is: it has counted. Everyone who is waiting starts within seconds of each other.

**If you would rather the program contacted nobody about it,** supply the block's header yourself. It is 160 characters, public the instant the block is mined. With your own Bitcoin node:

```bash
bitcoin-cli getblockheader $(bitcoin-cli getblockhash HEIGHT) false
```

or from any block explorer: find the block, copy its hash, and open `https://mempool.space/api/block/HASH/header`. Then either put the header in a file named `genesis_anchor_NETWORK.txt` beside the program (a waiting node picks it up within a minute), or start with `--genesis-anchor HEADER`. Add `--no-anchor-lookup` if the program should ask nobody at all.

The program checks a header you supply: it must carry the work of a real, current Bitcoin block, dated after the start was announced. It does not count up to a header supplied by hand, so getting the right block is then yours to see to. A wrong header cannot cost you coins. It would start your node on a network of its own, and the program warns you if that has happened.

**What being first is worth.** A waiting node starts within about ten to twenty seconds of the Bitcoin block appearing. Someone who supplies the header from their own Bitcoin node starts those seconds earlier. A block takes about 45 seconds for the whole network at the starting difficulty, and for the first 20 blocks that difficulty cannot fall, so a lead of that size is worth a fraction of one block's chance. Nobody has to wait for the clock at the start either: the first block is dated two hours before its Bitcoin block's own time, because Bitcoin allows a block's time to run up to two hours ahead. Those two hours of earlier dates buy nothing, because the difficulty rule counts any date before the Bitcoin block's own time as that time.

**If Bitcoin replaces that block.** About one Bitcoin block in several hundred is replaced by another moments after it appears. Nodes could then have started from two different blocks and be on two networks. The rule is fixed in advance and is nobody's to decide: **the network is the one that begins from the Bitcoin block still in Bitcoin's chain six blocks later. Blocks mined on the other are void.** For the first hour or so the program keeps following Bitcoin's headers to see whether its Bitcoin block is still the one at that height, and it also compares its first block with the seed nodes'. If either check fails it says so, repeatedly. To move over to the right network: stop the program, delete `genesis_anchor_NETWORK.txt` and that network's chain database, and start again. Your wallet is not in those files.

### If the seed nodes cannot be reached, or you do not trust them

Seed nodes are only where a new node looks first. They have no say over the rules: your node checks every block itself and follows whichever chain proves the most work, wherever it came from.

- To start from nodes you choose, add `--connect HOST` or `--connect HOST,HOST,...` (for example a friend's node, or your own second machine). They are asked alongside the seeds.
- A node remembers the nodes it has met and uses them when the seeds are silent.
- A node will not normally replace more than 100 of its own blocks. For a new node that limit does not apply to anything it has taken on the seeds' word: until some node that is not a seed has shown it holds the same chain, a newcomer follows whichever chain proves the most work, however far back the two part, and stopping and starting the program does not change that. So the seeds alone can never hold a newcomer on a chain of their own.
- If a seed tells your miner the network is ahead and then does not deliver the blocks, your miner waits two minutes at most.

### What the program contacts

- The seed nodes of the network you chose (listed in [README.md](README.md)), and other nodes it learns of from them.
- Your own router, to ask it to open the network's ports (switch off with the environment variable `CVN_DISABLE_UPNP`).
- On a launch network that has not started yet, and for about an hour after it starts: a few computers on the Bitcoin network, found through the Bitcoin project's published look-up names, to fetch Bitcoin block headers and nothing else. If none can be reached it asks the Bitcoin block explorers mempool.space, blockstream.info, mempool.emzy.de, btcscan.org and mempool.ninja for the same headers. `--no-anchor-lookup` switches all of this off.
- Nothing else. It sends no usage reports and checks for no updates.

Two things to know about privacy. A wallet asks a node for your balance and coins: the node on your own machine if one is running, otherwise a seed node, which then sees your address and the address your computer connects from. And any node shows its own mining address at `http://NODE:18082/status` (28082 on `devnet`). Balances and transfers are public on this chain in any case.

---

## 8. Looking at the chain

Any node answers in a web browser. On the machine running it:

- `http://localhost:18081/` — a small block explorer
- `http://localhost:18081/block` — the newest block
- `http://localhost:18081/addresses` — every address with a balance

That is the port on the CVN Testnet. On `devnet` it is 28081.

---

## 9. If something goes wrong

| What you see | What to do |
|---|---|
| "this machine has no mining address" | Create a wallet (`LaunchMiner.bat`, or `--generate-wallet` on Linux), or start with `--miner-address CVN_...` |
| "These addresses given with --seeds could not be read" | The list of seeds has something extra in it. Give the addresses alone with commas between them: no spaces, no port numbers (`:28081`), no `http://`. |
| "NOT MINING YET: none of the other machines answered" | The program was given seeds and none replied, so it waits and does not start a chain of its own. Check the addresses are the ones you were given, that the computer is online, and that its firewall or antivirus allows the program to connect. It carries on by itself as soon as one answers. |
| "The other machines are on a DIFFERENT CHAIN" | The seeds answered, and their first block is not this program's. On `devnet` the usual cause is that the Bitcoin block height was not set in this window (section 7, "Joining the development network"). Otherwise check the network name and that you have the release the network runs. |
| Your height started at 1 while the network is in the thousands | The node began a chain of its own (releases up to `v5.0.0-rc10` did this silently when no seed could be reached). Stop it, delete `cvn_devnet.db` in your CVN folder, and start again with the command from section 7. Coins mined on that chain exist nowhere else. |
| "Waiting before mining" | Normal. The node is checking that it is up to date, and starts by itself: within two minutes if no other node can be reached, or if another node says it is ahead and then does not deliver. |
| The height stays far below other nodes | Stop the miner, delete the chain database (`cvn_testnet.db` on the CVN Testnet, `cvn_devnet.db` on `devnet`), and start again. It downloads the chain afresh. Your wallet is not in that file. |
| Antivirus removes the program | Mining software is often flagged because malware abuses the same techniques. Build from this source yourself and allow the folder in your antivirus settings. |
| "miner_config.json is not written correctly" | The file was edited by hand and has a mistake, most often a missing comma. The message names the line. Fix it, or delete the file and start `LaunchMiner.bat` with `--miner-address CVN_your_address`. |
| "not enough coins are ready to spend" just after combining or sending | Wait for the next block and try again. See section 4. |
| The fast miner does not start | See "How to tell it is running" in [MINING_GUIDE.md](MINING_GUIDE.md). |
| "FAILED TO APPLY MSR MOD, HASHRATE WILL BE LOW" | Not a fault. The fast miner is running at its ordinary speed without an optional processor tuning. See section 3 of [MINING_GUIDE.md](MINING_GUIDE.md). |
| The desktop wallet shows no balance | Check that the miner is running and that both were started on the same network. |

---

## 10. Backups of the chain

The chain is one file in the CVN folder (`cvn_testnet.db`, or `cvn_devnet.db` on the development network). A node that loses it downloads the chain again from the other nodes, so on a network with other nodes a backup only saves time. It matters when there are no others to ask: a network of one machine, or a fault that takes every node at once.

**The node makes its own backups.** Two minutes after it starts, and then once a day, it saves a copy of the chain in the folder `backups`, and says so in its window or log. It keeps the newest seven and removes older ones. Each copy is checked before it is kept: every block is there and follows from the one before, each block is what its identifier says, and the coins add up to exactly what has been issued. A copy that fails is thrown away and the fault is reported.

| Option | What it does |
|---|---|
| `--backup-hours 6` | A backup every 6 hours (the default is 24) |
| `--backup-keep 30` | Keep the newest 30 (the default is 7) |
| `--no-backups` | Make none |

**Do not back up by copying the chain file while the node runs.** The node may be half-way through writing it, and such a copy can open with parts missing. It looks like a backup until the day it is needed. Copy the files in `backups` instead: those are whole, and safe to copy at any time, to another disk or another machine.

**Checking a backup.** This reads the file and changes nothing. It can run while the node does:

```
cvn_miner.exe --network devnet --check-backup backups\cvn_devnet_block00001468_20261119-031200.db
```

On Linux the program is `./cvn_node`. On a development network started from a Bitcoin block, set `CVN_DEVNET_ANCHOR_HEIGHT` as you do for the node, so that the backup's first block is compared with the network's.

**Checking a backup in full.** The check above reads the file and adds it up. It does not work the coins out again from the blocks. This one does:

```
cvn_miner.exe --network devnet --check-backup-fully backups\cvn_devnet_block00001468_20261119-031200.db
```

It starts from the first block and gives every block in the file the same checks a block from another node gets: every rule, every signature, every proof of work. Then it compares what those blocks make with what the file holds, coin by coin. A file in which a coin was altered, added or removed fails, even if the totals still add up.

It takes as long as the chain is long: minutes on a short chain, hours on a long one. It can run while the node does, and it uses the processor while it runs. It writes a scratch file beside the chain file and removes it at the end, so it needs free disk space about the size of the chain.

**Restoring a backup.** Stop the node first. Then:

```
cvn_miner.exe --network devnet --restore-backup backups\cvn_devnet_block00001468_20261119-031200.db
```

The backup is first checked in full, as `--check-backup-fully` does, so on a long chain this takes a while. Only if it passes is it put in place of the chain file. The file it replaces is not deleted: it is kept beside it with `.replaced-` and the time in its name. Start the node as usual afterwards; it fetches any later blocks from the network.

**What a backup is and is not.** It is a copy of public data, the same chain every node holds. It contains no private key and is no substitute for writing your recovery words down. Restoring an old backup onto the only machine of a network takes that network back to the backup's block: everything mined after it is gone.

## 11. Checking a download, and building the same program yourself

Every release on GitHub comes with two small files, `SHA256SUMS_windows.txt` and `SHA256SUMS_linux.txt`. They list the SHA-256 of each package and of each program inside it. A SHA-256 is a fingerprint: if one byte of a file differs, so does its fingerprint.

### Check that a download arrived whole

Work out the fingerprint of the file you downloaded and compare it with the line for that file in the sums file.

- Windows: `certutil -hashfile cvn_node_windows.zip SHA256`
- Linux: `sha256sum cvn_node_linux_amd64.tar.gz`

### Check that a download was built by this project

From release candidate 10 on, GitHub signs a statement for every package and program in a release, saying that it was built by this project's own build recipe from this repository at that tag. With GitHub's command-line tool (`gh`) installed:

```
gh attestation verify cvn_node_windows.zip --repo kalashnikovski/cvn_node
```

It works the same for the Linux packages and for a program taken out of a package. A file that did not come from the project's build is refused. This relies on GitHub; the check below relies on nobody.

### Check that the programs were built from the published source

This is the strongest check. It shows that the packaged programs contain exactly what is in the source and nothing else. You build the programs yourself from the release's tag and compare their fingerprints with the published ones.

You need git and the Go version named in the file `go.mod` (the same version: a different one gives a different program). Replace `TAG` with the release's name, for example `v5.0.0-rc1`.

```bash
git clone -c core.autocrlf=false --branch TAG https://github.com/kalashnikovski/cvn_node
cd cvn_node
```

The option `-c core.autocrlf=false` matters on Windows. Without it git rewrites the line endings of the text files, some of which are built into the program, and the result differs.

Then build, on Linux, macOS or Windows alike. These lines are for a Linux or macOS terminal, or Git Bash on Windows:

```bash
export CGO_ENABLED=0
GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o cvn_node_amd64 .
GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o cvn_node_arm64 .
GOOS=windows GOARCH=amd64 go build -trimpath -tags server -ldflags="-s -w" -o cvn_miner.exe .
GOOS=windows GOARCH=amd64 go build -trimpath -tags "desktop,production" -ldflags="-s -w -H windowsgui" -o cvn_node.exe .
sha256sum cvn_node_amd64 cvn_node_arm64 cvn_miner.exe cvn_node.exe
```

Each fingerprint should equal the one published for that program. All four programs of `v5.0.0-rc3` have been rebuilt this way on a separate machine and matched exactly. Of the two trial releases before it, the Linux programs match and the Windows programs do not: `rc1` was built with a setting that tied the result to the build machine, and `rc2` from files whose line endings git had rewritten (its Windows programs do match if you clone with `core.autocrlf=true` instead).

**The wallet screens.** The screens of the desktop wallet are kept in the repository already built (`frontend/dist`), so that building the program does not need Node.js. They are compiled into the program, so they matter. Every change to the repository and every release is checked automatically: the screens are built again from their source (`frontend/src`) with the versions fixed in `frontend/package-lock.json`, and the check fails unless the result is byte for byte what the repository holds. To do the same yourself, with Node.js 26:

```bash
cd frontend
npm ci --legacy-peer-deps
npm run build
git diff --exit-code -- dist
```

No output from the last command means they are identical.

**Seed servers.** The script that sets up a seed server builds from the newest source unless told otherwise. To run exactly one release, name its tag: `CVN_VERSION=v5.0.0 bash install_seed.sh NETWORK`.

If a fingerprint does not match, first check the Go version and that you built from the tag with the clone command above. If it still does not match, please report it.

---

To report a problem, open an issue on GitHub with a picture of the miner window and what `http://localhost:18081/block` shows. If the problem could let someone take or create coins, report it privately first: on the repository's Security tab on GitHub, choose "Report a vulnerability".
