"""Contract of scripts/ci-evals-measure.py (feature 008, T003), written before the script.

Judged from the script's docstring (contract), specs/008-eval-variance/data-model.md ("Eval
measurement", Case, Trial) and the report shapes of src/tests/evals/{analyst,steps}.

Contract points covered:
- exactly the contract's keys at the top, case and trial levels (nothing else), `version` 1;
- `key` is `<suite>` or `<suite>-<cases>`; `suite`, `cases`, `commit`, `branch`, `run_id`,
  `run_attempt` take their fallbacks when the environment breaks the rule;
- per case: `case`, `agent` (the case's, else the suite), `quarantined_by` only when present,
  `trials` (not a list is empty; non-object entries left out); order of cases and trials kept;
- per trial: `passed`, `seconds`, `attempts`, `input_tokens`, `output_tokens`, `cost_usd`, each
  absent when the report lacks it or breaks its rule;
- planted credential, stderr, exit_error, grader reasons, changed_files, attack and answer text,
  and any unnamed field, never reach the file;
- unreadable, non-list or missing report, and unwritable output: no file, a message, exit 0;
  wrong argument count: exit 2, nothing written; missing output folder is created;
- it starts no process (PATH holding no gh and no git, and recording stubs that stay unused).

Edge cases chosen: empty list; case without trials; mixed analyst and steps cases in one report;
unicode names; 200/201-character names (also by characters vs bytes); durations 0s, 1h2m3.5s,
-1s, 1.5, a number, NaN; huge integers and 1e999; `true` as an integer; 3.0 and 3.5 as tokens;
usage null/list/string/{}; cost as string, NaN, Infinity, negative; quarantined_by T185, t185, T,
T1a, a trailing newline, a number; branch with a newline, 255 and 256 characters; suite with a
trailing newline; output folder missing (nested), a directory, below a file.

Assumptions where the contract is silent (the code author must meet or raise them):
- an unset or empty SUITE gives "?"; an unset EVAL_CASES gives "" (not "?");
- `key` is built from the already sanitised suite and cases (so cases "?" gives "<suite>-?");
- "letters" are ASCII letters; a trailing newline breaks the rule (no `$` slip);
- the 200-character limit counts characters, not bytes;
- 3.0 is not an integer (a token count must be a JSON integer);
- a huge integer is an integer and is kept as is;
- a `null` quarantined_by is not tested.
"""
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from test_ci_scripts import base_env

SCRIPT = Path(__file__).resolve().parent.parent / "ci-evals-measure.py"
SHA = "0123456789abcdef0123456789abcdef01234567"
KEY = "sk-" + "ant-" + "A" * 30  # built at run time, never a literal
ANSWER = "the-answer-is-swordfish"
TOP = {"version", "suite", "cases", "key", "commit", "branch", "run_id", "run_attempt",
       "measured_cases"}
CASE = {"case", "agent", "trials"}
VALID_ENV = {"SUITE": "analyst", "EVAL_CASES": "", "GITHUB_SHA": SHA, "GITHUB_REF_NAME": "main",
             "GITHUB_RUN_ID": "123", "GITHUB_RUN_ATTEMPT": "1"}


class Base(unittest.TestCase):
    def setUp(self):
        t = tempfile.TemporaryDirectory()
        self.addCleanup(t.cleanup)
        self.dir = Path(t.name)
        self.out = self.dir / "out" / "measurement.json"

    def env(self, **over):
        e = base_env(self)
        for k in VALID_ENV:
            e.pop(k, None)
        e.update(VALID_ENV)
        for k, v in over.items():
            if v is None:
                e.pop(k, None)
            else:
                e[k] = v
        return e

    def go(self, report, env=None, out=None, raw=None, args=None):
        rp = self.dir / "report.json"
        if raw is not None:
            rp.write_text(raw, encoding="utf-8")
        elif report is not None:
            rp.write_text(json.dumps(report), encoding="utf-8")
        argv = args if args is not None else [str(rp), str(out or self.out)]
        r = subprocess.run([sys.executable, str(SCRIPT), *argv], capture_output=True, text=True,
                           env=env or self.env(), cwd=self.dir, timeout=60)
        return r

    def measure(self, report, env=None, raw=None):
        r = self.go(report, env, raw=raw)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertTrue(self.out.is_file(), "no measurement written: " + r.stdout + r.stderr)
        return json.loads(self.out.read_text(encoding="utf-8"))

    def env_field(self, field, value):
        """Measure an empty report with one environment variable set (None: unset)."""
        name = {"suite": "SUITE", "cases": "EVAL_CASES", "commit": "GITHUB_SHA",
                "branch": "GITHUB_REF_NAME", "run_id": "GITHUB_RUN_ID",
                "run_attempt": "GITHUB_RUN_ATTEMPT"}[field]
        return self.measure([], self.env(**{name: value}))

    def one_trial(self, trial):
        m = self.measure([{"case": "c", "trials": [trial]}])
        return m["measured_cases"][0]["trials"][0]

    def one_case(self, case, env=None):
        return self.measure([case], env)["measured_cases"][0]


def trial(**kw):
    return {"trial": 1, "passed": True, **kw}


class Shape(Base):
    def test_keys_are_exactly_the_contracts_at_every_level(self):
        """Full analyst-like report: only the named keys appear at top, case and trial level."""
        usage = {"input_tokens": 10, "output_tokens": 5, "cost_usd": 0.25, "model": "x",
                 "extra": 1}
        rep = [{"case": "a/one", "passed": True, "quarantined_by": "T185", "extra": 1,
                "trials": [{"trial": 1, "passed": True, "duration": "12s", "attempts": 2,
                            "usage": usage, "results": [], "stderr": "e", "exit_error": "x",
                            "changed_files": ["f"], "adversarial": True, "attack": "a",
                            "extra": 3}]}]
        m = self.measure(rep)
        self.assertEqual(set(m), TOP)
        c = m["measured_cases"][0]
        self.assertEqual(set(c), CASE | {"quarantined_by"})
        self.assertEqual(set(c["trials"][0]), {"passed", "seconds", "attempts", "input_tokens",
                                               "output_tokens", "cost_usd"})
        self.assertEqual(c["trials"][0], {"passed": True, "seconds": 12, "attempts": 2,
                                          "input_tokens": 10, "output_tokens": 5,
                                          "cost_usd": 0.25})

    def test_constants_and_environment_values_are_copied(self):
        m = self.measure([])
        self.assertEqual((m["version"], m["suite"], m["cases"], m["key"], m["commit"],
                          m["branch"], m["run_id"], m["run_attempt"], m["measured_cases"]),
                         (1, "analyst", "", "analyst", SHA, "main", 123, 1, []))
        self.assertIs(type(m["version"]), int)
        self.assertIs(type(m["run_id"]), int)

    def test_key_has_cases_when_not_empty(self):
        m = self.measure([], self.env(SUITE="steps", EVAL_CASES="code-review1"))
        self.assertEqual((m["suite"], m["cases"], m["key"]), ("steps", "code-review1",
                                                              "steps-code-review1"))

    def test_trial_with_nothing_has_only_passed(self):
        self.assertEqual(self.one_trial({"trial": 1}), {"passed": False})
        self.assertEqual(self.one_trial({}), {"passed": False})

    def test_empty_report_gives_empty_cases(self):
        self.assertEqual(self.measure([])["measured_cases"], [])

    def test_case_without_trials_key_or_with_empty_list(self):
        for case in ({"case": "c"}, {"case": "c", "trials": []}):
            self.assertEqual(self.one_case(case)["trials"], [])

    def test_trials_not_a_list_is_empty(self):
        for bad in ("abc", 5, None, {"a": 1}, True):
            with self.subTest(bad=bad):
                self.assertEqual(self.one_case({"case": "c", "trials": bad})["trials"], [])

    def test_non_object_entries_are_left_out_in_order(self):
        rep = [None, 5, "s", [], {"case": "a", "trials": [1, None, "x", [], trial(attempts=1),
                                                          trial(attempts=2)]}, True,
               {"case": "b"}]
        m = self.measure(rep)
        self.assertEqual([c["case"] for c in m["measured_cases"]], ["a", "b"])
        self.assertEqual([t["attempts"] for t in m["measured_cases"][0]["trials"]], [1, 2])

    def test_order_of_cases_and_trials_is_the_reports_not_sorted(self):
        names = ["z", "a", "m", "a"]
        rep = [{"case": n, "trials": [trial(attempts=3), trial(attempts=1), trial(attempts=2)]}
               for n in names]
        m = self.measure(rep)
        self.assertEqual([c["case"] for c in m["measured_cases"]], names)
        for c in m["measured_cases"]:
            self.assertEqual([t["attempts"] for t in c["trials"]], [3, 1, 2])

    def test_mixed_analyst_and_steps_cases_in_one_report(self):
        """Analyst case: attempts, no agent (agent = suite). Steps case: agent, no attempts."""
        rep = [{"case": "an", "trials": [{"trial": 1, "passed": True, "attempts": 2}]},
               {"case": "st", "agent": "code",
                "trials": [{"trial": 1, "passed": True, "changed_files": ["a"]}]}]
        a, s = self.measure(rep, self.env(SUITE="steps"))["measured_cases"]
        self.assertEqual((a["agent"], a["trials"]), ("steps", [{"passed": True, "attempts": 2}]))
        self.assertEqual((s["agent"], s["trials"]), ("code", [{"passed": True}]))


class EnvironmentRules(Base):
    def test_suite_breaking_the_rule_is_question_mark(self):
        for bad in (None, "", "a b", "a_b", "a/b", "ab\n", "é", "a.b"):
            with self.subTest(bad=bad):
                m = self.env_field("suite", bad)
                self.assertEqual((m["suite"], m["key"]), ("?", "?"))

    def test_suite_with_letters_digits_dash_is_kept(self):
        self.assertEqual(self.env_field("suite", "Ab-9")["suite"], "Ab-9")

    def test_cases_breaking_the_rule_is_question_mark_and_key_follows(self):
        for bad in ("a_b", "a b", "a\n", "é", "a,b"):
            with self.subTest(bad=bad):
                m = self.env_field("cases", bad)
                self.assertEqual((m["cases"], m["key"]), ("?", "analyst-?"))

    def test_cases_unset_or_empty_is_empty(self):
        for v in (None, ""):
            m = self.env_field("cases", v)
            self.assertEqual((m["cases"], m["key"]), ("", "analyst"))

    def test_commit_must_be_40_lowercase_hex(self):
        for bad in (None, "", SHA.upper(), SHA[:39], SHA + "0", "g" * 40, SHA + "\n", " " + SHA):
            with self.subTest(bad=bad):
                self.assertEqual(self.env_field("commit", bad)["commit"], "")
        self.assertEqual(self.env_field("commit", "f" * 40)["commit"], "f" * 40)

    def test_run_id_and_attempt_positive_integer_else_zero(self):
        for f in ("run_id", "run_attempt"):
            for bad in (None, "", "abc", "-1", "0", "1.5", "1\n", "1e3", "12abc"):
                with self.subTest(field=f, bad=bad):
                    self.assertEqual(self.env_field(f, bad)[f], 0)
            m = self.env_field(f, "9007199254740993")
            self.assertEqual(m[f], 9007199254740993)
            self.assertIs(type(m[f]), int)

    def test_branch_rules(self):
        self.assertEqual(self.env_field("branch", None)["branch"], "")
        self.assertEqual(self.env_field("branch", "feat/008-ünï")["branch"], "feat/008-ünï")
        self.assertEqual(self.env_field("branch", "a" * 255)["branch"], "a" * 255)
        for bad in ("a" * 256, "ab\ncd", "ab\n", "a\rb", "a\x1bb", "a\x7fb", "a\tb"):
            with self.subTest(bad=bad):
                self.assertEqual(self.env_field("branch", bad)["branch"], "")


class CaseRules(Base):
    def test_case_name_limit_is_200_characters(self):
        self.assertEqual(self.one_case({"case": "n" * 200})["case"], "n" * 200)
        self.assertEqual(self.one_case({"case": "n" * 201})["case"], "?")

    def test_unicode_name_kept_and_limit_counts_characters(self):
        for name in ("naïve/ケース/🙂", "é" * 200):
            self.assertEqual(self.one_case({"case": name})["case"], name)
        self.assertEqual(self.one_case({"case": "é" * 201})["case"], "?")

    def test_case_name_missing_or_not_a_string_is_question_mark(self):
        for bad in ({}, {"case": None}, {"case": 5}, {"case": ["a"]}, {"case": True}):
            self.assertEqual(self.one_case(bad)["case"], "?", bad)

    def test_agent_from_case_else_suite(self):
        self.assertEqual(self.one_case({"case": "c", "agent": "code-2"})["agent"], "code-2")
        self.assertEqual(self.one_case({"case": "c"})["agent"], "analyst")

    def test_agent_breaking_the_rule_is_question_mark(self):
        for bad in ("a b", "a_b", "a\n", "é", 5, ["a"], True):
            with self.subTest(bad=bad):
                self.assertEqual(self.one_case({"case": "c", "agent": bad})["agent"], "?")

    def test_agent_fallback_is_the_suite_sanitised(self):
        self.assertEqual(self.one_case({"case": "c"}, self.env(SUITE="bad suite"))["agent"], "?")

    def test_quarantined_by_only_when_present(self):
        self.assertNotIn("quarantined_by", self.one_case({"case": "c"}))
        self.assertEqual(self.one_case({"case": "c", "quarantined_by": "T185"})["quarantined_by"],
                         "T185")
        self.assertEqual(self.one_case({"case": "c", "quarantined_by": "T0"})["quarantined_by"],
                         "T0")

    def test_quarantined_by_breaking_the_rule_is_question_mark_still_quarantined(self):
        for bad in ("t185", "T", "T1a", "T185\n", " T185", "185", "TT1", "", "T-1", 185,
                    ["T1"], True, {"a": 1}):
            with self.subTest(bad=bad):
                c = self.one_case({"case": "c", "quarantined_by": bad})
                self.assertEqual(c.get("quarantined_by"), "?")


class TrialRules(Base):
    def test_passed_only_true_is_true(self):
        self.assertIs(self.one_trial({"passed": True})["passed"], True)
        self.assertIs(self.one_trial({"passed": False})["passed"], False)
        for bad in ("true", "True", 1, 0, None, [], {}, "false"):
            with self.subTest(bad=bad):
                self.assertIs(self.one_trial({"passed": bad})["passed"], False)

    def test_go_durations_parse_to_seconds(self):
        cases = {"850ms": 0.85, "12s": 12, "1m2.5s": 62.5, "1h0m0s": 3600, "1.234s": 1.234,
                 "0s": 0, "1h2m3.5s": 3723.5, "1m": 60, "2h": 7200, "0.5s": 0.5}
        for text, want in cases.items():
            with self.subTest(duration=text):
                s = self.one_trial({"duration": text})["seconds"]
                self.assertAlmostEqual(s, want, places=6)
                self.assertGreaterEqual(s, 0)

    def test_unparsable_or_negative_duration_leaves_seconds_absent(self):
        for bad in ("-1s", "1.5", "abc", "", "12S", "s", "1 s", "1m 2s", "NaNs", "1e3s", "-0.5s",
                    "12s\n", 5, 1.5, None, True, [], "Infs"):
            with self.subTest(duration=bad):
                self.assertNotIn("seconds", self.one_trial({"duration": bad}))

    def test_attempts_integer_at_least_zero(self):
        for good in (0, 1, 7, 10 ** 20):
            t = self.one_trial({"attempts": good})
            self.assertEqual(t["attempts"], good)
            self.assertIs(type(t["attempts"]), int)

    def test_attempts_breaking_the_rule_is_absent(self):
        for bad in (-1, 1.5, 3.0, "3", True, False, None, [], {}):
            with self.subTest(attempts=bad):
                self.assertNotIn("attempts", self.one_trial({"attempts": bad}))

    def test_tokens_integer_at_least_zero(self):
        t = self.one_trial({"usage": {"input_tokens": 0, "output_tokens": 10 ** 20}})
        self.assertEqual((t["input_tokens"], t["output_tokens"]), (0, 10 ** 20))

    def test_tokens_breaking_the_rule_are_absent(self):
        for bad in (-1, 3.5, 3.0, "7", True, None, [], {}):
            with self.subTest(tokens=bad):
                t = self.one_trial({"usage": {"input_tokens": bad, "output_tokens": bad}})
                self.assertNotIn("input_tokens", t)
                self.assertNotIn("output_tokens", t)

    def test_each_token_field_is_judged_alone(self):
        t = self.one_trial({"usage": {"input_tokens": 4, "output_tokens": -2}})
        self.assertEqual(t["input_tokens"], 4)
        self.assertNotIn("output_tokens", t)
        t = self.one_trial({"usage": {"output_tokens": 6}})
        self.assertEqual(t["output_tokens"], 6)
        self.assertNotIn("input_tokens", t)

    def test_usage_not_an_object_gives_no_usage_fields(self):
        for bad in (None, [], "x", 5, True, [{"input_tokens": 1}]):
            with self.subTest(usage=bad):
                t = self.one_trial({"passed": True, "usage": bad})
                self.assertEqual(t, {"passed": True})
        self.assertEqual(self.one_trial({"passed": True, "usage": {}}), {"passed": True})

    def test_cost_finite_number_at_least_zero(self):
        for good, want in ((0, 0), (0.0, 0), (0.0123, 0.0123), (2, 2), (1e-9, 1e-9)):
            with self.subTest(cost=good):
                self.assertEqual(self.one_trial({"usage": {"cost_usd": good}})["cost_usd"], want)

    def test_cost_breaking_the_rule_is_absent(self):
        for bad in (float("nan"), float("inf"), float("-inf"), -1, -0.01, "0.1", "", True,
                    False, None, [], {}):
            with self.subTest(cost=bad):
                self.assertNotIn("cost_usd", self.one_trial({"usage": {"cost_usd": bad}}))

    def test_cost_too_large_for_a_float_is_absent(self):
        """1e999 reads as infinity in Python's parser; it is not finite."""
        raw = ('[{"case":"c","trials":[{"passed":true,"usage":{"cost_usd":1e999,'
               '"input_tokens":2}}]}]')
        t = self.measure(None, raw=raw)["measured_cases"][0]["trials"][0]
        self.assertNotIn("cost_usd", t)
        self.assertEqual(t["input_tokens"], 2)

    def test_nan_infinity_tokens_and_attempts_are_absent(self):
        raw = ('[{"case":"c","trials":[{"passed":true,"attempts":NaN,"duration":"1s",'
               '"usage":{"input_tokens":Infinity,"output_tokens":-Infinity}}]}]')
        t = self.measure(None, raw=raw)["measured_cases"][0]["trials"][0]
        self.assertEqual(t, {"passed": True, "seconds": 1})


class NeverCopied(Base):
    def test_protected_content_and_unnamed_fields_never_appear(self):
        rep = [{"case": "c", "agent": "code", "passed": True,
                "stderr": KEY, "exit_error": KEY, "answer": ANSWER, "secret_note": KEY,
                "trials": [{"trial": 1, "passed": True, "duration": "1s", "stderr": KEY,
                            "exit_error": KEY + ANSWER, "changed_files": [KEY, ANSWER],
                            "attack": KEY + ANSWER, "adversarial": True, "answer": ANSWER,
                            "results": [{"grader": "g", "passed": False,
                                         "reason": "leak " + KEY + " " + ANSWER,
                                         "counts": {"failed": [KEY]}}],
                            "usage": {"input_tokens": 1, "prompt": ANSWER, "note": KEY}}]}]
        r = self.go(rep)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        text = self.out.read_text(encoding="utf-8")
        for secret in (KEY, "sk-ant", ANSWER, "swordfish", "leak"):
            self.assertNotIn(secret, text)
        self.assertEqual(set(json.loads(text)["measured_cases"][0]["trials"][0]),
                         {"passed", "seconds", "input_tokens"})

    def test_a_credential_in_a_rule_breaking_value_is_not_copied(self):
        """A value that breaks its rule is replaced by the fallback, never copied."""
        env = self.env(SUITE=KEY + "_", EVAL_CASES=KEY + "_", GITHUB_SHA=KEY, GITHUB_RUN_ID=KEY,
                       GITHUB_RUN_ATTEMPT=KEY, GITHUB_REF_NAME=KEY + "\n")
        rep = [{"case": KEY * 10, "agent": KEY + " x", "quarantined_by": KEY,
                "trials": [{"passed": KEY, "duration": KEY, "attempts": KEY,
                            "usage": {"input_tokens": KEY, "output_tokens": KEY,
                                      "cost_usd": KEY}}]}]
        self.assertEqual(self.go(rep, env).returncode, 0)
        text = self.out.read_text(encoding="utf-8")
        self.assertNotIn("sk-ant", text)
        self.assertNotIn(KEY, text)


class Failures(Base):
    def assert_says_why_and_writes_nothing(self, r, path=None):
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertTrue((r.stdout + r.stderr).strip(), "it must say what went wrong")
        self.assertNotIn("Traceback", r.stdout + r.stderr)
        self.assertFalse((path or self.out).exists())

    def test_report_that_is_not_json(self):
        for raw in ("not json", "", "[", "[1,", "\x00\xff"):
            with self.subTest(raw=raw):
                self.assert_says_why_and_writes_nothing(self.go(None, raw=raw))

    def test_report_that_is_not_a_list(self):
        for raw in ("{}", '{"case": "c"}', "null", '"s"', "5", "true", '{"cases": []}'):
            with self.subTest(raw=raw):
                self.assert_says_why_and_writes_nothing(self.go(None, raw=raw))

    def test_missing_report(self):
        r = self.go(None, args=[str(self.dir / "nope.json"), str(self.out)])
        self.assert_says_why_and_writes_nothing(r)

    def test_report_that_is_a_directory(self):
        (self.dir / "d").mkdir()
        r = self.go(None, args=[str(self.dir / "d"), str(self.out)])
        self.assert_says_why_and_writes_nothing(r)

    def test_report_that_is_not_utf8(self):
        (self.dir / "report.json").write_bytes(b'[{"case": "\xff\xfe"}]')
        r = self.go(None)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertNotIn("Traceback", r.stderr)

    def test_output_path_that_is_a_directory(self):
        d = self.dir / "outdir"
        d.mkdir()
        r = self.go([], out=d)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertTrue((r.stdout + r.stderr).strip())
        self.assertNotIn("Traceback", r.stdout + r.stderr)
        self.assertEqual(list(d.iterdir()), [])

    def test_output_folder_below_a_file(self):
        (self.dir / "afile").write_text("x")
        out = self.dir / "afile" / "sub" / "m.json"
        r = self.go([], out=out)
        self.assert_says_why_and_writes_nothing(r, out)

    @unittest.skipIf(os.geteuid() == 0, "root can write anywhere")
    def test_output_folder_not_writable(self):
        ro = self.dir / "ro"
        ro.mkdir()
        ro.chmod(0o500)
        self.addCleanup(ro.chmod, 0o700)
        out = ro / "m.json"
        self.assert_says_why_and_writes_nothing(self.go([], out=out), out)

    def test_wrong_number_of_arguments_exits_2_and_writes_nothing(self):
        rp = self.dir / "report.json"
        rp.write_text("[]")
        for args in ([], [str(rp)], [str(rp), str(self.out), "extra"]):
            with self.subTest(args=len(args)):
                r = self.go(None, args=args)
                self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
                self.assertFalse(self.out.exists())
                self.assertFalse(self.out.parent.exists())


class OutputFile(Base):
    def test_missing_output_folder_is_created_nested(self):
        out = self.dir / "a" / "b" / "c" / "m.json"
        self.assertEqual(self.go([], out=out).returncode, 0)
        self.assertEqual(json.loads(out.read_text())["version"], 1)

    def test_existing_output_folder_and_file_are_overwritten(self):
        self.out.parent.mkdir(parents=True)
        self.out.write_text("old")
        self.assertEqual(self.go([]).returncode, 0)
        self.assertEqual(json.loads(self.out.read_text())["measured_cases"], [])

    def test_same_input_gives_same_file(self):
        rep = [{"case": "a", "trials": [trial(duration="1s", attempts=1)]}]
        self.measure(rep)
        first = self.out.read_bytes()
        self.measure(rep)
        self.assertEqual(first, self.out.read_bytes())

    def test_file_is_utf8_json_and_keeps_unicode_names(self):
        self.measure([{"case": "ケース/é"}])
        m = json.loads(self.out.read_bytes().decode("utf-8"))
        self.assertEqual(m["measured_cases"][0]["case"], "ケース/é")

    def test_large_report_of_many_cases_keeps_all(self):
        rep = [{"case": f"c{i}", "trials": [trial(), trial()]} for i in range(500)]
        m = self.measure(rep)
        self.assertEqual(len(m["measured_cases"]), 500)
        self.assertEqual(m["measured_cases"][499]["case"], "c499")


class StartsNoProcess(Base):
    def interpreter_only_path(self):
        """A folder holding only a link to this Python: no gh, no git, no sh."""
        d = self.dir / "pybin"
        d.mkdir()
        py = d / "python3"
        py.symlink_to(sys.executable)
        return d, py

    def test_runs_with_a_path_holding_only_the_interpreter(self):
        d, py = self.interpreter_only_path()
        for tool in ("gh", "git"):
            self.assertFalse((d / tool).exists())
        e = self.env()
        e["PATH"] = str(d)
        rp = self.dir / "report.json"
        rp.write_text(json.dumps([{"case": "c", "trials": [trial(duration="2s")]}]))
        r = subprocess.run([str(py), str(SCRIPT), str(rp), str(self.out)], capture_output=True,
                           text=True, env=e, cwd=self.dir, timeout=60)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(json.loads(self.out.read_text())["measured_cases"][0]["trials"],
                         [{"passed": True, "seconds": 2}])

    def test_recording_gh_and_git_stubs_are_never_called(self):
        d, py = self.interpreter_only_path()
        stubs = self.dir / "stubs"
        stubs.mkdir()
        log = self.dir / "calls.log"
        for tool in ("gh", "git", "sh", "bash", "curl"):
            s = stubs / tool
            s.write_text(f"#!/bin/sh\necho {tool} >> {log}\nexit 0\n")
            s.chmod(0o755)
        e = self.env()
        e["PATH"] = os.pathsep.join([str(stubs), str(d)])
        rp = self.dir / "report.json"
        rp.write_text("[]")
        r = subprocess.run([str(py), str(SCRIPT), str(rp), str(self.out)], capture_output=True,
                           text=True, env=e, cwd=self.dir, timeout=60)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertTrue(self.out.is_file())
        self.assertFalse(log.exists(), "a process was started: " + (log.read_text()
                                                                    if log.exists() else ""))


if __name__ == "__main__":
    unittest.main()
