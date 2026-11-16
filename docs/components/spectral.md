---
type: Component
title: Spectral
description: "The OpenAPI linter: run from the pinned Node image on the platform's own contracts, and as a small contract-lint service on each slice's contract."
tags: [components, contracts, openapi]
status: draft
resource: https://github.com/stoplightio/spectral
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research
  - id: spectral-js
    resource: https://github.com/stoplightio/spectral/blob/develop/docs/guides/3-javascript.md
    title: Spectral JavaScript guide
  - id: adr-0004
    resource: /decisions/0004-contract-first-toolchain.md
    title: "ADR 0004: Contract-first toolchain"
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-13T12:00:00Z }
---

# Responsibility

Lints OpenAPI contracts (ADR 0004). Licence Apache-2.0, OpenSSF Scorecard 4.7, a low score, so it is
pinned and watched (research D22).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [ADR 0004](/decisions/0004-contract-first-toolchain.md).

# Interfaces

- `scripts/check.sh` runs `spectral lint` on each platform contract with the project ruleset
  `src/engine/api/ruleset.yaml` (its rule `ap-limits-declared` is a custom function).
- The `contract-lint` service (`src/deploy/lint/server.mjs`, about 30 lines of `node:http`) lints a
  slice's contract with `spectral:oas` and a resolver with no file or network resolvers; a `$ref`
  outside the document is an error. Only the engine reaches it, with a bearer token, a 1 MiB body
  cap and a 30 s limit. The engine's `contracts.NewLint` calls it (research D28).

# Dependencies

The pinned [Node image](/components/redocly-cli.md) and the npm lock file.

# Configuration

`@stoplight/spectral-cli` 6.15.0 in `scripts/tools/package.json`, with its tree and integrity hashes
in `package-lock.json`, run offline by `scripts/npm-tool.sh`. The service runs under
[gVisor](/components/gvisor.md).

Sources: [Spectral JavaScript guide](https://github.com/stoplightio/spectral/blob/develop/docs/guides/3-javascript.md).

# Operations

Slice contracts use `spectral:oas`, not the project ruleset, which encodes the engine API's own
conventions (research D28).
