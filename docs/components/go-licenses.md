---
type: Component
title: go-licenses
description: "Google's Go dependency licence checker, pinned at v2.0.1, run over every platform Go module by the deps step of scripts/check.sh with forbidden and unknown licence types disallowed."
tags: [components, go, licences, supply-chain, checks]
status: draft
resource: https://github.com/google/go-licenses
sources:
  - id: check-script
    resource: ../../scripts/check.sh
    title: scripts/check.sh, s_deps
  - id: images-lock
    resource: ../../src/deploy/images.lock
    title: src/deploy/images.lock
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D40 known limit
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/google/go-licenses
    title: OpenSSF Scorecard for google/go-licenses
  - id: repo
    resource: https://github.com/google/go-licenses
    title: google/go-licenses repository
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-17T22:00:00Z }
---

# Responsibility

It classifies the licence of every Go dependency of a platform module and fails on a licence type
that is forbidden or that it cannot identify. It serves the rule that every component is under an
OSI-approved licence (Principle VI) and the CI licence analysis (Principle XIII). Licence
Apache-2.0 (GitHub reports it for google/go-licenses, checked 2026-11-17).

Sources: [google/go-licenses repository](https://github.com/google/go-licenses).

# Interfaces

- Run: `go-licenses check ./... --disallowed_types=forbidden,unknown --ignore github.com/agentic-platform`
  inside each Go module, from the `deps` step of `scripts/check.sh` (`s_deps`), in the `deps`
  CI job. The platform's own modules are MIT by the root `LICENSE`, which lies outside the
  mounted `src/`, so they are ignored.
- It runs in the same loop as [govulncheck](/components/govulncheck.md).

Sources: [scripts/check.sh](../../scripts/check.sh).

# Dependencies

Built with `go install` in the pinned `golang` image into `/gopath/bin`. Its dependencies stay in
the tool's binary and the module cache. It needs the `bridge` network for the install.

Sources: [scripts/check.sh](../../scripts/check.sh).

# Configuration

Pin: `go-licenses  github.com/google/go-licenses/v2@v2.0.1` in `src/deploy/images.lock`. The Go
checksum database verifies the download.

Sources: [src/deploy/images.lock](../../src/deploy/images.lock).

# Operations

- Updated through the monthly dependency review.
- Known limit: it scans the platform modules, not the dependencies of the other installed tools
  (research D40).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md).

# Supply chain

OpenSSF Scorecard 5.0 (2026-11-09, read from the Scorecard API on 2026-11-17). The research's OSS
table (D22) has no row for it. It runs only in CI, is pinned by version, and the Go checksum
database verifies it. It ships in no image.

Sources: [OpenSSF Scorecard for google/go-licenses](https://api.securityscorecards.dev/projects/github.com/google/go-licenses).
