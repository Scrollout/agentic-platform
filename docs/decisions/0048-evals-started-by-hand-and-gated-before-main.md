---
type: Decision
title: "ADR 0048: The live evals are started by hand and gated before main"
description: Supersedes ADR 0044 on when the live evals run. evals.yml has no push trigger; the owner starts it, and it runs only the suites whose inputs changed since the last green evals run. A keyless check job lists pending suites, passing with a notice on the feature branch and failing on main.
tags: [decisions, ci, evals]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decisions, 2026-11-16
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D45
  - id: dispatch
    resource: https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch
    title: GitHub Actions, workflow_dispatch
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-16T23:45:00Z }
---

# Origin

On 2026-11-16 the owner asked that the evals not run all the time and that heavy tests not run on
the host, since CI exists for them. Supersedes [ADR 0044](/decisions/0044-full-checks-run-on-github-hosted-runners.md)
on when the live evals run; the rest of ADR 0044 stands.

Sources: [Feature 001 research, D45](../../specs/001-prose-to-accepted-slice/research.md).

# Context

Each live eval run costs money on the test-scoped key. Under ADR 0044 and T115 the evals started on
every push that changed one of about 30 input paths, engine code and module files included, so most
pushes paid for a run. Constitution XIX requires the suites to pass before a merge or release, not
on every push.

Sources: [Feature 001 research, D45](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- `evals.yml` runs only when the owner starts it (`workflow_dispatch`). Its selector starts only
  the suites whose inputs changed since the branch's last successful evals run, or every suite when
  there is none or the dispatch input `all` is `true`.
- A keyless `evals-current` job in `check.yml` lists the suites whose evals are pending: a notice
  on `001-prose-to-accepted-slice`, a failure on `main`.

# Consequences

- A push never spends the key; the owner decides when an eval run is worth its cost.
- A merge to `main` with an agent change whose evals have not passed turns its check run red.
- The selector has two callers and one owner of the base-run rule.

Sources: [GitHub Actions, workflow_dispatch](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch).
