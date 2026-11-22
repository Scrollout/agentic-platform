# Data Model: Results are reused only under the method that judged them

Additions to [feature 001's data model](../001-prose-to-accepted-slice/data-model.md); the schemas
live with the engine ([contract draft](contracts/engine-api-additions.md)).

## Method identity

| Field | Meaning | Rules |
|-------|---------|-------|
| identity | SHA-256 of the method's manifest ([declaration contract](contracts/method-declaration.md)) | 64 lowercase hex digits; derived, never set by hand; the engine does not start without a valid one |

One per engine build. Two builds of the same source have the same identity (research E1).

## Reuse key (changed)

The hash of pattern version, step, input hashes, agent definition digest, model id, for a review
round its round, and now the **method identity** (research R3). A key recorded before feature 006
covers no method, so no key the engine computes now equals it: those results are not reused.

## Run event (changed)

| Field | On | Meaning |
|-------|----|---------|
| `method` | `AttemptMade`, `StepAccepted`, `StepRejected`, `ResultReused` | the identity the step's `reuse_key` was computed with |

A run resumed after an upgrade records its later steps under the new identity; its earlier events
keep theirs. A reader recomputes `reuse_key` from the event's `pattern`, `step`, `inputs`,
`agent_digest`, `model`, `round` and `method`.

## Slice (changed)

| Field | Meaning | Rules |
|-------|---------|-------|
| `checks_method` | the identity `checks[]` were produced under | set whenever the checks run; a record without it (written before feature 006) counts as another method |

**Re-check under a new method**: when the slice service reaches a slice (any operation, or the
list) whose `checks_method` is not the engine's identity, it runs every check of the slice again
(part checks, contract compatibility, contract lint), withdraws the approval of any part whose
check now fails (recorded as `withdrawn`, as after any check), sets `checks_method` and stores the
record, before it answers. The slice's content is unchanged. A check that could not run is a
failed check with its reason, so the slice cannot be built while it fails.
