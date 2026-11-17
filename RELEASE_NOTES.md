Release candidate 8. A test release: the CVN Testnet has not started, and coins on any CVN test network have no value and carry over to nothing.

**This replaces release candidate 7, which should not be used.** The two do not agree about every block (see "Since release candidate 7" below), so a network must run one or the other, and it should be this one.

**This release changes rules every node enforces. It cannot follow a development-network (`devnet`) chain made by an earlier release, and it can no longer follow the first test network (`testnet-1`) at all.** Every machine on `devnet` must move to this release together and start from an empty chain.

## Rule changes

- **Relief for a late block is no longer inherited.** A block dated six minutes or more after its parent is still mined at a halved difficulty, but the next block is worked out from the difficulty its parent would have had without that. Before, one long gap left dozens of nearly free blocks for whoever held the most mining power.
- **Difficulty steps are applied as e^x.** Before, a miner holding most of the mining power could gain up to 13% by choosing the dates of its blocks. The same patterns now gain nothing. The arithmetic is done in whole numbers, so every node gets the same result.
- **The first test network is retired.** `--network testnet-1` is refused, and every rule that existed only for it is gone from the program: the first difficulty rule, decimal amounts, the looser block format, addresses without a check, and the uncle share. Two networks remain, `cvn_testnet` (the default) and `devnet`, on the same rules.

How these were found and checked is in `audit/CHANGES_FOR_ROUND_4.md` and `audit/FINDINGS_ROUND_4.md`. The change to difficulty steps has since been reviewed (`audit/FINDINGS_ROUND_5.md`); the rounding change listed below was made after that review.

## Since release candidate 7

Three reviewers examined release candidate 7's new difficulty arithmetic (`audit/FINDINGS_ROUND_5.md`). They agreed it gives the same result on every machine, and found smaller things, now fixed:

- **Each difficulty step is rounded to the nearest whole number.** It was rounded down, which lost a little on every step, always the same way. This is the change that makes this release disagree with release candidate 7.
- **Miners reach the six-minute halving at the same moment.** A block may be dated up to two minutes ahead of the clock, so a miner who dated ahead reached the halving before those who did not, and took about 1% more than its share. The program's miner now dates a block ahead for that purpose alone, so nobody has a head start.
- The program no longer builds for 32-bit computers, where difficulty would one day stop fitting.
- **The miner window has colour:** the heading and figures in green, and in the log the `[PoD CORE]` tag in purple and `[SUCCESS]` in green. Set the environment variable `NO_COLOR` to switch it off.

## Other changes

- The CVN Testnet is the network the program runs on when none is named. It cannot start until its Bitcoin block is announced; wallets still open.
- The fast miner (the CVN edition of XMRig) is used whenever its `xmrig-cvn` folder is beside the program. No setting is needed.
- The miner window shows whether the fast miner's processor tuning was applied. `MINING_GUIDE.md` section 3 explains the tuning, what it needs and what it costs.
- A `miner_config.json` that cannot be read is refused, with the line at fault.
- The built-in miner checks its own block as it would anyone else's before keeping it.
- Two log lines that read as faults and were not no longer appear.
- The desktop wallet no longer says "mainnet".

## Moving a devnet machine to this release

1. Stop the miner or seed.
2. Update to this release.
3. Delete the development network's files from the CVN folder: `cvn_devnet.db`, `genesis_anchor_devnet.txt` and `peers_devnet.json`. Your wallet (`miner_config.json`) is not touched and your address does not change.
4. Start again as before, with the Bitcoin height agreed for the restart.

The fast miner itself is unchanged: `cvn-6.26.0-2` from https://github.com/kalashnikovski/cvn-xmrig/releases still works.

## Checking a download

`SHA256SUMS_windows.txt` and `SHA256SUMS_linux.txt` list the SHA-256 of each package and of each program inside it. Section 10 of `Installation_Guide.md` says how to check them, and how to rebuild the programs yourself and compare.
