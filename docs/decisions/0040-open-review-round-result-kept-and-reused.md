---
type: Decision
title: "ADR 0040: An open review round's result is kept and reused like an accepted step's"
description: A review round left with blocking findings open keeps its result (findings, witnesses, project files, its NoBlockingFindingOpen result) under its reuse key, which covers the round, marked open; a later run of the same input reuses it as a ResultReused with open_round, so the fix and the later rounds are reused too and a replay with more than one review round is identical. Reuse runs no check; resume after an open round goes straight to the fix.
tags: [decisions, engine, reuse, review]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decision and answers, 2026-11-15
  - id: spec
    resource: ../../specs/001-prose-to-accepted-slice/spec.md
    title: Feature 001 spec, FR-019, US2.8, SC-005
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D36 (with D8, D35)
  - id: data-model
    resource: ../../specs/001-prose-to-accepted-slice/data-model.md
    title: Feature 001 data model, An open round reused
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: adr-0039
    resource: /decisions/0039-review-findings-confirmed-by-a-new-witness-test.md
    title: "ADR 0039: A blocking review finding is confirmed by a new failing witness test the review writes"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-15T13:00:00Z }
---

# Origin

T053a's live end-to-end run through `ap` on 2026-11-15 found that a replay of a run with two
review rounds was not identical: the replay ran review round 1, the fix and round 2 live, and a
witness came out under another name. An open round kept no result, so the replay ran it again,
and its new finding text changed the fix's reuse key. The owner decided on 2026-11-15 that an
open round keeps its result like an accepted step, and answered the design's questions the same
day; research D36 holds the design.

Sources: [Feature 001 research, D36](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 tasks, T053a](../../specs/001-prose-to-accepted-slice/tasks.md).

# Context

- A step's result is reused under a key of its pattern version, step, input hashes, agent digest
  and model; a fix's and a later round's inputs include the hash of the open findings and changed
  lines they are given.
- A round with only NoBlockingFindingOpen failed is recorded as `StepRejected` and committed with
  its witnesses ([ADR 0039](/decisions/0039-review-findings-confirmed-by-a-new-witness-test.md)).
- Agent output differs between runs, so only reuse makes a replay identical (FR-019, SC-005).

Sources: [Feature 001 research, D8 and D35](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 spec](../../specs/001-prose-to-accepted-slice/spec.md).

# Decision

- An open round's result (its artifacts, project files, findings in recorded order and its
  NoBlockingFindingOpen result) is kept under its reuse key, marked open; its `StepRejected`
  carries `open_round`, `reuse_key` and `artifacts`, so a restarted engine rebuilds it.
- The review step's key also covers its round, since a round's result is judged against that
  round's `before`.
- A later run of the same input records the kept findings as `FindingRecorded` events and then a
  `ResultReused` with `open_round`, the kept check result and the round's detail. The round is not
  accepted; the fix gets the same open findings, so it and the later rounds are reused too.
- The reused round is committed with `open_round` and `reused_from`; its subject says the round was
  left open.
- Resume after an open round makes its commit again and goes straight to the fix, or the
  hand-over at the last round; the round is not run again.
- Reuse runs no check, for an open round as for an accepted step; the witnesses are not
  re-confirmed.
- Rejected: recording the reuse as `StepRejected`; a new event kind; rerunning the review's checks
  on reuse; keeping the findings only in the `findings` artifact; a key without the round.

Sources: [Feature 001 research, D36](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 data model, An open round reused](../../specs/001-prose-to-accepted-slice/data-model.md).

# Consequences

- A replay of a run with any number of review rounds reuses every step and round, so its
  artifacts and commit trees equal the original's (US2.8, SC-005).
- The run-event schema, the check contract's commitStep and the terminal contract grow
  additively (T089); the machine, the engine API, the history client, the check service and the
  terminal client change (T088).
- A reused open round's evidence is in the original run's record, not the replay's.
- Every review round's reuse key changes once, so results kept before T088 are not reused for
  review rounds.
- Resume no longer runs an open round again, a change from earlier behaviour.

Sources: [Feature 001 tasks, T088 and T089](../../specs/001-prose-to-accepted-slice/tasks.md); [Feature 001 plan, Decisions](../../specs/001-prose-to-accepted-slice/plan.md).
