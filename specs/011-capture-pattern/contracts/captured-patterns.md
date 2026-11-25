# Contract draft: captured patterns

A draft for the first consumer (the terminal client). The authoritative copies are the engine
contract (`src/engine/api/preview/2026-11-12-preview/openapi.yaml`) and the check contract
(`src/check/api/preview/2026-11-13-preview/openapi.yaml`), extended additively before any code
(Contract-first). Fields are in the [data model](../data-model.md).

## Engine API: `/captured-patterns`

Turn a done run into a pattern that builds later slices of the same kind with no model call.

| Call | What it does | Answers |
|------|--------------|---------|
| `POST /captured-patterns` `{run, name?}` | proposes a captured pattern from a run that ended done, with proposed parameters | 202 operation; the operation fails `RunNotDone` (naming the run's state) or `CapturedPatternExists` |
| `GET /captured-patterns` | lists captured patterns, newest first | 200, paginated with opaque page tokens |
| `GET /captured-patterns/{id}` | one captured pattern: parameters with their places, proofs, approval, state, usable | 200; 404 |
| `PUT /captured-patterns/{id}/parameters` `{parameters}` | replaces the parameters of a proposed or proven one (removes, adds) | 202 operation; fails `ParameterRefused` naming the parameter and why; 409 when approved |
| `POST /captured-patterns/{id}/proof-slice` | gives its proof slice, making it from the captured copy when it has none | 202 operation, its target the captured pattern, its result the proof slice; `CapturedPatternWithdrawn`, `CapturedPatternBusy` at once |
| `POST /captured-patterns/{id}/approval` | approves it | 202 operation; fails `ProofRequired` when its latest proof did not pass under the current method |
| `POST /captured-patterns/{id}/withdrawal` | stops its use by new runs | 202 operation |
| `DELETE /captured-patterns/{id}` | erases it and the artifacts only it holds | 202 operation; the client first shows what will be erased and asks for confirmation |

One operation at a time per captured pattern: another state change while one runs answers 409
`CapturedPatternBusy`. Every state change takes an `Idempotency-Key` (held 24 h, ADR 0073); a reused key with another
body answers 409 `IdempotencyKeyReused` as for other calls. Limits in `x-limits`: at most 32 parameters, at most 10000
places per parameter, at most 64 characters per value.

`startRun` gains an optional `pattern` (a captured pattern's ID or the base pattern's ID). Without
it the most recently approved usable captured pattern that fits is used, otherwise the base
pattern. Its answer and `RunStarted` name `pattern`, `pattern_reason` and, for a captured pattern,
the parameters' values; a fitting captured pattern that needs a new proof is named in the answer.
The answer also names each captured pattern that did not fit only because of a refused value,
with the parameter and why. A named captured pattern that does not fit answers
`PatternDoesNotFit` with the first place the slice differs or the value refused; a withdrawn one `CapturedPatternWithdrawn`; one that needs a proof `ProofRequired`.

A proof is `startRun` of the proof slice into a new project folder: an ordinary run on that
slice's step workers, with `proof: true` on the run and its `RunStarted`; it passes when it ends
done and its outputs equal the captured pattern's `expected` hashes.

## Fit and fill (package `engine/captured`)

- `Propose(slice, files) -> []Parameter`: candidates from the slice's names and declared values,
  each with every place it occurs.
- `Check(parameters) error`: value forms, uniqueness in any case form.
- `Fit(source, parameters, approved) -> (values, ok, firstDifference)`: research R4.
- `Fill(files, parameters, values) -> files`: each place replaced in its case form, longest first.
- A proof's run takes the captured pattern's copy of the source slice, not a slice ID.

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
| `ap pattern <id> --drop <param>` / `--add <value>` | `PUT /captured-patterns/{id}/parameters` |
| `ap proof-slice <id>` | `POST /captured-patterns/{id}/proof-slice`, follows the operation, prints the proof slice's ID |
| `ap prove <id> <project>` | the launcher runs the client's `proof-slice <id>`, then `build <proof slice> <project>` as for `build` (`build-workers` first) |
| `ap approve-pattern <id>` | `POST /captured-patterns/{id}/approval` |
| `ap withdraw-pattern <id>` | `POST /captured-patterns/{id}/withdrawal` |
| `ap erase-pattern <id>` | shows what will be erased, asks for confirmation, then `DELETE /captured-patterns/{id}` |
| `ap build <slice> <project> [--pattern <id>]` | `startRun` with `pattern` |
