"""Contracts of the CI helper scripts (T107, T109; research D41, ADR 0044), written before them.

scripts/ci-step.sh, scripts/ci-summary.py, scripts/ci-gvisor.sh, scripts/ci-evals-summary.py and
the `--prepare` mode of scripts/check.sh. Where a real tool would be heavy or need root (docker,
sudo, curl, tar, systemctl, df) a stub on PATH records its calls in a log, so a test reads what the
script did, in order, and the install of a refused download is proved by an empty log.

Assumptions the scripts' author must meet or raise (each is a reading of T107/T109):
- ci-gvisor.sh reads `gvisor-release` and `gvisor-sha512` from src/deploy/images.lock found
  relative to the script (../src/deploy/images.lock), like scripts/check.sh does.
- It downloads the release with `curl` into a file (-o/--output), checks it with sha512, unpacks
  it with `tar` and runs the unpacked `runsc` (with `sudo`) in a folder of its own.
- A trial of an evals report may carry `"usage": {"input_tokens", "output_tokens", "cost_usd"}`.
"""
import hashlib
import json
import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

import gitenv

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
SCRIPTS = REPO / "scripts"
STEP = SCRIPTS / "ci-step.sh"
SUMMARY = SCRIPTS / "ci-summary.py"
GVISOR = SCRIPTS / "ci-gvisor.sh"
EVALS = SCRIPTS / "ci-evals-summary.py"
CHECK = SCRIPTS / "check.sh"


def tmpdir(tc):
    t = tempfile.TemporaryDirectory()
    tc.addCleanup(t.cleanup)
    return Path(t.name)


def run(args, env=None, cwd=None, timeout=60):
    return subprocess.run([str(a) for a in args], capture_output=True, text=True, env=env,
                          cwd=cwd, timeout=timeout)


def base_env(tc, extra=None, drop=("GITHUB_STEP_SUMMARY", "GITHUB_OUTPUT", "NEEDS")):
    env = gitenv.clean_env(gitenv.make_home(tc))
    for k in drop:
        env.pop(k, None)
    env.update(extra or {})
    return env


# ------------------------------------------------------------------ ci-step.sh

NOISE = "i=1; while [ $i -le 100 ]; do echo noise-$i; i=$((i+1)); done"
TABLE = ("echo '== time per step (setup / test / teardown, seconds):'; "
         "echo '  go             12.5  (1.0 / 10.5 / 1.0)'; echo '  all            12.5'")


class CiStepTest(unittest.TestCase):
    def setUp(self):
        self.t = tmpdir(self)
        self.summary = self.t / "summary.md"
        self.output = self.t / "output.txt"
        self.stubs = gitenv.stub_dir(self, {"df": 'echo "DF-STUB $*"\necho "Filesystem Size Avail DF-STUB-ROW"\n'})
        self.env = gitenv.with_path(base_env(self, {"GITHUB_STEP_SUMMARY": str(self.summary),
                                                    "GITHUB_OUTPUT": str(self.output)}), self.stubs)

    def step(self, name, *cmd, env=None):
        return run([STEP, name, *cmd], env=env or self.env, cwd=self.t)

    def text(self):
        return self.summary.read_text()

    def test_prints_the_commands_output(self):
        r = self.step("demo", "sh", "-c", "echo hello-out; echo hello-err >&2")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("hello-out", r.stdout + r.stderr)
        self.assertIn("hello-err", r.stdout + r.stderr)

    def test_exits_with_the_commands_code(self):
        for code in (0, 1, 7):
            with self.subTest(code):
                self.assertEqual(self.step("demo", "sh", "-c", f"exit {code}").returncode, code)

    def test_arguments_keep_their_spaces(self):
        r = self.step("demo", "sh", "-c", 'echo "[$1]"', "x", "a  b")
        self.assertIn("[a  b]", r.stdout + r.stderr)

    def test_summary_of_a_passing_step(self):
        self.step("my-step-name", "sh", "-c", "echo ok")
        s = self.text()
        self.assertIn("my-step-name", s)
        self.assertRegex(s, r"✅ my-step-name — \d+ s")
        self.assertNotRegex(s, r"(?i)fail")
        self.assertRegex(s, r"(?i)time|seconds|\d+\s*s\b")

    def test_summary_of_a_failing_step_names_the_exit_code(self):
        self.step("my-step-name", "sh", "-c", "exit 7")
        s = self.text()
        self.assertRegex(s, r"❌ my-step-name — \d+ s, exit 7")
        self.assertNotRegex(s, r"(?i)\bpassed\b")
        self.assertRegex(s, r"\b7\b")

    def test_summary_holds_no_timing_table_even_when_the_command_prints_one(self):
        self.step("demo", "sh", "-c", NOISE + "; " + TABLE)
        s = self.text()
        self.assertNotIn("== time per step", s)
        self.assertNotIn("12.5  (1.0", s)

    def test_summary_holds_the_last_40_lines_on_failure(self):
        self.step("demo", "sh", "-c", NOISE + "; exit 1")
        s = self.text()
        self.assertRegex(s, r"noise-100\b")
        self.assertRegex(s, r"noise-61\b")
        self.assertNotRegex(s, r"noise-60\b")
        self.assertNotRegex(s, r"noise-1\b")

    RESULTS = ["PASS  scenario-one  all 3 trials", "FAIL  scenario-two  stopped in 2 out of 3 trials",
               "  detail of the failure", "planted: 1 of 2"]

    def results_cmd(self, before, after, lines=None):
        """A script file (so the command text in the summary does not hold the lines) and its
        command: noise, the result lines, noise."""
        body = "".join(f"echo '{l}'\n" for l in (lines or self.RESULTS))
        loop = lambda tag, n: f"i=1; while [ $i -le {n} ]; do echo {tag}-$i; i=$((i+1)); done\n"
        f = self.t / "results.sh"
        f.write_text(loop("pre", before) + body + loop("post", after))
        return f"sh {f}"

    def test_ap_test_result_lines_are_kept_when_more_than_40_lines_follow(self):
        self.step("demo", "sh", "-c", self.results_cmd(5, 80))
        s = self.text()
        for l in ("PASS  scenario-one", "FAIL  scenario-two", "planted: 1 of 2"):
            self.assertIn(l, s)

    def test_ap_test_result_lines_are_kept_when_more_than_40_lines_precede(self):
        self.step("demo", "sh", "-c", self.results_cmd(80, 3))
        s = self.text()
        for l in ("PASS  scenario-one", "FAIL  scenario-two", "planted: 1 of 2"):
            self.assertIn(l, s)

    def test_ap_test_result_lines_are_kept_in_the_middle_of_noise(self):
        self.step("demo", "sh", "-c", self.results_cmd(80, 80))
        s = self.text()
        for l in ("PASS  scenario-one", "FAIL  scenario-two", "planted: 1 of 2"):
            self.assertIn(l, s)
        self.assertNotRegex(s, r"pre-40\b")      # noise is still cut, not everything is kept

    def test_us2_total_line_is_kept(self):
        self.step("demo", "sh", "-c", self.results_cmd(
            60, 60, ["PASS  alpha  ok", "PASS  beta  ok", "us2: 2 of 2"]))
        s = self.text()
        for l in ("PASS  alpha", "PASS  beta", "us2: 2 of 2"):
            self.assertIn(l, s)

    def test_every_result_line_of_many_scenarios_is_kept(self):
        lines = [f"PASS  scenario-{i}  ok" for i in range(30)] + ["planted: 30 of 30"]
        self.step("demo", "sh", "-c", self.results_cmd(60, 60, lines))
        s = self.text()
        for l in lines:
            self.assertRegex(s, re.escape(l) + r"\s*$|" + re.escape(l) + r"\b", l)

    def test_result_lines_are_kept_alongside_the_timing_table(self):
        self.step("demo", "sh", "-c", self.results_cmd(5, 0) + "; " + TABLE)
        s = self.text()
        self.assertIn("PASS  scenario-one", s)

    def test_a_single_space_or_other_word_is_not_a_result_line(self):
        script = self.t / "noisy.sh"
        script.write_text("i=1; while [ $i -le 80 ]; do echo pre-$i; i=$((i+1)); done\n"
                          "echo 'PASS scenario-x one space'\necho 'PASSED  scenario-y'\n"
                          "echo 'xFAIL  scenario-z'\n"
                          "i=1; while [ $i -le 80 ]; do echo post-$i; i=$((i+1)); done\n")
        self.step("demo", "sh", str(script))
        s = self.text()
        for l in ("scenario-x", "scenario-y", "scenario-z"):
            self.assertNotIn(l, s)

    def test_the_summary_is_appended_not_replaced(self):
        self.summary.write_text("EARLIER-JOB\n")
        self.step("one", "true")
        self.step("two", "true")
        s = self.text()
        self.assertTrue(s.startswith("EARLIER-JOB"))
        self.assertIn("one", s)
        self.assertIn("two", s)

    def test_output_gets_seconds(self):
        self.output.write_text("earlier=1\n")
        self.step("demo", "sleep", "2")
        lines = self.output.read_text().splitlines()
        self.assertEqual(lines[0], "earlier=1")
        sec = [l for l in lines if l.startswith("seconds=")]
        self.assertEqual(len(sec), 1, lines)
        self.assertRegex(sec[0], r"^seconds=\d+(\.\d+)?$")
        self.assertTrue(2 <= float(sec[0].split("=")[1]) < 12, sec)

    def test_seconds_and_summary_are_written_for_a_failing_command_too(self):
        r = self.step("demo", "sh", "-c", "exit 1")
        self.assertEqual(r.returncode, 1)
        self.assertIn("seconds=", self.output.read_text())
        self.assertIn("❌", self.text())
        self.assertIn("exit 1", self.text())

    def test_without_the_variables_it_only_runs_the_command(self):
        env = gitenv.with_path(base_env(self), self.stubs)
        before = set(os.listdir(self.t))
        r = run([STEP, "demo", "sh", "-c", "echo only-run; exit 7"], env=env, cwd=self.t)
        self.assertEqual(r.returncode, 7, r.stderr)
        self.assertIn("only-run", r.stdout + r.stderr)
        self.assertNotIn("DF-STUB", r.stdout + r.stderr)
        self.assertEqual(set(os.listdir(self.t)), before)

    def test_a_missing_command_fails_with_a_code(self):
        r = self.step("demo", "no-such-command-zz")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("❌", self.text())


# ------------------------------------------------------------------ ci-summary.py

class CiSummaryTest(unittest.TestCase):
    def setUp(self):
        self.t = tmpdir(self)
        self.file = self.t / "summary.md"

    def go(self, needs, to_file=True):
        extra = {"NEEDS": json.dumps(needs)}
        if to_file:
            extra["GITHUB_STEP_SUMMARY"] = str(self.file)
        r = run(["python3", SUMMARY], env=base_env(self, extra), cwd=self.t)
        out = self.file.read_text() if to_file and self.file.exists() else r.stdout
        return r, out

    @staticmethod
    def need(result, seconds=None):
        d = {"result": result, "outputs": {}}
        if seconds is not None:
            d["outputs"]["seconds"] = seconds
        return d

    def row(self, text, name):
        rows = [l for l in text.splitlines() if name in l]
        self.assertEqual(len(rows), 1, f"{name}: {rows}\n{text}")
        return rows[0]

    def test_all_success_exits_0_with_one_row_per_job(self):
        r, out = self.go({"alpha-job": self.need("success", "12"),
                          "beta-job": self.need("success", "345")})
        self.assertEqual(r.returncode, 0, r.stderr)
        a, b = self.row(out, "alpha-job"), self.row(out, "beta-job")
        self.assertIn("✅", a)
        self.assertIn("12", a)
        self.assertIn("345", b)
        self.assertIn("|", a)       # a Markdown table

    def test_a_non_success_result_exits_1_and_shows_the_result(self):
        for res in ("failure", "cancelled", "skipped"):
            with self.subTest(res):
                r, out = self.go({"ok-job": self.need("success", "1"), "bad-job": self.need(res, "2")})
                self.assertEqual(r.returncode, 1)
                self.assertIn("❌", self.row(out, "bad-job"))
                self.assertIn("✅", self.row(out, "ok-job"))
                self.file.unlink()

    def test_missing_or_non_numeric_seconds_show_a_dash(self):
        r, out = self.go({"none-job": self.need("success"),
                          "text-job": self.need("success", "soon"),
                          "empty-job": self.need("success", ""),
                          "num-job": self.need("success", "9")})
        self.assertEqual(r.returncode, 0)
        for n in ("none-job", "text-job", "empty-job"):
            self.assertIn("—", self.row(out, n))
        self.assertNotIn("—", self.row(out, "num-job"))

    def test_job_without_outputs_key_is_a_row_with_a_dash(self):
        r, out = self.go({"bare-job": {"result": "failure"}})
        self.assertEqual(r.returncode, 1)
        self.assertIn("—", self.row(out, "bare-job"))

    def test_writes_to_stdout_without_the_summary_file(self):
        r, out = self.go({"alpha-job": self.need("success", "5")}, to_file=False)
        self.assertEqual(r.returncode, 0)
        self.assertIn("alpha-job", r.stdout)

    def test_appends_to_the_summary_file(self):
        self.file.write_text("EARLIER\n")
        self.go({"alpha-job": self.need("success", "5")})
        self.assertTrue(self.file.read_text().startswith("EARLIER"))

    def test_the_failure_of_one_job_is_not_hidden_by_many_successes(self):
        needs = {f"j{i}-job": self.need("success", str(i)) for i in range(10)}
        needs["z-job"] = self.need("failure", "1")
        self.assertEqual(self.go(needs)[0].returncode, 1)


# ------------------------------------------------------------------ ci-gvisor.sh

DOCKER_STUB = r'''
echo "docker $*" >> "$LOG"
all="$*"
case "$1" in
info)
  restarted=no; [ -f "$STATE/restarted" ] && restarted=yes
  rt=runc; [ "$restarted" = yes ] && [ "${STUB_RUNSC:-yes}" = yes ] && rt="runc runsc"
  case "$all" in
    *Runtimes*) if [ "$rt" = runc ]; then echo '{"runc":{}}'; else echo '{"runc":{},"runsc":{}}'; fi ;;
    *CgroupVersion*CgroupDriver*) echo "2 ${STUB_CGROUP:-systemd}" ;;
    *CgroupDriver*) echo "${STUB_CGROUP:-systemd}" ;;
    *) echo "Server:"; echo " Runtimes: $rt"; echo " Cgroup Driver: ${STUB_CGROUP:-systemd}" ;;
  esac ;;
run)
  case "$all" in
    *--runtime=runsc*|*"--runtime runsc"*) ;;
    *) echo "stub: not the runsc runtime" >&2; exit 1 ;;
  esac
  case "$all" in
    *--memory*) [ "${STUB_SMOKE_FAIL:-}" = limits ] && exit 125 ;;
    *) [ "${STUB_SMOKE_FAIL:-}" = plain ] && exit 125 ;;
  esac ;;
esac
exit 0
'''
CURL_STUB = r'''
echo "curl $*" >> "$LOG"
out=; prev=
for a in "$@"; do
  case "$prev" in -o|--output) out=$a ;; esac
  case "$prev" in -*o) case "$prev" in --*) ;; *) out=$a ;; esac ;; esac
  prev=$a
done
[ -n "$out" ] || { echo "stub curl: no -o file" >&2; exit 2; }
cp "$PAYLOAD" "$out"
'''
TAR_STUB = r'''
echo "tar $*" >> "$LOG"
dir=.; prev=
for a in "$@"; do
  case "$prev" in -C|--directory) dir=$a ;; esac
  prev=$a
done
mkdir -p "$dir/gvisor-bin" || exit 1
cat > "$dir/runsc" <<'EOS'
#!/bin/sh
echo "runsc $*" >> "$LOG"
exit 0
EOS
chmod +x "$dir/runsc"
echo shim > "$dir/gvisor-bin/containerd-shim-runsc-v1"
exit 0
'''
SUDO_STUB = r'''
echo "sudo $*" >> "$LOG"
while [ $# -gt 0 ]; do case "$1" in -*) shift ;; *) break ;; esac; done
[ $# -gt 0 ] || exit 0
p=$(command -v "$1" 2>/dev/null || true)
case "$p" in "$ROOT"/*|"$STUBDIR"/*) exec "$@" ;; esac
exit 0
'''
SYSTEMCTL_STUB = r'''
echo "systemctl $*" >> "$LOG"
case "$*" in *restart*docker*) touch "$STATE/restarted" ;; esac
exit 0
'''
PAYLOAD = b"fake gvisor release payload\n"
RELEASE = "20261109.0"
SHA = hashlib.sha512(PAYLOAD).hexdigest()


class CiGvisorTest(unittest.TestCase):
    def setUp(self):
        self.root = tmpdir(self)
        self.repo = self.root / "repo"
        (self.repo / "scripts").mkdir(parents=True)
        (self.repo / "src" / "deploy").mkdir(parents=True)
        shutil.copy(GVISOR, self.repo / "scripts" / "ci-gvisor.sh") if GVISOR.exists() else None
        self.log = self.root / "log"
        self.log.write_text("")
        self.state = self.root / "state"
        self.state.mkdir()
        (self.root / "tmp").mkdir()
        (self.root / "payload").write_bytes(PAYLOAD)
        self.stubs = gitenv.stub_dir(self, {"docker": DOCKER_STUB, "curl": CURL_STUB, "tar": TAR_STUB,
                                            "sudo": SUDO_STUB, "systemctl": SYSTEMCTL_STUB})
        self.lock()

    def lock(self, release=RELEASE, sha=SHA, alpine="alpine:3.22@sha256:" + "a" * 64):
        lines = ["# test lock", "ci-runner ubuntu-26.04"]
        if alpine is not None:
            lines.append(f"alpine {alpine}")
        if release is not None:
            lines.append(f"gvisor-release {release}")
        if sha is not None:
            lines.append(f"gvisor-sha512 {sha}")
        (self.repo / "src" / "deploy" / "images.lock").write_text("\n".join(lines) + "\n")

    def go(self, **extra):
        env = base_env(self, {"LOG": str(self.log), "STATE": str(self.state), "ROOT": str(self.root), "STUBDIR": self.stubs,
                              "PAYLOAD": str(self.root / "payload"), "TMPDIR": str(self.root / "tmp"),
                              "RUNNER_TEMP": str(self.root / "tmp"), **extra})
        return run([self.repo / "scripts" / "ci-gvisor.sh"], env=gitenv.with_path(env, self.stubs),
                   cwd=self.repo, timeout=60)

    def calls(self):
        return self.log.read_text().splitlines()

    def index(self, pattern, after=-1):
        for i, l in enumerate(self.calls()):
            if i > after and re.search(pattern, l):
                return i
        self.fail(f"no call matching {pattern!r} after {after} in:\n" + "\n".join(self.calls()))

    def none(self, *patterns):
        for p in patterns:
            self.assertFalse([l for l in self.calls() if re.search(p, l)], f"{p}: {self.calls()}")

    # -- refusals

    def test_script_exists_and_is_executable(self):
        self.assertTrue(GVISOR.exists(), "scripts/ci-gvisor.sh does not exist")
        self.assertTrue(os.access(GVISOR, os.X_OK))

    def test_missing_release_pin_is_refused_before_anything(self):
        self.lock(release=None)
        r = self.go()
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("gvisor-release", r.stdout + r.stderr)
        self.none(r"^(curl|tar|sudo|systemctl) ", r"^docker run")

    def test_missing_checksum_pin_is_refused_before_anything(self):
        self.lock(sha=None)
        r = self.go()
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("gvisor-sha512", r.stdout + r.stderr)
        self.none(r"^(curl|tar|sudo|systemctl) ", r"^docker run")

    def test_missing_alpine_pin_is_refused_before_anything(self):
        self.lock(alpine=None)
        r = self.go()
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("alpine", r.stdout + r.stderr)
        self.none(r"^(curl|tar|sudo|systemctl) ", r"^docker run")

    def test_empty_pin_is_refused(self):
        self.lock(release="")
        r = self.go()
        self.assertNotEqual(r.returncode, 0)
        self.none(r"^(tar|sudo|systemctl) ", r"^docker run")

    def test_checksum_mismatch_is_refused_before_anything_is_installed(self):
        self.lock(sha="0" * 128)
        r = self.go()
        self.assertNotEqual(r.returncode, 0)
        self.assertRegex(r.stdout + r.stderr, r"(?i)checksum|sha512|mismatch")
        self.none(r"^(tar|sudo|systemctl) ", r"^docker run", r"runsc install")

    def test_a_download_changed_after_pinning_is_refused(self):
        (self.root / "payload").write_bytes(PAYLOAD + b"tampered")
        r = self.go()
        self.assertNotEqual(r.returncode, 0)
        self.none(r"^(tar|sudo|systemctl) ", r"^docker run")

    # -- the install

    def test_install_succeeds_in_order(self):
        r = self.go()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        c = self.index(r"^curl ")
        self.assertIn(RELEASE, self.calls()[c])
        t = self.index(r"^tar ", c)
        i = self.index(r"runsc install", t)
        s = self.index(r"systemctl restart docker", i)
        p = self.index(r"^docker run ", s)
        self.index(r"^docker run .*--cpus[ =]1 ", p)

    def test_extracts_into_a_folder_of_its_own(self):
        self.go()
        tar = [l for l in self.calls() if l.startswith("tar ")][0]
        m = re.search(r"(?:-C|--directory)[ =](\S+)", tar)
        self.assertTrue(m, f"tar without -C: {tar}")
        d = m.group(1)
        self.assertNotIn(d.rstrip("/"), ("", "/", "/usr", "/usr/bin", "/usr/local", "/usr/local/bin",
                                          "/bin", "/tmp", str(self.root / "tmp"), "."), d)
        self.assertTrue((Path(d) / "runsc").exists() or d.startswith("/"), d)

    def test_cgroup_flag_for_the_systemd_driver(self):
        r = self.go(STUB_CGROUP="systemd")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.index(r"runsc install -- --systemd-cgroup")

    def test_no_cgroup_flag_for_the_cgroupfs_driver(self):
        r = self.go(STUB_CGROUP="cgroupfs")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.index(r"runsc install")
        self.none(r"systemd-cgroup")

    def test_two_smoke_runs_with_the_guides_limits(self):
        self.go()
        runs = [l for l in self.calls() if l.startswith("docker run ")]
        self.assertEqual(len(runs), 2, runs)
        self.assertRegex(runs[0], r"--rm")
        self.assertRegex(runs[0], r"--runtime[= ]runsc")
        self.assertNotRegex(runs[0], r"--cpus|--memory|--pids-limit")
        for flag in (r"--cpus[ =]1\b", r"--memory[ =]256m\b", r"--pids-limit[ =]64\b", r"--rm",
                     r"--runtime[= ]runsc"):
            self.assertRegex(runs[1], flag)

    def test_fails_unless_docker_info_lists_runsc(self):
        r = self.go(STUB_RUNSC="no")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("runsc", r.stdout + r.stderr)

    def test_fails_when_the_plain_smoke_run_fails(self):
        self.assertNotEqual(self.go(STUB_SMOKE_FAIL="plain").returncode, 0)

    def test_fails_when_the_limited_smoke_run_fails(self):
        r = self.go(STUB_SMOKE_FAIL="limits")
        self.assertNotEqual(r.returncode, 0)

    def test_a_failing_download_fails(self):
        shutil.rmtree(self.stubs.__str__() + "/curl", ignore_errors=True)
        os.remove(os.path.join(self.stubs, "curl"))
        with open(os.path.join(self.stubs, "curl"), "w") as f:
            f.write("#!/bin/sh\necho \"curl $*\" >> \"$LOG\"\nexit 22\n")
        os.chmod(os.path.join(self.stubs, "curl"), 0o755)
        r = self.go()
        self.assertNotEqual(r.returncode, 0)
        self.none(r"^(tar|systemctl) ", r"^docker run")


# ------------------------------------------------------------------ ci-evals-summary.py

def trial(n, passed, usage=None):
    t = {"trial": n, "passed": passed, "results": [], "duration": "1m0s"}
    if usage:
        t["usage"] = usage
    return t


def case(name, agent, passes, usage=None, passed=None):
    return {"case": name, "agent": agent, "adversarial": False,
            "passed": all(passes) if passed is None else passed,
            "trials": [trial(i + 1, p, usage) for i, p in enumerate(passes)]}


class CiEvalsSummaryTest(unittest.TestCase):
    def setUp(self):
        self.t = tmpdir(self)
        self.summary = self.t / "summary.md"
        self.report = self.t / "report.json"

    def go(self, report, to_file=True, path=None):
        if report is not None:
            self.report.write_text(json.dumps(report))
        extra = {"GITHUB_STEP_SUMMARY": str(self.summary)} if to_file else {}
        r = run(["python3", EVALS, path or self.report], env=base_env(self, extra), cwd=self.t)
        return r, (self.summary.read_text() if to_file and self.summary.exists() else r.stdout)

    def line(self, text, name):
        rows = [l for l in text.splitlines() if name in l]
        self.assertEqual(len(rows), 1, f"{name}: {rows}\n{text}")
        return rows[0]

    def test_all_passing_exits_0_with_a_line_per_case(self):
        r, out = self.go([case("alpha-case", "tests", [True] * 3), case("beta-case", "code", [True] * 3)])
        self.assertEqual(r.returncode, 0, r.stderr)
        a = self.line(out, "alpha-case")
        self.assertRegex(a, r"(?i)pass")
        self.assertNotRegex(a, r"(?i)fail")
        self.assertIn("tests", a)
        self.assertIn("code", self.line(out, "beta-case"))
        self.assertIn("pass^3", out)
        self.assertRegex(out, r"2\s*(/|of)\s*2")

    def test_a_failed_case_exits_1_and_is_marked(self):
        r, out = self.go([case("alpha-case", "tests", [True] * 3), case("beta-case", "code", [True, False, True])])
        self.assertEqual(r.returncode, 1)
        self.assertRegex(self.line(out, "beta-case"), r"(?i)fail")
        self.assertNotRegex(self.line(out, "alpha-case"), r"(?i)fail")
        self.assertRegex(out, r"1\s*(/|of)\s*2")

    def test_pass_cubed_is_judged_on_the_trials_not_the_flag(self):
        r, out = self.go([case("beta-case", "code", [True, False, True], passed=True)])
        self.assertEqual(r.returncode, 1)
        self.assertRegex(self.line(out, "beta-case"), r"(?i)fail")

    def test_token_use_and_cost_when_the_report_holds_them(self):
        u = {"input_tokens": 1000, "output_tokens": 500, "cost_usd": 0.25}
        r, out = self.go([case("alpha-case", "tests", [True] * 3, usage=u),
                          case("beta-case", "code", [True] * 3, usage=u)])
        self.assertEqual(r.returncode, 0, r.stderr)
        flat = out.replace(",", "").replace("_", "")
        self.assertTrue("4500" in flat or ("3000" in flat and "1500" in flat) or "6000" in flat, out)
        self.assertRegex(flat, r"\b1\.5(0)?\b")
        self.assertRegex(out, r"(?i)token")
        self.assertRegex(out, r"(?i)cost|\$")

    def test_no_token_or_cost_text_when_the_report_has_none(self):
        r, out = self.go([case("alpha-case", "tests", [True] * 3)])
        self.assertEqual(r.returncode, 0)
        self.assertNotRegex(out, r"(?i)token|cost")

    def test_writes_to_stdout_without_the_summary_file(self):
        r, out = self.go([case("alpha-case", "tests", [True] * 3)], to_file=False)
        self.assertIn("alpha-case", r.stdout)

    def test_appends_to_the_summary_file(self):
        self.summary.write_text("EARLIER\n")
        self.go([case("alpha-case", "tests", [True] * 3)])
        self.assertTrue(self.summary.read_text().startswith("EARLIER"))

    def test_missing_file_exits_1(self):
        r, _ = self.go(None, path=self.t / "none.json")
        self.assertEqual(r.returncode, 1)
        self.assertTrue(r.stderr.strip() or r.stdout.strip())

    def test_a_report_that_is_not_json_exits_1(self):
        self.report.write_text("not json")
        r, _ = self.go(None)
        self.assertEqual(r.returncode, 1)

    def test_a_real_harness_report_shape_is_read(self):
        # the shape src/tests/evals/steps/harness_evals_test.go writes (CaseReport)
        rep = [{"case": "real-case", "agent": "tests", "adversarial": True, "attack": "x",
                "passed": True, "trials": [{"trial": i, "passed": True, "results": [],
                                            "duration": "2m1s", "changed_files": ["a.go"]}
                                           for i in (1, 2, 3)]}]
        r, out = self.go(rep)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("real-case", out)


# ------------------------------------------------------------------ check.sh --prepare

GEN_STUB = '#!/bin/sh\necho "generate.sh $*" >> "$LOG"\nexit 0\n'
PREP_DOCKER = r'''
echo "docker $*" >> "$LOG"
case "$*" in *"go env GOVERSION"*) echo go1.26.11 ;; esac
exit 0
'''


class CheckPrepareTest(unittest.TestCase):
    def setUp(self):
        self.root = tmpdir(self)
        self.repo = self.root / "repo"
        for d in ("scripts", "src/deploy"):
            (self.repo / d).mkdir(parents=True)
        shutil.copy(CHECK, self.repo / "scripts" / "check.sh")
        shutil.copy(REPO / "src/deploy/images.lock", self.repo / "src/deploy/images.lock")
        lock = (self.repo / "src/deploy/images.lock").read_text()
        godog = re.search(r"^godog\s+\S+@(\S+)", lock, re.M).group(1)
        fx = self.repo / "src/engine/checks/testdata/steps/godog/clean"
        fx.mkdir(parents=True)
        (fx / "go.mod").write_text(f"module x\n\nrequire github.com/cucumber/godog {godog}\n")
        (self.repo / "scripts" / "generate.sh").write_text(GEN_STUB)
        os.chmod(self.repo / "scripts" / "generate.sh", 0o755)
        self.log = self.root / "log"
        self.log.write_text("")
        self.stubs = gitenv.stub_dir(self, {"docker": PREP_DOCKER})
        self.env = gitenv.with_path(base_env(self, {"LOG": str(self.log),
                                                    "XDG_CACHE_HOME": str(self.root / "cache")}),
                                    self.stubs)
        subprocess.run(["git", "init", "-q"], cwd=self.repo, env=self.env, check=True)

    def calls(self):
        return self.log.read_text().splitlines()

    def test_prepare_generates_and_fills_the_module_cache(self):
        r = run([self.repo / "scripts" / "check.sh", "--prepare"], env=self.env, cwd=self.repo)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        c = self.calls()
        gen = [i for i, l in enumerate(c) if l.strip() == "generate.sh"]
        self.assertTrue(gen, c)
        dl = [i for i, l in enumerate(c) if l.startswith("docker run") and "go mod download" in l]
        self.assertTrue(dl, c)
        self.assertTrue(all(i > gen[0] for i in dl), c)
        for l in (c[i] for i in dl):
            self.assertRegex(l, r"--network bridge")
        text = "\n".join(c[i] for i in dl)
        for m in ("check", "engine", "slice", "cli", "page", "tests"):
            self.assertRegex(text, r"\b" + m + r"\b")
        self.assertIn("engine/checks/testdata/steps/godog/clean", text)

    def test_prepare_runs_no_step_and_no_check(self):
        r = run([self.repo / "scripts" / "check.sh", "--prepare"], env=self.env, cwd=self.repo)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        c = "\n".join(self.calls())
        self.assertNotRegex(c, r"go vet|go test|go build|go install(?! gotest\.tools/gotestsum@)")
        self.assertNotRegex(c, r"generate\.sh --check")
        self.assertNotIn("== time per step", r.stdout)
        self.assertNotIn("all checks passed", r.stdout)

    def test_prepare_fails_when_generate_fails(self):
        (self.repo / "scripts" / "generate.sh").write_text('#!/bin/sh\necho "generate.sh $*" >> "$LOG"\nexit 3\n')
        r = run([self.repo / "scripts" / "check.sh", "--prepare"], env=self.env, cwd=self.repo)
        self.assertNotEqual(r.returncode, 0)
        self.assertFalse([l for l in self.calls() if "go mod download" in l], self.calls())

    def test_prepare_fails_when_the_download_fails(self):
        gitenv_stub = gitenv.stub_dir(self, {"docker": 'echo "docker $*" >> "$LOG"\nexit 5\n'})
        env = gitenv.with_path(self.env, gitenv_stub)
        r = run([self.repo / "scripts" / "check.sh", "--prepare"], env=env, cwd=self.repo)
        self.assertNotEqual(r.returncode, 0)

    def test_list_does_not_list_prepare(self):
        r = run([CHECK, "--list"], env=base_env(self), cwd=REPO)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotIn("prepare", r.stdout)
        self.assertIn("py-tests", r.stdout)

    def test_prepare_is_not_a_step_name(self):
        r = run([CHECK, "--only", "prepare"], env=base_env(self), cwd=REPO)
        self.assertEqual(r.returncode, 2)


if __name__ == "__main__":
    unittest.main()
