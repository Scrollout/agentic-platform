# Data model: The delivered service refuses requests its contract does not allow

Phase 1 of the [plan](plan.md). No store, record or event of the platform changes: the new
values live in the generated project and in check results, whose shapes stay as they are.

## Request shape

What the approved contract states a request body may hold (glossary: Request schema), derived
once per operation with a request body ([contract](contracts/strict-requests.md)).

| Field | Meaning | Rule |
|-------|---------|------|
| `key` | the operation: method in upper case, a space, the path as the contract writes it (`POST /notes`) | unique per operation |
| `required` | whether the contract's `requestBody.required` is true | an empty body is refused only then |
| `types` | the JSON types the value may have, from `type` (a name or a list) | empty means any type; `integer` accepts a number with a zero fractional part |
| `properties` | the declared members: exact name to shape, in contract order, then names only in `required`, in `required` order, each of any type | absent when the schema has neither `properties` nor `required` nor `additionalProperties: false`; then the object is kept as it is; `properties: {}` is present and empty; applies to an object value whether or not `type` is present |
| `requiredMembers` | names from `required`, in contract order | each must be present |
| `closed` | `additionalProperties: false` | an undeclared member is refused; otherwise it is removed |
| `items` | the shape of each array item | absent means any item |

Owner: `engine/checks` (`RequestShapes`, `RenderRequestShapes`), used by the generate procedure
and RequestShapesGenerated, so they cannot disagree. A `$ref` target is one shape, shared by every
use (a recursive schema is a cycle).

## Refusal

The 400 answer to a body the shape does not allow (glossary: Refusal).

| Field | Meaning | Rule |
|-------|---------|------|
| status | `400` | always |
| `Content-Type` | `application/json` | always |
| body | `{"message": <text>}` | `<text>` is one of the messages in the [contract](contracts/strict-requests.md#messages); the first problem found: the size, emptiness and JSON checks, then the duplicate-name pre-pass, then the walk, in the order stated there |

Nothing of the operation runs, so nothing is changed (FR-002).

## Fit conditions (added)

| Condition | Part | Passes when |
|-----------|------|-------------|
| `RefusalDeclared` | `contract` | every operation with a `requestBody` declares `400` with an `application/json` schema that is an object requiring exactly `message`, a string with no value keyword |
| `RequestBodiesSupported` | `contract` | every `requestBody` is `application/json` only with a schema, and its schema shapes the body only with the simple keywords and the seven type names, a list only as one type and `"null"`; no `nullable` |

## Check (added)

| Check | Step | Passes when |
|-------|------|-------------|
| `RequestShapesGenerated` | generate | the project's `gen/request_shapes.go` equals `RenderRequestShapes` of the approved contract, byte for byte |
