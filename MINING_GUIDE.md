# Mining guide: choosing a miner, large pages and processor tuning

This guide applies to every CVN network: the development network today, and the public test network and mainnet when they launch. All of them mine with RandomX.

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
3. Make a folder named `xmrig-cvn` inside your CVN folder (the one that contains `cvn_node`) and unpack into it, so that the miner is at `cvn_node/xmrig-cvn/xmrig`:

   ```bash
   cd cvn_node
   mkdir -p xmrig-cvn
   tar -xzf ~/Downloads/xmrig-cvn-linux-static-x64.tar.gz -C xmrig-cvn
   ls xmrig-cvn
   ```

   If `ls` shows another folder and not a file named `xmrig`, move that folder's contents up one level into `xmrig-cvn`.
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
