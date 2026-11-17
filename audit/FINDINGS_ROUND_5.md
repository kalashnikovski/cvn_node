# Audit round 5: the exponential difficulty step

Three reviewers (Claude, Google AI, Grok) examined commit `ab485df` (release candidate 7) with Prompt 6 of `audit/AUDIT_PROMPT.md` and the small pack `cvn_audit_rule_h.txt`. The round had one subject: each block's difficulty step applied as `e^x`, in whole numbers (`ExponentialSteps`), which answered finding 1 of round 4.

Each finding was checked against the code, and each date pattern was run against the real arithmetic, before anything was done. None of the fixes below changes a rule nodes enforce: a node of release candidate 7 and a node with these fixes accept exactly the same blocks.

## What all three agreed on

- `expStepFactor` gives the same whole number on every 64-bit machine. One reviewer ran it for every step the rule can produce (102,401 values): always positive, strictly increasing, at most 16 terms, never stopping early.
- The seven pinned values are right.
- The setting is on for both networks and no path works out a difficulty with it off.
- A chain that really loses most of its miners still recovers, more slowly than before: after a fall to a hundredth, about 100 minutes for 30 blocks in one reviewer's simulation.

## Fixed

| # | Finding | Severity | What was wrong | Fix | Test |
|---|---|---|---|---|---|
| 1 | **A miner with part of the hashrate took about 1% more by claiming the late-block halving two minutes early** (Claude) | Low | A block dated six minutes after its parent is mined at half difficulty, and a block may be dated two minutes ahead of the clock. A miner who dated ahead reached the halving two minutes before miners who dated by the clock. Confirmed against the real arithmetic: +1.2% at 60% of the hashrate, +0.9% at 75%, nothing at 100%. | Every miner now does the same: when dating a block ahead, by no more than a miner will go (110 seconds), reaches a halving the true time does not, the block is dated at the moment that halving begins (`minerBlockDate`, used by the built-in miner and the work server). Nobody has a head start. This is each miner's choice of date, which the rules already allowed; nodes check nothing new. With it the pattern gains less than 0.4% at any share tried. | `TestMinersClaimALateBlockHalvingAsEarlyAsAllowed`; `TestChoosingDatesDoesNotPayTheLargestMiner` now covers this pattern at 94%, 75%, 60% and 35% |
| 2 | **On a 32-bit platform difficulty stops fitting once it passes 2,147,483,647** (Claude) | Medium if such builds existed | Difficulty is held in the platform's natural whole number. Above about 48 million hashes a second, a 32-bit node would refuse every block the others accept. Not from this change. No 32-bit program has been published. | The program no longer builds for a 32-bit platform (`node/needs_64_bit.go`). | Checked by building for one: refused. |
| 3 | **The walk back through late blocks did not stop at a change of mining algorithm** (Google AI) | Low (reported as High) | The reviewer's own trace shows the answer was right: the forward pass restarts difficulty at the new algorithm's starting value. But the walk went on into the old algorithm's blocks for nothing, and was made at all where the next block restarts. Neither network has a second algorithm. | The walk ends at the first block of a new algorithm, and is skipped where the next block restarts. | `TestALateFirstBlockOfANewAlgorithm` |
| 4 | **The tests would not catch finding 1, or a change in the final rounding** (Claude, Google AI) | Low | Only three shares and five patterns were tried, with a 2% pass mark; no value of the whole step was pinned, only the series inside it. | The dates test tries the new pattern at four more shares against the same miner dating honestly, with a 1% mark. Eight values of the whole step are pinned, at the floor, the ceiling and in between. A comment that said "within six parts in 2^64" now says eight, which is the true bound over the range. | `TestTheWholeStepIsPinned` |

## Not confirmed

| Finding | Reported as | What was found |
|---|---|---|
| **The late-block halving with the 495-second cap still lets a dominant miner harvest cheap blocks** (Google AI) | High, Likely | Not borne out. The pattern described (ten blocks one second apart, then one block 650 or 720 seconds after its parent) was run against the real arithmetic at five shares: it loses between 1% and 13% against honest dating. The cheap block is real, but the wait for the clock to allow its date costs more than it saves, and the cap on a slow block works against the miner, not for it. The other two reviewers ran the same family of patterns and found no gain. `TestRound5DatePatterns` prints the figures. |

## Not changed, and why

| Finding | Severity | Decision |
|---|---|---|
| **Each step rounds difficulty down, so at the 5,000 minimum a very small step up is lost** (Google AI) | Low | Left as it is. The loss is under one unit a step; at the lowest difficulty that is 0.02%, and the term that holds the chain to its schedule absorbs it. Two reviewers looked for a way to use it and found none. Rounding to the nearest instead would change the result of some steps, so nodes of release candidate 7 and later nodes would disagree about those blocks: a cost with nothing to set against it. |
| **`SelectNetwork` changes the network settings without a lock** (Google AI, as a note) | None | It is called once, before the database is opened or any connection made. |

## Not covered by this round

- The reviewers had seven files. Networking, wallets and the rest of validation were outside it.
- No search over date patterns is exhaustive. The argument that none can pay rests on two facts: steps depend only on their sum, and a date cannot run more than two minutes ahead of the clock.
- The miner's new choice of date has been tested against the rule and in simulation, not yet on a live network.
