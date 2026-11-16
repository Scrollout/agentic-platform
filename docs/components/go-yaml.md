---
type: Component
title: go-yaml
description: "The YAML library (go.yaml.in/yaml/v3) the slice model and the engine use to read contracts, patterns and rules."
tags: [components, yaml, go]
status: draft
resource: https://github.com/yaml/go-yaml
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research
  - id: goyaml-repo
    resource: https://github.com/yaml/go-yaml
    title: yaml/go-yaml repository
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-13T12:00:00Z }
---

# Responsibility

Reads YAML contracts and `pattern.yaml` (the engine since T018; `src/slice` for contracts). Licence
Apache-2.0; no OpenSSF Scorecard is published, so a local run is open (research D22, T063).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [repository](https://github.com/yaml/go-yaml).

# Interfaces

`go.yaml.in/yaml/v3` in `src/slice` and `src/engine` (`checks`, `slices`, `api`). Duplicate keys and
several documents are refused, and the checks fail closed on them.

# Dependencies

None of its own. oasdiff separately brings `go.yaml.in/yaml/v4` v4.0.0 (indirect).

# Configuration

`v3.0.4` in `src/slice/go.mod`, `v3.0.6` in `src/engine/go.mod`.

# Operations

Keep both modules on one version when next updated.
