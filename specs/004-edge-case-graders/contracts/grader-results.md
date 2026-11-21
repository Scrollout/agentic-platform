# Contract draft: the two new graders' results, in the eval report and its summary

Draft for the first consumer, `scripts/ci-evals-summary.py`. Its home is the step evals'
[README](../../../src/tests/evals/steps/README.md) ("Graders" and "The report"), where it moves
at implementation (Contracts between components: the provider keeps the authoritative copy). It
grows only additively.

## What it does for the caller

It tells the owner, for every tests and code trial, how many held-out tests the agent's code
passed and how many mutants the agent's tests killed, and which ones failed, so that a red trial
says why without opening its files.

## The report (`report.json`)

Unchanged, except that a grader result (`trials[].results[]`) may carry `counts`:

```json
{
  "grader": "MutantsKilled",
  "passed": false,
  "reason": "7 of 8 mutants killed; survived: R-2/always-one",
  "counts": { "total": 8, "passed": 7, "failed": ["R-2/always-one"] }
}
```

| Field | Meaning |
|-------|---------|
| `counts` | Present only on `HeldOutTestsPass` and `MutantsKilled`, and only when the grader got as far as running its variants. Absent when it stopped earlier; then `reason` says why (`not run: <grader> failed` when an earlier grader of the trial failed; the tests do not pass against the reference implementation; the code does not build) |
| `counts.total` | The fixed list judged: the held-out file's 6 tests, or the table's 8 mutants; never the number of events a run happened to report |
| `counts.passed` | Held-out tests passed, or mutants killed. `0 ≤ passed ≤ total` |
| `counts.failed` | The names of the held-out tests that failed, or of the mutants that survived, sorted; `total − passed` entries. A name is followed by `(timed out)` or `(did not run)`, after a space, when that is the reason |

The grader `passed` is true exactly when `counts` is present and `passed == total`.

Grader names: `HeldOutTestsPass` (every code case), `MutantsKilled` (every tests case). Held-out
test names are Go test names starting with their rule (`TestR1_EmptyList`); mutant names are
`<rule>/<name>` (`R-2/always-one`).

## The summary (`ci-evals-summary.py`)

- When no trial in the report has `counts`, the output is byte for byte what it is today.
- Otherwise the case table gains two columns after `pass^3`:

  ```text
  | Case | Agent | pass^3 | Held-out | Mutants killed |
  | code/example | code | ❌ fail | 6/6, 4/6, 6/6 | n/a |
  | tests/example | tests | ✅ pass | n/a | 8/8, 8/8, 8/8 |
  | review/clean | review | ✅ pass | n/a | n/a |
  ```

  A column lists the case's trials in order. `n/a`: the grader does not judge that agent. `—`: a
  trial of that agent that reports no counts (the grader stopped before its variants; its reason
  is in the failure lines).
- After the table, one line per trial with a failed held-out test, a surviving mutant, or a
  stopped new grader:

  ```text
  - code/example trial 2: held-out failed: TestR1_EmptyList, TestR1_ThreeInOrder
  - tests/inject-feature-done trial 1: MutantsKilled: the tests do not pass against the reference implementation
  ```

- Quarantine, the totals line, tokens and cost, and the exit code are unchanged: a counted case
  fails the summary exactly when one of its trials failed, as today.

## Limits

- The counts say what the graders saw on this slice's fixed held-out tests and mutants; they are
  not a coverage measure of the agent's tests, and no threshold below `total` passes.
- A held-out test or mutant name is stable only within one version of the evals.
