---
type: Decision
title: "ADR 0058: The analyst runs as a Docker Agent serve api service; the engine calls Docker Agent over its API"
description: The platform integrates Docker Agent through its serve api server, one titled session per call, started in its own container, and not by embedding the Go runtime in the engine. Follows up ADR 0002's open choice of integration mode.
tags: [decisions, docker-agent, engine]
status: draft
decision: accepted
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D27 (with D29)
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: adr-0002
    resource: /decisions/0002-build-on-docker-agent.md
    title: ADR 0002, Build the platform on Docker Agent
  - id: adr-0030
    resource: /decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md
    title: ADR 0030, Agent steps on Docker Agent
  - id: da-api
    resource: https://docker.github.io/docker-agent/features/api-server/
    title: Docker Agent - API server
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-17T22:00:00Z }
---

# Origin

The project owner approved this on 2026-11-12, in the feature 001 research as D27, with the
[plan](../../specs/001-prose-to-accepted-slice/plan.md) setting the layout. It settles the
integration-mode choice that [ADR 0002](/decisions/0002-build-on-docker-agent.md) left open:
`serve api` behind a gateway, or embedding `pkg/runtime` and `pkg/server` in Go.
[ADR 0030](/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md) (research
D29) applies the same mode to the agent steps and stands.

Sources: [Feature 001 research, D27 (with D29)](../../specs/001-prose-to-accepted-slice/research.md), [ADR 0002](/decisions/0002-build-on-docker-agent.md), [ADR 0030](/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md).

# Context

- The engine has no Docker socket, so it cannot start an agent per call.
- Embedding the Docker Agent Go SDK would put its whole runtime in the engine process, outside a
  sandbox.
- Starting analysts per call from the launcher would change the createSlice contract.

Sources: [Feature 001 research, D27](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- The analyst runs as an `analyst` service started by `ap up`: the pinned Docker Agent running
  `serve api /agents/analyst.yaml`, with the agent definitions mounted read-only, under gVisor,
  with a read-only root, user 65532, all capabilities dropped, a static bearer token from
  `ap up`, and the session database on a tmpfs. It is on its own network, shared only with the
  engine and the gateway.
- The engine calls it with the standard library, one session per analyst call:
  `POST /api/sessions` with a title and `safety_policy: restricted`, then
  `POST /api/sessions/{id}/agent/analyst`, reading the event stream until `stream_stopped`.
  The proposal is the last `agent_choice` content and the model is the `agent_info` model. The
  session is deleted afterwards, and the client never retries the call.
- The engine always sends a session title, because a session without one makes a second model
  call to name it.
- Each proposal's `agent_digest` is the SHA-256 of `analyst.yaml` and `analyst/instruction.md`
  as mounted under `/agents`.

Sources: [Feature 001 research, D27](../../specs/001-prose-to-accepted-slice/research.md), [Docker Agent - API server](https://docker.github.io/docker-agent/features/api-server/).

# Consequences

- The agent runs in a sandboxed container, not in the engine process. Docker Agent's API is the
  contract, and the platform adds none.
- In the 2026-11-12 evidence, with Docker Agent 1.160.0, the service ran under `runsc` read-only
  as 65532 and 8 concurrent sessions all returned the proposal. Its model request matched the
  recording, so tests replay it.
- `serve api` cannot register itself with the engine, so the launcher registers each step
  container on its behalf (ADR 0030).

Sources: [Feature 001 research, D27](../../specs/001-prose-to-accepted-slice/research.md), [ADR 0030](/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md).
