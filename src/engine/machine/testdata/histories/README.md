# Recorded run histories

Temporal histories of the `Run` workflow (`src/engine/durable`), replayed by
`workflow_replay_test.go` so that a change to the workflow code that breaks determinism fails in
CI's `go` job (research R2, R9; task T020).

| File | Run |
|------|-----|
| `uninterrupted.json.gz` | the stop-point suite's baseline: a run of the `resume` step set, no stop |
| `agent-step.json.gz` | the engine killed during the code step's agent call, then started again |
| `procedure-step.json.gz` | the engine killed during the generate step's procedure call |
| `review-round.json.gz` | the engine killed during the review round |
| `version-history-commit.json.gz` | the engine killed during the code step's commit |
| `agent-step-replay.json.gz` | `replayRun` of the continued agent-step run: every step reused |

Each stop-point history holds the activity's heartbeat timeout and the attempt made again.

How they were recorded (owner, 2026-11-20): on the owner's clean VM, from commit `eb6e1c8`,
trial 1 of `TestStopPoints` (`src/tests/acceptance/stop_points_test.go`) with a scratch-only edit
that, at the end of each trial, exported the run's history with the pinned
`temporal-admin-tools` CLI on the lab's durable network:

```sh
temporal workflow show --address durable:7233 --namespace ap --workflow-id RUN-<n> --output json
```

then `gzip -9n`. The histories carry identifiers, hashes and changed-line lists; none of the
recordings' prompt or completion text (FR-016, checked by the same trial). They are never
re-recorded to make a change pass: a change that alters the workflow's commands goes behind
`workflow.GetVersion`, so these histories still replay; new histories are added beside them.
