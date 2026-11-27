# src/catalogue

The catalogue's approved patterns, as declarations only. Each pattern gives the conditions for a
slice to fit, its steps, each step's writable paths, dependencies and checks, its limits and its
budgets. Every `pattern.yaml` must be valid against the engine's
[`pattern.schema.json`](../engine/api/preview/2026-11-12-preview/pattern.schema.json).

This version holds one pattern:

| Pattern | What it builds |
|---------|----------------|
| [`api-feature-go`](api-feature-go/README.md) | one use case of one service as a Go HTTP API |

## Captured patterns

Beside these base patterns, the engine keeps captured patterns (feature 011): a run of a base
pattern that ended done, captured with `ap capture <run>`, becomes a pattern that builds later
slices of the same kind with no model call. A captured pattern is not a file here. It is the
developer's data in the engine store, behind the engine API's `/captured-patterns`, and its
declaration is derived from its base pattern's `pattern.yaml`: each agent step is filled from the
source run's accepted files by the fixed procedure `fill`, and the review step drops the
checks that judge findings (FindingsCarryEvidence, NoBlockingFindingOpen) and adds AllTestsPass, so
its witness tests run against the filled code ([ADR 0101](../../docs/decisions/0101-filled-review-step-runs-every-test.md)). Every other check of the base pattern judges a filled step as it
judges an agent's. A later slice fits it only when it equals the source slice except for the
captured pattern's parameter values; the developer proves it with `ap prove` and approves it before
any run uses it. The rules are in the
[architecture overview](../../docs/architecture/overview.md) ("Shipped by feature 011") and the
engine contract.
