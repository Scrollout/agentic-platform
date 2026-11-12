---
type: Architecture
title: Architecture overview
description: System context, building blocks, and cross-cutting design of the platform.
tags: [architecture]
status: draft
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D5 to D13, D24, D27, D28
  - id: adr-0028
    resource: /decisions/0028-sign-platform-images-locally.md
    title: "ADR 0028: Sign the platform's images locally with cosign"
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-13T12:00:00Z }
---

# Context

A developer on one machine describes a use case in prose and the platform takes it to approved,
tested software; the model proposes, deterministic checks decide. The users and boundary come from
the [platform purpose](/overview/platform.md).

# Building blocks

Agents run on [Docker Agent](/references/docker-agent.md)
([ADR 0002](/decisions/0002-build-on-docker-agent.md)); interactive clients will share sessions
through the [Agent Host Protocol](/references/agent-host-protocol.md)
([ADR 0003](/decisions/0003-ahp-client-session-protocol.md)). Feature 001 builds one slice of the
platform on one machine ([ADR 0026](/decisions/0026-deterministic-workflow-engine.md)).

Shipped by Story 1:

- **Launcher `ap`** (`src/deploy/ap`): starts and stops the containers and passes each its token.
  It verifies the cosign signature of every platform image against the local registry before
  anything starts ([ADR 0028](/decisions/0028-sign-platform-images-locally.md),
  [cosign](/components/cosign.md), [registry](/components/distribution-registry.md)).
- **Engine** (`src/engine`): the slice service (proposes, changes and approves slices), the run store
  (slices and runs on a volume only it mounts) and the contract checks (schema, traceability,
  pattern fit, [oasdiff](/components/oasdiff.md) compatibility, lint). It serves the contract-first
  engine API.
- **Analyst service**: [Docker Agent](/components/docker-agent.md) running `serve api` on the
  analyst agent; the engine calls it for each proposal (research D27).
- **Contract-lint service**: a small [Spectral](/components/spectral.md) server that lints a slice's
  contract; only the engine reaches it (research D28).
- **Model gateway**: [nginx](/components/nginx.md), the only container with egress and the only one
  that holds the provider key.
- **Terminal client** (`src/cli`) and **run page** (`src/page`): both call the engine API.

Planned for Story 2: per-step **step containers** and **check containers**, started by `ap` when a
slice is approved and a build is asked for, each with only its step's paths writable
(research D5). The engine runs the step, then the check decides; the check container cannot reach
the gateway.

Every container runs read-only with all capabilities dropped; the analyst, contract-lint and the
workers run under [gVisor](/components/gvisor.md). Networks are internal and isolated: `clients`,
`agents` (engine, gateway, step workers), `checks`, `lint` and `analyst` (engine, gateway). Only the
gateway joins `egress`, and only the page's forwarder joins `page-host` to publish 127.0.0.1.
See [Compose](../../src/deploy/compose.yaml) and the [component list](/components/index.md).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md),
[ADR 0028](/decisions/0028-sign-platform-images-locally.md).

Each further building block gets its own concept:

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
