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
  - id: spec-006
    resource: ../../specs/006-method-record/spec.md
    title: Feature 006 specification, Key Entities
  - id: spec-008
    resource: ../../specs/008-eval-variance/spec.md
    title: Feature 008 specification, Key Entities
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-25T10:00:00Z }
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

# Feature 006 terms

These terms come from the Feature 006 specification.

| Term | Meaning |
|------|---------|
| Method | The parts of the platform that decide what a step produces and whether it is accepted: the pattern declaration, the template, the step definitions and their agents, the checks, the fixed procedures and the pinned agent runtime. One identity names it. |
| Accepted result | A step's output that passed its checks, or a review round left with blocking findings open, kept and reused for the same input under the same method. |

Sources: [Feature 006 specification](../../specs/006-method-record/spec.md).

# Feature 007 terms

These terms come from the Feature 007 specification.

| Term | Meaning |
|------|---------|
| Revision | The analyst's new proposal for a slice after the developer answers its questions; when accepted, it replaces the slice's parts. |

Sources: [Feature 007 specification](../../specs/007-analyst-answer-checks/spec.md).

# Feature 008 terms

These terms come from the Feature 008 specification.

| Term | Meaning |
|------|---------|
| Trial | One run of one eval case by its agent, judged by the case's graders; it passes or fails and carries its duration, tokens, cost and, in some suites, attempts. |
| Unstable case | An eval case some of whose trials passed and some failed in one run. |
| Eval measurement | What one eval run keeps about its cases and trials: the measures and what identifies the run, never protected content. |

Sources: [Feature 008 specification](../../specs/008-eval-variance/spec.md).

# Feature 009 terms

These terms come from the Feature 009 specification.

| Term | Meaning |
|------|---------|
| Declared value | A value the approved contract states, such as a limit, bound, allowed value or default, with the contract location that is its one home. |
| Deployment value | A value that changes with where a service runs: a listen address or port, the address of a service it calls, or a credential. It is read from configuration when the service starts. |
| Hard-coded value | A literal in a step's code that repeats a declared value, or a deployment value not read from configuration. |

Sources: [Feature 009 specification](../../specs/009-no-hard-coded-values/spec.md).

# Feature 010 terms

These terms come from the Feature 010 specification.

| Term | Meaning |
|------|---------|
| Request schema | What the approved contract states a request body may hold: its members' exact names, which are required, their types, and whether other members are allowed. |
| Refusal | The 400 answer a delivered service gives to a request its request schema does not allow, with a `message` naming what is wrong; nothing is changed. |

Sources: [Feature 010 specification](../../specs/010-strict-requests/spec.md).

# Feature 011 terms

These terms come from the Feature 011 specification.

| Term | Meaning |
|------|---------|
| Captured pattern | A pattern made from a run that ended done: its slice, tests and code as a template with named parameters, built later with no model call and used only after the developer approves its proof. |
| Parameter | A name, declared value or other literal value in a captured pattern's source slice that a later slice may give another value, with every place it occurs. |
| Proof | A build of a captured pattern's source slice from the captured pattern with no model call; it passes when every check passes and every file equals the accepted one. |

Sources: [Feature 011 specification](../../specs/011-capture-pattern/spec.md).
