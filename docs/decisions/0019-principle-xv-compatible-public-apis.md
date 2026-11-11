---
type: Decision
title: "ADR 0019: Principle XV - Compatible Public APIs"
description: Why the constitution adopts Principle XV, 'Compatible Public APIs'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: aip-180
    resource: https://google.aip.dev/180
    title: "AIP-180"
  - id: aip-185
    resource: https://google.aip.dev/185
    title: "AIP-185"
  - id: aip-155
    resource: https://google.aip.dev/155
    title: "AIP-155"
  - id: aip-158
    resource: https://google.aip.dev/158
    title: "AIP-158"
  - id: rfc-9745
    resource: https://www.rfc-editor.org/rfc/rfc9745
    title: "RFC 9745"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Origin

The project owner ratified the constitution, with Principle XV, "Compatible Public APIs", on
2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into this record,
without changing any rule.

# Context

Users automate the platform through these interfaces, and silent breaks break their automation
(AIP-180; AIP-185; AIP-155; AIP-158; RFC 9745).

Sources: [AIP-180](https://google.aip.dev/180); [AIP-185](https://google.aip.dev/185); [AIP-155](https://google.aip.dev/155); [AIP-158](https://google.aip.dev/158); [RFC 9745](https://www.rfc-editor.org/rfc/rfc9745).

# Decision

Adopt Principle XV, "Compatible Public APIs", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).
