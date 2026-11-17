---
type: Decision
title: "ADR 0056: The MVP keeps its own durable execution; Temporal replaces it after the MVP"
description: The engine's step machine, run store and job queue cover what durable-execution engines provide; they stay for the MVP with a written justification, and the first task after the MVP adopts Temporal for them.
tags: [decisions, architecture, workflow, oss]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's answer, 2026-11-17
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D56
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions and Complexity Tracking
  - id: temporal
    resource: /references/temporal.md
    title: Temporal reference
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-17T21:30:00Z }
---

# Origin

The project owner asked on 2026-11-17 whether the platform used mature open source instead of
writing its own. The answer found that the engine's durable execution had never been evaluated
against open source, as Principle VI requires. The owner agreed to keep it for the MVP and adopt
Temporal after it. Recorded in the
[plan's Decisions](../../specs/001-prose-to-accepted-slice/plan.md) as research D56. It refines
[ADR 0026](/decisions/0026-deterministic-workflow-engine.md), which says machines run on a durable
engine without naming one; ADR 0026 otherwise stands.

Sources: [Project owner's answer, 2026-11-17](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 001 research, D56](../../specs/001-prose-to-accepted-slice/research.md).

# Context

- The engine resumes runs, checkpoints each step, retries with backoff and jitter, keeps run
  history in its own store and queues operations for workers. Durable-execution engines provide
  all of this.
- The plan set them aside in one line, without the licence, Scorecard and project-health
  evaluation Principle VI requires.
- Temporal (MIT, Scorecard 6.1 for the server and 7.0 for the Go SDK, active) covers all three
  parts and runs self-hosted in Compose and offline. DBOS Transact Go has no published Scorecard;
  River is a queue only.

Sources: [Temporal reference](/references/temporal.md), [Feature 001 research, D56](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- The MVP keeps its own step machine, run store and job queue, an approved Principle VI exception
  in the plan's Complexity Tracking, for the MVP only.
- The first task after the MVP adopts Temporal for durable execution, history, retries,
  timeouts and the job queue.
- The engine's checks, review rules, reuse keys, slices and artifacts stay the platform's own,
  as the workflow and activities Temporal runs.
- Artifacts travel by hash, never inside Temporal's history.

Sources: [Feature 001 research, D56](../../specs/001-prose-to-accepted-slice/research.md), [Feature 001 plan, Decisions and Complexity Tracking](../../specs/001-prose-to-accepted-slice/plan.md).

# Consequences

- The MVP ships on code that is built and green in CI, without a rewrite before acceptance.
- After the MVP, the step machine, run store and job queue shrink to the platform's own rules,
  and the retry waits get one owner.
- Adopting Temporal adds a Temporal server and a database container. Rewriting the machine
  changes existing tests, each with the owner's approval.

Sources: [Feature 001 plan, Decisions and Complexity Tracking](../../specs/001-prose-to-accepted-slice/plan.md).
