# Quickstart: validating feature 007

How to see that the platform refuses an analyst answer that repeats a key and a revision that
loses items. Paths are from the repository root.

## Engine tests (no model, no Docker)

```sh
cd src/engine && go test ./slices/ ./analyst/
```

Expected: the new tests pass. They prove, with stub analysts: an answer with a repeated key (top
level, nested, same value twice) is asked for again and accepted on a good second answer; one
that repeats on every attempt fails `OperationFailed` naming the key; a revision that drops R-1,
re-homes R-2 or moves an example is asked for again, and fails after the last attempt with the
slice unchanged ([data model](data-model.md)).

## Analyst evals, stub run (Docker, no model, no key)

```sh
sh src/tests/evals/analyst/run.sh stub
```

Expected: `good-revised` passes with 1 attempt per trial; `bad-revised-dropped-rules` and
`bad-revised-repeated-key` fail every trial after 3 attempts (the pattern's `limits.attempts`).

## Analyst evals, live run (paid; only with the owner's go-ahead)

Dispatched in CI (`evals` workflow, analyst suite). Expected: every analyst and revision case
passes pass^3, none quarantined (SC-003); the report shows each revision trial's attempts.
