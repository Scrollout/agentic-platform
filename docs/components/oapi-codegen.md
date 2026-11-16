---
type: Component
title: oapi-codegen
description: "Generates Go server interfaces, types and clients from the platform's OpenAPI contracts, with its runtime library imported by the generated code."
tags: [components, contracts, openapi, codegen]
status: draft
resource: https://github.com/oapi-codegen/oapi-codegen
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research
  - id: oapi-readme
    resource: https://github.com/oapi-codegen/oapi-codegen
    title: oapi-codegen repository
  - id: adr-0004
    resource: /decisions/0004-contract-first-toolchain.md
    title: "ADR 0004: Contract-first toolchain"
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-13T12:00:00Z }
---

# Responsibility

Turns the bundled contract into the engine's strict `net/http` server and the client the terminal
client and run page use (research D4). Licence Apache-2.0; OpenSSF Scorecard 7.5 for the tool and 7.2
for the runtime library (research D22).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [oapi-codegen repository](https://github.com/oapi-codegen/oapi-codegen).

# Interfaces

`scripts/generate.sh` reads each `src/<module>/api/<service>.oapi-codegen.yaml` and writes
`src/<module>/api/gen/<service>.gen.go`. An OpenAPI Overlay (`src/engine/api/engine.overlay.yaml`) names
the few bundled schemas whose generated names would collide.

# Dependencies

[Redocly CLI](/components/redocly-cli.md) for bundling. The generated code imports
`github.com/oapi-codegen/runtime`, which brings `go-jsonmerge` and `google/uuid` (BSD-3-Clause, research D22).

# Configuration

- Tool: `oapi-codegen v2.8.2` in `images.lock`, built in the pinned Go image.
- Library: `github.com/oapi-codegen/runtime v1.8.0` in `src/engine/go.mod`, `src/cli/go.mod` and
  `src/page/go.mod`.

# Operations

Generated code is never committed or edited; `generate.sh --check` fails on drift.
