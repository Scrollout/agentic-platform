---
type: Decision
title: "ADR 0103: Step-worker registrations name their project"
description: The engine keeps one step-worker registration per slice, step and project, and a run uses only its own project's workers, so several projects of one slice can have ready workers at once.
tags: [decisions, workers, runs]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/012-run-variance/plan.md
    title: Feature 012 plan, Decisions
  - id: research
    resource: ../../specs/012-run-variance/research.md
    title: Feature 012 research, R3
  - id: adr-0036
    resource: /decisions/0036-step-containers-per-project-resume-reuses-them.md
    title: "ADR 0036: Step containers per project; resume reuses them"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-27T18:00:00Z }
---

# Origin

The [feature 012 plan](../../specs/012-run-variance/plan.md), Decisions; the project owner chose to
start all of a trial set's step workers before it starts, on 2026-11-27 (spec, Clarifications).

Sources: [Feature 012 plan](../../specs/012-run-variance/plan.md).

# Context

- Each project has its own step containers
  ([ADR 0036](/decisions/0036-step-containers-per-project-resume-reuses-them.md)), but the engine
  keeps one registration per slice and step, so registering one project's workers moves the slice
  away from another's.
- A trial set builds one slice into k projects without the terminal.

Sources: [Feature 012 research, R3](../../specs/012-run-variance/research.md).

# Decision

- `WorkerRegistration` gains an optional `project`, which the launcher always sends. The engine
  keeps registrations by slice, step and project.
- A run uses only its project's registration; one without a project (kept from before) serves any
  project of its slice.
- `WorkersNotReady` names the step and project that has no worker.

Sources: [Feature 012 research, R3](../../specs/012-run-variance/research.md).

# Consequences

- Several projects of one slice can be ready at once; a run can no longer be served by another
  project's container.
- Every project's containers stay up until `ap down`, as before; a trial set adds up to five
  projects' containers.

Sources: [Feature 012 plan](../../specs/012-run-variance/plan.md).
