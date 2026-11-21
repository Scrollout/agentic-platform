---
type: Decision
title: "ADR 0075: Eval mutants are planted by hand, not generated"
description: The mutants that judge the tests agent are one-line bugs planted by hand in the reference implementation, each breaking one rule on an input an example exercises, declared in one table that the review cases share; no mutation tool generates them.
tags: [decisions, evals]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/004-edge-case-graders/plan.md
    title: Feature 004 plan, Decisions
  - id: research
    resource: ../../specs/004-edge-case-graders/research.md
    title: Feature 004 research, Principle VI evaluation and R1
  - id: tools
    resource: /references/go-mutation-testing-tools.md
    title: Go mutation testing tools
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-21T16:00:00Z }
---

# Origin

The [feature 004 plan](../../specs/004-edge-case-graders/plan.md) made this decision for backlog
task T180, which the project owner approved on 2026-11-20.

Sources: [Feature 004 plan](../../specs/004-edge-case-graders/plan.md).

# Context

- The tests agent's evals must fail a test set that binds every example but checks too little.
  Running its tests against mutants of a correct implementation does that, but only if every
  mutant is one that tests derived from the examples can kill.
- The Go mutation tools (gremlins, go-mutesting) apply generic operators to any line. Some of
  their mutants are equivalent or reach no example, so "kill every mutant" cannot be the bar, and
  none plants a mutant the evals define ([Go mutation testing tools](/references/go-mutation-testing-tools.md)).
- The review cases already plant two bugs (`plantR2`, `plantR3`) by replacing one line of the
  reference implementation.

Sources: [Feature 004 research](../../specs/004-edge-case-graders/research.md).

# Decision

- Each slice the evals use has one table of mutants: a name, the rule it breaks, the example that
  kills it, the line replaced and its replacement. The review cases' plants are rows of it.
- A tests trial passes only when its tests kill every mutant of the table.
- No mutation tool is adopted for the evals.

Sources: [Feature 004 research, R1](../../specs/004-edge-case-graders/research.md).

# Consequences

- A new slice in the evals needs its own mutants written by hand; the self-tests check that the
  reference tests kill each one and that each planted line exists.
- The mutants say nothing about how much of a real project's code its tests check; a mutation
  tool stays a candidate for that need, evaluated when it arises.

Sources: [Feature 004 plan](../../specs/004-edge-case-graders/plan.md).
