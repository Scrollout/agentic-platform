---
type: Decision
title: "ADR 0105: The engine's preview contract gains the operation kind run-trials in place"
description: Feature 012 adds the OperationKind value run-trials to the engine contract's 2026-11-12-preview in place, an owner-approved exception to Principle XV, as ADR 0100 did for feature 011.
tags: [decisions, apis, variance]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/012-run-variance/plan.md
    title: Feature 012 plan, Complexity Tracking
  - id: research
    resource: ../../specs/012-run-variance/research.md
    title: Feature 012 research, R9
  - id: adr-0100
    resource: /decisions/0100-preview-contract-gains-enum-values-in-place.md
    title: "ADR 0100: The engine's preview contract gains enum values in place"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-27T18:30:00Z }
---

# Origin

The project owner's approval of 2026-11-27, recorded in the
[feature 012 plan](../../specs/012-run-variance/plan.md), Complexity Tracking.

Sources: [Feature 012 plan, Complexity Tracking](../../specs/012-run-variance/plan.md).

# Context

- A trial set is an operation whose target is the set, not a run, so it needs its own kind.
- The pinned oasdiff reports an added response enum value as breaking; every client of the engine
  contract is generated from it in the same commit
  ([ADR 0100](/decisions/0100-preview-contract-gains-enum-values-in-place.md)).

Sources: [Feature 012 research, R9](../../specs/012-run-variance/research.md).

# Decision

- `OperationKind` in the engine contract's `2026-11-12-preview` gains `run-trials` in place.
- Every other change of feature 012 to the contract is additive.

Sources: [Feature 012 research, R9](../../specs/012-run-variance/research.md).

# Consequences

- No second preview version is served; a client built from the old contract that meets the new kind
  sees an unknown enum value.
- The contract-diff check's report of this value is expected for this change.

Sources: [Feature 012 plan](../../specs/012-run-variance/plan.md).
