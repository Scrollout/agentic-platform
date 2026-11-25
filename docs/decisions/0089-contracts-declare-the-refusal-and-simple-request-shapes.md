---
type: Decision
title: "ADR 0089: Contracts declare the refusal and simple request shapes"
description: A slice fits the catalogue pattern only when every operation with a request body declares a 400 JSON body requiring only a string message (RefusalDeclared), and every request body is application/json with a simple shape (RequestBodiesSupported).
tags: [decisions, catalogue, fit]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/010-strict-requests/plan.md
    title: Feature 010 plan, Decisions
  - id: research
    resource: ../../specs/010-strict-requests/research.md
    title: Feature 010 research, R4
  - id: spec
    resource: ../../specs/010-strict-requests/spec.md
    title: Feature 010 specification, Clarifications
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-25T12:00:00Z }
---

# Origin

The project owner's answers of 2026-11-25 ([feature 010 specification](../../specs/010-strict-requests/spec.md),
Clarifications), recorded in the feature's plan.

Sources: [Feature 010 specification, Clarifications](../../specs/010-strict-requests/spec.md).

# Context

- The platform refuses a malformed request with the contract's own 400 body, so that body must be
  one it can fill.
- It can remove undeclared members only from a shape it knows exactly; composition keywords
  (`allOf`, `oneOf`, ...) and `additionalProperties` as a schema make which members are declared
  depend on the value.

Sources: [Feature 010 research, R4](../../specs/010-strict-requests/research.md).

# Decision

- New fit condition RefusalDeclared: every operation with a `requestBody` declares a `400` whose
  `application/json` schema is an object requiring exactly `message`, a string with no value
  keyword (owner, analyze round 1), and the object schema uses only `type`, `required`,
  `properties`, `additionalProperties` and annotations.
- New fit condition RequestBodiesSupported: every request body is `application/json` only, and
  its schema shapes the body only with `type`, `properties`, `required`, boolean
  `additionalProperties` and `items`, through local `$ref`s; value keywords and annotations are
  allowed; any other key fails (fail closed), `nullable` among them (owner, analyze round 1: a type
  list allows `null`); a body without a schema, a type name other than the seven JSON Schema
  types, a boolean schema and a YAML anchor or alias fail.

Sources: [Feature 010 research, R4](../../specs/010-strict-requests/research.md).

# Consequences

- Fewer contracts fit; the analyst's instruction says how to write one that does, and the
  condition, not the instruction, decides.
- Supporting `allOf` or other media types later is a new decision.

Sources: [Feature 010 plan](../../specs/010-strict-requests/plan.md).
