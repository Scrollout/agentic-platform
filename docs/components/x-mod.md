---
type: Component
title: golang.org/x/mod
description: "The Go team's module library; the engine's DependenciesAllowed step check reads a project's go.mod with its modfile package."
tags: [components, go, dependencies]
status: draft
resource: https://pkg.go.dev/golang.org/x/mod/modfile
sources:
  - id: modfile-doc
    resource: https://pkg.go.dev/golang.org/x/mod/modfile
    title: golang.org/x/mod/modfile package documentation
  - id: x-mod-repo
    resource: https://go.googlesource.com/mod
    title: golang.org/x/mod repository
  - id: data-model
    resource: ../../specs/001-prose-to-accepted-slice/data-model.md
    title: Feature 001 data model, Step checks
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T02:20:00Z }
---

# Responsibility

Parses a generated project's `go.mod` for the `DependenciesAllowed` step check
(`src/engine/checks/steps.go`, T048): the module path and each require with its `// indirect`
mark. It is maintained by the Go team, and the go command itself uses the same parser. Licence
BSD-3-Clause, listed in `NOTICE`. Adopted with the owner's approval on 2026-11-13, in place of a
hand-written parser.

Sources: [modfile documentation](https://pkg.go.dev/golang.org/x/mod/modfile), [repository](https://go.googlesource.com/mod), [Feature 001 data model](../../specs/001-prose-to-accepted-slice/data-model.md).

# Interfaces

`modfile.ParseLax` in `checks.Dependencies`. The check applies the pattern's allowed list to
direct requires only, and its forbidden list to every require, `// indirect` ones included. A
`go.mod` it cannot parse, or one that names no module, fails the check.

# Dependencies

None at run time. Its `go.mod` names `golang.org/x/tools`, marked to be ignored by the release tool.

# Configuration

`v0.42.0` in `src/engine/go.mod`.

# Operations

Update it with the other `golang.org/x` modules. `scripts/check.sh` runs govulncheck and
go-licenses over the engine module.
