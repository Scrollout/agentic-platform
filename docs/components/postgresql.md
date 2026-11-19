---
type: Component
title: PostgreSQL
description: "The database that holds Temporal's state (persistence and visibility) for the engine's durable execution (feature 002); not used by the engine directly."
tags: [components, database, durable-execution]
status: draft
resource: https://www.postgresql.org/
sources:
  - id: research
    resource: ../../specs/002-durable-execution/research.md
    title: Feature 002 research, R1, S13 and S14
  - id: adr-0065
    resource: /decisions/0065-temporal-and-postgresql-in-compose.md
    title: "ADR 0065: Temporal's server and PostgreSQL run in Compose"
  - id: versioning
    resource: https://www.postgresql.org/support/versioning/
    title: PostgreSQL versioning policy
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-19T09:00:00Z }
---

# Responsibility

Stores Temporal's executions, histories and visibility in the databases `temporal` and
`temporal_visibility`. Only the Temporal server and its schema job reach it. PostgreSQL Licence
(OSI-approved); OpenSSF Scorecard 6.1 (2026-10-19); version 16 is supported until November 2028.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md), [PostgreSQL versioning policy](https://www.postgresql.org/support/versioning/).

# Interfaces

- SQL on port 5432 on the internal network `durable-store`, password authentication with a
  password `ap` makes per install.

Sources: [ADR 0065](/decisions/0065-temporal-and-postgresql-in-compose.md).

# Dependencies

None beyond its image, `postgres:16-alpine`, pinned by digest.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md).

# Configuration

- Runs as 70:70 with a read-only root, no capabilities, tmpfs for `/tmp` and
  `/var/run/postgresql`; data in the volume `durable-store`.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md).

# Operations

- `ap down` keeps the volume; erasing a slice deletes its workflows through Temporal (research
  R8). Idle memory about 120 MiB on the owner's VM (research S14).

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md).
