# Audit prompts

How to get an independent review of the Covenant Standard (CVN) code from several AI models, and how to compare what they say.

## Before you start

1. Make the source packs from the commit you want reviewed:

   ```
   python audit/make_audit_pack.py
   ```

   They appear in `audit/out/`. Every pack names its commit at the top. Give every reviewer packs made from the **same commit**, or their findings cannot be compared.

2. Choose what to give each reviewer by how much it can read at once:

   | Reviewer can read | Give it |
   |---|---|
   | The whole thing (about 160,000 tokens) | `cvn_audit_full.txt` with Prompt 1, or Prompt 3 for a later round |
   | About 60,000 tokens at a time | `cvn_audit_1_rules.txt` with the prompt. Then, in a **new** conversation, `cvn_audit_2_program.txt` with the prompt. The tests are optional. |
   | Less than that | It cannot review this code usefully. A model that only sees part of a file will invent the rest. |

3. Start each review in a **new conversation** with nothing else in it.

4. Save each answer to a file as it comes: `audit/out/findings_<reviewer>.md`.

5. When all have answered, run Prompt 2 with each reviewer: it shows them the others' findings and asks which are real. A finding that survives being attacked by another reviewer is worth far more than one nobody checked.

Treat every finding as a claim to be checked against the code, not as a fact. Reviewers of this kind are good at reading widely and bad at being sure: some findings will be wrong, and some real problems will be missed by all of them.

---

## Prompt 1: the review

Copy everything in the box, attach or paste the pack after it, and send.

```text
You are reviewing the source code of a proof-of-work blockchain before it launches. Real people will mine it on their own computers. Your job is to find what is wrong with it. You are not being asked to summarise it, explain it, or say what is good about it.

WHAT YOU HAVE

One file, attached or pasted below. It starts with a list of what it contains: a design paper (WHITEPAPER.md) saying what the rules are meant to be, then source code in Go. Read the design paper first. The code is supposed to do what the paper says; where it does not, that is a finding.

Read all of it before you answer. If your copy is cut off, or you could not read part of it, say so at the very top of your answer and name the last file you saw complete. Do not guess at code you did not see.

WHAT THE PROJECT IS

Covenant Standard (CVN): a coin with a fixed supply, mined with RandomX on ordinary processors, with no pre-mine. Coins are separate unspent outputs, as in Bitcoin, owned by addresses and spent with ECDSA signatures. Unusual features: coins nobody has moved for seven years have a fifth of their value moved to a "vault", a number kept by the rules that pays out a little to miners with every block; a fifth of transaction fees also goes to the vault; and the first block is tied to a future Bitcoin block so nobody can mine early.

The code runs on more than one network, and they do not have the same rules:

- "testnet-1" is an older test network with looser rules. It is being retired. Problems that exist ONLY under testnet-1's rules are low priority.
- "cvn_testnet" and "devnet" run the LAUNCH RULES: the settings StrictFormat, NoUncles, MaxBlockBytes, AmountsAsUnits, ScheduleAnchored, a PowSchedule with RandomX, and an Economics block. These are what will launch. Review these.

One thing to look for in particular: places where a network on the launch rules still passes through old code written for testnet-1, and behaves in a way the launch rules did not intend.

WHAT TO LOOK FOR, MOST IMPORTANT FIRST

1. Creating coins. Any way for the total of all coins plus the vault to exceed what the schedule has issued: in the reward, the fee split, the vault payout, the sweep, rounding, overflow, a block that is accepted while paying too much, or a reorganisation that leaves a balance or the vault wrong.

2. Taking or double-spending coins. Any way to spend a coin you do not own, spend one coin twice, replay a transfer, or alter a transfer after it is signed in a way that matters.

3. Honest nodes disagreeing. Any block or transfer that one honest node accepts and another rejects: results that depend on a node's clock, its operating system or processor, the order a map is iterated in, floating-point numbers, how text or JSON is encoded, what is in its memory pool, or the order in which blocks arrived. Any two different blocks with the same identifier.

4. Bookkeeping across reorganisations. When blocks are removed and others applied, is everything put back exactly: spent coins, swept coins, the vault, total work, balances? Follow one removal and one re-application by hand.

5. Stalling or wrecking the chain. Any way for one participant, especially one with little mining power, to stop blocks being produced, push the difficulty very high or very low, exploit the rule that halves difficulty when no block arrives, exploit the rule that steers block times back to schedule, or manipulate block timestamps for gain.

6. The launch. Can anyone mine before the Bitcoin anchor block exists? Can the first miner gain by choosing timestamps? Is the first block's difficulty what is intended? What happens if the Bitcoin block's own timestamp is wrong by an hour, or the block is replaced just after it appears?

7. Which chain is followed. Nodes follow the chain with most total work and refuse to replace more than 100 of their own blocks. Can a node be made to follow a worse chain, abandon a better one, or be split from the network permanently by something an attacker sends?

8. Attacks through the network. Messages that make a node do expensive work for nothing, use unbounded memory or disk, crash, or drop honest peers. The cost of checking a RandomX proof matters here. Look also at the work server for external mining software and at the HTTP endpoints.

9. Wallets and keys. Anything that could expose a private key, sign something the user did not intend, or send coins to the wrong place.

10. The design itself. Rules that work as written but produce a bad outcome: an incentive to cheat, a rule that is easy to get round, something that fails at year 7 or year 49 that could not show up in a short test.

WHAT IS ALREADY KNOWN - do not report these

- A proof-of-work network with little mining power can be attacked by anyone with more. This is accepted.
- All balances and transfers are public. There are no privacy features.
- A lost private key means lost coins.
- Which transfers a miner includes is the miner's choice and is not a network rule.
- The RandomX engine is third-party code and is not included; only CVN's changes to it are.
- Capacity is deliberately modest and block relay is simple.
- Signatures use ECDSA on the P-256 curve, not secp256k1. Report this only if you find an actual weakness in how it is used.
- Style, naming, comments, and the wording of messages. Do not report these.

HOW TO ANSWER

Start with one line: how much of the material you read, and anything you could not check.

Then your findings, most serious first. For each one:

  Title: one line.
  Severity: Critical (coins can be created or taken, or the chain can be split or stopped) / High / Medium / Low.
  Confidence: Certain (you traced it through the code) / Likely / Possible (you suspect it but could not confirm).
  Where: the file and the function.
  What happens: the exact steps. Who does what, with what values, and what each node then does. A finding without concrete steps is a suspicion, and should be marked Possible.
  Why the code allows it: quote the lines that matter.
  What to change: the smallest fix you can see.

Then a short list titled "Checked and found sound": the specific things from the list above that you examined and believe are correct, with one line each saying what you checked. This matters as much as the findings. An empty list will be read as meaning you did not look.

Then a short list titled "Could not determine": things you wanted to check and could not, and what you would need.

Rules for your answer:
- Be specific or say nothing. "Consider adding more validation" is not a finding.
- Do not report something as a problem unless you can say what an attacker or an unlucky honest user would do.
- If you are unsure whether the code does what you think, say so and say what you assumed.
- Do not soften findings, and do not pad the answer with praise or a summary.
- A short answer with three real problems is better than a long one with thirty doubtful ones.
```

---

## Prompt 2: testing each other's findings

Use this after every reviewer has answered Prompt 1. Start a **new conversation** with each reviewer. Give it the same pack it had before, then this prompt, then the other reviewers' findings pasted at the end. Remove the reviewers' names and label them Reviewer A, B and C, so that none defers to another.

```text
You have the source code of a blockchain project above. Below are findings from other reviewers who examined the same code. Some of these findings are correct. Some are mistaken: the reviewer misread the code, missed a check made elsewhere, or described something that cannot actually happen.

For each finding, decide which, by reading the code it refers to. Do not take any finding on trust, and do not reject one because it is surprising.

For each finding, answer:

  Finding: its title, and which reviewer.
  Verdict: Confirmed / Refuted / Partly right / Cannot tell.
  Reasoning: what the code actually does at that point. Quote the lines. If refuted, name the check or the fact the reviewer missed. If confirmed, say whether the steps they gave would work exactly as written, and correct them if not.
  Severity if real: your own judgement, which may differ from theirs.

Then, separately: reading their findings may have pointed you at parts of the code worth a closer look. List anything new you found as a result, in the same form as the findings above.

Finish with a single ranked list of the findings you consider real, most serious first.

Rules:
- "Cannot tell" is an acceptable answer. A confident wrong verdict is worse than none.
- Where two reviewers contradict each other, say which is right and why.
- Do not summarise or praise.

THE OTHER REVIEWERS' FINDINGS FOLLOW.
```

---

## Prompt 3: a second round, after fixes

Use this when a round of findings has been fixed and you want the fixed code reviewed. Rebuild the packs first, so they name the new commit. Start a **new conversation** with each reviewer, send this prompt, and attach `cvn_audit_full.txt`.

The pack now contains `audit/FINDINGS_ROUND_1.md`, the record of what the last round found and what was done about it. This prompt tells the reviewer to check those fixes and then to look where nobody has looked yet.

```text
You are reviewing the source code of a proof-of-work blockchain before it launches. Real people will mine it on their own computers. Your job is to find what is wrong with it. You are not being asked to summarise it, explain it, or say what is good about it.

This is the SECOND round of review. A first round found real problems, including one that let any peer replace the first block and create coins. The authors have changed the code since. Your job has three parts, in this order.

WHAT YOU HAVE

One file, attached or pasted below. It starts with a list of what it contains:

- WHITEPAPER.md: what the rules are meant to be. Read this first.
- README.md, Installation_Guide.md, MINING_GUIDE.md: what users are told. If the code does not do what users are told, that is a finding.
- audit/FINDINGS_ROUND_1.md: what the first round found, and what the authors say they did about each finding.
- Source code in Go, then its tests.

Read all of it before you answer. If your copy is cut off, or you could not read part of it, say so at the very top of your answer and name the last file you saw complete. Do not guess at code you did not see.

WHAT THE PROJECT IS

Covenant Standard (CVN): a coin with a fixed supply, mined with RandomX on ordinary processors, with no pre-mine. Coins are separate unspent outputs, as in Bitcoin, owned by addresses and spent with ECDSA signatures. Unusual features: coins nobody has moved for seven years have a fifth of their value moved to a "vault", a number kept by the rules that pays out a little to miners with every block; a fifth of transaction fees also goes to the vault; and the first block is tied to a future Bitcoin block, by that block's 80-byte header, so nobody can mine early.

The code runs on more than one network, and they do not have the same rules:

- "testnet-1" is an older test network with looser rules. It is being retired. Problems that exist ONLY under testnet-1's rules are low priority.
- "cvn_testnet" and "devnet" run the LAUNCH RULES: the settings StrictFormat, NoUncles, MaxBlockBytes, AmountsAsUnits, ScheduleAnchored, a PowSchedule with RandomX, and an Economics block. These are what will launch. Review these.

PART 1: ARE THE FIXES REAL?

For every finding marked as fixed in audit/FINDINGS_ROUND_1.md, read the code it concerns and decide for yourself:

- Does the change actually close the hole, in every path that reaches it? A check added at one entry point is no use if another entry point reaches the same code without it.
- Could an attacker get the same result a slightly different way?
- Does the test named for it really exercise the attack, or only something like it?
- Did the fix break or weaken anything else?

Do not accept the authors' description of a fix. Several of the first-round problems existed because code written for one situation was reached from another.

PART 2: WHAT DID THE FIXES ADD?

The fixes brought new rules and new code. New code has new mistakes. Look hard at:

- The rule that a sender signs the amount of every coin spent and the fee (the AmountUnits field, and the fee check in checkTransferAgainstCoins). Can a valid transfer be made invalid by someone else? Can a wallet be left unable to spend?
- The single written form for keys and signatures. Is there any transfer a correct wallet could produce that is now refused? Any form still accepted in two ways?
- The launch window: for the first 20 blocks, no relief for a late block and no difficulty below the starting value. What if the starting difficulty is far too high? Can the window be exploited at its last block, or just after it?
- The rewritten sync pass (sync_pass.go): choosing a source, setting one aside, finding where two chains part by halving the range, the time limit, the rollback and restore. Can a peer make a node set aside every honest source? Give an answer that makes the halving search return the wrong height? Leave the node on a worse chain, or with its bookkeeping wrong after a rollback that is then restored?
- Refusing a peer that sends invalid work, now also for a rival to the newest block. Can an honest peer be refused? Can an attacker get an honest peer refused by others?
- The mempool now evicts the cheapest transfer when full. Can that be used to remove someone else's transfer cheaply, again and again?
- The wait for the Bitcoin anchor (anchor_wait.go), which asks two public websites for a block header. What can those websites, or someone between the node and them, make the node do?
- Limits added to web requests and block downloads. Is anything still unbounded?

PART 3: WHAT HAS NOBODY LOOKED AT?

The first round concentrated on the network code and the start of the chain. Give the rest the same attention, most important first:

1. Creating coins. Any way for the total of all coins plus the vault to exceed what the schedule has issued: in the reward, the fee split, the vault payout, the sweep, rounding, overflow, a block accepted while paying too much. Work one block through by hand, with real numbers.

2. The Sabbatical sweep and the vault across a reorganisation. When blocks are removed and others applied, is every swept coin, every vault balance and every spent coin put back exactly? Follow one removal and one re-application by hand, for a block that contains a sweep.

3. Honest nodes disagreeing. Any block or transfer one honest node accepts and another rejects: results that depend on a clock, an operating system, the order a map is iterated in, floating-point numbers, how text or JSON is encoded, or what is in a node's memory. Any two different blocks with the same identifier. Any field of a block or transfer that is stored and relayed but not covered by the identifier.

4. Things that only happen late. The reward halving at each seven-year boundary and its end at year 49. The first sweep at year seven. A vault that has grown large. Heights and amounts near the limits of the number types used.

5. The mining rule. The RandomX key and its daily change, the mining blob, how the work value is read and compared, the switch of algorithm at a height. Can a block be valid under one node's key and not another's?

6. The work server for external miners, and the program that starts the fast miner. What can a miner connected to the work server make the node do? What can a file placed where the fast miner is looked for make the program run?

7. The wallets. Anything that could expose a private key, sign something the user did not intend, or send coins to the wrong place. The "combine my coins" action.

8. The guides. Any instruction in README.md, Installation_Guide.md or MINING_GUIDE.md that is wrong, that would lose a user their coins, or that describes something the code does not do.

WHAT IS ALREADY KNOWN - do not report these

- Everything listed under "Not changed, and why" in audit/FINDINGS_ROUND_1.md, unless you can show it is worse than described there.
- A proof-of-work network with little mining power can be attacked by anyone with more.
- All balances and transfers are public. A lost private key means lost coins.
- Which transfers a miner includes is the miner's choice and is not a network rule.
- The RandomX engine is third-party code and is not included; only CVN's changes to it are.
- Signatures use ECDSA on the P-256 curve. Report this only if you find an actual weakness in how it is used.
- Style, naming, comments, and the wording of messages.

HOW TO ANSWER

Start with one line: how much of the material you read, and anything you could not check.

PART 1 first, as a table or list: each first-round finding by its number and title, then one of
  Fixed / Fixed, but ... / Not fixed / Cannot tell
with the reason. Where it is anything other than "Fixed", give it as a full finding below.

Then your findings from all three parts together, most serious first. For each one:

  Title: one line.
  Severity: Critical (coins can be created or taken, or the chain can be split or stopped) / High / Medium / Low.
  Confidence: Certain (you traced it through the code) / Likely / Possible (you suspect it but could not confirm).
  Where: the file and the function.
  What happens: the exact steps. Who does what, with what values, and what each node then does. A finding without concrete steps is a suspicion, and should be marked Possible.
  Why the code allows it: quote the lines that matter.
  What to change: the smallest fix you can see.

Then a short list titled "Checked and found sound": the specific things you examined and believe are correct, with one line each saying what you checked. This matters as much as the findings. An empty list will be read as meaning you did not look.

Then a short list titled "Could not determine": things you wanted to check and could not, and what you would need.

Rules for your answer:
- Be specific or say nothing. "Consider adding more validation" is not a finding.
- Do not report something as a problem unless you can say what an attacker or an unlucky honest user would do.
- If you are unsure whether the code does what you think, say so and say what you assumed.
- Do not soften findings, and do not pad the answer with praise or a summary.
- A short answer with three real problems is better than a long one with thirty doubtful ones.
```

After every reviewer has answered Prompt 3, run Prompt 2 again exactly as before, with their new findings.

---

## After the reviews

Bring the findings back to the development session, with each reviewer's verdicts from Prompt 2. Each one that survives is then:

1. reproduced with a test that fails,
2. fixed,
3. shown fixed by that test passing.

A finding that cannot be turned into a failing test is either not real or not yet understood.
