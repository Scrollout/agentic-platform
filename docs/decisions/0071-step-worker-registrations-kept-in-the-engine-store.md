---
type: Decision
title: "ADR 0071: Step worker registrations are kept in the engine store"
description: The engine keeps each step worker's registration (slice, step, endpoint and the hash naming its grant) in its store, so a run continues by itself after an engine restart; the call token stays in the grant file, read at each call; this supersedes the in-memory registry of ADR 0036's Context.
tags: [decisions, architecture, workers, runs, workflow]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decision, 2026-11-19
  - id: research
    resource: ../../specs/002-durable-execution/research.md
    title: Feature 002 research, R10
  - id: adr0036
    resource: /decisions/0036-step-containers-per-project-resume-reuses-them.md
    title: ADR 0036
  - id: adr0034
    resource: /decisions/0034-step-worker-grants-as-files-read-at-each-authentication.md
    title: ADR 0034
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-19T17:00:00Z }
---

# Origin

Found by feature 002's stop-point test (T014) in CI run 44537905339: after an engine restart the
run repeats its attempt before any step worker has registered again and waits for the workers,
so a run does not continue by itself (FR-005). The project owner chose on 2026-11-19 to keep the
registrations in the engine store (research R10). It supersedes ADR 0036's statement that "the
engine keeps one worker address per slice and step, in memory" and that "an engine restart loses
its registrations"; the rest of ADR 0036 stands.

Sources: [Project owner's decision, 2026-11-19](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 002 research, R10](../../specs/002-durable-execution/research.md), [ADR 0036](/decisions/0036-step-containers-per-project-resume-reuses-them.md).

# Context

- Since feature 002 the engine's Temporal worker takes a stopped run up as soon as the engine
  starts, before anyone can register a step worker.
- The step containers keep running across an engine restart (ADR 0036); only `ap` registers them,
  and only when a developer runs it.
- A worker's call token is in its grant file, named by the SHA-256 of the worker token and read
  at every authentication (ADR 0034).

Sources: [ADR 0034](/decisions/0034-step-worker-grants-as-files-read-at-each-authentication.md), [ADR 0036](/decisions/0036-step-containers-per-project-resume-reuses-them.md).

# Decision

- `registerWorker` writes the registration to the engine store before it answers: the slice, the
  step, the endpoint and the hash that names the worker's grant file. Registering again replaces
  it. Erasing the slice removes it.
- The engine reads the registrations at start, and reads the call token from the grant file at
  each call. A registration whose grant file is gone counts as not registered.
- No token is written to the store.

Sources: [Feature 002 research, R10](../../specs/002-durable-execution/research.md).

# Consequences

- After an engine restart a run's repeated attempt reaches the still-running step containers and
  continues by itself (FR-005). A container that is really gone fails the call with
  `WorkersNotReady`, and the run waits for `ap build-workers --resume` as before.
- Revoking a grant (removing its file) still stops calls at once.
- The engine store gains one small file; erasure covers it (FR-017).

Sources: [Feature 002 research, R10](../../specs/002-durable-execution/research.md).
