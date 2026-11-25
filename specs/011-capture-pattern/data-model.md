# Data model: A proven slice becomes a pattern that builds without a model

Phase 1 of the [plan](plan.md). Kept in the engine store (research R3); the template's files are
artifacts in its content-addressed store.

## Captured pattern

| Field | Meaning | Rule |
|-------|---------|------|
| `id` | `<base id>-<name>` | `pattern_id` (`^[a-z0-9-]+$`); unique; `<name>` given at capture or the source slice's ID |
| `version` | semver | `1.0.0` at capture; a change after approval is a new version (FR-007) |
| `base` | the base pattern's `id@version` | the engine's pattern at capture |
| `source` | source run ID, source slice ID | the run ended done (FR-001) |
| `slice` | the source slice's approved parts (goals, rules, examples, terms, contract) | copied at capture; kept when the slice is erased |
| `declaration` | the base pattern with each agent step's worker `procedure:fill` and the review step without FindingsCarryEvidence and NoBlockingFindingOpen | valid against `pattern.schema.json` (research R1) |
| `template` | per filled step (`tests`, `code`, `review`): the accepted output's artifact hash | held by the captured pattern (research R3) |
| `parameters` | list of Parameter | FR-003 |
| `proofs` | list of Proof, newest last | FR-005 |
| `approval` | who, when, the proof it approved | only after a passing proof under the current method (FR-006) |
| `state` | see below | |

## Parameter

| Field | Meaning | Rule |
|-------|---------|------|
| `name` | the parameter's name | `^[a-z][a-z0-9-]*$`, unique in the captured pattern |
| `kind` | `name`, `declared-value` or `literal` | research R4 |
| `value` | the source value | of its kind's form; for `name`, the lower form |
| `places` | each occurrence: part (`slice`, a step's file path), line, column, case form | found longest first, never overlapping |
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
proposed <--(parameters changed)-- proven
proven --(proof fails)--> proposed
approved --(method changes)--> approved, unusable until a new proof passes (FR-011)
any --(erase, confirmed)--> erased
```

A captured pattern is **usable** when it is `approved` and its latest passing proof's method is
the engine's current method. Only a usable captured pattern is judged at `startRun`.

## Run (added fields)

`RunStarted` gains, additively: `pattern` names the captured pattern's `id@version` as before for
any pattern; `pattern_reason` (`base`, `fits`, `chosen`); `parameters` (name → value) for a
captured pattern; `proof` (`true` for a proof run). The reuse key is unchanged: it already holds
the pattern's `id@version` and the method.
