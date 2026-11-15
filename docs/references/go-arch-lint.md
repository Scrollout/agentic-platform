---
type: Reference
title: go-arch-lint
description: What go-arch-lint v1.19.1 checks, its archfile schema version 3, how it classifies imports, its exit codes and output, and how it builds and runs offline, as checked on 2026-11-15.
resource: https://github.com/fe3dback/go-arch-lint
tags: [references, go, architecture, static-analysis]
status: draft
stale_after: 2027-05-15T00:00:00Z
sources:
  - id: gal-repo
    resource: https://github.com/fe3dback/go-arch-lint/tree/a34dbfc83fcf68b5b76aa6b4d7e959244f87f739
    title: fe3dback/go-arch-lint at commit a34dbfc8 (tag v1.19.1)
    last_modified: 2026-10-29T17:04:47Z
  - id: gal-syntax
    resource: https://github.com/fe3dback/go-arch-lint/blob/v1.19.1/docs/syntax/README.md
    title: go-arch-lint archfile syntax
  - id: gal-releases
    resource: https://github.com/fe3dback/go-arch-lint/releases
    title: go-arch-lint releases
  - id: gal-proxy
    resource: https://proxy.golang.org/github.com/fe3dback/go-arch-lint/@v/v1.19.1.info
    title: Go module proxy, go-arch-lint v1.19.1
  - id: gal-pkgsite
    resource: https://pkg.go.dev/github.com/fe3dback/go-arch-lint?tab=versions
    title: go-arch-lint versions (pkg.go.dev)
  - id: gal-scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/fe3dback/go-arch-lint
    title: OpenSSF Scorecard for go-arch-lint
  - id: gal-issue-59
    resource: https://github.com/fe3dback/go-arch-lint/issues/59
    title: "go-arch-lint issue 59: _test package makes a package import itself"
  - id: gal-pr-91
    resource: https://github.com/fe3dback/go-arch-lint/pull/91
    title: "go-arch-lint PR 91: tolerate missing optional std packages"
  - id: go-module-proxy
    resource: https://go.dev/ref/mod#goproxy-protocol
    title: Go modules reference, GOPROXY protocol (file:// proxies)
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D40 (the experiment)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-15T18:00:00Z }
---

# Release and project

- Latest release: v1.19.1, tagged 2026-10-29 (commit `a34dbfc8`), module hash
  `h1:8cGotGwAxpuKpOw9H8hhA6+EN2+HGU1eHcrwMvL4dC4=`. Earlier: v1.19.0 (2026-09-07), v1.18.0
  (2026-08-21, Go 1.27 support), v1.17.0 (2026-08-05), v1.16.0 (2026-07-09), v1.15.0
  (2026-05-04). First release v1.0.0 in 2020-08.
- `go.mod` says `go 1.25.0`; the README asks for Go 1.25 or later. It has no `replace`
  directive, so `go install …@v1.19.1` works.
- Licence MIT. About 630 stars, 29 forks, 17 open issues, not archived; one main maintainer who
  merges outside contributions.
- OpenSSF Scorecard 4.7 (2026-10-19): Maintained 8, Code-Review 7, License 10, Packaging 10;
  Pinned-Dependencies, Signed-Releases, SAST, Fuzzing and Security-Policy 0.
- No open issue names Go 1.26. Issue 75 (std packages failing to load in the Docker image) was
  fixed in v1.19.1 by PR 91.

Sources: [repository at v1.19.1](https://github.com/fe3dback/go-arch-lint/tree/a34dbfc83fcf68b5b76aa6b4d7e959244f87f739), [releases](https://github.com/fe3dback/go-arch-lint/releases), [Go module proxy](https://proxy.golang.org/github.com/fe3dback/go-arch-lint/@v/v1.19.1.info), [pkg.go.dev versions](https://pkg.go.dev/github.com/fe3dback/go-arch-lint?tab=versions), [OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/fe3dback/go-arch-lint), [PR 91](https://github.com/fe3dback/go-arch-lint/pull/91).

# What it checks

- It reads one Go module: `--project-path` must hold a `go.mod`, whose `module` line is the
  project's import prefix. A `go.work` is not read. At a folder with only a `go.work`, it fails
  with "not found project 'go.mod'". An import of another workspace module counts as a
  **vendor** import.
- It walks every `.go` file below `workdir`, `_test.go` files and files behind build constraints
  included. It reads only the import block (`go/parser`, `ImportsOnly`). It does not descend into
  `exclude` folders, and it skips files whose path matches an `excludeFiles` regular expression.
  It does not stop at a nested `go.mod`, so the fixture modules under `testdata/` are scanned
  unless excluded.
- Each import is classed as one of three kinds:
  - standard library: a known std package, from `go list std` (run through `golang.org/x/tools/go/packages`
    in `os.TempDir()`) plus a built-in list, used alone if `go` cannot run. Std imports are
    always allowed.
  - project: the module path or below it.
  - vendor: anything else.
- Each file belongs to the component whose `in` folder is exactly the file's folder. A file that
  matches several components goes to the one with the fewest files. A file that matches none is
  a "not matched" warning, which fails the check.
- A project import is allowed only into a package of a component in `mayDependOn` or
  `commonComponents`. A component's own package is not implied, so an external `_test` package
  that imports its own package needs the component in its own `mayDependOn` (issue 59).
- A vendor import is allowed only when it matches a `canUse` vendor or a `commonVendors` entry,
  or when `anyVendorDeps` or `allow.depOnAnyVendor` is set.
- Deep scan (`allow.deepScan`, on by default in version 3) also loads packages with types,
  through `go/packages`, to find forbidden dependency injection through method arguments.

Sources: [repository at v1.19.1](https://github.com/fe3dback/go-arch-lint/tree/a34dbfc83fcf68b5b76aa6b4d7e959244f87f739) (`internal/services/project/info`, `scanner`, `holder`, `checker/checker_imports.go`), [issue 59](https://github.com/fe3dback/go-arch-lint/issues/59).

# Archfile, schema version 3

`.go-arch-lint.yml` at the project path, or `--arch-file <path>`. `--arch-file` also takes a
URL, which the tool downloads with `http.Get`, so a local path is the only offline form.

| Key | Meaning |
|-----|---------|
| `version` | required, `3` (the tool reads 1 to 3) |
| `workdir` | folder to analyse, relative to the project path |
| `allow.depOnAnyVendor` | every file may import any vendor |
| `allow.deepScan` | the type-based scan; default `true` |
| `allow.ignoreNotFoundComponents` | a component whose `in` matches no folder is not an error |
| `exclude` | folders, relative, glob allowed, left out |
| `excludeFiles` | regular expressions on file paths, left out |
| `components.<name>.in` | one or more folders, with `*` and `**` globs, resolved to the folders that exist when the archfile is read |
| `vendors.<name>.in` | one or more import paths, with globs; `a/**` does not match `a` itself |
| `commonComponents`, `commonVendors` | allowed in every component |
| `deps.<name>.mayDependOn` | components it may import |
| `deps.<name>.canUse` | vendors it may import |
| `deps.<name>.anyProjectDeps`, `anyVendorDeps` | any project or any vendor import |
| `deps.<name>.deepScan` | overrides `allow.deepScan` for the component |

The schema has no `checks` key. A `deps` entry must name `mayDependOn`, `canUse` or one of the
`any…` flags. A component without a `deps` entry may import only the standard library.

Sources: [archfile syntax](https://github.com/fe3dback/go-arch-lint/blob/v1.19.1/docs/syntax/README.md), [repository at v1.19.1](https://github.com/fe3dback/go-arch-lint/tree/a34dbfc83fcf68b5b76aa6b4d7e959244f87f739) (`internal/services/schema/v3.json`, `spec/validator`).

# Commands, output and exit codes

- Commands: `check`, `graph`, `mapping`, `schema`, `self-inspect` and `version`. `check` takes
  `--project-path`, `--arch-file` and `--max-warnings`, which defaults to 100 in v1.19.1 (the
  README says 512).
- Output is `--output-type ascii|json`, with `--json` as a shorthand, plus
  `--output-json-one-line` and `--output-color`. The JSON for `check` is
  `{"Type": "models.Check", "Payload": {...}}`. The payload holds `ArchHasWarnings`,
  `ArchWarningsDeps` (component, file, import and line), `ArchWarningsNotMatched` (file),
  `ArchWarningsDeepScan`, `OmittedCount`, `ModuleName` and `Qualities`.
- Exit 0 means no warnings and no notices about the archfile. Exit 1 covers everything else:
  forbidden imports, files not matched, archfile notices, and system errors such as a missing
  `go.mod` or archfile. A system error is written to stderr as text, not as JSON. So the exit
  code alone does not separate a violation from a broken setup.
- A `go install` build prints `version` as v1.19.1, with build time and commit "unknown".

Sources: [repository at v1.19.1](https://github.com/fe3dback/go-arch-lint/tree/a34dbfc83fcf68b5b76aa6b4d7e959244f87f739) (`main.go`, `internal/app/cli.go`, `internal/operations/check`); [Feature 001 research, D40](../../specs/001-prose-to-accepted-slice/research.md) (experiment).

# Building and running offline

Checked on 2026-11-15 in the pinned `golang:1.26-alpine` image (Go 1.26.11) on this host:

- `go install github.com/fe3dback/go-arch-lint@v1.19.1` with network built a 47 MB binary in about
  35 s. It filled about 250 MB of module cache and 265 MB of build cache.
- Again with `--network none` and `GOPROXY=off`, it **fails**: "loading deprecation for
  github.com/fe3dback/go-arch-lint: module lookup disabled by GOPROXY=off". `go install pkg@version`
  asks the proxy for the module's deprecation even with every module cached.
- With `--network none` and `GOPROXY=file://$GOMODCACHE/cache/download` (the module cache's
  download folder served as a proxy, as the GOPROXY protocol allows), the same install succeeds
  offline in under 2 s.
- Running `check` needs no network and no write access to the project (it ran with `src/`
  read-only), and needs `go` on `PATH` for the std list. `GOPROXY=off` and `GOFLAGS=-mod=readonly`
  do not affect it. With deep scan off it took about 1 to 2 s per module of this repository. With
  deep scan on, the engine module took about 9 s.

Sources: [Go modules reference, GOPROXY protocol](https://go.dev/ref/mod#goproxy-protocol); [Feature 001 research, D40](../../specs/001-prose-to-accepted-slice/research.md) (experiment).
