---
type: Decision
title: "ADR 0074: Long acceptance suites run once, split across parallel CI jobs"
description: Each acceptance test runs in exactly one CI job. The Docker job no longer repeats the planted and Story 2 suites, the stop-point suite runs one job per stop point, and the planted suite one job per scenario, so a push's check run takes about 20 minutes instead of 80 with the same tests and trials.
tags: [decisions, ci]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's request and approval, 2026-11-20
  - id: run
    resource: https://github.com/Scrollout/agentic-platform/actions/runs/44781455877
    title: Check run 44781455877, job and test times
  - id: tasks
    resource: ../../specs/002-durable-execution/tasks.md
    title: Feature 002 tasks, T067
  - id: adr0044
    resource: /decisions/0044-full-checks-run-on-github-hosted-runners.md
    title: ADR 0044
  - id: adr0053
    resource: /decisions/0053-check-jobs-run-for-the-files-they-judge.md
    title: ADR 0053
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-20T21:30:00Z }
---

# Origin

The project owner asked on 2026-11-20 whether a push's 70 to 80 minute check run could be made
shorter with the same result, and approved all three steps proposed (task T067 of feature 002).

Sources: [Project owner's request](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np); [Feature 002 tasks, T067](../../specs/002-durable-execution/tasks.md).

# Context

- In check run 44781455877 the run took as long as its slowest job, "Service tests with Docker",
  80 minutes: `TestStopPoints` 33 minutes (4 stop points, 3 trials each, one after another),
  `TestPlanted` 26 minutes, the `TestUS2` tests about 8 minutes, the rest about 13 minutes.
- `TestPlanted` and the `TestUS2` tests also ran in their own jobs (`ap test planted`, 28
  minutes; `ap test us2`, 11 minutes), so they ran twice per push.
- A Free organization runs up to 20 hosted jobs at once; each job spends 2 to 3 minutes on its
  setup (module cache, gVisor).

Sources: [Check run 44781455877](https://github.com/Scrollout/agentic-platform/actions/runs/44781455877); [ADR 0044](/decisions/0044-full-checks-run-on-github-hosted-runners.md).

# Decision

- Every test of the Docker suites runs in exactly one job of a push's check run, and a test of
  the repository proves it from the workflow and the test sources.
- The Docker job skips the tests that other jobs run: `TestPlanted`, the `TestUS2` tests and
  `TestStopPoints`. `scripts/check.sh` with no arguments still runs everything on one machine.
- `TestStopPoints` runs one job per stop point, and `TestPlanted` one job per scenario, through
  `ap test planted <scenario>` (terminal contract, "The test harness"). The lists of stop points
  and scenarios in the workflow must equal the test sources' lists.
- This refines ADR 0044's "one job per step and per acceptance suite": a suite may span several
  jobs, never fewer than one. ADR 0053's job selection applies to each of them alike.

Sources: [Feature 002 tasks, T067](../../specs/002-durable-execution/tasks.md); [ADR 0053](/decisions/0053-check-jobs-run-for-the-files-they-judge.md).

# Consequences

- A push's check run takes about as long as its slowest job, about 20 to 30 minutes, and spends
  fewer runner minutes, since nothing runs twice.
- A new stop point or planted scenario needs its job in the workflow, or the repository's test
  fails.
- More jobs start at once; the short ones may wait for a free runner.

Sources: [Check run 44781455877](https://github.com/Scrollout/agentic-platform/actions/runs/44781455877).
