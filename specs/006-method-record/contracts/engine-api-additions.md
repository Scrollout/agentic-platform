# Contract: additions to the engine's API

The authoritative copies are in
[`src/engine/api/preview/2026-11-12-preview/`](../../../src/engine/api/preview/2026-11-12-preview/)
(T001): `run-event.schema.json` gained the optional `method` and `slice.schema.json` the optional
`checks_method`, both a `slice-defs.schema.json#/$defs/hash`; `getSlice`'s description says that
check results produced under another method are run again first. Both are optional properties,
additive within Principle XV, so the preview version is unchanged. `listSlices` returns summaries,
without checks, unchanged.
