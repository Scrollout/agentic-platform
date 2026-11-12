# src/engine

The engine: the state machine, checks, fixed procedures, run store and the engine API (contract under api/preview/).

Generated server interfaces and types: `api/gen/`, made by `scripts/generate.sh` from `api/engine.oapi-codegen.yaml`; never edited or committed.

Rate limits, both from the contract's `x-limits` and both a one-second sliding window: a request with an accepted token counts only against that token (`requests-per-second-per-token`); a request with no token, or one the engine does not accept, counts against its source address (`requests-per-second-per-address-without-token`), the host part of the connection's peer address, never a forwarding header. Such requests get 401 up to that limit and 429 with `Retry-After` beyond it. The limiter drops windows idle for a second or more, so its memory follows the addresses active in the last second, not every address ever seen.

The limits live once, in the contract's root `x-limits`; the engine refuses to start without a positive integer for each of its four keys, and the Spectral rule `ap-limits-declared` in `api/ruleset.yaml` (function `api/ruleset-functions/declaresLimits.js`) fails any service contract that lacks one.

Run store (`store/`): `store.Open(dir, ids)` takes the contract's schemas (`*slice.Schemas`) and parses every slice ID, run ID and artifact hash it is given, and every one it reads back from disk, with them; the patterns are not repeated in Go. Nothing in the engine opens the store yet; when it does (a later task), it will pass the schemas of the contract version it embeds, through `slice.LoadSchemasFS`.
