# Contract draft: captured patterns

**Authoritative copies** (T001): the engine contract
[`openapi.yaml`](../../../src/engine/api/preview/2026-11-12-preview/openapi.yaml) (tag
`captured-patterns`, `startRun`, `replayRun`) with
[`slice-defs.schema.json`](../../../src/engine/api/preview/2026-11-12-preview/slice-defs.schema.json),
[`run-event.schema.json`](../../../src/engine/api/preview/2026-11-12-preview/run-event.schema.json) and
[`slice.schema.json`](../../../src/engine/api/preview/2026-11-12-preview/slice.schema.json); the check
contract [`openapi.yaml`](../../../src/check/api/preview/2026-11-13-preview/openapi.yaml)
(`writeStep`); the terminal contract [`contract.md`](../../../src/cli/contract.md). Where this draft
and they differ, they hold.

A draft for the first consumer (the terminal client). The authoritative copies are the engine
contract (`src/engine/api/preview/2026-11-12-preview/openapi.yaml`) and the check contract
(`src/check/api/preview/2026-11-13-preview/openapi.yaml`), extended additively before any code, except the new enum values ([ADR 0100](../../../docs/decisions/0100-preview-contract-gains-enum-values-in-place.md))
(Contract-first). Fields are in the [data model](../data-model.md).

## Engine API: `/captured-patterns`

Turn a done run into a pattern that builds later slices of the same kind with no model call.

| Call | What it does | Answers |
|------|--------------|---------|
| `POST /captured-patterns` `{run, name?}` (`name` `^[a-z0-9]+(-[a-z0-9]+)*$`, at most 40 characters; otherwise 400 InvalidRequest) | proposes a captured pattern from a run that ended done, with proposed parameters | 202 operation, its target the new captured pattern's ID; refused at once with `RunNotDone` (naming the run's state), `RunNotCapturable` (a run built under another base pattern version than the current one, a run built from a captured pattern, a proof, or a run whose slice changed after it started, compared with the part hashes its `RunStarted` records) or `CapturedPatternExists` (an existing or erased ID); a capture whose operation fails leaves no captured pattern (getCapturedPattern answers 404) |
| `GET /captured-patterns` | lists captured patterns, newest first | 200, paginated with opaque page tokens |
| `GET /captured-patterns/{id}` | one captured pattern: parameters with their places, proofs, approval, state, usable | 200; 404 |
| `PUT /captured-patterns/{id}/parameters` `{parameters: [{name, value}]}` | replaces the parameters of a proposed or proven one (removes, adds); the engine sets each kind and source (data model, Parameter) | 400 `InvalidRequest` at once for a body outside the schema (more than 32 parameters, a value over 64 characters or outside `parameter_value`); otherwise 202 operation, which fails `ParameterRefused` naming the parameter and why (a value outside its kind's form, equal to another in any case form, not occurring in the slice, or more than 10000 places); 409 `CapturedPatternApproved`, `CapturedPatternWithdrawn`, or `CapturedPatternBusy` while an operation or a proof's run has not ended, at once |
| `POST /captured-patterns/{id}/proof-slice` | gives its proof slice, making it from the captured copy when it has none and restoring from the copy any part changed since (a `changed` then `approved` history entry) | 202 operation, its target the captured pattern, its result the proof slice; `CapturedPatternWithdrawn`, `CapturedPatternBusy` at once |
| `POST /captured-patterns/{id}/approval` | approves it, recorded with `by` (`client`), `at` and the proof (the state table in the data model gives every call's answer per state) | 202 operation; 409 `ProofRequired` at once when its latest proof did not pass under the current method |
| `POST /captured-patterns/{id}/withdrawal` | stops its use by new runs | 202 operation |
| `DELETE /captured-patterns/{id}` | erases it and the artifacts only it holds | 202 operation; 409 `CapturedPatternBusy` at once while an operation, a run built from it or its proof's run has not ended; the client first shows what will be erased and asks for confirmation |

Every `/captured-patterns/{id}` call answers 404 `NotFound` for an unknown or erased ID.
One operation at a time per captured pattern: another state change while one runs answers 409
`CapturedPatternBusy`. Every state change takes an `Idempotency-Key` (held 24 h, ADR 0073); a reused key with another
body answers 409 `IdempotencyKeyReused` as for other calls. Limits as integer `x-limits` keys:
`captured-pattern-parameters-max: 32`, `parameter-places-max: 10000`,
`parameter-value-max-chars: 64`, also the schemas' `maxItems` and `maxLength`.

`startRun` gains an optional `pattern` (a captured pattern's ID or the base pattern's ID). Without
it the most recently approved usable captured pattern that fits is used, otherwise the base
pattern. `startRun` still answers 202 with the run's operation. The run (`Run`) and its
`RunStarted` name `pattern`, `pattern_reason` (`base`, `fits`, `chosen`; a proof run `chosen`)
and, for a captured pattern, the parameters' values and `source_run`; the `Run` also names each
fitting captured pattern that needs a new proof and each that did not fit only because of a
refused value, with the parameter and why. Refusals, in this order, each at once: InvalidRequest or
InvalidProject; NotFound for an unknown slice or a `pattern` naming no pattern; PatternDoesNotFit
for the base conditions, as before; NotApproved; `CapturedPatternWithdrawn`, then
`CapturedPatternBusy` (only a proof's start, while another captured-pattern operation runs; a
non-proof `startRun`, named or not, never answers Busy: it judges the pattern by its recorded
state and `usable` value), then
`CapturedPatternNotApproved` (a named captured pattern that is proposed or proven, FR-006; a
proof slice's own run is not refused), then `ProofRequired`, for a named captured pattern (or a
proof slice's, withdrawn and busy only);
PatternDoesNotFit, with the first place the approved slice differs or the value refused, for a
named captured pattern that does not fit;
WorkersNotReady (a `fill` step needs no worker); RunActive; ProjectExists. `replayRun` of a run
built from a captured pattern answers `CapturedPatternWithdrawn` or `ProofRequired` when it is not
usable, and NotFound when it was erased; `replayRun` of a proof run follows the proof rule
instead: a new proof in any state but withdrawn, answering `CapturedPatternWithdrawn` or
`CapturedPatternBusy`, never `ProofRequired`; `resumeRun` and `acceptHandOver` keep the run's
pattern.

Changes elsewhere, additive except the new enum values of `OperationKind`, `ErrorCode` and the slice history kinds (ADR 0100): `Operation.target` takes a new `operation_target` rule (a slice, run
or captured pattern ID) in place of `slice_or_run_id`; `Operation.result` gains `captured_pattern`;
`OperationKind` gains `capture-run`, `replace-parameters`, `make-proof-slice`,
`approve-captured-pattern`, `withdraw-captured-pattern` and `erase-captured-pattern`; `Run` gains
`proof` and the pattern choice (`pattern_reason`, `parameters`, `source_run`, `not_used`, read
from `RunStarted`, which gains the same plus `proof` and `part_hashes`); `Slice` and `SliceSummary` gain `proof_of`, and the
slice history's kinds gain `made-for-proof`; `ErrorCode` gains the
new codes. The client and the launcher read `target` as a string and print or match it only where
it names a run.

A proof is `startRun` of the proof slice into a new project folder: an ordinary run on that
slice's step workers, with `proof: true` on the run and its `RunStarted`, that reuses no earlier
result; it passes when it ends done and its `tests`, `code` and `witnesses` outputs equal the
captured pattern's template hashes (data model, Proof); a hand-over fails it at once.

## Fit and fill (package `engine/captured`)

- `Propose(slice, files) -> []Parameter`: candidates from the slice's names and declared values,
  each with every place it occurs; at most `captured-pattern-parameters-max` (32): the first in
  its order (names in byte order, then declared values in the contract's order) among those of
  their kind's form and unique in every case form; the places are found among the kept candidates
  only, and a kept one left with no place or over `parameter-places-max` is left out too.
- `Candidates(slice) -> []Parameter`: every candidate in Propose's order, form and case-form
  refusals dropped, no cut and no places: the kind and source of a value `PUT /parameters` adds.
- `Check(copy, template, parameters) error`: value forms, uniqueness in any case form, each value
  occurring in the copy, the declared limits.
- `Fit(copy, template, writable, parameters, approved) -> (values, ok, refusal)`, `refusal`
  being either the first `place` the approved slice differs or a refused `parameter` with its
  `detail` (it fills `not_used`'s `parameter` and `detail` and `PatternDoesNotFit`'s detail): research
  R4, including the filled paths (no two equal, each inside its step's writable entries).
- `Fill(files, parameters, values) -> files`: each place replaced in its case form, longest first,
  in contents and paths; refuses two files with the same filled path (the writable check is
  `Fit`'s, and `writeStep` refuses such a path again).
- A proof slice is made from the captured pattern's copy; `Fit` and `Fill` take that copy.

Limits: research R4.

## Check API: `writeStep`

`POST /project-versions/write`, served by the project worker: the body is `CallCommon` (run,
step facts with its writable entries, attempt, project, `before`, time limit) plus `files` (each
path, `sha256` and base64 content; it may be empty: a step whose template output holds no file,
such as a clean review's `witnesses`, writes nothing). It checks the history and record as
`restoreStep` does, that the last commit holds `before`, and that the files outside the step's
writable entries equal `before`'s, then makes the files matching the step's writable entries
exactly `files` without committing, and answers 200 `{written, removed}` in byte order:
`written` the paths of `files`, `removed` the writable-matching files of `before` not in
`files`; so a repeated request leaves the same files and gives the same answer. `files` holds
at most `x-limits.project-files-max` entries and `x-limits.step-files-max-bytes` of file bytes
together (more entries is 400 `InvalidRequest` by the schema's `maxItems`; more bytes is 422
`StepFilesTooLarge`), and the body may be up to
`x-limits.commit-request-body-max-bytes`, as for `commitStep`. Errors: 400 InvalidRequest (a path
matching no writable entry, naming it), 401,
404 VersionHistoryNotServed (not the project worker), 409 ProjectStateDiffers or HistoryChanged,
422 UnreadableProject, InputHashMismatch or StepFilesTooLarge, 429, 500 WriteFailed, 503 Busy, 504
TimeLimitExceeded. The fill worker then reads the step's files with `readStepFiles` (check
service) for the checks, as the agent path does.
`restoreStep` before the next attempt removes them as it removes any uncommitted change.

## Terminal client

| Command | Call |
|---------|------|
| `ap capture <run> [--name <name>]` | `POST /captured-patterns`, follows the operation, prints the parameters |
| `ap patterns` | `GET /captured-patterns` |
| `ap pattern <id>` | `GET /captured-patterns/{id}`: parameters and places, proofs, state; with no parameter it prints `No parameters: only a slice identical to the source fits.` |
| `ap pattern <id> --drop <param>` / `--add [<name>=]<value>` | `PUT /captured-patterns/{id}/parameters` |
| `ap proof-slice <id>` | `POST /captured-patterns/{id}/proof-slice`, follows the operation, prints the proof slice's ID |
| `ap prove <id> <project>` | the launcher runs the client's `proof-slice <id>`, then `build <proof slice> <project>` as for `build` (`build-workers` first); after the run's end line the client prints `Proof of <id>: passed`, `Proof of <id>: failed: <failure>; ...` or, while the proof is still running (its run interrupted), `Proof of <id>: running` with the run state's exit; a proof run exits 0 for a passed proof and 1 for a failed one, over the run state's own code (2 for a hand-over, 5 expired, 6 cancelled); `src/cli/contract.md`'s "Run states and exit codes" gains that row |
| `ap approve-pattern <id>` | `POST /captured-patterns/{id}/approval` |
| `ap withdraw-pattern <id>` | `POST /captured-patterns/{id}/withdrawal` |
| `ap erase-pattern <id>` | shows what will be erased, asks for confirmation, then `DELETE /captured-patterns/{id}` |
| `ap build <slice> <project> [--pattern <id>]` | `startRun` with `pattern` |

`ap proof-slice <id>` prints only the proof slice's ID and a line feed on standard output, which
the launcher reads. Output lines `src/cli/contract.md` gains: the `RunStarted` event line's fields
become `slice <slice>  pattern <pattern>[  reason <pattern_reason>][  proof][  source <run>][
parameters <name>=<value>,...]` (parameters in byte order of their names, here and on the run page); `build`'s first line adds `with <pattern> (<pattern_reason>)`;
the `run` header adds `reason`, `proof` and `source`; each fitting captured pattern that needs a
proof or was refused for a value prints one line `Not used: <captured pattern>: <reason>` after the
first line, `<reason>` being the entry's `detail` (and, for `refused-value`, the parameter);
`ap slices` gains a `PROOF OF` column and `ap show` a `proof of <captured pattern>`
line; after a proof's run `ap prove` prints `Proof of <id>: passed` or `Proof of <id>: failed:
<failure>; ...`. A failed proof's run waits for the developer like any handed-over run: cancel
or take it over before changing the parameters or proving again.
