---
type: Decision
title: "ADR 0047: End to end on the Compose stack with the recorded model"
description: scripts/e2e.sh drives the real ap/Compose stack under gVisor against the recorded model, through a test-only Compose override file the launcher accepts only from src/tests/e2e/.
tags: [decisions, testing, ci, deployment]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decisions, 2026-11-16
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D44
  - id: compose
    resource: https://docs.docker.com/compose/how-tos/multiple-compose-files/merge/
    title: Docker Compose, merge and override
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-16T17:00:00Z }
---

# Origin

On 2026-11-16 the owner chose to build feature 001's T024 fully: the Compose stack itself, not
only the test lab, runs end to end on every push.

Sources: [Feature 001 research, D44](../../specs/001-prose-to-accepted-slice/research.md).

# Context

The acceptance suites run the platform's parts in a test lab whose settings are proved equal to
`compose.yaml` ([ADR 0038](/decisions/0038-lab-runs-the-build-path-with-synthesised-step-recordings.md));
`ap up` and `compose.yaml` themselves ran only by hand, with a live key.

Sources: [Feature 001 research, D44](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- `scripts/e2e.sh` (the `check.sh` step `e2e`) starts the stack with `ap up` and drives describe,
  questions, approvals, `build-workers`, `build`, `replay` and `compare` through `ap`.
- The recorded model comes in through Compose's own override mechanism: `AP_COMPOSE_OVERRIDE`
  names one file, which `ap up` accepts only from `src/tests/e2e/`; it replaces the gateway by the
  replay server and gives the run its own volumes.
- `e2e.sh` refuses (exit 3) while the platform is running on the machine.

# Consequences

- A change to `compose.yaml` or the launcher is proved on every push without a key.
- The product launcher carries one test seam, bounded to a folder of tests and announced when used.

Sources: [Compose merge](https://docs.docker.com/compose/how-tos/multiple-compose-files/merge/).
