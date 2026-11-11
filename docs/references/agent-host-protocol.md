---
type: Reference
title: Agent Host Protocol (AHP)
description: Microsoft's protocol for several clients sharing synchronized, replayable state of agent sessions on one host, as of spec v1.0.0.
resource: https://microsoft.github.io/agent-host-protocol/
tags: [references, ahp, protocols, sessions]
status: draft
stale_after: 2027-02-11T00:00:00Z
sources:
  - id: ahp-repo
    resource: https://github.com/microsoft/agent-host-protocol/tree/0e079ec7ff42a9fba0f7266d713efd7ab8ab9bf0
    title: microsoft/agent-host-protocol at commit 0e079ec7 (spec v1.0.0 released, 1.1.0 unreleased)
    last_modified: 2026-11-09T23:52:08Z
  - id: ahp-what
    resource: https://microsoft.github.io/agent-host-protocol/guide/what-is-ahp.html
    title: What is the Agent Host Protocol?
  - id: ahp-doctrine
    resource: https://microsoft.github.io/agent-host-protocol/guide/doctrine.html
    title: Agent Host Protocol Doctrine
  - id: ahp-acp
    resource: https://microsoft.github.io/agent-host-protocol/guide/ahp-and-acp.html
    title: AHP and the Agent Client Protocol
  - id: ahp-spec
    resource: https://microsoft.github.io/agent-host-protocol/specification/overview.html
    title: Specification Overview
  - id: ahp-transport
    resource: https://microsoft.github.io/agent-host-protocol/specification/transport.html
    title: Transport
  - id: ahp-auth
    resource: https://microsoft.github.io/agent-host-protocol/specification/authentication.html
    title: Authentication
  - id: ahp-lifecycle
    resource: https://microsoft.github.io/agent-host-protocol/specification/lifecycle.html
    title: Lifecycle
  - id: ahp-versioning
    resource: https://microsoft.github.io/agent-host-protocol/specification/versioning.html
    title: Versioning
  - id: ahp-tcp
    resource: https://microsoft.github.io/agent-host-protocol/specification/tcp-channel.html
    title: TCP Channel
  - id: ahp-telemetry
    resource: https://microsoft.github.io/agent-host-protocol/specification/telemetry-channel.html
    title: Telemetry Channel
  - id: ahp-impl
    resource: https://microsoft.github.io/agent-host-protocol/guide/implementations.html
    title: Implementations
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Summary

The Agent Host Protocol (AHP) "defines how a portable, standalone sessions server communicates
with its clients". Several clients connect to one host and see the same synchronized view of
AI agent sessions.

- **Publisher:** Microsoft, under the MIT license. The repo was created on 2026-03-12.
- **Versions:** spec v1.0.0 was released on 2026-10-02, and 1.1.0 is open but unreleased.
  - The spec and each language SDK are versioned separately, with SemVer.
- **Governance:** the project is run by Microsoft. No standards body is involved.
- **What it covers:** synchronization, ordering, replay and reconnection.
  - It deliberately does not define how agents reason, models, tools, authentication systems or storage.

How we use it is in [ADR 0003](/decisions/0003-ahp-client-session-protocol.md).

Sources: [What is the Agent Host Protocol?](https://microsoft.github.io/agent-host-protocol/guide/what-is-ahp.html); [microsoft/agent-host-protocol at commit 0e079ec7 (spec v1.0.0 released, 1.1.0 unreleased)](https://github.com/microsoft/agent-host-protocol/tree/0e079ec7ff42a9fba0f7266d713efd7ab8ab9bf0); [Agent Host Protocol Doctrine](https://microsoft.github.io/agent-host-protocol/guide/doctrine.html).

# Model

- **Channels:** every push-style interaction lives on a URI-addressed channel, and each channel holds an immutable state tree.
  - Channels include root (`ahp-root://`), session (`ahp-session:/<uuid>`), chat (`ahp-chat:/<uuid>`), terminal and changeset.
  - Every command and notification carries `params.channel`, so a proxy can route a message by its method and channel alone.
- **Host-authoritative state:** state changes only through typed actions, applied by pure reducers that run the same way on host and client.
  - The host gives each action a sequence number, `serverSeq`, and broadcasts it to every subscriber.
- **Write-ahead reconciliation:** clients apply their own actions at once, then reconcile when the host echoes them back in server order, together with any concurrent actions.
- **Subscriptions:** subscribing returns a snapshot plus `fromSeq`, followed by action envelopes.
- **Content by reference:** large content is fetched separately with `resourceRead`.
- **Reconnection:** `reconnect` with `lastSeenServerSeq` replays the actions a client missed.
  - If the gap is larger than the replay buffer, the host sends fresh snapshots instead.
- **Arbitration between clients:** one turn runs per chat at a time. The first tool-call confirmation wins, and later ones are rejected.
  - Any client can cancel a turn, and the host forwards the cancellation to the agent.

Sources: [What is the Agent Host Protocol?](https://microsoft.github.io/agent-host-protocol/guide/what-is-ahp.html); [Specification Overview](https://microsoft.github.io/agent-host-protocol/specification/overview.html); [Agent Host Protocol Doctrine](https://microsoft.github.io/agent-host-protocol/guide/doctrine.html); [Lifecycle](https://microsoft.github.io/agent-host-protocol/specification/lifecycle.html); [AHP and the Agent Client Protocol](https://microsoft.github.io/agent-host-protocol/guide/ahp-and-acp.html).

# Wire protocol

- **Framing:** JSON-RPC 2.0. The transport can be anything reliable, ordered and bidirectional.
  - WebSocket is the usual choice, and is what VS Code uses: one JSON-RPC message per text frame.
  - A `ping` command keeps the connection alive.
- **Main messages:**
  - Requests: `initialize`, `reconnect`, `subscribe`, `createSession`, `disposeSession`, `listSessions`, `fetchTurns`, `resource*`.
  - Notifications: `dispatchAction`, `action`, `root/session*`, `auth/required`.
- **Host-to-client requests:** the protocol is symmetrical, so the host can send `resource*` requests to the client, for example to read or write the client's files.
- **Extensions:** names starting with `x-` are reserved for implementation-defined channels, methods and actions.
- **Schemas:** JSON Schema 2020-12 files for state, actions, commands, notifications and errors are generated from the TypeScript types.
- **Versioning:** the client offers a list of versions and the host picks one. Each channel also carries its own stability index:
  - Stable: root, session, chat, terminal, telemetry, resource watch.
  - Release candidate: changeset, MCP.
  - Early development or experimental: TCP, automation, canvas, annotations.
  - The spec says backwards-incompatible changes are "inevitable".

Sources: [Transport](https://microsoft.github.io/agent-host-protocol/specification/transport.html); [Specification Overview](https://microsoft.github.io/agent-host-protocol/specification/overview.html); [Versioning](https://microsoft.github.io/agent-host-protocol/specification/versioning.html).

# Authentication

- **Access to the endpoint:** gating the connection is a transport concern and outside AHP's scope, for example a header on the WebSocket upgrade.
- **Credentials for agents and resources:** each agent declares `protectedResources` in RFC 9728 format.
  - Clients obtain OAuth tokens themselves and push them to the host with `authenticate`, using RFC 6750 Bearer semantics. `expiresIn` and `scopes` are optional.
  - The host can answer `AuthRequired` (`-32007`) or send `auth/required`.
  - A `target` field binds a token to one MCP server in one session. Older hosts ignore it, so "isolation is not guaranteed with older hosts".
- **Not covered by the spec:** tenants, users or authorization per channel.

Sources: [Transport](https://microsoft.github.io/agent-host-protocol/specification/transport.html); [Authentication](https://microsoft.github.io/agent-host-protocol/specification/authentication.html).

# Relation to other protocols

- **ACP:** "AHP is a coordination layer. ACP is a communication layer. They compose naturally."
  - A host speaks AHP upstream to clients and ACP downstream to agents. An event mapper translates ACP updates into AHP actions.
- **MCP:** orthogonal. AHP surfaces a session's MCP servers and their authentication challenges, but agents still call tools over MCP.
- **A2A:** out of scope. Agent-to-agent coordination is a stated anti-goal.
- **Docker Agent:** it has no AHP support, but it implements ACP (`serve acp`, stdio only). That makes it a candidate agent behind an AHP host.
  - See the [Docker Agent reference](/references/docker-agent.md).

Sources: [AHP and the Agent Client Protocol](https://microsoft.github.io/agent-host-protocol/guide/ahp-and-acp.html); [Specification Overview](https://microsoft.github.io/agent-host-protocol/specification/overview.html); [Agent Host Protocol Doctrine](https://microsoft.github.io/agent-host-protocol/guide/doctrine.html).

# Implementations

- **SDKs** shipped from the repo: Rust (`ahp-types`, `ahp`, `ahp-ws`), TypeScript (`@microsoft/agent-host-protocol`), Kotlin/JVM, Swift and Go (`ahptypes`, `ahp`, `ahpws`).
  - A .NET client also exists in the repo.
- **Hosts:**
  - The VS Code agent host is the reference implementation.
  - Community hosts: pi-ahp, ahpd, hydra-ahp, AHP-ACP Gateway (ACP agents with persistent sessions and replay) and AgentConnect.
- **Clients:** VS Code Agent Sessions, AHPX, ahpc and a WeChat bridge.
- The maturity of the community projects has not been assessed.

Sources: [Implementations](https://microsoft.github.io/agent-host-protocol/guide/implementations.html); [microsoft/agent-host-protocol at commit 0e079ec7 (spec v1.0.0 released, 1.1.0 unreleased)](https://github.com/microsoft/agent-host-protocol/tree/0e079ec7ff42a9fba0f7266d713efd7ab8ab9bf0).

# Risks for a multi-tenant platform

- **Credentials held by the host:** clients push their own bearer tokens to the host.
  - Our host has to exchange these for delegated, audience-bound tokens rather than forward them, and has to require `target` binding.
- **Host access to client files:** host-to-client `resource*` requests let a host act on a client's files.
  - Our host should not use them unless a feature needs them.
- **TCP forwarding:** the TCP channel forwards connections that the host opens on its own network. It is early development.
  - It is an egress and SSRF path and should stay disabled.
- **Telemetry to clients:** the telemetry channel passes OTLP data to clients verbatim.
  - It must not carry protected content, and must be scoped to the session's tenant.
- **Churn:** the spec is young, 1.1 is open, and breaking changes are expected.
  - Pin a protocol version, and use only Stable channels at first.

Sources: [Authentication](https://microsoft.github.io/agent-host-protocol/specification/authentication.html); [Specification Overview](https://microsoft.github.io/agent-host-protocol/specification/overview.html); [TCP Channel](https://microsoft.github.io/agent-host-protocol/specification/tcp-channel.html); [Telemetry Channel](https://microsoft.github.io/agent-host-protocol/specification/telemetry-channel.html); [Versioning](https://microsoft.github.io/agent-host-protocol/specification/versioning.html).
