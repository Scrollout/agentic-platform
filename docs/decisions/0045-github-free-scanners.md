---
type: Decision
title: "ADR 0045: GitHub's free scanners are on for the public repository"
description: Secret scanning with push protection, Dependabot alerts, CodeQL default setup for Go, Python and the workflows, and private vulnerability reporting are enabled; Dependabot's pull requests are not, since the project uses none.
tags: [decisions, security, supply-chain, ci]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's request, 2026-11-16
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D42
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Constitution, Principles VI and XIII
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-16T14:00:00Z }
---

# Origin

On 2026-11-16 the owner asked for GitHub's free, useful and relevant scanners on the public
repository `Scrollout/agentic-platform` ([ADR 0044](/decisions/0044-full-checks-run-on-github-hosted-runners.md)).

Sources: [Feature 001 research, D42](../../specs/001-prose-to-accepted-slice/research.md).

# Context

Public repositories get secret scanning, push protection, Dependabot alerts, the dependency graph
and CodeQL code scanning at no cost; push protection and Dependabot alerts are off until enabled.
The project uses no pull requests.

Sources: [Feature 001 research, D42](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- Enabled: secret scanning (whole history) and push protection; Dependabot alerts and the
  dependency graph; CodeQL default setup; private vulnerability reporting.
- Not enabled: Dependabot security and version updates, which open pull requests. Alerts are
  read in the monthly dependency review.

# Consequences

- A push holding a credential GitHub recognises is refused before it is public.
- New advisories on pinned Go and npm dependencies, and CodeQL findings in Go, Python and the
  workflows, appear in the Security tab; they are not in the CI run summary.
- Fixing a Dependabot alert stays a manual change through the usual process.

Sources: [Constitution](../../.specify/memory/constitution.md).
