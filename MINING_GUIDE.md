# Mining guide: choosing a miner and enabling large pages

This guide applies to CVN networks that mine with RandomX: the development network today, and the public test network and mainnet when they launch. The first test network (testnet-1) mines with SHA-256 and needs none of this.

## 1. Two miners to choose from

| | Built-in miner | Fast miner (CVN edition of XMRig) |
|---|---|---|
| What it is | Part of the CVN program | A separate program the CVN program starts for you |
| To install | Nothing | Download and unpack one folder |
| Speed on a 16-core desktop (Ryzen 9 7950X3D, large pages on) | about 16,400 hashes per second | about 17,900 hashes per second |
| Chosen with | nothing, or `--miner builtin` | `--miner xmrig` |

The built-in miner is what runs unless you ask for the other one. Both pay the same address, show the same dashboard and follow the same rules; the fast miner is roughly a tenth quicker.

### Using the fast miner

1. Download the CVN edition for your system from https://github.com/kalashnikovski/cvn-xmrig/releases
2. Unpack it into a folder named `xmrig-cvn` beside the CVN program, so that the folder contains `xmrig.exe` (Windows) or `xmrig` (Linux).
3. Start the CVN program with `--miner xmrig`:

   ```
   cvn_miner.exe --network NAME --miner-address YOUR_CVN_ADDRESS --miner xmrig
   ```

   Or make it permanent by adding one line to `miner_config.json`:

   ```json
   {
     "miner_address": "CVN_...",
     "miner": "xmrig"
   }
   ```

The CVN program starts the fast miner, gives it work, shows its speed on the usual dashboard and stops it when you close the program. You never run or configure XMRig yourself.

Things to know:

- **Only the CVN edition works.** The standard XMRig computes a different RandomX, so the CVN program refuses to start it.
- **If the fast miner is missing or keeps failing, the built-in miner takes over** and the dashboard says so. Your machine does not sit idle.
- **A different location:** `--xmrig-path FOLDER_OR_FILE`, or `"xmrig_path"` in `miner_config.json`.
- **Fewer threads:** `--threads N` limits either miner to N processor threads.
- **Antivirus programs often flag mining software**, including XMRig and programs that start it, because the same software is abused by malware. If yours removes the files, you will need to allow the CVN folder in your antivirus settings. Only do this for files you downloaded from the two project pages yourself.

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

1. Unpack the CVN edition of XMRig as in section 1.
2. Right-click `xmrig.exe`, choose **Run as administrator**, wait a few seconds and close it. When run as administrator it gives your account the right by itself.
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

You can measure your machine at any time, without mining, with:

```
cvn_miner.exe --randomx-bench
```

### A warning about unstable machines

RandomX uses the processor and memory harder than almost any other program. A machine whose memory is run faster than its tested settings (overclocked, or with timings set by hand) may run everything else without trouble and still crash or restart when mining starts. If that happens, return the memory to its standard profile before mining; an unstable machine also produces wrong results that the network refuses.
