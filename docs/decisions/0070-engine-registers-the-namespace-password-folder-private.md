---
type: Decision
title: "ADR 0070: The engine registers Temporal's namespace; the store's password file is private by its folder"
description: The engine registers the namespace ap when it connects, and the PostgreSQL password file is mode 644 in a mode-700 folder, because the schema job cannot reach the server and Compose ignores a file secret's owner and mode; this changes two details of ADR 0065.
tags: [decisions, architecture, workflow, deployment, security]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's answers, 2026-11-19
  - id: research
    resource: ../../specs/002-durable-execution/research.md
    title: Feature 002 research, R1 and S17
  - id: adr0065
    resource: /decisions/0065-temporal-and-postgresql-in-compose.md
    title: ADR 0065
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-19T14:00:00Z }
---

# Origin

Found while implementing feature 002's T004; the project owner chose both answers on 2026-11-19
(research R1, S17). It supersedes two details of [ADR 0065](/decisions/0065-temporal-and-postgresql-in-compose.md):
the schema job no longer registers the namespace, and the password file is no longer mode 600.

Sources: [Project owner's answers, 2026-11-19](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 002 research, R1 and S17](../../specs/002-durable-execution/research.md).

# Context

- The schema job must finish before the server starts, so it cannot register a namespace, which needs the running server.
- A fourth job that registers it would have to join the `durable` network, which ADR 0065 keeps to the engine and the server.
- Docker Compose mounts a file secret with the host file's owner and mode and ignores the secret's `uid`, `gid` and `mode` (S17), so PostgreSQL (uid 70) cannot read a mode-600 file owned by the developer.

Sources: [Feature 002 research, R1 and S17](../../specs/002-durable-execution/research.md).

# Decision

- The engine registers the namespace `ap` when it connects to the server, with the retention the engine contract's `x-limits.idempotency-key-retention-hours` gives, and keeps an existing one unchanged.
- `ap` makes the PostgreSQL password once in its own folder `durable/` in its state folder: the folder mode 700, the file mode 644, passed to the services as a Compose secret.

Sources: [Feature 002 research, R1 and S17](../../specs/002-durable-execution/research.md), [ADR 0065](/decisions/0065-temporal-and-postgresql-in-compose.md).

# Consequences

- The namespace's retention has one home, the engine contract, with the operation records' retention.
- On the host only the developer reaches the password, through the folder's mode; inside the containers any user can read it, which the internal `durable-store` network already limits to the server, the schema job and PostgreSQL.
- Two tests written for T003 and T005 change, with the owner's approval of 2026-11-19.

Sources: [Feature 002 research, R1 and S17](../../specs/002-durable-execution/research.md).
