# Research: Eval graders judge edge cases, not only the examples

Phase 0 of the [plan](plan.md) for the [spec](spec.md). Each decision names its deciding source:
a cited primary source, or the code and fixtures of the step agents' evals as they stand on
`main` (`cb13b11`). Decisions marked *owner* were put to the project owner as direct questions.

## Sources

| ID | Source |
|----|--------|
| S1 | [go-gremlins/gremlins](https://api.github.com/repos/go-gremlins/gremlins) (Apache-2.0; v0.6.0, 2025-12-06; last commit 2026-03-30; 5 contributors in the last 12 months; 69 open issues and PRs); [usage, `unleash`](https://gremlins.dev/latest/usage/commands/unleash/) and [configuration](https://gremlins.dev/latest/usage/configuration/): generic operators only (arithmetic-base, conditionals-boundary, conditionals-negation, increment-decrement, invert-negatives on by default; others opt-in), files excluded by regex, no custom test command found, thresholds `--threshold-efficacy`; binaries with `checksums.txt`. [Scorecard](https://api.securityscorecards.dev/projects/github.com/go-gremlins/gremlins): no result (404). Read 2026-11-21 |
| S2 | [avito-tech/go-mutesting](https://api.github.com/repos/avito-tech/go-mutesting) (MIT; v2.3.1, 2025-12-26; last commit 2026-10-02; 26 open issues and PRs); [README](https://raw.githubusercontent.com/avito-tech/go-mutesting/master/README.md) and [`internal/models/report.go`](https://raw.githubusercontent.com/avito-tech/go-mutesting/master/internal/models/report.go): generic mutators, `--exec` replaces the test step, `--match` by function name, `report.json` with killed, escaped, timed-out and errored mutants, a `--blacklist` of mutant checksums that "change when the source changes"; no automatic handling of equivalent mutants; installed by `go install`, no signed release found. [Scorecard](https://api.securityscorecards.dev/projects/github.com/avito-tech/go-mutesting): no result (404). Read 2026-11-21 |
| S3 | [zimmski/go-mutesting](https://api.github.com/repos/zimmski/go-mutesting) (MIT; v1.2, 2021-06-10; no commit since 2021; avito-tech's is its fork); [Scorecard](https://api.securityscorecards.dev/projects/github.com/zimmski/go-mutesting) 2.3 (2026-10-26; Maintained 0, Code-Review 2). Read 2026-11-21 |
| S4 | [OpenSSF Concise Guide for Evaluating Open Source Software](https://best.openssf.org/Concise-Guide-for-Evaluating-Open-Source-Software.html) (2025-03-28): whether the dependency is needed at all, maintenance and releases, more than one maintainer, security practices and Scorecard results, licence and adoption, a trial in an isolated environment |
| S5 | [`cmd/go`](https://pkg.go.dev/cmd/go): `go test -c` compiles a test binary without running it; `-overlay` replaces or adds source files for one build by a JSON map; "Go generate is never run automatically by go build, go test, and so on"; with `CGO_ENABLED=0` no C toolchain runs. [`cmd/test2json`](https://pkg.go.dev/cmd/test2json): `go tool test2json -t <binary>` runs a test binary and reports each test's pass or fail as JSON events |
| S6 | The step agents' evals on `main` (`cb13b11`): [README](../../src/tests/evals/steps/README.md); `grade.go` (`GradeTests`, `GradeCode`, `Result`); `cases.go` (`plantR2`, `plantR3`, `reviewBefore`); `sandbox.go` (`DockerRunner`: no network, read-only project, read-only root, a fresh tmpfs `/tmp` per `go` command); `harness_evals_test.go` (the trial loop, `Trial`, `CaseReport`, `report.json`); `testdata/slice/` (rules R-1 to R-3, examples E-1 to E-4, the contract); `testdata/ref/` (reference tests and implementation); [`scripts/ci-evals-summary.py`](../../scripts/ci-evals-summary.py) and its test `scripts/tests/test_ci_evals_summary_quarantine.py` |
| S8 | Experiment on the owner's host (16 CPUs, Go 1.26, warm module and build cache), 2026-11-21, a temporary test in `src/tests/evals/steps` (removed afterwards): the generated notes project with the reference tests; `go test -c -vet=off -mod=vendor -overlay=<json> -o <dir>/ ./tests` with `internal/app/app.go` replaced by the reference implementation, then by two mutants (every id 1; ids from 0): 3.7 s for the first build, 1.21 s and 1.28 s for the next; each binary run through `go tool test2json -t <bin> -test.v=test2json -test.count=1` from `tests/` in 0.12 to 0.13 s; the reference variant reported 6 passes and 0 fails, the mutants 3 and 4 fails with exit status 1. Without `-test.v=test2json` passing tests emit no events (reported by the analyze reviewer, 2026-11-21) |
| S7 | Constitution Principle XIX and the Engineering Principle "Checks prove they can fail"; backlog T180 (owner, 2026-11-20); the spec's clarifications (owner, 2026-11-21) |
| S9 | CI measurements of this feature (GitHub-hosted runner, the `go` job's `evals/steps` package time): before, main `cb13b11` run 44899948184: 146.0 s; US1 (`HeldOutTestsPass`), run 44912154688 (`ba27fbb`): 160.0 s, run 44913952612 (`5f2732f`, green 26/26): 121.9 s. US2 (`MutantsKilled`), run 44921637628 (`8e00061`, green 26/26): 311.6 s, 165.6 s more than before. Host, 2026-11-21, a load average of 13 to 15 on 16 CPUs: `run.sh graders` 8 min 51 s against 3 min 44 s before. After US2, at a load of 12 to 16: 13 min 33 s. SC-004 holds in CI. Live evals run 44933201919 (`c6d323e`, 2026-11-21, steps tests, code and review, all green pass^3) against the last green steps run 44890878806: grading and harness time per trial (the `TestStepEvals` time less the agents' trial times) code 13.3 s before, 36.8 s after (+23.5 s); tests 41.2 s before, 87.5 s after (+46.3 s); SC-005's 2 minutes hold. Jobs: code 20 min 33 s (11 min 45 s before), tests 35 min 0 s (24 min 21 s), review 39 min 17 s (36 min 57 s, no new grader) |

## Principle VI evaluation: a mutation tool or hand-planted mutants

The spec needs mutants that each break one rule of the slice on an input an example exercises
(FR-006), that a faithful set of tests always kills (SC-003: the reference tests kill 100%), and
that each name the rule and the killing example.

| Option | Fit | Health (S4) | Verdict |
|--------|-----|-------------|---------|
| gremlins (S1) | Generic operators on any line; it cannot plant "the list comes back newest first" or "empty text is accepted", and it mutates lines no example reaches, so some mutants are equivalent or unkillable by tests derived from the examples; no custom test command found, so it cannot run only `./tests/...` | Active, 5 contributors, checksummed binaries; no Scorecard result | Does not meet FR-006 or SC-003 |
| avito-tech/go-mutesting (S2) | Generic mutators; `--exec` can run our command, but the mutants are still its own; equivalent mutants are excluded only by a checksum list that breaks on every source change | Active; no Scorecard result; no signed release | Does not meet FR-006 or SC-003 |
| zimmski/go-mutesting (S3) | As avito's | Unmaintained since 2021, Scorecard 2.3 | Rejected |
| Hand-planted mutants (S6) | Each mutant is a declared one-line change of the reference implementation, named by rule and example; the review cases already plant `plantR2` and `plantR3` this way | No new dependency | **Chosen** |

**Justification for custom code** (Principle VI): the mutants are test data, eight
one-line replacements (`R-1/newest-first` reverses the list in a one-line function literal), not a mutation engine; no tool plants user-defined mutants (S1 to S3). The
grader that runs them reuses the evals' own runner and the go toolchain (S5). A mutation tool
stays a candidate for a later need: measuring how much of a real project's code its tests check,
which is a different question from this eval's (whether the tests agent binds what the examples
say).

## Decisions

### R1. The mutants are planted by hand and declared beside the review plants

- **Decision**: one table of mutants in the step evals' package, each a `plantSpec` (rule, marker,
  the line replaced, its replacement, what it breaks) plus the example that kills it. `plantR2`
  and `plantR3` move into it and the review cases take them from it, so each planted bug has one
  home. The mutants (each kills a test set that leaves its outcome unchecked):

  | Mutant | Rule | Killed by | Planted bug |
  |--------|------|-----------|-------------|
  | `R-1/newest-first` | R-1 | E-1 | the list comes back newest first |
  | `R-1/drops-first` | R-1 | E-1 | the list leaves out the first note |
  | `R-2/from-last` (= `plantR2`) | R-2 | E-2 | the next id is read from the last note, which panics for the first |
  | `R-2/zero-based` | R-2 | E-2 | the first note gets id 0 |
  | `R-2/always-one` | R-2 | E-3 | every note gets id 1 |
  | `R-2/status-200` | R-2 | E-2 | a created note is answered 200, not 201 |
  | `R-3/first-byte` (= `plantR3`) | R-3 | E-4 | empty text is tested by reading its first byte, which panics |
  | `R-3/accepts-empty` | R-3 | E-4 | empty text is stored and answered 201 |

- **Rationale**: the Principle VI evaluation above; Principle XVII's "declare once": the review
  cases and the mutant grader read the same plants.
- **Alternatives**: a mutation tool (rejected above); a YAML declaration of the mutants (the
  evals' cases are already a Go table read by the harness, and a second format for the same kind
  of fact would split it).

### R2. Held-out tests: a Go test package kept in the evals' test data, added to a copy

- **Decision**: the held-out tests are one Go test file in `testdata/heldout/`, package
  `heldout_test`, using only the standard library, `example.com/service/gen` (`gen.Handler`) and
  `example.com/service/internal/app` (`app.New`), the seam the slice's tests use (FR-002). Each
  test's name starts with its rule (`TestR1_…`). The grader writes a scratch copy of the project
  the agent left, adds the file there as `heldout/heldout_test.go`, and runs it (R3); the agent's
  project is never changed. The tests:

  | Test | Rule | Input no example shows | Expected (from the rule or the contract) |
  |------|------|------------------------|------------------------------------------|
  | `TestR1_EmptyList` | R-1 | no notes | 200 and a JSON array with no item (the contract's `type: array`; `null` is not an array) |
  | `TestR1_ThreeInOrder` | R-1 | three notes | `a`, `b`, `c` in that order |
  | `TestR2_FiveIDs` | R-2 | five notes | ids 1 to 5 in creation order |
  | `TestR2_TextKept` | R-2, contract | a text with inner spaces and non-ASCII letters, no leading or trailing space | the created note and the listed note carry the text as sent |
  | `TestR3_RefusedNotListed` | R-3 | an empty text after one note | 400, and the list still holds only the first note |
  | `TestR3_RefusalIsAnError` | R-3, contract | an empty text | 400 with a JSON object whose `message` is a string |

  Not held out, because the rules do not fix the answer (FR-002): whether a refused note uses up
  an id, whether whitespace-only text is empty, concurrent requests.
- **Rationale**: Go ignores `testdata/` (S5), so the file is never built by the repository's own
  `go test ./...` or vet; it reaches no folder a trial's agent container mounts (`/projects/p` is
  the trial's project, `src/agents` the definitions; S6), meeting FR-004. A separate package keeps
  its names from clashing with the agent's own tests.
- **Alternatives**: godog scenarios in a second feature file (the code agent reads
  `features/`, and a held-out feature would have to stay out of the project anyway; plain Go tests
  are shorter and need no step definitions).

### R3. A grader that runs model-written code builds every variant first, then runs each alone

- **Decision**: both new graders work in two phases on a scratch copy of the agent's project:
  1. **Build**: `go test -c -vet=off -mod=vendor -o <bins>/<random>/ ./<pkg>/...` once per
     variant, each in its own throwaway grading container as today, with one build-cache folder
     per trial on the host mounted writable as `GOCACHE` for these build commands only (a
     `Cache` field on the runner value the build phase uses; the run phase's runner has none).
     The overlay JSON and each mutant's `app.go` are written inside the scratch copy, which every
     build container mounts read-only. The cache and binaries folders are removed after the trial.
     For
     `MutantsKilled` the variants are the reference implementation and each mutant, given as an
     `-overlay` that replaces `internal/app/app.go` (S5, S8), and `<pkg>` is `tests`; for
     `HeldOutTestsPass` the one variant is the agent's code, with `<pkg>` `heldout`. Compiling
     runs none of the agent's code (S5: no `go generate`, `CGO_ENABLED=0`), so a shared cache is
     safe during builds.
  2. **Run**: each test binary in its own fresh throwaway grading container (no network, read-only
     root, a fresh tmpfs, no build cache), with the project root and that variant's binaries folder mounted
     read-only and the working directory set to the binary's package folder (`<copy>/tests` or
     `<copy>/heldout`), so the slice's tests find `../features` as under `go test`. The command is
     `go tool test2json -t <binary> -test.v=test2json -test.count=1` (S5, S8), so every test's
     result is known by name. Each variant's binaries are written to a folder with a fresh
     random name, never the variant's index or name, a run container mounts only that one
     folder (never the folder holding every variant's), and the overlay files are removed before
     any run, so a test binary cannot tell from its own path, its siblings or the copy whether it
     runs against the baseline or a mutant (found by the T005 author, 2026-11-21). Accepted
     limits (analyze round 4, LOW): a binary's own modification time and the paths compiled into
     it differ between variants only by build order and random names, with no reference value
     visible to the test.
  3. **Fail fast**: a new grader runs only when every earlier grader of the trial passed; otherwise
     it fails with the reason `not run: <grader> failed` and no `counts`. The trial already fails,
     so its verdict is unchanged; this keeps grading time low for trials that are already red,
     above all the self-tests' planted violations.
- **Rationale**: running the variants in one container would let the agent's tests, run first
  against the reference, leave a file or a cache entry that makes every later variant fail, which
  would count every mutant as killed; only the run phase executes agent code, and there every
  binary is alone. A shared cache for builds only keeps one cold build per trial (S8: 3.7 s
  first, about 1.25 s for each further variant on a warm host).
- **Alternatives**: `go test` per variant in a cold container each (sound, about 9 cold builds
  per tests trial); one container for everything (fast, not sound); one binary that selects the
  mutant at run time from an environment variable or argument (one build, but the agent's tests
  could read the selector and fail on purpose).
- **Estimate** (S8, to be measured in T008, T012 and T018): host self-tests about 15 s per full
  `MutantsKilled` and 4 s per full `HeldOutTestsPass`; with fail fast, about 11 and 13 full runs
  in the self-tests, about 3.5 minutes added in all, within SC-004's 5 (owner, 2026-11-21). Live, one cold build (estimated under a
  minute on a CI runner) plus about 2 s per further variant and about 2 s per run container.

### R4. A mutant counts as killed only against a passing baseline

- **Decision**: `counts.total` is the fixed list (the held-out file's six tests, the table's eight
  mutants), never the events a run happened to report, so a test or mutant that never ran is
  named `(did not run)`. `MutantsKilled` first runs the agent's tests against the reference
  implementation. If they do not build or do not all pass there, the grader fails with that reason
  and judges no mutant (FR-008). A mutant is killed when its run reports a failed test or a
  panic, within the run's time limit (`checks.DefaultGoTimeout`, as every other grader); a run that
  times out or cannot start is reported as such and is not a kill. Every package under `tests/`
  is built; a mutant is killed when any of them fails. A binary that does not build for a mutant
  (cannot happen for a body-only change, but is reported if it does) is not a kill. Limit: the
  baseline runs once, so an agent test that is flaky could pass it and then fail a mutant by
  chance.
- **Rationale**: spec FR-008 and its edge cases; a kill must come from the planted bug alone.

### R5. Grader results carry counts and names; the summary shows them only when present

- **Decision**: `Result` gains an optional `counts` object: `total`, `passed` (held-out tests
  passed, or mutants killed) and `failed` (the names of the failed held-out tests, or the
  surviving mutants, with `timed out` or `did not run` after a name when that is why). The summary
  adds two columns, `Held-out` and `Mutants killed`, each listing a case's trials as
  `passed/total`, `n/a` for a case of another agent, and `—` when a trial reports no counts; then
  one line per trial with a failure, naming what failed. A report that has no `counts` anywhere
  prints exactly as today ([contract](contracts/grader-results.md)).
- **Rationale**: FR-012, FR-016, FR-017; keeping old reports' output unchanged leaves the existing
  summary tests (`scripts/tests/test_ci_evals_summary_quarantine.py`) as they are.

### R6. No coverage gate, and the engine is unchanged

- **Decision**: no grader reads a coverage percentage (FR-014); the engine's checks, the agents'
  definitions and the platform's runs do not change, except an agent's instruction if the first
  live run fails a new grader (FR-019, owner 2026-11-21).
- **Rationale**: T180 (e); the spec's assumptions.

### R7. Existing tests stay unchanged

- **Decision**: `plantR2` and `plantR3` keep their names as rows of the mutant table, and the
  reference fixtures pass both new graders, so the existing self-tests (`grade_test.go`) and the
  summary's tests stay as they are; the new self-tests go in new files. Should an existing test
  in `src/tests/evals/steps` still need to change, T180's approval covers it, each commit with
  `Tests-approved-by:`.
- **Rationale**: the constitution's "Existing tests change only with the owner's approval"; T180's
  approval (owner, 2026-11-20).
