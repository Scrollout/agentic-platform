---
type: Decision
title: "ADR 0012: Principle VIII - Containers and Sandboxed Execution"
description: Why the constitution adopts Principle VIII, 'Containers and Sandboxed Execution'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: nist-sp-800
    resource: https://csrc.nist.gov/pubs/sp/800/190/final
    title: "NIST SP 800-190"
  - id: pod-security-standards
    resource: https://kubernetes.io/docs/concepts/security/pod-security-standards/
    title: "Pod Security Standards"
  - id: gvisor
    resource: https://gvisor.dev/docs/
    title: "gVisor"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Origin

The project owner ratified the constitution, with Principle VIII, "Containers and Sandboxed
Execution", on 2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into
this record, without changing any rule.

# Context

Containers decouple the product from hosts, and agent-run code is untrusted code that needs a real
isolation boundary (NIST SP 800-190; Pod Security Standards;
gVisor).

Sources: [NIST SP 800-190](https://csrc.nist.gov/pubs/sp/800/190/final); [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/); [gVisor](https://gvisor.dev/docs/).

# Decision

Adopt Principle VIII, "Containers and Sandboxed Execution", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).
