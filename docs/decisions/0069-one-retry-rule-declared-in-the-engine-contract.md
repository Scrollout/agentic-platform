---
type: Decision
title: "ADR 0069: One retry rule, declared in the engine contract"
description: The retry rule (3 retries, first wait 1 s doubling to at most 30 s, up to half again of random jitter) is declared once in the engine contract's x-limits and applied by one small Go module, with durable timers in workflows and in the terminal client; Temporal's activity retry is not used.
tags: [decisions, architecture, workflow, api]
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
  - id: aws-backoff
    resource: https://aws.amazon.com/builders-library/timeouts-retries-and-backoff-with-jitter/
    title: "AWS Builders' Library: Timeouts, retries and backoff with jitter"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-19T09:00:00Z }
---

# Origin

Feature 002's [plan](../../specs/002-durable-execution/plan.md) adopts Temporal for the engine's durable execution, as [ADR 0056](/decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md) decided. The project owner chose one declared rule on 2026-11-19 (research R5). It closes feature 001's Single-owner exception for the retry wait.

Sources: [Project owner's answers, 2026-11-19](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 002 research](../../specs/002-durable-execution/research.md), [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).

# Context

- Feature 001 had three hand-written backoff forms (operations, version-history calls, the client's resends).
- Temporal documents no jitter for activity retries, and its code adds none; Principle IV requires backoff with jitter.
- The terminal contract already publishes the client's waits: 1, 2 and 4 s, each with up to half again of jitter.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md).

# Decision

- `x-limits` gains `retry-attempts` 3, `retry-first-wait-ms` 1000, `retry-longest-wait-ms` 30000 and `retry-jitter-fraction` 0.5.
- The module `src/retry` computes each wait; the engine's workflows wait on `workflow.Sleep` with the random part from a side effect, and the client sleeps; a contract test proves both read the contract's values.
- Every activity has Temporal's retry off (`MaximumAttempts: 1`).

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md), [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).

# Consequences

- One rule, one home; the published client behaviour is unchanged.
- The tests that pinned the removed forms change, each with the owner's approval.

Sources: [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).
