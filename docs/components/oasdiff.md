---
type: Component
title: oasdiff
description: The OpenAPI breaking-change checker, run as a pinned container by scripts/contract-diff.sh for the platform's own contracts and as a Go library in the engine for each slice's contract.
tags: [components, contracts, openapi]
status: draft
resource: https://github.com/oasdiff/oasdiff
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, S6 and D4
  - id: adr-0004
    resource: /decisions/0004-contract-first-toolchain.md
    title: "ADR 0004: Contract-first toolchain"
  - id: oasdiff-repo
    resource: https://github.com/oasdiff/oasdiff/tree/v1.37.0
    title: oasdiff v1.37.0 source
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T16:00:00Z }
---

# Responsibility

Decides whether a contract revision breaks its baseline (Principle XV). It runs all of its
backward-compatibility checks with their default severity levels; an error-level finding is a
breaking change. Its rules, finding IDs and texts are used as they are, never rewritten
(Principle VI). It is licensed Apache-2.0, at v1.37.0 (research S6, D4; ADR 0004).

Sources: [Feature 001 research, S6 and D4](../../specs/001-prose-to-accepted-slice/research.md),
[ADR 0004](/decisions/0004-contract-first-toolchain.md).

# Interfaces

- **Container**: `scripts/contract-diff.sh` runs `oasdiff breaking <previous> <newest> --fail-on
  ERR` for each service's two newest contract versions, offline and read-only.
- **Go library**: `src/engine/contracts` implements `slices.ContractDiff` with the packages
  `load`, `diff` and `checker`, judging as `oasdiff breaking` does: all checks, default levels,
  warnings and errors reported. A slice's first contract is compared with an empty document. Each
  finding carries oasdiff's level, check ID and English text, prefixed with the operation and path.

Sources: [oasdiff v1.37.0 source](https://github.com/oasdiff/oasdiff/tree/v1.37.0).

# Dependencies

As a library it brings [kin-openapi](https://github.com/getkin/kin-openapi) (MIT), which parses
the contracts, and a few small modules ([gjson](https://github.com/tidwall/gjson) and
[sjson](https://github.com/tidwall/sjson), [jsondiff](https://github.com/wI2L/jsondiff),
[yaml/v4](https://github.com/yaml/go-yaml), [jsonpointer](https://github.com/go-openapi/jsonpointer),
[cloud.google.com/go](https://github.com/googleapis/google-cloud-go) civil), all MIT, BSD-style or
Apache-2.0.

# Configuration

- Container: the `oasdiff` pin in `src/deploy/images.lock` (`tufin/oasdiff:v1.37.0`, by digest).
- Library: `github.com/oasdiff/oasdiff v1.37.0` in `src/engine/go.mod`; keep it at the same
  version as the image so both judge alike.
- The engine reads JSON or YAML from memory only: kin-openapi's external references are disabled
  and its reader refuses every document, so no file or URL is ever read; a document above 4 MiB,
  one that does not parse, or one that is not OpenAPI 3 is an error, never an empty result.

# Operations

A diff the engine cannot make fails the slice's `ContractCompatibility` check; a breaking
change in the platform's own contracts fails `scripts/check.sh`.
