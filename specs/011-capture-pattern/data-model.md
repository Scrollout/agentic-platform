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
| `template` | per filled step (`tests`, `code`, `review`): the accepted output's artifact hash | held by the captured pattern (research R3) |
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
| `run` | the proof's run ID (a run of the source slice under the captured pattern with the source values) |
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
time per captured pattern; another answers `CapturedPatternBusy`.

## Run (added fields)

`RunStarted` gains, additively: `pattern` names the captured pattern's `id@version` as before for
any pattern; `pattern_reason` (`base`, `fits`, `chosen`); for a captured pattern `parameters`
(name → value) and `source_run`; `proof` (`true` for a proof run, which has no slice). The method
is already in every step outcome that carries a reuse key (feature 006). The reuse key is unchanged: it already holds
the pattern's `id@version` and the method.
