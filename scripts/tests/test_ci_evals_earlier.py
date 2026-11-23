"""Tests for scripts/ci-evals-earlier.py (feature 008, T012): for each eval matrix key, the
measurement of the newest earlier run in this commit's history.

Every case builds a throwaway git repository (main with three commits, an unmerged branch with
one), a listing file as evals.yml's config job writes it, and a stub `gh` on PATH that serves a
prepared zip per artifact id (or fails) and logs its calls. Stubs for other tools that would
count as starting a process (unzip, tar, curl, wget, jq, python, ...) log into OTHER.
"""
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
import zipfile
from pathlib import Path

import gitenv
from gitenv import clean_env, make_home

HERE = Path(__file__).resolve().parent
SCRIPT = HERE.parent / "ci-evals-earlier.py"
REPO_NAME = "owner/repo"
THIS_RUN = 500

GH_STUB = r'''
echo "$*" >> "$GHLOG"
case "$1 $2" in
  "api repos/owner/repo/actions/artifacts/"*"/zip") ;;
  *) echo "stub gh: unexpected call $*" >&2; exit 3 ;;
esac
id=${2#repos/owner/repo/actions/artifacts/}
id=${id%/zip}
[ -f "$ZIPS/$id.zip" ] || { echo "stub gh: HTTP 404 for $id" >&2; exit 1; }
if [ -f "$ZIPS/$id.fail" ]; then echo "stub gh: download failed" >&2; exit 1; fi
/bin/cat "$ZIPS/$id.zip"
'''
OTHER_STUB = 'echo "$0 $*" >> "$OTHERLOG"\nexit 1\n'
OTHER_TOOLS = ["unzip", "tar", "curl", "wget", "jq", "sh-other", "7z", "zip", "bsdtar", "python",
               "python3-other", "cat-other", "cp", "mkdir", "rm", "mktemp"]


def measurement(key, run_id, commit):
    suite, _, cases = key.partition("-")
    return {"version": 1, "suite": suite, "cases": cases, "key": key, "commit": commit,
            "branch": "main", "run_id": run_id, "run_attempt": 1,
            "measured_cases": [{"case": "c1", "agent": suite, "trials": [
                {"passed": True, "seconds": 12.5, "attempts": 1, "input_tokens": 10,
                 "output_tokens": 5, "cost_usd": 0.01}]}]}


def make_zip(files):
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as z:
        for name, data in files.items():
            z.writestr(name, data)
    return buf.getvalue()


class EarlierTest(unittest.TestCase):
    def setUp(self):
        self.home = make_home(self)
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = Path(tmp.name)
        self.repo = self.tmp / "repo"
        self.repo.mkdir()
        self.zips = self.tmp / "zips"
        self.zips.mkdir()
        self.ghlog = self.tmp / "gh.log"
        self.otherlog = self.tmp / "other.log"
        self.out = self.tmp / "github_output"
        self.listing = self.tmp / "artifacts.jsonl"
        stubs = {"gh": GH_STUB}
        stubs.update({t: OTHER_STUB for t in OTHER_TOOLS})
        self.stubs = gitenv.stub_dir(self, stubs)
        self.git("init", "-q", "-b", "main")
        self.git("config", "user.email", "t@example.com")
        self.git("config", "user.name", "T")
        self.c1 = self.commit("one")
        self.c2 = self.commit("two")
        self.c3 = self.commit("three")
        self.git("checkout", "-q", "-b", "feature")
        self.branch_commit = self.commit("branch only")
        self.git("checkout", "-q", "main")
        self.env = gitenv.with_path(clean_env(self.home), self.stubs)
        self.env.update({"GHLOG": str(self.ghlog), "OTHERLOG": str(self.otherlog),
                         "ZIPS": str(self.zips), "GITHUB_REPOSITORY": REPO_NAME,
                         "RUN_ID": str(THIS_RUN), "SHA": self.c3})
        self.env.pop("GITHUB_OUTPUT", None)
        self.lines = []
        self.next_id = 1000

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.repo, env=clean_env(self.home), check=True,
                              capture_output=True, text=True).stdout.strip()

    def commit(self, msg):
        (self.repo / "f.txt").write_text(msg)
        self.git("add", ".")
        self.git("commit", "-q", "-m", msg)
        return self.git("rev-parse", "HEAD")

    def artifact(self, key, run_id, commit, created="2026-11-20T10:00:00Z", expired=False,
                 zip_files="default", name=None, fail=False, **over):
        """Adds a listing line and the zip it downloads; returns the artifact's id."""
        self.next_id += 1
        aid = self.next_id
        entry = {"id": aid, "name": name or f"eval-measurements-{key}-{run_id}",
                 "expired": expired, "created_at": created, "run_id": run_id, "head_sha": commit}
        entry.update(over)
        self.lines.append(json.dumps({k: v for k, v in entry.items() if v is not None}))
        if zip_files == "default":
            zip_files = {"measurement.json": json.dumps(measurement(key, run_id, commit))}
        if zip_files is not None:
            (self.zips / f"{aid}.zip").write_bytes(make_zip(zip_files))
        if fail:
            (self.zips / f"{aid}.fail").write_text("x")
        return aid

    def write_listing(self, extra_lines=()):
        self.listing.write_text("\n".join(self.lines + list(extra_lines)) + "\n")

    def run_script(self, matrix, args="listing", env=None, write=True):
        if write and args == "listing":
            self.write_listing()
        e = dict(self.env)
        if matrix is not None:
            e["MATRIX"] = matrix if isinstance(matrix, str) else json.dumps(matrix)
        e.update(env or {})
        argv = [sys.executable, str(SCRIPT)]
        if args == "listing":
            argv.append(str(self.listing))
        else:
            argv += args
        return subprocess.run(argv, cwd=self.repo, env=e, capture_output=True, text=True,
                              timeout=60)

    def earlier(self, matrix, **kw):
        """Runs with GITHUB_OUTPUT set; returns the parsed earlier map."""
        self.env["GITHUB_OUTPUT"] = str(self.out)
        r = self.run_script(matrix, **kw)
        self.assertEqual(r.returncode, 0, r.stderr)
        text = self.out.read_text()
        lines = text.splitlines()
        self.assertEqual(len(lines), 1, text)
        self.assertTrue(lines[0].startswith("earlier="), text)
        return json.loads(lines[0][len("earlier="):]), r

    def ghcalls(self):
        return self.ghlog.read_text().splitlines() if self.ghlog.exists() else []

    def assertNone(self, entry, *words):
        self.assertEqual(list(entry), ["none"], entry)
        self.assertIsInstance(entry["none"], str)
        self.assertTrue(entry["none"].strip())
        for w in words:
            self.assertIn(w.lower(), entry["none"].lower())

    def assertEntry(self, entry, run_id, commit, key=None):
        self.assertEqual(set(entry), {"run_id", "commit", "measurement"}, entry)
        self.assertEqual(entry["run_id"], run_id)
        self.assertIsInstance(entry["run_id"], int)
        self.assertEqual(entry["commit"], commit)
        self.assertIsInstance(entry["measurement"], dict)


M_ANALYST = [{"suite": "analyst", "cases": ""}]
M_CODE = [{"suite": "steps", "cases": "code"}]
M_STEPS = [{"suite": "steps", "cases": "code"}, {"suite": "steps", "cases": "tests"}]


class ChoiceTest(EarlierTest):
    def test_commit_itself_is_chosen_and_measurement_copied(self):
        self.artifact("analyst", 100, self.c3)
        m, _ = self.earlier(M_ANALYST)
        self.assertEntry(m["analyst"], 100, self.c3)
        self.assertEqual(m["analyst"]["measurement"], measurement("analyst", 100, self.c3))
        self.assertEqual(list(m), ["analyst"])

    def test_an_ancestor_commit_is_chosen(self):
        self.artifact("analyst", 100, self.c1)
        m, _ = self.earlier(M_ANALYST)
        self.assertEntry(m["analyst"], 100, self.c1)

    def test_newest_by_created_at_among_ancestors(self):
        self.artifact("analyst", 101, self.c1, created="2026-11-18T10:00:00Z")
        self.artifact("analyst", 103, self.c2, created="2026-11-21T10:00:00Z")
        self.artifact("analyst", 102, self.c3, created="2026-11-19T10:00:00Z")
        m, _ = self.earlier(M_ANALYST)
        self.assertEntry(m["analyst"], 103, self.c2)

    def test_newest_is_by_created_at_not_by_listing_order_or_id(self):
        self.artifact("analyst", 110, self.c3, created="2026-11-22T10:00:00Z")
        self.artifact("analyst", 111, self.c3, created="2026-11-22T09:59:59Z")
        self.artifact("analyst", 109, self.c3, created="2026-11-22T10:00:01Z")
        m, _ = self.earlier(M_ANALYST)
        self.assertEntry(m["analyst"], 109, self.c3)

    def test_one_entry_per_matrix_key_each_with_its_own_artifact(self):
        self.artifact("steps-code", 120, self.c2)
        self.artifact("steps-tests", 121, self.c1)
        m, _ = self.earlier(M_STEPS)
        self.assertEqual(set(m), {"steps-code", "steps-tests"})
        self.assertEntry(m["steps-code"], 120, self.c2)
        self.assertEntry(m["steps-tests"], 121, self.c1)
        self.assertEqual(m["steps-code"]["measurement"]["key"], "steps-code")
        self.assertEqual(m["steps-tests"]["measurement"]["key"], "steps-tests")

    def test_key_is_suite_alone_when_cases_empty_and_suite_dash_cases_otherwise(self):
        self.artifact("analyst", 130, self.c1)
        self.artifact("steps-code", 131, self.c1)
        m, _ = self.earlier([{"suite": "analyst", "cases": ""}, {"suite": "steps", "cases": "code"}])
        self.assertEqual(set(m), {"analyst", "steps-code"})
        self.assertEntry(m["analyst"], 130, self.c1)
        self.assertEntry(m["steps-code"], 131, self.c1)

    def test_no_artifact_at_all_gives_none_for_every_key(self):
        m, _ = self.earlier(M_STEPS)
        self.assertEqual(set(m), {"steps-code", "steps-tests"})
        for e in m.values():
            self.assertNone(e)
        self.assertEqual(self.ghcalls(), [])


class LeftOutTest(EarlierTest):
    def only_none_with_older_good(self, left_out_id_kwargs):
        """The left-out artifact is newer than a good one; the good one must be chosen."""
        self.artifact("analyst", 90, self.c1, created="2026-11-01T10:00:00Z")
        self.artifact("analyst", **left_out_id_kwargs)
        m, _ = self.earlier(M_ANALYST)
        self.assertEntry(m["analyst"], 90, self.c1)

    def test_this_run_is_left_out(self):
        self.only_none_with_older_good({"run_id": THIS_RUN, "commit": self.c3,
                                        "created": "2026-11-22T10:00:00Z"})

    def test_this_run_alone_gives_none(self):
        self.artifact("analyst", THIS_RUN, self.c3)
        m, _ = self.earlier(M_ANALYST)
        self.assertNone(m["analyst"])
        self.assertEqual(self.ghcalls(), [])

    def test_expired_is_left_out(self):
        self.only_none_with_older_good({"run_id": 91, "commit": self.c3, "expired": True,
                                        "created": "2026-11-22T10:00:00Z"})

    def test_commit_not_here_is_left_out(self):
        self.only_none_with_older_good({"run_id": 92, "commit": "f" * 40,
                                        "created": "2026-11-22T10:00:00Z"})

    def test_unmerged_branch_commit_is_left_out(self):
        self.only_none_with_older_good({"run_id": 93, "commit": self.branch_commit,
                                        "created": "2026-11-22T10:00:00Z"})

    def test_a_descendant_of_sha_is_left_out(self):
        self.artifact("analyst", 94, self.c3, created="2026-11-01T10:00:00Z")
        self.artifact("analyst", 95, self.c3, created="2026-11-22T10:00:00Z")
        m, _ = self.earlier(M_ANALYST, env={"SHA": self.c2})
        self.assertNone(m["analyst"])  # c3 is not an ancestor of c2
        self.assertEqual(self.ghcalls(), [])

    def test_another_key_is_left_out_steps_code_against_steps_tests(self):
        self.artifact("steps-tests", 96, self.c1)
        m, _ = self.earlier(M_CODE)
        self.assertNone(m["steps-code"])
        self.assertEqual(self.ghcalls(), [])

    def test_key_that_is_a_prefix_of_another_is_left_out_analyst_against_analyst_x(self):
        self.artifact("analyst-x", 97, self.c1)
        m, _ = self.earlier(M_ANALYST)
        self.assertNone(m["analyst"])
        m2, _ = self.earlier([{"suite": "analyst", "cases": "x"}])
        self.assertEntry(m2["analyst-x"], 97, self.c1)

    def test_name_without_digits_suffix_or_prefix_is_left_out(self):
        self.artifact("analyst", 98, self.c1, name="eval-measurements-analyst-abc")
        self.artifact("analyst", 99, self.c1, name="eval-measurements-analyst-")
        self.artifact("analyst", 88, self.c1, name="eval-measurements-analyst")
        self.artifact("analyst", 89, self.c1, name="xeval-measurements-analyst-7")
        self.artifact("analyst", 87, self.c1, name="other-analyst-7")
        m, _ = self.earlier(M_ANALYST)
        self.assertNone(m["analyst"])
        self.assertEqual(self.ghcalls(), [])

    def test_without_run_id_or_head_sha_is_left_out(self):
        self.artifact("analyst", 90, self.c1, created="2026-11-01T10:00:00Z")
        self.artifact("analyst", None, self.c3, created="2026-11-22T10:00:00Z")
        self.artifact("analyst", 91, None, created="2026-11-22T11:00:00Z")
        m, _ = self.earlier(M_ANALYST)
        self.assertEntry(m["analyst"], 90, self.c1)

    def test_lines_that_are_not_json_objects_are_left_out(self):
        self.artifact("analyst", 90, self.c1)
        self.write_listing(["not json at all", "[1, 2]", "42", '"str"', "null", "{broken", "",
                            "{}"])
        m, r = self.earlier(M_ANALYST, write=False)
        self.assertEntry(m["analyst"], 90, self.c1)

    def test_listing_of_two_pages_lines_is_read_whole(self):
        for i in range(150):
            self.artifact("steps-tests", 200 + i, self.c1, created="2026-10-01T10:00:00Z")
        self.artifact("analyst", 90, self.c2)
        m, _ = self.earlier(M_ANALYST)
        self.assertEntry(m["analyst"], 90, self.c2)
        self.assertEqual(len(self.ghcalls()), 1)

    def test_a_listing_without_trailing_newline_is_read(self):
        self.artifact("analyst", 90, self.c1)
        self.listing.write_text(self.lines[0])
        m, _ = self.earlier(M_ANALYST, write=False)
        self.assertEntry(m["analyst"], 90, self.c1)


class DownloadFailureTest(EarlierTest):
    def test_failed_download_gives_none_naming_it(self):
        self.artifact("analyst", 90, self.c1, zip_files=None)  # stub gh exits 1
        m, r = self.earlier(M_ANALYST)
        self.assertNone(m["analyst"], "download")
        self.assertNotEqual(r.stderr.strip(), "")

    def test_failed_download_does_not_fall_back_to_an_older_artifact(self):
        self.artifact("analyst", 80, self.c1, created="2026-11-01T10:00:00Z")
        self.artifact("analyst", 90, self.c2, created="2026-11-20T10:00:00Z", zip_files=None)
        m, _ = self.earlier(M_ANALYST)
        self.assertNone(m["analyst"], "download")
        self.assertEqual(len(self.ghcalls()), 1)

    def test_one_failure_leaves_the_other_key_intact(self):
        self.artifact("steps-code", 90, self.c1, zip_files=None)
        self.artifact("steps-tests", 91, self.c1)
        m, _ = self.earlier(M_STEPS)
        self.assertNone(m["steps-code"])
        self.assertEntry(m["steps-tests"], 91, self.c1)

    def test_zip_without_measurement_json_gives_none_naming_it(self):
        self.artifact("analyst", 90, self.c1, zip_files={"other.json": "{}"})
        m, _ = self.earlier(M_ANALYST)
        self.assertNone(m["analyst"], "measurement.json")

    def test_empty_zip_gives_none(self):
        self.artifact("analyst", 90, self.c1, zip_files={})
        m, _ = self.earlier(M_ANALYST)
        self.assertNone(m["analyst"])

    def test_bytes_that_are_not_a_zip_give_none(self):
        aid = self.artifact("analyst", 90, self.c1, zip_files=None)
        (self.zips / f"{aid}.zip").write_bytes(b"this is not a zip")
        m, _ = self.earlier(M_ANALYST)
        self.assertNone(m["analyst"])

    def test_measurement_that_is_not_json_gives_none_naming_json(self):
        self.artifact("analyst", 90, self.c1, zip_files={"measurement.json": "{not json"})
        m, _ = self.earlier(M_ANALYST)
        self.assertNone(m["analyst"], "json")

    def test_measurement_that_is_not_utf8_gives_none(self):
        self.artifact("analyst", 90, self.c1, zip_files={"measurement.json": b"\xff\xfe\x00{"})
        m, _ = self.earlier(M_ANALYST)
        self.assertNone(m["analyst"])

    def test_measurement_that_is_json_but_not_an_object_gives_none(self):
        self.artifact("analyst", 90, self.c1, zip_files={"measurement.json": "[1, 2]"})
        m, _ = self.earlier(M_ANALYST)
        self.assertNone(m["analyst"])

    def test_measurement_json_inside_a_folder_is_found_or_none_never_a_crash(self):
        self.artifact("analyst", 90, self.c1, zip_files={"sub/measurement.json": "{}"})
        m, _ = self.earlier(M_ANALYST)
        self.assertIn("analyst", m)  # either reading is acceptable; the contract names the file


class EnvironmentAndOutputTest(EarlierTest):
    def test_matrix_unset_gives_empty_map(self):
        self.artifact("analyst", 90, self.c1)
        m, _ = self.earlier(None)
        self.assertEqual(m, {})
        self.assertEqual(self.ghcalls(), [])

    def test_matrix_not_json_gives_empty_map_with_reason_on_stderr(self):
        m, r = self.earlier("{not json")
        self.assertEqual(m, {})
        self.assertNotEqual(r.stderr.strip(), "")

    def test_matrix_empty_list_gives_empty_map(self):
        m, _ = self.earlier([])
        self.assertEqual(m, {})

    def test_matrix_json_of_the_wrong_shape_exits_0_and_writes_one_line(self):
        for bad in ('{"suite": "analyst"}', '"x"', "42", "null", '[1, "a", {"suite": 3}]'):
            self.out.write_text("")
            self.env["GITHUB_OUTPUT"] = str(self.out)
            r = self.run_script(bad)
            self.assertEqual(r.returncode, 0, (bad, r.stderr))
            lines = self.out.read_text().splitlines()
            self.assertEqual(len(lines), 1, bad)
            self.assertIsInstance(json.loads(lines[0][len("earlier="):]), dict)

    def test_missing_listing_file_gives_none_naming_it(self):
        self.env["GITHUB_OUTPUT"] = str(self.out)
        r = self.run_script(M_STEPS, write=False)
        self.assertEqual(r.returncode, 0, r.stderr)
        m = json.loads(self.out.read_text().strip()[len("earlier="):])
        self.assertEqual(set(m), {"steps-code", "steps-tests"})
        for e in m.values():
            self.assertNone(e, "artifacts")
        self.assertEqual(self.ghcalls(), [])

    def test_empty_listing_file_gives_none(self):
        self.listing.write_text("")
        m, _ = self.earlier(M_ANALYST, write=False)
        self.assertNone(m["analyst"])

    def test_listing_that_is_a_directory_exits_0(self):
        d = self.tmp / "adir"
        d.mkdir()
        self.env["GITHUB_OUTPUT"] = str(self.out)
        r = self.run_script(M_ANALYST, args=[str(d)])
        self.assertEqual(r.returncode, 0, r.stderr)
        m = json.loads(self.out.read_text().strip()[len("earlier="):])
        self.assertNone(m["analyst"])

    def test_output_is_appended_not_overwritten(self):
        self.out.write_text("before=1\n")
        self.artifact("analyst", 90, self.c1)
        self.env["GITHUB_OUTPUT"] = str(self.out)
        r = self.run_script(M_ANALYST)
        self.assertEqual(r.returncode, 0, r.stderr)
        lines = self.out.read_text().splitlines()
        self.assertEqual(lines[0], "before=1")
        self.assertEqual(len(lines), 2)
        self.assertTrue(lines[1].startswith("earlier="))
        self.assertEqual(r.stdout, "")

    def test_output_line_is_one_line_even_with_newlines_in_the_data(self):
        m = measurement("analyst", 90, self.c1)
        m["branch"] = "a\nb c"
        self.artifact("analyst", 90, self.c1, zip_files={"measurement.json": json.dumps(m)})
        got, _ = self.earlier(M_ANALYST)  # earlier() asserts exactly one line
        self.assertEqual(got["analyst"]["measurement"]["branch"], "a\nb c")

    def test_printed_when_github_output_is_unset(self):
        self.artifact("analyst", 90, self.c1)
        r = self.run_script(M_ANALYST)
        self.assertEqual(r.returncode, 0, r.stderr)
        lines = r.stdout.splitlines()
        self.assertEqual(len(lines), 1, r.stdout)
        self.assertTrue(lines[0].startswith("earlier="))
        self.assertEntry(json.loads(lines[0][len("earlier="):])["analyst"], 90, self.c1)

    def test_printed_empty_map_when_unset_and_matrix_unset(self):
        r = self.run_script(None)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout.strip(), "earlier={}")

    def test_empty_github_output_value_is_treated_as_unset_or_unwritable_never_a_crash(self):
        r = self.run_script(M_ANALYST, env={"GITHUB_OUTPUT": str(self.tmp / "nodir" / "out")})
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotEqual(r.stderr.strip(), "")

    def test_sha_unset_or_unknown_gives_none_and_exit_0(self):
        self.artifact("analyst", 90, self.c1)
        for sha in ("", "f" * 40, "not-a-sha"):
            m, _ = self.earlier(M_ANALYST, env={"SHA": sha})
            self.out.unlink()
            self.assertNone(m["analyst"])

    def test_run_id_unset_still_works(self):
        self.artifact("analyst", 90, self.c1)
        self.env.pop("RUN_ID")
        m, _ = self.earlier(M_ANALYST)
        self.assertEntry(m["analyst"], 90, self.c1)

    def test_run_outside_a_git_repository_gives_none_and_exit_0(self):
        self.artifact("analyst", 90, self.c1)
        self.write_listing()
        e = dict(self.env)
        e.update({"MATRIX": json.dumps(M_ANALYST), "GIT_CEILING_DIRECTORIES": str(self.tmp)})
        r = subprocess.run([sys.executable, str(SCRIPT), str(self.listing)], cwd=self.zips, env=e,
                           capture_output=True, text=True, timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr)
        m = json.loads(r.stdout.strip()[len("earlier="):])
        self.assertNone(m["analyst"])


class ProcessesTest(EarlierTest):
    def test_gh_is_called_only_for_the_chosen_artifacts(self):
        self.artifact("steps-code", 80, self.c1, created="2026-11-01T10:00:00Z")
        chosen = self.artifact("steps-code", 81, self.c2, created="2026-11-20T10:00:00Z")
        self.artifact("steps-code", THIS_RUN, self.c3)
        self.artifact("steps-code", 82, self.c1, expired=True)
        chosen2 = self.artifact("steps-tests", 83, self.c1)
        self.artifact("steps-tests", 84, self.branch_commit)
        self.artifact("analyst", 85, self.c1)
        self.earlier(M_STEPS)
        calls = sorted(self.ghcalls())
        self.assertEqual(calls, sorted(
            f"api repos/{REPO_NAME}/actions/artifacts/{i}/zip" for i in (chosen, chosen2)))

    def test_each_call_is_gh_api_on_the_repository_of_github_repository(self):
        self.artifact("analyst", 90, self.c1)
        self.earlier(M_ANALYST, env={"GITHUB_REPOSITORY": REPO_NAME})
        for c in self.ghcalls():
            self.assertRegex(c, r"^api repos/owner/repo/actions/artifacts/\d+/zip$")

    def test_no_process_but_git_and_gh(self):
        self.artifact("steps-code", 90, self.c1)
        self.artifact("steps-tests", 91, self.c2, zip_files={"x": "y"})
        self.artifact("analyst", 92, self.c2, zip_files=None)
        self.earlier(M_STEPS + M_ANALYST)
        self.assertFalse(self.otherlog.exists(), self.otherlog.read_text()
                         if self.otherlog.exists() else "")

    def test_runs_on_a_path_holding_only_git_and_gh(self):
        git = subprocess.run(["which", "git"], capture_output=True, text=True).stdout.strip()
        d = self.tmp / "bin"
        d.mkdir()
        os.symlink(git, d / "git")
        os.symlink(Path(self.stubs) / "gh", d / "gh")
        self.artifact("analyst", 90, self.c1)
        self.write_listing()
        e = dict(self.env)
        e.update({"PATH": str(d), "MATRIX": json.dumps(M_ANALYST)})
        r = subprocess.run([sys.executable, str(SCRIPT), str(self.listing)], cwd=self.repo,
                           env=e, capture_output=True, text=True, timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr)
        got = json.loads(r.stdout.strip()[len("earlier="):])
        # the stub gh needs /bin/sh only; with a PATH of git and gh the entry must be read
        self.assertEntry(got["analyst"], 90, self.c1)


class ArgumentsTest(EarlierTest):
    def check_usage(self, args):
        self.env["GITHUB_OUTPUT"] = str(self.out)
        r = self.run_script(M_ANALYST, args=args)
        self.assertEqual(r.returncode, 2, (args, r.stderr))
        self.assertFalse(self.out.exists() and self.out.read_text())
        self.assertEqual(r.stdout, "")
        self.assertNotEqual(r.stderr.strip(), "")
        self.assertEqual(self.ghcalls(), [])

    def test_no_argument_exits_2_and_writes_nothing(self):
        self.check_usage([])

    def test_two_arguments_exit_2_and_write_nothing(self):
        self.write_listing()
        self.check_usage([str(self.listing), str(self.listing)])

    def test_one_empty_argument_is_a_missing_file_not_a_usage_error(self):
        self.env["GITHUB_OUTPUT"] = str(self.out)
        r = self.run_script(M_ANALYST, args=[""])
        self.assertEqual(r.returncode, 0, r.stderr)
        m = json.loads(self.out.read_text().strip()[len("earlier="):])
        self.assertNone(m["analyst"])


if __name__ == "__main__":
    unittest.main()
