---
type: Reference
title: Temporal
description: Temporal's licence, OpenSSF Scorecard, releases, images, self-hosted stores, upgrades, retries and default limits, as checked on 2026-11-17 and 2026-11-19 for the platform's durable execution.
resource: https://github.com/temporalio/temporal
tags: [references, workflow, durable-execution, go]
status: draft
stale_after: 2027-05-19T00:00:00Z
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
  - id: research-002
    resource: ../../specs/002-durable-execution/research.md
    title: Feature 002 research, S1 to S14
  - id: auto-setup
    resource: https://hub.docker.com/r/temporalio/auto-setup
    title: temporalio/auto-setup on Docker Hub (deprecated)
  - id: upgrade
    resource: https://docs.temporal.io/self-hosted-guide/upgrade-server
    title: Temporal self-hosted guide, upgrading the server
  - id: retry
    resource: https://docs.temporal.io/encyclopedia/retry-policies
    title: Temporal retry policies
  - id: retry-code
    resource: https://github.com/temporalio/temporal/blob/main/service/history/workflow/retry.go
    title: Temporal server source, activity retry intervals
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-19T09:00:00Z }
---

# Project

- Server `temporalio/temporal`: MIT licence, latest release v1.32.2 (2026-11-02), commits daily,
  about 24,000 stars, OpenSSF Scorecard 6.1 (2026-11-09), 6.0 on 2026-11-16 (Signed-Releases 0).
- Go SDK `temporalio/sdk-go`: MIT licence, latest release v1.51.0 (2026-11-10), OpenSSF Scorecard
  7.0 (2026-11-09), 7.3 on 2026-11-16.
- CLI `temporalio/cli`: MIT, latest release v1.10.1 (2026-11-13).

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

# Images and upgrades

- Self-hosting uses `temporalio/server` and `temporalio/admin-tools` (schema tool
  `temporal-sql-tool`, the `temporal` CLI); `temporalio/auto-setup` is deprecated, last tag 1.29.7.
- Digests on 2026-11-19: `temporalio/server:1.32.2` `sha256:7bf69188a6232fea14869b9cef3d94aa82a1601d99517f7e86c71f22da4e0670`,
  `temporalio/admin-tools:1.32.2` `sha256:4a95866efba29d7e17ab012df5232795e8b0ad0f7d5f9d1ec4df321f7792a754`.
- The server image runs as user `temporal` (uid 1000); on the owner's VM it ran with a read-only
  root and a tmpfs `/tmp` (feature 002 research S14).
- An upgrade updates the schema before the server, one minor version at a time; a server refuses
  an older schema.

Sources: [temporalio/auto-setup on Docker Hub (deprecated)](https://hub.docker.com/r/temporalio/auto-setup), [Temporal self-hosted guide, upgrading the server](https://docs.temporal.io/self-hosted-guide/upgrade-server), [Feature 002 research, S1 to S14](../../specs/002-durable-execution/research.md).

# Retries

- Activities retry by default (initial interval 1 s, coefficient 2, maximum interval 100 times
  the initial, unlimited attempts); `MaximumAttempts: 1` turns retries off.
- No jitter is documented, and the server's retry interval code adds none (code reading).

Sources: [Temporal retry policies](https://docs.temporal.io/encyclopedia/retry-policies), [Temporal server source, activity retry intervals](https://github.com/temporalio/temporal/blob/main/service/history/workflow/retry.go).
