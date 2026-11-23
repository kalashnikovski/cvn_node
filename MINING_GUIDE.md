# Mining guide: choosing a miner, large pages, processor tuning and pools

This guide applies to every CVN network: the development network today, and the public test network and mainnet when they launch. All of them mine with RandomX.

Sections 1 to 4 are about mining by yourself, with your own node. Section 5 is about mining at a pool.

## 1. Two miners to choose from

| | Built-in miner | Fast miner (CVN edition of XMRig) |
|---|---|---|
| What it is | Part of the CVN program | A separate program the CVN program starts for you |
| To install | Nothing | Download and unpack one folder |
| Speed on a 16-core desktop (Ryzen 9 7950X3D, large pages on) | about 16,400 hashes per second | about 17,900 hashes per second |
| To use it | Nothing to do | Put its folder in the right place (below) |

Both pay the same address, show the same dashboard and follow the same rules; the fast miner is roughly a tenth quicker. You start mining the same way with either: the CVN program uses the fast miner when it finds it and its own miner when it does not.

### Using the fast miner

There is nothing to type and no file to edit. You download one folder, put it inside your CVN folder, and start the miner the way you always do.

**Windows**

1. Close the miner if it is running.
2. Go to https://github.com/kalashnikovski/cvn-xmrig/releases and download `xmrig-cvn-windows-x64.zip` from the newest release (currently `cvn-6.26.0-2`).
3. Open your CVN folder: the one that contains `cvn_miner.exe` and `LaunchMiner.bat`.
4. Inside it, make a new folder named exactly `xmrig-cvn`.
5. Unpack the ZIP and move its contents into that folder. When you are done it must look like this, with `xmrig.exe` directly inside `xmrig-cvn` and not one folder further down:

   ```
   cvn_node\                  (your CVN folder; yours may have another name)
       cvn_miner.exe
       LaunchMiner.bat
       xmrig-cvn\
           xmrig.exe
           WinRing0x64.sys
           ...
   ```

6. Double-click `LaunchMiner.bat` as usual.

**Linux**

1. Stop the miner if it is running.
2. Download `xmrig-cvn-linux-static-x64.tar.gz` from the newest release at https://github.com/kalashnikovski/cvn-xmrig/releases.
3. Unpack it inside your CVN folder (the one that contains `cvn_node`). The download makes the `xmrig-cvn` folder itself, so do not make one first:

   ```bash
   cd cvn_node
   tar -xzf ~/Downloads/xmrig-cvn-linux-static-x64.tar.gz
   ls xmrig-cvn
   ```

   `ls` must show a file named `xmrig`, so that the miner is at `cvn_node/xmrig-cvn/xmrig`.
4. Start the miner as usual.

**How to tell it is running.** Among the first lines the miner window prints, look for:

```
[MINER] Starting the fast miner: C:\cvn_node\xmrig-cvn\xmrig.exe
```

and, a little later, lines beginning `[FAST MINER]`. If you see `[RANDOMX] Mining engine ready` and no `[FAST MINER]` lines, the built-in miner is running: check step 5 (Windows) or step 3 (Linux), and look just above for a line beginning `[MINER]` that says why the fast miner was not used.

The CVN program starts the fast miner, gives it work, shows its speed on the usual dashboard and stops it when you close the program. You never run or configure XMRig yourself, and you do not need to double-click anything inside `xmrig-cvn`.

**To go back to the built-in miner,** rename or delete the `xmrig-cvn` folder, or start with `--miner builtin` (how to add an option like that is in [Installation_Guide.md](Installation_Guide.md), "Typing a command").

**Checking the download (optional).** Compare the file's SHA-256 with the release's `SHA256SUMS` file, which for `cvn-6.26.0-2` reads:

```
7782052f46500678aa06d5e798ea8e9b0e9ff0febc51a1048559cb280b67fd8d  xmrig-cvn-linux-static-x64.tar.gz
60c081d09fdeb3628995d5ce28a13c000372cf93b26dbf55922f2081d5f72a4b  xmrig-cvn-windows-x64.zip
```

On Windows: `certutil -hashfile xmrig-cvn-windows-x64.zip SHA256`. On Linux: `sha256sum xmrig-cvn-linux-static-x64.tar.gz`.

**Where it comes from.** Its complete source is in that repository under the same tag: it is XMRig with CVN's two RandomX settings and no donation, under the GNU GPL version 3, so anyone can build it and compare what it does and how fast it runs. It is built exactly as XMRig is: follow XMRig's own instructions at https://xmrig.com/docs/miner/build on the tagged source. A build of your own will not have the same SHA-256 as the published one, because XMRig's builds are not repeatable bit for bit; the sums above check a download, not a rebuild.

Things to know:

- **Only the CVN edition works.** The standard XMRig computes a different RandomX. The CVN program asks the miner for its name before using it and will not use one that does not say it is the CVN edition. That check is against mistakes, not against a forged program: only put a miner there that you downloaded from the project page yourself.
- **It is looked for inside the CVN folder only**, never in whatever folder you happen to start the program from.
- **If the fast miner keeps failing, the built-in miner takes over** and the dashboard says so. Your machine does not sit idle.
- **A different location:** `--xmrig-path FOLDER_OR_FILE`, or `"xmrig_path"` in `miner_config.json`.
- **Fewer threads:** `--threads N` limits either miner to N processor threads.
- **Antivirus programs often flag mining software**, including XMRig and programs that start it, because the same software is abused by malware. If yours removes the files, you will need to allow the CVN folder in your antivirus settings. Only do this for files you downloaded from the two project pages yourself.
- **If you edit `miner_config.json` by hand,** every line between the `{` and the `}` ends with a comma except the last, and an address is written in full, exactly as your wallet shows it. The program refuses to start if the file cannot be read, and says which line to look at.

### A small machine is a good node and a poor miner

RandomX needs about 2.3 GB of free memory to mine at full speed. With less, the miner falls back to a low-memory mode that is many times slower: a 1 GB server manages a handful of hashes per second, against thousands on a desktop. It will almost never find a block.

Such a machine is still useful to the network as a node. Run it with `--no-mining` and it does everything a node does without wasting its one processor on mining; see the installation guide.

## 2. Large pages

RandomX works through a 2 GB table in memory. Normally the system hands out memory in 4 KB pieces; with large pages it uses 2 MB pieces, and the processor spends far less time finding its place in the table.

Measured on the same 16-core desktop, built-in miner:

| | Hashes per second |
|---|---|
| Without large pages | about 12,800 |
| With large pages | about 16,400 |

That is over a quarter more, for a one-time setting. Both miners use large pages automatically once the system allows it. Without the setting, everything still works at the ordinary speed.

### Windows

Windows only gives large pages to an account that holds the "Lock pages in memory" right.

**Windows Pro, Enterprise or Education**

1. Press `Win + R`, type `secpol.msc`, press Enter.
2. Open **Local Policies**, then **User Rights Assignment**.
3. Double-click **Lock pages in memory**.
4. Click **Add User or Group**, type your Windows user name, click **OK** twice.
5. Restart the computer.

**Windows Home** (which has no `secpol.msc`)

1. Put the fast miner in place as in section 1.
2. Open the `xmrig-cvn` folder, right-click `xmrig.exe`, choose **Run as administrator**, wait a few seconds and close it. When run as administrator it gives your account the right by itself.
3. Restart the computer.

After that, start the miner normally. Administrator rights are not needed again.

### Linux

Reserve the pages (1,280 pages of 2 MB covers the table and the working memory of the mining threads):

```bash
sudo sysctl -w vm.nr_hugepages=1280
```

To keep the setting across restarts:

```bash
echo "vm.nr_hugepages=1280" | sudo tee -a /etc/sysctl.conf
```

### How to tell it worked

When mining starts, the dashboard shows one of these lines.

Built-in miner:

```
[RANDOMX] Mining engine ready (full memory, large pages mode).
```

Fast miner:

```
[FAST MINER] randomx allocated 2336 MB (2080+256) huge pages 100% 1168/1168
```

If the built-in miner says only `full memory mode`, or the fast miner shows `huge pages 0%`, large pages were not granted. The usual reasons:

- The computer was not restarted after the setting was made.
- The computer has been running for a long time and its free memory is in small scattered pieces. Restart, and start the miner soon after.
- Less than about 3 GB of memory is free.

You can measure your machine at any time with the miner stopped (how to type a command is in [Installation_Guide.md](Installation_Guide.md), "Typing a command"):

```
cvn_miner.exe --randomx-bench
```

On Linux: `./cvn_node --randomx-bench`.

## 3. Processor tuning (optional, fast miner only)

The fast miner can change a few processor settings (called MSR registers) that slow RandomX down. It tries this by itself every time it starts and carries on at the ordinary speed if the system refuses. The built-in miner does not do it.

Measured on the same 16-core desktop (Ryzen 9 7950X3D, large pages on, fast miner):

| | Hashes per second |
|---|---|
| Tuning refused | about 17,900 |
| Tuning applied | about 19,300 |

That is roughly 8% more on this processor; the gain differs from one processor to another. It is the smallest of the three gains in this guide and the only one that asks you to lower your system's defences, so treat it as optional. Large pages (section 2) matter far more.

### Windows

Two things must both be true.

1. **The miner is started as administrator.** Right-click the Start button, choose **Terminal (Admin)**, go to your CVN folder and start the miner from there (how to type a command is in [Installation_Guide.md](Installation_Guide.md), "Typing a command"):

   ```
   cd /d C:\cvn_node
   LaunchMiner.bat
   ```

   The fast miner is started by the CVN program and receives its rights from it.

2. **Windows is not running on top of its own hypervisor.** It is whenever any of these is switched on: Memory integrity (Windows Security, Device security, Core isolation), Hyper-V, Windows Subsystem for Linux 2, Docker Desktop, or other virtual-machine features. While it is, Windows refuses the change even for an administrator. Either switch those features off and restart, or switch off virtualization in the computer's BIOS (named **SVM** on AMD, **VT-x** or **Intel Virtualization Technology** on Intel), which stops all of them at once and can be switched back on later.

Know what you are giving up. Memory integrity is a protection against malicious drivers, and the tuning itself is done through a driver (`WinRing0x64.sys`, in the `xmrig-cvn` folder) that security programs warn about because other software can misuse it. If the computer holds a wallet with coins you care about, weigh that against 8%.

### Linux

Start the CVN program as root, so that the fast miner it starts may write the settings:

```bash
sudo ./cvn_node
```

Files the program creates while run this way belong to root; if you later run it as yourself again, give them back with `sudo chown -R "$USER" .` in the CVN folder. The project has measured the tuning on Windows only.

### How to tell it worked

Soon after the fast miner starts, the dashboard shows what happened:

```
[FAST MINER] msr register values for "ryzen_19h_zen4" preset have been set successfully (6 ms)
```

The name in quotes depends on your processor. If the tuning was refused you see the reason and then the result:

```
[FAST MINER] msr to access MSR registers Administrator privileges required.
[FAST MINER] msr FAILED TO APPLY MSR MOD, HASHRATE WILL BE LOW
```

| Line | Meaning |
|---|---|
| `Administrator privileges required` | The miner was not started as administrator (Windows). |
| `cannot set MSR 0x...` | Started as administrator, but Windows is running on its hypervisor (point 2 above). |
| `msr kernel module is not available` | Not started as root, or the system has no `msr` module (Linux). |

"HASHRATE WILL BE LOW" means the ordinary speed without the tuning, not a fault: the miner works normally. The settings last until the computer restarts and are applied again each time the miner starts.

## 4. A warning about unstable machines

RandomX uses the processor and memory harder than almost any other program. A machine whose memory is run faster than its tested settings (overclocked, or with timings set by hand) may run everything else without trouble and still crash or restart when mining starts. If that happens, return the memory to its standard profile before mining; an unstable machine also produces wrong results that the network refuses.

## 5. Mining at a pool

A machine that mines by itself is paid the whole of every block it finds, and finds one rarely. On a busy network a single desktop can wait days or weeks for a block. A pool is many machines working on the same blocks: every block the pool finds is divided among them by the work each did, so each is paid a little, often.

Over a long time the two come to the same. A pool does not add to what a machine earns; it evens out the waiting.

| | Mining by yourself | Mining at a pool |
|---|---|---|
| What runs on your machine | The whole program: a node with its copy of the chain, and a miner | The fast miner only. No node and no chain |
| Who builds the blocks | Your own node | The pool |
| When you are paid | When your machine finds a block: all of it, straight to your address | Your part of every block the pool finds, sent by the pool once it owes you enough |
| Whom you rely on | Nobody | The pool's operator, for the coins the pool holds until it pays them |

Nobody has to use a pool. Mining by yourself works exactly as before, and you can change from one to the other whenever you like: the coins arrive at the same address either way.

### What you need

- **The fast miner** in your CVN folder, as section 1 describes. The built-in miner cannot mine at a pool: it takes its work from the node in the same program.
- **A mining address.** The one already saved on your machine is used. It is the name the pool knows your machine by, and where the pool sends what it owes.
- **The pool's name**, for example `pool.example.org`. A pool that listens on a port of its own choosing gives it with the name: `pool.example.org:3333`.

### Starting

**Windows:** open a Command Prompt in your CVN folder ([Installation_Guide.md](Installation_Guide.md), "Typing a command") and type:

```
LaunchMiner.bat --pool-mine pool.example.org
```

**Linux:**

```bash
./cvn_node --pool-mine pool.example.org
```

Add `--network devnet` (or whichever network the pool is on) and `--threads N` as you would for mining by yourself.

**To mine at the pool every time** without typing anything, add one line to `miner_config.json` in your CVN folder, and start the miner the way you always do:

```
{
  "miner_address": "CVN_your_address",
  "mine_at_pool": "pool.example.org"
}
```

Mind the comma at the end of every line but the last. On Windows, the first time you start after changing the file, `LaunchMiner.bat` asks you to confirm your mining address once more. To mine by yourself again, take the line out.

### What you see

```
====================================================================
 MINING AT A POOL ON DEVNET
====================================================================
 Pool:          pool.example.org:28083   (asked for with --pool-mine)
 Your address:  CVN_your_address
                The pool counts this machine's work for that address and pays it there.
 Fast miner:    C:\cvn_node\xmrig-cvn\xmrig.exe
 No node runs on this machine and no chain is kept. The pool builds the blocks.
 The pool says: 12 miner(s) connected, 96.40 kH/s, 310 block(s) found so far. It takes no fee
                and pays every address it owes 1 CVN or more.
 Its page:      http://pool.example.org:28081/pool
====================================================================
2026/11/24 09:26:28 [POOL] Connected to the pool.
2026/11/24 09:26:37 [POOL] The pool took this machine's first share. From here on it has a part in the blocks the pool finds.
2026/11/24 09:27:28 [POOL] 17.85 kH/s · shares taken 2 · the pool owes this address 0 CVN, has 0 CVN on its way and has paid 0 CVN
```

The last line comes once a minute. In it:

- **Shares taken** are pieces of work the pool has counted for you. The pool asks each machine for work it can do about once every 30 seconds, so a fast machine and a slow one both hand in about two shares a minute; the fast machine's shares are simply worth more.
- **Too late** counts shares that arrived just after somebody found a block. Every miner has a few. They are not a fault.
- **Refused**, if it appears, counts shares the pool would not take for another reason, and the reason is printed when it happens. More than a handful means something is wrong: usually a miner that is not the CVN edition, or an unstable machine (section 4).
- **Owes, on its way, has paid** are the pool's own figures for your address, read from its page.

The program checks that the pool is on the network it was started for, and stops with a message if it is not. If the pool cannot be reached, the fast miner keeps trying, and the line says "Not connected to the pool at the moment".

### How you are paid

This is how a pool run with this program works. A pool run with other software has its own rules: read them before you mine there.

1. **A block the pool finds is divided by recent work.** The pool looks back over the most recent shares, as far as it takes to add up to twice the work of one block, and divides the block's reward among their addresses by the work each did. A machine that has been at the pool for a while has a part in every block, whoever's machine found it.
2. **The block has to last.** It counts once 100 more blocks stand on it, about 75 minutes. Until then it is "waiting". If the network replaces it in that time, which happens now and then to every miner, it counts for nothing and nobody is owed for it.
3. **You are owed your part** from that moment. The pool page shows it.
4. **The pool pays** every address it owes 1 CVN or more, by an ordinary transfer, whenever its operator's payments run (once an hour, unless the operator has chosen otherwise). Each transfer carries a fee of 0.001 CVN, shared equally among the addresses it pays, so each receives at most 0.001 CVN less than it was owed.
5. **An amount under 1 CVN stays owed** until it has grown. It does not expire.

The pool page, at `http://POOL-NAME:18081/pool` on the CVN Testnet and `http://POOL-NAME:28081/pool` on devnet, shows every block the pool found, what became of it, and how its reward was divided address by address. Anyone can add the division up. The coins themselves are on the chain: the pool's address, and every payment from it, can be looked up on any node's explorer.

### What a pool can and cannot do

- **It holds your coins until it pays them.** A block's reward is paid by the network to the pool's address, and the pool sends each miner's part on later. An operator who is dishonest, careless or broken into could fail to pay. The pool page lets you check that the division is right and that payments are made; it cannot make an operator pay. Mine at a pool whose operator you have reason to trust, and notice if the amount it owes you keeps growing without being paid.
- **It cannot touch your wallet.** The pool is given your address and nothing else. Never give a pool, or anyone, your private key or recovery words.
- **It may keep a fee.** A pool run with this program keeps none: there is no fee in it to switch on. A pool run with other software sets its own.
- **The connection is not encrypted** in this version. Somebody between you and the pool could see your address and your work. That is all it carries.
- **Antivirus programs are quicker to object to pool mining** than to mining by yourself: mining software that connects to a pool is what coin-mining malware looks like. See the note on antivirus programs in section 1.

A network is healthier with many miners and several pools than with one large pool. If one pool comes to hold a large part of the network's speed, mine at another, or by yourself.

### Running a pool

Anyone can run one: it is the same program with one more option. See [POOL_GUIDE.md](POOL_GUIDE.md).
