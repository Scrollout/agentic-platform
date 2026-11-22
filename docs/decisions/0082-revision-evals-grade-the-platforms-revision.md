---
type: Decision
title: "ADR 0082: The revision evals grade the platform's revision"
description: Each trial of the analyst's revision evals goes through the slices service's Answer with the pattern's retry, as the description cases go through Create; the last attempt's answer is graded, a failed Answer fails the trial, and each trial reports its attempts.
tags: [decisions, evals, analyst]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/007-analyst-answer-checks/plan.md
    title: Feature 007 plan, Decisions
  - id: research
    resource: ../../specs/007-analyst-answer-checks/research.md
    title: Feature 007 research, R3
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-22T22:00:00Z }
---

# Origin

The plan of [feature 007](../../specs/007-analyst-answer-checks/plan.md) (backlog T195 and T196);
the owner chose that the revision evals grade the platform's revision (2026-11-22, spec FR-006).

Sources: [Feature 007 plan, Decisions](../../specs/007-analyst-answer-checks/plan.md).

# Context

- The description cases already run through the service's `Create` with its retry; the revision
  cases graded one raw analyst call, so a failure the platform would have refused and asked
  again for still failed the case and blocked merges.

Sources: [Feature 007 research, S8 and R3](../../specs/007-analyst-answer-checks/research.md).

# Decision

- A revision trial creates the slice from the fixed earlier proposal and calls `Answer` with the
  case's answers; the live analyst answers each attempt.
- The last attempt's answer is graded by every revision grader; a failed `Answer` fails the
  trial; the trial reports the number of analyst calls.
- Planted stub answers (a repeated key, dropped rules) must fail every trial after the pattern's
  attempts; a good one must pass in one attempt.

Sources: [Feature 007 research, R3](../../specs/007-analyst-answer-checks/research.md).

# Consequences

- The evals judge what the developer gets; a case still fails when every attempt is unusable.
- A refused revision costs up to the pattern's attempts in calls; attempts in the report show
  how often the platform had to ask again.

Sources: [Feature 007 plan](../../specs/007-analyst-answer-checks/plan.md).
