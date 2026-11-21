"""Quarantine of live eval cases (owner-approved, T185): every `Quarantine: "<value>"` in the Go
files under src/tests/evals/ names an open task in specs/backlog.md, so a quarantine ends with its
task. This runs on the whole repository (CI's Go job mounts only src/, which has no backlog).

The rule is `open_task_problems(go_sources, backlog)`. A value must look like T<digits> and have a
backlog line that starts `- [ ] <value> ` (exact ID; `- [X]`/`- [x]` is closed; a mention in the
middle of a line is not a task line).
"""
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
QUARANTINE = re.compile(r'Quarantine:\s*"([^"]*)"')
ID_FORM = re.compile(r"^T[0-9]+$")


def open_task_problems(go_sources, backlog):
    """Return (quarantines examined, problems) for {path: Go text} and the backlog text."""
    open_ids = set()
    for line in backlog.splitlines():
        m = re.match(r"^- \[ \] (T[0-9]+)(?:\s|$)", line)
        if m:
            open_ids.add(m.group(1))
    examined, problems = 0, []
    for path, text in sorted(go_sources.items()):
        for m in QUARANTINE.finditer(text):
            value = m.group(1)
            if value == "":  # an empty Quarantine is "not quarantined"
                continue
            examined += 1
            if not ID_FORM.match(value):
                problems.append(f'{path}: Quarantine "{value}" is not a task ID (T<digits>)')
            elif value not in open_ids:
                problems.append(f'{path}: Quarantine "{value}" is not an open task in specs/backlog.md')
    return examined, problems


def go(value):
    return {"a.go": f'{{Name: "x", Quarantine: "{value}"}}'}


class OpenTaskRule(unittest.TestCase):
    BACKLOG = "# Backlog\n- [ ] T185 open\n- [X] T186 done\n- [x] T187 done\n- [ ] T200 see T300\n"

    def test_open_task_passes(self):
        self.assertEqual(open_task_problems(go("T185"), self.BACKLOG), (1, []))

    def test_closed_task_fails(self):
        for v in ("T186", "T187"):
            self.assertEqual(len(open_task_problems(go(v), self.BACKLOG)[1]), 1, v)

    def test_missing_task_fails(self):
        self.assertEqual(len(open_task_problems(go("T999"), self.BACKLOG)[1]), 1)

    def test_malformed_id_fails(self):
        for v in ("example", "t185", "T", "T18x", " T185"):
            self.assertEqual(len(open_task_problems(go(v), "- [ ] T185 open\n")[1]), 1, v)

    def test_prefix_id_is_not_a_match(self):
        self.assertEqual(len(open_task_problems(go("T18"), self.BACKLOG)[1]), 1)

    def test_mid_line_mention_is_not_open(self):
        self.assertEqual(len(open_task_problems(go("T300"), self.BACKLOG)[1]), 1)

    def test_reopened_after_closed_line_is_open(self):
        self.assertEqual(open_task_problems(go("T5"), "- [X] T5 a\n- [ ] T5 b\n")[1], [])

    def test_last_line_without_newline_and_empty_backlog(self):
        self.assertEqual(open_task_problems(go("T5"), "- [ ] T5 last")[1], [])
        self.assertEqual(len(open_task_problems(go("T5"), "")[1]), 1)

    def test_empty_quarantine_is_not_counted(self):
        self.assertEqual(open_task_problems(go(""), self.BACKLOG), (0, []))

    def test_every_quarantine_is_checked(self):
        srcs = {"a.go": 'Quarantine: "T185"', "b.go": 'Quarantine: "T186"\nQuarantine: "T185"'}
        examined, problems = open_task_problems(srcs, self.BACKLOG)
        self.assertEqual((examined, len(problems)), (3, 1))


class OpenTaskRealRepository(unittest.TestCase):
    def test_real_quarantines_name_open_tasks(self):
        files = sorted((ROOT / "src" / "tests" / "evals").rglob("*.go"))
        self.assertTrue(files, "no .go file examined under src/tests/evals")
        sources = {str(f.relative_to(ROOT)): f.read_text() for f in files}
        backlog = (ROOT / "specs" / "backlog.md").read_text()
        examined, problems = open_task_problems(sources, backlog)
        print(f"examined {len(files)} .go files, {examined} quarantines")
        self.assertGreater(examined, 0, "no Quarantine found; the check lost its targets")
        self.assertEqual(problems, [])


if __name__ == "__main__":
    unittest.main()
