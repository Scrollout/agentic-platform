---
type: Component
title: Hadolint
description: "Lints the Dockerfiles a commit changes; the pre-commit hook keeps only findings on the changed lines, so older findings stay recorded in feature 002's T037."
tags: [components, docker, static-analysis, checks]
status: draft
resource: https://github.com/hadolint/hadolint
sources:
  - id: tasks
    resource: ../../specs/002-durable-execution/tasks.md
    title: Feature 002 tasks, T041
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/hadolint/hadolint
    title: OpenSSF Scorecard result
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-20T00:00:00Z }
---

# Responsibility

Lints the Dockerfiles a commit changes; the pre-commit hook keeps only findings on the changed lines, so older findings stay recorded in feature 002's T037. Adopted by feature 002's T041 (owner, 2026-11-19) for a fast check of each commit; CI does
not run it. Licence GPL-3.0: it runs as its own container and is not linked into or shipped with
the platform. OpenSSF Scorecard: 4.7 (2026-11-02).

Sources: [Feature 002 tasks, T041](../../specs/002-durable-execution/tasks.md), [OpenSSF Scorecard result](https://api.securityscorecards.dev/projects/github.com/hadolint/hadolint).

# Interfaces

Declared in `scripts/lint-staged.yaml` and run by `scripts/lint-staged.py` from the pre-commit
hook, over the staged `Dockerfile` and `Dockerfile.*` files. A finding on a line the commit adds or changes refuses the commit; a run
that fails in a way the declaration does not name refuses it too (the linter could not run).

# Dependencies

The pinned image only; no network while it runs.

# Configuration

Pin `hadolint` in `src/deploy/images.lock` (v2.15.1, by digest); its arguments are in
`scripts/lint-staged.yaml`.

# Operations

Pulled on first use; updated by replacing the pin.
