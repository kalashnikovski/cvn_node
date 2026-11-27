# Covenant Standard (CVN)

Covenant Standard is a proof-of-work blockchain written from scratch in Go. It is built for fair, open mining on ordinary home computers: no pre-mine, no founder allocation, no investor sale.

Website: https://covenantstandard.online. The seed nodes, what each holds, and their block explorers: https://covenantstandard.online/explorer/.

> **Status: before the public test.** The CVN Testnet, the public test of the launch rules, has not started yet. Its start will be announced as a Bitcoin block height, and everyone begins from zero at that block. Until then the rules are being rehearsed on a development network (`devnet`), which is joined by invitation ([Installation_Guide.md](Installation_Guide.md), section 7). The first test network (`testnet-1`) has been retired. The development network was started afresh on 27 November 2026, and the coins on it from before are gone. Coins on all of these have no value. Mine to help test, not to accumulate.

## What works today

- Mining with 45-second blocks; difficulty adjusts on every block.
- Sending CVN between wallets. Every transfer is signed by its owner and checked by every node.
- A desktop wallet on Windows and a text wallet on Linux and Windows. A private key is also given as 24 recovery words, and either form opens the wallet.
- Nodes find each other, sync from scratch, and recover from forks without help. A first download uses several processor cores and comes from the seed that answers soonest.
- Blocks travel over the connections nodes already have open, and a large block is announced first and sent only to nodes that ask for it.
- A new node joins the network that is there. If the seeds it was given do not answer it waits, and if they are on another chain it stops and says so; it does not start a chain of its own.
- No port forwarding needed: a node behind a home router still receives blocks and can mine.
- A node saves a checked copy of its chain once a day and keeps the newest seven. The daily check is a quick one. Putting a copy back first works the whole chain out again from its blocks, coin by coin.
- Downloads anyone can check: every release lists its SHA-256 sums, is signed by GitHub as built from its tag, and can be rebuilt to the same bytes.

See [ROADMAP.md](ROADMAP.md) for what is finished and what is still being built. The rules planned for launch are set out in [WHITEPAPER.md](WHITEPAPER.md).

## Quick start

Full step-by-step instructions are in [Installation_Guide.md](Installation_Guide.md). In short:

### Windows

1. Download `cvn_node_windows.zip` from the newest [release](https://github.com/kalashnikovski/cvn_node/releases) and unpack it.
2. In the unpacked folder, double-click `LaunchMiner.bat` to create a wallet and start mining. **Write the 24 recovery words down when they are shown.** If it shows a mining address that is not yours, choose "Create a new wallet".
3. Double-click `LaunchWallet.bat` for the desktop wallet.

To build from the source instead: install [Go](https://go.dev/dl/), download this repository, and run `build_clean.bat`. Every step is spelled out in [Installation_Guide.md](Installation_Guide.md).

### Linux

```bash
git clone https://github.com/kalashnikovski/cvn_node
cd cvn_node
GOTOOLCHAIN=auto go build -o cvn_node .
./cvn_node --generate-wallet
./cvn_node
```

Each release also has the program ready built, as `cvn_node_linux_amd64.tar.gz` and `cvn_node_linux_arm64.tar.gz`.

`--generate-wallet` prints your address, your private key, and the same key as 24 recovery words, once. **Write the 24 words down on paper.** The key and the words are saved nowhere, and coins mined to an address cannot be spent without them. The address alone is saved, as this machine's mining address.

To mine to a different address, or to limit how many processor threads the miner uses:

```bash
./cvn_node --miner-address YOUR_CVN_ADDRESS --threads 4
```

A one-time system setting (large pages) makes mining about a quarter faster, and an optional separate fast miner adds about a tenth more: you unpack one folder into your CVN folder and the program uses it by itself. With the fast miner, an optional processor tuning adds a few percent more for those willing to run it as administrator. All three are in [MINING_GUIDE.md](MINING_GUIDE.md).

To mine at a pool and not by yourself, see section 5 of the same guide. A pool is this program with one more option, and anyone may run one: [POOL_GUIDE.md](POOL_GUIDE.md).

### The wallet

While the miner runs in one terminal, open another:

```bash
./cvn_node --wallet
```

On Windows: `cvn_miner.exe --wallet`, or `LaunchWallet.bat` for the desktop version.

A miner receives a separate coin for every block won, and one transfer can spend at most 200 coins. **Combine my coins**, in both wallets, merges them into a few large coins in your own address. **Renew my coins** gives coins nearing their seven-year date a new count in one step, and takes back a fifth that was set aside on a coin at such a date. Neither needs a fee.

**These commands run on the CVN Testnet** (`cvn_testnet`). Until its start has been announced you can create a wallet and open it, but the miner says the network is not scheduled yet and stops. What to do before and on the day it starts is in [Installation_Guide.md](Installation_Guide.md), section 7. For another network add `--network NAME` to every command, including the one that creates the wallet, for example `--network devnet`.

## Network

| | Port | Purpose |
|---|---|---|
| Node to node | 18080 | Blocks and transfers |
| Sync and wallet | 18081 | Block download, balances, sending transfers |
| Status | 18082 | Height and tip of this node |
| Mining work | 18083 | Work for the fast miner on this same computer |

These are the CVN Testnet's ports. On `devnet` they are 28080, 28081, 28082 and 28083.

Seed nodes (all run by the project's maintainer; they are where a new node looks first, they have no say over the rules, and none of them mines): Singapore `207.148.67.11`, Atlanta `64.177.44.185`, Los Angeles `149.28.69.173`, Toronto `149.248.54.9`, Amsterdam `95.179.179.191`, Mumbai `65.20.67.25`.

Any node answers these in a browser:

- `http://NODE:18081/block` — current height, block hash and total work, and how many heights this node has seen two blocks compete at
- `http://NODE:18081/addresses` — balances
- `http://NODE:18081/` — a simple block explorer

## Fair launch statement

From the maintainer, who wrote this software and runs its seed nodes:

- **No coins have been sold, promised or set aside** for anyone, by any arrangement, and none will be. The first block pays nothing. Every coin there will ever be is paid by the rules to whoever mines a block.
- **Coins on the test networks carry over to nothing.** The CVN Testnet and `devnet` are for testing, as was the first test network (`testnet-1`, now retired). Their coins have no value and no claim on any later network, which will start from zero for everyone.
- **I will mine from the first block, like anyone else,** with one home computer (a 16-core desktop processor) running the programs published here. Its mining address will be published before the start, so that anyone can see what it earned.
- **The seed nodes do not mine.** They are rented servers that keep the chain and pass blocks on. The computer I mine with is not a seed and has no better place on the network than yours.
- **I will run a mining pool that anyone may use. It takes no fee, and its blocks pay the miners themselves.** It is the published program with one option added, on a rented server that does nothing else. The reward of every block it finds is paid, in that block, to the machines that worked for it, by the work each did. Those coins never pass through the pool. Only parts too small for a block, or beyond the fifty a block has room for, go to the pool's address, and are paid on from there; what it found, what its blocks paid and what it paid can be checked on its page and on the chain. Its address will be published before the start, like my own. Nobody has to use it: mining by yourself works as it always has, and anyone can run a pool of their own with the same program.
- **The start is announced in advance** as a Bitcoin block height fixed in a tagged release of this software, together with the packages, their SHA-256 sums and the source of the faster miner, so that everybody has the same programs and nobody can begin early. How that works, and how to check it afterwards, is in section 11 of the [design paper](WHITEPAPER.md).

What reviewers found when they tested these claims against the code, and what was done about it, is recorded in [audit/FINDINGS_FAIR_LAUNCH.md](audit/FINDINGS_FAIR_LAUNCH.md).

## Design in brief

| | |
|---|---|
| Block time | 45 seconds |
| Mining algorithm | RandomX, tuned for home processors |
| Block reward | 50 CVN, halving every 7 years for 49 years |
| Total supply | 486,727,500 CVN |
| Fees | Voluntary; zero is allowed. Every fee goes to the miner of the block. A fifth of every block is closed to transfers that pay a fee |
| Lost coins | 20% of any coin unmoved for 7 years is set aside. Its owner has 7 more years to take it back; after that it returns to miners over time |
| Pre-mine | None |

The launch rules run on a separate development network (`--network devnet`). The public test of them, the CVN Testnet (`--network cvn_testnet`), is defined but not started; its start will be announced as a Bitcoin block height.

## Building and testing

```bash
go test ./...
go build -o cvn_node .
```

`go test ./...` runs every rule inside one process and takes about six minutes on the author's machine, longer on a slower one. A second set of tests starts several real nodes on this one machine and watches them work together: seeds, miners, a late joiner, a transfer, a node that loses power, a miner that returns from being cut off, two halves of a network that meet, and this version sharing a network with the release before it. It takes a few minutes and is run separately:

```bash
go test -tags multinode -run MultiNode -timeout 30m ./node/
```

Those nodes run a test-only network that exists only in a program built with `-tags multinode`. It is reachable from this machine alone, and no released program contains it.

Where things are:

| Folder | What is in it |
|---|---|
| the main folder | The guides, the launcher scripts, and one small start-up file (`main.go`) |
| `node/` | The program itself: node, miner and wallets, with its tests beside the code they check |
| `internal/randomx/` | The RandomX engine: a published Go implementation with CVN's two settings and large-page memory on Windows (see its `README_CVN.md`) |
| `frontend/` | The screens of the Windows desktop wallet |

The project depends on three libraries: [bbolt](https://github.com/etcd-io/bbolt) for storage, [Wails](https://wails.io) for the desktop wallet window, and [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys), the Go project's own package for talking to the operating system. Everything that comes from someone else, and its licence, is listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Reporting problems

Open an issue on GitHub with a screenshot of the miner window and the output of `http://localhost:18081/block` (on `devnet`, port 28081). Make sure no private key or recovery words are in the picture. For anything that could let someone take or create coins, please report it privately first: on the repository's Security tab, choose "Report a vulnerability".

## Licence

[MIT](LICENSE).
