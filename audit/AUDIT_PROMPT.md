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
   | The whole thing (about 140,000 tokens) | `cvn_audit_full.txt` with Prompt 1 |
   | About 60,000 tokens at a time | `cvn_audit_1_rules.txt` with Prompt 1. Then, in a **new** conversation, `cvn_audit_2_program.txt` with Prompt 1. The tests are optional. |
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

## After the reviews

Bring the findings back to the development session, with each reviewer's verdicts from Prompt 2. Each one that survives is then:

1. reproduced with a test that fails,
2. fixed,
3. shown fixed by that test passing.

A finding that cannot be turned into a failing test is either not real or not yet understood.
