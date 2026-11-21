# Implementation Plan: Eval graders judge edge cases, not only the examples

**Branch**: `004-edge-case-graders` | **Date**: 2026-11-21 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/004-edge-case-graders/spec.md` (clarified
2026-11-21)

## Summary

The step agents' evals get two graders. `HeldOutTestsPass` judges every code case: six tests of
the slice's rules on inputs no example shows, kept in the evals' test data and run against a copy
of the agent's code (research R2). `MutantsKilled` judges every tests case: the agent's tests must
pass against the reference implementation and fail against each of eight hand-planted mutants,
one-line bugs that each break a rule on an input an example exercises (R1, R4). Both build every
variant first, without running model-written code and with a build cache shared only by the
builds, then run each test binary alone in its own throwaway grading container, so no run can
sway another; they run only when the trial's earlier graders passed (R3). Their results carry counts and
names, which the evals summary shows per case and trial (R5). Both prove they can fail with no
model. No mutation tool is adopted: none plants user-defined mutants (Principle VI evaluation).

## Technical Context

**Language/Version**: Go 1.26 (the evals harness, as feature 001); Python 3 (the summary script)

**Primary Dependencies**: none added; the go toolchain's `go test -c`, `-overlay` and
`go tool test2json`; the evals' `DockerRunner` and the step agent image

**Storage**: none; test data under `src/tests/evals/steps/testdata/`, results in the eval
`report.json` ([data model](data-model.md))

**Testing**: the grader self-tests (`run.sh graders`, CI's `go` job, no model); Python
`unittest` for the summary (`scripts/tests`); one live evals run with the owner's go-ahead

**Target Platform**: as feature 001: Linux with Docker; CI's `go` job and the evals workflow

**Project Type**: test harness of the existing platform

**Performance Goals**: grader self-tests at most 5 minutes longer (SC-004, owner 2026-11-21; estimated about 3.5, research S8); a live tests or code
trial at most 2 minutes longer to grade (SC-005), measured (research R3)

**Constraints**: no model call in a grader; held-out tests and mutants unreachable from the
agent's container (FR-004, FR-009); grading containers as today's (no network, read-only, no
capabilities); no coverage gate (FR-014)

**Scale/Scope**: one example slice; 6 held-out tests, 8 mutants; 5 tests cases and 6 code cases,
3 trials each

## Constitution Check

*Evaluated before research and again after design: PASS. Feature 001's approved exceptions
still apply to the platform; this feature adds none.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Needs before solutions | Pass | Serves N-013 (owner set `yes`, 2026-11-21) |
| II. Two planes | Not affected | Test harness only |
| III. API-first | Not affected | No management interface |
| IV. Asynchronous operations | Not affected | |
| V. Zero-trust identity | Not affected | No new component or call |
| VI. Adopt mature OSS | Pass | gremlins and go-mutesting evaluated (research, Principle VI evaluation); hand-planted mutants justified there; no dependency added |
| VII. Pluggable providers | Not affected | |
| VIII. Containers and sandboxing | Pass | Model-written code is built and run only in throwaway grading containers with no network, read-only root and project, no capabilities; each binary in its own (R3) |
| IX. Knowledge stays true | Pass | ADRs 0075, 0076 and 0077; reference `go-mutation-testing-tools`; the evals README; glossary terms (spec) |
| X. No silent loss | Not affected | |
| XI. Agent data privacy | Not affected | Fixtures only; no tenant data |
| XII. Tenant isolation | Not applicable | One developer (feature 001 FR-021) |
| XIII. Secure supply chain | Pass | No new dependency, image or tool |
| XIV. Safe defaults | Pass | A grader that cannot run its variants fails, never passes (R4) |
| XV. Compatible public APIs | Not affected | The report and summary are internal; the report grows additively |
| XVI. Human control | Pass | Changed existing tests carry `Tests-approved-by:` (T180's approval); an instruction change (FR-019) is reviewed and evaluated like code |
| XVII. Declare, derive, converge | Pass | Each planted bug has one home, the mutant table, which the review cases also read (R1) |
| XVIII. Abstractions say what | Pass | Two graders, named for what they judge; their results say what was examined and what failed |
| XIX. Evals | Pass | This feature strengthens the regression suite; an instruction change it makes passes that suite before merge; deterministic graders only |
| XX. Deterministic acceptance | Not affected | Engine checks unchanged |
| Checks prove they can fail | Pass | Each grader has a clean fixture and a planted violation, with no model (FR-011) |
| Existing tests change only with approval | Pass, by task | T180's approval covers `src/tests/evals/steps`; each commit carries the trailer; the summary's existing tests stay unchanged (R5) |
| Fresh review | Pass | A fresh read-only reviewer per story and before done, judging the CI run |
| Layout, Clean tree, Built artifacts | Pass | Everything under `src/tests` and `scripts/`; no image built from it |

## Decisions

Each decision that outlives this feature has a record in `docs/decisions/`, linked back here.

| Decision | Research | Record |
|----------|----------|--------|
| Eval mutants are planted by hand, one declared table per slice that the review plants share, not generated by a mutation tool | R1, Principle VI evaluation | [ADR 0075](../../docs/decisions/0075-eval-mutants-planted-by-hand.md) |
| A grader that runs model-written code against several variants builds them all first and runs each test binary alone in its own grading container; a mutant counts as killed only against a passing baseline | R3, R4 | [ADR 0076](../../docs/decisions/0076-variant-graders-build-first-run-each-alone.md), its mechanics superseded by [ADR 0077](../../docs/decisions/0077-variant-builds-share-a-cache-runs-stay-alone.md) (analyze round 1) |

## Threat model

New elements: the held-out tests and the mutant table (trusted fixtures), the scratch copies of
the agent's project, the build container and the per-binary run containers. Trust boundary: the
agent's files (model output) entering a grading container, as today. No new model, tool,
external interface or agent context.

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| Held-out tests and mutants | | ✓ | | ✓ | | | ASI01 goal manipulation (gaming the grader) | Under `testdata/`, outside every folder a trial's agent container mounts; never in the message (FR-004, FR-009); an injection case cannot reach them |
| Scratch copy of the agent's project | | ✓ | | | | | LLM05 improper output handling | The agent's project is copied, never changed; the copy is mounted read-only into every grading container |
| Build container | | ✓ | | | ✓ | ✓ | LLM05, ASI05 unexpected code execution | Compiling runs no agent code (no `go generate`, `CGO_ENABLED=0`, `-vet=off`); no network, read-only root, no capabilities, memory and PID limits (`DockerRunner`); the writable build-cache and binaries folders are per trial and removed after the trial; the cache is never mounted into a run container, and a run container mounts only its own variant's random-named binaries folder, read-only (research R3); their size is bounded by one small project's build (accepted, as the evals' temp folders are) |
| Run containers | | ✓ | | | ✓ | ✓ | ASI05, LLM10 unbounded consumption | One fresh container per binary, so a run cannot leave a file or cache entry for the next; time limit `checks.DefaultGoTimeout`; a time-out is not a pass or a kill |
| Grader verdict | ✓ | ✓ | | | | | ASI09 human-agent trust exploitation | A mutant is killed only after the same tests pass on the reference implementation (R4); an agent's answer is never read by these graders |

## Project Structure

### Documentation (this feature)

```text
specs/004-edge-case-graders/
├── plan.md              # this file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── grader-results.md
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
src/tests/evals/steps/
├── mutants.go                 # new: the mutant table (plantR2, plantR3 move here)
├── variants.go                # new: build every variant, run each binary alone, read test2json
├── grade_heldout.go           # new: HeldOutTestsPass
├── grade_mutants.go           # new: MutantsKilled
├── grade.go                   # Result.Counts; GradeTests and GradeCode add the new graders
├── cases.go                   # the review cases take their plants from mutants.go
├── sandbox.go                 # DockerRunner: a build cache folder for build commands; the run of one binary (project and binaries read-only, working dir its package)
├── trial_mounts.go            # new: the trial agent container's mounts, read by the harness and a self-test
├── grade_heldout_test.go      # new self-tests: reference passes, fitted implementation fails
├── grade_mutants_test.go      # new self-tests: reference tests kill all, weak tests leave survivors; table checks
├── README.md                  # the graders, held-out tests, mutants, the report's counts
└── testdata/
    ├── heldout/heldout_test.go        # new: the held-out tests
    └── planted/                        # new: the fitted implementation and the weak test set's edit
scripts/ci-evals-summary.py    # the two columns and the failure lines
scripts/tests/test_ci_evals_summary_counts.py   # new
```

**Structure Decision**: the change lives in the step evals' package and the summary script. The
engine, the agents' definitions (unless FR-019 applies), the analyst evals and CI's workflows do
not change.

**Existing tests expected to change**: none. `plantR2` and `plantR3` keep their names when they
move into the mutant table, and the reference fixtures pass the new graders, so `grade_test.go`'s
clean cases stay green unchanged. Should one need to change, T180's approval covers
`src/tests/evals/steps` (each commit with `Tests-approved-by:`); the summary's existing tests in
`scripts/tests/` stay unchanged (research R5).

## Exceptions

None.

## Complexity Tracking

No violation.
