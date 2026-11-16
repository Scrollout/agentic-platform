---
type: Decision
title: "ADR 0043: Module boundaries are checked by go-arch-lint, one archfile per Go module"
description: Each Go module in src/go.work declares its packages as components and their allowed imports in a .go-arch-lint.yml; go-arch-lint v1.19.1, built from source in the pinned Go image and run offline from scripts/check.sh and the pre-commit hook, fails any undeclared import and any unmapped package. The platform's own modules come first; generated projects come later.
tags: [decisions, architecture, checks, supply-chain]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decisions, 2026-11-15
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D40
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: tasks
    resource: ../../specs/001-prose-to-accepted-slice/tasks.md
    title: Feature 001 tasks, T098 and T099
  - id: reference
    resource: /references/go-arch-lint.md
    title: go-arch-lint reference
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Constitution, Principles VI, XIII, XVI and XVII
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-15T18:00:00Z }
---

# Origin

Principle XVII requires the platform to "reject a change that … adds a dependency that crosses a
boundary the project's design forbids". So far no check applied that rule to the platform's own
Go code. The layering lived only in the engine README and in reviews. On 2026-11-15 the owner
chose the tool, how it is installed and run, how it is configured, and the order of scope.
Research D40 holds the design and the dependency graph derived from the code as it is.

Sources: [Feature 001 research, D40](../../specs/001-prose-to-accepted-slice/research.md); [Constitution](../../.specify/memory/constitution.md).

# Context

- The workspace `src/go.work` has six modules: check, cli, engine, page, slice and tests. The
  engine alone has fifteen packages, and their import directions are a design choice. For
  example, `machine` has no dependency on `api`, and `checks` depends on nothing but `canonical`.
- go-arch-lint checks one module per run. It is configured per module with components and their
  allowed dependencies, and it reads every `.go` file's imports, test and tagged files included.
  It is MIT-licensed, released regularly, and has an OpenSSF Scorecard of 4.7.
- `go install pkg@version` with `GOPROXY=off` fails offline on a deprecation lookup. With the
  module cache served as a `file://` proxy, it builds offline.

Sources: [go-arch-lint reference](/references/go-arch-lint.md); [Feature 001 research, D40](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- **Tool**: `fe3dback/go-arch-lint` v1.19.1, pinned in `src/deploy/images.lock` by module
  version and module hash (`h1:8cGotGwAxpuKpOw9H8hhA6+EN2+HGU1eHcrwMvL4dC4=`).
- **Build**: it is built from source in the pinned `golang` image into `/gopath/bin`. The install
  runs offline through `GOPROXY=file:///gopath/pkg/mod/cache/download`. Only the first fetch,
  when the module is not cached, uses the network, in the arch step's setup, with the checksum
  database (owner, 2026-11-15). A binary whose `go version -m`
  module line is not the pin is refused.
- **Run**: offline (`--network none`, `GOPROXY=off`, `src/` read-only). It runs as the `arch`
  step of `scripts/check.sh`, which reports its setup, test and teardown time, and from the
  pre-commit hook.
- **Configuration**: one `src/<module>/.go-arch-lint.yml` per `use` of `src/go.work`, schema
  version 3:
  - every package is one component with an exact folder;
  - `allow.depOnAnyVendor: false`, so every vendor package is declared and listed per component;
  - `testdata` is excluded;
  - `allow.deepScan: false` (owner, 2026-11-15: deep scan stays off).

  Any undeclared import fails, and so does any package no component maps.
- **Rules**: each component may import exactly what it imports today (research D40's graph).
  That graph keeps the intended direction: the engine's leaves import nothing of the engine,
  `checks` imports only `canonical`, only `cmd/engine` imports `api`, and nothing imports a
  `cmd/*`. The two edges D40 calls findings are allowed now as named exceptions in
  `src/engine/.go-arch-lint.yml`, each with a comment naming its follow-up in T097, so the check
  passes on day one (owner, 2026-11-15). F1 is `steps` → `checksvc`: move `APIVersion()` and
  `CallTimeLimitMax()` into the check contract's generated package or a small shared package.
  F2 is logic in `cmd/engine`: move `sliceWorkers` and the agent digest into a package.
- **Approval**: an archfile is a check, so `scripts/hooks/commit-msg` treats `**/.go-arch-lint.yml`
  as a protected path (T010e, ADR 0029). A commit that changes one needs an `Approved-by:`
  trailer (owner, 2026-11-15).
- **Supply chain**: the OpenSSF Scorecard of 4.7 is accepted, mitigated by the version and
  module-hash pin, the build from source against the checksum database, and manual review of
  every update (owner, 2026-11-15).
- **Scope**: the platform's own `src/` modules first. Layering inside generated projects is a
  later pattern-level check (T099, not blocking the MVP).
- **Rejected**:
  - arch-go: thresholds allow partial compliance.
  - depguard through golangci-lint: prefix lists with no components, a package outside every
    list is not flagged, and it brings in golangci-lint.
  - An own `go list` test: a rewrite, and blind to build-tagged files.
  - The tool's Docker image: unsigned, with a std-loading bug until v1.19.1.

Sources: [Feature 001 research, D40](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 plan, Decisions](../../specs/001-prose-to-accepted-slice/plan.md).

# Consequences

- A change that adds an import across a forbidden boundary, or a new package, fails locally
  before commit, until its archfile allows it. An archfile is a check, so changing one follows
  Principle XVII (fixed before the change, changed only by a recorded, independent decision). The
  commit-msg hook enforces this with `Approved-by:`.
- One more pinned tool to review monthly. Its low Scorecard (no signed releases, unpinned CI
  dependencies) is offset by building from source against the checksum database and a pinned
  module hash. Its dependencies stay out of every platform `go.mod`.
- About 0.5 GB more in the shared Go cache. The step itself takes a few seconds once the tool is
  built.
- Known limits: it checks imports only (not `//go:embed`, `os/exec` or reflection). A sibling
  module counts as a vendor, matched by import path. Exit 1 covers both a violation and a broken
  setup.
- The work is T098, with tests first by a separate author. T099 follows for generated projects.

Sources: [Feature 001 tasks, T098 and T099](../../specs/001-prose-to-accepted-slice/tasks.md); [go-arch-lint reference](/references/go-arch-lint.md).
