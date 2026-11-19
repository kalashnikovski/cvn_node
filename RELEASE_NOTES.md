Release candidate 11. A test release: the CVN Testnet has not started, and coins on any CVN test network have no value and carry over to nothing.

**This release changes no rule.** It agrees with release candidates 9 and 10 about every block, so machines on any of the three can share a network, and a development-network (`devnet`) chain carries on unchanged. Nothing needs deleting: update and start again.

One setting of the CVN Testnet changes before it starts: its first block's difficulty is 2,500,000 (was 1,000,000), chosen for a publicly announced start. The difficulty rule takes over from the second block either way.

## For someone joining a network

- **A new node no longer starts a chain of its own by mistake.** A mining node that was given seeds asks them for their first block before anything else. If none answers and it has no blocks, it says NOT MINING YET, mines nothing, and carries on by itself as soon as one answers. If they answer and are on a different chain, it stops and says DIFFERENT CHAIN, with what to check. Earlier releases began mining alone without a word. Whoever means to mine a network's very first blocks before its other machines are up adds `--start-alone`.
- **A seed address that cannot be read stops the program and is named.** An address written with a port number, a space or `http://` used to be left out silently.
- **The download at start shows its progress bar** as the blocks arrive. It was drawn only between passes, and a whole chain usually arrives in one, so it was never seen.
- **The installation guide's Windows section is rewritten** as two ways, A and B, of which you do one. It says where the download is on the releases page, and has the one command for joining the development network (section 7).

## For miners

- **The built-in miner no longer slows down on top of a full block.** To see whether the newest block had changed, it re-read that whole block on every hash. For an ordinary block that costs nothing; for a full one it cost more time than the mining. It now asks for the block's identifier alone. The fast miner was not affected.
- **The private key is not shown as you type or paste it** in the text wallet on Windows. The prompt already said so, and it was true only on Linux.

## For anyone running a node

- **Backups of the chain.** Two minutes after it starts, and then once a day, a node saves a copy of its chain into a `backups` folder beside the program, checks the copy (every block present and linked, each block matching its identifier, the first block this network's, and coins plus vault equal to what was issued), and keeps the newest seven. `--check-backup FILE` checks one by hand; `--restore-backup FILE` puts one back, with the node stopped, and keeps the file it replaced. `--backup-hours N`, `--backup-keep N` and `--no-backups` change the schedule. Section 10 of `Installation_Guide.md`.
- **A block that arrives while the node is catching up is fetched as soon as the catch-up ends.** It used to be dropped and not asked for again for up to thirty seconds, which on a 45-second network left the node a block behind for a whole interval.
- **Races are counted.** A node counts the heights at which it saw two valid blocks compete, logs each as `[RACE]` with the share of blocks so far, and reports the count with its newest block (`races`, `races_blocks`, `races_since` in the reply to `/block`). The count starts from zero when the node starts. Nothing about which block is kept has changed.

## Tested since the last release

- **Several real nodes on one machine.** A new set of tests starts the program several times over, each copy at its own address, and takes them through seeds, miners, a late joiner, a transfer, a seed that is killed and comes back, and a miner that returns from being cut off. At the end every node must hold the same block, the same work, the same vault and the same coins. `go test -tags multinode -run MultiNode -timeout 30m ./node/`. The network these run on exists only in a program built with that tag; this release does not contain it.
- **Full blocks.** The block size limit is 300,000 bytes. On the development network, across six seeds on three continents, five blocks of about 296,700 bytes (76 transfers of 50 outputs each) and six of about 286,000 bytes (16 transfers of 200 inputs each) reached every seed within 5 seconds of being mined, against a block interval of 45.

## Checking a download

Every file in this release is signed by GitHub as built by this project's build recipe from this repository at this tag: `gh attestation verify cvn_node_windows.zip --repo kalashnikovski/cvn_node`. `SHA256SUMS_windows.txt` and `SHA256SUMS_linux.txt` list the SHA-256 of each package and of each program inside it, and anyone can rebuild the programs from the tag and compare. Section 11 of `Installation_Guide.md` says how.

## Updating

1. Stop the miner or seed.
2. Replace the programs with this release's (a seed: `bash /opt/cvn_node/deploy/update_seed.sh`).
3. Start again exactly as before. Keep the chain files and, on a development network started from a Bitcoin block, the same height.

The fast miner itself is unchanged: `cvn-6.26.0-2` from https://github.com/kalashnikovski/cvn-xmrig/releases still works.

Everything in this release was made after the last outside review and has not been reviewed by anyone but its authors.
