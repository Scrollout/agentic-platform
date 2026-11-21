---
type: Decision
title: "ADR 0077: Variant builds share a build cache; each run stays alone"
description: Supersedes the mechanics of ADR 0076. A grader's variant builds run one per grading container with a per-trial build cache mounted for builds only; each test binary then runs in its own container with the project read-only and its package folder as working directory, reported through test2json with -test.v=test2json; the new graders run only when the trial's earlier graders passed.
tags: [decisions, evals, sandbox]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/004-edge-case-graders/plan.md
    title: Feature 004 plan, Decisions
  - id: research
    resource: ../../specs/004-edge-case-graders/research.md
    title: Feature 004 research, R3 and S8
  - id: adr0076
    resource: /decisions/0076-variant-graders-build-first-run-each-alone.md
    title: ADR 0076
  - id: test2json
    resource: https://pkg.go.dev/cmd/test2json
    title: Command test2json
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-21T17:00:00Z }
---

# Origin

The `/speckit-analyze` round 1 of [feature 004](../../specs/004-edge-case-graders/plan.md)
(fresh reviewer, 2026-11-21) found that ADR 0076's "build every variant in one grading container"
does not fit the evals' grading runner, which starts one container per `go` command with a fresh
cache, and that a run needs its package folder as working directory and `-test.v=test2json` to
report passing tests. The plan's research R3 was revised; this decision supersedes ADR 0076's
mechanics. ADR 0076's rule (build first, run each binary alone, kill only against a passing
baseline) stands.

Sources: [Feature 004 research, R3](../../specs/004-edge-case-graders/research.md); [ADR 0076](/decisions/0076-variant-graders-build-first-run-each-alone.md).

# Context

- Compiling a variant runs none of the agent's code, so a shared build cache is safe while only
  builds use it; running a test binary executes the agent's code, so a run must share nothing.
- On a warm host the first variant built in 3.7 s and each further one in about 1.25 s; a run
  took about 0.13 s.
- `go tool test2json` reports passing tests only with `-test.v=test2json`.

Sources: [Feature 004 research, S8](../../specs/004-edge-case-graders/research.md); [Command test2json](https://pkg.go.dev/cmd/test2json).

# Decision

- Each variant is built by its own `go test -c` in a grading container, with one build-cache
  folder per trial mounted writable for build commands only.
- Each test binary runs in its own fresh grading container with no build cache, the project root
  and the binaries' folder read-only, and its package folder as working directory, through
  `go tool test2json -t <binary> -test.v=test2json -test.count=1`.
- A grader's count is its fixed list (held-out tests, mutants), so a test or mutant that never ran
  is named, not dropped.
- A new grader runs only when every earlier grader of the trial passed; otherwise it fails with
  `not run: <grader> failed`, which leaves the trial's verdict unchanged.

Sources: [Feature 004 research, R3 and R4](../../specs/004-edge-case-graders/research.md).

# Consequences

- One cold build per trial; a trial that is already red costs no variant work.
- The summary shows no counts for a trial whose earlier grader failed.

Sources: [Feature 004 plan](../../specs/004-edge-case-graders/plan.md).
