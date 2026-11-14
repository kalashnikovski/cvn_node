"""Compares the RandomX engine kept in this repository with the original it
was copied from, and writes the result to internal/randomx/DIFFERENCES.md.

The original is go-randomx version 4.7.0. Fetch it with Go, which checks it
against Go's public checksum database:

    go mod download -json git.gammaspectra.live/P2Pool/go-randomx/v4@v4.7.0

and pass the folder that command prints as "Dir":

    python audit/compare_randomx.py FOLDER

Every file is compared byte for byte after two allowances, which are the
first two changes README_CVN.md lists: line endings, and the import path
(the original's own name against this repository's).
"""
import difflib
import os
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OURS = os.path.join(ROOT, "internal", "randomx")
THEIR_PATH = "git.gammaspectra.live/P2Pool/go-randomx/v4"
OUR_PATH = "cvn_node/internal/randomx"
UPSTREAM_SUM = "h1:bgsnAaOAyBMIU6hmg7JibsoTeSt9C6PbtEHn1fMvPtg="
NEWLINE = chr(10)


def files_under(folder):
    found = {}
    for base, _, names in os.walk(folder):
        for name in names:
            full = os.path.join(base, name)
            found[os.path.relpath(full, folder).replace(os.sep, "/")] = full
    return found


def text_of(path, original):
    with open(path, "rb") as handle:
        text = handle.read().decode("utf-8", errors="replace").replace(chr(13) + NEWLINE, NEWLINE)
    return text.replace(THEIR_PATH, OUR_PATH) if original else text


def main():
    if len(sys.argv) != 2 or not os.path.isdir(sys.argv[1]):
        sys.exit(__doc__)
    theirs, ours = files_under(sys.argv[1]), files_under(OURS)
    ours.pop("DIFFERENCES.md", None)

    same, changed = [], []
    for name in sorted(set(theirs) & set(ours)):
        a, b = text_of(theirs[name], True), text_of(ours[name], False)
        if a == b:
            same.append(name)
        else:
            diff = list(difflib.unified_diff(a.split(NEWLINE), b.split(NEWLINE), "original/" + name, "cvn/" + name, lineterm="", n=2))
            changed.append((name, diff))
    only_theirs = sorted(set(theirs) - set(ours))
    only_ours = sorted(set(ours) - set(theirs))

    out = [
        "# The RandomX engine compared with its original",
        "",
        "Written by `audit/compare_randomx.py`. Do not edit by hand; run the script again.",
        "",
        "The original is `%s` version 4.7.0, as fetched by Go and checked against Go's public checksum database (`%s`)." % (THEIR_PATH, UPSTREAM_SUM),
        "Files are compared byte for byte, allowing only for line endings and for the import path.",
        "",
        "| | Files |",
        "|---|---|",
        "| Identical to the original | %d |" % len(same),
        "| Changed | %d |" % len(changed),
        "| In the original and not copied | %d |" % len(only_theirs),
        "| Added by CVN | %d |" % len(only_ours),
        "",
        "## Changed files, with every changed line",
        "",
    ]
    for name, diff in changed:
        out += ["### `%s`" % name, "", "```diff"] + diff + ["```", ""]
    out += ["## Added by CVN", ""] + ["- `%s`" % name for name in only_ours] + [""]
    out += ["## In the original and not copied", ""] + ["- `%s`" % name for name in only_theirs] + [""]
    out += ["## Identical to the original", ""] + ["- `%s`" % name for name in same] + [""]
    with open(os.path.join(OURS, "DIFFERENCES.md"), "w", encoding="utf-8", newline=NEWLINE) as handle:
        handle.write(NEWLINE.join(out))
    print("identical %d, changed %d, not copied %d, added %d" % (len(same), len(changed), len(only_theirs), len(only_ours)))
    for name, diff in changed:
        print("  changed: %-40s %d lines of difference" % (name, sum(1 for line in diff if line[:1] in "+-" and line[:3] not in ("+++", "---"))))


if __name__ == "__main__":
    main()
