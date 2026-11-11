---
type: Decision
title: "ADR 0008: Principle IV - Asynchronous Operations on Worker Pools"
description: Why the constitution adopts Principle IV, 'Asynchronous Operations on Worker Pools'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: aip-151
    resource: https://google.aip.dev/151
    title: "AIP-151"
  - id: asynchronous-request-reply
    resource: https://learn.microsoft.com/en-us/azure/architecture/patterns/async-request-reply
    title: "Asynchronous Request-Reply"
  - id: queue-based-load
    resource: https://learn.microsoft.com/en-us/azure/architecture/patterns/queue-based-load-leveling
    title: "Queue-based load leveling"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with Principle IV, "Asynchronous Operations on Worker
Pools", on 2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into this
record, without changing any rule.

# Context

Long or multi-step work must not block clients, and one slow class of work must not starve the
others (AIP-151[^aip-151]; Asynchronous Request-Reply[^asynchronous-request-reply]; Queue-based load
leveling[^queue-based-load]).

# Decision

Adopt Principle IV, "Asynchronous Operations on Worker Pools", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^aip-151]: AIP-151
[^asynchronous-request-reply]: Asynchronous Request-Reply
[^queue-based-load]: Queue-based load leveling
