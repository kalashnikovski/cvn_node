"""Builds the source packs handed to auditors.

Run from anywhere:

    python audit/make_audit_pack.py

It writes four text files into audit/out/ (which is not kept in the
repository):

    cvn_audit_full.txt        everything in one file
    cvn_audit_1_rules.txt     part 1: the design paper, the guides given to
                              users, the roadmap, the record of the last
                              audit round, and every file that decides
                              whether a block or a transfer is valid
    cvn_audit_2_program.txt   part 2: networking, mining, wallets, start-up
    cvn_audit_3_tests.txt     part 3: the tests

The three parts together hold exactly what the full file holds. They exist
because some tools cut a long paste short without saying so.

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
    "internal/randomx/LICENSE",
    "internal/randomx/README_CVN.md",
    "internal/randomx/config_cvn.go",
    "internal/randomx/config_reference.go",
]

PROGRAM = [
    "main.go",
    "node/main.go",
    "node/anchor_wait.go",
    "node/mining_readiness.go",
    "node/sync_pass.go",
    "node/gossip_mesh.go",
    "node/relay_uplink.go",
    "node/peer_cache.go",
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
    "node/wallet.go",
    "node/combine.go",
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


def main():
    listed = set(RULES + PROGRAM)
    tests = sorted("node/" + name for name in os.listdir(os.path.join(ROOT, "node")) if name.endswith("_test.go"))
    tests.append("internal/randomx/cvn_test.go")

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
    ]
    os.makedirs(OUT, exist_ok=True)
    for name, title, purpose, files in packs:
        text = build(title, purpose, files)
        with open(os.path.join(OUT, name), "w", encoding="utf-8", newline=NEWLINE) as handle:
            handle.write(text)
        print("%-26s %4d files  %7d characters  about %6d tokens" % (name, len(files), len(text), len(text) // 4))
    print("Written to " + OUT)


if __name__ == "__main__":
    main()
