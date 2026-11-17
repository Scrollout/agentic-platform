---
type: Decision
title: "ADR 0063: ap start is the start command; ap up stays unchanged"
description: A new launcher command, ap start, checks the machine, builds only the images that changed, then runs ap up unchanged; ap start --check shows what it would do; ap up stays as it is for CI and the test suites. Supersedes ADR 0061 in part (which command builds).
tags: [decisions, deployment, launcher]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's choice, 2026-11-18
  - id: research
    resource: ../../specs/003-one-command-start/research.md
    title: Feature 003 research, R1, R4, R6 and R7
  - id: plan
    resource: ../../specs/003-one-command-start/plan.md
    title: Feature 003 plan, Decisions
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-18T14:00:00Z }
---

# Origin

`/speckit-analyze` round 1 of feature 003 (2026-11-18) found that building inside `ap up`, as
[ADR 0061](/decisions/0061-up-builds-only-changed-images-by-reproducible-digest.md) said, changes
about seven existing test files, which all assume `up` starts images built beforehand. It also
found that the converging command had no way to show its difference without changing anything,
which Principle XVII requires. The project owner chose a new `ap start` (option B). Recorded in
the [plan](../../specs/003-one-command-start/plan.md)'s Decisions.

Sources: [Project owner's choice, 2026-11-18](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 003 research, R1, R4, R6 and R7](../../specs/003-one-command-start/research.md).

# Decision

- `ap start` is the developer's start command. It takes the start lock, checks the machine,
  runs `build.sh --changed` with every image, then runs `ap up` with the resolved options.
- `ap start --check` shows the failing prerequisites, the images it would build and whether it
  would start, and changes nothing.
- `ap up` keeps its contract and behaviour. CI and the test suites build with `build.sh <name>`
  and start with `up`, as before. `up` only gains the remote-access options, which `start`
  passes on.
- Everything else in ADR 0061 stands: the reproducible candidate digest, the fingerprints in
  `built.env`, and the one password prompt.

# Consequences

- No existing test changes.
- The launcher has two start commands: `start` for people and `up` for CI and tests. The
  terminal contract says which is which.

Sources: [Feature 003 plan, Decisions](../../specs/003-one-command-start/plan.md).
