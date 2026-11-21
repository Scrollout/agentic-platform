---
type: Component
title: ShellCheck
description: "Lints the shell scripts a commit changes (`*.sh`, and files without an extension whose first line is a sh or bash shebang); the pre-commit hook keeps only findings on the changed lines."
tags: [components, shell, static-analysis, checks]
status: draft
resource: https://github.com/koalaman/shellcheck
sources:
  - id: tasks
    resource: ../../specs/002-durable-execution/tasks.md
    title: Feature 002 tasks, T041
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/koalaman/shellcheck
    title: OpenSSF Scorecard result
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-20T00:00:00Z }
---

# Responsibility

Lints the shell scripts a commit changes (`*.sh`, and files without an extension whose first line is a sh or bash shebang); the pre-commit hook keeps only findings on the changed lines. Adopted by feature 002's T041 (owner, 2026-11-19) for a fast check of each commit; CI does
not run it. Licence GPL-3.0: it runs as its own container and is not linked into or shipped with
the platform. OpenSSF Scorecard: 4.7 (2026-11-09).

Sources: [Feature 002 tasks, T041](../../specs/002-durable-execution/tasks.md), [OpenSSF Scorecard result](https://api.securityscorecards.dev/projects/github.com/koalaman/shellcheck).

# Interfaces

Declared in `scripts/lint-staged.yaml` and run by `scripts/lint-staged.py` from the pre-commit
hook, over the staged shell files, in `gcc` format. A finding on a line the commit adds or changes refuses the commit; a run
that fails in a way the declaration does not name refuses it too (the linter could not run).

# Dependencies

The pinned image only; no network while it runs.

# Configuration

Pin `shellcheck` in `src/deploy/images.lock` (v0.11.0, by digest); its arguments are in
`scripts/lint-staged.yaml`.

# Operations

Pulled on first use; updated by replacing the pin.
