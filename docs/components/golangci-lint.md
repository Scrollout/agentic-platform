---
type: Component
title: golangci-lint
description: "Runs Go linters (with gosec) on the Go packages a commit changes; the pre-commit hook keeps only findings on the changed lines."
tags: [components, golangci, go, static-analysis, checks]
status: draft
resource: https://github.com/golangci/golangci-lint
sources:
  - id: tasks
    resource: ../../specs/002-durable-execution/tasks.md
    title: Feature 002 tasks, T041
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/golangci/golangci-lint
    title: OpenSSF Scorecard result
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-20T00:00:00Z }
---

# Responsibility

Runs Go linters (with gosec) on the Go packages a commit changes; the pre-commit hook keeps only findings on the changed lines. Adopted by feature 002's T041 (owner, 2026-11-19) for a fast check of each commit; CI does
not run it. Licence GPL-3.0: it runs as its own container and is not linked into or shipped with
the platform. OpenSSF Scorecard: 7.7 (2026-11-16).

Sources: [Feature 002 tasks, T041](../../specs/002-durable-execution/tasks.md), [OpenSSF Scorecard result](https://api.securityscorecards.dev/projects/github.com/golangci/golangci-lint).

# Interfaces

Declared in `scripts/lint-staged.yaml` and run by `scripts/lint-staged.py` from the pre-commit
hook, over the Go module of `src/go.work` the staged files belong to, from inside the module, over the changed package folders, offline with the hook's Go module cache and its own lint cache. A finding on a line the commit adds or changes refuses the commit; a run
that fails in a way the declaration does not name refuses it too (the linter could not run).

# Dependencies

The pinned image only; no network while it runs.

# Configuration

Pin `golangci-lint` in `src/deploy/images.lock` (v2.15.0, by digest); its arguments are in
`scripts/lint-staged.yaml`.

# Operations

Pulled on first use; updated by replacing the pin.
