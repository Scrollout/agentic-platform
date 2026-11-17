---
type: Decision
title: "ADR 0057: Go 1.26 is the stack for all platform code"
description: All platform code is Go 1.26, one stack for the platform because Docker Agent and its SDK are Go; this is the decision the constitution's Stack rule asks for.
tags: [decisions, stack, go]
status: draft
decision: accepted
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D1
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-17T22:00:00Z }
---

# Origin

Feature 001 chose its stack in the [plan](../../specs/001-prose-to-accepted-slice/plan.md)'s
research as D1. D1 left a follow-up: a stack that sets a pattern needs a decision record
(task T060). This is that record.

Sources: [Feature 001 research, D1](../../specs/001-prose-to-accepted-slice/research.md), [Feature 001 plan, Decisions](../../specs/001-prose-to-accepted-slice/plan.md).

# Context

- The constitution's Stack rule says custom code uses the best stack for its job, that fit with
  Docker Agent's Go SDK counts, that each extra stack costs tooling, skills and patching, and that
  a stack which sets a pattern for several components gets a decision.
- Docker Agent and its SDK are Go. The spike's engine, guards and generator ran on Go 1.26.
- The spike's driver was Python. TypeScript was the other candidate.

Sources: [Feature 001 research, D1](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- All platform code is Go 1.26.
- Python and TypeScript are not used for platform code, since each would be a second stack for no
  gain.

Sources: [Feature 001 research, D1](../../specs/001-prose-to-accepted-slice/research.md).

# Consequences

- One toolchain, one set of pins and one set of checks cover the platform's modules, which
  `src/go.work` joins (`go 1.26.0`).
- A later component in another language needs its own decision that shows what it gains over Go.

Sources: [Feature 001 plan, Decisions](../../specs/001-prose-to-accepted-slice/plan.md).
