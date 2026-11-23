# Data model: Code that hard-codes a declared value is rejected

Phase 1 of the [plan](plan.md). No store, record or event changes: the new values live in the
generated project and in check results, whose shapes stay as they are.

## Declared value

What the approved contract states once ([contract](contracts/declared-values.md)).

| Field | Meaning | Rule |
|-------|---------|------|
| `pointer` | JSON Pointer of the keyword in the contract | unique per value; values are kept in document order |
| `keyword` | `maxLength`, ..., `default`, `const`, or `enum` | one of the listed keywords |
| `kind` | `number`, `string`, `bool`, `null` or `unjudged` | by the YAML node's resolved tag; a number no Go constant reads is `unjudged` |
| `text` | the scalar as written | |
| `value` | the Go constant value | numbers and strings only; only these are judged |
| `name` | the Go constant in `gen/declared.go` | numbers and strings, not enum items; derived from `pointer`, unique within `gen/` (contract) |

Owner: `engine/checks` (`DeclaredValues`, `RenderDeclared`), used by the generate procedure,
DeclaredValuesGenerated and DeclaredValuesNotRepeated, so they cannot disagree.

## Deployment value

| Field | Meaning |
|-------|---------|
| `variable` | the environment variable (`LISTEN_ADDRESS`) |
| `default` | the value used when it is unset or empty (`:8080`) |
| `reader` | the function in `gen/config.go` (`ListenAddress`) |

Declared once, in the template's `gen/config.go`, whose doc comment is the delivered project's
documentation of its configuration (FR-005).

## Hard-coded value (a failing check result)

Each is one `CheckResult` with `passed: false` and `part: project` (feature 001, as every step
check): `check` is DeclaredValuesNotRepeated or DeploymentValuesConfigured, and `reason` names the
file and line, the value (declared values) or the kind (deployment values; never a credential's text), and
where it belongs.
A passing result states what was examined (FR-008).

## Pattern `api-feature-go` 1.1.0

- `version: 1.1.0` (was 1.0.0).
- The generate step's `checks`: `GenerationRepeatable, GeneratedCodeBuilds, DeclaredValuesGenerated,
  DependenciesAllowed, ScopeKept`.
- The code step's `checks`: `..., DependenciesAllowed, DeclaredValuesNotRepeated,
  DeploymentValuesConfigured, AllTestsPass` (the two cheap static checks before the test run).
- The code step's `agent_digest`: the new digest of the changed instruction.
- The template: `gen/config.go` added.

The method identity (feature 006) changes with these files; an accepted code step judged under
the old identity is judged again.
