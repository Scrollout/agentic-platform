---
type: Decision
title: "ADR 0100: The engine's preview contract gains enum values in place"
description: Feature 011 adds error codes, operation kinds and a slice history kind to the engine contract's 2026-11-12-preview in place, an owner-approved exception to Principle XV, although the pinned oasdiff reports a response enum value added as breaking.
tags: [decisions, apis, captured-patterns]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/011-capture-pattern/plan.md
    title: Feature 011 plan, Complexity Tracking
  - id: research
    resource: ../../specs/011-capture-pattern/research.md
    title: Feature 011 research, E2
  - id: arm
    resource: /references/arm-api-versioning.md
    title: Azure API versioning (preview versions, breaking-change gates)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-26T12:00:00Z }
---

# Origin

The project owner's answer of 2026-11-26 to a question found while writing feature 011's contract
(T001), recorded in the [feature 011 plan](../../specs/011-capture-pattern/plan.md), Complexity
Tracking.

Sources: [Feature 011 plan, Complexity Tracking](../../specs/011-capture-pattern/plan.md).

# Context

- Feature 011 adds 9 `ErrorCode` values, 6 `OperationKind` values and the slice history kind
  `made-for-proof`. `getOperation` and `startRun` must return them, so feature 002's way out (a
  separate problem schema for a new code) does not fit.
- The pinned oasdiff (v1.37.0) reports each added response enum value as an error,
  `response-property-enum-value-added`: 1258 errors from `main`'s engine contract to the new one
  (experiment E2). Every other change is additive or a warning.
- Every client of the engine contract (the terminal client and the run page) is generated from the
  same contract and built in the same commit; both print a problem's code and reason, whatever the
  code.
- Azure treats a break against the previous preview as a warning only, and lets a preview break its
  own preview-only features ([Azure API versioning](/references/arm-api-versioning.md)).

Sources: [Feature 011 research, E2](../../specs/011-capture-pattern/research.md); [Azure API versioning](/references/arm-api-versioning.md).

# Decision

- Feature 011 extends the engine contract's `2026-11-12-preview` in place with the new enum values
  (owner, 2026-11-26).
- It is an exception to Principle XV, approved by the owner and recorded in the plan's Complexity
  Tracking. Every other change in it stays additive: oasdiff reports no other error.
- The oasdiff report from `main` is kept in the feature's research.

Sources: [Feature 011 plan, Complexity Tracking](../../specs/011-capture-pattern/plan.md).

# Consequences

- No second contract version, no deprecation period and no change to `scripts/contract-diff.sh`.
- A client built from an older copy of the preview may get a code or kind it does not name; it has
  no other copy in use.
- The exception holds only for this preview while its clients are built with it. A stable version
  will need enum values declared extensible, or a new version for each one added.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).
