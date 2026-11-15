---
type: Decision
title: "ADR 0041: A test check needs a go test event for every declared test, and each check call runs in a fresh folder"
description: AllTestsPass and TestsFailAgainstStub read `go test -json -count=1` events and need one for every top-level Test function declared under `tests/**` and `review/**` (AllTestsPass a pass for each; TestsFailAgainstStub an event for each and at least one fail), so a test that vanishes fails the check; each check call runs with its own new, empty temporary folder as `GOCACHE`, `GOTMPDIR`, `TMPDIR`, `HOME` and `GOPATH`, removed after the call, and the pinned modules' cache shared read-only.
tags: [decisions, checks, security]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decisions and answers, 2026-11-15
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D37 (with D35)
  - id: data-model
    resource: ../../specs/001-prose-to-accepted-slice/data-model.md
    title: Feature 001 data model, Step checks
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions and threat model
  - id: test2json
    resource: https://pkg.go.dev/cmd/test2json
    title: "cmd/test2json: the go test -json events"
  - id: go-caching
    resource: https://pkg.go.dev/cmd/go#hdr-Build_and_test_caching
    title: "go command: build and test caching"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-15T16:00:00Z }
---

# Origin

T057's fresh review of Story 2 (2026-11-15) found two defects in how the check service runs Go.
AllTestsPass passed on exit code 0 and one `ok` line, so a probe whose application `init()`
exits 0 under a test binary passed with a failing test (R1). And the long-lived check service
shared one writable Go cache across all calls, without `-count=1` (R2). The owner decided both
on 2026-11-15; research D37 holds the design.

Sources: [Feature 001 research, D37](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 tasks, T057](../../specs/001-prose-to-accepted-slice/tasks.md).

# Context

- `go test -json` writes one event per test with its action; the go tool writes them, not the
  test, while the `ok` summary line only says the binary exited 0.
- The check service lives for the whole session, runs code the agents wrote, and before this
  decision kept its Go cache, home and GOPATH under one shared `/tmp`.
- The witness runs already read `-json` events (ADR 0039).

Sources: [cmd/test2json](https://pkg.go.dev/cmd/test2json); [go command: build and test caching](https://pkg.go.dev/cmd/go#hdr-Build_and_test_caching); [Feature 001 research, D35](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- The declared tests are the top-level `Test*` functions of the `_test.go` files under
  `tests/**` and `review/**`, each with its package.
- AllTestsPass runs `go test -json -count=1 ./...` and passes only on exit 0 with a `pass`
  event for every declared test; a declared test with no event, `fail` or `skip` fails it.
- TestsFailAgainstStub runs `go test -json -count=1 ./tests/...` and passes only when the tests
  build, every declared test under `tests/**` has a result event and at least one fails.
- EveryExampleBound's fallback uses the same events; the witness runs and their baseline keep
  ADR 0039's rule, which holds with `-json`.
- Each check call gets a new, empty temporary folder, removed after the call, as `GOCACHE`,
  `GOTMPDIR`, `TMPDIR`, `HOME` and `GOPATH`; every `go test` carries `-count=1`. The pinned
  modules' cache stays shared, read-only, in the image.
- Rejected: requiring every test to fail against the stub; counting `ok` or `--- PASS` lines;
  `-count=1` with a shared cache; one cache per project.

Sources: [Feature 001 research, D37](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 data model, Step checks](../../specs/001-prose-to-accepted-slice/data-model.md).

# Consequences

- A test that vanishes, because the code ended the test binary early, fails the check that
  needs it, so the code cannot pass by stopping the tests.
- Nothing a check writes through those variables survives its call; a test that writes to an
  absolute path elsewhere under `/tmp` still can (a stated limit).
- Each call compiles the standard library and the project from scratch, so checks and the lab
  suites take longer; if that proves too slow, the owner decides then.
- The check contract's runCheck wording changes first (T092); the checks, the check service and
  `Dockerfile.check` follow (T093, T094).

Sources: [Feature 001 tasks, T092 to T094](../../specs/001-prose-to-accepted-slice/tasks.md); [Feature 001 plan, threat model](../../specs/001-prose-to-accepted-slice/plan.md).
