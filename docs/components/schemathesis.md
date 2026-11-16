---
type: Component
title: Schemathesis
description: "The property-based API tester that checks the running engine against its OpenAPI contract."
tags: [components, testing, openapi]
status: draft
resource: https://github.com/schemathesis/schemathesis
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research
  - id: st-docs
    resource: https://schemathesis.readthedocs.io/
    title: Schemathesis documentation
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-13T12:00:00Z }
---

# Responsibility

Generates requests from the contract and checks the real engine's responses, so the contract and the
service cannot drift (ADR 0004). Licence MIT, OpenSSF Scorecard 6.2 (research D22).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [Schemathesis documentation](https://schemathesis.readthedocs.io/).

# Interfaces

`src/tests/lab/lab.go` runs the container against the lab's engine for the operations under test,
once per caller token, with `--include-operation-id`; `src/tests/contract/slices_test.go` asserts the
exit code and that test cases ran.

# Dependencies

A running engine on an internal Docker network and the contract file.

# Configuration

Image: `schemathesis` pin in `images.lock` (`schemathesis/schemathesis:v4.33.2`, by digest). It runs
read-only as 65532 with no capabilities; a generated `schemathesis.toml` sets the `api-version` parameter.

# Operations

Tests only; not part of the running platform.
