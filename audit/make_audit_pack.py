"""Builds the source packs handed to auditors.

Run from anywhere:

    python audit/make_audit_pack.py

It writes seven text files into audit/out/ (which is not kept in the
repository):

    cvn_audit_full.txt        everything in one file
    cvn_audit_1_rules.txt     part 1: the design paper, the guides given to
                              users, the roadmap, the record of the last
                              audit round, and every file that decides
                              whether a block or a transfer is valid
    cvn_audit_2_program.txt   part 2: networking, mining, wallets, start-up
    cvn_audit_3_tests.txt     part 3: the tests
    cvn_audit_focus.txt       for a reviewer that cannot take the full file:
                              the design paper, the record of what changed
                              for this round, every file that decides whether
                              a block or a transfer is valid, the miner, and
                              the tests of the newest rule
    cvn_audit_rule_b.txt      smaller still: only what the change to late-
                              block relief needs
    cvn_audit_rule_h.txt      likewise for the exponential difficulty step
    cvn_audit_network.txt     how blocks travel and the starting block (round 6)

and each prompt of audit/AUDIT_PROMPT.md as its own file (PROMPT_1.txt and so
on), ready to paste.

The three parts together hold exactly what the full file holds. They exist
because some tools cut a long paste short without saying so. The last two
packs are selections from the full file, for tools with a small input limit;
a review made from one of them covers only what is in it.

Each pack begins with the commit it was made from and a list of its files,
numbers every file, and ends with a line saying it is whole.

Left out on purpose:
  - anything private or local: wallet and peer files, databases, logs
  - the RandomX engine itself (internal/randomx), which is third-party code
    kept as published; only the files CVN added or changed are included
  - generated files: the built wallet screens, lock files, the screens' link
"""
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "audit", "out")
END_MARKER = "END OF PACK"
NEWLINE = chr(10)

RULES = [
    "WHITEPAPER.md",
    "README.md",
    "Installation_Guide.md",
    "MINING_GUIDE.md",
    "ROADMAP.md",
    "LICENSE",
    "audit/FINDINGS_ROUND_1.md",
    "audit/FINDINGS_ROUND_2.md",
    "audit/FINDINGS_ROUND_3.md",
    "audit/FINDINGS_FAIR_LAUNCH.md",
    "audit/CHANGES_FOR_ROUND_4.md",
    "audit/FINDINGS_ROUND_4.md",
    "audit/FINDINGS_ROUND_5.md",
    "audit/CHANGES_FOR_ROUND_6.md",
    "node/chain_params.go",
    "node/amount.go",
    "node/block_format.go",
    "node/blockchain_core.go",
    "node/tx_rules.go",
    "node/vault.go",
    "node/sweep.go",
    "node/heartbeat_difficulty.go",
    "node/pow.go",
    "node/chain_work.go",
    "node/genesis.go",
    "node/database_core.go",
    "node/crypto_auth.go",
    "node/security_harness.go",
    "node/needs_64_bit.go",
    "internal/randomx/LICENSE",
    "internal/randomx/README_CVN.md",
    "internal/randomx/DIFFERENCES.md",
    "internal/randomx/config_cvn.go",
    "internal/randomx/config_reference.go",
]

PROGRAM = [
    "main.go",
    "node/main.go",
    "node/proof_ahead.go",
    "node/pool.go",
    "node/pool_inblock.go",
    "node/pool_ledger.go",
    "node/pool_payments.go",
    "node/pool_pay.go",
    "node/pool_chain.go",
    "node/pool_backup.go",
    "node/pool_page.go",
    "node/pool_mine.go",
    "node/http_api.go",
    "node/dashboard.go",
    "node/builtin_miner.go",
    "node/anchor_wait.go",
    "node/genesis_watch.go",
    "node/bitcoin_headers.go",
    "node/bitcoin_peers.go",
    "node/mining_readiness.go",
    "node/sync_pass.go",
    "node/gossip_mesh.go",
    "node/relay_uplink.go",
    "node/decode_limits.go",
    "node/block_relay.go",
    "node/peer_cache.go",
    "node/own_address.go",
    "node/join_check.go",
    "node/listen_host.go",
    "node/block_races.go",
    "node/fill_blocks_devtool.go",
    "node/localnet_multinode.go",
    "node/backup.go",
    "node/recovery_words.go",
    "node/recovery_wordlist.go",
    "node/nat_traversal.go",
    "node/stratum.go",
    "node/fast_miner.go",
    "node/fastminer_windows.go",
    "node/fastminer_linux.go",
    "node/fastminer_other.go",
    "node/randomx_bench.go",
    "node/affinity_windows.go",
    "node/affinity_linux.go",
    "node/affinity_other.go",
    "node/memory_windows.go",
    "node/memory_linux.go",
    "node/memory_other.go",
    "node/ansi_windows.go",
    "node/secret_input_windows.go",
    "node/secret_input_other.go",
    "node/terminal_colors.go",
    "node/terminal_size_windows.go",
    "node/terminal_size_linux.go",
    "node/terminal_size_other.go",
    "node/wallet.go",
    "node/combine.go",
    "node/renew.go",
    "node/renew_record.go",
    "node/app.go",
    "node/wails_desktop.go",
    "node/wallet_window_stub.go",
    "internal/randomx/internal/memory/large_windows.go",
    "frontend/src/App.svelte",
    "explorer.html",
    "LaunchMiner.bat",
    "LaunchWallet.bat",
    "build_clean.bat",
    "deploy/install_seed.sh",
    "deploy/update_seed.sh",
    ".github/workflows/tests.yml",
    ".github/workflows/release.yml",
    "go.mod",
    "audit/compare_randomx.py",
    "THIRD_PARTY_NOTICES.md",
]


# Selections for reviewers with a small input limit. Every file here is also
# in the full pack.
FOCUS = [
    "audit/CHANGES_FOR_ROUND_4.md",
    "WHITEPAPER.md",
    "node/chain_params.go",
    "node/heartbeat_difficulty.go",
    "node/blockchain_core.go",
    "node/tx_rules.go",
    "node/block_format.go",
    "node/amount.go",
    "node/vault.go",
    "node/sweep.go",
    "node/pow.go",
    "node/chain_work.go",
    "node/genesis.go",
    "node/database_core.go",
    "node/security_harness.go",
    "node/crypto_auth.go",
    "node/main.go",
    "node/relief_not_inherited_test.go",
    "node/hopping_sim_test.go",
    "node/log_noise_test.go",
]

RULE_B = [
    "audit/CHANGES_FOR_ROUND_4.md",
    "node/heartbeat_difficulty.go",
    "node/chain_params.go",
    "node/blockchain_core.go",
    "node/relief_not_inherited_test.go",
    "node/hopping_sim_test.go",
]

RULE_H = [
    "audit/FINDINGS_ROUND_4.md",
    "node/heartbeat_difficulty.go",
    "node/chain_params.go",
    "node/blockchain_core.go",
    "node/exponential_steps_test.go",
    "node/chosen_dates_sim_test.go",
    "node/relief_not_inherited_test.go",
]

# How blocks travel and how a node finds its starting block: the subject of
# round 6, for a reviewer that cannot take the full pack.
NETWORK = [
    "audit/CHANGES_FOR_ROUND_6.md",
    "node/block_relay.go",
    "node/gossip_mesh.go",
    "node/relay_uplink.go",
    "node/decode_limits.go",
    "node/blockchain_core.go",
    "node/sync_pass.go",
    "node/join_check.go",
    "node/own_address.go",
    "node/peer_cache.go",
    "node/bitcoin_headers.go",
    "node/bitcoin_peers.go",
    "node/anchor_wait.go",
    "node/genesis_watch.go",
    "node/genesis.go",
    "node/builtin_miner.go",
    "node/stratum.go",
    "node/security_harness.go",
    "node/block_relay_test.go",
    "node/place_on_tip_test.go",
    "node/bitcoin_headers_test.go",
    "node/decode_limits_test.go",
]


def git(*args):
    try:
        return subprocess.run(["git", "-C", ROOT] + list(args), capture_output=True, text=True, check=True).stdout.strip()
    except Exception:
        return ""


def read(path):
    with open(os.path.join(ROOT, path), "rb") as handle:
        return handle.read().decode("utf-8").replace(chr(13) + NEWLINE, NEWLINE)


def build(title, purpose, files):
    """One pack: a header naming the commit and listing the files, the files
    themselves, each numbered, and a last line that says the pack is whole.
    A reviewer whose copy was cut short cannot see that line, and can say
    which file was the last one it saw."""
    commit = git("rev-parse", "HEAD") or "unknown"
    dirty = [line for line in git("status", "--porcelain").splitlines()
             if line[3:].strip('"') in files]
    total = len(files)
    rule = "=" * 78
    lines = [
        rule,
        "COVENANT STANDARD (CVN) - SOURCE FOR AUDIT: " + title,
        rule,
    ]
    lines += purpose
    lines += [
        "",
        "Repository: https://github.com/kalashnikovski/cvn_node",
        "Commit:     " + commit,
        "",
        'This pack holds %d files. Each begins with a line "FILE n OF %d".' % (total, total),
        'It ends with the line "%s". If you cannot see that line, your copy' % END_MARKER,
        "was cut short: say so, and name the number of the last file you can see.",
    ]
    if dirty:
        lines.append("WARNING: these files differ from that commit: " + ", ".join(sorted(d[3:] for d in dirty)))
    lines += ["", "Files in this pack, in order:"]
    lines += ["  %2d. %s" % (i + 1, path) for i, path in enumerate(files)]
    lines.append("")
    for number, path in enumerate(files, 1):
        text = read(path).rstrip(NEWLINE)
        lines += ["", rule, "FILE %d OF %d: %s" % (number, total, path), rule, text, ""]
    lines += ["", rule, "%s (%d files, commit %s)" % (END_MARKER, total, commit[:12]), rule]
    return NEWLINE.join(lines) + NEWLINE


# The pool: a node that takes miners in, records their work and pays them.
# It holds other people's coins, so it is reviewed by itself (Prompt 8).
POOL = [
    "POOL_DESIGN.md",
    "POOL_GUIDE.md",
    "node/pool.go",
    "node/pool_inblock.go",
    "node/stratum.go",
    "node/builtin_miner.go",
    "node/pool_ledger.go",
    "node/pool_payments.go",
    "node/pool_pay.go",
    "node/pool_chain.go",
    "node/pool_backup.go",
    "node/pool_page.go",
    "node/pool_mine.go",
    "node/tx_rules.go",
    "node/block_format.go",
    "node/recovery_words.go",
    "deploy/install_pool.sh",
    "node/reward_outputs_test.go",
    "node/pool_inblock_test.go",
    "node/pool_test.go",
    "node/pool_pay_test.go",
    "node/pool_backup_test.go",
    "node/pool_mine_test.go",
    "node/pool_multinode_test.go",
]


def main():
    listed = set(RULES + PROGRAM)
    for name, selection in (("FOCUS", FOCUS), ("RULE_B", RULE_B), ("RULE_H", RULE_H)):
        stray = [path for path in selection if path not in listed and not path.endswith("_test.go")]
        if stray:
            sys.exit(name + " names files that are not in the full pack: " + ", ".join(stray))
    tests = sorted("node/" + name for name in os.listdir(os.path.join(ROOT, "node")) if name.endswith("_test.go"))
    tests.append("internal/randomx/cvn_test.go")
    tests.append("internal/randomx/reference_test.go")

    # Nothing in node/ may be forgotten: a file missing from the lists above
    # would silently go unaudited.
    missing = sorted("node/" + name for name in os.listdir(os.path.join(ROOT, "node"))
                     if name.endswith(".go") and not name.endswith("_test.go") and "node/" + name not in listed)
    if missing:
        sys.exit("These files are in node/ but in no pack. Add them to RULES or PROGRAM: " + ", ".join(missing))

    packs = [
        ("cvn_audit_full.txt", "EVERYTHING IN ONE FILE",
         ["The design paper, the guides given to users, the roadmap, the record of the last audit",
          "round, the source code and the tests. The same files as parts 1, 2 and 3 together."],
         RULES + PROGRAM + tests),
        ("cvn_audit_1_rules.txt", "PART 1 OF 3 - THE RULES",
         ["The design paper, the guides given to users, the roadmap, the record of the last audit",
          "round, then every file that decides whether a block or a transfer is valid."],
         RULES),
        ("cvn_audit_2_program.txt", "PART 2 OF 3 - THE PROGRAM",
         ["Networking, mining, wallets and start-up. Nothing here decides validity, but it decides",
          "what a node does with what it receives."],
         PROGRAM),
        ("cvn_audit_3_tests.txt", "PART 3 OF 3 - THE TESTS",
         ["The tests. They show what the authors checked, and by omission what they did not."],
         tests),
        ("cvn_audit_focus.txt", "A SELECTION - WHAT DECIDES VALIDITY",
         ["For a reviewer that cannot take the full pack. The design paper, the record of what",
          "changed for this round, every file that decides whether a block or a transfer is valid,",
          "the miner, and the tests of the newest rule. Networking, wallets and most tests are NOT here."],
         FOCUS),
        ("cvn_audit_rule_b.txt", "A SMALL SELECTION - THE NEWEST DIFFICULTY RULE ONLY",
         ["For a reviewer with a small input limit. Only what is needed to judge the change to the",
          "difficulty rule described in audit/CHANGES_FOR_ROUND_4.md, section 1. Nothing else is here."],
         RULE_B),
        ("cvn_audit_rule_h.txt", "A SMALL SELECTION - THE EXPONENTIAL DIFFICULTY STEP ONLY",
         ["For a short review of one change: each difficulty step applied as e^x, in whole numbers.",
          "See audit/FINDINGS_ROUND_4.md, finding 1. Nothing else is here."],
         RULE_H),
        ("cvn_audit_network.txt", "A SELECTION - HOW BLOCKS TRAVEL, AND THE STARTING BLOCK",
         ["For round 6, for a reviewer that cannot take the full pack. How a node passes blocks on,",
          "places them, compares chains, and finds the Bitcoin block it starts from. See",
          "audit/CHANGES_FOR_ROUND_6.md, sections 2 to 4. The rules of validity are NOT here."],
         NETWORK),
        ("cvn_audit_pool.txt", "A SELECTION - THE POOL",
         ["The pool built into the node: shares, a block's reward paid to the miners in the block,",
          "the record of who was paid and is owed what, paying, the record's copies, and the miner's",
          "side. See POOL_DESIGN.md, the first file. With it the rule that a reward may pay fifty",
          "addresses. Networking and the wallets are NOT here."],
         POOL),
    ]
    os.makedirs(OUT, exist_ok=True)
    for name, title, purpose, files in packs:
        text = build(title, purpose, files)
        with open(os.path.join(OUT, name), "w", encoding="utf-8", newline=NEWLINE) as handle:
            handle.write(text)
        print("%-26s %4d files  %7d characters  about %6d tokens" % (name, len(files), len(text), len(text) // 4))
    write_prompts()
    print("Written to " + OUT)


def write_prompts():
    """Each prompt in AUDIT_PROMPT.md is the first fenced block after a
    heading that begins "Prompt N". Written out one per file, ready to paste."""
    fence = chr(96) * 3
    heading, inside, body = None, False, []
    for text_line in read("audit/AUDIT_PROMPT.md").split(NEWLINE):
        stripped = text_line.strip()
        if not inside and stripped.lstrip("#").strip().startswith("Prompt ") and stripped.startswith("#"):
            heading = stripped.lstrip("#").strip().split(":")[0].replace("Prompt ", "PROMPT_").replace(" ", "_")
        elif not inside and heading and stripped == fence + "text":
            inside, body = True, []
        elif inside and stripped == fence:
            text = NEWLINE.join(body) + NEWLINE
            with open(os.path.join(OUT, heading + ".txt"), "w", encoding="utf-8", newline=NEWLINE) as handle:
                handle.write(text)
            print("%-26s             %7d characters" % (heading + ".txt", len(text)))
            heading, inside = None, False
        elif inside:
            body.append(text_line)


if __name__ == "__main__":
    main()
