---
type: Decision
title: "ADR 0055: A later review round blocks only within its scope, judged by the check"
description: In a review round after the first, a confirmed finding blocks only when it repeats an open finding the round was given or sits on a line the fix before the round changed; NoBlockingFindingOpen applies the rule from the scope the engine sends, and any other confirmed finding is a suggestion.
tags: [decisions, review, checks]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's answer, /speckit-clarify, 2026-11-17
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D55
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: tasks
    resource: ../../specs/001-prose-to-accepted-slice/tasks.md
    title: Feature 001 tasks, T135
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-17T19:30:00Z }
---

# Origin

The project owner's answer in `/speckit-clarify` on 2026-11-17, after the whole-feature converge
(T070, round 1) found that the rule for later review rounds was only in the review agent's
instructions. Recorded in the [plan's Decisions](../../specs/001-prose-to-accepted-slice/plan.md)
as research D55. It narrows what
[ADR 0042](/decisions/0042-only-a-confirmed-finding-is-recorded-as-blocking.md) records as
blocking in rounds after the first; ADR 0039 and ADR 0042 otherwise stand.

Sources: [Project owner's answer, /speckit-clarify, 2026-11-17](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 001 research, D55](../../specs/001-prose-to-accepted-slice/research.md).

# Context

The constitution's "Review converges" says a review round after the first checks only the open
findings and the lines the fixes changed, and may raise a new blocking finding only for a
regression those fixes caused. Principle XX says the platform applies the project's rules, not
the agent's instructions. A finding blocks only when its new witness test confirms it
([ADR 0039](/decisions/0039-review-findings-confirmed-by-a-new-witness-test.md)), but in rounds 2
and 3 a confirmed finding on a line no fix changed still blocked.

Sources: [Feature 001 research, D55](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- In a review round after the first, a finding its witness confirms blocks only when it repeats an
  open finding the round was given (the same rule in the same file) or its line is among the
  lines the fix before the round changed.
- Any other confirmed finding is recorded as a suggestion.
- The check NoBlockingFindingOpen applies the rule. The engine sends the round's scope in the
  check request's `round_scope`, taken from the run, never from a worker's answer. The changed
  lines are those the engine computes from the fix's files.

# Consequences

- A later round cannot add new demands on code no fix touched, so a review converges by rule, not
  by the prompt.
- A regression on a line no fix changed, such as a caller broken by a changed function, is only a
  suggestion (a stated limit the owner accepted).
- A repeat is matched by rule and file, since the fix moves lines, so a finding of an open rule at
  another line of the same file still blocks (a stated limit).
- The planted non-converging review (SC-003, US2.6) must raise each later round's finding on a line
  the fix before it changed.

Sources: [Feature 001 tasks, T135](../../specs/001-prose-to-accepted-slice/tasks.md).
