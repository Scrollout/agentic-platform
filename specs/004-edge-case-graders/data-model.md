# Data model: Eval graders judge edge cases, not only the examples

Phase 1 of the [plan](plan.md). All of it is test data and test results of the step agents'
evals (`src/tests/evals/steps`); nothing here is stored by the platform.

## Held-out test

| Field | Rule |
|-------|------|
| Name | A Go test name `TestR<n>_<What>`; `<n>` is the rule it checks (FR-003) |
| Rule | One or more of the slice's rules (R-1 to R-3); the expected result follows from them and the contract alone (FR-002) |
| Input | One no example of `testdata/slice/slice.feature` shows |
| Entry point | `gen.Handler(app.New())` served by `httptest.NewServer`, as the slice's tests |

Lives in `testdata/heldout/heldout_test.go` (package `heldout_test`), never in a project an agent
sees (FR-004). The list is in [research R2](research.md#r2-held-out-tests-a-go-test-package-kept-in-the-evals-test-data-added-to-a-copy).

## Mutant

| Field | Rule |
|-------|------|
| Name | `<rule>/<what>`, unique |
| Rule | The one rule it breaks |
| Killed by | The example (E-1 to E-4) whose input reaches the bug (FR-006) |
| From, To | One line of the reference implementation `testdata/ref/impl/internal/app/app.go` and its replacement; `From` occurs exactly once |
| What | One sentence: the planted bug |

One table in the evals' package; the review cases' `plantR2` and `plantR3` are two of its rows
(research R1). Validation, in the self-tests: every `From` occurs once in the reference
implementation, every name is unique, every rule of the slice has at least one mutant (FR-007),
and the reference tests kill every mutant (SC-003).

## Grader result

As today (`grader`, `passed`, `reason`), plus `counts` for the two new graders
([contract](contracts/grader-results.md)):

| Field | Rule |
|-------|------|
| `counts.total` | The fixed list: 6 held-out tests or 8 mutants, not the events a run reported |
| `counts.passed` | `0 ≤ passed ≤ total`; held-out tests passed or mutants killed |
| `counts.failed` | `total − passed` names, sorted |

`passed` is true exactly when `counts` is present and `counts.passed == counts.total`.

## States of one variant run (both graders)

```text
built ──► ran: passed | failed | panicked | timed out
  └─► did not build | did not start
```

- Held-out test: passes only on `passed`.
- Mutant: killed only on `failed` or `panicked`, and only after the baseline (the reference
  implementation) ran `passed` for every test (FR-008, research R4).
