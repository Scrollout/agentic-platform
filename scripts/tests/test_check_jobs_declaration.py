"""The declaration .github/check-jobs.yaml and check.yml agree (T117; research D50, ADR 0053),
written before them.

Rules, each a judge_<rule>(root) over root/.github, with a test on the real files and planted
violations in a temporary tree that is valid otherwise (a baseline test proves it valid):

- declaration: `.github/check-jobs.yaml` has `jobs:`, a mapping that names every job id of
  check.yml's `jobs:` except `config` and `summary` exactly once (a key written twice fails),
  names `config`, `summary` and no unknown id, and gives each entry exactly one of
  `always: true` or `paths:`, a non-empty list of strings, and no other key;
- patterns: every path pattern of the declaration, and of src/tests/evals/suites.yaml, keeps to
  the subset scripts/ci_select.py supports: no `?`, `+`, `[`, `]` or `\\` (GitHub gives them other
  meanings); a leading `!` is the exclude marker; a violation names the job or suite and the
  pattern;
- job_conditions (owner, 2026-11-17): every check.yml job the declaration gives `paths:` has
  exactly `if: contains(fromJSON(needs.config.outputs.jobs), '<its id>')`; every job declared
  `always: true`, `config` and `summary` has no `if:` besides the summary's `always()`;
- config_selects: check.yml's `config` job has `permissions: {contents: read, actions: read}`,
  an output `jobs` that is `${{ steps.<id>.outputs.jobs }}` of the step running
  scripts/ci-check-select.py; that step lists check.yml's successful runs (`gh api` on
  actions/workflows/check.yml/runs with status=success) into RUNS with GH_TOKEN
  `${{ github.token }}`, has SHA `${{ github.sha }}` and EVENT `${{ github.event_name }}` in its
  env and no `${{ }}` in its run; the job has the full history before it (checkout fetch-depth 0
  or, as expected, `git fetch --unshallow` in the selector step), and setup-python and the hashed PyYAML install before it.
"""
import copy
import json
import re
import sys
import tempfile
import unittest
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
DECL = ".github/check-jobs.yaml"
CHECK = ".github/workflows/check.yml"
EXEMPT = ("config", "summary")
HASH = "a" * 40


class _UniqueLoader(yaml.SafeLoader):
    pass


def _no_dupes(loader, node, deep=False):
    keys = [loader.construct_object(k, deep=deep) for k, _ in node.value]
    dup = sorted({str(k) for k in keys if keys.count(k) > 1})
    if dup:
        raise yaml.constructor.ConstructorError(None, None, f"duplicate key(s) {dup}",
                                                node.start_mark)
    return yaml.SafeLoader.construct_mapping(loader, node, deep=deep)


_UniqueLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, _no_dupes)


def load_check(root):
    p = Path(root) / CHECK
    print(f"workflow examined: {p}", file=sys.stderr)
    if not p.exists():
        return None
    data = yaml.safe_load(p.read_text()) or {}
    if True in data and "on" not in data:
        data["on"] = data.pop(True)
    return data


def check_jobs(root):
    w = load_check(root)
    return None if w is None else (w.get("jobs") or {})


def cond_for(job_id):
    return f"contains(fromJSON(needs.config.outputs.jobs), '{job_id}')"


def judge_declaration(root):
    js = check_jobs(root)
    if js is None:
        return [f"{CHECK} is missing"]
    p = Path(root) / DECL
    print(f"declaration examined: {p}", file=sys.stderr)
    if not p.exists():
        return [f"{DECL} is missing"]
    try:
        d = yaml.load(p.read_text(), Loader=_UniqueLoader)
    except yaml.YAMLError as e:
        return [f"{DECL} does not parse: {e}"]
    if not isinstance(d, dict) or not isinstance(d.get("jobs"), dict):
        return [f"{DECL} has no jobs: mapping"]
    decl = d["jobs"]
    v = []
    want = [j for j in js if j not in EXEMPT]
    v += [f"{DECL} does not name the check.yml job {j}" for j in want if j not in decl]
    for j, e in decl.items():
        if j in EXEMPT:
            v.append(f"{DECL} names {j}, which always runs")
            continue
        if j not in js:
            v.append(f"{DECL} names {j}, which is not a check.yml job")
        if not isinstance(e, dict):
            v.append(f"{DECL} job {j} is not a mapping")
            continue
        extra = set(e) - {"always", "paths"}
        if extra:
            v.append(f"{DECL} job {j} has unknown keys {sorted(extra)}")
        has_always, has_paths = "always" in e, "paths" in e
        if has_always == has_paths:
            v.append(f"{DECL} job {j} needs exactly one of always: true or paths:")
            continue
        if has_always and e["always"] is not True:
            v.append(f"{DECL} job {j} has always: {e['always']!r}, not true")
        if has_paths:
            ps = e["paths"]
            if not isinstance(ps, list) or not ps or not all(isinstance(x, str) and x for x in ps):
                v.append(f"{DECL} job {j} paths is not a non-empty list of globs: {ps!r}")
    return v


UNSUPPORTED = re.compile(r"[?+\[\]\\\\]")
SUITES = "src/tests/evals/suites.yaml"


def bad_patterns(owner, patterns):
    out = []
    for pat in patterns if isinstance(patterns, list) else []:
        body = pat[1:] if isinstance(pat, str) and pat.startswith("!") else pat
        if not isinstance(body, str) or UNSUPPORTED.search(body) or "!" in body:
            out.append(f"{owner} pattern {pat!r} uses a character outside the supported subset")
    return out


def judge_patterns(root):
    v = []
    decl = load_declaration(root)
    if decl is None:
        v.append(f"{DECL} is missing or has no jobs: mapping")
    else:
        for j, e in decl.items():
            if isinstance(e, dict):
                v += bad_patterns(f"{DECL} job {j}", e.get("paths"))
    p = Path(root) / SUITES
    if p.exists():
        d = yaml.safe_load(p.read_text()) or {}
        for s in d.get("suites") or []:
            if isinstance(s, dict):
                name = (str(s.get("suite")) + " " + str(s.get("cases") or "")).strip()
                v += bad_patterns(f"{SUITES} suite {name}", s.get("paths"))
    return v


def load_declaration(root):
    p = Path(root) / DECL
    if not p.exists():
        return None
    try:
        d = yaml.safe_load(p.read_text())
    except yaml.YAMLError:
        return None
    jobs = d.get("jobs") if isinstance(d, dict) else None
    return jobs if isinstance(jobs, dict) else None


def judge_job_conditions(root):
    js = check_jobs(root)
    if js is None:
        return [f"{CHECK} is missing"]
    decl = load_declaration(root)
    if decl is None:
        return [f"{DECL} is missing or has no jobs: mapping"]
    v = []
    for j, job in js.items():
        if j == "summary":
            continue
        raw = decl.get(j)
        entry = raw if isinstance(raw, dict) else {}
        if j == "config" or entry.get("always") is True:
            if "if" in job:
                v.append(f"check.yml job {j} has if: {job['if']!r}; it always runs")
            continue
        if "paths" not in entry:
            continue   # judge_declaration reports a job the declaration lacks
        got = job.get("if")
        if got != cond_for(j):
            v.append(f"check.yml job {j} has if: {got!r}, not {cond_for(j)!r}")
    return v


EXPR = re.compile(r"\$\{\{(.*?)\}\}", re.S)
SELECTOR = re.compile(r"\bscripts/ci-check-select\.py\b")


def judge_config_selects(root):
    js = check_jobs(root)
    if js is None:
        return [f"{CHECK} is missing"]
    cfg = js.get("config")
    if cfg is None:
        return ["check.yml has no config job"]
    v = []
    if cfg.get("permissions") != {"contents": "read", "actions": "read"}:
        v.append(f"check.yml config permissions are {cfg.get('permissions')!r}, "
                 "not {contents: read, actions: read}")
    steps = cfg.get("steps") or []
    idx = [i for i, s in enumerate(steps) if SELECTOR.search(str(s.get("run", "")))]
    if len(idx) != 1:
        return v + [f"check.yml config has {len(idx)} steps running scripts/ci-check-select.py, not 1"]
    i = idx[0]
    st = steps[i]
    out = (cfg.get("outputs") or {}).get("jobs")
    sid = st.get("id")
    if not sid or not re.fullmatch(r"\$\{\{\s*steps\.%s\.outputs\.jobs\s*\}\}" % re.escape(str(sid)),
                                   str(out or "")):
        v.append(f"check.yml config output jobs is {out!r}, not the selector step's output jobs")
    run = str(st.get("run", ""))
    if EXPR.search(run):
        v.append("check.yml config: the selector step's run holds a ${{ }} expression")
    m = SELECTOR.search(run)
    sel = m.start() if m else len(run)
    lists = re.search(r"\bgh\s+api\b[^\n]*actions/workflows/check\.yml/runs[^\n]*status=success", run)
    if not lists or lists.start() > sel or not re.search(r"\bRUNS=", run[:sel]):
        v.append("check.yml config: the selector step does not list check.yml's successful runs into RUNS first")
    env = st.get("env") or {}
    for k, val in (("GH_TOKEN", "github.token"), ("SHA", "github.sha"),
                   ("EVENT", "github.event_name")):
        if not re.fullmatch(r"\$\{\{\s*%s\s*\}\}" % re.escape(val), str(env.get(k, ""))):
            v.append(f"check.yml config: the selector step's env {k} is {env.get(k)!r}, not ${{{{ {val} }}}}")
    before = steps[:i]
    deep = any(str(s.get("uses", "")).startswith("actions/checkout@")
               and str((s.get("with") or {}).get("fetch-depth")) == "0" for s in before)
    unshallow = any(re.search(r"\bgit\s+fetch\b[^\n]*--unshallow", str(s.get("run", "")))
                    for s in before) or re.search(r"\bgit\s+fetch\b[^\n]*--unshallow", run[:sel])
    if not (deep or unshallow):
        v.append("check.yml config: no full history before the selector")
    sp = [k for k, s in enumerate(before) if str(s.get("uses", "")).startswith("actions/setup-python@")]
    ins = [k for k, s in enumerate(before) if "--require-hashes" in str(s.get("run", ""))
           and "requirements-ci.txt" in str(s.get("run", ""))]
    if not sp or not ins or sp[0] > ins[-1]:
        v.append("check.yml config: no setup-python then hashed PyYAML install before the selector")
    return v


# ---------------------------------------------------------------- planted tree

CHECKOUT = {"uses": f"actions/checkout@{HASH}", "with": {"persist-credentials": False}}
LIST = ('RUNS=$(gh api "repos/$GITHUB_REPOSITORY/actions/workflows/check.yml/runs?status=success'
        '&per_page=100" --jq \'[.workflow_runs[] | {head_sha, head_branch, conclusion}]\' || true)\n'
        "export RUNS\n")
SELECT_RUN = "git fetch -q --unshallow --no-tags origin\n" + LIST + "python3 scripts/ci-check-select.py\n"


def good_check():
    cfg = {"runs-on": "ubuntu-latest",
           "permissions": {"contents": "read", "actions": "read"},
           "outputs": {"runner": "${{ steps.pins.outputs.runner }}",
                       "jobs": "${{ steps.select.outputs.jobs }}"},
           "steps": [copy.deepcopy(CHECKOUT),
                     {"id": "pins", "run": "echo runner=x >> \"$GITHUB_OUTPUT\""},
                     {"uses": f"actions/setup-python@{HASH}",
                      "with": {"python-version": "${{ steps.pins.outputs.python }}"}},
                     {"run": "python3 -m pip install --require-hashes -r scripts/tools/requirements-ci.txt"},
                     {"id": "select", "env": {"GH_TOKEN": "${{ github.token }}",
                                              "SHA": "${{ github.sha }}",
                                              "EVENT": "${{ github.event_name }}"},
                      "run": SELECT_RUN}]}
    js = {"config": cfg}
    for j in ("py_tests", "layout", "markdown", "go", "evals-current"):
        js[j] = {"needs": "config", "runs-on": "x",
                 "steps": [copy.deepcopy(CHECKOUT), {"run": f"scripts/check.sh --only {j}"}]}
        if j not in ("layout", "evals-current"):   # GOOD_DECL's always jobs carry no if:
            js[j]["if"] = cond_for(j)
    js["summary"] = {"if": "always()", "needs": list(js), "runs-on": "ubuntu-latest",
                     "steps": [{"run": "python3 scripts/ci-summary.py"}]}
    return {"name": "check", "on": {"push": None, "workflow_dispatch": None},
            "permissions": {"contents": "read"}, "jobs": js}


GOOD_DECL = """jobs:
  py_tests:
    paths: ["**", "!specs/**"]
  layout: {always: true}
  markdown:
    paths: ["**.md"]
  go:
    paths: ["**", "!docs/**"]
  evals-current: {always: true}
"""


def planted(tc, mutate_check=None, decl: "str | None" = GOOD_DECL):
    tmp = tempfile.TemporaryDirectory()
    tc.addCleanup(tmp.cleanup)
    root = Path(tmp.name)
    (root / ".github" / "workflows").mkdir(parents=True)
    w = good_check()
    if mutate_check:
        mutate_check(w)
    (root / CHECK).write_text(yaml.safe_dump(w, sort_keys=False))
    if decl is not None:
        (root / DECL).write_text(decl)
    return root


def real_copy(tc, decl_edit=None, check_edit=None):
    """A copy of the real check.yml and declaration, each text optionally edited."""
    tmp = tempfile.TemporaryDirectory()
    tc.addCleanup(tmp.cleanup)
    root = Path(tmp.name)
    (root / ".github" / "workflows").mkdir(parents=True)
    tc.assertTrue((REPO / DECL).exists(), f"{DECL} does not exist")
    for rel, edit in ((CHECK, check_edit), (DECL, decl_edit)):
        text = (REPO / rel).read_text()
        (root / rel).write_text(edit(text) if edit else text)
    return root


class BaselineTest(unittest.TestCase):
    def test_planted_tree_is_valid(self):
        root = planted(self)
        for fn in (judge_declaration, judge_job_conditions, judge_config_selects):
            with self.subTest(fn.__name__):
                self.assertEqual(fn(root), [])

    def test_fetch_depth_zero_also_gives_history(self):
        def m(w):
            c = w["jobs"]["config"]
            c["steps"][0]["with"]["fetch-depth"] = 0
            c["steps"][-1]["run"] = LIST + "python3 scripts/ci-check-select.py\n"
        self.assertEqual(judge_config_selects(planted(self, m)), [])


class DeclarationTest(unittest.TestCase):
    def test_real_declaration_names_every_job_once(self):
        self.assertEqual(judge_declaration(REPO), [])

    def test_planted_missing_declaration_fails(self):
        self.assertTrue(judge_declaration(planted(self, decl=None)))

    def test_planted_job_left_out_fails_naming_it(self):
        decl = GOOD_DECL.replace("  go:\n    paths: [\"**\", \"!docs/**\"]\n", "")
        v = judge_declaration(planted(self, decl=decl))
        self.assertTrue(any("go" in x for x in v), v)

    def test_planted_new_workflow_job_not_declared_fails_naming_it(self):
        def m(w):
            w["jobs"]["gateway"] = {"needs": "config", "if": cond_for("gateway"), "runs-on": "x",
                                    "steps": [{"run": "true"}]}
        v = judge_declaration(planted(self, m))
        self.assertTrue(any("gateway" in x for x in v), v)

    def test_planted_job_named_twice_fails(self):
        v = judge_declaration(planted(self, decl=GOOD_DECL + "  layout: {always: true}\n"))
        self.assertTrue(any("layout" in x for x in v), v)

    def test_planted_config_or_summary_named_fails(self):
        for j in EXEMPT:
            with self.subTest(j):
                v = judge_declaration(planted(self, decl=GOOD_DECL + f"  {j}: {{always: true}}\n"))
                self.assertTrue(any(j in x for x in v), v)

    def test_planted_unknown_job_fails(self):
        v = judge_declaration(planted(self, decl=GOOD_DECL + "  nosuch: {always: true}\n"))
        self.assertTrue(any("nosuch" in x for x in v), v)

    def test_planted_bad_entries_fail(self):
        for entry in ("{always: true, paths: ['**']}", "{}", "{always: false}", "{always: 'yes'}",
                      "{paths: []}", "{paths: '**'}", "{paths: [1]}", "{paths: ['']}",
                      "{paths: ['**'], when: push}", "true"):
            with self.subTest(entry):
                decl = GOOD_DECL.replace("  layout: {always: true}\n", f"  layout: {entry}\n")
                v = judge_declaration(planted(self, decl=decl))
                self.assertTrue(any("layout" in x for x in v), v)

    def test_planted_no_jobs_mapping_fails(self):
        for decl in ("", "jobs: []\n", "suites: {}\n", "jobs: {a: [}\n"):
            with self.subTest(decl):
                self.assertTrue(judge_declaration(planted(self, decl=decl)))

    def test_planted_real_copy_without_a_job_fails_naming_it(self):
        def drop_docker(text):
            d = yaml.safe_load(text)
            self.assertIn("docker", d["jobs"], "the real declaration has no docker entry to drop")
            del d["jobs"]["docker"]
            return yaml.safe_dump(d, sort_keys=False)
        v = judge_declaration(real_copy(self, decl_edit=drop_docker))
        self.assertTrue(any("docker" in x for x in v), v)


GOOD_SUITES = """suites:
  - suite: analyst
    cases: ""
    paths: ["src/agents/analyst/**", "src/tests/evals/**/*.go"]
    pins: [nginx]
  - suite: steps
    cases: review
    paths: ["src/agents/review.yaml", "src/agents/review/**"]
    pins: []
"""


class PatternsTest(unittest.TestCase):
    def test_real_patterns_keep_to_the_subset(self):
        self.assertEqual(judge_patterns(REPO), [])

    def tree(self, decl=GOOD_DECL, suites=GOOD_SUITES):
        root = planted(self, decl=decl)
        (root / SUITES).parent.mkdir(parents=True)
        (root / SUITES).write_text(suites)
        return root

    def test_planted_tree_is_valid(self):
        self.assertEqual(judge_patterns(self.tree()), [])

    def test_planted_declaration_pattern_fails_naming_job_and_pattern(self):
        for pat in ("src/?.go", "src/a+b/**", "src/[ab]/**", "src/a]/**", "src\\x/**",
                    "!src/?.go", "!!src/**", "src/!x/**"):
            with self.subTest(pat):
                decl = GOOD_DECL.replace('paths: ["**.md"]', "paths: [" + json.dumps(pat) + "]")
                self.assertNotEqual(decl, GOOD_DECL)
                v = judge_patterns(self.tree(decl=decl))
                self.assertTrue(any("markdown" in x and repr(pat) in x for x in v), v)

    def test_planted_suites_pattern_fails_naming_suite_and_pattern(self):
        for pat in ("src/agents/review/?.md", "src/agents/[rv]eview/**", "src/a+/**"):
            with self.subTest(pat):
                suites = GOOD_SUITES.replace('"src/agents/review/**"', json.dumps(pat))
                self.assertNotEqual(suites, GOOD_SUITES)
                v = judge_patterns(self.tree(suites=suites))
                self.assertTrue(any("steps review" in x and repr(pat) in x for x in v), v)


class JobConditionsTest(unittest.TestCase):
    def test_real_jobs_have_the_selector_condition(self):
        self.assertEqual(judge_job_conditions(REPO), [])

    def test_planted_missing_condition_fails_naming_the_job(self):
        for j in ("go", "py_tests"):
            with self.subTest(j):
                def m(w, j=j):
                    del w["jobs"][j]["if"]
                v = judge_job_conditions(planted(self, m))
                self.assertTrue(any(j in x for x in v), v)

    def test_planted_condition_on_an_always_job_fails_naming_it(self):
        for j, cond in (("layout", cond_for("layout")), ("evals-current", cond_for("evals-current")),
                        ("evals-current", "github.ref_name != 'main'"), ("layout", "always()")):
            with self.subTest(f"{j}: {cond}"):
                def m(w, j=j, cond=cond):
                    w["jobs"][j]["if"] = cond
                v = judge_job_conditions(planted(self, m))
                self.assertTrue(any(j in x for x in v), v)

    def test_planted_job_turned_always_must_lose_its_condition(self):
        decl = GOOD_DECL.replace('  go:\n    paths: ["**", "!docs/**"]\n', "  go: {always: true}\n")
        self.assertNotEqual(decl, GOOD_DECL)
        v = judge_job_conditions(planted(self, decl=decl))
        self.assertTrue(any("go" in x for x in v), v)

    def test_planted_missing_declaration_fails(self):
        self.assertTrue(judge_job_conditions(planted(self, decl=None)))

    def test_planted_wrong_conditions_fail(self):
        for cond in (cond_for("markdown"), "${{ " + cond_for("go") + " }}",
                     "contains(needs.config.outputs.jobs, 'go')",
                     cond_for("go") + " && always()", "always()", "true"):
            with self.subTest(cond):
                def m(w, cond=cond):
                    w["jobs"]["go"]["if"] = cond
                v = judge_job_conditions(planted(self, m))
                self.assertTrue(any("go" in x for x in v), v)

    def test_planted_condition_on_config_fails(self):
        def m(w):
            w["jobs"]["config"]["if"] = "always()"
        self.assertTrue(judge_job_conditions(planted(self, m)))

    def test_planted_real_copy_with_a_condition_removed_fails(self):
        cond = cond_for("docker")

        def edit(text):
            self.assertIn(cond, text, "the real check.yml has no docker condition to drop")
            return re.sub(r"(?m)^\s*if:\s*['\"]?" + re.escape(cond) + r"['\"]?\s*\n", "", text, count=1)
        v = judge_job_conditions(real_copy(self, check_edit=edit))
        self.assertTrue(any("docker" in x for x in v), v)


class ConfigSelectsTest(unittest.TestCase):
    def test_real_config_runs_the_selector(self):
        self.assertEqual(judge_config_selects(REPO), [])

    def mutated(self, fn):
        return judge_config_selects(planted(self, fn))

    def test_planted_permissions_fail(self):
        for perms in (None, {"contents": "read"}, {"actions": "read"},
                      {"contents": "read", "actions": "write"},
                      {"contents": "read", "actions": "read", "checks": "write"}):
            with self.subTest(perms):
                def m(w, perms=perms):
                    if perms is None:
                        del w["jobs"]["config"]["permissions"]
                    else:
                        w["jobs"]["config"]["permissions"] = perms
                self.assertTrue(self.mutated(m))

    def test_planted_output_fails(self):
        for out in (None, "${{ steps.pins.outputs.jobs }}", "${{ steps.select.outputs.matrix }}",
                    '["layout"]'):
            with self.subTest(out):
                def m(w, out=out):
                    o = w["jobs"]["config"]["outputs"]
                    if out is None:
                        del o["jobs"]
                    else:
                        o["jobs"] = out
                self.assertTrue(self.mutated(m))

    def test_planted_no_selector_fails(self):
        def m(w):
            w["jobs"]["config"]["steps"][-1]["run"] = LIST + "python3 scripts/ci-evals-select.py\n"
        self.assertTrue(self.mutated(m))

    def test_planted_runs_not_listed_fail(self):
        for run in ("git fetch -q --unshallow origin\npython3 scripts/ci-check-select.py\n",
                    "git fetch -q --unshallow origin\n" + LIST.replace("check.yml", "evals.yml")
                    + "python3 scripts/ci-check-select.py\n",
                    "git fetch -q --unshallow origin\n" + LIST.replace("status=success", "status=completed")
                    + "python3 scripts/ci-check-select.py\n",
                    "git fetch -q --unshallow origin\npython3 scripts/ci-check-select.py\n" + LIST):
            with self.subTest(run):
                def m(w, run=run):
                    w["jobs"]["config"]["steps"][-1]["run"] = run
                self.assertTrue(self.mutated(m))

    def test_planted_env_fails(self):
        for k, val in (("GH_TOKEN", None), ("SHA", None), ("SHA", "HEAD"), ("EVENT", None),
                       ("EVENT", "push"), ("GH_TOKEN", "${{ secrets.GITHUB_TOKEN }}")):
            with self.subTest(f"{k}={val}"):
                def m(w, k=k, val=val):
                    env = w["jobs"]["config"]["steps"][-1]["env"]
                    if val is None:
                        del env[k]
                    else:
                        env[k] = val
                self.assertTrue(self.mutated(m))

    def test_planted_expression_in_run_fails(self):
        def m(w):
            st = w["jobs"]["config"]["steps"][-1]
            st["run"] = "EVENT=${{ github.event_name }}\n" + st["run"]
        self.assertTrue(self.mutated(m))

    def test_planted_no_history_fails(self):
        for run in (LIST + "python3 scripts/ci-check-select.py\n",
                    "git fetch -q --depth=1 origin\n" + LIST + "python3 scripts/ci-check-select.py\n"):
            with self.subTest(run[:30]):
                def m(w, run=run):
                    w["jobs"]["config"]["steps"][-1]["run"] = run
                self.assertTrue(self.mutated(m))

    def test_planted_no_python_install_fails(self):
        for drop in (2, 3):
            with self.subTest(drop):
                def m(w, drop=drop):
                    del w["jobs"]["config"]["steps"][drop]
                self.assertTrue(self.mutated(m))


if __name__ == "__main__":
    unittest.main()
