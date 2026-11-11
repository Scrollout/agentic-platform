---
type: Decision
title: "ADR 0006: Principle II - Two Planes, Agent Work Stays in the Runtime Plane"
description: Why the constitution adopts Principle II, 'Two Planes, Agent Work Stays in the Runtime Plane'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: aws-control-planes
    resource: https://docs.aws.amazon.com/whitepapers/latest/aws-fault-isolation-boundaries/control-planes-and-data-planes.html
    title: "AWS: Control planes and data planes"
  - id: aws-static-stability
    resource: https://aws.amazon.com/builders-library/static-stability-using-availability-zones/
    title: "AWS: Static stability"
  - id: azure-control-plane
    resource: https://learn.microsoft.com/en-us/azure/azure-resource-manager/management/control-plane-and-data-plane
    title: "Azure: Control plane and data plane"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with Principle II, "Two Planes, Agent Work Stays in the
Runtime Plane", on 2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into
this record, without changing any rule.

# Context

Run traffic dwarfs management traffic, agents must not stop because management is down, and run
content needs the smallest possible exposure. This is the static-stability pattern (AWS: Control
planes and data planes[^aws-control-planes]; AWS: Static stability[^aws-static-stability]; Azure:
Control plane and data plane[^azure-control-plane]).

# Decision

Adopt Principle II, "Two Planes, Agent Work Stays in the Runtime Plane", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^aws-control-planes]: AWS: Control planes and data planes
[^aws-static-stability]: AWS: Static stability
[^azure-control-plane]: Azure: Control plane and data plane
