---
type: Decision
title: "ADR 0076: Graders that run model-written code on variants build first, then run each alone"
description: A grader that runs an agent's code or tests against several variants compiles them all in one grading container, which runs none of that code, then runs each test binary in its own fresh grading container; a mutant counts as killed only when the same tests pass on the reference implementation.
tags: [decisions, evals, sandbox]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/004-edge-case-graders/plan.md
    title: Feature 004 plan, Decisions and threat model
  - id: research
    resource: ../../specs/004-edge-case-graders/research.md
    title: Feature 004 research, R3 and R4
  - id: cmd-go
    resource: https://pkg.go.dev/cmd/go
    title: Command go
  - id: test2json
    resource: https://pkg.go.dev/cmd/test2json
    title: Command test2json
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-21T16:00:00Z }
---

# Origin

The [feature 004 plan](../../specs/004-edge-case-graders/plan.md) made this decision for backlog
task T180, which the project owner approved on 2026-11-20.

Sources: [Feature 004 plan](../../specs/004-edge-case-graders/plan.md).

# Context

- The mutant grader runs the tests agent's tests against the reference implementation and eight
  mutants; the held-out grader runs held-out tests against the code agent's code. Both run model
  output.
- Each `go` command of the evals' grading runner starts a fresh container with a cold build cache,
  so one `go test` per variant compiles the dependencies again every time.
- If the variants ran in one container, the agent's tests, run first against the reference,
  could leave a file or a cache entry that makes every later variant fail, which would count every
  mutant as killed.
- `go test -c` compiles a test binary without running it; `-overlay` swaps a source file for one
  build; with `CGO_ENABLED=0` and no `go generate`, compiling runs no code of the module. `go tool
  test2json` runs a test binary and reports each test by name.

Sources: [Command go](https://pkg.go.dev/cmd/go); [Command test2json](https://pkg.go.dev/cmd/test2json); [Feature 004 research, R3](../../specs/004-edge-case-graders/research.md).

# Decision

- Such a grader works on a scratch copy of the agent's project, never on the project itself.
- It compiles every variant in one grading container, sharing the build cache there.
- It runs each test binary in its own fresh grading container, with the binary and the project
  read-only, and reads each test's result by name.
- A mutant counts as killed only when the same tests built and passed on the reference
  implementation, and its own run failed or panicked within the time limit. A time-out or a run
  that could not start is reported, and is neither a pass nor a kill.

Sources: [Feature 004 research, R3 and R4](../../specs/004-edge-case-graders/research.md).

# Consequences

- Grading costs one cold build per trial plus one short container per variant.
- The grading runner gains the run of one test binary beside its `go` commands.

Sources: [Feature 004 plan](../../specs/004-edge-case-graders/plan.md).
