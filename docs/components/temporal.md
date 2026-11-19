---
type: Component
title: Temporal
description: "The durable-execution engine that runs the engine's runs and operations as workflows (feature 002): the server in one process, its Go SDK in the engine, state in PostgreSQL."
tags: [components, workflow, durable-execution]
status: draft
resource: https://github.com/temporalio/temporal
sources:
  - id: research
    resource: ../../specs/002-durable-execution/research.md
    title: Feature 002 research, R1 to R5
  - id: temporal-ref
    resource: /references/temporal.md
    title: Temporal reference
  - id: adr-0065
    resource: /decisions/0065-temporal-and-postgresql-in-compose.md
    title: "ADR 0065: Temporal's server and PostgreSQL run in Compose"
  - id: adr-0067
    resource: /decisions/0067-a-run-is-one-temporal-workflow.md
    title: "ADR 0067: A run is one Temporal workflow"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-19T09:00:00Z }
---

# Responsibility

Keeps the engine's runs and operations going across a platform stop: each run and each operation
is a workflow, each call the engine makes is an activity, and waits (step workers, the developer,
the 14-day hand-over) are durable signals and timers. The engine's rules stay its own code; the
run record stays in the engine store ([ADR 0066](/decisions/0066-the-run-record-stays-the-engines.md)).
Server and Go SDK under MIT; OpenSSF Scorecard 6.0 (server) and 7.3 (SDK) on 2026-11-16.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md), [ADR 0067](/decisions/0067-a-run-is-one-temporal-workflow.md), [Temporal reference](/references/temporal.md).

# Interfaces

- The frontend's gRPC API, used only by the engine through the Go SDK (`go.temporal.io/sdk`),
  on the internal network `durable`.
- Namespace `ap`, task queues `runs` and `operations`.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md).

# Dependencies

- [PostgreSQL](/components/postgresql.md) for persistence and visibility.
- `temporalio/admin-tools` for the schema job.

Sources: [ADR 0065](/decisions/0065-temporal-and-postgresql-in-compose.md).

# Configuration

- Pinned by digest in `src/deploy/images.lock` (server and admin-tools on one version line).
- One process for every service, 4 history shards, the server version check off, retention 24 h.

Sources: [ADR 0065](/decisions/0065-temporal-and-postgresql-in-compose.md), [Temporal reference](/references/temporal.md).

# Operations

- The schema job runs before the server on every start and is safe to repeat; an upgrade moves
  one minor version at a time.
- Idle memory about 75 MiB on the owner's VM (research S14).

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md).
