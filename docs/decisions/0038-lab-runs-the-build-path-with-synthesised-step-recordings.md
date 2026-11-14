---
type: Decision
title: "ADR 0038: The test lab runs the build path as the platform does, with step agents answering from synthesised recordings"
description: The acceptance lab starts the check service, the project worker and per-project step workers from settings proved equal to compose.yaml, with grants, registrations and a durable store, and the step agents answer from recordings synthesised from reviewed answer scripts, never recorded live, replayed by WireMock with exact matching.
tags: [decisions, tests, lab, recorded-model, workers]
status: draft
decision: accepted
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D34 (with D26, D29 to D33)
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: adr-0036
    resource: /decisions/0036-step-containers-per-project-resume-reuses-them.md
    title: "ADR 0036: Step containers are kept per project"
  - id: adr-0037
    resource: /decisions/0037-terminal-run-commands-exit-codes-and-test-on-host.md
    title: "ADR 0037: ap test runs on the host"
  - id: docker-tmpfs
    resource: https://docs.docker.com/engine/storage/tmpfs/
    title: "Docker: tmpfs mounts are removed when the container stops"
  - id: wiremock-faults
    resource: https://wiremock.org/docs/simulating-faults/
    title: "WireMock: fixed delays per stub"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-14T18:00:00Z }
---

# Origin

The first `ap test planted` failed all five scenarios at its readiness check: a stale path to the
check command, no recordings of the step agents, and a lab that starts no check service, project
worker or step workers. The lab's engine also lacks the check service's and project worker's
addresses, which the engine has required since T054c and T053e, so no lab can start its engine.
Task T054f's design, research D34, answers what the lab must do. The owner said go ahead with
the design on 2026-11-14 and answered its three questions the same day: a test-only mount for
two planted scenarios, the non-converging review sent back to the requirement, and a leaner
`ap test us2`.

Sources: [Feature 001 research, D34](../../specs/001-prose-to-accepted-slice/research.md).

# Context

- The suites start their own labs on the host with the developer's Docker access
  ([ADR 0037](/decisions/0037-terminal-run-commands-exit-codes-and-test-on-host.md)).
- Step containers are per project, registered by the launcher, granted by files the engine reads
  ([ADR 0036](/decisions/0036-step-containers-per-project-resume-reuses-them.md),
  [ADR 0034](/decisions/0034-step-worker-grants-as-files-read-at-each-authentication.md)).
- The recorded model matches a request's body exactly, so a stale answer is never served
  (research D26).
- A step agent's session has several model turns, each request holding the earlier tool
  results; the live agents run `go test`, whose output carries elapsed times.
- The host is small and shared with another project's containers.

Sources: [Feature 001 research, D26, D31, D32 and D34](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- The lab starts its containers with `docker run` on its own internal network, not through
  Compose or `ap`, whose fixed subnets would collide with a running session. Its worker settings
  and mounts are the ones `compose.yaml` and `ap build-workers` declare, and a test without
  Docker proves they agree.
- The lab builds the agent, check and project images from the source under test, once per test
  process, unsigned and never pushed.
- With its build option, a lab starts the check service and the project worker; per project it
  starts the five step containers, writes their grant files, registers them and waits for their
  health, and removes them after that project's run has ended. At most 12 lab containers run at
  a time.
- The engine's store is a host folder, so it survives an engine restart; the lab's engine runs
  as the developer's user. Restarting the engine registers the running step containers again,
  as `ap resume` does.
- Every step recording is synthesised from a reviewed answer script by a small scripted server
  in a generator run; no step agent is recorded live, and nothing is spent. The acceptance run
  always replays through WireMock with exact matching and ends with no miss.
- For the planted scenarios US2.2 and US2.3 only, the lab mounts the code step's project root
  writable, `.git` still read-only, so TestsUnchanged and ScopeKept stop the run. This test-only
  deviation proves the second guard; production mounts are unchanged.
- The planted non-converging review waits until the gap in FR-016 and ADR 0031 is fixed upstream
  (`/speckit-clarify`). `ap test us2` drops its five one-trial copies of the planted scenarios.
- Every lab resource is named and labelled with its process, removed at the end, and removed by
  the next lab when its process died. `ap test` caps each suite's time.

Sources: [Feature 001 research, D34](../../specs/001-prose-to-accepted-slice/research.md); [Docker tmpfs mounts](https://docs.docker.com/engine/storage/tmpfs/); [WireMock fixed delays](https://wiremock.org/docs/simulating-faults/).

# Consequences

- A passing T046 suite shows what a session would do, with only the model replaced.
- A changed prompt, agent definition, engine message or Docker Agent version makes its
  recordings miss; one free generator run makes them again.
- The repository gains about 3 MB of generated mappings beside the small answer scripts, which
  are what a reviewer reads.
- In production the mounts stop an edit of the tests or a write outside the scope before any
  check; the planted suite proves the checks behind them with its test-only mount.
- A non-converging review cannot be confirmed by rerunning tests that already passed, so US2.6
  stays unproven until its requirement is fixed.
- Changing the committed lab and recording tool needs the owner's approval (constitution,
  "Existing tests change only with the owner's approval").

Sources: [Feature 001 research, D34](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 plan](../../specs/001-prose-to-accepted-slice/plan.md).
