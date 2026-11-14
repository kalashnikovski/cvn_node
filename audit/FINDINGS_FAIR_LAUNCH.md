# Fair-launch scorecard: findings and what was done

Three reviewers (Claude, Google AI, Grok) judged the code at commit `d0a7aa0` against the 33 code items and 6 conduct items of the fair-launch scorecard (Prompt 4 in `AUDIT_PROMPT.md`). This is the record of every item that at least one of them did not pass, what is true of it, and what was done. Tests named below fail on the old code and pass on the new.

No reviewer found a way to create coins before the public can mine, a share for anyone but miners, a privileged key, or a supply different from the paper's. All three passed A1, A2, B1, B4, C1 to C5, C8, D1 to D4 and F1.

## Fixed in the code

| Item | Finding | What was wrong | Fix | Test |
|---|---|---|---|---|
| C6 | **A seed could hold newcomers on a chain of its own** | A new node downloads its chain from a seed. The rule against replacing more than 100 of one's own blocks then protected that download, so a newcomer handed a lighter chain by a seed would have refused the real one for ever. The seeds are run by one person. | Blocks downloaded at start-up are not counted against that limit while the program runs: if any node later proves more work, the newcomer follows it. Blocks already in the database at start, and blocks that arrive afterwards, count as before. Start-up also asks other known nodes once the seeds have nothing more. | `TestANewcomerIsNotLockedOntoASeedsChain`, `TestTheReorgLimitStillCountsBlocksThatWereNotJustDownloaded` |
| C6, B2 | **Seeds could keep miners waiting** | A seed claiming to be ahead held a miner for up to ten minutes, and silent seeds for five, whenever the newest block was six minutes old. In a slow start that favoured whoever the seeds answered. | A hold lasts two minutes at most without progress. In the first 20 blocks, where difficulty cannot fall, there is no hold at all. | `TestNobodyCanHoldMinersBackForLong` |
| A3 | **The Bitcoin header check was far weaker than Bitcoin** | A header had to meet the target it stated itself and show 16 zero digits: about 2^64 hashes, where a Bitcoin block takes about 2^79. A header could have been made with rented hardware, and headers of smaller chains with Bitcoin's format passed. A real header of another height was accepted when supplied by hand. | The stated target must be no easier than a quarter of Bitcoin's difficulty of November 2026. A network can fix the earliest date its block may carry (`NotBefore`, set when the height is announced). A node that itself saw Bitcoin short of the height takes only a block dated within three hours of its clock. | `TestAnchorHeaderMustCarryBitcoinsWork`, `TestAnchorCannotBeABlockFromBeforeTheAnnouncement`, `TestWaitingNodeRefusesAnOldBlock` |
| A4 | **Starting depended on two named websites** | Both of two explorers had to answer. If either was down, waiting nodes waited while anyone supplying the header by hand mined alone. Polling fell back to once a minute if the explorers stopped reporting Bitcoin's height. | Five explorers are asked at once and any two agreeing are enough, provided no other header also has two. Once Bitcoin is close the node keeps asking every ten seconds, with a little unevenness so that waiting nodes do not all ask at once. The by-hand method is now in the guide, with the command for one's own Bitcoin node. | `TestAnchorLookupSurvivesExplorersBeingDown` |
| A5 | **Two first blocks: nobody was told** | If Bitcoin replaced the anchor block, nodes could start from two headers. The only sign was a log line the dashboard clears. | Every node compares its first block with each seed's every five minutes and shows a warning naming both, repeated for as long as they differ. It does not stop mining by itself: a seed can be the one that is wrong. | `TestANodeSaysWhenSeedsHoldAnotherFirstBlock` |
| F3 | **A mistyped mining address could be given a valid check** | An address without its check, given with `--miner-address` or in the environment, was completed. A typing mistake in it would then have passed, and every reward gone to nobody. | Only an address the program itself saved is completed. One that was typed is refused with instructions. | `TestATypedMiningAddressIsNeverCompleted` |
| B6 | **The launcher made wallets for the wrong network** | `LaunchMiner.bat` created a wallet without passing on `--network`, so on a launch network it saved an address in the older form. `--network mainnet` quietly chose the first test network. | The launcher passes its options on. There is no network called `mainnet`, and the name is refused. | `TestThereIsNoNetworkCalledMainnet`; launcher by reading |
| F4 | **Results of database writes when saving a block** | Four writes in the saving of a block ignored their result. | They are checked, and a failure abandons the whole block. (See below for what this did and did not risk.) | |

## Fixed in the documents and the build

| Item | Finding | Fix |
|---|---|---|
| E5 | The paper said the proof is the RandomX hash of the block's identifier. It is the hash of a 43-byte message built from it. | The paper gives the exact layout. |
| E5 | The paper's wording suggested the first RandomX key lasts 101 blocks. By its own formula the first block is the key up to height 2,020. | Stated plainly. |
| E5 | The paper left out the launch window (20 blocks) and the ramp after it (40 blocks), which are rules, and called a step of up to a halving "a little". | Both described, with the limits. |
| E5 | The paper said an address is a hash of the public key. It is a hash of the key as written: 128 hex characters. | Stated. |
| E5 | "Not independently audited" beside three recorded rounds of review. | The status says what has happened: reviewed by AI models, not audited by an independent firm or person. |
| E5 | The roadmap said three seeds (there are seven) and "no ban for sending invalid blocks" (there is one for false proof of work). | Corrected. |
| E3 | The paper and README said the engine differs from the published one in two settings. It also places memory in large pages on Windows and for mining threads, which changes nothing it computes. The test against RandomX's published values was never run automatically. | Both documents say so. The test now runs on every change and before every release. |
| E2 | Packages carried only the project's own licence. The RandomX engine's licence requires its notice with the program; the libraries likewise. The fast miner's licence (GPL 3) was not mentioned. | `THIRD_PARTY_NOTICES.md` lists every part that comes from someone else with its licence text, and goes into every package. It states that the fast miner is a separate program under the GPL with its source in its own repository. |
| E4 | No way to check a package against the source. | Programs are built with `-trimpath`, and each release publishes SHA-256 sums of the packages and of the programs inside them. |
| C7, B6 | The guide did not name the Bitcoin explorers, did not say that a wallet without a local node shows a seed your address, and did not explain how to supply the header by hand or what to do if the seeds are unreachable or untrusted. It did not say the desktop wallet is Windows only, or that every command without `--network` runs on the first test network. | All added to the installation guide: "On the day a network starts", "If the seed nodes cannot be reached, or you do not trust them", "What the program contacts". |

## Reported, and not so

| Item | Claim | What the code does |
|---|---|---|
| F4 (Google) | A full disk part-way through a block leaves half of it stored. | The database applies a block in one transaction that is committed whole or not at all, and a failed commit is reported and the block refused. The individual writes can fail only for a malformed key or an oversized value. The four in block saving are now checked anyway; the twelve in the undo paths are not, for the same reason they could not fail. |
| F4 (Google) | The cached choice of transfers can make a miner build an invalid block for five seconds after a transfer is replaced. | The cache holds one consistent choice made against the current tip and is dropped when the tip changes. A replaced transfer in it is still valid against the chain; at worst the cheaper of an owner's two versions is mined. |
| F4 (Google) | Blocks before the transfer rules start can attach to the wrong parent. | Those rules start at block 1 on every launch network. The remark applies to the first test network's early history only. |
| E4, B6 (Google) | The wallet screens and their bindings are missing, so the program cannot be built. | They are in the repository (`frontend/dist`, `frontend/wailsjs`); the audit pack leaves generated files out, and says so. |
| C7 (Google) | An undisclosed connection to 192.0.2.1. | Nothing is sent: it asks the operating system which local address it would use, the standard way. |
| A3 (Grok: pass) | — | Two of three reviewers were right that the check was too weak; see above. |

## Not changed, and why

| Item | Finding | Decision |
|---|---|---|
| B3 | A miner can gain a bounded amount by choosing timestamps (two minutes ahead at most; 1% a block on the schedule term). | Accepted, as in round 3. |
| F2 | Enough addresses can fill a node's connections. | Open, as in round 3. Seeds are always let in. |
| E4 | The wallet screens are a prebuilt bundle in the repository, not rebuilt from their source by the release. The seed installer builds from the newest source, not a tagged release. | Open. Both belong with the first packaged release of the launch rules. |
| B5 | The fast miner is recognised by what it says about itself, not by its file's hash. | Open, as in round 3: it needs the miner's hashes published with each of its releases. |
| A4 | Whoever gets the Bitcoin block first can start a few seconds before the rest. | Accepted. It is seconds, at a difficulty that does not fall in the first 20 blocks, and the by-hand method is open to everyone and documented. |

## What code cannot show (section G, and items A6 and B5)

These are for the person running the launch. Each reviewer said the same things in different words.

| Item | What has to be done | How anyone can check |
|---|---|---|
| A6, G1 | Fix the Bitcoin height, and the earliest date its block may carry, in a tagged release, and announce both in a dated public place well ahead. No later change to the network's settings. | The tag's date against the Bitcoin block's date; `--show-genesis`. |
| G2 | Publish the exact source, the packages and their SHA-256 sums with that release. | Build from the tag and compare. |
| B5 | Publish the fast miner's complete source, tagged, with builds and their SHA-256 sums, no later than that release. | Build it and compare; compare its speed with the published build. |
| G3 | Say beforehand that the maintainer will mine from the first block, with which address and roughly how much power, using the published programs. One seed (Melbourne) also mined. **Done:** it is no longer a seed. No seed mines, and the maintainer's one mining machine joins the network like anyone else's. The statement is in the README. | The first blocks on the explorer. |
| G4 | State that no coins have been sold, promised or set aside. **Done:** in the README. | The list of addresses after the first day. |
| G5 | Say who runs each seed, and add seeds run by others as they come forward. | Connect without the listed seeds, with `--connect`. |
| G6 | State that test-network coins carry over to nothing (**done:** in the README). A mainnet needs its own name, message key, ports and database file. | The network's settings in the source. |
