# src/catalogue

The catalogue's approved patterns, as declarations only. Each pattern gives the conditions for a
slice to fit, its steps, each step's writable paths, dependencies and checks, its limits and its
budgets. Every `pattern.yaml` must be valid against the engine's
[`pattern.schema.json`](../engine/api/preview/2026-11-12-preview/pattern.schema.json).

This version holds one pattern:

| Pattern | What it builds |
|---------|----------------|
| [`api-feature-go`](api-feature-go/README.md) | one use case of one service as a Go HTTP API |
