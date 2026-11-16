---
type: Component
title: kin-openapi
description: "The Go OpenAPI 3 parser that oasdiff uses as a library, and which the engine imports for contract loading."
tags: [components, contracts, openapi, go]
status: draft
resource: https://github.com/getkin/kin-openapi
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research
  - id: kin-repo
    resource: https://github.com/getkin/kin-openapi
    title: getkin/kin-openapi repository
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-13T12:00:00Z }
---

# Responsibility

Parses the contracts the engine compares. It is a direct requirement of `src/engine` as well as
oasdiff's dependency ([oasdiff](/components/oasdiff.md)). Licence MIT (as the oasdiff concept records);
no Scorecard is recorded in research D22.

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [repository](https://github.com/getkin/kin-openapi).

# Interfaces

Used through `src/engine/contracts`, with external references disabled and a reader that refuses every
document, so no file or URL is read.

# Dependencies

`gjson`, `sjson`, `jsondiff` and others via oasdiff; see the oasdiff concept.

# Configuration

`github.com/getkin/kin-openapi v0.153.0` in `src/engine/go.mod`.

# Operations

Documents above 4 MiB or not OpenAPI 3 are errors, never empty results.
