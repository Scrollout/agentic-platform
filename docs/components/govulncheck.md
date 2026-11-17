---
type: Component
title: govulncheck
description: "The Go team's vulnerability scanner, pinned at v1.9.0, run over every platform Go module by the deps step of scripts/check.sh in the deps CI job."
tags: [components, go, security, supply-chain, checks]
status: draft
resource: https://github.com/golang/vuln
sources:
  - id: check-script
    resource: ../../scripts/check.sh
    title: scripts/check.sh, s_deps
  - id: images-lock
    resource: ../../src/deploy/images.lock
    title: src/deploy/images.lock
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D22 and the D40 known limit
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/golang/vuln
    title: OpenSSF Scorecard for golang/vuln
  - id: repo
    resource: https://github.com/golang/vuln
    title: golang/vuln repository
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-17T22:00:00Z }
---

# Responsibility

It reports known vulnerabilities, from the Go vulnerability database, that the code of a platform
Go module actually reaches. It serves the constitution's rule that a known exploitable
vulnerability in a shipped component is fixed or mitigated (Principle XIII). The platform runs it
and does not rewrite it (Principle VI). Licence BSD-3-Clause, the Go project's (GitHub reports it
for golang/vuln, checked 2026-11-17).

Sources: [golang/vuln repository](https://github.com/golang/vuln), [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md).

# Interfaces

- Run: `govulncheck ./...` inside each Go module, from the `deps` step of `scripts/check.sh`
  (`s_deps`). A finding fails the step. The step skips when the tree has no Go files.
- Caller: the `deps` job of the CI workflow. The same step runs `go-licenses` in the same loop
  ([go-licenses](/components/go-licenses.md)).

Sources: [scripts/check.sh](../../scripts/check.sh).

# Dependencies

- It is built with `go install` in the pinned `golang` image into `/gopath/bin`. Its module
  dependencies stay in the tool's binary and the module cache, and none enters a platform
  module's `go.mod`.
- It needs network (the `bridge` network) to fetch the module and the vulnerability database.

Sources: [scripts/check.sh](../../scripts/check.sh).

# Configuration

- Pin: `govulncheck  golang.org/x/vuln/cmd/govulncheck@v1.9.0` in `src/deploy/images.lock`. The Go
  checksum database verifies the download.
- It has no configuration file of its own here.

Sources: [src/deploy/images.lock](../../src/deploy/images.lock).

# Operations

- Updated through the monthly dependency review, like every pin in `images.lock`.
- Known limit: it scans the platform modules, not the dependencies of the other installed tools
  (research D40, analyze 2026-11-15).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md).

# Supply chain

OpenSSF Scorecard 3.7 (2026-11-09, read from the Scorecard API on 2026-11-17). This is low. The
research's OSS table (D22) has no row for it, so no owner decision on the score is recorded
here. The reasons the risk is small are facts about the tool:

- The Go team, which also runs the vulnerability database, maintains it. It is the Go project's
  own tool.
- The Go checksum database verifies the module on install, and the version is pinned.
- It runs only in CI and does not ship in any image or generated project.

Sources: [OpenSSF Scorecard for golang/vuln](https://api.securityscorecards.dev/projects/github.com/golang/vuln), [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md).
