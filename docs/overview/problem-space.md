---
type: Project
title: Problem space
description: The needs the agentic platform has to meet, stated independently of any design, with prior art and the spec that addresses each.
tags: [overview, requirements, scope]
status: draft
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's answers, 2026-11-11
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# How to use these tables

Each row is a **need**: something users or operators require, stated without
naming a mechanism. The rows say what has to be solved, never how. The design
comes from specs and the [architecture](/architecture/overview.md). The
constitution's Principle I makes this the one place needs are stated.

- **ID** is `N-NNN`. It never changes and is never reused. Specs link to it.
- **In scope** is `yes`, `no`, `later` or `undecided`. Settle it in
  `/speckit-specify` or with a [decision](/decisions/index.md).
- **Prior art** links to how existing projects handle the need. It is optional
  background, never a requirement or the reason for a design choice.
- **Spec** links to `../../specs/<NNN-name>/spec.md` once a spec covers the row.
- **State** is `todo`, `specified`, `in progress` or `done`.
- Add, split or remove rows freely. This list is a first cut and has gaps.

Every `yes` and `later` on this page was set by the project owner on 2026-11-11.

Sources: [Project owner's answers, 2026-11-11](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).

# Purpose

| ID | Need | In scope | Prior art | Spec | State |
|----|------|----------|-----------|------|-------|
| N-001 | State who the platform serves, what problem it solves for them, and what it will not do | yes | | | done |
| N-002 | Serve individual developers, teams and organizations | yes | | | todo |
| N-003 | Support software projects | yes | | | todo |
| N-004 | Support agent work outside software projects | later | | | todo |
| N-005 | Run on the user's own infrastructure, and as a hosted service | yes | | | todo |

N-001 is settled by the [platform overview](/overview/platform.md).

# Agents

| ID | Need | In scope | Prior art | Spec | State |
|----|------|----------|-----------|------|-------|
| N-010 | Define an agent (instructions, model, tools, collaborators) and keep every version of it | undecided | [Docker Agent](/references/docker-agent.md) | | todo |
| N-011 | Run agents against more than one model provider, and switch providers without redefining the agent | undecided | [Docker Agent](/references/docker-agent.md) | | todo |
| N-012 | Package an agent and share it with others | undecided | [Docker Agent](/references/docker-agent.md) | | todo |
| N-013 | Know that a changed agent still behaves as expected before it is released | undecided | [Docker Agent](/references/docker-agent.md) | | todo |

# Sessions

| ID | Need | In scope | Prior art | Spec | State |
|----|------|----------|-----------|------|-------|
| N-020 | Several clients (web console, IDE, CLI, phone) see and steer the same live session at once | yes | [Agent Host Protocol](/references/agent-host-protocol.md) | | todo |
| N-021 | Reconnect to a session after a drop and see everything that happened meanwhile | undecided | [Agent Host Protocol](/references/agent-host-protocol.md) | | todo |
| N-022 | Approve, cancel or redirect an agent's work from any connected client | undecided | [Agent Host Protocol](/references/agent-host-protocol.md) | | todo |

N-020 is settled by [ADR 0003](/decisions/0003-ahp-client-session-protocol.md).

# Tenancy and safety

| ID | Need | In scope | Prior art | Spec | State |
|----|------|----------|-----------|------|-------|
| N-030 | Serve several organizations from one installation without one seeing another's data | yes | | | todo |
| N-031 | Limit what an agent may reach and do, and require a person's approval for actions that cannot be undone | undecided | [Docker Agent](/references/docker-agent.md) | | todo |
| N-032 | Keep what users tell agents private, for no longer than allowed, and erase it on request | undecided | | | todo |
| N-033 | Cap what a user, organization or agent may spend | undecided | [Docker Agent](/references/docker-agent.md) | | todo |

N-030 follows from N-002 and N-005: a hosted service serves several organizations.

# Operation

| ID | Need | In scope | Prior art | Spec | State |
|----|------|----------|-----------|------|-------|
| N-040 | Never silently lose work the platform has accepted | undecided | | | todo |
| N-041 | Keep running agents working while management functions are down | undecided | | | todo |
| N-042 | Show who did what, when, through an audit trail | undecided | | | todo |

# Reliable agent work

These needs come from the problem the platform exists to solve.
Constitution Principle XX makes them binding for the product.

| ID | Need | In scope | Prior art | Spec | State |
|----|------|----------|-----------|------|-------|
| N-050 | Applying the same method to the same input gives the same accepted result | yes | | | todo |
| N-051 | A result is accepted only when it passes checks that do not depend on a model's judgement | yes | | | todo |
| N-052 | A proven solution is captured once and reused, without the model generating it again | yes | | | todo |
| N-053 | Every result can be traced to the method, inputs, model and rules that produced it, and reproduced from them | yes | | | todo |
| N-054 | How much results vary from run to run is measured and shown | undecided | | | todo |
| N-055 | The project's principles, docs, specs, decisions, validations, tests and reviews hold at every stage of the lifecycle | yes | | | todo |
| N-056 | The author of a change cannot alter, skip or weaken the checks that judge it; only a recorded, independent decision can change a check | yes | | | todo |
| N-057 | A change stays within the files and areas its task allows; anything outside is rejected | yes | | | todo |
| N-058 | Code depends only on what the design allows; a dependency across a forbidden boundary is rejected | yes | | | todo |
| N-059 | Values that belong in data or configuration cannot be hard-coded | yes | | | todo |
| N-060 | Work cannot be reported done until its checks pass | yes | | | todo |
| N-061 | Work is reviewed by an agent or person that did not do it | yes | | | todo |
| N-062 | Every change traces back to a need, spec or decision | undecided | | | todo |
| N-063 | Review and fixing converge: a change reaches an accepted or escalated end in a bounded number of rounds, and a review cannot keep adding new demands | yes | | | todo |
