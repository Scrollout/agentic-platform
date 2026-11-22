# Research: Results are reused only under the method that judged them

Phase 0 of the [plan](plan.md) for the [spec](spec.md). Each decision names its deciding source:
the code on `main` (`e4348bf`), a reproduced experiment, or a cited document. Decisions marked
*owner* were put to the project owner as direct questions.

## Sources

| ID | Source |
|----|--------|
| S1 | `src/engine/machine/reuse.go` on `main`: `KeyParts{PatternVersion, Step, Inputs, AgentDigest, Model, Round}`; `ReuseKey` frames each part by its length and writes `round` only when it is not 0, "so a key without a round is unchanged" (research D36 of feature 001). `machine.Event` carries `pattern`, `agent_digest`, `model`, `reuse_key`; no field names the checks' or the procedures' code |
| S2 | `src/engine/durable/workflow.go` `stepKey`: the workflow computes the key itself when no findings are open and passes it in `ReuseLookup.Key`; `durable/activities.go` `LookupReuse` computes it only when `Key` is empty. The workflow's input (`in.Pattern`) is recorded by Temporal at the run's start, so a value taken from it stays the same after an engine upgrade while the run resumes |
| S3 | `src/engine/api/runs.go` (lines 160-190): at start the engine rebuilds the accepted-result store from the run records' `StepAccepted`, `ResultReused` and open-round `StepRejected` events, by their recorded `reuse_key` |
| S4 | `src/engine/slices/persist.go`: a slice record stores `checks` (`part`, `check`, `passed`, `reason`, `from_pattern`) and restores them as they are on `load`; `service.go` `find` is the one lookup behind every slice operation but `List`; `recheck` runs the part checks, and the contract compatibility and lint checks; a check that could not run is a failed check with its reason (`checks.go` `compatibility`); a part whose check fails loses its approval, recorded as `withdrawn` (`service.go` ~680) |
| S5 | `git log -- src/catalogue/api-feature-go/pattern.yaml`: six commits since `effded5`, `version: 1.0.0` throughout (T136) |
| S6 | What runs a step and judges it, on `main`: the engine image (`Dockerfile.engine`: the whole engine module, embedding the pattern, the template's declaration and the agent definitions through `scripts/generate.sh`'s `gen/` copies); the check and project worker images (`Dockerfile.check`, `Dockerfile.project`: `cmd/check`, the template, `alpine-git`); the agent image (`Dockerfile.agent`: the `docker-agent` and `golang` pins); the lint service (`Dockerfile.lint`, `src/deploy/lint/`, the `node` pin). `go list -deps ./cmd/check ./machine ./slices` reaches, in this repository, the `engine`, `check`, `slice` and `retry` modules only |
| S7 | `src/deploy/build.sh` header and ADR 0061: each image has a fingerprint, the digest of a reproducible build (`SOURCE_DATE_EPOCH=0`, `rewrite-timestamp=true`), known only after the image is built |
| S8 | `git ls-files --cached --others --exclude-standard` ([git-ls-files](https://git-scm.com/docs/git-ls-files)): the tracked files and the untracked files that no ignore rule covers, which is what a Docker build with `src/` as its context sees, less what `.gitignore` names (generated code, local state) |
| S9 | Experiment E1 below |
| S10 | Constitution: Principle XX ("Every accepted result MUST record the method, inputs, model, and rule versions"), XVII ("Derive the rest", "Prove agreement"), Engineering Principles "Values that check themselves", "Checks prove they can fail", Clean tree |

## R1. What the method identity is

- **Decision**: the method identity is the SHA-256, in lowercase hex, of a manifest of the
  method's source files and pins, declared once in `src/engine/method/method.json`
  ([contract](contracts/method-declaration.md)): every regular file a `git ls-files --cached
  --others --exclude-standard` lists (S8) whose path matches an `include` glob and no `exclude`
  glob (case-sensitive), as `<sha256 of its bytes>  <path>` lines in path order, then one
  `pin <name> <reference>` line per named pin of `src/deploy/images.lock`, in the declared order.
  The declaration includes the catalogue, the agent definitions and their instructions, the
  `engine`, `check`, `slice` and `retry` modules with `src/go.work`, `src/.dockerignore` (what the
  images ship), the Dockerfiles and lint service of the images that run or judge a step (S6), the
  lint service's npm tools (`scripts/tools/package.json`, `package-lock.json`), and the pins
  `docker-agent`, `golang`, `alpine`, `alpine-git`, `node` and `oapi-codegen` (the template's
  generator in the check image); it excludes `README.md` files, `_test.go` files, `testdata/` and
  the identity file itself. The declaration lies inside `src/engine/`, so a change to it changes
  the identity too. JSON, not YAML: the Go job that checks it runs without PyYAML; its contract
  lives in `src/engine/method/README.md`.
- **Left out, on purpose**: `src/go.work.sum` (it records the terminal client's, page's and tests'
  dependencies too; each included module's `go.sum` already covers its own, FR-002);
  `src/deploy/compose.yaml` (resource limits and wiring of every service; a step's limits and
  budgets come from the pattern); `scripts/generate.sh` (it copies included files, which are
  hashed themselves); the `oasdiff` pin (the engine uses oasdiff as a Go library, pinned by its
  `go.sum`; the image pin serves `scripts/contract-diff.sh`); test-only pins (`golang-race`,
  `wiremock`, `schemathesis`) and the durable services' pins.
- **Rationale**: FR-001 and FR-002 ask for an identity that is the same for every build of the
  same source and that changes with any part of the method. Hashing the declared inputs is what
  input-addressed builds do; it needs no image built, so the engine's tests, CI and the
  developer's host compute the same value (E1); paths and file bytes are all it reads, so no build
  time, folder or host enters it. The engine's module is taken whole (spec, Clarifications):
  splitting it by package would need a second declaration of which packages judge, kept in step
  with the code by hand.
- **Alternatives**:
  - *A version bump the build checks* (T136's first option): a hand-set version in
    `pattern.yaml` and a check that fails when a method file changes without it. It still needs the
    same file list to know a bump is due, and a person to pick the new number; the digest is what
    the check would compare anyway (Principle XVII, "Derive the rest").
  - *The images' fingerprints* (S7): exact for what runs, but known only after every image is
    built; the engine cannot carry its own; and the engine's Go tests, which judge reuse, run
    without images.
  - *Git tree IDs* (`git rev-parse HEAD:src/engine`): Git's own content hashes, but per folder
    only, with tests and documentation inside, and blind to uncommitted changes a local build
    ships.
  - *Go's build ID*: stripped from our binaries (`-buildid=`), and it covers only Go code.
- **Limits**: a change that does not change what is produced, such as a refactoring of the
  engine's API code, still changes the identity and costs one more model call per step on the next
  run (spec, Assumptions). A symbolic link among the declared files is an error (exit 2): none
  exists today, and hashing either the link or its target would be a choice the caller cannot see.
  A tracked file deleted from the working tree is not listed (it is not a regular file).

## R2. How the identity reaches the engine and stays true

- **Decision** (owner, 2026-11-22, analyze round 1): the identity is **committed**, in
  `src/engine/method/method.sha256`, and checked, not generated at build time. One converging
  script, `scripts/method-digest.py`, computes it from the declaration and writes the file when it
  differs; run again it changes nothing; `--check` changes nothing and exits 1 naming the stored and
  the computed identity when they differ or the file is missing. A new test of the `py-tests`
  step, `scripts/tests/test_method_identity_current.py`, runs `--check` on the repository itself,
  so the CI run of a commit whose identity is stale fails (FR-010); `py_tests` runs on every
  change outside `specs/`, `.specify/` and `.claude/` (`.github/check-jobs.yaml`); after changing a method file the author runs the script and commits the new value with
  the change. The Go package `src/engine/method` embeds the file (`//go:embed method.sha256`) and
  parses it into a `method.ID` that rejects anything but 64 lowercase hex digits; the engine refuses
  to start without a valid one. `scripts/check.sh` and `src/deploy/build.sh` do not change.
- **Rationale**: Principle XVII "Derive the rest": the identity is derived from its declaration
  and checked against it, never kept by hand. Committed, it is present for every Go build, vet and
  lint on every path (CI jobs, the pre-commit hook's `go vet`, image builds, a developer's
  `go test`) with no new step in any of them, and the existing tests of `check.sh` and `build.sh`, which copy
  trees without the script or without `.git`, stand (analyze F2, F3; round 2 N1: a step in
  `check.sh`'s `go_checks` would break `test_test_reports.py` and `test_inline_schema.py`). It is
  a 65-byte value, not code; committing it is recorded as an exception to the Clean tree rule
  (plan, Exceptions). A separate script, rather than a step of `scripts/generate.sh`, keeps
  generate.sh to its one job, and leaves generate.sh's own tests as they are.
- **Alternatives**: gitignored and generated at every entry point (`check.sh --prepare`,
  `build.sh`, the lint and vet paths): every such path gains a step, and the existing tests of
  `check.sh --prepare` and `build.sh` change (the owner chose the committed value); a
  `generate.sh` step (its gen/ copy rule maps `gen/<path>` to `src/<path>`, which a digest is not,
  and its tests would change); an environment variable set by `ap` (the identity is a fact of the
  build, and a launcher could set a wrong one).
- **Limit**: a build from a tree whose committed identity is stale (a change not yet through the
  test command) carries the stale identity; the CI run of that commit fails, so it is not merged.

## R3. The reuse key and the run record

- **Decision**: `KeyParts` gains `Method`, written into the key after the round and only when it
  is not empty, as `Round` is (S1): a key without a method is unchanged, and the engine always has
  one (R2), so every key it computes now differs from every key recorded before (FR-008), and no
  existing test that computes a key without a method changes. The events that carry a `reuse_key`
  (`AttemptMade`, `StepAccepted`, `ResultReused`, and the `StepRejected` that leaves a review round
  open) gain `method`, the identity their key covers, so a reader can recompute the key from the
  event's own fields. No other event carries it (FR-004).
- **Durable runs**: the method enters the key on the engine's side, never from the workflow's
  input. The workflow still computes its key from the method-less parts, as today, and passes the
  parts to `LookupReuse`; an activity given an identity computes the key from the parts with it
  (ignoring the workflow's key) and returns `Key`, `Inputs` and `Method`. The workflow takes all
  three from the lookup whenever its `Key` is not empty, which widens today's branch for open
  findings (`workflow.go`, `key == "" && a.Key != ""`). A run resumed after an upgrade therefore
  looks up its remaining steps under the new identity (spec, Edge Cases), while its recorded input
  (S2) still names the pattern it started with. The activity calls, their order and their
  conditions do not change, so the recorded histories still replay; the test harness's fake
  `LookupReuse` returns an empty result, so the existing workflow tests keep today's keys and
  events (analyze F1).
- **Limits**: a lookup that fails (the store cannot be read) returns nothing, so the step keeps
  the method-less key, as today: its result is kept under a key no later lookup computes, so it is
  never reused (fail closed), and its events name no method. A step whose lookup was recorded
  before an upgrade replays that recorded result after it (spec, Edge Cases).
- **The non-durable driver** (`machine.Machine`, kept by feature 002) takes the identity in its
  `Config` and puts it in the same parts.
- **Rationale**: FR-003, FR-004, FR-008; Principle XX. The accepted-result store is rebuilt from
  the recorded keys (S3), so a key that covers the method is all that reuse needs; no store
  migration.
- **Alternatives**: the workflow computes no key at all (analyze F1: the fake lookup would leave
  every event without its schema-required `reuse_key`, and a failed lookup would skip `PutResult`,
  changing the workflow's commands); a separate comparison of a stored method beside the key (two
  rules where the key already is one); stamping the method in `RecordEvent` (it could name a
  different identity than the key's after a restart between the lookup and the record).

## R4. A slice's check results

- **Decision**: a slice record stores `checks_method`, the identity its `checks` were produced
  under. The slice service is given the engine's identity. Every path that reads a slice (`find`,
  `List`, and `Create`'s offer of an existing approved slice, `approvedFor`) treats a record whose
  `checks_method` differs from it (an older record has none) as stale: the service runs every
  check again (part checks, compatibility, lint) outside its lock and under the checks' own time
  limits, as `replace` does, applies the approval rule as after any check (S4), and stores the
  record with `checks_method` set before it answers. A stale approved slice is offered by `Create` only once re-checked and still
  approved. How: each public entry that reads a slice (`Get`, `List`, `Create`, and every
  operation through `find`) first calls a step that, under the lock, collects the stale records it
  will read, then, without the lock, re-checks each under that slice's `changing` mutex (the one
  `replace` holds, so a re-check and a contract change of one slice never interleave). Holding
  that mutex it re-reads the record and tests again whether it is stale (a reader queued behind
  another's re-check, or behind `replace`, which checks under the current method, finds it current
  and runs nothing); it then runs the checks on a copy of the record's parts, with
  `context.Background()` and the checks' own time limits, and, under the lock again, applies the
  results onto the record as it is then, as `replace` does for a contract change: when the parts
  it checked (the `parts` hashes and the contract baseline) are unchanged, the results, the
  approval rule and `checks_method` are applied and stored, and an approval or answer that landed
  meanwhile is kept and judged by the approval rule; when an answer changed the parts meanwhile,
  that change ran its checks under the current method (every check run sets `checks_method`), so
  the record is already current. `find` keeps its contract (the caller holds the lock). A
  check that could not run is a failed check with its reason, as today, and leaves
  `checks_method` unset, so the next read checks again (analyze F12). A store write that fails
  leaves the record stale: the answer shows the new results, and the next read checks again. The
  slice view (`getSlice`) gains `checks_method`; the list returns summaries, whose state follows
  the new results.
- **Rationale**: FR-005 to FR-008. Checking on first use, rather than while the engine loads its
  store, runs the checks once the lint service is up; these three paths are every way to a slice
  (S4). Outside the lock, a slow lint does not hold other slices, and the signatures of `Get`,
  `List` and the rest stay.
- **Alternatives**: re-check at `load` (the lint service may not be up when the engine starts, so
  every contract check would fail "could not run"); record the pattern's `id@version` beside the
  checks, as T144 first proposed (S5 shows that version does not follow the pattern's changes).
- **Limit**: the first read of each slice after an upgrade waits for its checks (a lint and a
  comparison call); one developer holds a few slices.

## Analyze round 1 (fresh Sonnet, at 4fd7522)

3 CRITICAL, 6 HIGH, 6 MEDIUM, 4 LOW, fixed at their source:

- F1 (the workflow computing no key breaks existing tests and the failed-lookup path) → R3: the
  workflow keeps its key and takes the lookup's. F2, F3 (generating the identity in
  `check.sh --prepare` and `build.sh` breaks their tests) → R2: committed and checked (owner).
- F4 (the template's generator and the lint tools missing) → R1: `oapi-codegen` pin and
  `scripts/tools/` lock files; the `oasdiff` pin left out with its reason. F5 (no test for the
  check step) → tasks T003. F6 (the arch lint's component map) → plan and T001. F7 (the workflow
  test helpers live in `machine`) → T010 moved. F8 (re-check under the lock, no context) and F9
  (`Create`'s `approvedFor`) → R4.
- F10 (`listSlices` returns summaries) → R4, contract, T014. F11 (which events carry `method`) →
  spec FR-004, SC-003, R3. F12 (a check that could not run stays failed) → R4. F13
  (`go.work.sum`) → R1 left out. F14 (FR-010 claimed a failing build) → spec FR-010, R2. F15 (a
  step in flight across an upgrade) → spec edge case, R3, ADR 0080.
- F16 (glob case, `README.md` basename, links) → R1, contract. F17 (`compose.yaml`,
  `generate.sh`, `.dockerignore`) → R1. F18 (no case for a pin left out) → T002. F19 (status
  words) → closed: "Draft" is the spec template's status, and `status: draft` with `decision:
  accepted` is every ADR's front matter in this knowledge bundle.

## Analyze round 2 (fresh Sonnet, at b58ef1e)

0 CRITICAL, 1 HIGH, 3 MEDIUM, 2 LOW; round 1: 18 closed, F6 partly (N2). Fixed at the source:

- N1 (HIGH: the identity check in `check.sh`'s `go_checks` breaks two existing tests that copy
  `check.sh` without the script) → R2: a new test in the `py-tests` step checks the real tree;
  `check.sh` does not change.
- N2 (`api`'s tests take a `method.ID`) → `api` among the arch lint's `method` consumers (T001).
- N3 (how a re-check runs outside the lock) → R4 "How".
- N4 (FR-004 and SC-003 missed the failed-lookup limit) → spec.
- N5 (a task reference) → fixed. N6 (production builds no `machine.Machine`) → T006.

## Analyze round 3 (fresh Sonnet, at ab929e1)

N1, N2, N4, N5, N6 closed; N3 partly: M1 (MEDIUM, no block) R4 "How" left undefined what "the
record is still the one it checked" compares, and `Approve`, `Answer` and `CheckApprove` change a
record without the `changing` mutex → R4 "How" now re-tests staleness after the mutex, compares
the checked parts, and applies the results onto the record as it is then; T013 gains the case.
0 CRITICAL, 0 HIGH: the gate is passed. The constitution's three rounds are used; the M1 wording
fix is not reviewed again by an agent.

## E1. The identity is reproducible and selective (2026-11-22)

A prototype of R1's computation (Python, `fnmatch` globs, the first declaration of R1, before analyze round 1) over this
repository at `e4348bf`:

| Tree | Files | Identity (first 12 hex digits) |
|------|-------|--------------------------------|
| the working tree | 120 | `403eee018e41` |
| a fresh `git clone` into another folder | 120 | `403eee018e41` |
| the clone, a line added to `src/engine/README.md` and to `src/cli/contract.md` | 120 | `403eee018e41` |
| the clone, a comment added to `src/engine/checks/shared.go` | 120 | `09b65ebdcfae` |

It took 0.04 s. Same source in another folder gives the same identity, documentation and the
terminal client do not change it, and a change to a check does (SC-002).

## Implementation findings

- **I1 (T008, CI run 45032308393, arch job)**: the `method` package's external tests import it,
  so the arch file lets `method` depend on itself, as every other component with external tests
  does (370516f).
- **I2 (T008, CI run 45033385634, py-tests job, `test_eval_suites_declaration`)**: the analyst
  eval suite reaches every package its harness imports, and the slice service then imported
  `method`, whose embedded `method.sha256` changes with any engine file. Declaring it would make
  the analyst suite pending, and so a paid eval run due, on every engine change. *Owner,
  2026-11-22*: `slices.Config.Method` is the identity's hex string, parsed once by the engine's
  start (`method.Current`); the stored `checks_method` is checked with the contract's hash rule
  (`slice-defs.schema.json` `hash`, the type `slice.schema.json` gives it); `slices` no longer
  depends on `method` (arch file); the committed test `method_start_test.go` compares the string
  (owner-approved change). `machine`, `durable` and `api` keep `method.ID`; no eval suite reads
  them.
