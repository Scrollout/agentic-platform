---
type: Component
title: actions/upload-artifact
description: GitHub's actions/upload-artifact action, pinned at v7.0.2 by commit hash; a failed live eval job uploads its report folder with it, kept 7 days ([research D46](../../specs/001-prose-to-accepted-slice/research.md)); MIT, OpenSSF Scorecard 5.5 on 2026-11-09.
resource: https://github.com/actions/upload-artifact
tags: [components, ci, github-actions]
status: draft
sources:
  - id: repo
    resource: https://github.com/actions/upload-artifact/tree/cf430e030ddbb5b0abf93d22962f4752f3646cd9
    title: actions/upload-artifact at v7.0.2
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/actions/upload-artifact
    title: OpenSSF Scorecard for actions/upload-artifact
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-17T01:00:00Z }
---

# Responsibility

Keeps a failed live eval job's report folder as a workflow artifact for 7 days, so a failing trial's answer can be read ([ADR 0050](/decisions/0050-failed-eval-jobs-keep-their-outputs.md)). Used only by the evals job of `.github/workflows/evals.yml`.

# Pin and health

- `actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9` (tag v7.0.2), the tag's commit; updated by the monthly manual review (Principle XIII exception).
- Licence MIT; OpenSSF Scorecard 5.5 on 2026-11-09: Code-Review 10, Dangerous-Workflow 10, Maintained 3; no signed releases. GitHub's own action, the standard way to keep a job's files.

Sources: [actions/upload-artifact at v7.0.2](https://github.com/actions/upload-artifact/tree/cf430e030ddbb5b0abf93d22962f4752f3646cd9); [OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/actions/upload-artifact).
