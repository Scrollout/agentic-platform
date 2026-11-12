# api-feature-go

Builds one use case of one service as a Go HTTP API from an approved slice. The declaration is
[`pattern.yaml`](pattern.yaml), valid against the engine's
[`pattern.schema.json`](../../engine/api/preview/2026-11-12-preview/pattern.schema.json). This file
states what its values mean, so the checks can be written from it.

## Fit conditions

A slice fits when all four pass. Each one is a rule on the slice's `contract` (YAML text) and
`rules`. None calls a model. The checks run when the contract is approved and again at `startRun`.
A failing condition is reported as a `check_result` with `check` set to the condition's name,
`part` set as below, `passed: false`, and a `reason` that names what failed. The contract is
parsed once as a single YAML document. Text that does not parse, holds more than one document, or
has a duplicate key fails every contract condition (fail closed).

| Condition | Part | Passes when | Reason on failure names |
|-----------|------|-------------|-------------------------|
| `ContractIsOpenApi31` | `contract` | the root is a mapping and its `openapi` value is a string matching `^3\.1\.[0-9]+$` | the value found, or that it is missing |
| `OnlyHttpOperations` | `contract` | `paths` is a mapping with at least one operation. Each path item has only the keys `get`, `put`, `post`, `delete`, `options`, `head`, `patch`, `trace`, `summary`, `description`, `servers`, `parameters` or `x-*`, and no `$ref`. No operation has a non-empty `callbacks`. `webhooks` and `components.pathItems` are absent or empty | each offending location, for example `webhooks/noteAdded` or `paths//notes/post/callbacks` |
| `ContractRefsLocal` | `contract` | every `$ref` value in the document is a string starting with `#/` | the location of each other `$ref` |
| `RulesNameOneSystem` | `rules` | there is at least one rule, and every rule's `system` is byte-for-byte equal to the first rule's | the distinct names, with the rule IDs that use each |

What the conditions keep out:

- `OnlyHttpOperations` admits only operations a client calls over HTTP. It refuses webhooks and
  callbacks, which the service would send, and refuses keys this version does not know
  (fail closed).
- `ContractRefsLocal` is there because generation runs without network, and a reference outside
  the document cannot be resolved there.
- `RulesNameOneSystem` compares the names exactly, with no trimming or case folding. If the slice
  says "the notes service" in one rule and "notes service" in another, it fails, and the developer
  is asked which name is right. The rule does not compare the name with the contract's
  `info.title`, because nothing links the two deterministically.

## Steps and project layout

Paths are relative to the new project's folder. Each step's container has only its own paths
mounted writable.

| Step | Worker | Writes | Accepted when |
|------|--------|--------|---------------|
| contract-check | procedure | `api/openapi.yaml`, `features/slice.feature` (copies of the approved contract and examples) | both hashes equal the approved hashes, Spectral passes, nothing else changed |
| generate | procedure | `gen/**` (oapi-codegen output and the stub), `go.mod`, `go.sum`, `vendor/**` | a second run gives identical files, the code builds, dependencies are allowed, nothing else changed |
| tests | agent | `tests/**` (godog step definitions) | the tests build, godog `--strict` binds every example, the tests fail against the stub, dependencies are allowed, nothing else changed |
| code | agent | `internal/**`, `cmd/**` | all tests pass, and tests, generated code and contract are unchanged, dependencies are allowed, nothing else changed |
| review | agent | nothing | every blocking finding carries evidence a check confirms, and none is open |

`inputs` and `outputs` name artifacts. `slice/<part>` is an approved part of the slice. Any other
name is the accepted output of the earlier step that lists it under `outputs`.

## Dependencies

Each entry is a Go package pattern: `std` (the standard library), an import path, or a path
ending in `/...` (that path and everything below it), as defined by the
[go command](https://pkg.go.dev/cmd/go#hdr-Package_lists_and_patterns). The check matches every
package the step's code imports from outside the project's own module, directly or not, as
`go list -deps` reports it. An import must match an `allowed` entry and no `forbidden` entry.
`forbidden` wins over `allowed`, which is how `os/exec` and `syscall` stay out even though `std`
is allowed.

## Budgets

Each budget is reached by whichever of tokens, cost or minutes comes first, and the run is then
handed over. The step budget applies to one attempt, or to one review round. The numbers are
explained in `pattern.yaml`: they come from Claude Haiku's price and the ADR 0026 spike's measured
run.

## Agent digests

The `tests`, `code` and `review` steps carry an all-zero `agent_digest` until T049 writes their
agent definitions in `src/agents/`. No definition hashes to all zeros, so a run fails closed
until then.
