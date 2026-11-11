---
type: Architecture
title: Architecture overview
description: System context, building blocks, and cross-cutting design of the platform.
tags: [architecture]
status: draft
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T16:37:58Z }
---

# Context

Not yet designed. The users, external systems, and platform boundary come
from the [platform purpose](/overview/platform.md).

# Building blocks

Agents are defined and executed with [Docker Agent](/references/docker-agent.md)
([ADR 0002](/decisions/0002-build-on-docker-agent.md)). The platform's own building blocks
wrap it with identity, tenancy, policy, durable runs, quotas and audit.

Interactive clients share and steer live sessions through the
[Agent Host Protocol](/references/agent-host-protocol.md), served by the platform's own AHP host
([ADR 0003](/decisions/0003-ahp-client-session-protocol.md)).

Each building block gets its own concept and is linked from here:

- Agents go in [agents/](/agents/conventions.md).
- Components go in [components/](/components/conventions.md).
- APIs go in [apis/](/apis/conventions.md).
- Data stores go in [data/](/data/conventions.md).

# Cross-cutting concerns

- Security: [security conventions](/security/conventions.md).
- Operations: [operations conventions](/operations/conventions.md).

# Decisions

- [ADR 0001: Adopt OKF for project knowledge](/decisions/0001-adopt-okf.md)
- [ADR 0002: Build the platform on Docker Agent](/decisions/0002-build-on-docker-agent.md)
- [ADR 0003: AHP is the client-facing session protocol](/decisions/0003-ahp-client-session-protocol.md)
- [ADR 0004: Contract-first toolchain, service-owned APIs and versioning](/decisions/0004-contract-first-toolchain.md)
- [ADR 0026: Deterministic workflow engine with agents as state workers](/decisions/0026-deterministic-workflow-engine.md)
- [ADR 0027: Contract-driven frontend](/decisions/0027-contract-driven-frontend.md)
- ADRs 0005 to 0025: why each constitution principle exists, starting with [ADR 0005](/decisions/0005-principle-i-needs-before-solutions.md)
