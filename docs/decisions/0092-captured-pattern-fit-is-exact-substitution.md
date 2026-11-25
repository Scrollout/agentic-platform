---
type: Decision
title: "ADR 0092: A slice fits a captured pattern by exact substitution"
description: Superseded in part by ADR 0094 (any literal value may be a parameter) and ADR 0095 (a value keeps its parameter's kind and a name has at least two letters). A captured pattern's parameters are names, in three case forms, and declared values; a slice fits when filling the source slice with one value per parameter gives exactly the approved slice; one engine owns these rules.
tags: [decisions, catalogue, captured-patterns]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/011-capture-pattern/plan.md
    title: Feature 011 plan, Decisions
  - id: research
    resource: ../../specs/011-capture-pattern/research.md
    title: Feature 011 research, R4 and E1
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

> Superseded in part by [ADR 0095](/decisions/0095-captured-patterns-never-change-values-keep-their-kind.md): a value keeps its parameter's kind and a name has at least two letters.
>
> Superseded in part by [ADR 0094](/decisions/0094-any-literal-value-may-be-a-parameter.md): the
> developer may also add any other literal value of the slice as a parameter.

- A fit must be decided without a model and give one answer per slice.
- In an accepted project every occurrence of a resource's name is one of its lower, capitalised
  or upper forms, alone or inside a word (research E1).
- gonew, Copier, Cookiecutter and `text/template` cannot derive a template from an accepted
  project.

Sources: [Feature 011 research, R4 and E1](../../specs/011-capture-pattern/research.md).

# Decision

- Candidates are the slice's names and declared values; the developer may remove any and add
  another (owner).
- A name covers its lower, capitalised and upper forms, inside words too; a plural is its own
  parameter (owner). Occurrences are matched longest first, never overlapping; two parameters
  equal in any case form are refused.
- Values are parsed into their form first: a name `[a-z][a-z0-9]*`, a declared value a number or
  a string of letters, digits, `_`, `-` and spaces.
- A slice fits when filling the source slice's canonical text gives exactly the approved slice's.
- The package `engine/captured` owns the proposal, the fit and the fill.

Sources: [Feature 011 research, R4 and E1](../../specs/011-capture-pattern/research.md).

# Consequences

- Other spellings (`note_id`), values derived from a declared value and words that merely
  contain a name are limits: they make a slice not fit or a proof fail, never a wrong build.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).
