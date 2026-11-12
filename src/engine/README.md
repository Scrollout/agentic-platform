# src/engine

The engine: the state machine, checks, fixed procedures, run store and the engine API (contract under api/preview/).

Generated server interfaces and types: `api/gen/`, made by `scripts/generate.sh` from `api/engine.oapi-codegen.yaml`; never edited or committed.

Rate limits, both from the contract's `x-limits` and both a one-second sliding window: a request with an accepted token counts only against that token (`requests-per-second-per-token`); a request with no token, or one the engine does not accept, counts against its source address (`requests-per-second-per-address-without-token`), the host part of the connection's peer address, never a forwarding header. Such requests get 401 up to that limit and 429 with `Retry-After` beyond it. The limiter drops windows idle for a second or more, so its memory follows the addresses active in the last second, not every address ever seen.
