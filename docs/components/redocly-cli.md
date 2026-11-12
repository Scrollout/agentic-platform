---
type: Component
title: Redocly CLI
description: "Bundles the multi-file engine contract into one OpenAPI document and dereferences schemas for code and agent output generation."
tags: [components, contracts, openapi]
status: draft
resource: https://github.com/Redocly/redocly-cli
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research
  - id: redocly-docs
    resource: https://redocly.com/docs/cli/
    title: Redocly CLI documentation
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-13T12:00:00Z }
---

# Responsibility

Bundles contracts only (research D4): `scripts/generate.sh` bundles each service contract into one
document before [oapi-codegen](/components/oapi-codegen.md) runs, and dereferences the schemas that
`src/engine/api/` turns into the analyst's proposal schema. Licence MIT; no OpenSSF Scorecard is
published, so a local run is open (research D22, T063).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [Redocly CLI documentation](https://redocly.com/docs/cli/).

# Interfaces

`scripts/npm-tool.sh redocly bundle <contract> -o <out>`, and `bundle --dereferenced --ext json`.

# Dependencies

The pinned Node image (`node:24.22.1-alpine` in `images.lock`).

# Configuration

`@redocly/cli` 2.72.1 in `scripts/tools/package.json`, with the whole tree and integrity hashes in
`scripts/tools/package-lock.json`, installed with `npm ci` and run without network (Principle XIII).

# Operations

Generated code is not committed; `scripts/generate.sh --check` proves it matches the contract.
