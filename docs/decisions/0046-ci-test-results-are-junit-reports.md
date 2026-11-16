---
type: Decision
title: "ADR 0046: CI test results are JUnit reports shown by action-junit-report"
description: Go tests run through gotestsum and the Python check tests through testrun.py, both writing JUnit XML; action-junit-report shows each test job's results in its summary with a read-only token.
tags: [decisions, ci, testing]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's request, 2026-11-16
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D43
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Constitution, Principle VI
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-16T16:00:00Z }
---

# Origin

On 2026-11-16 the owner found that the CI job summaries hold nothing useful and asked for the
tests' results the standard way (feature 001, T111; [ADR 0044](/decisions/0044-full-checks-run-on-github-hosted-runners.md)).

Sources: [Feature 001 research, D43](../../specs/001-prose-to-accepted-slice/research.md).

# Context

GitHub shows a job's summary from `$GITHUB_STEP_SUMMARY`. JUnit XML is the format CI tools read
for test results. The workflows' token is read-only, so a report that needs a check run
(`checks: write`) does not fit.

Sources: [Feature 001 research, D43](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- `scripts/check.sh` runs every Go test through `gotestsum` (pinned in `src/deploy/images.lock`),
  which writes a JUnit file per module or package when `AP_TEST_REPORT_DIR` is set.
- `scripts/testrun.py --junit <file>` writes the Python check tests' JUnit file with the standard
  library; no open-source reporter fits (D43).
- The `go`, `docker` and `py-tests` jobs end with `mikepenz/action-junit-report` (pinned by
  commit hash), `annotate_only: true`, which writes the counts and each failed test with its
  message into the job's summary.
- The runner's free disk is no longer reported.

# Consequences

- A failing job's summary names the failed tests and their messages.
- Two more pinned tools to review monthly; gotestsum's low Scorecard is accepted for a test-time
  tool that is never in a built image.

Sources: [Constitution](../../.specify/memory/constitution.md).
