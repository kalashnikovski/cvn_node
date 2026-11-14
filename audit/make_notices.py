"""Writes THIRD_PARTY_NOTICES.md from the licence files of what the project is
built with: the Go modules as they sit in Go's module cache, and Svelte as
installed in frontend/node_modules (run `npm ci --legacy-peer-deps` there
first).

    python audit/make_notices.py
"""
import json
import os
import subprocess

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
NEWLINE = chr(10)


def clean(text):
    return text.replace(chr(13) + NEWLINE, NEWLINE).strip()


def go_modules():
    out = subprocess.run(["go", "list", "-m", "-json", "all"], cwd=ROOT, capture_output=True, text=True, check=True).stdout
    modules, depth, start = [], 0, 0
    for i, ch in enumerate(out):
        if ch == "{":
            if depth == 0:
                start = i
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                modules.append(json.loads(out[start:i + 1]))
    return [m for m in modules if not m.get("Main")]


def licence_file(folder):
    if folder and os.path.isdir(folder):
        for name in sorted(os.listdir(folder)):
            if name.upper().split(".")[0] in ("LICENSE", "LICENCE", "COPYING", "UNLICENSE"):
                return os.path.join(folder, name)
    return None


def main():
    svelte_folder = os.path.join(ROOT, "frontend", "node_modules", "svelte")
    with open(os.path.join(svelte_folder, "package.json"), encoding="utf-8") as handle:
        svelte_version = json.load(handle)["version"]
    with open(os.path.join(svelte_folder, "LICENSE.md"), encoding="utf-8") as handle:
        svelte_licence = clean(handle.read())
    with open(os.path.join(ROOT, "internal", "randomx", "LICENSE"), encoding="utf-8") as handle:
        randomx_licence = clean(handle.read())

    head = [
        "# Third-party notices",
        "",
        "The Covenant Standard (CVN) program is published under the MIT licence (see `LICENSE`). It contains, or is built with, the work of others listed here. Each is used under its own licence, reproduced below as its authors published it.",
        "",
        "This file is written by `audit/make_notices.py`.",
        "",
        "## Kept inside this repository",
        "",
        "**RandomX engine** (`internal/randomx/`). A copy of the Go implementation of RandomX from `git.gammaspectra.live/P2Pool/go-randomx`, version 4.7.0, with the changes listed in `internal/randomx/README_CVN.md` and shown line by line in `internal/randomx/DIFFERENCES.md`. BSD 3-clause licence; its text is in `internal/randomx/LICENSE` and is reproduced below. RandomX itself was designed by tevador and contributors.",
        "",
        "**Svelte** %s, the library the desktop wallet's screens are written with. Its runtime is compiled into the built screens (`frontend/dist`) and so into the desktop wallet. MIT licence; its text is reproduced below. The tool that builds the screens, Vite, is not part of any program." % svelte_version,
        "",
        "## Separate programs",
        "",
        "**CVN edition of XMRig** (the optional fast miner). It is not part of this program and none of its code is in this repository. It is a separate program, a modified XMRig, published under the GNU General Public License version 3 with its complete source at https://github.com/kalashnikovski/cvn-xmrig. XMRig is copyright its authors (https://github.com/xmrig/xmrig). The CVN program starts it as a separate process when asked to and talks to it over a local network connection.",
        "",
        "## Built with",
        "",
        "**The Go programming language and its standard library.** Copyright The Go Authors. BSD 3-clause licence: https://go.dev/LICENSE",
        "",
        "The modules below are named in `go.mod`. The node and miner use bbolt and golang.org/x/sys; the others are there for the Windows desktop wallet (Wails and what it depends on).",
        "",
    ]
    listing, texts, absent = [], [], 0
    for module in go_modules():
        path, version = module["Path"], module.get("Version", "")
        found = licence_file(module.get("Dir", ""))
        if not found:
            absent += 1  # in the module graph, not downloaded: not built into anything
            continue
        with open(found, encoding="utf-8", errors="replace") as handle:
            text = clean(handle.read())
        listing.append("- `%s` %s" % (path, version))
        texts += ["### %s %s" % (path, version), "", "```text", text, "```", ""]

    body = head + listing + [
        "",
        "## Licence texts",
        "",
        "### RandomX engine (internal/randomx)",
        "",
        "```text",
        randomx_licence,
        "```",
        "",
        "### Svelte %s" % svelte_version,
        "",
        "```text",
        svelte_licence,
        "```",
        "",
    ] + texts
    with open(os.path.join(ROOT, "THIRD_PARTY_NOTICES.md"), "w", encoding="utf-8", newline=NEWLINE) as handle:
        handle.write(NEWLINE.join(body))
    print("Go modules with a licence: %d; in the module graph but not downloaded, so not built in: %d; Svelte %s" % (len(listing), absent, svelte_version))


if __name__ == "__main__":
    main()
