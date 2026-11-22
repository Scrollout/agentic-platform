---
type: Decision
title: "ADR 0081: The platform refuses ambiguous analyst answers and revisions that lose items"
description: An analyst answer that repeats an object key, and a revision that drops or moves a goal, term, rule, example or operation of the current proposal, are unusable; the analyst is asked again within the pattern's attempts, then the request fails with OperationFailed and nothing changes. An item is removed only by changing its part.
tags: [decisions, analyst, slices, security]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/007-analyst-answer-checks/plan.md
    title: Feature 007 plan, Decisions
  - id: research
    resource: ../../specs/007-analyst-answer-checks/research.md
    title: Feature 007 research, R1 and R2
  - id: spec
    resource: ../../specs/007-analyst-answer-checks/spec.md
    title: Feature 007 specification, Clarifications
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-22T22:00:00Z }
---

# Origin

The plan of [feature 007](../../specs/007-analyst-answer-checks/plan.md) (backlog T195 and T196,
owner 2026-11-22); the owner chose that a revision through answers never removes an item.

Sources: [Feature 007 plan, Decisions](../../specs/007-analyst-answer-checks/plan.md); [Feature 007 specification, Clarifications](../../specs/007-analyst-answer-checks/spec.md).

# Context

- An analyst answer held the key `examples` twice; Go's `encoding/json` kept the last, nearly
  empty value silently, and the revision lost its scenarios (T195).
- Injected text in a developer's answer made the analyst drop two rules, a goal and their
  scenarios; the platform accepted the revision (T196).
- Principle XIV requires defences that assume the injection succeeds.

Sources: [Feature 007 research, E1, R1 and R2](../../specs/007-analyst-answer-checks/research.md).

# Decision

- The analyst's answer is parsed by one function, which refuses an answer whose objects hold a
  key twice at any depth.
- A revision must keep every goal, term, rule, example and operation ID of the current proposal,
  each rule serving the same goal and each example under the same rule.
- Both are unusable proposals: the analyst is asked again with faults that name keys and IDs only,
  within the pattern's `limits.attempts`; after the last attempt the request fails with
  `OperationFailed` and the slice is unchanged.
- A developer removes an item by changing its part directly.

Sources: [Feature 007 research, R1 and R2](../../specs/007-analyst-answer-checks/research.md).

# Consequences

- An answer cannot delete the developer's items, whatever it makes the analyst do.
- A revision can no longer break another part's links, so withdrawing an approval after an
  answer remains only for other failing checks.
- The duplicate-key walk is custom code over the standard library until the platform moves to
  Go 1.27, whose stable `encoding/json/v2` rejects duplicate names (backlog T197).

Sources: [Feature 007 plan](../../specs/007-analyst-answer-checks/plan.md); [Go JSON duplicate names](/references/go-json-duplicate-names.md).
