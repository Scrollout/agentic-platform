---
type: Component
title: actions/setup-python
description: GitHub's actions/setup-python action, pinned at v7.1.0 by commit hash in the CI workflows ([research D41](../../specs/001-prose-to-accepted-slice/research.md)); MIT, OpenSSF Scorecard 6.6 on 2026-11-09.
resource: https://github.com/actions/setup-python
tags: [components, ci, github-actions]
status: draft
sources:
  - id: repo
    resource: https://github.com/actions/setup-python/tree/6d4fd22b20465ccc0f7354476588e8df7c9ca0e0
    title: actions/setup-python at v7.1.0
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/actions/setup-python
    title: OpenSSF Scorecard for actions/setup-python
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-16T12:00:00Z }
---

# Responsibility

Installs the Python version images.lock names (`ci-python`, added by T107) on a job's runner. Used by the jobs of `.github/workflows/check.yml` (ADR 0044).

# Pin and health

- `actions/setup-python@6d4fd22b20465ccc0f7354476588e8df7c9ca0e0` (tag v7.1.0), the tag's commit; updated by the monthly manual review (Principle XIII exception).
- Licence MIT; OpenSSF Scorecard 6.6 on 2026-11-09: Code-Review 10, Maintained 10; no signed releases.

Sources: [actions/setup-python at v7.1.0](https://github.com/actions/setup-python/tree/6d4fd22b20465ccc0f7354476588e8df7c9ca0e0); [OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/actions/setup-python).
