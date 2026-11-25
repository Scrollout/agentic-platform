# Data model: A proven slice becomes a pattern that builds without a model

Phase 1 of the [plan](plan.md). Kept in the engine store (research R3); the template's files are
artifacts in its content-addressed store.

## Captured pattern

| Field | Meaning | Rule |
|-------|---------|------|
| `id` | `<base id>-<name>` | `pattern_id` (`^[a-z0-9-]+$`); `<name>` `^[a-z0-9]+(-[a-z0-9]+)*$`, at most 40 characters; unique, an existing or erased one answers `CapturedPatternExists` (an erased ID is never given again, so a reuse key naming `<id>@1.0.0` always means one template); `<name>` given at capture or the source slice's ID in lower case (`s-3`) |
| `version` | semver | always `1.0.0`: an approved captured pattern never changes; a change is a new capture under another name (FR-007) |
| `base` | the base pattern's `id@version` | the engine's pattern at capture |
| `source` | source run ID, source slice ID | the run ended done (FR-001) |
| `slice` | the source slice's approved parts (goals, rules, examples, terms, contract) | copied at capture; kept when the slice is erased |
| `declaration` | the base pattern with each agent step's worker `procedure:fill`, `limits.attempts` 1 and `limits.review_rounds` 1 for the whole declaration (the schema has no per-step attempts), and the review step without FindingsCarryEvidence and NoBlockingFindingOpen and with outputs `[witnesses]` | valid against `pattern.schema.json` (research R1) |
| `template` | per filled output (`tests`, `code`, `witnesses`): the accepted output's artifact hash | held by the captured pattern (research R3) |
| `proof_slice` | the slice ID of its proof slice, absent until the first proof request and cleared when that slice is erased | an approved slice made from `slice`; its runs are the proofs (research R5) |
| `parameters` | list of Parameter | FR-003 |
| `proofs` | list of Proof, newest last | FR-005 |
| `approval` | `by` (`client`: the developer's client token, the only caller that may approve; the engine holds no user identity), `at`, the proof it approved | only after a passing proof under the current method (FR-006) |
| `state` | see below | |

## Parameter

| Field | Meaning | Rule |
|-------|---------|------|
| `name` | the parameter's name | `^[a-z][a-z0-9-]*$`, unique in the captured pattern |
| `kind` | `name`, `number` or `string` | a later value must be of the same kind (research R4) |
| `value` | the source value | of its kind's form, at most 64 characters: `name` `[a-z][a-z0-9]*` with at least two letters (the lower form); a number an integer or decimal without a sign; a string letters, digits, `_`, `-` and spaces (research R4) |
| `places` | each occurrence: part (`slice`, a step's file path), `in` (`content` or `path`), line and column (for a path, line 1 and the column in the path), case form | found longest first, never overlapping; paths are filled like contents, and two files that would get the same filled path make the slice not fit (research R4) |
| `source` | `name`, `declared-value` or `literal`: where the value came from | |
| `origin` | `proposed` or `added` | added only from the slice's names, declared values or literal values (*owner*) |

Two parameters equal in any case form are refused (`ParameterRefused`).

`PUT /parameters` takes `{name, value}` per parameter, nothing else: the engine sets `kind` and
`source` (a value among the slice's names is kind `name`, source `name`; one the contract
declares is source `declared-value`, kind `number` or `string` as the contract writes it; any other
is source `literal`, kind `number` when it is a number, else `string`; a parameter sent again keeps
its kind and source), finds the `places`, and sets `origin`. The terminal client's `--add
[<name>=]<value>` takes the name from the value when none is given (refused when the value is not
a valid name). Capture refuses with `RunNotCapturable` a run whose recorded `pattern` is not the
engine's current base pattern `id@version` (naming both; the declaration and template would come
from different versions), a run of a captured pattern, a proof run,
and a run whose slice changed after it started (a part's hash differs from the
part hash its `RunStarted` records, or a part is no longer approved), naming the part; a run
whose `RunStarted` has no part hashes (started before this feature) is refused the same way. The limits (32 parameters, 10000 places, 64 characters) are the contract's integer
`x-limits` keys.

## Proof

| Field | Meaning |
|-------|---------|
| `run` | the proof's run ID: a run of the proof slice under the captured pattern with the source values, into a new project folder the developer named |
| `method` | the method identity it ran under |
| `outcome` | `passed`, `failed` or `running` |
| `failures` | each failing check (step, check, reason), each differing file (step, path), or the run's end when it ended other than `done` |
| `parameters` | the parameters' names and source values it proved |

No Proof exists until a `RunStarted` with `proof: true` is on record; the Proof-completion
function creates it from that run (`running`), and completes it when
the run is handed over or ends (idempotently): `done` with the `tests`, `code` and `witnesses`
outputs' hashes equal to the `template` hashes is `passed`; `done` with a differing hash is
`failed`, naming the files; a hand-over (`RunHandedOver`) is `failed` at once, naming the failing
check and its reason, while the run waits for the developer as any handed-over run; `expired` or
`cancelled` is `failed`, naming the end; only an interrupted run stays `running`. Generated code is
not compared: its checks judge it (research R5). `getCapturedPattern` and `listCapturedPatterns`
complete any proof whose run was handed over or ended before they answer, from the run record, so
no reader sees `running` for a finished proof, also after an engine restart; every reader of a
Proof's state (approval, the usable rule at `startRun` and `replayRun`, the busy checks) goes
through the same completion first, so a restart or a lost start answer never leaves a proof
run without its Proof. Busy is judged from the proof slice's runs, not from Proof records: while
a run of the proof slice is starting (accepted, its `RunStarted` not yet recorded) or has not
ended, `PUT /parameters` and erasure answer `CapturedPatternBusy`. A Proof records the
parameters it proved; it moves a captured pattern to `proven`, and counts for approval, only
while those equal the current parameters. Erasing the proof slice
keeps every Proof; its `run` then answers NotFound.

## States

```text
proposed --(proof passes)--> proven --(developer approves)--> approved --(withdraw)--> withdrawn
proven --(parameters changed, or a proof fails)--> proposed
approved: a new proof may run at any time; the state stays approved
withdrawn: final; only erasure follows
any --(erase, confirmed)--> erased
```

| Call | proposed | proven | approved | withdrawn |
|------|----------|--------|----------|-----------|
| `replaceParameters` | changes them | changes them, back to proposed | `CapturedPatternApproved` | `CapturedPatternWithdrawn` |
| `makeProofSlice`, a proof's `startRun` | allowed | allowed | allowed | `CapturedPatternWithdrawn` |
| `approveCapturedPattern` | `ProofRequired` | approves when its latest proof passed under the current method, else `ProofRequired` | changes nothing | `CapturedPatternWithdrawn` |
| `withdrawCapturedPattern` | withdraws | withdraws | withdraws | changes nothing |
| `eraseCapturedPattern` | erases | erases | erases | erases |

Every call answers `CapturedPatternBusy` while another of its operations runs; `replaceParameters`
and erasure also while a proof's run (or, for erasure, a run built from it) has not ended.
`approveCapturedPattern` on an approved one changes nothing whatever its proofs.

A captured pattern is **usable** when it is `approved` and its latest proof passed under the
engine's current method; a method change or a later failing proof makes it unusable until a new
proof passes (FR-011). Only a usable captured pattern is judged at `startRun`. One operation at a
time per captured pattern; another answers `CapturedPatternBusy`. While a newer proof runs, the latest
proof has not passed, so the captured pattern is unusable until it does (fail closed). Erasure is
refused with `CapturedPatternBusy` while a run built from it, or its proof, has not ended; erasing
it also erases its proof slice and that slice's runs (its proofs), and keeps the records of runs of
other slices built from it, which keep naming it.

## Proof slice

An approved slice (research R5) made by the slices service from the captured pattern's copy:
described `Proof of <captured pattern>`, with `proof_of` naming the captured pattern, and each
part approved by the engine after the part checks pass under the current method. Its history
(`slice.schema.json`) starts with a new kind, `made-for-proof` (no model, `refs` its parts),
added to the kind enum additively, then one `approved` entry per part; a failing check fails the operation with
`PartFailsChecks` and makes no slice. It takes the next `S-<n>`. Asked again, `proof-slice`
gives the existing proof slice while every part is approved under the current method; after a
method change withdrew a part's approval, it checks the parts again and approves them (one
`approved` history entry per part), or fails with `PartFailsChecks` and leaves the slice as it
is. `createSlice` never reuses it.
A `startRun` of it is a proof: it always uses that captured pattern, with the source values,
whatever its state except withdrawn (`CapturedPatternWithdrawn`); a `pattern` naming another one,
or a part changed since it was made (so it no longer equals the copy), is `PatternDoesNotFit`. Its
step workers are started by `ap build-workers <proof slice> <project>` as for any slice; a `fill`
step needs none. A proof never reuses an earlier run's results, so every step and check runs under
the current method. The developer may erase it like any slice (refused while its run is active);
that clears `proof_slice`, and the next proof request makes a new one. The `Slice` resource and
`SliceSummary` gain an optional `proof_of`, additively; `ap slices` and `ap show` print it.

## Runs that continue

A resumed or taken-over run keeps its recorded pattern, parameters and source run. A replay of a
run built from a captured pattern uses that captured pattern again only while it is usable, and is
refused with `CapturedPatternWithdrawn` or `ProofRequired` otherwise, and NotFound when it was
erased; it keeps the parameter values the run recorded, with no new fit, and copies the earlier
run's `part_hashes` (none when the earlier run had none, so capturing it is refused); a replay of a proof run is
a new proof. The readiness check of `startRun`, `resumeRun` and `replayRun` skips `fill` steps.

## Run (added fields)

A proof's run has `proof: true` (in `Run` and in `RunStarted`) and keeps its proof slice as its
`slice`. A captured-pattern operation's `target` is the captured pattern's ID; the proof-slice
operation's result names the proof slice.
The run (`getRun`, readable as soon as `startRun` answers; the 202 is the operation, as before)
carries the pattern choice, read from its `RunStarted`; a proof run's `pattern_reason` is
`chosen`: `pattern`,
`pattern_reason`, and for a captured pattern `parameters` and `source_run`, plus the captured
patterns that fit but need a new proof and those refused only for a value, each with the reason.

`RunStarted` gains, additively: `pattern` names the captured pattern's `id@version` as before for
any pattern; `pattern_reason` (`base`, `fits`, `chosen`); for a captured pattern `parameters`
(an object, parameter name → value, each a `parameter_value`) and `source_run` (a run ID); `proof`
(`true` for a proof run); `part_hashes` (the goals, rules and contract part hashes the run was
started on, since its `inputs` hold only the pattern's slice inputs: an object with the
required keys `goals`, `rules` and `contract`, each a `hash`); `not_used`: a list, each entry `{pattern, reason, parameter?, detail}`,
`pattern` a captured pattern's ID, `reason` `needs-proof` or `refused-value`, `parameter` the
refused one (with `refused-value` only), `detail` why in words; judged at the start and recorded,
since that state changes later. `Run` carries the same fields with the same shapes, except `part_hashes`, which only capture
reads. `source_run` keeps naming the
source run after its slice and runs are erased. The method
is already in every step outcome that carries a reuse key (feature 006). The reuse key holds the pattern's `id@version`
and the method; for a `fill` step it also takes the hash of the run's parameter values as an
extra input (research R4, Reuse), so two slices that differ only in a value never share a filled
result.
