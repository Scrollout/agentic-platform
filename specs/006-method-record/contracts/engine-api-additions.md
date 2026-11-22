# Contract draft: additions to the engine's API

Draft for T001; the authoritative copies are in
[`src/engine/api/preview/2026-11-12-preview/`](../../../src/engine/api/preview/2026-11-12-preview/).
Both additions are optional properties (additive within Principle XV; no new version).

## `run-event.schema.json`

```json
"method": {
  "$ref": "slice-defs.schema.json#/$defs/hash",
  "description": "The method identity the step was judged under, which its reuse_key covers (feature 006). Present on the events that carry a reuse_key (AttemptMade, StepAccepted, ResultReused, and the StepRejected that leaves a review round open) recorded since feature 006 with a successful reuse lookup; absent on earlier events and on every other kind."
}
```

## `slice.schema.json`

```json
"checks_method": {
  "$ref": "slice-defs.schema.json#/$defs/hash",
  "description": "The method identity the slice's checks were produced under. The platform runs a slice's checks again before it shows them when they were produced under another method (feature 006)."
}
```

`getSlice` returns `checks_method` whenever every check of the slice ran under the engine's
method; it is absent while a check could not run (the next read checks again) and on records
written before feature 006. `listSlices` returns summaries, without checks, unchanged.
