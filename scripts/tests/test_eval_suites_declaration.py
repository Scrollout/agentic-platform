"""The declaration of the live eval suites' inputs agrees with the code (T115).

src/tests/evals/suites.yaml is the one home of what each suite reads and runs on:

    suites:
      - suite: analyst        # the folder under src/tests/evals: the Go package ./evals/<suite>
        cases: ""             # EVAL_CASES: "" for every case, or the step agent's name
        paths: [<glob>, ...]  # GitHub's paths form: ** across /, * and ? within one segment
        pins: [<name>, ...]   # src/deploy/images.lock pin names

Each rule is a function judge_<rule>(...) returning violations; each has a test on the real
repository and a planted violation (a copy of the declaration missing one glob or pin). Since
T120 evals.yml has no push trigger whose paths could miss a glob: check.yml's evals-current job
runs the selector on every push (scripts/tests/test_workflows_evals_current.py). What a suite reads, found from the repository:

- every file of every workspace-module package `go list -deps -test -tags evals ./evals/<suite>`
  gives (a dependency's own tests are not built, so only its non-test files count; the
  standard library and third-party modules are not repository paths), and every file the
  packages embed;
- the Go workspace the suites build in: src/go.work, src/go.work.sum, and the go.mod and go.sum
  (where present) of every module its `use` list names;
- every file under the suite package's testdata/;
- every repository path the packages' Go files build: `filepath.Join(<root>, "src", ...)` (its
  leading string literals; a folder means every tracked file in it), `filepath.Join(
  filepath.Dir(...), "..", ...)` and "../..." literals, relative to the Go file's folder. The
  folder src/agents (mounted whole) counts as the suite's own agent: src/agents/<agent>.yaml and
  src/agents/<agent>/**, the agent being `cases`, or the suite's name when cases is "". The
  folder src itself is an image build context: it counts as the files the built Dockerfiles
  COPY or ADD from it (Dockerfile.agent takes none);
- the pins: every `pin(..., "<name>")` and `f[0] == "<name>"` lock lookup in those Go files, and
  the agent image's build arguments (imageArgs in src/tests/lab/settings.go) when a Go file
  builds src/deploy/Dockerfile.agent.

go list runs in the pinned golang container (offline, the module cache scripts/check.sh fills)
when Docker is there, else with the host's go, offline.
"""
import copy
import json
import os
import re
import shutil
import subprocess
import sys
import unittest
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
DECL = REPO / "src" / "tests" / "evals" / "suites.yaml"
LOCK = REPO / "src" / "deploy" / "images.lock"
SETTINGS = REPO / "src" / "tests" / "lab" / "settings.go"
AGENTS = "src/agents"
LOCK_REL = "src/deploy/images.lock"
GO_LIST_TIMEOUT = 240


# ---------------------------------------------------------------- helpers

def glob_rx(g):
    out, i = "", 0
    while i < len(g):
        if g.startswith("**", i):
            out, i = out + ".*", i + 2
        elif g[i] == "*":
            out, i = out + "[^/]*", i + 1
        elif g[i] == "?":
            out, i = out + "[^/]", i + 1
        else:
            out, i = out + re.escape(g[i]), i + 1
    return re.compile(out + r"\Z")


def covered(path, globs):
    return any(glob_rx(g).match(path) for g in globs)


def tracked(prefix):
    r = subprocess.run(["git", "ls-files", "-z", "--", prefix], cwd=REPO, capture_output=True,
                       text=True, timeout=30)
    assert r.returncode == 0, r.stderr
    return [p for p in r.stdout.split("\0") if p]


def load_decl(path=DECL):
    return yaml.safe_load(Path(path).read_text())


def workspace_files():
    """src/go.work, src/go.work.sum and each `use` module's go.mod and go.sum (where present):
    the suites build in workspace mode, so these choose every module version they compile."""
    text = (REPO / "src" / "go.work").read_text()
    uses = re.findall(r"^\s*use\s+([^\s(]\S*)\s*$", text, re.M)
    for block in re.findall(r"^\s*use\s*\((.*?)\)", text, re.M | re.S):
        uses += [l.split("//")[0].strip() for l in block.splitlines() if l.split("//")[0].strip()]
    assert uses, "src/go.work has no use list"
    out = [p for p in ("src/go.work", "src/go.work.sum") if (REPO / p).exists()]
    for u in uses:
        d = os.path.normpath(f"src/{u}")
        out += [f"{d}/{n}" for n in ("go.mod", "go.sum") if (REPO / d / n).exists()]
    return out


def agent_of(s):
    return s.get("cases") or s["suite"]


def lock_names():
    return {l.split()[0] for l in LOCK.read_text().splitlines()
            if len(l.split()) >= 2 and not l.startswith("#")}


def go_list(suite):
    """The workspace-module packages of `go list -deps -test -tags evals ./evals/<suite>`."""
    args = ["go", "list", "-deps", "-test", "-tags", "evals", "-json", f"./evals/{suite}"]
    env = {"GOTOOLCHAIN": "local", "GOPROXY": "off", "GOFLAGS": "-mod=readonly"}
    if shutil.which("docker"):
        image = next(l.split()[1] for l in LOCK.read_text().splitlines()
                     if l.split()[:1] == ["golang"])
        cache = Path(os.environ.get("XDG_CACHE_HOME", Path.home() / ".cache")) / "agentic-platform" / "go"
        cmd = ["docker", "run", "--rm", "--network", "none", "-v", f"{REPO / 'src'}:/src:ro",
               "-w", "/src/tests", "-v", f"{cache}:/gopath", "--user", f"{os.getuid()}:{os.getgid()}",
               "-e", "GOPATH=/gopath", "-e", "GOCACHE=/gopath/cache"]
        cmd += sum((["-e", f"{k}={v}"] for k, v in env.items()), []) + [image] + args
        src_prefix = "/src"
        run_env, cwd = None, REPO
    else:
        cmd, src_prefix = args, str(REPO / "src")
        run_env, cwd = dict(os.environ, **env), REPO / "src" / "tests"
    r = subprocess.run(cmd, cwd=cwd, env=run_env, capture_output=True, text=True,
                       timeout=GO_LIST_TIMEOUT)
    assert r.returncode == 0, r.stderr
    pkgs, dec, s, i = [], json.JSONDecoder(), r.stdout, 0
    while True:
        while i < len(s) and s[i].isspace():
            i += 1
        if i >= len(s):
            break
        p, i = dec.raw_decode(s, i)
        if p.get("Standard") or not (p.get("Module") or {}).get("Main"):
            continue
        d = p["Dir"]
        assert d.startswith(src_prefix + "/"), d
        p["RelDir"] = "src/" + d[len(src_prefix) + 1:]
        pkgs.append(p)
    return pkgs


FILE_KEYS = ("GoFiles", "CgoFiles", "TestGoFiles", "XTestGoFiles", "EmbedFiles",
             "TestEmbedFiles", "XTestEmbedFiles", "OtherFiles")
DEP_KEYS = ("GoFiles", "CgoFiles", "EmbedFiles", "OtherFiles")
SRC_KEYS = ("GoFiles", "CgoFiles", "TestGoFiles", "XTestGoFiles")

JOIN = re.compile(r"filepath\.Join\(")
LIT = re.compile(r'\s*"([^"\\]*)"\s*')


def join_args(text, start):
    """The top-level arguments of the call whose '(' ends at start."""
    depth, args, cur, i = 1, [], "", start
    while i < len(text) and depth:
        c = text[i]
        if c in "\"`":
            j = i + 1
            while j < len(text) and text[j] != c:
                j += 2 if c == '"' and text[j] == "\\" else 1
            cur += text[i:j + 1]
            i = j + 1
            continue
        if c in "([{":
            depth += 1
        elif c in ")]}":
            depth -= 1
            if not depth:
                break
        if c == "," and depth == 1:
            args.append(cur)
            cur = ""
        else:
            cur += c
        i += 1
    args.append(cur)
    return [a.strip() for a in args]


def built_paths(rel_file, text):
    """Repository paths a Go file builds: (path, how)."""
    out = []
    here = Path(rel_file).parent
    for m in JOIN.finditer(text):
        args = join_args(text, m.end())
        lits = []
        for a in args[1:]:
            lm = LIT.fullmatch(a)
            if not lm:
                break
            lits.append(lm.group(1))
        if lits and lits[0] == "src":
            out.append(("/".join(lits), f"filepath.Join in {rel_file}"))
        elif args[0].startswith("filepath.Dir(") and lits and lits[0] == "..":
            out.append((os.path.normpath(here.joinpath(*lits)), f"filepath.Join(filepath.Dir) in {rel_file}"))
    for lm in re.finditer(r'"(\.\./[^"]+)"', text):
        p = os.path.normpath(here / lm.group(1))
        if (REPO / p).exists():   # a "../" literal that names no file is data, not a path
            out.append((p, f"a relative path in {rel_file}"))
    return out


PIN_CALL = re.compile(r'\b[Pp]in\(\s*[^()]*?"([\w.-]+)"\s*\)')
PIN_LOOKUP = re.compile(r'f\[0\]\s*==\s*"([\w.-]+)"')


def agent_image_pins():
    text = SETTINGS.read_text()
    m = re.search(r'\{"agent",\s*map\[string\]string\{([^}]*)\}', text)
    assert m, "imageArgs has no agent entry in src/tests/lab/settings.go"
    return set(re.findall(r'"[A-Z_]+":\s*"([\w.-]+)"', m.group(1)))


def context_sources(go_text):
    """The repository files the Dockerfiles a Go file builds take from the build context src."""
    out = []
    for name in sorted(set(re.findall(r'"(Dockerfile\.[\w-]+)"', go_text))):
        for line in (REPO / "src" / "deploy" / name).read_text().splitlines():
            f = line.split()
            if f[:1] in (["COPY"], ["ADD"]) and not any(x.startswith("--from") for x in f):
                for src in [x for x in f[1:-1] if not x.startswith("--")]:
                    p = os.path.normpath(f"src/{src}")
                    out += tracked(p) if (REPO / p).is_dir() else [p]
    return out


_FACTS = {}


def facts(s):
    """{path: why} the suite reads, and {pin: why} it runs on, found from the repository."""
    key = (s["suite"], agent_of(s))
    if key in _FACTS:
        return _FACTS[key]
    files, pins = {}, {}
    pkgs = go_list(s["suite"])
    suite_dir = f"src/tests/evals/{s['suite']}"
    for p in pkgs:
        # A dependency's tests are not built: only the suite's own package counts its test files.
        p["Keys"] = FILE_KEYS if p["RelDir"] == suite_dir else DEP_KEYS
        for k in p["Keys"]:
            for f in p.get(k) or []:
                if not f.startswith("/"):
                    files[f"{p['RelDir']}/{f}"] = f"{k} of {p['ImportPath']}"
    for f in tracked(f"{suite_dir}/testdata"):
        files[f] = f"testdata of {suite_dir}"
    for f in workspace_files():
        files[f] = "the Go workspace (src/go.work)"
    for p in pkgs:
        for k in SRC_KEYS:
            if k not in p["Keys"]:
                continue
            for f in p.get(k) or []:
                if f.startswith("/"):
                    continue
                rel = f"{p['RelDir']}/{f}"
                text = (REPO / rel).read_text()
                for path, how in built_paths(rel, text):
                    if path == AGENTS:
                        a = agent_of(s)
                        for t in [f"{AGENTS}/{a}.yaml"] + tracked(f"{AGENTS}/{a}"):
                            files[t] = f"{how} (src/agents, agent {a})"
                    elif path == "src":
                        # The image build context: a build reads from it only what its
                        # Dockerfiles COPY or ADD (not --from a stage).
                        for t in context_sources(text):
                            files[t] = f"{how} (build context src)"
                    elif (REPO / path).is_dir():
                        for t in tracked(path):
                            files[t] = how
                    elif path != LOCK_REL:
                        files[path] = how
                for n in PIN_CALL.findall(text) + PIN_LOOKUP.findall(text):
                    pins[n] = f"read in {rel}"
                if "Dockerfile.agent" in text:
                    for n in agent_image_pins():
                        pins[n] = f"agent image build argument (imageArgs), built in {rel}"
    print(f"suite {key}: {len(pkgs)} packages, {len(files)} paths, pins {sorted(pins)}",
          file=sys.stderr)
    _FACTS[key] = (files, pins)
    return _FACTS[key]


# ---------------------------------------------------------------- rules

def judge_schema(decl):
    v = []
    if not isinstance(decl, dict) or not isinstance(decl.get("suites"), list) or not decl["suites"]:
        return ["the declaration has no suites list"]
    seen = set()
    for s in decl["suites"]:
        if set(s) != {"suite", "cases", "paths", "pins"}:
            v.append(f"suite {s.get('suite')!r} has keys {sorted(s)}, not suite, cases, paths, pins")
            continue
        k = (s["suite"], s["cases"])
        if k in seen:
            v.append(f"suite {k} declared twice")
        seen.add(k)
        if not (REPO / "src" / "tests" / "evals" / str(s["suite"]) / "run.sh").exists():
            v.append(f"suite {s['suite']!r} has no src/tests/evals/{s['suite']}/run.sh")
        if not isinstance(s["cases"], str):
            v.append(f"suite {k}: cases is not a string")
        elif not (REPO / AGENTS / f"{agent_of(s)}.yaml").exists():
            v.append(f"suite {k}: no agent definition src/agents/{agent_of(s)}.yaml")
        if not s["paths"] or not all(isinstance(p, str) and p for p in s["paths"]):
            v.append(f"suite {k}: paths is not a list of globs")
        if not isinstance(s["pins"], list):
            v.append(f"suite {k}: pins is not a list")
    return v


def judge_inputs(decl, facts_of=facts):
    v = []
    for s in decl["suites"]:
        files, _ = facts_of(s)
        for f, why in sorted(files.items()):
            if not covered(f, s["paths"]):
                v.append(f"suite ({s['suite']}, {s['cases']!r}) reads {f} ({why}) but no glob covers it")
    return v


def judge_pins(decl, facts_of=facts, known=None):
    known = lock_names() if known is None else known
    v = []
    for s in decl["suites"]:
        _, pins = facts_of(s)
        for n, why in sorted(pins.items()):
            if n not in s["pins"]:
                v.append(f"suite ({s['suite']}, {s['cases']!r}) runs on pin {n} ({why}) but does not declare it")
        for n in s["pins"]:
            if n not in known:
                v.append(f"suite ({s['suite']}, {s['cases']!r}) declares pin {n}, which images.lock does not have")
    return v


def judge_lock_not_globbed(decl):
    """images.lock is judged by its pins only: a glob over it would run a suite on any pin."""
    return [f"suite ({s['suite']}, {s['cases']!r}) glob {g} covers {LOCK_REL}"
            for s in decl["suites"] for g in s["paths"] if glob_rx(g).match(LOCK_REL)]


# ---------------------------------------------------------------- tests

def real_decl(tc):
    tc.assertTrue(DECL.exists(), f"{DECL.relative_to(REPO)} does not exist")
    return load_decl()


def without_glob(decl, s_index, path):
    """A copy of the declaration whose suite s_index lost the globs that cover path."""
    d = copy.deepcopy(decl)
    s = d["suites"][s_index]
    s["paths"] = [g for g in s["paths"] if not glob_rx(g).match(path)]
    return d


class SchemaTest(unittest.TestCase):
    def test_real_schema(self):
        self.assertEqual(judge_schema(real_decl(self)), [])

    def test_planted_unknown_suite_and_agent(self):
        d = copy.deepcopy(real_decl(self))
        d["suites"][0]["suite"] = "nope"
        self.assertTrue(judge_schema(d))
        d = copy.deepcopy(real_decl(self))
        d["suites"][-1]["cases"] = "nobody"
        self.assertTrue(judge_schema(d))

    def test_planted_duplicate_and_extra_key(self):
        d = copy.deepcopy(real_decl(self))
        d["suites"].append(copy.deepcopy(d["suites"][0]))
        self.assertTrue(any("twice" in x for x in judge_schema(d)))
        d = copy.deepcopy(real_decl(self))
        d["suites"][0]["extra"] = 1
        self.assertTrue(judge_schema(d))

    def test_the_four_suites(self):
        got = {(s["suite"], s["cases"]) for s in real_decl(self)["suites"]}
        self.assertEqual(got, {("analyst", ""), ("steps", "tests"), ("steps", "code"),
                               ("steps", "review")})


class FactsTest(unittest.TestCase):
    """The scan finds what T115 names, so a passing agreement is not an empty one."""

    def test_known_inputs_are_found(self):
        want = {("analyst", ""): ["src/engine/slices/message.go", "src/agents/analyst/instruction.md",
                                  "src/agents/analyst.yaml", "src/catalogue/api-feature-go/pattern.yaml",
                                  "src/tests/evals/runevents/runevents.go", "src/tests/go.mod", "src/go.work", "src/engine/go.mod"],
                ("steps", "review"): ["src/deploy/Dockerfile.agent", "src/agents/review/instruction.md",
                                      "src/agents/review.yaml",
                                      "src/catalogue/api-feature-go/pattern.yaml",
                                      "src/tests/evals/steps/testdata/slice/contract.yaml"]}
        for (suite, cases), paths in want.items():
            files, _ = facts({"suite": suite, "cases": cases})
            for p in paths:
                self.assertIn(p, files, (suite, cases))
            self.assertTrue(any(f.startswith("src/deploy/gateway/") for f in files), (suite, cases))
            if cases == "review":
                self.assertFalse(any(f.startswith(f"{AGENTS}/code") for f in files),
                                 "the review suite counts the code agent")
        self.assertTrue(any(f.startswith("src/engine/api/preview/")
                            for f in facts({"suite": "analyst", "cases": ""})[0]))
        self.assertTrue(any(f.startswith("src/catalogue/api-feature-go/template/")
                            for f in facts({"suite": "steps", "cases": "code"})[0]))

    def test_known_pins_are_found(self):
        self.assertEqual(set(facts({"suite": "analyst", "cases": ""})[1]), {"docker-agent", "nginx"})
        self.assertEqual(set(facts({"suite": "steps", "cases": "tests"})[1]),
                         {"docker-agent", "golang", "nginx", "oapi-codegen"})

    def test_scanner_on_planted_source(self):
        src = ('x := filepath.Join(l.root, "src", "deploy", "gateway")\n'
               'y := filepath.Join(root, "src", "agents", name+".yaml")\n'
               'z := filepath.Join(filepath.Dir(f), "..", "..", "agents", "a.md")\n'
               'w := filepath.Join(dir, "out.txt")\n'
               'p := "../../../catalogue/api-feature-go/pattern.yaml"\n'
               'q := "../not-a-file.go"\n')
        got = {p for p, _ in built_paths("src/tests/evals/x/a.go", src)}
        self.assertEqual(got, {"src/deploy/gateway", "src/agents", "src/tests/agents/a.md",
                               "src/catalogue/api-feature-go/pattern.yaml"})


class WorkspaceTest(unittest.TestCase):
    def test_workspace_files_from_the_use_list(self):
        got = set(workspace_files())
        for p in ("src/go.work", "src/go.work.sum", "src/engine/go.mod", "src/tests/go.mod",
                  "src/slice/go.mod"):
            self.assertIn(p, got)
        self.assertEqual({p for p in got if p.endswith("/go.mod")},
                         {f"src/{m}/go.mod" for m in ("check", "cli", "engine", "page", "retry", "slice", "tests")})

    def test_planted_missing_workspace_file_fails(self):
        decl = real_decl(self)
        for path in ("src/go.work", "src/engine/go.mod"):
            for i, s in enumerate(decl["suites"]):
                with self.subTest(suite=(s["suite"], s["cases"]), path=path):
                    d = without_glob(decl, i, path)
                    v = judge_inputs({"suites": [d["suites"][i]]})
                    self.assertTrue(any(path in x for x in v), v)


class InputsTest(unittest.TestCase):
    def test_real_every_input_is_declared(self):
        self.assertEqual(judge_inputs(real_decl(self)), [])

    def test_planted_missing_glob_fails(self):
        decl = real_decl(self)
        probes = {("analyst", ""): ["src/engine/slices/message.go", "src/tests/go.mod",
                                    "src/agents/analyst/instruction.md"],
                  ("steps", "review"): ["src/deploy/Dockerfile.agent",
                                        "src/agents/review/instruction.md",
                                        "src/tests/evals/steps/testdata/slice/contract.yaml"]}
        for i, s in enumerate(decl["suites"]):
            for path in probes.get((s["suite"], s["cases"]), []):
                with self.subTest(suite=(s["suite"], s["cases"]), path=path):
                    v = judge_inputs({"suites": [without_glob(decl, i, path)["suites"][i]]})
                    self.assertTrue(any(path in x for x in v), v)

    def test_planted_every_suite_missing_its_first_glob_fails(self):
        decl = real_decl(self)
        for i, s in enumerate(decl["suites"]):
            files, _ = facts(s)
            first = sorted(files)[0]
            with self.subTest(suite=(s["suite"], s["cases"])):
                d = without_glob(decl, i, first)
                self.assertTrue(judge_inputs({"suites": [d["suites"][i]]}))


class PinsTest(unittest.TestCase):
    def test_real_every_pin_is_declared(self):
        self.assertEqual(judge_pins(real_decl(self)), [])

    def test_planted_missing_pin_fails(self):
        decl = real_decl(self)
        for i, s in enumerate(decl["suites"]):
            for n in sorted(facts(s)[1]):
                with self.subTest(suite=(s["suite"], s["cases"]), pin=n):
                    d = copy.deepcopy(decl["suites"][i])
                    d["pins"].remove(n)
                    v = judge_pins({"suites": [d]})
                    self.assertTrue(any(n in x for x in v), v)

    def test_planted_unknown_pin_fails(self):
        d = copy.deepcopy(real_decl(self)["suites"][0])
        d["pins"].append("no-such-pin")
        self.assertTrue(any("no-such-pin" in x for x in judge_pins({"suites": [d]})))

    def test_agent_image_pins_parsed(self):
        self.assertEqual(agent_image_pins(), {"docker-agent", "golang"})


class LockNotGlobbedTest(unittest.TestCase):
    def test_real_no_glob_covers_images_lock(self):
        self.assertEqual(judge_lock_not_globbed(real_decl(self)), [])

    def test_planted_glob_over_images_lock_fails(self):
        for g in ("src/deploy/**", "src/deploy/images.lock", "src/**"):
            with self.subTest(g):
                d = copy.deepcopy(real_decl(self))
                d["suites"][0]["paths"].append(g)
                self.assertTrue(judge_lock_not_globbed(d))


class GlobTest(unittest.TestCase):
    def test_glob_form(self):
        self.assertTrue(covered("src/a/b/c.go", ["src/a/**"]))
        self.assertTrue(covered("src/a/x.yaml", ["src/a/*.yaml"]))
        self.assertFalse(covered("src/a/b/x.yaml", ["src/a/*.yaml"]))
        self.assertFalse(covered("src/ab/c.go", ["src/a/**"]))


if __name__ == "__main__":
    unittest.main()
