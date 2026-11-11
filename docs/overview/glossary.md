---
type: Glossary
title: Glossary
description: Shared vocabulary for the platform and its knowledge bundle.
tags: [overview, glossary]
status: draft
sources:
  - id: okf-spec
    resource: /references/okf-spec.md
    title: Open Knowledge Format v0.2 specification
  - id: docker-agent
    resource: /references/docker-agent.md
    title: Docker Agent reference
  - id: ahp
    resource: /references/agent-host-protocol.md
    title: Agent Host Protocol reference
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Knowledge bundle terms

These terms come from the OKF spec.

| Term | Meaning |
|------|---------|
| Knowledge bundle | The `docs/` directory: a self-contained tree of OKF concepts that holds all project knowledge. Not to be confused with software or OCI bundles. |
| Concept | One markdown file with YAML frontmatter. Its id is its path without `.md`. |
| Actor | Who or what performed an action: `<producer>/<version>`, `human:<id>` or `process:<id>`. |
| Trust tier | Derived from `verified`: unverified, machine-confirmed, or human-reviewed. |
| Attested Computation | A concept carrying a sanctioned, checkable way to compute a value. |

Sources: [Open Knowledge Format v0.2 specification](/references/okf-spec.md).

# Platform terms

These terms come from Docker Agent and the Agent Host Protocol. Add each new term here when it first appears in another concept.

| Term | Meaning |
|------|---------|
| Agent | A model plus a description, an instruction (system prompt), toolsets and optional sub-agents, declared in a Docker Agent config file. |
| Root agent | The agent that receives the user's messages: the one named `root`, or the first defined. |
| Sub-agent | An agent that another agent delegates a task to with `transfer_task`. It runs in an isolated sub-session. |
| Handoff | Passing the conversation to a peer agent in the same session, with the full history. |
| Toolset | A group of tools given to an agent: a built-in type (shell, filesystem, fetch, ...) or an MCP server. |
| Session | One conversation with an agent team, with its messages, tool calls, cost and sub-sessions. |
| Safety mode | How unmatched tool calls are handled: `strict`, `balanced`, `restricted` or `autonomous`. |
| Agent artifact | An agent config packaged as an OCI artifact and stored in a registry. |
| AHP host | The server that holds the authoritative state of agent sessions and synchronizes it to every connected client over the Agent Host Protocol. |
| Guard | A deterministic check that must pass before a workflow moves to its next state. |
| State worker | The agent or deterministic step that does the work inside one state of a workflow. It never chooses the next state. |
| Channel | A URI-addressed AHP resource (root, session, chat, terminal, ...) with its own state tree, which clients subscribe to. |

Sources: [Docker Agent reference](/references/docker-agent.md); [Agent Host Protocol reference](/references/agent-host-protocol.md).
