---
type: Component
title: go-arch-lint
description: "Checks each platform Go module's imports against the components and allowed dependencies its .go-arch-lint.yml declares; an undeclared import or an unmapped package fails the arch step of scripts/check.sh, run in CI."
tags: [components, go, architecture, static-analysis, checks]
status: draft
resource: https://github.com/fe3dback/go-arch-lint
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D40
  - id: reference
    resource: /references/go-arch-lint.md
    title: go-arch-lint reference
  - id: adr-0043
    resource: /decisions/0043-module-boundaries-checked-by-go-arch-lint.md
    title: "ADR 0043: Module boundaries are checked by go-arch-lint"
  - id: tasks
    resource: ../../specs/001-prose-to-accepted-slice/tasks.md
    title: Feature 001 tasks, T098 and T099
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-15T18:00:00Z }
---

# Responsibility

It checks the platform's own layering. Each Go module in `src/go.work` declares its packages as
components and lists, for each one, the components and the vendor packages it may import. Any
other import fails, and so does a package that no component maps. This applies the constitution's
rule that a change must not add a dependency that crosses a boundary the design forbids (Principle
XVII). Licence MIT. The rules and the graph they come from are in research
D40.

Sources: [Feature 001 research, D40](../../specs/001-prose-to-accepted-slice/research.md), [ADR 0043](/decisions/0043-module-boundaries-checked-by-go-arch-lint.md).

# Interfaces

- Configuration: `src/<module>/.go-arch-lint.yml`, archfile schema version 3, one per `use`
  entry of `src/go.work` (check, cli, engine, page, slice, tests).
- Run: `go-arch-lint check --project-path /src/<module> --output-color=false`, with `--json`
  where a test reads the result. Exit 0 means clean. Exit 1 means a violation, an unmatched file
  or a setup error (see the [reference](/references/go-arch-lint.md)).
- Callers: the `arch` step of `scripts/check.sh`, run by the `check.yml` job `arch` for code
  changes; the pre-commit hook does not run it ([ADR 0054](/decisions/0054-module-boundary-check-runs-in-ci.md); T098).

Sources: [go-arch-lint reference](/references/go-arch-lint.md), [Feature 001 tasks, T098](../../specs/001-prose-to-accepted-slice/tasks.md).

# Dependencies

- Built from source in the pinned `golang` image (`src/deploy/images.lock`). Its module dependencies
  (cobra, golang.org/x/tools, golang.org/x/mod, d2 and others) live only in the tool's binary
  and the module cache. None enters a platform module's `go.mod`.
- At run time it needs `go` on `PATH` to list the standard library, and nothing else.

Sources: [go-arch-lint reference](/references/go-arch-lint.md).

# Configuration

- Pin: `go-arch-lint github.com/fe3dback/go-arch-lint@v1.19.1` in `src/deploy/images.lock`, with
  its module hash `h1:8cGotGwAxpuKpOw9H8hhA6+EN2+HGU1eHcrwMvL4dC4=` on its own line. The Go
  checksum database verifies the download. After the build, `go version -m` on the binary must
  show that exact module line, or the step fails.
- Every archfile sets `allow.depOnAnyVendor: false` and `allow.deepScan: false`, and excludes
  `testdata` (`excludeFiles: ["^.*/testdata/.*$"]`). Components are exact folders, never `**`, so
  a new package fails until the archfile declares it.

Sources: [Feature 001 research, D40](../../specs/001-prose-to-accepted-slice/research.md).

# Operations

- Installed into the shared Go cache (`${XDG_CACHE_HOME:-~/.cache}/agentic-platform/go`, mounted
  as `/gopath`). The install runs offline first, with `GOPROXY=file:///gopath/pkg/mod/cache/download`
  and `--network none`. Only when the module is not cached does it fetch the module once from
  the Go proxy, with network and the checksum database. `GOPROXY=off` cannot be used, because
  `go install pkg@version` looks up deprecation.
- The check itself always runs with `--network none`, `GOPROXY=off` and `src/` read-only.
- Updated through the monthly dependency review, like every pin in `images.lock`. Each update is
  reviewed by hand.
- An archfile change is a protected path in `scripts/hooks/commit-msg`, so it needs an
  `Approved-by:` trailer (owner, 2026-11-15).

Sources: [go-arch-lint reference](/references/go-arch-lint.md), [ADR 0043](/decisions/0043-module-boundaries-checked-by-go-arch-lint.md).

# Supply chain

OpenSSF Scorecard 4.7 (2026-10-19). Pinned-Dependencies, Signed-Releases, SAST and Fuzzing score
0. The owner accepted the score (owner, 2026-11-15), mitigated by:

- the version and module-hash pin in `images.lock`;
- the build from source in the pinned Go image, verified against the Go checksum database;
- manual review of every update.

Sources: [go-arch-lint reference](/references/go-arch-lint.md), [ADR 0043](/decisions/0043-module-boundaries-checked-by-go-arch-lint.md).
