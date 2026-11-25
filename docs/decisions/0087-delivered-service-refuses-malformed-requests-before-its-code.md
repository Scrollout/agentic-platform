---
type: Decision
title: "ADR 0087: The delivered service refuses malformed requests before its code runs"
description: The catalogue pattern overrides oapi-codegen's handler template so every operation's registration is wrapped by a generated request check, which refuses a body its contract's shape does not allow and passes on only the declared members under their exact names.
tags: [decisions, catalogue, codegen, http]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/010-strict-requests/plan.md
    title: Feature 010 plan, Decisions
  - id: research
    resource: ../../specs/010-strict-requests/research.md
    title: Feature 010 research, E2, E4, R1, R5
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-25T12:00:00Z }
---

# Origin

The plan of [feature 010](../../specs/010-strict-requests/plan.md), from backlog task T202 (the
review agent's confirmed finding on the evals' clean reference); the owner chose the refusal's
rules (spec, Clarifications, 2026-11-25).

Sources: [Feature 010 plan, Decisions](../../specs/010-strict-requests/plan.md).

# Context

- The seam serves the generated `ServerInterface`, so the code step's own code reads each body,
  with Go's `encoding/json` v1, which matches names without regard to case, keeps the last of
  two equal names and requires nothing: `{"TEXT":"x"}` is stored as `"x"`.
- Validating the body is not enough: `{"text":"a","TEXT":"b"}` passes a schema that allows other
  members and still gives the code `"b"`.
- oapi-codegen's `user-templates` replace a built-in template; `HandlerWithOptions` registers
  every operation, and every other handler constructor calls it.

Sources: [Feature 010 research, E2, E4, R1, R5](../../specs/010-strict-requests/research.md).

# Decision

- The pattern's template overrides `stdhttp/std-http-handler.tmpl` with the pinned release's
  text, one line changed: each registration is wrapped by `refuseMalformed("<METHOD> <path>", ...)`.
- The request check refuses a missing body, invalid JSON, a name twice, a missing required member,
  a wrong type and, where the contract closes an object, an undeclared member, with
  `400 {"message": ...}`; otherwise it passes on a body holding only the declared members under
  their exact names.
- Limits, allowed values and formats stay with the code step's code (owner).
- The copied template names its upstream release; a test fails when it differs from the
  oapi-codegen pin.

Sources: [Feature 010 research, R1, R5](../../specs/010-strict-requests/research.md).

# Consequences

- Every way of serving the generated handler refuses malformed requests, the stub included; the
  seam, the tests and the code agent's code are unchanged.
- An oapi-codegen upgrade must copy the new handler template and reapply the one change.
- Each request body is read once more and written again, up to 1 MiB.

Sources: [Feature 010 plan](../../specs/010-strict-requests/plan.md).
