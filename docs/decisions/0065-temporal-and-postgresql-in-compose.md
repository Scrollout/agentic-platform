---
type: Decision
title: "ADR 0065: Temporal's server and PostgreSQL run in Compose, with a schema job that converges them"
description: Temporal's server (all services in one process) and PostgreSQL 16 run as pinned, hardened Compose services on internal networks; a one-off admin-tools job converges both databases' schemas and the namespace before the server starts.
tags: [decisions, architecture, workflow, deployment]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's answers, 2026-11-19
  - id: research
    resource: ../../specs/002-durable-execution/research.md
    title: Feature 002 research
  - id: plan
    resource: ../../specs/002-durable-execution/plan.md
    title: Feature 002 plan, Decisions
  - id: temporal
    resource: /references/temporal.md
    title: Temporal reference
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-19T09:00:00Z }
---

# Origin

> Superseded in part by [ADR 0070](/decisions/0070-engine-registers-the-namespace-password-folder-private.md): the engine, not the schema job, registers the namespace `ap`, and the password file is mode 644 in a mode-700 folder.

Feature 002's [plan](../../specs/002-durable-execution/plan.md) adopts Temporal for the engine's durable execution, as [ADR 0056](/decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md) decided. The project owner chose PostgreSQL over the SQLite dev server on 2026-11-19 (research R1).

Sources: [Project owner's answers, 2026-11-19](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 002 research](../../specs/002-durable-execution/research.md), [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).

# Context

- Temporal supports PostgreSQL and MySQL for production; its SQLite dev server is not for production and its file schema cannot be upgraded.
- `temporalio/auto-setup` is deprecated; the vendor's Compose sample uses `temporalio/server` with an `admin-tools` schema job and PostgreSQL 16.
- An upgrade must update the schema before the server, one minor version at a time.
- On the owner's clean VM the two services ran non-root, with a read-only root and no capabilities, at about 200 MiB idle together (research S14).

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md).

# Decision

- Services `durable-store` (`postgres:16-alpine`, user 70:70), `durable-schema` (`temporalio/admin-tools`, `temporal-sql-tool` create, setup and update for both databases, then the namespace `ap` with 24 h retention when missing) and `durable` (`temporalio/server`, one process, 4 history shards, version check off), all pinned by digest in `src/deploy/images.lock`.
- The schema job and the server share one version; the job runs before the server on every start, so an upgrade updates the schema first.
- Networks `durable` (engine and server) and `durable-store` (server, job, PostgreSQL), both internal; the data in the volume `durable-store`, kept by `ap down`.
- Hardened as every platform service: read-only root, no capabilities, no new privileges, memory limits.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md), [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).

# Consequences

- Two long-running containers and a one-off job join the platform, within SC-004's 512 MiB idle.
- The trust in the internal networks and a per-install password extends feature 001's Principle V exception, approved by the owner for the local version only.
- A Temporal upgrade is a pin change that passes the regression suites, one minor version at a time.

Sources: [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).
