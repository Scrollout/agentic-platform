---
type: Decision
title: "ADR 0002: Build the platform on Docker Agent"
description: Docker Agent is the agent definition format and runtime the platform is designed around; the platform adds what multi-tenant use needs.
tags: [decisions, docker-agent, architecture]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's request, 2026-11-11 ("We will need to design our platform based on Docker Agents")
  - id: docker-agent
    resource: /references/docker-agent.md
    title: Docker Agent reference
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T23:30:00Z }
---

# Origin

The project owner directed that the platform be designed on Docker Agent on 2026-11-11, added
the rule that Docker Agent runs only in containers, and confirmed this decision the same day.

Sources: [Project owner's request, 2026-11-11 ("We will need to design our platform based on Docker Agents")](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).

# Context

The platform needs:

- a way to define agents,
- a runtime that executes them with models and tools,
- a way to package and share them.

Docker Agent provides all three, under Apache-2.0:

- **Definitions:** declarative YAML agents with a versioned schema.
- **Execution:** multi-agent delegation and handoffs, about 40 model providers, MCP and built-in toolsets, budgets, permissions, hooks and a microVM sandbox.
- **Packaging:** OCI distribution.
- **Server modes:** a REST/SSE API server, plus OpenAI-compatible, A2A, ACP and MCP surfaces.
- **Embedding:** a Go SDK.

It is designed for one user on one machine. Several things a multi-tenant platform needs are missing:

- **Access and isolation:** identity, authorization and tenant isolation.
- **Durable operation:** durable sessions and runs, and persistent quotas.
- **Governance:** audit, and an enforced supply-chain policy.

Sources: [Docker Agent reference](/references/docker-agent.md).

# Decision

- **Docker Agent's YAML is our agent definition format.**
  - We use Docker Agent's YAML config schema, pinned to one schema version.
  - We do not invent our own format.
  - Agents are distributed as Docker Agent OCI artifacts.
- **Docker Agent executes the agents.**
  - The platform runs them on Docker Agent's runtime, through `serve api` or the embedded Go runtime.
  - Which of the two is still open (see Follow-up).
- **The platform adds what Docker Agent lacks.** It wraps the runtime and provides:
  - identity,
  - tenancy,
  - policy authority over safety mode, permissions, hooks and egress,
  - durable run state and quotas,
  - audit,
  - artifact admission.
- **Docker Agent runs only in containers, never directly on a host** (owner, 2026-11-11). This
  holds everywhere: production, development, tests, evals and spikes.
  - Every Docker Agent runtime, and every tool or MCP server it starts, runs inside a container
    built from a pinned image. Nothing is installed on, or run from, a developer's or operator's
    host.
  - The container mounts only the files its task may change. Host credentials, the host's
    environment and the host's home directory are never passed in.
  - The container engine's socket is never mounted into an agent container. MCP servers that
    Docker Agent would start through the Docker MCP Gateway (`ref: docker:<name>`) run instead as
    their own containers, reached over the network.
  - Running agent-generated code still needs the stronger sandbox of Principle VIII inside or
    around that container.
- **Model for the MVP, demos and drafts:** Docker Agent agents run on Claude Haiku
  (`claude-haiku-5-5`) (owner, 2026-11-11). The production model choice is still open, and every
  agent revision pins its model version (Principle XV).
- **We extend, we don't fork.** Missing capabilities go upstream, or into extension points: hooks, the `session.Store` interface, the models gateway and providers.

# Consequences

- **What we don't build:** an agent DSL, a model-provider abstraction, an MCP client or an agent packaging format.
- **Tracking upstream:** Docker Agent ships three to four releases a week, and minor releases sometimes carry breaking changes.
  - We pin one version and its digest.
  - Each upgrade is re-validated with `docker agent eval` regression suites and our own tests.
- **Untrusted authors:** client-side permissions are "not a security boundary for untrusted agents".
  - Isolation must come from a sandbox or container per tenant or session, plus network-level egress control.
- **Telemetry:** Docker product telemetry is on by default and can carry prompt text. The platform disables it everywhere.

# Follow-up

- Decide how `docker agent eval` runs under this rule. It starts one container per eval through
  the host's container engine, so it needs an engine that is not the host's own, such as a
  rootless engine inside a dedicated eval container or a CI runner VM.
- Build the pinned, non-root Docker Agent runtime image, with a read-only root file system and no
  world-writable directories (the upstream image's `/data` and `/work` are `chmod 777`).

- Choose the integration mode: `serve api` behind a gateway, or embedding `pkg/runtime` and `pkg/server` in Go.
- Choose the session and run store, and whether a durable-execution engine wraps runs.
- Choose the isolation unit, per tenant or per session, and its mechanism: Docker Sandboxes microVM, gVisor or Kata.
- Define the admission policy for agent artifacts: registry allowlist, digest pinning, signature verification.
- Record the adoption in a [Docker Agent component concept](/components/docker-agent.md) once its role is designed.
