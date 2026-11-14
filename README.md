# Covenant Standard (CVN)

Covenant Standard is a proof-of-work blockchain written from scratch in Go. It is built for fair, open mining on ordinary home computers: no pre-mine, no founder allocation, no investor sale.

> **Status: public test network.** The chain running today is a test network (`testnet-1`). Its coins have no value, and it will be reset before the real launch so that everyone starts from zero at an announced time. Mine it to help test, not to accumulate.

## What works today

- Mining with 45-second blocks; difficulty adjusts on every block.
- Sending CVN between wallets. Every transfer is signed by its owner and checked by every node.
- A desktop wallet on Windows and a text wallet on Linux and Windows.
- Nodes find each other, sync from scratch, and recover from forks without help.
- No port forwarding needed: a node behind a home router still receives blocks and can mine.

See [ROADMAP.md](ROADMAP.md) for what is finished and what is still being built. The rules planned for launch are set out in [WHITEPAPER.md](WHITEPAPER.md).

## Quick start

Full step-by-step instructions are in [Installation_Guide.md](Installation_Guide.md). In short:

### Windows

1. Install the current version of [Go](https://go.dev/dl/).
2. Download this repository and unpack it.
3. Run `build_clean.bat`.
4. Run `LaunchMiner.bat` to create a wallet and start mining. If it shows a mining address that is not yours, choose "Create a new wallet".
5. Run `LaunchWallet.bat` for the desktop wallet.

### Linux

```bash
git clone https://github.com/kalashnikovski/cvn_node
cd cvn_node
go build -o cvn_node .
./cvn_node --generate-wallet
./cvn_node --miner-address YOUR_CVN_ADDRESS
```

`--generate-wallet` prints your address and your private key once. **Write the private key down.** It is not saved anywhere, and coins mined to an address cannot be spent without it.

To limit how many processor threads the miner uses:

```bash
./cvn_node --miner-address YOUR_CVN_ADDRESS --threads 4
```

On networks that mine with RandomX you can choose between the built-in miner and a faster separate one, and a one-time system setting (large pages) makes either of them about a quarter faster. See [MINING_GUIDE.md](MINING_GUIDE.md).

### The wallet

While the miner runs in one terminal, open another:

```bash
./cvn_node --wallet
```

On Windows: `cvn_miner.exe --wallet`, or `LaunchWallet.bat` for the desktop version.

A miner receives a separate coin for every block won, and one transfer can spend at most 200 coins. **Combine my coins**, in both wallets, merges them into a few large coins in your own address.

## Network

| | Port | Purpose |
|---|---|---|
| Node to node | 8080 | Blocks and transfers |
| Sync and wallet | 8081 | Block download, balances, sending transfers |
| Status | 8082 | Height and tip of this node |

Seed nodes (all run by the project's maintainer; they are where a new node looks first, they have no say over the rules, and none of them mines): Singapore `207.148.67.11`, Atlanta `64.177.44.185`, Los Angeles `149.28.69.173`, Toronto `149.248.54.9`, Amsterdam `95.179.179.191`, Mumbai `65.20.67.25`.

Any node answers these in a browser:

- `http://NODE:8081/block` — current height, block hash and total work
- `http://NODE:8081/addresses` — balances
- `http://NODE:8081/` — a simple block explorer

## Fair launch statement

From the maintainer, who wrote this software and runs its seed nodes:

- **No coins have been sold, promised or set aside** for anyone, by any arrangement, and none will be. The first block pays nothing. Every coin there will ever be is paid by the rules to whoever mines a block.
- **Coins on the test networks carry over to nothing.** `testnet-1`, the CVN Testnet and `devnet` are for testing. Their coins have no value and no claim on any later network, which will start from zero for everyone.
- **I will mine from the first block, like anyone else,** with one home computer (a 16-core desktop processor) running the programs published here. Its mining address will be published before the start, so that anyone can see what it earned.
- **The seed nodes do not mine.** They are rented servers that keep the chain and pass blocks on. The computer I mine with is not a seed and has no better place on the network than yours.
- **The start is announced in advance** as a Bitcoin block height fixed in a tagged release of this software, together with the packages, their SHA-256 sums and the source of the faster miner, so that everybody has the same programs and nobody can begin early. How that works, and how to check it afterwards, is in section 11 of the [design paper](WHITEPAPER.md).

What reviewers found when they tested these claims against the code, and what was done about it, is recorded in [audit/FINDINGS_FAIR_LAUNCH.md](audit/FINDINGS_FAIR_LAUNCH.md).

## Design in brief

| | Test network today | Planned for launch |
|---|---|---|
| Block time | 45 seconds | 45 seconds |
| Mining algorithm | SHA-256 | RandomX, tuned for home processors |
| Block reward | 50 CVN | 50 CVN, halving every 7 years for 49 years |
| Total supply | — | 486,727,500 CVN |
| Fees | Voluntary; zero is allowed | Voluntary; zero is allowed |
| Lost coins | — | 20% of any coin unmoved for 7 years returns to miners over time |
| Pre-mine | None | None |

The launch rules run on a separate development network (`--network devnet`). The public test of them, the CVN Testnet (`--network cvn_testnet`), is defined but not started; its start will be announced as a Bitcoin block height.

## Building and testing

```bash
go test ./...
go build -o cvn_node .
```

Where things are:

| Folder | What is in it |
|---|---|
| the main folder | The guides, the launcher scripts, and one small start-up file (`main.go`) |
| `node/` | The program itself: node, miner and wallets, with its tests beside the code they check |
| `internal/randomx/` | The RandomX engine: a published Go implementation with CVN's two settings and large-page memory on Windows (see its `README_CVN.md`) |
| `frontend/` | The screens of the Windows desktop wallet |

The project depends on two libraries: [bbolt](https://github.com/etcd-io/bbolt) for storage and [Wails](https://wails.io) for the desktop wallet window. Everything that comes from someone else, and its licence, is listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Reporting problems

Open an issue on GitHub with a screenshot of the miner window and the output of `http://localhost:8081/block`. For anything that could let someone take or create coins, please contact the maintainer privately first.

## Licence

[MIT](LICENSE).
