---
type: Component
title: actions/setup-go
description: GitHub's actions/setup-go action, pinned at v7.0.0 by commit hash in the CI workflows ([research D41](../../specs/001-prose-to-accepted-slice/research.md)); MIT, OpenSSF Scorecard 6.6 on 2026-11-09.
resource: https://github.com/actions/setup-go
tags: [components, ci, github-actions]
status: draft
sources:
  - id: repo
    resource: https://github.com/actions/setup-go/tree/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e
    title: actions/setup-go at v7.0.0
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/actions/setup-go
    title: OpenSSF Scorecard for actions/setup-go
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-16T12:00:00Z }
---

# Responsibility

Installs the host Go the live evals need, at the golang-race pin's version, with caching off. Used by `.github/workflows/evals.yml` only (ADR 0044).

# Pin and health

- `actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` (tag v7.0.0), the tag's commit; updated by the monthly manual review (Principle XIII exception).
- Licence MIT; OpenSSF Scorecard 6.6 on 2026-11-09: Code-Review 10, Maintained 10; no signed releases.

Sources: [actions/setup-go at v7.0.0](https://github.com/actions/setup-go/tree/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e); [OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/actions/setup-go).
