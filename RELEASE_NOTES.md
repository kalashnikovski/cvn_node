Release candidate 10. A test release: the CVN Testnet has not started, and coins on any CVN test network have no value and carry over to nothing.

**This release changes no rule.** It agrees with release candidate 9 about every block, so machines on the two can share a network, and a development-network (`devnet`) chain made with release candidate 9 carries on unchanged. Nothing needs deleting: update and start again.

## For your wallet

- **Recovery words.** A private key is now also shown as 24 ordinary English words, which are the same key written another way, with a check built in. A misspelt, missing or misplaced word is refused at once. Wherever a wallet asks for the private key, the 24 words work too. A wallet made with an earlier release has its words as well: in the text wallet, choose "Recovery words and saved key". The word list is the one Bitcoin wallets use (BIP39).
- **A saved key.** The text wallet can keep your key on your computer in `wallet_key.json`, locked with a password. After that, either wallet takes the password where it asks for the key, so the key need not be pasted for every transfer. The file is a convenience and not a backup: keep the 24 words on paper.

Section 4 of `Installation_Guide.md` explains both.

## For miners

- **The miner window says what the node is doing.** A STATUS line in the heading reads UP TO DATE, CATCHING UP (with the block it has reached, a bar and a percentage) or WAITING. While catching up or waiting, the reason is shown on a line of its own, and the regular line says "Not mining yet" in place of "Mining at 0 H/s". The download at start-up shows the same bar.
- **A 60-second average hashrate** heads the averages, above 15 minutes, 60 minutes and 24 hours.
- **The heading stays on screen** however fast lines arrive, and the fast miner's "stale job" lines are no longer shown. They are not faults: they are answers to work the chain had already moved past.
- The fast miner's repeated "not ready" errors while the node is catching up are no longer shown either. A connection lost at any other time still is.

## For anyone running a node

- **A node no longer treats itself as a peer.** A seed given the list of all the seeds, itself among them, used to ping itself, send itself every block, and log every minute or two that it had closed an idle connection to its own address.
- That log line now says what it means: nothing was heard from a node for 90 seconds, so nothing is sent to it until it is heard from again. Nodes do not hold connections open to each other.
- A second copy of a block already held, arriving after the chain has moved on, is no longer reported as a rejected block.

## Checking a download

- **New: every file in this release is signed by GitHub** as built by this project's build recipe from this repository at this tag. With GitHub's command-line tool: `gh attestation verify cvn_node_windows.zip --repo kalashnikovski/cvn_node`.
- As before, `SHA256SUMS_windows.txt` and `SHA256SUMS_linux.txt` list the SHA-256 of each package and of each program inside it, and anyone can rebuild the programs from the tag and compare. Section 11 of `Installation_Guide.md` says how.

## Updating

1. Stop the miner or seed.
2. Replace the programs with this release's (a seed: `bash /opt/cvn_node/deploy/update_seed.sh`).
3. Start again exactly as before. Keep the chain files and, on a development network started from a Bitcoin block, the same height.

The fast miner itself is unchanged: `cvn-6.26.0-2` from https://github.com/kalashnikovski/cvn-xmrig/releases still works.

The recovery words, the saved key and the changes to the miner window were made after the last review and have not been reviewed by anyone but their authors.
