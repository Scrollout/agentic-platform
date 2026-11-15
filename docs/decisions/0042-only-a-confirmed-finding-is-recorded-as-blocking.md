---
type: Decision
title: "ADR 0042: Only a finding the check confirmed is recorded as blocking"
description: NoBlockingFindingOpen's verdict names the findings its witness runs confirmed (`confirmed`, positions in the request's findings); the engine records exactly those as blocking and every other finding, the agent's unconfirmed claims included, as a suggestion, and only confirmed findings reach the fix, the next round's reuse key, the hand-over and the run's open findings. Amends what ADR 0039 means by blocking in the run record.
tags: [decisions, engine, checks, review]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decisions and answers, 2026-11-15
  - id: spec
    resource: ../../specs/001-prose-to-accepted-slice/spec.md
    title: Feature 001 spec, FR-017
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D39 (with D35, D36)
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: adr-0039
    resource: /decisions/0039-review-findings-confirmed-by-a-new-witness-test.md
    title: "ADR 0039: A blocking review finding is confirmed by a new failing witness test the review writes"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-15T16:00:00Z }
---

# Origin

T057's fresh review of Story 2 (2026-11-15) found that the engine built a review round's open
findings from the agent's own `blocking: true`, so a claim no witness confirmed was given to the
fix, entered the next round's reuse key and appeared in the hand-over (R5). The owner decided on
2026-11-15 to fix it now; research D39 holds the design.

Sources: [Feature 001 research, D39](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 tasks, T057](../../specs/001-prose-to-accepted-slice/tasks.md).

# Context

- FR-017: a finding blocks only when a check confirms its evidence; any other is a suggestion.
- The check service runs the witnesses and decides which findings are confirmed
  ([ADR 0039](/decisions/0039-review-findings-confirmed-by-a-new-witness-test.md)), but its
  verdict said so only in free text.
- The step adapter drops some findings before the call, so the request's findings are not always
  the attempt's.

Sources: [Feature 001 spec, FR-017](../../specs/001-prose-to-accepted-slice/spec.md); [Feature 001 research, D35](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- NoBlockingFindingOpen's verdict carries `confirmed`: the positions, in the request's
  `findings`, of the findings its witness runs confirmed; empty when it passes.
- The engine maps them back to the attempt's findings and records exactly those with `blocking:
  true`; every other finding is recorded with `blocking: false`, a suggestion.
- A review round is open only when NoBlockingFindingOpen is the only failed check and it
  confirmed at least one finding.
- Only recorded blocking findings go to the fix, into the next round's reuse key, the hand-over
  reason and the run's open findings. A kept open round (ADR 0040) keeps the recorded findings.
- The `findings` artifact stays the agent's answer as given; the record holds the verdict.
- Rejected: reading the confirmed set from the verdict's reason text; returning the findings as
  objects; the engine running the witnesses itself.

Sources: [Feature 001 research, D39](../../specs/001-prose-to-accepted-slice/research.md).

# Consequences

- In the run record, `blocking` now means confirmed, not claimed; this amends what ADR 0039 means
  by a blocking finding in the record. Its confirmation rule is unchanged.
- The check contract's verdict and the run event's `blocking` description change first (T092);
  the machine, the step adapter and the check service follow (T096).
- Results kept before T096 hold the agent's claims as blocking; a replay of such a run
  reproduces them.

Sources: [Feature 001 tasks, T092 and T096](../../specs/001-prose-to-accepted-slice/tasks.md); [ADR 0039](/decisions/0039-review-findings-confirmed-by-a-new-witness-test.md).
