# Data model: The platform refuses a revision that loses the slice's items

Phase 1 of the [plan](plan.md). No stored record, schema or event changes; these are the values
the slices service reads and the faults it gives.

## Analyst answer

The structured answer text an analyst client receives (`agentcall` answer content, or the evals'
extracted JSON).

- Parsed only by `slices.ParseProposal` into a `Proposal` (research R1).
- Refused, as an `AnswerFault`, when an object at any depth holds a key twice, whatever the
  values, keys compared without regard to letter case (`strings.EqualFold`: Go's decoder matches
  struct fields so). The fault names the key and the JSON Pointer of the object (`/` for the top
  level, `/rules/1` for the second rule); both are model text, so each is quoted with Go's `%q`
  (no raw line break reaches the message) and cut to 64 characters.
- Not JSON, not one object, or trailing data: an error as today, not an `AnswerFault`.
- A repeat inside a string value (Gherkin or YAML text) is not a repeated key.

## Kept IDs

Read from the slice's current proposal (`AnalystInput.Current`) when the analyst revises
(research R2).

| Item | Read from | Kept when the revision has |
|------|-----------|----------------------------|
| Goal | `goals[].id` | the same ID |
| Term | `terms[].id` | the same ID |
| Rule | `rules[].id` with `rules[].goal` | the same ID serving the same goal |
| Example | `@E-<n>` tag of a scenario under a `Rule: R-<n>` block of `examples` | the same `@E-<n>` under the same rule |
| Operation | `operationId` of each operation in `contract` | the same `operationId` |

- A kept rule whose goal is not among the current proposal's goals keeps its ID; its goal is not
  checked.
- When the current `examples` or `contract` does not parse, its IDs are not known and not
  checked.
- New IDs are allowed.

## Faults (sent to the analyst, and in `OperationFailed` after the last attempt)

| Cause | Fault text (shape) |
|-------|--------------------|
| Repeated key | `the answer holds the key "<key>" twice at <pointer>` |
| Lost item | `the revision drops <ID> of the current proposal` |
| Re-homed rule | `<R-n> served <G-a> and now serves <G-b>` |
| Re-homed example | `<E-n> was under <R-a> and is now under <R-b>` |

Each fault names keys and IDs only (FR-010). A refused answer is sent back with no `Previous`;
the message then reads "Previous proposal: null" followed by the fault, as `AnalystMessage`
already does and `TestAnalystMessageRetryWithErrorsOnlyPinsNullPrevious` pins (no message
change). A refused revision is sent back with its proposal as `Previous`, as any unusable
proposal.

## State

A refused answer, after the last attempt, changes nothing: the slice's parts, approvals,
questions (the developer's answers are not recorded) and history stay as they were.
