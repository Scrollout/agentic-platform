---
type: Decision
title: "ADR 0034: Step worker grants are files the engine reads at each authentication"
description: The launcher writes one grant file per step worker, named by the SHA-256 of its token, into a folder only the engine mounts read-only; the engine reads it at each worker-token authentication, so removing a file revokes the grant and a malformed one grants nothing.
tags: [decisions, engine, deploy, identity, workers]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decision on step worker grants, 2026-11-13
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D31 (with D12, D29, D30)
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: adr-0030
    resource: /decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md
    title: "ADR 0030: Agent steps run on Docker Agent's API server; checks and procedures run in a check service with its own contract"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T23:59:00Z }
---

# Origin

[ADR 0030](/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md) has the
launcher start and register each step worker with a token it creates. The engine read its worker
grants only when it started, but `ap build-workers` makes a slice's workers later. The project
owner decided on 2026-11-13 how the engine learns those grants. The feature's research records it
as D31, and the plan lists it in its Decisions table.

Sources: [Project owner's decision, 2026-11-13](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np); [Feature 001 research, D31](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 plan](../../specs/001-prose-to-accepted-slice/plan.md).

# Context

- Docker Agent's `serve api` cannot call `registerWorker`, so the launcher registers each step
  container on its behalf with the worker's token.
- Restarting the engine to load new grants would drop its in-memory registrations and running
  operations.
- The engine runs as its own user, non-root, with a read-only root file system.

Sources: [Feature 001 research, D29 and D31](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- `ap build-workers` writes one file per grant into a host folder that only the engine mounts,
  read-only. The file is named the lowercase hex SHA-256 of the worker's token and holds
  `{"slice","step","call_token"}`, where `call_token` is the token the engine calls that worker
  with.
- The folder and its files belong to the engine's user. The files are mode 0600, the folder mode
  700. The launcher writes them from a one-off container running as that user, with the tokens on
  standard input only.
- The engine reads the folder at each authentication of a token that is no session token
  (`api.Config.GrantsDir`). A removed file revokes the grant. An unreadable or malformed file
  grants nothing. The grants given at start (`WorkerGrants`) keep working.
- A project folder that holds only empty folders, including an empty `.git` (what `ap build-workers`
  makes), is not an existing project: the engine's startRun and `ap build-workers` refuse a folder
  only when it holds a file or symlink at any depth, or a `.git` that is not empty (owner,
  2026-11-13).
- Rejected: restarting the engine with new grants; a new engine operation to add grants.

Sources: [Feature 001 research, D31](../../specs/001-prose-to-accepted-slice/research.md).

# Consequences

- A grant takes effect without an engine restart, and revoking one is removing its file.
- No token appears in a file name, on a command line of the launcher or in the grant folder's
  listing.
- `ap down` removes every grant with the step workers.
- Inside an agent step's sandbox the call token is on Docker Agent's command line
  (`--auth-token`), a known limit.

Sources: [Feature 001 research, D31](../../specs/001-prose-to-accepted-slice/research.md).
