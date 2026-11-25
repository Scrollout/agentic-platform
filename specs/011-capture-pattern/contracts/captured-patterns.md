# Contract draft: captured patterns

A draft for the first consumer (the terminal client). The authoritative copies are the engine
contract (`src/engine/api/preview/2026-11-12-preview/openapi.yaml`) and the check contract
(`src/check/api/preview/2026-11-13-preview/openapi.yaml`), extended additively before any code
(Contract-first). Fields are in the [data model](../data-model.md).

## Engine API: `/captured-patterns`

Turn a done run into a pattern that builds later slices of the same kind with no model call.

| Call | What it does | Answers |
|------|--------------|---------|
| `POST /captured-patterns` `{run, name?}` | proposes a captured pattern from a run that ended done, with proposed parameters | 202 operation, its target the new captured pattern's ID; refused at once with `RunNotDone` (naming the run's state), `RunNotCapturable` (a run built from a captured pattern, a proof, or a run whose slice changed after it started, compared with the part hashes its `RunStarted` records) or `CapturedPatternExists` (an existing or erased ID); a capture whose operation fails leaves no captured pattern (getCapturedPattern answers 404) |
| `GET /captured-patterns` | lists captured patterns, newest first | 200, paginated with opaque page tokens |
| `GET /captured-patterns/{id}` | one captured pattern: parameters with their places, proofs, approval, state, usable | 200; 404 |
| `PUT /captured-patterns/{id}/parameters` `{parameters: [{name, value}]}` | replaces the parameters of a proposed or proven one (removes, adds); the engine sets each kind and source (data model, Parameter) | 202 operation; the operation fails `ParameterRefused` naming the parameter and why; 409 `CapturedPatternApproved`, `CapturedPatternWithdrawn`, or `CapturedPatternBusy` while an operation or a proof's run has not ended, at once |
| `POST /captured-patterns/{id}/proof-slice` | gives its proof slice, making it from the captured copy when it has none | 202 operation, its target the captured pattern, its result the proof slice; `CapturedPatternWithdrawn`, `CapturedPatternBusy` at once |
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
`ProofRequired`, for a named captured pattern (or a proof slice's, withdrawn only);
PatternDoesNotFit, with the first place the approved slice differs or the value refused, for a
named captured pattern that does not fit;
WorkersNotReady (a `fill` step needs no worker); RunActive; ProjectExists. `replayRun` of a run
built from a captured pattern answers `CapturedPatternWithdrawn` or `ProofRequired` when it is not
usable, and NotFound when it was erased; `resumeRun` and `acceptHandOver` keep the run's
pattern.

Additive changes elsewhere: `Operation.target` takes a new `operation_target` rule (a slice, run
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
  each with every place it occurs.
- `Check(copy, template, parameters) error`: value forms, uniqueness in any case form, each value
  occurring in the copy, the declared limits.
- `Fit(copy, template, writable, parameters, approved) -> (values, ok, firstDifference)`: research
  R4, including the filled paths (no two equal, each inside its step's writable entries).
- `Fill(files, parameters, values) -> files`: each place replaced in its case form, longest first,
  in contents and paths; refuses two files with the same filled path (the writable check is
  `Fit`'s, and `writeStep` refuses such a path again).
- A proof slice is made from the captured pattern's copy; `Fit` and `Fill` take that copy.

Limits: research R4.

## Check API: `writeStep`

`POST /project-versions/write` `{project, step, files}`: the project worker writes the files into
the step's writable paths (each path must match one; otherwise 400 naming it) without committing.
`restoreStep` before the next attempt removes them as it removes any uncommitted change.

## Terminal client

| Command | Call |
|---------|------|
| `ap capture <run> [--name <name>]` | `POST /captured-patterns`, follows the operation, prints the parameters |
| `ap patterns` | `GET /captured-patterns` |
| `ap pattern <id>` | `GET /captured-patterns/{id}`: parameters and places, proofs, state |
| `ap pattern <id> --drop <param>` / `--add [<name>=]<value>` | `PUT /captured-patterns/{id}/parameters` |
| `ap proof-slice <id>` | `POST /captured-patterns/{id}/proof-slice`, follows the operation, prints the proof slice's ID |
| `ap prove <id> <project>` | the launcher runs the client's `proof-slice <id>`, then `build <proof slice> <project>` as for `build` (`build-workers` first); after the run's end line the client prints `Proof of <id>: passed` or `Proof of <id>: failed: <failure>; ...` |
| `ap approve-pattern <id>` | `POST /captured-patterns/{id}/approval` |
| `ap withdraw-pattern <id>` | `POST /captured-patterns/{id}/withdrawal` |
| `ap erase-pattern <id>` | shows what will be erased, asks for confirmation, then `DELETE /captured-patterns/{id}` |
| `ap build <slice> <project> [--pattern <id>]` | `startRun` with `pattern` |

`ap proof-slice <id>` prints only the proof slice's ID and a line feed on standard output, which
the launcher reads. Output lines `src/cli/contract.md` gains: the `RunStarted` event line's fields
become `slice <slice>  pattern <pattern>[  reason <pattern_reason>][  proof][  source <run>][
parameters <name>=<value>,...]`; `build`'s first line adds `with <pattern> (<pattern_reason>)`;
the `run` header adds `reason`, `proof` and `source`; each fitting captured pattern that needs a
proof or was refused for a value prints one line `Not used: <captured pattern>: <reason>` after the
first line, `<reason>` being the entry's `detail` (and, for `refused-value`, the parameter);
`ap slices` gains a `PROOF OF` column and `ap show` a `proof of <captured pattern>`
line; after a proof's run `ap prove` prints `Proof of <id>: passed` or `Proof of <id>: failed:
<failure>; ...`. A failed proof's run waits for the developer like any handed-over run: cancel
or take it over before changing the parameters or proving again.
