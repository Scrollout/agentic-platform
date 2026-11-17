---
type: Decision
title: "ADR 0054: The module-boundary check runs in CI, not in the pre-commit hook"
description: The arch step (go-arch-lint) runs in its own check.yml job for code changes; the pre-commit hook does not run it. Supersedes ADR 0043's run from the pre-commit hook; the rest of ADR 0043 stands.
tags: [decisions, ci, checks]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decision, 2026-11-17
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D54
  - id: tasks
    resource: ../../specs/001-prose-to-accepted-slice/tasks.md
    title: Feature 001 tasks, T098
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-17T15:00:00Z }
---

# Origin

The owner's decision on 2026-11-17, while T098 was planned. It supersedes
[ADR 0043](/decisions/0043-module-boundaries-checked-by-go-arch-lint.md) in one respect, that the
check also runs from the pre-commit hook; the rest of ADR 0043 stands.

Sources: [Feature 001 research, D54](../../specs/001-prose-to-accepted-slice/research.md).

# Context

Since T108 the pre-commit hook keeps only quick static checks, and CI runs the rest
([ADR 0044](/decisions/0044-full-checks-run-on-github-hosted-runners.md)); each `scripts/check.sh`
step runs in its own `check.yml` job, selected by the files it judges
([ADR 0053](/decisions/0053-check-jobs-run-for-the-files-they-judge.md)). The hook's tests commit
Go files into folders that no archfile maps, which the check refuses by design.

Sources: [Feature 001 research, D54](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- The `arch` step runs in its own `check.yml` job, `arch`, for the code paths that the `go` job
  judges.
- The pre-commit hook does not run it.
- An archfile change stays a protected path in `scripts/hooks/commit-msg`.

# Consequences

- A forbidden import is refused by CI, about a minute into the `arch` job, not at commit time.
- The hook and its tests stay as they are.

Sources: [Feature 001 tasks, T098](../../specs/001-prose-to-accepted-slice/tasks.md).
