# Implementation Plan: A proven slice becomes a pattern that builds without a model

**Branch**: `011-capture-pattern` | **Date**: 2026-11-25 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/011-capture-pattern/spec.md` (needs N-052, N-050,
N-051, N-053)

## Summary

A run that ended done is captured as a captured pattern: its base pattern with each agent step's
worker replaced by the fixed procedure `fill`, whose template is the run's accepted `tests`,
`code` and `witnesses` outputs, and whose parameters are names (in three case forms, longest
first), declared values and other literal values of the source slice (research R1, R4). `gen/**` is derived again from
each slice's contract by the generate procedure. The engine renders a filled step's files and the
project worker writes them with a new `writeStep` call; the base pattern's checks judge them,
except the two that read review findings (R1, R2). A slice fits a captured pattern when filling
the source slice with one value per parameter gives exactly the approved slice (R4). A proof is a
run of the captured pattern's copy of the source slice under the captured pattern with its own
values: an ordinary build of its proof slice (an approved slice made from that copy) into a
project folder the developer names, whose outputs must equal the source run's, kept with the
captured pattern; the developer approves after a passing proof, and a captured pattern is used only
while its latest proof passed under the current method (R5). `startRun` uses the newest usable fitting
captured pattern unless the request names another (R6). Captured patterns live in the engine
store, behind a new `/captured-patterns` resource and new terminal commands (R3, R7).

## Technical Context

**Language/Version**: Go 1.26 (engine, check program), POSIX shell (`ap`)

**Primary Dependencies**: none added; the engine's existing `go.yaml.in/yaml/v3`, the content-
addressed store and Temporal workflows

**Storage**: the engine store gains captured-pattern records; the content-addressed store gains a
second holder for template artifacts (research R3)

**Testing**: Go tests in `src/engine` (`captured`, `api`, `durable`, `checksvc`) and `src/cli`, by
a separate test author in new files; a new acceptance suite on the Compose stack with the recorded
model (CI); CI's check run of the pushed commit as the full test command; the analyst and steps
evals once before merge (method change; owner's go-ahead)

**Target Platform**: the local platform (Compose), as feature 001

**Project Type**: engine (new package, API resource, run choice), check program (`writeStep`),
terminal client, contracts

**Performance Goals**: a captured run makes no model call (SC-001); fit is linear in the slice's
text per captured pattern

**Constraints**: the base pattern, its template, agents and recordings are unchanged; contract
changes are additive except the new enum values (ADR 0100); existing tests change only as the owner approves; the Run workflow's new
commands (the `fill` path, the new `RunStarted` fields) sit behind a `workflow.GetVersion` bump
so runs in flight replay on the code that started them
([ADR 0067](../../docs/decisions/0067-a-run-is-one-temporal-workflow.md))

**Scale/Scope**: one engine package, one resource with eight calls, one check-contract call, one
field on `startRun`, eight terminal commands, one acceptance suite

## Constitution Check

*Evaluated before research and again after design: PASS.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Needs before solutions | Pass | Serves N-052, N-050, N-051, N-053 (spec) |
| II. Two planes | Pass | No new component; the engine and project worker keep their planes; no run content leaves them |
| III. API-first | Pass | Captured patterns enter and change only through the engine API; a declaration valid against `pattern.schema.json`; desired state (approved) and observed state (usable under the current method) are separate (data model); the parameter limits and name rule are declared in the contract and a contract test proves `engine/captured` matches them (research R4) |
| IV. Asynchronous operations | Pass | Capture, parameters, proof, approval, withdrawal and erasure are operations (FR-014) |
| V. Identity | Pass | No new caller; `writeStep` is called by the engine with its existing token, authorised as `commitStep`; a proof is a build of its proof slice, on that slice's step workers and grants |
| VI. Adopt mature OSS | Pass | gonew, Copier, Cookiecutter and `text/template` evaluated; none derives a template from an accepted project (research R4); the custom engine is justified there |
| VII. Providers | Pass | The rule that chooses between fitting patterns is specified (R6) |
| VIII. Containers | Pass | No new image |
| IX. Knowledge stays true | Pass | ADR 0090 to 0101, the reference "Project template tools", the architecture overview and the log at implementation |
| X. No silent loss | Pass | A captured run and a proof are ordinary runs: recorded before 202, one terminal state, checkpointed steps |
| XI. Privacy | Pass | A captured pattern holds the developer's slice and code like a slice does; erasing it is an operation; telemetry names IDs and hashes only |
| XII. Tenants | Not applicable | One developer by scope (feature 001 FR-021), as for slices; Complexity Tracking |
| XIII. Supply chain | Pass | No dependency |
| XIV. Safe defaults | Pass | Unusable without a passing proof under the current method; values outside their form refused; a failed check hands the run over, never repaired by a model |
| XV. Compatible APIs | Exception | Additive resource and fields in the preview contracts; paginated list; Idempotency-Key on every state change; the new enum values are an owner-approved exception (Complexity Tracking, ADR 0100) |
| XVI. Human control | Pass | The developer approves; the owner merges |
| XVII. Declare, derive, converge | Pass | The captured declaration is derived from the base pattern; `gen/**` derived from the contract; one generic substitution engine |
| XVIII. Abstractions say what | Pass | `Propose`, `Fit`, `Fill`; `/captured-patterns`; each named for what it does |
| XIX. Evals | Pass | No agent, prompt or model change; the suites run once for the method change |
| XX. Deterministic acceptance | Pass | The feature is the principle's "a proven solution is captured as a deterministic asset"; acceptance by checks only |
| Values that check themselves | Pass | Parameter values parsed into their forms at the API before any substitution |
| Single owner for rules | Pass | `engine/captured` owns candidates, case forms, fit and fill |
| Checks prove they can fail | Pass | Fit and proof have clean and planted cases (SC-005's planted set) |
| Existing tests change only with approval | Pass | Each change listed below needs the owner's approval by name |
| Fresh review | Pass | A fresh read-only reviewer per story and before done, judging the CI run |
| Layout | Pass | Files under `src/`, `specs/`, `docs/` only |

## Decisions

Each decision that outlives the feature has a record in `docs/decisions/`.

| Decision | Research | Record |
|----------|----------|--------|
| A captured pattern is its base pattern with each agent step filled from the accepted files by the fixed procedure `fill`; generated code is derived again; the review step keeps only the checks that need no findings | R1, R2 | [ADR 0090](../../docs/decisions/0090-captured-pattern-fills-agent-steps-from-accepted-files.md) |
| Captured patterns are the developer's data in the engine store, behind `/captured-patterns`; their template artifacts are held apart from the source slice | R3, R7 | [ADR 0091](../../docs/decisions/0091-captured-patterns-kept-in-the-engine-store.md) |
| Parameters are names in three case forms and declared values; a slice fits when filling the source gives it exactly; one engine owns the rules | R4 | [ADR 0092](../../docs/decisions/0092-captured-pattern-fit-is-exact-substitution.md) |
| Any other literal value of the slice may be added as a parameter; a number matches only as a whole number | E1, R4 | [ADR 0094](../../docs/decisions/0094-any-literal-value-may-be-a-parameter.md) (supersedes ADR 0092 in part) |
| A captured pattern never changes (always `1.0.0`; a change is a new capture); a value keeps its parameter's kind; a name has at least two letters; a proof builds from the captured pattern's copy of the slice | R3, R4, R5 | [ADR 0095](../../docs/decisions/0095-captured-patterns-never-change-values-keep-their-kind.md) (supersedes ADR 0091 and ADR 0092 in part) |
| A captured pattern is used only while its latest proof passed under the current method, a run of its proof slice whose outputs equal the expected ones (ADR 0097); the newest usable fitting one is chosen unless the run names another | R5, R6 | [ADR 0093](../../docs/decisions/0093-captured-pattern-used-only-under-its-proven-method.md) |
| A proof's step workers are keyed by its captured pattern; a proof's run has no slice (superseded by ADR 0097) | R2, R5, S17 | [ADR 0096](../../docs/decisions/0096-proof-step-workers-keyed-by-captured-pattern.md) |
| A proof is an ordinary build of the captured pattern's proof slice, an approved slice made from its copy; the captured pattern keeps the expected output hashes | R2, R5, S17, S18 | [ADR 0097](../../docs/decisions/0097-proof-builds-a-slice-made-from-the-captured-copy.md) (supersedes ADR 0096) |
| A proof slice is marked (`Proof of <captured pattern>`, `proof_of`) and never reused; a proof reuses no result; resume and take-over keep the pattern; a replay follows the usable rule and a replayed proof is a new proof | R5 | [ADR 0098](../../docs/decisions/0098-proof-slice-marked-and-runs-keep-their-pattern.md) |
| The filled review step also declares AllTestsPass, so the witness tests it writes run against the filled code (T025 round 1, B1) | R1 | [ADR 0101](../../docs/decisions/0101-filled-review-step-runs-every-test.md) |
| A proof compares only the filled outputs (`tests`, `code`, `witnesses`) with the template; a hand-over fails it at once; template paths are filled like contents | R4, R5 | [ADR 0099](../../docs/decisions/0099-proof-compares-filled-outputs-and-paths-are-filled.md) |
| The engine's preview contract gains the new enum values in place, an exception to Principle XV | E2, R7 | [ADR 0100](../../docs/decisions/0100-preview-contract-gains-enum-values-in-place.md) |

## Threat model

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| Parameter values (from the approved slice the analyst model may have written) entering filled code | | ✓ | | | | ✓ | LLM01 prompt injection, LLM05 improper output handling, ASI05 unexpected code execution | Each value parsed into its parameter's kind before any substitution, at most 64 characters: a name `[a-z][a-z0-9]*`, a number without a sign, a string of letters, digits, `_`, `-` and spaces; a number slot takes only a number, a name slot only a name; no quote, backslash, newline or operator other than `-` can enter, and a string's `-` and spaces can change code only where the source string occurred; the filled project runs only in the check sandbox (gVisor, no egress) and must pass every check |
| Captured template (code a model wrote, accepted once) reused without review | | ✓ | | | | ✓ | ASI06 memory and context poisoning, LLM03 supply chain | Only from a run of a base pattern that ended done, whose review left no blocking finding (a run built from a captured pattern, or a proof, is refused: `RunNotCapturable`); the developer approves after a proof; withdrawal and erasure; every captured run is judged by the base checks, including DependenciesAllowed and DeploymentValuesConfigured |
| Fit judge (`engine/captured`) | | ✓ | | | ✓ | | ASI02 tool misuse (a slice crafted to fit and pick a pattern) | Exact equality only; a fit picks values, never code; limits on parameters and places; linear matching, no backtracking regular expressions |
| `/captured-patterns` and `writeStep` (new calls) | ✓ | ✓ | ✓ | | ✓ | ✓ | API Top 10 API1, API4, API5 | Same token, rate limit and Idempotency-Key as the engine's other calls; `writeStep` only from the engine's token and only into the step's writable paths; operations recorded in the run store |
| Method change after approval | | ✓ | | | | | ASI08 cascading failures (an old verification carried into new runs) | Usable only while its latest proof passed under the current method (FR-011) |
| Proof slice (made by the engine from the captured copy) | | ✓ | | | | ✓ | ASI06 memory and context poisoning | Made only from the captured pattern's own copy, approved by the engine at creation; a part changed afterwards no longer equals the copy, so its run is refused PatternDoesNotFit, and the next `proof-slice` restores it from the copy (owner, T039); its runs always use that captured pattern; erasing it only stops proofs until the next one makes it again |
| Approval and withdrawal | ✓ | | ✓ | | | ✓ | ASI09 human-agent trust exploitation, ASI03 identity abuse | Only the developer's own client token reaches the engine API; each approval and withdrawal is an operation recorded with its caller and time, and the approval names the proof it approved; an agent has no engine token |
| Fill output (`engine/captured`, the project worker's `writeStep`) | | | | | ✓ | | API4 resource consumption | At most 32 parameters, 10000 places each, values of at most 64 characters, so a filled file grows by a bounded amount; `writeStep` keeps the step's file and size limits; a filled path must match its step's writable entries and no two files may get the same path |

## Project Structure

### Documentation (this feature)

```text
specs/011-capture-pattern/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   └── captured-patterns.md
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
src/engine/api/preview/2026-11-12-preview/openapi.yaml   # /captured-patterns, startRun.pattern, error codes, x-limits
src/engine/api/preview/2026-11-12-preview/run-event.schema.json  # RunStarted pattern_reason, parameters, source_run, proof, not_used
src/engine/api/preview/2026-11-12-preview/slice-defs.schema.json # operation_target, parameter_name, parameter_value
src/engine/api/preview/2026-11-12-preview/slice.schema.json      # proof_of, history kind made-for-proof
src/engine/api/operations.go             # the new operation kinds
src/engine/api/proof_slice.go            # new: makeProofSlice (T029)
src/engine/api/captured_limits_contract_test.go  # new: the limits pinned to the contract (T005)
src/check/api/preview/2026-11-13-preview/openapi.yaml    # writeStep
src/engine/captured/                     # new: Propose, Check, Fit, Fill, the declaration from the base pattern
src/engine/store/                        # captured-pattern records, second artifact holder
src/engine/api/captured.go               # new: the resource and its operations
src/engine/api/runs.go                   # pattern choice at startRun (R6); part_hashes in newRun; replay
src/engine/machine/                      # RunStarted's new fields
src/engine/durable/                      # RunInput/RunStart: part_hashes, no reuse; workflow version bump
src/engine/durable/workflow.go, steps/   # the fill worker: render, writeStep, checks, commit
src/engine/checksvc/history.go, contract.go, handler.go  # writeStep
src/engine/machine/, src/engine/durable/  # machine.Writer (optional, by type assertion) and the WriteProject activity
src/engine/slices/                       # the proof slice, made from the captured copy and approved
src/deploy/ap                            # prove: proof-slice, then build
src/engine/method/method.sha256          # derived
src/cli/                                 # the commands; contract.md rows and the proof run's exit
src/page/                                # the run page shows RunStarted's new fields (T031, T032)
src/tests/acceptance/                    # new suite: capture, prove, approve, build tasks with no model call
```

**Structure Decision**: the substitution rules have one owner, `engine/captured`, used by the API
(proposal, parameters), the run choice (fit) and the workflow (fill); the project worker stays
the only writer of a project.

**Existing tests expected to change** (each needs the owner's approval by name before its
commit; any other is stopped and asked for):

- Tests that list the engine contract's paths, operation kinds or error codes exactly (found by
  the code author, then asked for).
- Tests that list `RunStarted`'s fields exactly, if any.
- Tests in `src/page` and `src/tests` that list the generated client's types or a run event's
  fields exactly, if any.
- Tests that list a slice record's or `Run`'s fields exactly, if any (a proof slice names its
  captured pattern; a run gains `proof` and the pattern choice); launcher tests that list the
  commands it intercepts (`prove` is new).

## Exceptions

| Rule | Location | Reason |
|------|----------|--------|
| Outside-in, test-first: acceptance tests are seen failing before the code | `src/tests/acceptance/captured_prove_test.go` (T036) | T028 wrote the `ap prove` cases in `src/tests/lab`, which no CI job runs (CI run 45819156622), so T029 and T009 landed before the cases ever ran; they move to the acceptance suite after the code, and pass or fail on T010's CI run. Recorded 2026-11-26 with the owner's choice |

## Complexity Tracking

| Violation | Why needed | Simpler alternative rejected because |
|-----------|------------|--------------------------------------|
| Principle XII: captured patterns have no tenant scope | One developer by scope (feature 001 FR-021) | Not applicable until the hosted version, as for slices (feature 001's row); approved by the owner 2026-11-26 (T002) |
| Principle XV: the engine contract's `2026-11-12-preview` gains 9 `ErrorCode` values, 6 `OperationKind` values and the slice history kind `made-for-proof` in place, which the pinned oasdiff reports as breaking (`response-property-enum-value-added`, research E2) | `getOperation` and `startRun` must return the new kinds and codes, so a separate problem schema (feature 002) does not fit; every client is generated from this contract in the same commit and prints any code | A new preview version needs a deprecation period, the engine serving two versions and a change to `scripts/contract-diff.sh`; extensible enums drop the generated Go constants across the modules. Approved by the owner 2026-11-26 ([ADR 0100](../../docs/decisions/0100-preview-contract-gains-enum-values-in-place.md)) |
