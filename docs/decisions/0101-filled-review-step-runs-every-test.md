---
type: Decision
title: "ADR 0101: A captured pattern's filled review step runs every test"
description: Refines ADR 0090 in part. The filled review step of a captured pattern also declares AllTestsPass, so the witness tests it writes run against the filled code before a captured run can end done.
tags: [decisions, catalogue, captured-patterns]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/011-capture-pattern/plan.md
    title: Feature 011 plan, Decisions
  - id: research
    resource: ../../specs/011-capture-pattern/research.md
    title: Feature 011 research, R1
  - id: adr-0090
    resource: /decisions/0090-captured-pattern-fills-agent-steps-from-accepted-files.md
    title: "ADR 0090: A captured pattern fills its agent steps from the accepted files"
  - id: adr-0039
    resource: /decisions/0039-review-findings-confirmed-by-a-new-witness-test.md
    title: "ADR 0039: A blocking review finding is confirmed by a new failing witness test the review writes"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-27T14:00:00Z }
---

# Origin

Feature 011's final fresh review (T025, round 1, 2026-11-27, finding B1), recorded in the
[feature 011 plan](../../specs/011-capture-pattern/plan.md), Decisions; the project owner chose
to fix it on 2026-11-27.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).

# Context

- [ADR 0090](/decisions/0090-captured-pattern-fills-agent-steps-from-accepted-files.md) drops
  FindingsCarryEvidence and NoBlockingFindingOpen from the filled review step and keeps its other
  checks. None of those runs a test: TestsBuild only compiles them.
- The filled review step writes the source run's witness tests (`review/r<n>/`) after the code
  step, the only step that declares AllTestsPass. In the base pattern a fix round re-runs the code
  step with the witnesses present
  ([ADR 0039](/decisions/0039-review-findings-confirmed-by-a-new-witness-test.md)); a captured
  run has no fix round.
- So a filled witness that fails against the filled code would leave a captured run done, against
  FR-009 and research R1's rationale (the same tests decide a filled build). The proof cannot see
  it: it rebuilds with the source values, where the witness passed.

Sources: [Feature 011 research, R1](../../specs/011-capture-pattern/research.md).

# Decision

- The filled review step keeps the base review step's checks except FindingsCarryEvidence and
  NoBlockingFindingOpen, and adds AllTestsPass after them.
- The base pattern is unchanged.

Sources: [Feature 011 research, R1](../../specs/011-capture-pattern/research.md).

# Consequences

- A captured run ends done only when every test in the project, the witnesses included, passes
  against the filled code.
- Each existing captured pattern's declaration is made at capture, so one captured before this
  change keeps its declaration, and the method change leaves it unusable (FR-011); none exists
  outside development.
- The method identity changes, so the regression evals run again.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).
