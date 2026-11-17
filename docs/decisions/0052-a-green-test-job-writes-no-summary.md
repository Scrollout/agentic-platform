---
type: Decision
title: "ADR 0052: A green test job writes no summary at all"
description: The CI test jobs' action-junit-report steps set job_summary only when the job has failed, so a green job has no summary, not one that holds only a separator; failures still show the counts and each failed test. Replaces ADR 0051's separator-only summary for green jobs.
tags: [decisions, ci, testing]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decision, 2026-11-17
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D49
  - id: contexts
    resource: https://docs.github.com/en/actions/reference/workflows-and-actions/contexts
    title: GitHub Actions, contexts reference
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-17T11:00:00Z }
---

# Origin

The owner's decision on 2026-11-17, after seeing the `go` and `py-tests` jobs of a green run show
an empty summary. It changes one consequence of
[ADR 0051](/decisions/0051-a-green-test-report-stays-quiet.md), which otherwise stands.

Sources: [Feature 001 research, D49](../../specs/001-prose-to-accepted-slice/research.md).

# Context

With `skip_success_summary` the action still writes a separator line to a green job's summary,
which GitHub shows as an empty summary.

Sources: [Feature 001 research, D49](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- The `go`, `docker` and `py-tests` report steps set `job_summary: ${{ job.status != 'success' }}`.
- A green test job writes no summary; a failed one shows the counts and each failed test.
- `skip_success_summary` and `require_tests` stay.

# Consequences

- A job's summary appears only when the job failed.
- One more input to keep when the action is updated.

Sources: [GitHub Actions, contexts reference](https://docs.github.com/en/actions/reference/workflows-and-actions/contexts).
