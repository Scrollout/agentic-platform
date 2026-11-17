---
type: Component
title: gotestsum
description: Runs go test and writes its results as JUnit XML for the CI job summaries ([research D43](../../specs/001-prose-to-accepted-slice/research.md)); Apache-2.0, OpenSSF Scorecard 3.6 on 2026-10-19.
resource: https://github.com/gotestyourself/gotestsum
tags: [components, ci, testing, go]
status: draft
sources:
  - id: repo
    resource: https://github.com/gotestyourself/gotestsum/tree/v1.13.0
    title: gotestsum v1.13.0
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/gotestyourself/gotestsum
    title: OpenSSF Scorecard for gotestsum
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-16T16:00:00Z }
---

# Responsibility

Runs every Go test in `scripts/check.sh` with output equal to `go test`'s and the same exit code, and writes a JUnit file into `AP_TEST_REPORT_DIR` when it is set; for the `docker` step's test binaries it converts their recorded output ([ADR 0046](/decisions/0046-ci-test-results-are-junit-reports.md)).

# Pin and health

- `gotest.tools/gotestsum@v1.13.0`, the `gotestsum` line of `src/deploy/images.lock`; installed into the shared Go cache's `bin/` through the Go module proxy and checksum database; never in a built image.
- Licence Apache-2.0; OpenSSF Scorecard 3.6 on 2026-10-19 (no signed releases); about 2.7k stars, last release 2025-09-11.

Sources: [gotestsum v1.13.0](https://github.com/gotestyourself/gotestsum/tree/v1.13.0); [OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/gotestyourself/gotestsum).
