---
type: Reference
title: Temporal
description: Temporal's licence, OpenSSF Scorecard, releases, self-hosted stores and default limits, as checked on 2026-11-17 for the platform's durable execution.
resource: https://github.com/temporalio/temporal
tags: [references, workflow, durable-execution, go]
status: draft
stale_after: 2027-05-17T00:00:00Z
sources:
  - id: temporal-repo
    resource: https://github.com/temporalio/temporal
    title: temporalio/temporal
  - id: temporal-sdk-go
    resource: https://github.com/temporalio/sdk-go
    title: temporalio/sdk-go
  - id: temporal-scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/temporalio/temporal
    title: OpenSSF Scorecard for temporalio/temporal
  - id: sdk-scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/temporalio/sdk-go
    title: OpenSSF Scorecard for temporalio/sdk-go
  - id: temporal-compose
    resource: https://github.com/temporalio/samples-server/tree/main/compose
    title: Temporal's Docker Compose examples (samples-server)
  - id: temporal-visibility
    resource: https://docs.temporal.io/self-hosted-guide/visibility
    title: Temporal self-hosted guide, Visibility
  - id: temporal-defaults
    resource: https://docs.temporal.io/self-hosted-guide/defaults
    title: Temporal self-hosted guide, default limits
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D56
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-17T21:30:00Z }
---

# Project

- Server `temporalio/temporal`: MIT licence, latest release v1.32.2 (2026-11-02), commits daily,
  about 24,000 stars, OpenSSF Scorecard 6.1 (2026-11-09).
- Go SDK `temporalio/sdk-go`: MIT licence, latest release v1.51.0 (2026-11-10), OpenSSF Scorecard
  7.0 (2026-11-09).

Sources: [temporalio/temporal](https://github.com/temporalio/temporal), [temporalio/sdk-go](https://github.com/temporalio/sdk-go), [OpenSSF Scorecard for temporalio/temporal](https://api.securityscorecards.dev/projects/github.com/temporalio/temporal), [OpenSSF Scorecard for temporalio/sdk-go](https://api.securityscorecards.dev/projects/github.com/temporalio/sdk-go).

# Self-hosting

- The Docker Compose examples moved from the archived `temporalio/docker-compose` (archived
  2026-01-05) to `temporalio/samples-server`, folder `compose`, with PostgreSQL, MySQL and
  development variants.
- Visibility stores: Elasticsearch, OpenSearch, MySQL, PostgreSQL and SQLite (v3.31.0 or later
  with server v1.20 or later); Cassandra is no longer supported for visibility.

Sources: [Temporal's Docker Compose examples (samples-server)](https://github.com/temporalio/samples-server/tree/main/compose), [Temporal self-hosted guide, Visibility](https://docs.temporal.io/self-hosted-guide/visibility).

# Default limits

- A payload (each argument and return value): warning at 512 KB, error at 2 MB; a gRPC message
  at most 4 MB.
- A workflow's history: warning at 10 MB or 10,240 events, error at 50 MB or 51,200 events, which
  terminates the workflow; both are dynamic configuration.
- At most 2,000 pending activities, child workflows, signals or cancellations per workflow (500
  or fewer recommended).

Sources: [Temporal self-hosted guide, default limits](https://docs.temporal.io/self-hosted-guide/defaults).
