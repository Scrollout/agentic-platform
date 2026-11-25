# Data model: A proven slice becomes a pattern that builds without a model

Phase 1 of the [plan](plan.md). Kept in the engine store (research R3); the template's files are
artifacts in its content-addressed store.

## Captured pattern

| Field | Meaning | Rule |
|-------|---------|------|
| `id` | `<base id>-<name>` | `pattern_id` (`^[a-z0-9-]+$`); unique, an existing one answers `CapturedPatternExists`; `<name>` given at capture or the source slice's ID in lower case (`s-3`) |
| `version` | semver | always `1.0.0`: an approved captured pattern never changes; a change is a new capture under another name (FR-007) |
| `base` | the base pattern's `id@version` | the engine's pattern at capture |
| `source` | source run ID, source slice ID | the run ended done (FR-001) |
| `slice` | the source slice's approved parts (goals, rules, examples, terms, contract) | copied at capture; kept when the slice is erased |
| `declaration` | the base pattern with each agent step's worker `procedure:fill`, one attempt per `fill` step, and the review step without FindingsCarryEvidence and NoBlockingFindingOpen and with outputs `[witnesses]` | valid against `pattern.schema.json` (research R1) |
| `template` | per filled output (`tests`, `code`, `witnesses`): the accepted output's artifact hash | held by the captured pattern (research R3) |
| `expected` | the source run's `generated-code`, `tests`, `code` and `witnesses` hashes | copied at capture and held as artifacts, so a proof compares with them after the source run's record is erased (research R5) |
| `proof_slice` | the slice ID of its proof slice, absent until the first proof request or after that slice is erased | an approved slice made from `slice`; its runs are the proofs (research R5) |
| `parameters` | list of Parameter | FR-003 |
| `proofs` | list of Proof, newest last | FR-005 |
| `approval` | who, when, the proof it approved | only after a passing proof under the current method (FR-006) |
| `state` | see below | |

## Parameter

| Field | Meaning | Rule |
|-------|---------|------|
| `name` | the parameter's name | `^[a-z][a-z0-9-]*$`, unique in the captured pattern |
| `kind` | `name`, `number` or `string` | a later value must be of the same kind (research R4) |
| `value` | the source value | of its kind's form, at most 64 characters: `name` `[a-z][a-z0-9]*` with at least two letters (the lower form); a number an integer or decimal without a sign; a string letters, digits, `_`, `-` and spaces (research R4) |
| `places` | each occurrence: part (`slice`, a step's file path), line, column, case form | found longest first, never overlapping |
| `source` | `name`, `declared-value` or `literal`: where the value came from | |
| `origin` | `proposed` or `added` | added only from the slice's names, declared values or literal values (*owner*) |

Two parameters equal in any case form are refused (`ParameterRefused`).

## Proof

| Field | Meaning |
|-------|---------|
| `run` | the proof's run ID: a run of the proof slice under the captured pattern with the source values, into a new project folder the developer named |
| `method` | the method identity it ran under |
| `outcome` | `passed`, `failed` or `running` |
| `failures` | each failing check (step, check, reason) and each differing file (step, path) |

## States

```text
proposed --(proof passes)--> proven --(developer approves)--> approved --(withdraw)--> withdrawn
proven --(parameters changed, or a proof fails)--> proposed
approved: a new proof may run at any time; the state stays approved
any --(erase, confirmed)--> erased
```

A captured pattern is **usable** when it is `approved` and its latest proof passed under the
engine's current method; a method change or a later failing proof makes it unusable until a new
proof passes (FR-011). Only a usable captured pattern is judged at `startRun`. One operation at a
time per captured pattern; another answers `CapturedPatternBusy`. While a newer proof runs, the latest
proof has not passed, so the captured pattern is unusable until it does (fail closed). Erasure is
refused with `CapturedPatternBusy` while a run built from it, or its proof, has not ended; erasing
it also erases its proof slice and that slice's runs.

## Proof slice

An ordinary approved slice (research R5): its parts are the captured pattern's copy, approved when
it is made, and its record names the captured pattern it proves. A `startRun` of it is a proof:
it always uses that captured pattern, with the source values, whatever its state except
withdrawn (`CapturedPatternWithdrawn`); a `pattern` naming another one, or a part changed since
it was made (so it no longer equals the copy), is `PatternDoesNotFit`.
Its step workers are started by `ap build-workers <proof slice> <project>` as for any slice; a
`fill` step needs none. A second proof under the same method may reuse the first one's accepted
results, as any run of a slice does (feature 006); under a new method the reuse keys differ and
every step runs again. The developer may erase it like any slice; the next proof request makes a
new one.

## Run (added fields)

A proof's run has `proof: true` (in `Run` and in `RunStarted`) and keeps its proof slice as its
`slice`. A captured-pattern operation's `target` is the captured pattern's ID; the proof-slice
operation's result names the proof slice.
The run (`getRun`, readable as soon as `startRun` answers) carries the pattern choice: `pattern`,
`pattern_reason`, and for a captured pattern `parameters` and `source_run`, plus the captured
patterns that fit but need a new proof and those refused only for a value, each with the reason.

`RunStarted` gains, additively: `pattern` names the captured pattern's `id@version` as before for
any pattern; `pattern_reason` (`base`, `fits`, `chosen`); for a captured pattern `parameters`
(name → value) and `source_run`; `proof` (`true` for a proof run). `source_run` keeps naming the
source run after its slice and runs are erased. The method
is already in every step outcome that carries a reuse key (feature 006). The reuse key is unchanged: it already holds
the pattern's `id@version` and the method.
