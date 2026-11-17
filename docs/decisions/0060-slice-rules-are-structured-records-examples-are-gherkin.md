---
type: Decision
title: "ADR 0060: A slice's rules are structured records and its examples are Gherkin scenarios"
description: A rule is a structured record validated by JSON Schema, with its EARS sentence generated and never parsed; each rule's examples are Gherkin scenarios under a Rule block, run with godog --strict.
tags: [decisions, slice, ears, gherkin]
status: draft
decision: accepted
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D2 and D3
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: ears
    resource: https://alistairmavin.com/ears/
    title: EARS official guide
  - id: gherkin
    resource: https://cucumber.io/docs/gherkin/reference/
    title: Gherkin reference
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-17T22:00:00Z }
---

# Origin

Feature 001 chose the slice format in its research as D2 (rules) and D3 (examples), recorded in
the [plan](../../specs/001-prose-to-accepted-slice/plan.md)'s decisions. Task T060 asks for this
record.

Sources: [Feature 001 research, D2 and D3](../../specs/001-prose-to-accepted-slice/research.md), [Feature 001 plan, Decisions](../../specs/001-prose-to-accepted-slice/plan.md).

# Context

- FR-004 requires every rule to follow the rule grammar, every rule to have an example, and every
  term a rule uses to be defined.
- EARS fixes clause order and keywords. Models produce valid structure reliably but are weakest at
  rule meaning, which the developer approves.
- In Gherkin, `Rule` represents one business rule. godog's `--strict` fails the suite on
  pending, undefined or ambiguous steps.
- Parsing EARS prose would need a parser of our own and fuzzy input. FRET was heavy for the MVP.
  Plain Go table tests lose the link from rule to example.

Sources: [Feature 001 research, D2 and D3](../../specs/001-prose-to-accepted-slice/research.md), [EARS official guide](https://alistairmavin.com/ears/), [Gherkin reference](https://cucumber.io/docs/gherkin/reference/).

# Decision

- A rule is a structured record with a pattern, precondition, trigger, feature, system, response
  and term references, validated by JSON Schema. Its EARS sentence is generated, never parsed.
- Each rule's examples are Gherkin scenarios under a `Rule:` block tagged with the rule's ID.
- The build runs the scenarios with godog `--strict`.

Sources: [Feature 001 research, D2 and D3](../../specs/001-prose-to-accepted-slice/research.md).

# Consequences

- The grammar and term checks become schema and reference checks.
- `--strict` proves that every example is bound to a test.
- The gherkin parser is generated from a grammar, so the platform writes no parser for either
  format.

Sources: [Feature 001 research, D2 and D3](../../specs/001-prose-to-accepted-slice/research.md).
