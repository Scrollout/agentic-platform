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
  - id: spec-001
    resource: ../../specs/001-prose-to-accepted-slice/spec.md
    title: Feature 001 specification, Key Entities
  - id: data-model-001
    resource: ../../specs/001-prose-to-accepted-slice/data-model.md
    title: Feature 001 data model
  - id: spec-002
    resource: ../../specs/002-durable-execution/spec.md
    title: Feature 002 specification, Key Entities
  - id: spec-004
    resource: ../../specs/004-edge-case-graders/spec.md
    title: Feature 004 specification, Key Entities
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-18T09:00:00Z }
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

# Feature 001 terms

These terms come from the Feature 001 specification and data model.

| Term | Meaning |
|------|---------|
| Slice | One use case taken from description to working software; holds goals, rules, examples, terms and a contract. |
| Pattern | A reviewed way to build one kind of slice: its conditions for fitting, its steps, each step's scope, allowed dependencies and checks, its limits and budgets, and its sources. Declared in a `pattern.yaml`. |
| Catalogue | The set of approved patterns (`src/catalogue/`). |
| Run | One attempt to build a slice from a pattern. It ends in exactly one outcome: done, handed over, expired or cancelled. |
| Step | One part of a run, filled by an agent or a fixed procedure, with a scope and checks. |
| Check | A deterministic test that decides whether a step or an artifact is accepted. A model never decides. |
| Hand-over | The end of a run the platform could not finish within its attempts, review rounds or budgets, after which the developer takes the work over. It has three forms: the spec says a run is "handed over", the data model's outcome is `handed-over`, and the terminal client's command is `take-over`. |

Sources: [Feature 001 specification](../../specs/001-prose-to-accepted-slice/spec.md); [Feature 001 data model](../../specs/001-prose-to-accepted-slice/data-model.md).

# Feature 002 terms

These terms come from the Feature 002 specification.

| Term | Meaning |
|------|---------|
| Operation | A state-changing request the platform accepted, followed by its identifier until it ends succeeded, failed or cancelled. |
| Durable execution | Running a multi-step process so that its progress survives a stop of whatever runs it: each finished step is recorded, and after a restart the process continues after the last one, never repeating it. |
| Retry rule | The one declaration of how a failed call is retried: how many times, how long to wait, the jitter and the longest wait. |

Sources: [Feature 002 specification](../../specs/002-durable-execution/spec.md).

# Feature 004 terms

These terms come from the Feature 004 specification.

| Term | Meaning |
|------|---------|
| Eval | A repeatable trial of an agent on a fixed case, judged by graders; a case passes (pass^k) only when all k trials pass. |
| Grader | A deterministic test of what an agent produced in an eval trial. A model never decides. |
| Held-out test | A test of a slice's rules on an input no example shows, kept from the agent and run against its code after the trial. |
| Mutant | A correct implementation with one planted bug; a set of tests kills it when they fail against it and pass against the correct one. |

Sources: [Feature 004 specification](../../specs/004-edge-case-graders/spec.md).

# Feature 005 terms

These terms come from the Feature 005 specification.

| Term | Meaning |
|------|---------|
| Quarantine | The state of a flaky eval case that names an open backlog task: it still runs and is reported, but its result does not count until the task closes it. |

Sources: [Feature 005 specification](../../specs/005-analyst-reliability/spec.md).

# Feature 007 terms

These terms come from the Feature 007 specification.

| Term | Meaning |
|------|---------|
| Revision | The analyst's new proposal for a slice after the developer answers its questions; when accepted, it replaces the slice's parts. |

Sources: [Feature 007 specification](../../specs/007-analyst-answer-checks/spec.md).
