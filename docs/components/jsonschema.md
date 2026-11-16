---
type: Component
title: santhosh-tekuri/jsonschema
description: "The Go JSON Schema validator the slice model and the engine use to check parts, proposals and request bodies against the contract's schemas."
tags: [components, validation, go]
status: draft
resource: https://github.com/santhosh-tekuri/jsonschema
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research
  - id: js-repo
    resource: https://github.com/santhosh-tekuri/jsonschema
    title: santhosh-tekuri/jsonschema repository
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-13T12:00:00Z }
---

# Responsibility

Validates JSON documents against the schemas the contract carries, so structure rules are schema
checks, not code (research D2). Licence Apache-2.0; OpenSSF Scorecard 3.2, a low score, kept because it
is small and replaceable (research D22).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [repository](https://github.com/santhosh-tekuri/jsonschema).

# Interfaces

`github.com/santhosh-tekuri/jsonschema/v6`. `src/slice/schema.go` compiles the slice and proposal
schemas and reports faults by part and JSON pointer; `src/engine/api/contract.go` validates request
bodies.

# Dependencies

`golang.org/x/text`.

# Configuration

`v6.0.3` in `src/slice/go.mod` and `src/engine/go.mod`. The Go checksum database verifies the content.

# Operations

Failure reasons name the part and pointer in words and never expose a schema URL or local path.
