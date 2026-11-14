# Installation guide

How to run a Covenant Standard (CVN) node, mine, and use the wallet, on Windows and Linux.

> **This is a test network.** The chain running today (`testnet-1`) is for testing. Its coins have no value and the chain will be reset before any real launch. See [README.md](README.md) for the current status.

One program does everything: it is the node, the miner and the text wallet. On Windows there is also a desktop wallet with a window.

**Build from source for now.** The packages on the GitHub Releases page from October 2026 are too old to follow the current chain. Trial packages of the current program are published as pre-releases (names ending in `-rc`); the packages meant for general use will be published with the public test network. Section 10 shows how to check any package you download.

---

## 1. Before you start

- A 64-bit computer running Windows 10 or 11, or Linux.
- About 1 GB of free disk space.
- An internet connection. No port forwarding is needed.
- **Your private key is your wallet.** It is shown once, when the wallet is created, and saved nowhere. Anyone who has it can spend your coins, and without it nobody can, including you. Write it down on paper before you start mining.

---

## 2. Windows

### Install

1. Install Go from https://go.dev/dl/ (the Windows installer, a file ending in `.msi`). Accept the defaults.
2. Download this project: on https://github.com/kalashnikovski/cvn_node click **Code**, then **Download ZIP**.
3. Unpack the ZIP into a folder of your own, for example `C:\cvn_node`.
4. In that folder, double-click **`build_clean.bat`**. It builds two programs:
   - `cvn_miner.exe`, the node, miner and text wallet;
   - `cvn_node.exe`, the desktop wallet.

### Mine

Double-click **`LaunchMiner.bat`**.

The first time, it asks about your mining address:

- If the folder has no address yet, it offers to create a wallet.
- If the folder has an address saved that you have not confirmed on this machine, or that has changed since you confirmed it, it shows that address and asks whether it is yours. **Unless you put it there, choose "Create a new wallet"**, or everything you mine is paid to someone else.

Creating a wallet prints your address and your private key. **Write the private key down**, then press a key to start mining. You are not asked again.

The miner downloads the chain first (a progress bar), then starts mining. The dashboard shows the network height, your balance and your mining speed.

Useful options are added after the file name, in a Command Prompt opened in the folder:

| To do this | Run |
|---|---|
| Mine to a wallet you already have | `LaunchMiner.bat --miner-address CVN_your_address` |
| Use only some processor threads | `LaunchMiner.bat --threads 8` |

To stop the miner, press `Ctrl+C` or close its window.

### The wallet

The wallet runs alongside the miner; leave the miner running.

- **Desktop wallet:** double-click **`LaunchWallet.bat`**. If the window does not open on Windows 10, install Microsoft's WebView2 runtime ("Evergreen") from https://developer.microsoft.com/microsoft-edge/webview2/ . Windows 11 already has it.
- **Text wallet:** in a Command Prompt opened in the folder, run `cvn_miner.exe --wallet`.

---

## 3. Linux

### Install

```bash
sudo apt update && sudo apt install -y git golang-go
```

Any Go from version 1.21 works; it fetches the exact version the project needs the first time you build. If your system's Go is older than that, install a current one from https://go.dev/dl/ instead.

```bash
git clone https://github.com/kalashnikovski/cvn_node
```

```bash
cd cvn_node && go build -o cvn_node .
```

### Create a wallet

```bash
./cvn_node --generate-wallet
```

This prints your address and your private key and saves the address as this machine's mining address. **Write the private key down.**

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

On a server you will want it to start by itself when the server boots and to restart if it stops. One command sets that up on a fresh Ubuntu server, run as root:

```bash
curl -fsSL https://raw.githubusercontent.com/kalashnikovski/cvn_node/main/deploy/install_seed.sh | bash
```

It installs what is needed, builds the program in `/opt/cvn_node`, opens the network's two ports and installs a service. To update the server later: `bash /opt/cvn_node/deploy/update_seed.sh`.

This is the right way to run a small rented server. On one shared processor, mining would earn next to nothing and get in the way of the server's real use to the network, which is being online and reachable. Open the network's two ports in the server's firewall so that other nodes can reach it (8080 and 8081 on `testnet-1`; see section 7 for the others).

---

## 4. Using the wallet

Both wallets do the same things.

| Action | What it does |
|---|---|
| Show a balance | The balance of any address. The text wallet also shows how many separate coins it is held in and, where it applies, when the oldest coin is due to be swept. |
| Send CVN | Sends an amount to an address, with an optional fee |
| Combine my coins | Merges many small coins into a few large ones in your own address |
| Create a new wallet | Makes a new address and private key (text wallet) |

**Sending.** You enter the recipient's address, the amount, an optional fee, and the private key of the wallet you are sending from. In the text wallet on Windows the key is visible on screen as you type or paste it, so make sure nobody is watching and close the window afterwards; on Linux it is hidden. The transfer is signed on your own computer. The key is used for that one transfer and is not saved or sent anywhere. A transfer normally confirms in the next block.

**Why combine?** A miner receives one separate coin for every block won, and one transfer can spend at most 200 coins. A wallet holding hundreds of mining rewards therefore cannot send them all at once. Combining sends them back to your own address as a few large coins. Your balance does not change, apart from any fee you choose to pay.

---

## 5. Mining faster

On networks that mine with RandomX, two things make a large difference: a one-time system setting called large pages, and an optional faster miner. Both are covered in [MINING_GUIDE.md](MINING_GUIDE.md). Neither applies to `testnet-1`.

---

## 6. Updating

Stop the miner first.

**Windows:** download the ZIP again, unpack it over your folder, and run `build_clean.bat`. The download does not contain a `miner_config.json` or a chain database, so yours are kept. If `LaunchMiner.bat` then shows you a mining address and asks whether it is yours, check it before answering: it asks whenever the saved address is not the one you confirmed.

**Linux:**

```bash
cd cvn_node && git pull && go build -o cvn_node .
```

Then start the miner again.

---

## 7. Networks

The program runs on one network at a time.

| Network | How to choose it | What it is |
|---|---|---|
| `testnet-1` | nothing; it is the default | The public test network |
| `cvn_testnet` | add `--network cvn_testnet` | The CVN Testnet: the public test of the launch rules. **Not started yet.** Its start will be announced as a Bitcoin block height. Once it is announced, start your miner on it any time beforehand: it waits, and begins by itself at the same moment as everyone else. |
| `devnet` | add `--network devnet` | The development network for the launch rules. It has no public seeds and may be wiped at any time. |

The same option works for the wallet, for example `./cvn_node --network devnet --wallet` or `LaunchWallet.bat --network devnet`. Each network keeps its own database file and uses its own ports, so they do not interfere.

**Give the option every time, including when you create a wallet.** Every command in this guide that has no `--network` runs on `testnet-1`. Addresses are written differently on the launch networks (they end with 8 check characters), so a wallet for `cvn_testnet` must be created with `--network cvn_testnet`: `LaunchMiner.bat --network cvn_testnet`, or `./cvn_node --network cvn_testnet --generate-wallet`. There is no network called `mainnet` yet, and the program refuses the name.

### On the day a network starts

A launch network starts when one particular Bitcoin block is mined. Its height is announced in advance and written into the program.

1. **Before that day:** update the program, create your wallet for that network, and start the miner on it. It shows how many Bitcoin blocks are left and waits.
2. **At the moment itself:** nothing to do. The program asks several public Bitcoin block explorers for the block at that height, and starts as soon as two of them give the same one. Everyone who is waiting starts within seconds of each other.
3. **If you would rather not depend on those websites,** supply the block's header yourself. It is 160 characters, public the instant the block is mined. With your own Bitcoin node:

   ```bash
   bitcoin-cli getblockheader $(bitcoin-cli getblockhash HEIGHT) false
   ```

   or from any block explorer: find the block, copy its hash, and open `https://mempool.space/api/block/HASH/header`. Then either put the header in a file named `genesis_anchor_NETWORK.txt` beside the program (a waiting node picks it up within a minute), or start with `--genesis-anchor HEADER`. Add `--no-anchor-lookup` if the program should ask no website at all.

   The program checks the header whoever supplies it: it must carry the work of a real, current Bitcoin block. A wrong header cannot cost you coins. It would start your node on a network of its own, and the program warns you if the seed nodes started from a different first block than you did.

### If the seed nodes cannot be reached, or you do not trust them

Seed nodes are only where a new node looks first. They have no say over the rules: your node checks every block itself and follows whichever chain proves the most work, wherever it came from.

- To start from a node you choose, add `--connect HOST` (for example a friend's node, or your own second machine). It is asked alongside the seeds.
- A node remembers the nodes it has met and uses them when the seeds are silent.
- If a seed tells your miner the network is ahead and then does not deliver the blocks, your miner waits two minutes at most.

### What the program contacts

- The seed nodes of the network you chose (listed in [README.md](README.md)), and other nodes it learns of from them.
- Your own router, to ask it to open the network's ports (switch off with the environment variable `CVN_DISABLE_UPNP`).
- On a launch network that has not started yet, and only then: the Bitcoin block explorers mempool.space, blockstream.info, mempool.emzy.de, btcscan.org and mempool.ninja, to ask for one Bitcoin block. `--no-anchor-lookup` switches this off.
- Nothing else. It sends no usage reports and checks for no updates.

Two things to know about privacy. A wallet asks a node for your balance and coins: the node on your own machine if one is running, otherwise a seed node, which then sees your address and the address your computer connects from. And any node shows its own mining address at `http://NODE:8082/status`. Balances and transfers are public on this chain in any case.

---

## 8. Looking at the chain

Any node answers in a web browser. On the machine running it:

- `http://localhost:8081/` — a small block explorer
- `http://localhost:8081/block` — the newest block
- `http://localhost:8081/addresses` — every address with a balance

That is the port on `testnet-1`. On the CVN Testnet it is 18081, and on `devnet` 28081.

---

## 9. If something goes wrong

| What you see | What to do |
|---|---|
| "this machine has no mining address" | Create a wallet (`LaunchMiner.bat`, or `--generate-wallet` on Linux), or start with `--miner-address CVN_...` |
| "Waiting before mining" | Normal. The node is checking that it is up to date, and starts by itself: within two minutes if no other node can be reached, or if another node says it is ahead and then does not deliver. |
| The height stays far below other nodes | Stop the miner, delete the chain database (`cvn_mainnet.db` on `testnet-1`), and start again. It downloads the chain afresh. Your wallet is not in that file. |
| Antivirus removes the program | Mining software is often flagged because malware abuses the same techniques. Build from this source yourself and allow the folder in your antivirus settings. |
| The desktop wallet shows no balance | Check that the miner is running and that both were started on the same network. |

---

## 10. Checking a download, and building the same program yourself

Every release on GitHub comes with two small files, `SHA256SUMS_windows.txt` and `SHA256SUMS_linux.txt`. They list the SHA-256 of each package and of each program inside it. A SHA-256 is a fingerprint: if one byte of a file differs, so does its fingerprint.

### Check that a download arrived whole

Work out the fingerprint of the file you downloaded and compare it with the line for that file in the sums file.

- Windows: `certutil -hashfile cvn_node_windows.zip SHA256`
- Linux: `sha256sum cvn_node_linux_amd64.tar.gz`

### Check that the programs were built from the published source

This is the stronger check. It shows that the packaged programs contain exactly what is in the source and nothing else. You build the programs yourself from the release's tag and compare their fingerprints with the published ones.

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

Each fingerprint should equal the one published for that program. The two Linux programs of `v5.0.0-rc1` have been rebuilt this way on a separate machine and matched exactly. The Windows programs of that release were built with a setting that made them depend on the machine that built them, so they do not match; releases after it are built with the setting above and should.

If a fingerprint does not match, first check the Go version and that you built from the tag with the clone command above. If it still does not match, please report it.

---

To report a problem, open an issue on GitHub with a picture of the miner window and what `http://localhost:8081/block` shows. If the problem could let someone take or create coins, contact the maintainer privately first.
