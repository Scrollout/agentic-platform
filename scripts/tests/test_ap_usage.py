"""Tests for T019: the usage of `ap` states `start` as the contract does and names no internal part
(feature 003, user story 3, scenario 2).

Spec: specs/003-one-command-start/spec.md (User Story 3, FR-009); tasks T019, T020; constitution
Principle XVIII (an interface says what, not how).
Contract: src/cli/contract.md, "Starting the platform", "### `start`".

The checks are small pure functions, each proven against a planted violation and a clean text
(constitution: checks prove they can fail). The synopsis is read from the contract at test time.
"""
import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent.parent
AP = Path(os.environ.get("AP_TEST_AP") or REPO / "src/deploy/ap")
CONTRACT = REPO / "src/cli/contract.md"

# "sign in" / "sign-in" is the remote-access secret; it is removed before the search for "sign".
SIGN_IN = re.compile(r"\bsign[ -]in\b", re.I)
STEP_TOOL = r"(?:\S*build\.sh|\S*generate\.sh|cosign)"
STEP_SENTENCE = re.compile(r"\b(?:run|runs|execute|invoke|use)\b[^.\n]{0,40}?" + STEP_TOOL, re.I)


def normalise(text):
    return re.sub(r"\s+", " ", text).strip()


def start_section(contract_text):
    m = re.search(r"^### `start`\s*$(.*?)(?=^### |\Z)", contract_text, re.M | re.S)
    if not m:
        raise AssertionError("no '### `start`' section in the contract")
    return m.group(1)


def synopsis(contract_text):
    m = re.search(r"```text\n(.*?)```", start_section(contract_text), re.S)
    if not m:
        raise AssertionError("no text block in the contract's start section")
    return normalise(m.group(1))


def internal_words(text):
    """Every internal word in text (sign-in allowed), in order of appearance."""
    text = SIGN_IN.sub(" ", text)
    return [m.group(0) for m in re.finditer(
        r"(?<![\w-])(?:build\.sh|generate\.sh|images?|generate|generator|cosign|registry|signs?|signed|signing)(?![\w-])",
        text, re.I)]


def developer_steps(section):
    """Lines of the section that tell the developer to run a build, generator or signing step."""
    found, fenced = [], False
    for line in section.splitlines():
        if line.lstrip().startswith("```"):
            fenced = not fenced
            continue
        if fenced:
            if re.search(r"(?<![\w.-])" + STEP_TOOL + r"(?![\w-])", line, re.I):
                found.append(line.strip())
        elif STEP_SENTENCE.search(line):
            found.append(line.strip())
    return found


def run_ap(*args):
    base = Path(tempfile.mkdtemp(prefix="test-usage-"))
    try:
        env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "HOME": str(base / "home"),
               "XDG_CONFIG_HOME": str(base / "c"), "XDG_STATE_HOME": str(base / "s"),
               "XDG_DATA_HOME": str(base / "d"), "XDG_RUNTIME_DIR": str(base / "r")}
        return subprocess.run([str(AP), *args], env=env, capture_output=True, text=True,
                              timeout=30, cwd=str(base))
    finally:
        shutil.rmtree(base, ignore_errors=True)


class UsageTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.result = run_ap("--bogus")
        cls.usage = cls.result.stderr

    def test_unknown_option_exits_3_with_usage(self):
        self.assertEqual(self.result.returncode, 3, "stderr: %r" % self.usage)
        self.assertTrue(self.usage.startswith("usage:"), "stderr began: %r" % self.usage[:80])

    def test_usage_has_the_contract_synopsis_of_start(self):
        want = synopsis(CONTRACT.read_text())
        got = normalise(re.sub(r"^usage: ", "", self.usage))
        self.assertIn(want, got, "synopsis from %s:\n  %s\nnot contiguous in usage:\n  %s" % (CONTRACT, want, got))

    def test_usage_names_no_internal_part(self):
        found = internal_words(self.usage)
        self.assertEqual(found, [], "internal words in the usage: %s\nexamined:\n%s" % (found, self.usage))

    def test_contract_start_section_gives_no_developer_step(self):
        section = start_section(CONTRACT.read_text())
        steps = developer_steps(section)
        self.assertEqual(steps, [], "developer steps in %s start section: %s" % (CONTRACT, steps))


class CheckSelfTest(unittest.TestCase):
    def test_internal_words_finds_planted(self):
        self.assertEqual(internal_words("Build the images first with src/deploy/build.sh <name>."),
                         ["images", "build.sh"])
        self.assertEqual(internal_words("ap registry"), ["registry"])
        self.assertEqual(internal_words("signing key; cosign signs; signed"),
                         ["signing", "cosign", "signs", "signed"])
        self.assertEqual(internal_words("scripts/generate.sh and the generator"), ["generate.sh", "generator"])

    def test_internal_words_passes_clean(self):
        for t in ("with a sign-in secret", "sign in as ap", "ap start [--check] --listen <address>",
                  "Sign-In secret; design assigned"):
            self.assertEqual(internal_words(t), [], t)

    def test_synopsis_reads_the_contract_block(self):
        doc = "### `start`\n\n```text\nap start [--check]\n         [--x]\n```\n\ntext\n### `up`\n```text\nap up\n```\n"
        self.assertEqual(synopsis(doc), "ap start [--check] [--x]")

    def test_developer_steps_finds_planted(self):
        self.assertTrue(developer_steps("First run `src/deploy/build.sh cli`, then go."))
        self.assertTrue(developer_steps("Please Run scripts/generate.sh now."))
        self.assertTrue(developer_steps("run cosign verify"))
        self.assertTrue(developer_steps("text\n```sh\ncosign sign x\n```\n"))
        self.assertTrue(developer_steps("```sh\nsrc/deploy/build.sh --changed\n```\n"))

    def test_developer_steps_passes_clean(self):
        clean = ("`start` itself builds the images through `src/deploy/build.sh --changed`.\n"
                 "```text\nap start [--check]\n```\nSigns with cosign inside build.sh.\n")
        self.assertEqual(developer_steps(clean), [])


if __name__ == "__main__":
    unittest.main()
