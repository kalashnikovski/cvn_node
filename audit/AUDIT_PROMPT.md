# Audit prompts

How to get an independent review of the Covenant Standard (CVN) code from several AI models, and how to compare what they say.

## Before you start

1. Make the source packs from the commit you want reviewed:

   ```
   python audit/make_audit_pack.py
   ```

   They appear in `audit/out/`. Every pack names its commit at the top. Give every reviewer packs made from the **same commit**, or their findings cannot be compared.

2. Choose how to give it to each reviewer:

   | Reviewer | Give it |
   |---|---|
   | Takes a file of about 950,000 characters whole | `cvn_audit_full.txt`, attached, with the prompt |
   | Cuts a long file or paste short | The three parts (each about 300,000 to 350,000 characters), one message each, as described under "Sending the pack in parts" below |
   | Takes about 275,000 characters at most | `cvn_audit_focus.txt`: the rules only. A narrower review; see Prompt 5B |
   | Takes about 80,000 characters at most | `cvn_audit_rule_b.txt`: one rule only; see Prompt 5C |
   | Less than that | It cannot review this code usefully. A model that only sees part of a file will invent the rest. |

   The script prints the size of every pack in characters. Compare those figures with what the tool accepts before you start; a tool's limit on typed or pasted text is often far lower than its limit on an attached file, so attach where you can.

   **Check every time that the reviewer saw all of it.** Each pack numbers its files and ends with the line `END OF PACK`, and the prompts ask the reviewer to say which file it saw last. If its answer does not name the last file, or says the copy was cut short, its review covers only part of the code: start again, in parts.

3. Start each review in a **new conversation** with nothing else in it.

4. Save each answer to a file as it comes: `audit/out/findings_<reviewer>.md`.

5. When all have answered, run Prompt 2 with each reviewer: it shows them the others' findings and asks which are real. A finding that survives being attacked by another reviewer is worth far more than one nobody checked.

Treat every finding as a claim to be checked against the code, not as a fact. Reviewers of this kind are good at reading widely and bad at being sure: some findings will be wrong, and some real problems will be missed by all of them.

---

## Sending the pack in parts

Some tools cut a long file or paste short without saying so. One reviewer reported its copy stopped part-way through the sixth file and resumed among the tests. When that happens, send the three parts one after another **in the same conversation**:

1. `cvn_audit_1_rules.txt` with the first message below.
2. `cvn_audit_2_program.txt` with the second message below.
3. `cvn_audit_3_tests.txt` together with the review prompt itself (Prompt 1, Prompt 3 or Prompt 5A).

Attach each file if the tool allows it; paste it only if it does not. Wait for the reply before sending the next.

First message, with part 1:

```text
I am going to give you the source code of a software project to review, in three parts, one message each. This is part 1 of 3. Do not review anything yet. Read it, then reply with exactly: the number of files in this part, the name of the last file you can see, and whether you can see the line "END OF PACK" at the end. Nothing else.
```

Second message, with part 2:

```text
This is part 2 of 3. As before: do not review anything yet. Reply with the number of files in this part, the name of the last file you can see, and whether you can see the line "END OF PACK" at the end. Nothing else.
```

Third message: the review prompt, with part 3 attached, and this line added at the very top of the prompt:

```text
This is part 3 of 3, the tests. With it you now have everything: parts 1, 2 and 3 together are the material this prompt describes.
```

If any reply says the last line was not visible, that part was cut short too. That tool cannot take this code; use another reviewer.

---

## Prompt 1: the review

Copy everything in the box, attach or paste the pack after it, and send.

```text
You are reviewing the source code of a proof-of-work blockchain before it launches. Real people will mine it on their own computers. Your job is to find what is wrong with it. You are not being asked to summarise it, explain it, or say what is good about it.

WHAT YOU HAVE

One file, attached or pasted below. It starts with a list of what it contains: a design paper (WHITEPAPER.md) saying what the rules are meant to be, then source code in Go. Read the design paper first. The code is supposed to do what the paper says; where it does not, that is a finding.

Read all of it before you answer. The material numbers its files ("FILE n OF N") and ends with the line "END OF PACK". Begin your answer by stating the number of the last file you can see and whether you can see that last line. If you cannot, your copy was cut short: say so plainly, review only what you have, and do not guess at code you did not see.

WHAT THE PROJECT IS

Covenant Standard (CVN): a coin with a fixed supply, mined with RandomX on ordinary processors, with no pre-mine. Coins are separate unspent outputs, as in Bitcoin, owned by addresses and spent with ECDSA signatures. Unusual features: coins nobody has moved for seven years have a fifth of their value moved to a "vault", a number kept by the rules that pays out a little to miners with every block; a fifth of transaction fees also goes to the vault; and the first block is tied to a future Bitcoin block so nobody can mine early.

The code runs on two networks, and both have the same rules:

- "testnet-1" was an older test network with looser rules. It has been retired and its rules removed from the code; a few comments still mention it as history.
- "cvn_testnet" and "devnet" run the LAUNCH RULES, which are now the only rules in the code. The settings that differ between them are names, keys, ports, files, seeds and starting difficulty (node/chain_params.go). These are what will launch. Review these.

One thing to look for in particular: anything left over from the older network's rules that the removal missed, and any place where taking those rules out changed what the launch rules do.

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

Read all of it before you answer. The material numbers its files ("FILE n OF N") and ends with the line "END OF PACK". Begin your answer by stating the number of the last file you can see and whether you can see that last line. If you cannot, your copy was cut short: say so plainly, review only what you have, and do not guess at code you did not see.

WHAT THE PROJECT IS

Covenant Standard (CVN): a coin with a fixed supply, mined with RandomX on ordinary processors, with no pre-mine. Coins are separate unspent outputs, as in Bitcoin, owned by addresses and spent with ECDSA signatures. Unusual features: coins nobody has moved for seven years have a fifth of their value moved to a "vault", a number kept by the rules that pays out a little to miners with every block; a fifth of transaction fees also goes to the vault; and the first block is tied to a future Bitcoin block, by that block's 80-byte header, so nobody can mine early.

The code runs on two networks, and both have the same rules:

- "testnet-1" was an older test network with looser rules. It has been retired and its rules removed from the code; a few comments still mention it as history.
- "cvn_testnet" and "devnet" run the LAUNCH RULES, which are now the only rules in the code. The settings that differ between them are names, keys, ports, files, seeds and starting difficulty (node/chain_params.go). These are what will launch. Review these.

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

## Prompt 4: the fair-launch scorecard

Use this before a launch, after the rounds above. It asks a different question from the others. They ask "what is broken?". This one asks "does this project meet every test of a fair launch of a layer-1 coin built from its own code, and if not, exactly what is missing?".

Rebuild the packs first. Start a **new conversation** with each reviewer, send this prompt, and attach `cvn_audit_full.txt` (or send the three parts as described above).

Two things to know before reading the answers:

- **No review can certify "100%".** A reviewer reads code and documents. Some tests of a fair launch are about what the people running the launch do, and no reading of code can prove those. The prompt makes the reviewer keep the two apart: what the code proves, and what the project must publish or do so that others can check. A reviewer who answers "100% fair" without that separation has not done the work.
- A "Fail" is not a verdict on the project. It is a list of work. The answer is useful exactly as far as each Fail says what would turn it into a Pass.

```text
You are assessing a proof-of-work cryptocurrency before it launches. The project says of itself: it is a layer-1 chain written from its own code, not a fork of another coin, and it will have a fair launch: no coins made before the public can mine, no share for the founders, no special powers for anyone. Your job is to test those claims against the code and documents you are given, item by item, and to say for every item that falls short exactly what must change. You are not being asked to summarise the project or to say what is good about it.

WHAT YOU HAVE

One file, attached or pasted below (or three parts sent one after another). It starts with a list of what it contains:

- WHITEPAPER.md: what the rules are meant to be. Read this first.
- README.md, Installation_Guide.md, MINING_GUIDE.md, ROADMAP.md: what the public is told.
- LICENSE files and THIRD_PARTY_NOTICES.md.
- internal/randomx/DIFFERENCES.md: the RandomX engine compared file by file with the original it was copied from, with every changed line, and audit/compare_randomx.py, the script that wrote it. The engine's unchanged files are not in the pack.
- audit/FINDINGS_ROUND_1.md, _2.md, _3.md: what three earlier rounds of review found and what was done.
- audit/FINDINGS_FAIR_LAUNCH.md: what two earlier runs of this same scorecard found and what was done. Do not take its word: for each item it says was fixed, check the code, and for each claim it says was mistaken, decide for yourself.
- Source code in Go, the scripts that set up servers, the build instructions, then the tests.

Read all of it before you answer. The material numbers its files ("FILE n OF N") and ends with the line "END OF PACK". Begin your answer by stating the number of the last file you can see and whether you can see that last line. If you cannot, your copy was cut short: say so plainly, assess only what you have, and mark every item you could not check as "Cannot tell".

WHAT THE PROJECT IS

Covenant Standard (CVN): a coin with a fixed supply, mined with RandomX on ordinary processors. Coins are separate unspent outputs, as in Bitcoin, owned by addresses and spent with ECDSA signatures. Unusual features: coins nobody has moved for seven years have a fifth of their value moved to a "vault", a number kept by the rules that pays out a little to miners with every block; a fifth of transaction fees also goes to the vault; and the first block is tied to a future Bitcoin block, by that block's 80-byte header, so that nobody can mine early.

The code runs on more than one network:

- "testnet-1": an older test network with looser rules. It has been retired and its rules removed from the code.
- "cvn_testnet" (the CVN Testnet, about to launch) and "devnet" (for development): these run the LAUNCH RULES, which are now the only rules in the code. The real network ("mainnet") will run these same rules and is not yet defined in the code. Assess the launch rules, and assess the CVN Testnet's launch as the dress rehearsal for mainnet's: whatever would be unfair in one would be unfair in the other.

The project is run by one person, who also runs the six "seed" computers that new nodes first connect to. The seeds do not mine. That person will mine from the first block like anyone else, with one computer that is not a seed, and has published a statement to that effect in README.md.

HOW TO JUDGE EACH ITEM

Give every numbered item below one of these, and nothing else:

  PASS           the code and documents you read prove it. Say which file and function proves it.
  FAIL           the code or documents show it is not so. Give the steps, quote the lines, and say what to change.
  PARTLY         true with an exception. Name the exception as you would a FAIL.
  NOT IN CODE    it cannot be proven or disproven by reading code, because it depends on what people do.
                 Say exactly what the project should publish or do, and when, so that an outsider could check it.
  CANNOT TELL    you could not determine it from what you have. Say what you would need.

Do not give PASS on the strength of what a document or a comment says. A claim in the design paper is a thing to test, not evidence. Do not give FAIL without being able to say what a person would do to take advantage, or what an honest person would lose.

A. NO COINS BEFORE THE PUBLIC CAN MINE

A1. The first block creates no coin that anyone can spend, on every network that uses the launch rules.
A2. No coin exists at any height that was not paid out by the reward schedule to the miner of a block, or was not an unspent coin before. Trace where coins come into being and name every place.
A3. Nobody can make a valid block 1 before the Bitcoin block named for the launch exists. Consider: a node given a made-up header, a header from another Bitcoin block, a header from a Bitcoin fork or a low-difficulty Bitcoin test network, a first block with a chosen timestamp.
A4. Nobody who learns the Bitcoin block a few seconds before others gains more than those few seconds. Consider what the websites the program asks could do, and what an operator who supplies the header by hand could do.
A5. The header a node starts from cannot be made to differ between honest nodes without those nodes noticing. If two groups start from different headers, what happens, and is anyone told?
A6. The Bitcoin height for the launch must be announced in advance and fixed in the published code, not supplied at the last minute. Is it? (At the time of this pack the CVN Testnet's is deliberately not yet set. Say what the code does in that state, and what must be true of the code on the day.)

B. NO HEAD START AFTER THE FIRST BLOCK

B1. The difficulty of the first blocks: is it set so that the first minutes do not hand out a large share of coins to whoever is fastest off the mark? Work it through with real numbers from the code: the starting difficulty, the rule for the first 20 blocks, the rule for blocks 20 to 60, and how many blocks a miner with far more power than expected could take before the difficulty catches up. Compare the coins paid in the first hour and the first day with the total supply.
B2. The same, the other way: if far less power turns up than expected, does the chain stall, and can anyone turn that to their advantage?
B3. Can the miner of early blocks gain by choosing timestamps?
B4. Is the reward in the early blocks exactly what the published schedule says, with no bonus period, no larger early reward, and no "slow start" that is different from what the paper states?
B5. Is the fastest mining software available to everyone on equal terms before the launch? The project publishes its own edition of a third-party miner (XMRig). Its source is not in this pack; MINING_GUIDE.md says where it is published. Say what would have to be published, and by when, for nobody to hold a faster miner than the public.
B6. Does the published program contain everything needed to mine from the first block, with instructions that work? Read the guides as a newcomer would and name anything that would leave an ordinary person unable to start on time while an insider could.

C. NO SHARE AND NO POWERS FOR THE FOUNDERS

C1. No address, key or account in the code receives any part of any reward, fee, sweep or vault payout. Search for every literal address and every literal key in the source and say what each is for.
C2. The "vault" is a number kept by the rules and not a coin that a key can spend. Prove that no transaction and no key can move value out of it except the per-block payout to whoever mines the block.
C3. The seven-year sweep takes from every address by the same rule, with no exemption for any address, and gives to nobody but the vault.
C4. No key, signature, message or setting lets one party do what others cannot: stop the chain, reverse blocks, freeze or seize coins, change a rule, mint coins, or push an update. Look for checkpoints, alert systems, signed messages, remote configuration, automatic updates, hidden command-line options and environment variables.
C5. No environment variable, file or option can change a rule on a launch network. Some exist for "devnet" only. Prove they cannot be reached on "cvn_testnet", and say what would stop the same switches being reachable on a future mainnet.
C6. The seed computers have no power over the rules. The code prefers seeds when choosing where to download the chain from. Can whoever runs the seeds make new nodes follow a chain with less work, keep them off the real chain, or censor blocks or transfers? What does a newcomer do if every seed is offline or lying?
C7. The program contacts no computer the user was not told about, and sends nothing about the user anywhere. List every outside address it contacts and why.
C8. Coins or signed transfers from a test network cannot be replayed on another network.

D. THE SUPPLY IS WHAT THE PAPER SAYS

D1. The total that can ever be issued, computed from the code, equals the figure in the design paper. Do the sum.
D2. Every block pays exactly: the scheduled reward, plus the miner's share of fees, plus the vault payout, and no more. Work one block through by hand with real numbers, including the rounding.
D3. Coins plus vault never exceeds what has been issued, after a sweep, after a reorganisation, at each halving and after the last one.
D4. Amounts cannot overflow or lose value through floating-point arithmetic or the way numbers are written and read.

E. BUILT FROM ITS OWN CODE, AND HONEST ABOUT WHAT IS NOT

E1. The chain's own rules (blocks, transfers, difficulty, sync, storage) are original code and not another coin's code renamed. Say what in the source supports or contradicts this. You cannot compare against every project that exists; say what you could and could not check.
E2. Every part that comes from someone else is named, and its licence allows this use and is kept with it: the RandomX engine, the miner edition, the libraries in go.mod. Name anything used but not credited.
E3. The changes made to RandomX are stated accurately, and do not give anyone who knows them an advantage over someone who reads the documents.
E4. Everything a person needs in order to check the launch for themselves is public: all source for what runs, how to build it, and how to get the same program from the same source.
E5. Every claim of fact in WHITEPAPER.md, README.md and the guides is true of the code. List each claim that is false, overstated, or not yet built, with the line.

F. SOUND ENOUGH TO LAUNCH

The earlier rounds covered this ground; their records are in the pack. Do not repeat them. Check instead:

F1. Every finding the records mark as fixed is in fact fixed in this code. List any that is not.
F2. Every item the records list as "not changed": is any of them a reason not to launch? Say which, and why.
F3. The address checksum, which is new since the last round (block_format.go: addressChecksum, looksLikeWalletAddress, completeOlderAddress; tx_rules.go: validRecipient). Can any coin still be made out to something nobody can spend? Can honest nodes disagree about whether an address is valid?
F4. Anything you find while doing A to E that lets coins be created or taken, lets honest nodes disagree, or lets one participant stop the chain. Report it in full even though it is not a fairness item.

G. WHAT THE CODE CANNOT SHOW

These are part of a fair launch and are decided by conduct, not code. Do not mark them PASS or FAIL. For each, say what the project should publish or do, and how an outsider could check afterwards:

G1. The launch block and time announced publicly, far enough ahead, in a place that can be dated.
G2. The exact source and the built programs published before that announcement's deadline, with a way to check that the programs match the source.
G3. The founder mining from the first block with the same software as everyone else, and saying so, including with how much computing power.
G4. No private agreement, sale, or allocation of coins before or after launch.
G5. The seed computers: who runs them, and how others can add their own.
G6. What happens to the test networks' coins (they should be worth nothing and carry over to nothing).

WHAT IS ALREADY KNOWN - do not report these as findings

- A proof-of-work network with little mining power can be attacked by anyone with more.
- All balances and transfers are public. A lost private key means lost coins.
- Which transfers a miner includes is the miner's choice and is not a network rule.
- One person wrote and runs the project. Report what follows from that only where the code gives that person a power others lack.
- Everything listed under "not changed" in the audit records, unless you can show it bears on fairness.
- Style, naming, comments, and the wording of messages.

HOW TO ANSWER

1. One line: how much of the material you read, and anything you could not check.

2. THE SCORECARD. A table with one row for every item A1 to F4, in order: the item, your judgement (PASS / FAIL / PARTLY / NOT IN CODE / CANNOT TELL), and one line of reason naming the file and function. No row may be left out.

3. EVERYTHING THAT IS NOT A PASS, most serious first. For each:

  Item: its number and a one-line title.
  Judgement: FAIL / PARTLY / NOT IN CODE / CANNOT TELL.
  Seriousness for a fair launch: Blocks the launch / Should be fixed before launch / Can follow launch.
  What is so: the facts, with the lines quoted. For a FAIL, the exact steps: who does what, and who gains or loses what.
  What would make it a PASS: the smallest change to the code, the documents, or the launch procedure. Be exact enough that someone could do it without asking you.

4. SECTION G: for each of G1 to G6, what to publish or do, and how it can be checked.

5. THE COUNT: how many items are PASS, FAIL, PARTLY, NOT IN CODE and CANNOT TELL. Do not turn this into a percentage or a score out of ten. Then one sentence: on what you read, is there any item that should stop the CVN Testnet from launching as it stands, and which.

Rules for your answer:
- Be specific or say nothing. "Consider a third-party audit" is not an answer to any item.
- Judge what is in front of you. Do not mark an item down because other projects do it differently, and do not mark one up because the documents sound careful.
- If you are unsure whether the code does what you think, say so and say what you assumed.
- Do not soften a FAIL, and do not pad the answer with praise or a summary.
- If you cannot find a way to break an item, say what you tried.
```

After every reviewer has answered Prompt 4, run Prompt 2 with their answers as before. For the scorecard, add this line at the top of Prompt 2:

```text
The findings below are answers to a fair-launch scorecard with items numbered A1 to G6. Where two reviewers give the same item different judgements, say which is right and quote the code that settles it.
```

---

## Prompt 5: the fourth round, after a rule change and a large removal

Use this for the round that follows commit `a3c8c9f`. Two things changed that every node enforces: the difficulty rule ("rule B"), and the removal of the first test network together with every rule that existed only for it. Both are described, as claims to be tested, in `audit/CHANGES_FOR_ROUND_4.md`, which is in every pack.

Rebuild the packs first, so they name the commit. Start a **new conversation** with each reviewer. There are three versions of the prompt, for three sizes of input:

| Reviewer can take | Pack | Prompt |
|---|---|---|
| About 950,000 characters in one file | `cvn_audit_full.txt` | Prompt 5A |
| That much only in pieces | `cvn_audit_1_rules.txt`, `_2_program.txt`, `_3_tests.txt`, sent as under "Sending the pack in parts" | Prompt 5A |
| About 275,000 characters | `cvn_audit_focus.txt` | Prompt 5B |
| About 80,000 characters | `cvn_audit_rule_b.txt` | Prompt 5C |

A review made with 5B or 5C covers only what its pack holds. Record which version each reviewer was given; their findings are not comparable across versions without that.

`python audit/make_audit_pack.py` also writes each prompt below to its own file in `audit/out/` (`PROMPT_5A.txt`, `PROMPT_5B.txt`, `PROMPT_5C.txt`), ready to paste.

### Prompt 5A: the whole code

```text
You are reviewing the source code of a proof-of-work blockchain before it launches. Real people will mine it on their own computers. Your job is to find what is wrong with it. You are not being asked to summarise it, explain it, or say what is good about it.

This is the FOURTH round of review. Earlier rounds found real problems, including one that let any peer replace the first block and create coins. Since the last round the authors have made two changes to the rules every node enforces, and they have not been reviewed by anyone. Your job has three parts, in this order.

WHAT YOU HAVE

One file, attached or pasted below (or three parts, if you were sent it in pieces). It starts with a list of what it contains:

- WHITEPAPER.md: what the rules are meant to be. Read this first.
- audit/CHANGES_FOR_ROUND_4.md: what changed for this round, written by the authors as claims. Read this second. Do not accept any of it without reading the code.
- README.md, Installation_Guide.md, MINING_GUIDE.md, ROADMAP.md: what the public is told.
- audit/FINDINGS_*.md: what earlier rounds found and what was done.
- Source code in Go, then its tests.

Read all of it before you answer. The material numbers its files ("FILE n OF N") and ends with the line "END OF PACK". Begin your answer by stating the number of the last file you can see and whether you can see that last line. If you cannot, your copy was cut short: say so plainly, review only what you have, and do not guess at code you did not see.

WHAT THE PROJECT IS

Covenant Standard (CVN): a coin with a fixed supply, mined with RandomX on ordinary processors, with no pre-mine. Coins are separate unspent outputs, as in Bitcoin, owned by addresses and spent with ECDSA signatures. Blocks are meant to come every 45 seconds; difficulty is adjusted on every block. Unusual features: coins nobody has moved for seven years have a fifth of their value moved to a "vault", a number kept by the rules that pays out a little to miners with every block; a fifth of transaction fees also goes to the vault; and the first block is tied to a future Bitcoin block, by that block's 80-byte header, so nobody can mine early.

Two networks are defined, "cvn_testnet" (about to launch) and "devnet" (for development). They run the same rules and differ only in names, keys, ports, files, seeds and starting difficulty (node/chain_params.go). An older network, "testnet-1", has been removed.

PART 1: THE NEW DIFFICULTY RULE ("rule B")

Read section 1 of audit/CHANGES_FOR_ROUND_4.md, then node/heartbeat_difficulty.go in full, then node/relief_not_inherited_test.go and node/hopping_sim_test.go.

The rule: a block dated six minutes or more after its parent is mined at a halved difficulty (halved again per further three minutes). That relief now belongs to the late block alone; the next block is worked out from the difficulty its parent would have had without relief, found by walking back through consecutive late blocks and working forward again, with answers remembered by block identifier.

Try to break it:

1. Disagreement. Find any way for two honest nodes to compute different difficulties for the same block. Look at the remembered table (unrelievedCache), at what happens when an ancestor is missing or is not the block's real parent (the "exact" flag), at a node that has just restarted, at a reorganisation, and at two rival blocks at one height. A single disagreement splits the network.
2. Cost. Can anyone build a chain, or send blocks, that make the walk back through late blocks expensive for every node that checks them?
3. The burst it was meant to stop. Is there still a pattern of stopping and starting, or of dating blocks within the 120 seconds a block may be dated ahead of the clock, that hands a miner cheap blocks? The authors' simulations assume honest dates. Yours need not.
4. The start of the chain. For blocks 1 to 20 no relief is given and difficulty cannot fall below its starting value; over the next 40 blocks relief returns one halving at a time (launchRampGap). Does the new rule behave correctly across both boundaries? At the first block after an algorithm change (powEraStartingAt)?
5. Recovery. After most of the hashrate really leaves, does the chain still recover, and how long does it take? Is there a state from which it cannot?
6. The tests. Do they test the rule, or only that the code agrees with itself? Name a case they miss.
7. The authors say a miner who mines only when difficulty dips still earns 10% to 30% more per hash and that this was left alone. Is leaving it alone defensible for a launch? Is it worse than they say?

PART 2: THE REMOVAL

Read section 2 of audit/CHANGES_FOR_ROUND_4.md. The authors say that removing the older network's rules changed nothing for the launch networks, and offer as evidence that 2,944 real blocks replay identically. That evidence covers only blocks that were accepted.

1. Find a block or a transfer that the launch rules refused before and that this code accepts, or the reverse. Look hardest at ValidateBlockSignatures (node/blockchain_core.go), where the median-time check, the two-hour check and the uncle branch were taken out, and at SaveBlockToStorageInternal (node/database_core.go).
2. The claim "a date later than the parent's makes the median-of-eleven rule unnecessary": is it true in every case, including the first eleven blocks and a reorganisation?
3. Find any place where a removed setting was the only thing standing between today's data and code written for the older format.
4. Find anything left behind: a branch, a default, a message, a file name or a document that still behaves or reads as if the older network existed. Check that what users are told (the guides) matches the code.
5. The genesis block is not mined and is treated specially in several places (its work, its transaction digest, IsHeartbeatHeight(0), IsTxForkHeight(0)). Is every one of those cases still safe now that the surrounding code is gone? Can anything other than the program's own genesis block reach them?
6. The block header keeps two "uncle" fields that must hold fixed values. Is that enforced on every path by which a block can be stored?

PART 3: EVERYTHING ELSE, AGAIN

The rest of the code was reviewed in earlier rounds, but a large edit can break what it did not mean to touch. With the time you have left, look for, most important first:

1. Creating coins: any way for the total of all coins plus the vault to exceed what the schedule has issued.
2. Taking or double-spending coins.
3. Honest nodes disagreeing about any block or transfer, for any reason: clocks, operating systems, map order, encodings, memory pools, arrival order.
4. Bookkeeping across a reorganisation: spent coins, swept coins, the vault, total work, balances, and now the remembered difficulty table.
5. Stalling the chain or pushing difficulty very high or very low.
6. The launch: mining before the Bitcoin anchor block exists; gains from choosing dates; the first block's difficulty.
7. Attacks through the network that cost a node work, memory or its honest peers.
8. Wallets and keys.

WHAT IS ALREADY KNOWN - do not report these

- Everything under "What it costs, and what it does not fix" and "Open, known to the authors" in audit/CHANGES_FOR_ROUND_4.md, unless you can show it is worse than described.
- Everything under "Not changed, and why" in the audit/FINDINGS files, on the same condition.
- A proof-of-work network with little mining power can be attacked by anyone with more.
- All balances and transfers are public. A lost private key means lost coins.
- Which transfers a miner includes is the miner's choice and is not a network rule.
- The RandomX engine is third-party code and is not included; only CVN's changes to it are.
- Signatures use ECDSA on the P-256 curve. Report this only if you find an actual weakness in how it is used.
- Style, naming, comments, and the wording of messages.

HOW TO ANSWER

Start with one line: how much of the material you read, and anything you could not check.

Then, for PART 1 and PART 2, go through the numbered questions in order and answer each in a line or two: what you checked and what you concluded. "Did not check" is an acceptable answer; silence is not.

Then your findings from all three parts together, most serious first. For each one:

  Title: one line.
  Severity: Critical (coins can be created or taken, or the chain can be split or stopped) / High / Medium / Low.
  Confidence: Certain (you traced it through the code) / Likely / Possible (you suspect it but could not confirm).
  Where: the file and the function.
  What happens: the exact steps. Who does what, with what values, and what each node then does. For a difficulty finding, give block heights, dates and difficulties. A finding without concrete steps is a suspicion, and should be marked Possible.
  Why the code allows it: quote the lines that matter.
  What to change: the smallest fix you can see.

Then a short list titled "Checked and found sound": the specific things you examined and believe are correct, with one line each saying what you checked.

Then a short list titled "Could not determine": things you wanted to check and could not, and what you would need.

Rules for your answer:
- Be specific or say nothing. "Consider adding more validation" is not a finding.
- Do not report something as a problem unless you can say what an attacker or an unlucky honest user would do.
- If you are unsure whether the code does what you think, say so and say what you assumed.
- Do not soften findings, and do not pad the answer with praise or a summary.
- A short answer with three real problems is better than a long one with thirty doubtful ones.
```

### Prompt 5B: the rules only, for a smaller input

For `cvn_audit_focus.txt`. The pack leaves out networking, wallets and most tests, and the prompt says so.

```text
You are reviewing part of the source code of a proof-of-work blockchain before it launches. Your job is to find what is wrong with it. Do not summarise it, explain it or praise it.

WHAT YOU HAVE

One file, attached or pasted below. It is a SELECTION from a larger code base: the design paper (WHITEPAPER.md), the authors' record of what changed for this review (audit/CHANGES_FOR_ROUND_4.md), every source file that decides whether a block or a transfer is valid, the miner (node/main.go), and three test files. Networking, block download, wallets and most tests are NOT included. Do not guess at code you cannot see; if a finding depends on it, say so and mark the finding Possible.

The material numbers its files ("FILE n OF N") and ends with the line "END OF PACK". Begin your answer by stating the number of the last file you can see and whether you can see that last line. If you cannot, your copy was cut short: say so, and review only what you have.

WHAT THE PROJECT IS

Covenant Standard (CVN): a coin with a fixed supply, mined with RandomX on ordinary processors, no pre-mine. Coins are unspent outputs owned by addresses, spent with ECDSA signatures. Blocks are meant to come every 45 seconds and difficulty is adjusted on every block. Coins unmoved for seven years have a fifth moved to a "vault" that pays a little to miners each block; a fifth of fees also goes there. Two networks, "cvn_testnet" and "devnet", run the same rules. An older network, "testnet-1", has been removed from the code.

Since the last review the authors made two changes to the rules every node enforces. Nobody else has checked them.

PART 1: THE NEW DIFFICULTY RULE ("rule B")

Read section 1 of audit/CHANGES_FOR_ROUND_4.md, then node/heartbeat_difficulty.go in full, then the two test files about it.

A block dated six minutes or more after its parent is mined at a halved difficulty (halved again per further three minutes). That relief now belongs to the late block alone: the next block is worked out from the difficulty its parent would have had without relief, found by walking back through consecutive late blocks and forward again, with answers remembered by block identifier.

1. Find any way for two honest nodes to compute different difficulties for the same block: through the remembered table, a missing or wrong ancestor (the "exact" flag), a restart, a reorganisation, or two rival blocks at one height.
2. Can a chain be built that makes the walk back expensive for every node?
3. Is there still a pattern of stopping and starting, or of dating blocks up to 120 seconds ahead, that hands a miner cheap blocks? The authors' simulations assume honest dates.
4. Does the rule behave correctly across the first 20 blocks (no relief, difficulty cannot fall below its start), the next 40 (relief returns one halving at a time), and an algorithm change?
5. After most hashrate really leaves, does the chain recover? Is there a state from which it cannot?
6. Do the tests test the rule, or only that the code agrees with itself? Name a case they miss.

PART 2: THE REMOVAL

Read section 2 of audit/CHANGES_FOR_ROUND_4.md. The authors claim that removing the older network's rules changed nothing for the remaining networks; their evidence is that 2,944 accepted blocks replay identically, which says nothing about refused blocks.

1. Find a block or transfer the rules refused before and now accept, or the reverse. Look hardest at ValidateBlockSignatures (node/blockchain_core.go) and SaveBlockToStorageInternal (node/database_core.go).
2. Is "a date later than the parent's makes the median-of-eleven rule unnecessary" true in every case?
3. The genesis block is not mined and has special cases (its work, its transaction digest, IsHeartbeatHeight(0), IsTxForkHeight(0)). Can anything but the program's own genesis block reach them?
4. The header keeps two "uncle" fields that must hold fixed values. Is that enforced on every path you can see?
5. Find anything left behind that still behaves as the older rules did.

PART 3: IF YOU HAVE TIME

In the files you have: any way to create coins beyond the schedule, spend a coin twice or without owning it, or leave the vault, the swept coins or the balances wrong after blocks are removed and others applied.

DO NOT REPORT

What the authors list under "What it costs, and what it does not fix" and "Open, known to the authors", unless it is worse than they say. That weak networks can be out-mined. That balances are public. Style and wording.

HOW TO ANSWER

One line first: how much you read and what you could not check.

Then answer each numbered question of PART 1 and PART 2 in a line or two: what you checked and what you concluded. "Did not check" is acceptable; silence is not.

Then your findings, most serious first, each with: Title; Severity (Critical / High / Medium / Low); Confidence (Certain / Likely / Possible); Where (file and function); What happens (exact steps, with block heights, dates and difficulties where they matter); the lines that allow it, quoted; the smallest fix.

Then "Checked and found sound": what you examined and believe is correct, one line each.

Be specific or say nothing. Do not soften findings or add a summary. Three real problems are worth more than thirty doubtful ones.
```

### Prompt 5C: the difficulty rule only, for a small input

For `cvn_audit_rule_b.txt`, about 75,000 characters. It asks one question well instead of many badly.

```text
You are reviewing ONE rule of a proof-of-work blockchain before it launches: how the difficulty of each block is set. Your job is to find what is wrong with it. Do not summarise, explain or praise.

WHAT YOU HAVE

One file, attached or pasted below, holding six files from a larger code base: the authors' description of the change (audit/CHANGES_FOR_ROUND_4.md; only section 1 matters here), the difficulty rule (node/heartbeat_difficulty.go), the network settings (node/chain_params.go), block validation (node/blockchain_core.go), and two test files. Everything else is missing. Do not guess at code you cannot see; where a finding depends on it, say so.

The material numbers its files ("FILE n OF 6") and ends with the line "END OF PACK". Begin by stating the number of the last file you can see and whether you can see that line. If you cannot, your copy was cut short: say so.

THE RULE

Blocks are meant to come every 45 seconds. Each block's difficulty is its parent's, moved up by at most 5% if the parent was found quickly and down by at most half if slowly, with a small correction (at most 1%) toward the chain's schedule. If no block arrives for six minutes, the difficulty of the block being mined is halved, and halved again for every further three minutes.

The change under review: that halving now belongs to the late block alone. The next block is worked out from the difficulty its parent WOULD have had without the halving. That figure cannot be read from the block, so it is recomputed by walking back through consecutive late blocks to the first one that was on time and working forward again; answers are remembered by block identifier (unrelievedDifficulty and unrelievedCache).

It was made because one long gap used to leave about seventy-five nearly free blocks for whoever held the most hashrate.

QUESTIONS

1. Can two honest nodes ever compute different difficulties for the same block? Consider the remembered table, a node that has just restarted, an ancestor that is missing or is not the block's real parent (the "exact" flag), a reorganisation, and two rival blocks at one height. One disagreement splits the network.
2. Can a chain be built that makes the walk back expensive for every node that checks it?
3. A block may be dated up to 120 seconds ahead of the clock and must be dated after its parent. Using dates, or by stopping and starting, can a miner still obtain cheap blocks? Give heights, dates and difficulties.
4. For blocks 1 to 20 no halving is given and difficulty cannot fall below its starting value; over the next 40 blocks the halvings return one at a time (launchRampGap). Is the new rule right across both boundaries?
5. If most miners really leave, does the chain recover, and is there a state from which it cannot?
6. Do the tests test the rule, or only that the code agrees with itself? Name a case they miss.

DO NOT REPORT

What the authors list under "What it costs, and what it does not fix". Style and wording.

HOW TO ANSWER

Answer each question in order, in a few lines: what you checked and what you concluded. "Did not check" is acceptable.

Then any findings, most serious first, each with: Title; Severity (Critical / High / Medium / Low); Confidence (Certain / Likely / Possible); the function; the exact steps with numbers; the lines that allow it, quoted; the smallest fix.

Be specific or say nothing. Do not add a summary.
```

After every reviewer has answered, run Prompt 2 as before, with each reviewer given the pack it had and the others' findings. Findings from a reviewer that had a smaller pack may rest on code another reviewer could see: say which pack each finding came from.

---

## Prompt 6: the exponential difficulty step

A short, narrow review of one change made after round 4: each block's difficulty step is now applied as `e^x`, worked out in whole numbers (`ExponentialSteps`, `expStepFactor` in `node/heartbeat_difficulty.go`). It answers finding 1 of `audit/FINDINGS_ROUND_4.md`.

Give the reviewer `cvn_audit_rule_h.txt` (about 82,000 characters) with the prompt below, in a **new conversation**. A reviewer that can take the whole code may be given `cvn_audit_full.txt` with the same prompt; change the first sentence of "WHAT YOU HAVE" to say so.

```text
You are reviewing ONE change to a proof-of-work blockchain before it launches: the arithmetic that sets each block's difficulty. A mistake here either splits the network or lets a large miner take more than its share. Your job is to find what is wrong. Do not summarise, explain or praise.

WHAT YOU HAVE

One file, attached or pasted below, holding seven files from a larger code base: the record of the last review (audit/FINDINGS_ROUND_4.md; finding 1 is what this change answers), the difficulty rule (node/heartbeat_difficulty.go), the network settings (node/chain_params.go), block validation (node/blockchain_core.go), and three test files. Everything else is missing. Do not guess at code you cannot see; where a finding depends on it, say so.

The material numbers its files ("FILE n OF 7") and ends with the line "END OF PACK". Begin by stating the number of the last file you can see and whether you can see that line. If you cannot, your copy was cut short: say so.

THE RULE

Blocks are meant to come every 45 seconds. For each block a step x is worked out:

  x = (45 - solve) / 900  -  drift / 180000

where solve is how many seconds the parent block's date is after its own parent's (held between 1 and 495), and drift is how far the chain's dates are from its schedule (the second term is held between -0.01 and +0.01). So x lies between about -0.51 and +0.059.

Before this change the next difficulty was difficulty * (1 + x). A reviewer showed that a miner with most of the hashrate could date a run of blocks one second after their parents (twelve steps of about +5%) and then one block 495 seconds after its parent (one step of -50%); those multiply to less than 1 although the x's sum to more than 0, so difficulty stayed level while the blocks' dates advanced by less than 45 seconds a block. That paid up to 13% more coins.

Now the next difficulty is difficulty * e^x. The authors' claim: e^a * e^b = e^(a+b), so any run of steps depends only on the sum of the x's, that is, on how much time the blocks' dates span, and choosing dates gains nothing.

Separately, and unchanged: if a block is dated 360 seconds or more after its parent, that block alone is mined at a halved difficulty (halved again per further 180 seconds). The next block is worked out from the difficulty its parent would have had without that halving. A block may be dated up to 120 seconds ahead of the checking node's clock and must be dated after its parent.

e^x is computed by expStepFactor(numerator, denominator): the series 1 + x + x^2/2 + x^3/6 + ... in 64-bit fixed point using Go's math/big, each term found from the last as term * numerator / (denominator * k) with the division rounded toward zero, stopping when a term is zero or after 200 terms. The result is multiplied into the difficulty and shifted right by 64 bits.

QUESTIONS

1. Exactness. Will every correct implementation, on every machine and operating system, get the same whole number from expStepFactor and from heartbeatRetargetAnchored for the same inputs? Is there any input the rule can produce for which the series misbehaves: does not converge, overflows, goes negative, or stops early? Check the smallest and largest x, a difficulty at the network minimum (5,000) and at the maximum (2^55), and what rounding toward zero does to negative terms.

2. The claim. Does difficulty * e^x really remove the gain from choosing dates? The solve time is held to at most 495 seconds and at least 1, and the drift term is capped, so the x's are not simply proportional to elapsed time. Using those limits, the 120 seconds a block may be dated ahead, or the separate late-block halving, find a pattern of dates that still gives a miner with most of the hashrate more blocks than steady honest mining would. Give heights, dates and difficulties.

3. Rounding as a lever. Each step rounds the difficulty down to a whole number. At low difficulty can a miner choose steps so that rounding accumulates in its favour?

4. Recovery. A slow block now lowers difficulty by about 39% at most where it was 50%. With the late-block halving not carried forward, does a chain that has really lost most of its miners still recover? Work one case through: hashrate falls to a hundredth.

5. The first blocks. For blocks 1 to 20 no halving is given and difficulty cannot fall below its starting value; over the next 40 blocks halvings return one at a time (launchRampGap). Does the new step behave correctly there, and at a change of mining algorithm (powEraStartingAt), where difficulty restarts?

6. The tests. node/exponential_steps_test.go pins seven values and compares the series with floating point; node/chosen_dates_sim_test.go runs five date patterns. What do they not cover? Is any pinned value wrong? (e^(8800/180000) * 2^64 should be within a few units of 19370993578904439459.)

7. The switch. ExponentialSteps is a per-network setting, on for both networks. Is there any path that works out a difficulty with the setting in the wrong state, or before the network has been chosen?

DO NOT REPORT

What audit/FINDINGS_ROUND_4.md lists as known or not changed. That a miner who mines only when difficulty dips earns more per hash. Style and wording.

HOW TO ANSWER

Answer each question in order, in a few lines: what you checked and what you concluded. "Did not check" is acceptable; silence is not.

Then any findings, most serious first, each with: Title; Severity (Critical: nodes can disagree, or coins can be taken / High / Medium / Low); Confidence (Certain / Likely / Possible); the function; the exact steps with numbers; the lines that allow it, quoted; the smallest fix.

Be specific or say nothing. Do not add a summary.
```

---

## After the reviews

Bring the findings back to the development session, with each reviewer's verdicts from Prompt 2. Each one that survives is then:

1. reproduced with a test that fails,
2. fixed,
3. shown fixed by that test passing.

A finding that cannot be turned into a failing test is either not real or not yet understood.
