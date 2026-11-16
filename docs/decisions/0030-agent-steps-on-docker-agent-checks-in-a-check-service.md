---
type: Decision
title: "ADR 0030: Agent steps run on Docker Agent's API server; checks and procedures run in a check service with its own contract"
description: Each agent step container runs the pinned Docker Agent serve api and the engine calls it with one session per attempt; checks run in a read-only check service and each fixed procedure in its own step worker, both serving one versioned contract reached only by the engine; reuse keys use the model each agent definition pins.
tags: [decisions, engine, checks, docker-agent, contracts]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's choices for agent steps, checks, procedures and the reuse-key model, 2026-11-13
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Project Structure
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D29 (with D5, D8, D13, D27)
  - id: contract
    resource: ../../src/check/api/preview/2026-11-13-preview/openapi.yaml
    title: Check service contract, 2026-11-13-preview
  - id: da-api
    resource: https://docker.github.io/docker-agent/features/api-server/
    title: Docker Agent - API server
  - id: aip-193
    resource: https://google.aip.dev/193
    title: "Google AIP-193: Errors"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T15:00:00Z }
---

# Origin

Feature 001 builds an approved slice through a declared state machine whose steps run agents or
fixed procedures and whose checks decide. The engine needed to know how it calls an agent step and
where checks and procedures run. The project owner chose on 2026-11-13; the plan records the
layout and the feature's research records the choice as D29.

Sources: [Project owner's choices, 2026-11-13](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np); [Feature 001 plan](../../specs/001-prose-to-accepted-slice/plan.md); [Feature 001 research, D29](../../specs/001-prose-to-accepted-slice/research.md).

# Context

- The engine holds no container-engine socket and must not run code an agent wrote (Principle
  VIII); step and check containers are started by the launcher, under gVisor.
- The engine already calls the analyst as a Docker Agent `serve api` service, one session per call.
- Components interact only through named, versioned contracts that live with their provider; for
  an adopted protocol its published API is the contract.
- An accepted result records the model that produced it (Principle XX), and each agent revision
  pins its model (Principle XV).

Sources: [Feature 001 research, D29](../../specs/001-prose-to-accepted-slice/research.md); [Docker Agent - API server](https://docker.github.io/docker-agent/features/api-server/).

# Decision

- Each agent step container runs the pinned Docker Agent `serve api` with its step's agent
  definition. The engine calls it as it calls the analyst: one session per attempt. Step sessions use
  `safety_policy: autonomous` inside the step container (gVisor, no egress, only the step's
  writable paths); the analyst stays `restricted` (research D29, T049b). The launcher
  registers the container with the engine on its behalf. Docker Agent's API is the contract.
- The model in a step's reuse key is the model its agent definition pins. The engine checks it
  against the model the session reports; a different model fails the attempt.
- Checks and fixed procedures are served by one small Go program with its own versioned OpenAPI
  contract, of which it is the provider. Its operations are synchronous and bounded by a time
  limit, keep nothing between calls, and accept only a token issued to the engine.
- The check service, in the check container with the project read-only, runs a named check on one
  attempt and returns the files an agent step's attempt left in its writable paths, which the
  engine stores by hash.
- Each procedure step gets its own worker container, started and registered by the launcher, with
  only that step's writable paths writable. It runs the step's procedure, writes its files into
  the project and returns them. The engine calls the endpoint the worker registered.
- The engine judges ContractLintPasses with the contract-lint service, not the check service.
- A verdict is bound to the files the engine stored: each check recomputes the hash of every output
  it judges from the folder and fails on a mismatch, and an attempt is accepted only when all its
  verdicts read the same files. Paths are restricted to a safe ASCII form, and nothing is read or
  written through a symbolic link.
- The contract and its generated code live in a `src/check` module; the service's command lives in
  the engine module, so it serves the engine's checks and procedures without copying them.

Sources: [Check service contract](../../src/check/api/preview/2026-11-13-preview/openapi.yaml); [Google AIP-193: Errors](https://google.aip.dev/193).

# Consequences

- The engine has one client for every agent, and no shim runs in any container.
- Checks and procedures can be tested through their contract alone, and a replay can compare their
  answers, since equal requests over an equal project give equal answers.
- Requests carry the project's files by hash and responses return them, so the engine carries the
  project's last accepted state from step to step.
- Still to decide, with the launcher's work: how a replay's reused files are written into the new
  project, and how the project's version history is kept.

Sources: [Feature 001 research, D29](../../specs/001-prose-to-accepted-slice/research.md).
