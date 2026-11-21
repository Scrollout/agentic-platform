# Quickstart: check that the step evals judge edge cases

Validation guide for the [spec](spec.md). Details of the results are in the
[contract](contracts/grader-results.md); the graders, held-out tests and mutants are in
[research](research.md).

## Prerequisites

As the step evals' [README](../../src/tests/evals/steps/README.md), "How to run": Go, the pinned
oapi-codegen and the module cache filled for godog and the oapi-codegen runtime. Section 3 also
needs Docker, the agents and the test-scoped key, and the owner's go-ahead (it costs money).

## 1. The graders prove they can fail (no model, no cost)

```sh
sh src/tests/evals/steps/run.sh graders
```

Expected:

- `HeldOutTestsPass` passes on the reference implementation with `6/6`, and fails on the planted
  implementation that answers only the examples, naming the held-out tests it failed (SC-001).
- `MutantsKilled` passes on the reference tests with every mutant killed, and fails on the planted
  weak test set, naming at least one surviving mutant (SC-002, SC-003).
- Every mutant's line occurs once in the reference implementation, and every rule has a mutant.
- The whole run takes at most 5 minutes longer than before (SC-004); the test prints its time.

## 2. The summary shows the new results (no model)

```sh
python3 -m unittest discover -s scripts/tests -p 'test_ci_evals_summary*.py'
```

Expected: the summary of a fixed report shows the `Held-out` and `Mutants killed` columns and the
failure lines; a report without counts prints as before (FR-016, FR-017).

## 3. A live run (owner's go-ahead)

Dispatch the evals workflow for the steps suite on the feature branch (the branch must be allowed
in the GitHub environment `dev`). Expected: the run summary shows held-out and mutant results for
every tests and code trial (SC-006), and each tests or code trial takes at most 2 minutes longer to
grade than in the last green run (SC-005). A case that fails a new grader leads to an instruction
change of its agent and another run (FR-019).
