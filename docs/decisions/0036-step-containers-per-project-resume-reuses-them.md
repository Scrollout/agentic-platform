---
type: Decision
title: "ADR 0036: Step containers are kept per project; resume reuses them and replay builds a new project"
description: Each project of a slice gets its own step containers, named by slice, step and the project's hash and labelled with the project, kept until ap down; build-workers refuses while a run of the slice is active, resume registers the run's own containers again or, when they are gone, points to build-workers --resume, which rebuilds them for that interrupted run only; replay always builds a new, empty project.
tags: [decisions, deploy, launcher, workers, runs]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decisions on resume and replay set-up, 2026-11-14
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D32 (with D29, D31)
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: adr-0034
    resource: /decisions/0034-step-worker-grants-as-files-read-at-each-authentication.md
    title: "ADR 0034: Step worker grants are files the engine reads at each authentication"
  - id: rfc1035
    resource: https://www.rfc-editor.org/rfc/rfc1035#section-2.3.4
    title: "RFC 1035, 2.3.4: a label is at most 63 octets"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-14T12:00:00Z }
---

# Origin

[ADR 0034](/decisions/0034-step-worker-grants-as-files-read-at-each-authentication.md) has `ap
build-workers` start one container per step of a slice. The T053d review found that running it for
another project of the same slice replaced the slice's containers, also during a run. It also found
that `resume` and `replay` had no way to get workers for an existing project, since `build-workers`
refuses a folder that holds files. The project owner decided on 2026-11-14 how resume and replay get
their workers. The feature's research records it as D32, and the plan lists it in its Decisions
table.

Sources: [Project owner's decisions, 2026-11-14](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np); [Feature 001 research, D32](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 plan](../../specs/001-prose-to-accepted-slice/plan.md).

# Context

- The engine keeps one worker address per slice and step, in memory. Registering again replaces it.
- A slice has at most one active run, and a run is active until it ends (FR-021).
- A run is interrupted when the platform stopped. An engine restart loses its registrations, but
  the step containers keep running until `ap down`.
- The engine calls a worker by its container name, a host name of one DNS label of at most 63
  characters. A project name may itself be 63 characters.

Sources: [Feature 001 research, D29, D31 and D32](../../specs/001-prose-to-accepted-slice/research.md); [RFC 1035](https://www.rfc-editor.org/rfc/rfc1035#section-2.3.4).

# Decision

- A step container is named `<compose project>-worker-<slice>-<step>-<p>`, where `<compose
  project>` is `agentic-platform` in a real session and `<p>` is the first 12 hex digits of the
  SHA-256 of the project name. Its labels name the slice, the step and the
  project in full. `build-workers` for one project never removes or replaces another project's
  containers. Every step container stays up until `ap down`.
- `build-workers` refuses while a run of the slice has not ended, with exit 4 and `RunActive`,
  before it changes anything. It finds such a run with the engine's `listRuns`, using the
  session's client token.
- `resume <run>` reads the run with `getRun`, which gives its slice and project. For an interrupted
  run it checks that every step's container for that slice and project is running. It then
  registers each one again and passes the command on. It does not run `build-workers`. When the
  containers are gone, it refuses with exit 3, naming `ap build-workers --resume <run>`.
- `ap build-workers --resume <run>` (T053j) rebuilds an interrupted run's workers after `ap down` or
  a host reboot (FR-020). It reads the run's slice and project with `getRun`, accepts the existing
  folder only for that interrupted run, creates nothing in it, and starts and registers that
  project's containers.
- `replay <run> <project>` always builds into a new, empty project. It runs `build-workers`
  unchanged for the run's slice.
- Rejected: learning the run's project by reading the project worker's record through a one-off
  container (more launcher code and a record-reading path); registrations per project in
  the engine; the full project name in the container name.

Sources: [Feature 001 research, D32](../../specs/001-prose-to-accepted-slice/research.md).

# Consequences

- Building a second project of a slice leaves the first project's containers running.
- The engine, its contract and the grant files do not change.
- An interrupted run can be resumed while its containers run, also after an engine restart. After
  `ap down` or a host reboot it is resumed with `ap build-workers --resume <run>`, then `ap resume
  <run>` (FR-020). That command touches only the folder of the run it names and writes nothing
  there, and the project worker's history guard still checks `.git`, so FR-025 holds.
- `build-workers` for another slice while a run is active starts containers that wait, because
  the following `startRun` is refused with `RunActive`.
- Every project built in a session keeps its step containers, and their resources, until `ap down`.

Sources: [Feature 001 research, D32](../../specs/001-prose-to-accepted-slice/research.md).
