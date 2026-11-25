# Research: A proven slice becomes a pattern that builds without a model

Phase 0 of the [plan](plan.md) for the [spec](spec.md). Each decision names its deciding source:
the code on branch `011-capture-pattern` at `527857c` (the same as `main` `0bd5f11` outside
`specs/` and `docs/`), a reproduced experiment, or a cited document. Decisions marked *owner* were
put to the project owner as direct questions on 2026-11-25 (spec, Clarifications).

## Sources

| ID | Source |
|----|--------|
| S1 | `src/engine/cmd/engine/setup.go:37` embeds one pattern (`gen/catalogue/api-feature-go/pattern.yaml`); `slices.Service` holds one `pattern []byte` (`slices/service.go:26`, `:95`); `api/runs.go:255-274` builds the run's `machine.Pattern` from it. There is no catalogue endpoint, list or chooser |
| S2 | `src/engine/checks/fit.go:49` `Fit(pattern, slice)`: runs the pattern's named `conditions`, an unknown name fails closed (`:66`); evaluated at contract approval (`slices/service.go:396`) and at `startRun` (`api/runs.go:732`), refusing with `PatternDoesNotFit` |
| S3 | `src/engine/durable/workflow.go:149` `steps()` runs the pattern's steps in order and ends `RunCompleted` done; nothing requires an agent step. The review step is recognised by listing the check NoBlockingFindingOpen (`workflow.go:154`, `machine/drive.go:31`). `steps/steps.go:232` dispatches a step by its worker's kind, `agent:` or `procedure:` |
| S4 | A step's accepted outputs are framed file sets (`canonical.Framed`) in the content-addressed store (`store/store.go:240` `PutArtifact`, `:291` `Artifact`), named by hash in the run's `StepAccepted` events; api-feature-go's outputs are `contract`, `examples`, `generated-code`, `tests`, `code` and the review's `witnesses`. Erasing a slice removes the artifacts it alone holds (`store.go:319`, `heldElsewhere` `:357-364`) |
| S5 | The project worker (`checksvc/handler.go:118-119`, `/project-versions/commit` and `/restore`; `steps/history.go:38`, `:57`) is the only writer of a project folder besides the step containers; a reused step's stored files are written by `commitStep` with no worker call (`workflow.go:389-413`) |
| S6 | `src/engine/method/method.json` and `method.go`: the method identity covers `src/catalogue`, `src/agents`, `src/engine`, `src/check`, `src/slice`, `src/retry`, the Dockerfiles, `scripts/tools` and the pins; the reuse key ends with it (`machine/reuse.go:22-46`) and includes the pattern's `id@version` (`:14`) ([ADR 0079](../../docs/decisions/0079-method-identity-is-a-digest-of-declared-sources.md), [ADR 0080](../../docs/decisions/0080-results-reused-and-checks-kept-only-under-their-method.md)) |
| S7 | `src/engine/api/operations.go:137` maps operation IDs to `OperationKind` (contract `openapi.yaml:1460`); the generic `Operation` workflow (`durable/operation.go:53`) runs any job ([ADR 0068](../../docs/decisions/0068-operations-recorded-then-run-as-workflows.md)) |
| S8 | `src/catalogue/api-feature-go/pattern.yaml`: steps contract-check, generate (procedures), tests, code, review (agents) and each step's checks; no check calls a model (the agents are in the workers) |
| S9 | `src/deploy/ap` `read_pattern` (line 1091): `build-workers` reads api-feature-go's steps and writable entries from the repository and starts one container per step |
| S10 | `src/engine/api/preview/2026-11-12-preview/slice-defs.schema.json` `pattern_id`: `^[a-z0-9-]+$`; `pattern.schema.json` has `additionalProperties: false` |
| S11 | [gonew](https://pkg.go.dev/golang.org/x/tools/cmd/gonew) (v0.53.0, read 2026-11-25): "starts a new Go module by copying a template module", changing only its module path; "highly experimental and subject to change"; BSD-3-Clause |
| S12 | [Copier](https://copier.readthedocs.io/en/stable/) (read 2026-11-25): renders hand-written Jinja templates (placeholders in names and contents, questions in `copier.yml`, an answers file); Python 3.10+; nothing on deriving a template from an existing project or on case forms |
| S13 | [Cookiecutter](https://cookiecutter.readthedocs.io/en/stable/) (read 2026-11-25): "creates projects from cookiecutters (project templates)", Jinja2, Python; nothing on deriving a template or case forms |
| S14 | [text/template](https://pkg.go.dev/text/template) (Go 1.26 standard library): data-driven templates with `{{...}}` actions; assumes trusted template authors, no escaping |
| S15 | Experiment E1 (name forms in the evals' accepted notes project) |
| S17 | Step workers are per slice: a worker token's grant names one slice and one step (`api/workers.go:29` `WorkerGrant{Slice, Step, CallToken}`, grant files `:81`); `Workers` maps slice to step to endpoint (`:131`); registerWorker refuses a slice that does not exist (`:273`); `startRun` refuses WorkersNotReady while a step has no live worker for the slice (`:250`, contract `startRun`); `ap build-workers <slice> <project>` asks the engine for the slice, makes the project folder and starts and registers one container per step (`src/deploy/ap:1362`, `src/cli/contract.md`) |
| S18 | Runs are found by slice (analyze round 3): run records and their events are stored per slice (`store/records.go:157`, `store/store.go:76`); findings, reuse lookups and worker and check calls take the slice (`durable/activities.go:112`, `:238`); resume, replay and take-over read `run.Slice` (`api/runs.go:806`, `:848`, `:869`); `Run.slice` and `Worker.slice` are required in the engine contract; the launcher refuses a listed run without a slice (`src/deploy/ap` `refuse_active_run`, `read_run`); `deleteSlice` erases the slice's run records |
| S16 | Constitution: Principle VI (OSS first; custom code needs a written justification), XII (every store query scoped by tenant), XIV (fail closed), XVII (one generic engine per kind of thing), XX (a proven solution is captured as a deterministic asset and reused); Engineering Principles "Values that check themselves", "Single owner for rules" |

## E1. Name forms in an accepted project

Over the evals' clean reference (`src/tests/evals/steps/testdata/ref`: the tests and code a run of
the notes slice accepts) and its slice (`testdata/slice`), every word holding `note` in any case:
`notes` 25, `note` 18, `Note` 6, `Notes` 3, `NewNote` 3, `ListNotes` 2, `CreateNote` 2,
`listNotes` 1, `createNote` 1. No `NOTE`, no `note_id`, no other spelling.

Every occurrence is one of the three case forms of `note` or `notes`, alone or inside a word
(`NewNote`, `listNotes`), so two name parameters (`note`, `notes`), each in three case forms,
matched longest first, cover them all (FR-003).

Corrected in analyze round 1 (C1): this slice declares no limit. Its `200`, `201` and `400` are
response statuses (`slice/contract.yaml:11`, `:29`), and its numbers in the examples are ids and
statuses; a literal parameter on one of them would replace every status of that number too. A
declared limit and a value derived from it in the examples (a text one character over the limit)
are the case of feature 009's planted contract (`src/tests/acceptance/planted_values_test.go`,
`maxLength: 200`); that case is why the developer may add any literal value as a parameter
(*owner*, spec Clarifications), and SC-006 is proven on a unit fixture that declares a limit.

## R1. What a captured pattern is (FR-002, FR-007, FR-009)

**Decision**: a captured pattern is its base pattern with each agent step's worker replaced by the
fixed procedure `fill`, which writes that step's accepted files from the template with the
parameters' values filled in. Step names, order, writable paths, dependencies, limits and checks
stay the base pattern's, except that the review step drops FindingsCarryEvidence and
NoBlockingFindingOpen, which judge findings no agent writes, and so is no longer a review step
(S3); its outputs are `witnesses` only, since `findings` is not a file set; and every `fill` step
has one attempt, since filling is deterministic and nothing may repair it (analyze R1). The
pattern schema sets attempts for a whole pattern (`limits.attempts`; steps take no attempts of
their own), so the captured declaration has `limits.attempts` 1 and `limits.review_rounds` 1 for
every step, the procedure steps included, which are deterministic too (analyze round 5, F14). The template is the source run's `tests`, `code` and `witnesses` outputs (S4); `gen/**` and
the module files are not captured: the generate procedure derives them again from the new
slice's contract, as for any slice, and GenerationRepeatable, GeneratedCodeBuilds and
GeneratedCodeUnchanged judge them as before.

**Rationale**: it reuses every step boundary and check the source run passed, so the same scope
rules (ScopeKept, TestsUnchanged, GeneratedCodeUnchanged) and the same tests (EveryExampleBound,
TestsFailAgainstStub, AllTestsPass) decide a filled build as they decided the agent's; nothing in
the run machine needs an agent step (S3). Regenerating `gen/**` keeps one owner for the generated
code, and oapi-codegen's own naming of the new contract's names is then judged by the build and
tests, not guessed by the substitution (the forms R4 does not cover).

**Alternatives considered**: one step that writes every file (loses the per-step scope and the
TestsFailAgainstStub order); capturing `gen/**` too (two owners of generated code; GenerationRepeatable
would fail on any name oapi-codegen spells differently); keeping the review agent (a model call,
against the owner's answer).

## R2. How a filled step's files reach the project (FR-009)

**Decision**: the engine renders a `fill` step's files (template, values, the rules of R4) and the
project worker writes them into the step's writable paths with a new call, `writeStep`, before the
step's checks run; after the checks pass, `commitStep` commits as for any step, and `restoreStep`
before an attempt restores as before (S5). `fill` needs no registered step worker; the engine's
readiness check skips it. For a build, `ap build-workers` is unchanged (S9): it still starts the
base pattern's containers for the slice, and a captured run leaves the agent ones unused. A proof
is a build of its proof slice (R5), so its step workers are that slice's, started by `ap
build-workers <proof slice> <project>` as for any slice (S17, S18; *owner*, analyze round 3,
superseding the T001 answer that keyed them by the captured pattern).

**Rationale**: the project worker is already the only writer of a project besides the step
containers, and already writes stored files for a reused step (S5); the engine stays without a
project mount and without git (ADR 0032). Rendering in the engine keeps one owner for the
substitution rules (Single owner for rules), shared by proposal, fit and fill.

**Alternatives considered**: a `fill` procedure on the step containers (needs the template sent to
a worker per step and `build-workers` to know which pattern a run uses, S9); writing through
`commitStep` before the checks (an unchecked commit in the project's history).

## R3. Where captured patterns are kept (FR-002, FR-007, FR-012)

**Decision**: the engine store keeps captured patterns as records beside slices: the declaration
(the base pattern's steps with `fill` workers, R1), the source slice's approved parts, the
parameters, the template's artifact hashes, every proof, the approval and the state. The template
artifacts are held by the captured pattern as well as by the source slice (S4), so erasing the
slice keeps them; erasing a captured pattern is a separate, confirmed operation. A captured
pattern's ID is `<base id>-<name>` (S10), `<name>` given at capture or the source slice's ID in
lower case (`S-3` gives `api-feature-go-s-3`); an ID that exists answers `CapturedPatternExists`.
Its version is always `1.0.0`: an approved captured pattern never changes, and a change is a new
capture under another name (*owner*, FR-007). One operation at a time per captured pattern: a
parameters change, proof, approval, withdrawal or erasure while another of its operations runs
answers `CapturedPatternBusy` (Principle IV ordering; analyze O1). Erasure is also refused while a run built from it, or its proof, has not ended (analyze round 2, F4).

**Rationale**: Principle III (user-managed things enter through the API and live in the owning
component's store); the repository's `src/catalogue/` stays the reviewed base patterns, and a
captured pattern is the developer's data, like a slice. Reusing the existing holder logic of the
content-addressed store needs no second copy.

**Alternatives considered**: writing a captured pattern into `src/catalogue/` (a management action
through the file system, Principle III; it would also change the method identity, S6).

## R4. Parameters, substitution and fit (FR-003, FR-008)

**Decision**: one generic engine, package `engine/captured`, owns the rules, used by the proposal,
the fit and the fill:

- **Candidates**: the slice's names (each schema property name and each word of an operation ID or
  schema name, split at case changes: `createNote` gives `note`) and its declared values (feature
  009's walk). The developer may remove any and add another name, declared value or literal value
  (a number or a string that occurs in the slice) (*owner*).
- **Value forms** (Values that check themselves): a parameter's kind fixes the form of every value
  it takes, the source's and a later slice's: a name is `[a-z][a-z0-9]*` with at least two
  letters, so its capitalised and upper forms differ; a number (a declared or literal value
  written as a number) is an integer or a decimal without a sign; a string (a declared or literal
  value written as a string) is letters, digits, `_`, `-` and spaces. Every value is at most 64
  characters. A number is matched only as a whole number: the characters around it are not
  digits, letters or `_`, and not a `.` followed by a digit (`200` is not in `2001`, `1.200`,
  `200ms` or `v200`; it is in `-200` and `"200"`). A value outside its form is refused at the proposal and at the fit,
  before any substitution.
- **Case forms** (*owner*): a name covers its lower (`note`), capitalised (`Note`) and upper
  (`NOTE`) forms, each replaced wherever it occurs, inside longer words too (`NewNote`,
  `note_id`, `denote`); a plural is its own parameter.
- **Matching**: occurrences are found longest source form first, left to right, never
  overlapping; two parameters whose values are equal in any case form are refused.
- **Paths** (*owner*, analyze round 5): template file paths are filled like contents, in the three
  case forms (`cmd/notes/main.go` becomes `cmd/tasks/main.go`); a place records whether it is in a
  file's content or its path. Two template files that would get the same filled path, or a filled
  path that matches none of its step's writable entries, make the slice not fit, naming the
  paths.
- **Fit**: the source slice's canonical text (the approved goals, rules, examples, terms and
  contract, as feature 001's reuse compares them) is split into literal text and parameter holes;
  the approved slice fits when one value per parameter, read from the first hole of each and of
  its form, fills the source into exactly the approved slice's text, and no two parameters take
  equal values. Otherwise the slice does not fit, naming the first place it differs or the value
  refused and why (*owner*: a value outside its form does not fit, and the start's answer says
  so). A captured pattern's base fit conditions also run.
- **Limits** (stated in the contract): a spelling that is none of the three forms (`nOTE`) is not
  replaced; irregular plurals only as their own parameter; a derived value that is not added as
  its own parameter (E1); a value that occurs where it was not meant (`denote`, a status equal to
  a limit) is replaced everywhere, which the proposal lists place by place.

**Rationale**: E1 shows the three case forms and longest-first matching cover a whole accepted
project. A fit by substitution equality is exact, needs no model and has a single answer per
slice. Restricting values to their forms keeps a value from becoming Go syntax in the filled code
(threat model).

**Alternatives considered** (Principle VI): gonew rewrites only a module path and is experimental
(S11); Copier and Cookiecutter render hand-written Jinja templates and cannot derive one from an
accepted project, and would add a Python stack to the engine (S12, S13); `text/template` would
need the accepted files rewritten with `{{...}}` actions first, which is the substitution itself,
and assumes trusted templates (S14). Custom code is justified: deriving a template from an
accepted project by named values is what none of them does; it is one small engine with tests.

## R5. The proof (FR-004, FR-005, FR-006, FR-011)

**Decision**: each captured pattern has one **proof slice**: an approved slice made from its copy
of the source slice, created by `POST /captured-patterns/{id}/proof-slice` when it has none (the
first time, and again after the developer erased it). A proof is a `startRun` of the proof slice
into a new project folder the developer names: an ordinary run, on the proof slice's step workers,
under the captured pattern with each parameter's source value, recorded like any run (`RunStarted`
naming the captured pattern, `pattern_reason` `chosen`, `source_run` and `proof: true`), so it
works after the source slice is erased (*owner*, analyze round 3; S18). It passes when the run
ends done and its filled outputs, `tests`, `code` and `witnesses`, have the hashes of the captured
pattern's template, which are the source run's accepted outputs and outlive its record; the
generated code is judged by its checks, as in every build, so a method change that changes
generated bytes does not fail a proof (*owner*, analyze round 5, F7) (*owner*:
the proof shows the captured pattern reproduces its source under the current method; whether other
values work is judged by each later run's checks). A captured pattern is **usable** when it is
approved and its latest proof passed under the current method; a later failing proof makes it
unusable (fail closed; analyze M1). The captured pattern keeps each proof's run ID,
outcome, differing files and the method; it may be approved, and used, only while its latest
passing proof's method is the engine's current method. A proof is started by the developer and
followed as a run.

The proof slice (analyze round 4, *owner*): the slices service makes it from the captured copy
with its own description `Proof of <captured pattern>` and a `proof_of` naming the captured
pattern; the engine approves each part, recorded in its history as made by the engine for that
proof, after running the part checks under the current method; a part that fails a check makes the
operation fail with `PartFailsChecks`, listing the failures, and no slice is made. It takes the
next `S-<n>`. Slice reuse at `createSlice` (`slices/service.go` `approvedFor`) never returns a
proof slice. A proof never reuses an earlier run's results: every step runs, so every check runs
under the current method (FR-004). The proof's record is written into the captured pattern when
`startRun` of the proof slice is accepted (`running`) and completed when the run's end is
recorded, idempotently: `done` with every template hash equal passes; `done` with a hash that
differs fails, naming the differing files; a hand-over (`RunHandedOver`, the run then waits for
the developer) fails at once, naming the failing check and its reason; `expired` or `cancelled`
fails, naming the end; only an interrupted run stays `running`. `getCapturedPattern` and
`listCapturedPatterns` complete any proof whose run has ended or been handed over before they
answer, from the run record, so a reader never sees `running` for a finished proof, also after an
engine restart (analyze round 5, F1, F9). The failed proof's run waits like any handed-over run
until the developer cancels or takes it over. While a proof's run has not ended, `PUT
/parameters` and erasure answer `CapturedPatternBusy`, so a passing proof always proved the
parameters it is recorded with (F6). A replay of a proof run is a new proof, with no reuse. Erasing the
proof slice (`deleteSlice`, refused while its run is active) keeps the recorded proofs, whose run
IDs then answer NotFound, and clears `proof_slice`.

Runs that continue (analyze round 4, *owner*): resume and take-over keep the run's recorded
pattern, its parameters and its source run; a replay of a run built from a captured pattern uses
that captured pattern again, refused with `CapturedPatternWithdrawn` or `ProofRequired` when it is
not usable; the readiness check of `startRun`, `resumeRun` and `replayRun` skips `fill` steps
(`api/runs.go` `ready`, `ResumeRun`, `ReplayRun`). The source run's ID keeps naming it after the source slice and its runs are
erased (getRun then answers NotFound).

**Rationale**: running the proof as an ordinary run reuses every check, the record, durability and
cancellation (features 001, 002); the method rule reuses feature 006's identity (S6).

**Alternatives considered**: comparing files only (would not prove the checks still pass under the
current method); a proof per later slice (that is the run itself, judged by its checks). A proof
run with no slice, its workers keyed by the captured pattern (the owner's T001 answer, ADR 0096):
analyze round 3 found it needs a second home for runs in the store, workflows, API and launcher
and a breaking contract change (S18). The source slice's workers: no proof after the slice is
erased, against ADR 0095. A new slice per proof: slices pile up; one proof slice per captured
pattern is enough.

## R6. Which pattern a run uses (FR-010)

**Decision**: at `startRun` the engine judges every approved, usable captured pattern against
the approved slice; the most recently approved that fits is used, unless
the request names a pattern (a captured pattern's ID or the base pattern's). A fitting captured
pattern whose proof's method is not current is skipped and named in the answer as needing a new
proof. The run (`Run`, readable as soon as `startRun` answers with its operation, as before) and
`RunStarted` name the pattern and why (`pattern_reason`: `fits`, `chosen` or `base`; a proof run
is `chosen`); the run also names each fitting captured pattern that needs a new proof and each
that did not fit only because of a refused value, with the reason (*owner*).

**Rationale**: the owner's answer; one rule chooses between providers of one kind (Principle VII).

## R7. Interface: contract, operations and CLI (FR-014)

**Decision**: the engine contract gains, additively, the resource `/captured-patterns`: `POST`
(capture a run: an operation), `GET` (list, paginated), `GET /{id}`, `PUT /{id}/parameters`
(before approval), `POST /{id}/proof-slice`, `POST /{id}/approval`, `POST /{id}/withdrawal` and
`DELETE /{id}` (erase, confirmed), each state change an operation with an Idempotency-Key;
`startRun` gains an optional `pattern`. New error codes: `RunNotDone`, `ParameterRefused`,
`ProofRequired`, `CapturedPatternWithdrawn`, `CapturedPatternExists`, `CapturedPatternBusy`.
Erasure shows what it will erase (the captured pattern, the artifacts only it holds) before the
client asks for confirmation (Principle XVII). The terminal client gains `ap capture`, `ap patterns`,
`ap pattern`, `ap proof-slice`, `ap approve-pattern`, `ap withdraw-pattern`, `ap erase-pattern` and
`build --pattern`; the launcher gains `ap prove`, which prints the proof's outcome. The
check contract gains `writeStep` (R2). A proof is `startRun` on the proof slice; the run and
`RunStarted` carry `proof: true`, additively; an operation's `target` may be a captured
pattern's ID. The launcher intercepts `prove <captured pattern> <project>`: the client's
`proof-slice <captured pattern>` gives the proof slice, then `build <proof slice> <project>` runs
as `build` does (R2, R5).

**Rationale**: Principles III, IV and XV; the existing operation workflow runs any job (S7).

## R8. Evals and recordings

No agent, prompt or model changes, so no agent's behaviour changes (Principle XIX). Engine code
changes the method identity, which every suite reads, so the step and analyst suites run before
merge (with the owner's go-ahead), as after any engine change. The recorded step sets do not
change: no generated file or step request changes for the base pattern. A captured run makes no
model call, which the acceptance tests assert (SC-001).

## Threat-model input

- Parameter values come from the approved slice, which the analyst model may have written:
  untrusted text that becomes part of Go code and tests (LLM01, LLM05).
- The template is code a model wrote and the checks and review accepted once; it is reused
  without a review (ASI06: memory and context poisoning across tasks).
- A captured pattern is the developer's data; the platform serves one developer (feature 001 FR-021), so tenant isolation (Principle XII) is not applicable until the hosted version, as for slices.
