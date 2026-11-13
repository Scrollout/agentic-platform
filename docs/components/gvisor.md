---
type: Component
title: gVisor
description: "The application-kernel sandbox (runtime runsc) that every step, check, analyst and contract-lint container runs under."
tags: [components, sandbox, security]
status: draft
resource: https://github.com/google/gvisor
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research
  - id: gvisor-docker
    resource: https://gvisor.dev/docs/user_guide/quick_start/docker/
    title: gVisor Docker Quick Start
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T18:00:00Z }
---

# Responsibility

Runs agent-generated code and command-running tools in a sandbox stronger than a plain container
(Principle VIII, research D13). Licence Apache-2.0, OpenSSF Scorecard 6.2 (research D22).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [gVisor Docker Quick Start](https://gvisor.dev/docs/user_guide/quick_start/docker/).

# Interfaces

`runtime: runsc` per container in Compose; `runsc install` adds the runtime to Docker once, as part of
the developer's container runtime (research S12, D13).

# Dependencies

The host's Docker. It is not an image, so it has no pin in `images.lock`; it was tested at runsc
release-20261109.0 on Docker 29.

# Configuration

Step workers, the check service, the analyst and contract-lint use it, with read-only root, user 65532 (the step agent runs as the developer's uid:gid, `AP_HOST_USER`, so it can write the project), all
capabilities dropped and CPU, memory and process limits. A gVisor container cannot use Docker's
embedded DNS, so the engine and gateway have fixed addresses reached through `extra_hosts` (research
D13, T022c; `src/deploy/README.md`).

# Operations

Only one platform can run per machine because of the fixed subnets.
