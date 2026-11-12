---
type: Component
title: Docker Agent
description: "The agent runtime the platform is built on; here it runs the analyst as a long-lived serve api service."
tags: [components, agents, runtime]
status: draft
resource: https://github.com/docker/docker-agent
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research
  - id: ref
    resource: /references/docker-agent.md
    title: Docker Agent reference
  - id: adr-0002
    resource: /decisions/0002-build-on-docker-agent.md
    title: "ADR 0002: Build the platform on Docker Agent"
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-13T12:00:00Z }
---

# Responsibility

Defines and runs agents from YAML. In Story 1 it runs the analyst: the engine calls it to turn a
description into a slice proposal. The platform adds the checks around it; the model proposes and
the engine's deterministic checks decide. Licence Apache-2.0; no OpenSSF Scorecard is published, so a
local scorecard run is open (research D22, T063).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [Docker Agent reference](/references/docker-agent.md), [ADR 0002](/decisions/0002-build-on-docker-agent.md).

# Interfaces

- `serve api /agents/analyst.yaml` on port 8080 of the `analyst` service. The engine opens one
  titled session per analyst call (`POST /api/sessions` with `safety_policy: restricted`), runs the
  agent, reads the event stream until `stream_stopped`, takes the last `agent_choice` as the
  proposal and deletes the session (research D27). It calls no other Docker Agent interface.
- Model calls go to the [model gateway](/components/nginx.md), never to the provider.

# Dependencies

The gateway, and the agent definitions in `src/agents/` mounted read-only.

# Configuration

- Image: the `docker-agent` pin in `src/deploy/images.lock` (`docker/docker-agent:1.160.0`, by
  digest). `compose.yaml` repeats it for the analyst because Compose cannot read the lock; a test
  fails when they differ.
- The analyst runs under [gVisor](/components/gvisor.md), read-only root, user 65532, all
  capabilities dropped, a bearer token from `ap up`, the session database on a tmpfs, and
  `TELEMETRY_ENABLED=false`; its only way out is the gateway.
- Its digest recorded on each proposal is the SHA-256 of `analyst.yaml` and `analyst/instruction.md`.
- The image carries no cosign signature; it is pinned by digest only (research D24).

# Operations

A Docker Agent upgrade must pass the analyst's eval suite (`src/tests/evals/analyst/`, Principle XIX).
