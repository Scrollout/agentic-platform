---
type: Decision
title: "ADR 0037: The terminal client's run commands detach on Ctrl-C and exit by run state; ap test runs on the host"
description: build, resume, replay and run exit with one code per run state (done 0; waiting, interrupted and handed-over 2; expired 5; cancelled 6), compare exits 7 when the runs differ, Ctrl-C (130) or SIGTERM (143) stops following while the run continues, and ap test builds a suite in the pinned Go container and runs its test binary on the host with the developer's own Docker access, an owner-approved exception to the test-security rule.
tags: [decisions, cli, launcher, runs, tests]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decisions on Ctrl-C, compare's exit code and ap test, 2026-11-14
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D33 (with D7, D15)
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: contract
    resource: ../../src/cli/contract.md
    title: Terminal contract
  - id: bash-exit
    resource: https://www.gnu.org/software/bash/manual/html_node/Exit-Status.html
    title: "Bash manual: a command killed by signal N exits 128 + N"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-14T15:00:00Z }
---

# Origin

The `/speckit-analyze` readiness check before T055 found that the terminal contract did not say
what following a run prints, how its end maps to an exit code, what Ctrl-C does, or how `ap test`
runs the T046 suites. The project owner decided on 2026-11-14 that Ctrl-C detaches, that
`compare` exits 7 when the runs differ, and that `ap test` runs the suites on the host. The
feature's research records it as D33, and the plan lists it in its Decisions table.

Sources: [Project owner's decisions, 2026-11-14](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np); [Feature 001 research, D33](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 plan](../../specs/001-prose-to-accepted-slice/plan.md).

# Context

- A run can last up to 30 minutes and spends model tokens. It stops as `ended` with an outcome,
  `waiting-for-developer` or `interrupted`.
- The engine keeps an `Idempotency-Key` and its first result for 24 hours.
- The T046 suites start their own labs with the container engine (`src/tests/lab`). The
  constitution's test-security rule says a test holds no raw container-engine socket.

Sources: [Feature 001 research, D7, D15 and D33](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- `build`, `resume`, `replay` and `run` exit with the code of the run's state: `done` 0;
  `waiting-for-developer`, `interrupted` and `handed-over` 2; `expired` 5; `cancelled` 6; a
  failed run operation 4; the engine lost 1. `interrupted` shares 2 with a waiting run because
  both are kept and go on only when the developer acts.
- `compare` exits 0 when the runs do not differ and 7 when they differ, a new general exit code.
- Ctrl-C while following stops following, never the run: the client names `run <run>` and
  `cancel <run>` and exits 130 (128 + SIGINT). SIGTERM detaches the same way and exits 143
  (128 + SIGTERM). Following any other operation detaches the same way.
- Each run command sends one random `Idempotency-Key` and resends it on a lost answer, so a retry
  never starts a second run. Model-written text is printed with control characters escaped.
- `ap test <suite>` builds the suite in the pinned Go container, offline, and runs its test binary
  on the host, as `scripts/check.sh` runs its `docker` suites. No tests image is built, and it
  needs no started session. The binary uses the developer's own Docker access on the host, which
  the test-security rule does not allow, so the plan records it as an owner-approved exception
  (Exceptions to Engineering Principles, 2026-11-14). No container ever holds the socket.
- Rejected: Ctrl-C cancelling the run; a separate code for `interrupted`; a tests image run as a
  client of an engine, recorded model and workers the launcher would start through Compose (a
  second deployment for tests and a rewrite of the suites).

Sources: [Feature 001 research, D33](../../specs/001-prose-to-accepted-slice/research.md); [Terminal contract](../../src/cli/contract.md); [Bash manual, exit status](https://www.gnu.org/software/bash/manual/html_node/Exit-Status.html).

# Consequences

- A script can tell "done", "needs the developer" and "lost" apart by the exit code alone.
- Closing the terminal or pressing Ctrl-C never cancels or loses a run.
- The launcher no longer only calls `docker compose`: for `test` it runs a test binary on the
  host with the developer's Docker access, under the plan's exception. The suites stay as
  written, and no container holds the container engine's socket.
- The `tests` Compose service and image are unused. Removing them changes existing deploy tests,
  so it waits for the owner's approval.

Sources: [Feature 001 research, D33](../../specs/001-prose-to-accepted-slice/research.md).
