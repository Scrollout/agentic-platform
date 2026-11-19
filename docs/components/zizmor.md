---
type: Component
title: zizmor
description: "Audits the GitHub Actions workflows and actions a commit changes for security weaknesses, offline; the pre-commit hook keeps only findings on the changed lines."
tags: [components, github-actions, security, static-analysis, checks]
status: draft
resource: https://github.com/zizmorcore/zizmor
sources:
  - id: tasks
    resource: ../../specs/002-durable-execution/tasks.md
    title: Feature 002 tasks, T041
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/zizmorcore/zizmor
    title: OpenSSF Scorecard result
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-20T00:00:00Z }
---

# Responsibility

Audits the GitHub Actions workflows and actions a commit changes for security weaknesses, offline; the pre-commit hook keeps only findings on the changed lines. Adopted by feature 002's T041 (owner, 2026-11-19) for a fast check of each commit; CI does
not run it. Licence MIT: it runs as its own container and is not linked into or shipped with
the platform. OpenSSF Scorecard: not published by the Scorecard API (2026-11-19).

Sources: [Feature 002 tasks, T041](../../specs/002-durable-execution/tasks.md), [OpenSSF Scorecard result](https://api.securityscorecards.dev/projects/github.com/zizmorcore/zizmor).

# Interfaces

Declared in `scripts/lint-staged.yaml` and run by `scripts/lint-staged.py` from the pre-commit
hook, over the staged workflows and `action.yml` files, offline, in GitHub annotation format. A finding on a line the commit adds or changes refuses the commit; a run
that fails in a way the declaration does not name refuses it too (the linter could not run).

# Dependencies

The pinned image only; no network while it runs.

# Configuration

Pin `zizmor` in `src/deploy/images.lock` (1.32.0, by digest); its arguments are in
`scripts/lint-staged.yaml`.

# Operations

Pulled on first use; updated by replacing the pin.
