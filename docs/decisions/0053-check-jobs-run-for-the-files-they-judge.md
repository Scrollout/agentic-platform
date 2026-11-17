---
type: Decision
title: "ADR 0053: Each check job runs only when a file it judges changed"
description: .github/check-jobs.yaml declares per check.yml job the paths it judges or that it always runs; the config job selects the jobs from the files changed since the branch's last green check run. Supersedes ADR 0044's every step on every push.
tags: [decisions, ci]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decision, 2026-11-17
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D50
  - id: syntax
    resource: https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax
    title: GitHub Actions, workflow syntax
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-17T12:00:00Z }
---

# Origin

The owner's decision on 2026-11-17: the CI need not run every job for a Markdown change. It
supersedes the CI bullet of [ADR 0044](/decisions/0044-full-checks-run-on-github-hosted-runners.md)
in one respect, that every step runs on every push; the rest of ADR 0044 stands.

Sources: [Feature 001 research, D50](../../specs/001-prose-to-accepted-slice/research.md).

# Context

A docs-only push started all 15 jobs of `check.yml`, the `docker` job alone about 40 minutes.
The evals already run only the suites whose inputs changed since their last green run (ADR 0048).

Sources: [Feature 001 research, D50](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- `.github/check-jobs.yaml` declares, per job of `check.yml`, `always: true` or the paths it
  judges, as ordered globs with `!` excludes, as GitHub's `paths` filters.
- The `config` job selects the jobs whose paths a file changed since the branch's last green
  `check` run that is an ancestor of the commit; every job runs for a dispatch or without a base.
- The dependency, licence, secret and layout checks and `evals-current` always run.
- One selector module serves the check jobs and the eval suites.
- The run summary counts a job the selector skipped as passed and lists it as skipped.

# Consequences

- A push that changes only `specs/` runs a few minutes of checks, and one that changes only
  `docs/` adds `py_tests` (about 13 minutes), instead of about 40 for every job.
- A wrong path list can skip a job that a change needed; the declaration is a protected path
  (`.github/`), changed with the owner's `Approved-by:`, and the next push that touches the
  job's paths runs it.

Sources: [GitHub Actions, workflow syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax).
