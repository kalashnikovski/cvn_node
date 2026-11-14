"""Builds the source packs handed to auditors.

Run from anywhere:

    python audit/make_audit_pack.py

It writes four text files into audit/out/ (which is not kept in the
repository):

    cvn_audit_1_rules.txt     the design paper and every file that decides
                              whether a block or a transfer is valid
    cvn_audit_2_program.txt   networking, mining, wallets and start-up
    cvn_audit_3_tests.txt     the tests
    cvn_audit_full.txt        all three in one file

Each pack begins with the commit it was made from and a list of its files.

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

RULES = [
    "WHITEPAPER.md",
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
    "internal/randomx/README_CVN.md",
    "internal/randomx/config_cvn.go",
    "internal/randomx/config_reference.go",
]

PROGRAM = [
    "main.go",
    "node/main.go",
    "node/anchor_wait.go",
    "node/mining_readiness.go",
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
        return handle.read().decode("utf-8").replace("\r\n", "\n")


def build(title, purpose, files):
    commit = git("rev-parse", "HEAD") or "unknown"
    dirty = [line for line in git("status", "--porcelain").splitlines()
             if line[3:].strip('"') in files]
    lines = [
        "=" * 78,
        "COVENANT STANDARD (CVN) - SOURCE FOR AUDIT: " + title,
        "=" * 78,
        purpose,
        "",
        "Repository: https://github.com/kalashnikovski/cvn_node",
        "Commit:     " + commit,
    ]
    if dirty:
        lines.append("WARNING: these files differ from that commit: " + ", ".join(sorted(d[3:] for d in dirty)))
    lines += ["", "Files in this pack, in order:"]
    lines += ["  %2d. %s" % (i + 1, path) for i, path in enumerate(files)]
    lines.append("")
    for path in files:
        text = read(path)
        lines += ["", "=" * 78, "FILE: " + path, "=" * 78, text.rstrip("\n"), ""]
    return "\n".join(lines) + "\n"


def main():
    listed = set(RULES + PROGRAM)
    tests = sorted("node/" + name for name in os.listdir(os.path.join(ROOT, "node")) if name.endswith("_test.go"))
    tests.append("internal/randomx/cvn_test.go")

    # Nothing in node/ may be forgotten: a file missing from the lists above
    # would silently go unaudited.
    missing = sorted("node/" + name for name in os.listdir(os.path.join(ROOT, "node"))
                     if name.endswith(".go") and not name.endswith("_test.go") and "node/" + name not in listed)
    if missing:
        sys.exit("These files are in node/ but in no pack. Add them to RULES or PROGRAM:\n  " + "\n  ".join(missing))

    packs = [
        ("cvn_audit_1_rules.txt", "PACK 1 OF 3 - THE RULES",
         "The design paper, then every file that decides whether a block or a transfer is valid.", RULES),
        ("cvn_audit_2_program.txt", "PACK 2 OF 3 - THE PROGRAM",
         "Networking, mining, wallets and start-up. Nothing here decides validity, but it decides\n"
         "what a node does with what it receives.", PROGRAM),
        ("cvn_audit_3_tests.txt", "PACK 3 OF 3 - THE TESTS",
         "The tests. They show what the authors checked, and by omission what they did not.", tests),
    ]
    os.makedirs(OUT, exist_ok=True)
    combined = []
    for name, title, purpose, files in packs:
        text = build(title, purpose, files)
        combined.append(text)
        with open(os.path.join(OUT, name), "w", encoding="utf-8", newline="\n") as handle:
            handle.write(text)
        print("%-26s %4d files  %7d characters  about %6d tokens" % (name, len(files), len(text), len(text) // 4))
    full = "\n".join(combined)
    with open(os.path.join(OUT, "cvn_audit_full.txt"), "w", encoding="utf-8", newline="\n") as handle:
        handle.write(full)
    print("%-26s %4d files  %7d characters  about %6d tokens" % ("cvn_audit_full.txt", sum(len(p[3]) for p in packs), len(full), len(full) // 4))
    print("Written to " + OUT)


if __name__ == "__main__":
    main()
