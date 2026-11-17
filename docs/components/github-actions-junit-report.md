---
type: Component
title: mikepenz/action-junit-report
description: Shows each CI test job's JUnit results in its job summary and as annotations, with a read-only token ([research D43](../../specs/001-prose-to-accepted-slice/research.md)); Apache-2.0, OpenSSF Scorecard 8.3 on 2026-11-16.
resource: https://github.com/mikepenz/action-junit-report
tags: [components, ci, github-actions, testing]
status: draft
sources:
  - id: repo
    resource: https://github.com/mikepenz/action-junit-report/tree/a16be3a69023e9bd7e748ede768b3282ea7b30ce
    title: mikepenz/action-junit-report at v6.7.1
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/mikepenz/action-junit-report
    title: OpenSSF Scorecard for mikepenz/action-junit-report
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-16T16:00:00Z }
---

# Responsibility

Reads the JUnit files the `go`, `docker` and `py-tests` jobs write and puts the counts and each failed test with its message into the job's summary, with `annotate_only: true`, so no check run and no `checks: write` ([ADR 0046](/decisions/0046-ci-test-results-are-junit-reports.md)). With `skip_success_summary: true`, a job whose tests all pass writes no table ([ADR 0051](/decisions/0051-a-green-test-report-stays-quiet.md)).

# Pin and health

- `mikepenz/action-junit-report@a16be3a69023e9bd7e748ede768b3282ea7b30ce` (tag v6.7.1), the tag's commit; updated by the monthly manual review (Principle XIII exception).
- Licence Apache-2.0; OpenSSF Scorecard 8.3 on 2026-11-16; last release 2026-11-06.

Sources: [mikepenz/action-junit-report at v6.7.1](https://github.com/mikepenz/action-junit-report/tree/a16be3a69023e9bd7e748ede768b3282ea7b30ce); [OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/mikepenz/action-junit-report).
