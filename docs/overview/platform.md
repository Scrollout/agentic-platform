---
type: Project
title: Agentic Platform
description: What the agentic platform is, who it serves, the problem it solves, what it will not do, and its current state.
tags: [overview, project]
status: draft
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's answers and wording, 2026-11-11
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Purpose

The platform makes AI-driven software work reliable and repeatable. A method that worked once
gives the same result every time it is applied, across the whole project lifecycle.

Sources: [Project owner's answers and wording, 2026-11-11](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).

# Problem

Software engineering depends on determinism. A configuration, a script or a proven solution can
be applied any number of times, and the result is the same. AI agents are probabilistic. The same
request, method or prompt gives different results each time, and nothing in a project's lifecycle
can be relied on.

- Agents drift from principles, docs, specs and decisions.
- They skip validations, tests and reviews, and hard-code what belongs in data.
- They reach outside their task. They import code they should not depend on, and modify files they were not asked to touch, breaking boundaries the design relies on.
- They even change the tests so that their own code passes, which removes the one check meant to catch them.
- Instructions to the model are only advice, and nothing guarantees they were followed.
- They report work as done when it isn't.
- Independent review does not converge. A fresh reviewer and a fixer agent loop without end: each review finds new or different issues, and each fix brings new ones.

Each project turns into spaghetti, and every result has to be checked again by a person.

Sources: [Project owner's answers and wording, 2026-11-11](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).

# Approach

The model proposes, and something deterministic decides.

- **Acceptance:** a result is accepted only when deterministic checks prove it.
- **Reuse:** once a solution is proven, it is captured as a deterministic asset, such as a template, generator, declaration or script.
  - From then on it is reused instead of being generated again.
  - Over time, work moves from the probabilistic side to the deterministic side.

The [problem space](/overview/problem-space.md) states this as needs. Specs and decisions settle the design.

# Who it serves

- **Users:** individual developers, teams and organizations.
- **Scope:** software projects, for now.
- **Delivery:** self-hosted, and as a hosted service.

Sources: [Project owner's answers and wording, 2026-11-11](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).

# Non-goals

- Not another AI assistant with optional sub-agents. Enforcing the project's rules is the core, not an add-on.
- Not a model, and not a way to train one.
- Not a replacement for the editor, git or CI. It works with them.
- Not unsupervised autonomy. People decide what cannot be undone.
- Not a no-code workflow builder.
- Not, for now, agent work outside software projects.

# Status

The repository was bootstrapped on 2026-11-11 and has no code yet. All project knowledge lives in
the knowledge bundle in `docs/` ([ADR 0001](/decisions/0001-adopt-okf.md)).

- **Runtime:** the platform is designed on [Docker Agent](/references/docker-agent.md), which provides the agent definition format and runtime. The platform adds what serving several organizations needs ([ADR 0002](/decisions/0002-build-on-docker-agent.md)).
- **Shared sessions:** several clients can share and steer the same live session through the [Agent Host Protocol](/references/agent-host-protocol.md) ([ADR 0003](/decisions/0003-ahp-client-session-protocol.md)).

# Map

- [Problem space](/overview/problem-space.md): the needs the platform must meet.
- [Architecture overview](/architecture/overview.md): how the platform fits together.
- [Glossary](/overview/glossary.md): shared vocabulary.
- [Maintaining the knowledge bundle](/playbooks/maintain-knowledge-bundle.md): how to add or change knowledge.

# Open questions

- Which agents, components, APIs and data stores will the platform include?
- Which checks are built in, and how do projects add their own?
