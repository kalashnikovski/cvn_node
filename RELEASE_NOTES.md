Release candidate 12. A test release: the CVN Testnet has not started, and coins on any CVN test network have no value and carry over to nothing.

**This release changes no rule.** It agrees with release candidates 9, 10 and 11 about every block, so machines on any of them can share a network, and a development-network (`devnet`) chain carries on unchanged. Nothing needs deleting: update and start again. A test runs this release and release candidate 11 together on one network.

Most of what is new is in how nodes talk to each other, and in what three rounds of outside review found.

## For someone joining a network

- **The first download is several times faster.** Each block's proof of work used to be checked on one core, one block after another: nine blocks a second on a small rented server, which would be sixteen hours for a year of blocks. The proofs of a group of blocks are now worked out on as many cores as mining may use (`--threads` limits this too) and then checked exactly as before.
- **A node downloads from the seed that answers soonest**, among seeds that hold the same chain. It used to take the first in the list, so a node in London fetched the whole chain from Singapore. A seed with more work is still chosen however slow it is.
- **A node that cannot open the port other nodes reach it on says so and stops.** It used to carry on with no connections.
- **A node counts Bitcoin's blocks to its starting block itself.** A network that starts at an announced Bitcoin block height used to take the block at that height on the word of two block explorers. The node now follows Bitcoin's headers from a block fixed in the software (975,744) to that height, checking each one's link, work, target and date as a Bitcoin node does. This matters for the start of the CVN Testnet; a network already running is not affected.

## For miners

- **The fast miner on a slow machine is no longer dropped every ten minutes.** The work server closes a connection it has heard nothing on for ten minutes, and a miner speaks only when it finds a block. The fast miner is now started so that it says it is still there.
- **The fast miner is found if it was unpacked one folder too deep** (`xmrig-cvn/xmrig-cvn/xmrig`). The Linux download makes its own `xmrig-cvn` folder; the mining guide used to say to make one first, which left the built-in miner running without a word. The guide is corrected.
- **A fast miner that keeps stopping is given up on** after five times in ten minutes, with a message, and the built-in miner takes over. One that had connected and then stopped is no longer reported as never having connected.
- **A block you mine is placed with the chain held still.** A block from a neighbour could land between your block being checked and being stored.
- **A block reward could be counted twice in the balance a node shows.** Only what was shown was wrong; blocks are checked against the coins themselves.

## For anyone running a node

- **Blocks travel over the connections already open.** A node used to open a new connection to every node it knew for every block. It now sends blocks and transfers down open connections, in whichever direction they were opened, and calls up at most eight other nodes afresh.
- **A large block is announced first.** A block over 16,384 bytes is announced by its height and identifier and handed over only to nodes that ask for it, so nobody is sent a full block several times. A smaller block is simply sent. Nodes on release candidate 11 are sent whole blocks as before.
- **Limits on what a stranger can make a node do.** Every connection has a message allowance, including the ones this node opened. Transfers have an allowance of their own, and are checked away from the connection so that a burst of them cannot hold up a block. A reply to a request for blocks is cut to 512. The list of an address's coins has an allowance for each caller. A message larger than any node reads is not sent.
- **An honest source is no longer set aside by mistake.** Several cases in which a node stopped asking a good neighbour for ten minutes are closed: its own newest block changing while blocks were on their way, a block arriving while two chains were being compared, and a source that had nothing to send from a height it had reported.
- **On Linux and macOS the database is given room to grow into when it is opened**, so that a block does not wait for a backup that is being written. Windows is unchanged.
- **Built with Go 1.27.4.** Go's own checker reported eleven faults in the 1.27.1 standard library that this program reaches, one of which let an HTTP server be crashed from outside. All are fixed by 1.27.4.
- **The seed update script starts only the services that are switched on**, and `deploy/install_https.sh` gives a seed a name and a certificate that renews by itself.

## In the wallet

- **A wrong password made only of the characters 0-9 and a-f was taken as a private key.** It is refused now.
- **The recovery-words screen shows the address the words belong to**, so that a set of words can be checked against an address before it is relied on.

## The website

The project has a website at https://covenantstandard.online, with a page that asks each seed what it holds and links to its block explorer. Its addresses all end in `covenantstandard.online`. The address `covenantstandard.com` belongs to someone else. The project sends no email.

## Reviewed and tested since the last release

- **Three rounds of outside review.** In round 6 five reviewers examined the code, and in round 7 the changes since. Neither found a fault in the rules every node enforces. The faults they did find, in how nodes talk to each other and in the wallet, are fixed, each with a test that failed before; `audit/ROUND_6_RESULTS.md` and `audit/ROUND_7_RESULTS.md` have the record. A third review, of the round 7 fixes, found three more things, which are fixed too. Those last three fixes have been read by nobody but their authors.
- **Real nodes on one machine.** The tests that start the program several times over now also cover two halves of a network that each mine a chain and then meet, the limit on how many of its own blocks a node will replace, and this release sharing a network with release candidate 11. `go test -tags multinode -run MultiNode -timeout 30m ./node/`.
- **Full blocks.** On the development network, across six seeds on three continents, ten blocks of about 290,000 bytes each reached every seed within 3.7 seconds of being mined, against a block interval of 45. With release candidate 11 the figure was 5 seconds.
- **The race detector.** The tests pass under Go's race detector.

## Checking a download

Every file in this release is signed by GitHub as built by this project's build recipe from this repository at this tag: `gh attestation verify cvn_node_windows.zip --repo kalashnikovski/cvn_node`. `SHA256SUMS_windows.txt` and `SHA256SUMS_linux.txt` list the SHA-256 of each package and of each program inside it, and anyone can rebuild the programs from the tag and compare. Section 11 of `Installation_Guide.md` says how.

## Updating

1. Stop the miner or seed.
2. Replace the programs with this release's (a seed: `bash /opt/cvn_node/deploy/update_seed.sh`).
3. Start again exactly as before. Keep the chain files and, on a development network started from a Bitcoin block, the same height.

The fast miner itself is unchanged: `cvn-6.26.0-2` from https://github.com/kalashnikovski/cvn-xmrig/releases still works.
