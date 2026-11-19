---
type: Decision
title: "ADR 0067: A run is one Temporal workflow running the engine's step rules"
description: Each run is one workflow (ID = run ID) whose code is the engine's step rules and whose calls are activities with heartbeats; a stop repeats only the interrupted attempt, and the run continues by itself when the engine is back.
tags: [decisions, architecture, workflow]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's answers, 2026-11-19
  - id: research
    resource: ../../specs/002-durable-execution/research.md
    title: Feature 002 research
  - id: plan
    resource: ../../specs/002-durable-execution/plan.md
    title: Feature 002 plan, Decisions
  - id: temporal
    resource: /references/temporal.md
    title: Temporal reference
  - id: adr-0026
    resource: /decisions/0026-deterministic-workflow-engine.md
    title: "ADR 0026: deterministic workflow engine"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-19T09:00:00Z }
---

# Origin

Feature 002's [plan](../../specs/002-durable-execution/plan.md) adopts Temporal for the engine's durable execution, as [ADR 0056](/decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md) decided. The project owner decided on 2026-11-18 that a stopped run continues by itself and that attempts count across a stop (spec Clarifications).

Sources: [Project owner's answers, 2026-11-19](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 002 research](../../specs/002-durable-execution/research.md), [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).

# Context

- The engine's rules (steps, reuse, attempts, review rounds, budgets, hand-over) stay the platform's own (ADR 0056).
- Temporal resumes a workflow after a stop, keeps durable timers and signals, and requires deterministic workflow code: time through `workflow.Now`, randomness through a side effect, I/O in activities.
- A heartbeat timeout finds an activity whose worker stopped.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md).

# Decision

- One workflow per run on the task queue `runs`, started with `RejectDuplicate`.
- Each call (step worker, check, restore, commit, reuse lookup, artifact, event) is an activity with a StartToClose of its own time limit plus 30 s and a 30 s heartbeat timeout; Temporal's own retry is off (ADR 0069 retries).
- An activity cut off by a stop is not a failed attempt: the run records `RunInterrupted` and `RunResumed`, restores the project and makes the same attempt again.
- The time budget sums the activities' own running times, so downtime and waits never count; a hand-over waits for a signal with a 14-day durable timer.
- Workflow changes with runs in flight use `workflow.GetVersion`, and CI replays recorded histories.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md), [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).

# Consequences

- The engine's own resume, record replay into state, restart interruption and in-memory driver go away.
- Existing machine tests that drive the old driver are rewritten, each change approved by the owner first.
- After a reboot a run waits, interrupted, for its step workers and continues when `ap build-workers --resume` registers them.

Sources: [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).
