---
type: Decision
title: "ADR 0051: A green test report adds nothing to the job's summary"
description: The CI test jobs' action-junit-report steps set skip_success_summary, so a job whose tests all pass writes no results table; a failing job still shows the counts and each failed test; require_tests fails a step that finds no result. Replaces ADR 0046's counts for green jobs.
tags: [decisions, ci, testing]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decision, 2026-11-17
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D47
  - id: action
    resource: https://github.com/mikepenz/action-junit-report/tree/a16be3a69023e9bd7e748ede768b3282ea7b30ce
    title: action-junit-report at v6.7.1
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-17T09:00:00Z }
---

# Origin

The owner's decision on 2026-11-17, after finding the py-tests job's JUnit table on a green run
confusing. It changes one consequence of
[ADR 0046](/decisions/0046-ci-test-results-are-junit-reports.md), which otherwise stands.

Sources: [Feature 001 research, D47](../../specs/001-prose-to-accepted-slice/research.md).

# Context

On a green run the table held only the counts. Whether each job passed is already shown by the run's job list and its run table.

Sources: [Feature 001 research, D47](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- The `go`, `docker` and `py-tests` report steps set `skip_success_summary: true`.
- A job whose tests all pass writes no results table to its summary.
- A job with a failed test still shows the counts and each failed test with its message.
- The steps also set `require_tests: true`: a step that finds no test result fails the job, so a
  quiet summary always means tests ran and passed.

# Consequences

- A green test job's summary holds no table, only the action's separator line.
- The test counts of a green job are no longer in its summary; the step's log still has them.

Sources: [action-junit-report at v6.7.1](https://github.com/mikepenz/action-junit-report/tree/a16be3a69023e9bd7e748ede768b3282ea7b30ce).
