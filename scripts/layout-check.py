#!/usr/bin/env python3
"""Fail when the repository root holds anything outside the constitution's allowlist.

usage: python3 scripts/layout-check.py [ROOT]

The allowlist is the constitution's Layout constraint (.specify/memory/constitution.md,
"Technology and Repository Constraints"): the named entries below, plus tool dotfiles.
"""
import sys
from pathlib import Path

ALLOWED = {
    "src", "LICENSE", "NOTICE", "README.md", "SECURITY.md", "CONTRIBUTING.md",
    "AGENTS.md", "CLAUDE.md", ".specify", "specs", "docs", "scripts",
}


def main(argv: list[str]) -> int:
    root = Path(argv[1]) if len(argv) > 1 else Path(__file__).resolve().parent.parent
    entries = sorted(p.name for p in root.iterdir())
    print(f"layout-check: examined {len(entries)} entries in {root}")
    bad = [name for name in entries if name not in ALLOWED and not name.startswith(".")]
    for name in bad:
        print(name)
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
