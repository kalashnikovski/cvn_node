Release candidate 9. A test release: the CVN Testnet has not started, and coins on any CVN test network have no value and carry over to nothing.

**This replaces release candidates 7 and 8, which should not be used.** It does not agree with either about every block, so a network must run one release, and it should be this one.

**This release changes rules every node enforces. It cannot follow a development-network (`devnet`) chain made by an earlier release, and it can no longer follow the first test network (`testnet-1`) at all.** Every machine on `devnet` must move to this release together and start from an empty chain.

## Rule changes

- **Relief for a late block is no longer inherited.** A block dated six minutes or more after its parent is still mined at a halved difficulty, but the next block is worked out from the difficulty its parent would have had without that. Before, one long gap left dozens of nearly free blocks for whoever held the most mining power.
- **Difficulty steps are applied as e^x, rounded to the nearest whole number.** Before, a miner holding most of the mining power could gain up to 13% by choosing the dates of its blocks. The same patterns now gain nothing. The arithmetic is done in whole numbers, so every node gets the same result.
- **The seven-year sweep leaves the smallest coins alone.** A coin so small that a fifth could not be taken without leaving less than the smallest coin allowed used to be taken entirely. It is now left as it is. The sweep takes a fifth from a coin or nothing; it never takes a coin whole.
- **Plain names inside a block.** The fixed texts in a block's reward transaction, and the first block's fields, said things like "immutable consensus valid" and named a "steward assembly" that does not exist. They now say what they are: a reward with no key and no signature, and a first block that pays nothing to nobody. This changes every block's identifier.
- **The first test network is retired.** `--network testnet-1` is refused, and every rule that existed only for it is gone from the program: the first difficulty rule, decimal amounts, the looser block format, addresses without a check, and the uncle share. Two networks remain, `cvn_testnet` (the default) and `devnet`, on the same rules.

How the difficulty changes were found and checked is in `audit/FINDINGS_ROUND_4.md` and `audit/FINDINGS_ROUND_5.md`. The sweep change and the names were made after the last review and have not been reviewed by anyone but their authors.

## Other changes

- **Miners reach the six-minute halving at the same moment.** A block may be dated up to two minutes ahead of the clock, so a miner who dated ahead reached the halving before those who did not, and took about 1% more than its share. The program's miner now dates a block ahead for that purpose alone, so nobody has a head start.
- **Renew my coins**, in both wallets: one step gives the coins that are within a year of their seven-year date a new seven-year count.
- **The miner window has colour and plain wording:** the heading and figures in green; in the log the `[MINING]` tag in purple and `[SUCCESS]` in green. It says "mining address" and "recent activity" where it said "rig identity" and "live streaming consensus and hardware action regions". Set the environment variable `NO_COLOR` to switch the colour off.
- **The desktop wallet uses plain wording too:** "balance", "your address", "newest block", "send CVN".
- The CVN Testnet is the network the program runs on when none is named. It cannot start until its Bitcoin block is announced; wallets still open.
- The fast miner (the CVN edition of XMRig) is used whenever its `xmrig-cvn` folder is beside the program. No setting is needed.
- The miner window shows whether the fast miner's processor tuning was applied. `MINING_GUIDE.md` section 3 explains the tuning, what it needs and what it costs.
- A `miner_config.json` that cannot be read is refused, with the line at fault.
- The built-in miner checks its own block as it would anyone else's before keeping it.
- The program no longer builds for 32-bit computers, where difficulty would one day stop fitting.
- Two log lines that read as faults and were not no longer appear.

## Moving a devnet machine to this release

1. Stop the miner or seed.
2. Update to this release.
3. Delete the development network's files from the CVN folder: `cvn_devnet.db`, `genesis_anchor_devnet.txt` and `peers_devnet.json`. Your wallet (`miner_config.json`) is not touched and your address does not change.
4. Start again as before, with the Bitcoin height agreed for the restart.

The fast miner itself is unchanged: `cvn-6.26.0-2` from https://github.com/kalashnikovski/cvn-xmrig/releases still works.

## Checking a download

`SHA256SUMS_windows.txt` and `SHA256SUMS_linux.txt` list the SHA-256 of each package and of each program inside it. Section 10 of `Installation_Guide.md` says how to check them, and how to rebuild the programs yourself and compare.
