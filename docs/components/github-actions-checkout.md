---
type: Component
title: actions/checkout
description: GitHub's actions/checkout action, pinned at v7.0.1 by commit hash in the CI workflows ([research D41](../../specs/001-prose-to-accepted-slice/research.md)); MIT, OpenSSF Scorecard 5.9 on 2026-11-09.
resource: https://github.com/actions/checkout
tags: [components, ci, github-actions]
status: draft
sources:
  - id: repo
    resource: https://github.com/actions/checkout/tree/3d3c42e5aac5ba805825da76410c181273ba90b1
    title: actions/checkout at v7.0.1
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/actions/checkout
    title: OpenSSF Scorecard for actions/checkout
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-16T12:00:00Z }
---

# Responsibility

Checks out the repository in a job, with persist-credentials false so no token stays in .git/config. Used by every job of `.github/workflows/check.yml` and by `.github/workflows/evals.yml` (ADR 0044).

# Pin and health

- `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1` (tag v7.0.1), the tag's commit; updated by the monthly manual review (Principle XIII exception).
- Licence MIT; OpenSSF Scorecard 5.9 on 2026-11-09: Code-Review 10, Maintained 0 at the scan (no commit in the 90 days before; last push 2026-10-17); no signed releases.

Sources: [actions/checkout at v7.0.1](https://github.com/actions/checkout/tree/3d3c42e5aac5ba805825da76410c181273ba90b1); [OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/actions/checkout).
