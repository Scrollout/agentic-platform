"""Regression for US3's fresh review round 1 (feature 003, user story 3).

Spec: specs/003-one-command-start/spec.md, User Story 3 scenario 2: "every option is about where
their key and projects are, or whether other machines may reach the run page, and each has a
default". Contract: src/cli/contract.md, "Starting the platform": the `start` option table and the
`up`: remote access option table give each option's default.

The usage `ap --bogus` prints must give every option an entry that states its default, and the
`--name` entry must state both cases of the contract's default.
"""
import re
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_ap_usage import run_ap, CONTRACT  # noqa: E402,F401

OPTIONS = ["--check", "--credentials", "--projects", "--listen", "--name", "--certificate"]


def option_entries(usage):
    """Map each option in the usage's option list to its entry text (line plus indented continuations).

    The list runs from the line starting `start starts the platform` to the line starting
    `AP_PAGE_PORT`. An entry starts at a line whose first non-space character is `-` and ends at the
    next such line. Every `--option` named on the entry's first line is a key for that entry.
    """
    entries, current, keys, inside = {}, [], [], False
    for line in usage.splitlines():
        if line.startswith("start starts the platform"):
            inside = True
            continue
        if line.startswith("AP_PAGE_PORT"):
            break
        if not inside:
            continue
        if line.lstrip().startswith("-"):
            if keys:
                for k in keys:
                    entries[k] = "\n".join(current)
            current, keys = [line], re.findall(r"--[a-z-]+", line)
        elif keys:
            current.append(line)
    if keys:
        for k in keys:
            entries[k] = "\n".join(current)
    return entries


def missing_default(entries, options=OPTIONS):
    """Options with no entry, or an entry without the word `default`."""
    return [o for o in options if o not in entries or "default" not in entries[o].lower()]


class OptionEntriesSelfTest(unittest.TestCase):
    PLANTED = ("start starts the platform.\n"
               "  --check                show it\n"
               "  --name <host>          a name (default: x)\n"
               "                         more\n"
               "AP_PAGE_PORT, if set.\n")

    def test_entry_without_default_is_detected(self):
        e = option_entries(self.PLANTED)
        self.assertEqual(missing_default(e, ["--check", "--name", "--gone"]), ["--check", "--gone"])

    def test_clean_usage_passes(self):
        clean = self.PLANTED.replace("show it", "show it (default: off)")
        self.assertEqual(missing_default(option_entries(clean), ["--check", "--name"]), [])

    def test_continuation_lines_belong_to_entry(self):
        planted = self.PLANTED.replace("a name (default: x)\n", "a name\n").replace("more", "default here")
        self.assertIn("default here", option_entries(planted)["--name"])


class UsageOptionDefaultsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.usage = run_ap("--bogus")
        cls.text = cls.usage.stdout + cls.usage.stderr
        cls.entries = option_entries(cls.text)

    def test_every_option_has_an_entry_with_a_default(self):
        self.assertTrue(self.entries, "no option list found in the usage")
        bad = missing_default(self.entries)
        self.assertEqual(bad, [], f"options without an entry stating a default: {bad}")

    def test_name_default_matches_contract(self):
        e = self.entries.get("--name", "").lower()
        self.assertIn("127.0.0.1", e)
        self.assertIn("localhost", e)
        self.assertTrue("one address" in e or "that address" in e,
                        "--name entry must state the single-address case")


if __name__ == "__main__":
    unittest.main()
