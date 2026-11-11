---
type: Decision
title: "ADR 0003: AHP is the client-facing session protocol"
description: Clients (web console, IDE, CLI, mobile) share and steer live agent sessions through the Agent Host Protocol; the platform's host bridges AHP to Docker Agent.
tags: [decisions, ahp, protocols, sessions]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's answers, 2026-11-11 (AHP is Microsoft's Agent Host Protocol; several clients must see and steer the same session)
  - id: ahp
    resource: /references/agent-host-protocol.md
    title: Agent Host Protocol reference
  - id: docker-agent
    resource: /references/docker-agent.md
    title: Docker Agent reference
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T22:45:00Z }
---

# Origin

On 2026-11-11 the project owner confirmed two things:

- several clients (web console, IDE, CLI, phone) must see and steer the same live session at once;
- Microsoft's Agent Host Protocol is the protocol to use for it.

The owner confirmed this decision the same day.

Sources: [Project owner's answers, 2026-11-11 (AHP is Microsoft's Agent Host Protocol; several clients must see and steer the same session)](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).

# Context

- **Docker Agent is one client to one agent.** Its API server, ACP and chat surfaces each serve a single client.
  - None of them synchronizes state across clients or replays missed events after a reconnect.
- **AHP was built for exactly this.**
  - The host holds the authoritative state, orders every action, lets clients apply their own changes at once and reconcile, and replays missed actions on reconnect.
  - It is designed to sit above ACP, which Docker Agent implements.
- **AHP is young.**
  - Spec 1.0.0 was released on 2026-10-02.
  - It is run by Microsoft, not a standards body, and it expects breaking changes.
  - It leaves tenancy and connection auth to the host.

Sources: [Docker Agent reference](/references/docker-agent.md); [Agent Host Protocol reference](/references/agent-host-protocol.md).

# Decision

- **AHP is the client protocol.** Every interactive client reaches live sessions through AHP, over WebSocket.
- **The host is ours.** The platform runs its own AHP host in the runtime plane. It:
  - authenticates and authorizes every connection and channel per tenant;
  - drives Docker Agent runtimes downstream, through ACP or the Docker Agent runtime API (the choice comes from ADR 0002's follow-up and the spike below);
  - maps their events into AHP actions.
- **The host is the authority for client-visible session state.** That state must survive restarts and must be recoverable on reconnect.
- **We start with the Stable channels only:** root, session, chat, terminal, telemetry and resource watch.
  - Experimental and early-development channels stay disabled until a decision enables them. The TCP channel is one of them.
- **We pin one AHP protocol version.** We generate our host and client types from AHP's published JSON Schemas and official SDKs.

# Consequences

- Clients get shared live sessions, steering, cancellation and approvals from any device, plus reconnection without losing state.
  - VS Code's Agent Sessions view works as a client.
- We own the bridge between AHP and Docker Agent, and must keep it in step with releases of both.
- **The host must add what the spec omits:**
  - tenant isolation for every session and channel;
  - per-user authorization;
  - token exchange in place of the user tokens clients push to it;
  - no host-to-client file access unless a feature needs it;
  - telemetry that carries no protected content.
- Non-interactive clients, such as CI and API automation, may keep using the management and run APIs. AHP is for interactive and shared use.

# Follow-up

- **Spike:** put an AHP host in front of one Docker Agent instance.
  - Decide between ACP (stdio) and the Docker Agent runtime API downstream.
  - Measure what session state maps cleanly and what is lost.
- **Evaluate the existing open-source hosts before building our own**, as Principle VI requires: AHP-ACP Gateway, ahpd, hydra-ahp, pi-ahp and AgentConnect.
- **Define how a connection is authenticated**, for example an OIDC token on the WebSocket upgrade, and how tenants map to channels.
- This decision settles need N-020 in the [problem space](/overview/problem-space.md).
