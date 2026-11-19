# Quickstart: check that runs and operations survive a platform stop

A validation guide for feature 002 ([spec](spec.md), [plan](plan.md)). It assumes feature 001's
and 003's quickstart: a machine with Docker 28+, gVisor and a model key, and the platform started
with `src/deploy/ap start` ([feature 001 quickstart](../001-prose-to-accepted-slice/quickstart.md)).
The automated form of each check is named; CI runs them on every push.

## 1. The platform starts with its durable store

```sh
src/deploy/ap start
docker compose -p agentic-platform ps --format '{{.Service}} {{.State}}'
```

Expected: `durable-store` and `durable` running, `durable-schema` exited 0, and the engine and
feature 001's services running. A second `ap start` changes nothing (feature 003). Idle memory of
`durable` and `durable-store` together at most 512 MiB (`docker stats --no-stream`; SC-004).

## 2. A run continues by itself after the engine stops (US1, SC-001)

```sh
src/deploy/ap build <slice> <project> &      # a slice approved as in feature 001
# while an agent step is attempted (the event lines show `AttemptMade` pending):
docker compose -p agentic-platform kill engine
docker compose -p agentic-platform start engine
src/deploy/ap run <run>                      # follow it again
```

Expected: the record shows `RunInterrupted` (`the platform stopped`) and `RunResumed`
(`continued after the platform stopped`), the interrupted attempt again with the same number, no
accepted step twice, and the run ends `done` with the artifacts an uninterrupted run of the same
slice gives (`ap compare`). Nobody typed `ap resume`. Automated: the stop-point acceptance suite
(engine killed during an agent step, a procedure step, a review round and a commit; 3 trials each)
in the `e2e` job.

## 3. An accepted operation is not lost (US2, SC-002)

```sh
# start any state-changing request, note its OP-<n>, then at once:
docker compose -p agentic-platform kill engine && docker compose -p agentic-platform start engine
# follow it with getOperation (the client token from the session), as the docker job's tests do
```

Expected: the same `OP-<n>` ends `succeeded`, `failed` or `cancelled`; the next request gets an ID
never used before. With `durable` stopped, a state-changing request answers 503 `Unavailable`
with `Retry-After` and nothing is accepted; the reads still answer. Automated: the operations
integration tests in the `docker` job.

## 4. Nothing feature 001 delivered changed (US3, SC-003)

Every CI job that judged feature 001 is green on this feature's commit, the planted suite 3 of 3
trials, and the eval suites (run by hand, owner's go-ahead). `oasdiff` reports no breaking change
against the engine contract of feature 001 (`API breaking changes` job).

## 5. Erasing a slice leaves nothing (SC-006)

Delete a slice whose runs have ended; then no row with its run or operation IDs is left in the
`durable-store` tables `executions`, `current_executions`, `history_node` and
`executions_visibility`, and nothing under its folder in the engine store. Automated: the erasure
test in the `docker` job.
