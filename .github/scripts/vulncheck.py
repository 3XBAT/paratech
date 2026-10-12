#!/usr/bin/env python3
"""Run govulncheck per module and fail on called vulnerabilities that are not in .github/vulncheck-allow.txt.

Run from paratech/ (as in CI): python3 ../.github/scripts/vulncheck.py
"""
import json
import pathlib
import subprocess
import sys

MODULES = ["desktop", "hub", "shared"]
TOOL = "golang.org/x/vuln/cmd/govulncheck@v1.9.0"
ALLOW = pathlib.Path(__file__).resolve().parent.parent / "vulncheck-allow.txt"


def allowed():
    ids = {}
    for line in ALLOW.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if line and not line.startswith("#"):
            ids[line.split("|")[0].strip()] = line
    return ids


def called_ids(module):
    out = subprocess.run(
        ["go", "run", TOOL, "-format", "json", "./..."],
        cwd=module, capture_output=True, text=True, check=False,
    )
    if out.returncode != 0:
        sys.exit(f"govulncheck failed in {module}:\n{out.stderr}")
    dec, text, pos, found = json.JSONDecoder(), out.stdout, 0, set()
    while pos < len(text):
        while pos < len(text) and text[pos].isspace():
            pos += 1
        if pos >= len(text):
            break
        msg, pos = dec.raw_decode(text, pos)
        finding = msg.get("finding")
        # A frame with a function means the vulnerable symbol is actually called (govulncheck exit code 3).
        if finding and finding.get("trace") and finding["trace"][0].get("function"):
            found.add(finding["osv"])
    return found


def main():
    allow = allowed()
    seen = set()
    bad = {}
    for module in MODULES:
        for osv in sorted(called_ids(module)):
            seen.add(osv)
            if osv not in allow:
                bad.setdefault(osv, []).append(module)
    for osv, mods in bad.items():
        print(f"::error::{osv} is called from {', '.join(mods)} and is not allowed: https://pkg.go.dev/vuln/{osv}")
    stale = sorted(set(allow) - seen)
    for osv in stale:
        print(f"::error::{osv} is allow-listed but no longer found: remove it from .github/vulncheck-allow.txt")
    for osv in sorted(seen & set(allow)):
        print(f"allowed: {allow[osv]}")
    sys.exit(1 if bad or stale else 0)


main()
