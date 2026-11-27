---
type: Decision
title: "ADR 0090: A captured pattern fills its agent steps from the accepted files"
description: Refined in part by ADR 0101 (the filled review step also runs AllTestsPass). A captured pattern is its base pattern with each agent step's worker replaced by the fixed procedure fill, which writes the source run's accepted files with the parameters filled in; generated code is derived again and the review step keeps only the checks that need no findings.
tags: [decisions, catalogue, captured-patterns]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/011-capture-pattern/plan.md
    title: Feature 011 plan, Decisions
  - id: research
    resource: ../../specs/011-capture-pattern/research.md
    title: Feature 011 research, R1 and R2
  - id: spec
    resource: ../../specs/011-capture-pattern/spec.md
    title: Feature 011 specification, Clarifications
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-25T23:00:00Z }
---

# Origin

The [feature 011 plan](../../specs/011-capture-pattern/plan.md), Decisions, from the project
owner's answers of 2026-11-25 ([specification](../../specs/011-capture-pattern/spec.md),
Clarifications).

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).

# Context

> Refined in part by [ADR 0101](/decisions/0101-filled-review-step-runs-every-test.md): the filled review step also declares AllTestsPass, so its witness tests run against the filled code.

- The owner wants a later slice of the same kind built with no model call (feature 011).
- The run machine needs no agent step, and the base pattern's step boundaries and checks already
  judge what an agent writes.

Sources: [Feature 011 research, R1 and R2](../../specs/011-capture-pattern/research.md).

# Decision

- Each agent step (tests, code, review) of the base pattern gets the worker `procedure:fill`;
  names, order, writable paths, dependencies, limits and checks stay the base pattern's.
- The review step drops FindingsCarryEvidence and NoBlockingFindingOpen, which read findings no
  agent writes.
- The template is the source run's `tests`, `code` and `witnesses` outputs; `gen/**` and the
  module files are derived again by the generate procedure from each slice's contract.
- The engine renders a filled step's files; the project worker writes them with `writeStep`
  before the step's checks, then `commitStep` as for any step.

Sources: [Feature 011 research, R1 and R2](../../specs/011-capture-pattern/research.md).

# Consequences

- A captured run is judged by the same checks as the source run, without a review agent.
- oapi-codegen's naming of new names is judged by the build and the tests, not by the
  substitution.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).
