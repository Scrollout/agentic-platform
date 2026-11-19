# Draft: engine API and terminal contract changes for feature 002

Draft for the contract changes (Contract-first). The authoritative copies stay with their
providers: the engine contract `src/engine/api/preview/2026-11-12-preview/openapi.yaml` and the
terminal contract `src/cli/contract.md`. Every change below is additive (Principle XV); no
response, field or exit code is removed or changes meaning.

## Engine API (`openapi.yaml`)

1. `x-limits` gains the retry rule, the one home of its values (research R5):

   ```yaml
   # Retries after the first try of a call that failed retryably, by the engine and its clients.
   retry-attempts: 3
   # The first retry's wait; each further retry doubles it, up to the longest wait.
   retry-first-wait-ms: 1000
   retry-longest-wait-ms: 30000
   # Each wait adds a uniform random part of up to this fraction of itself.
   retry-jitter-fraction: 0.5
   ```

2. Every state-changing operation (`proposeSlice`, the slice edits, `approveSlice`,
   `deleteSlice`, `startRun`, `resumeRun`, `replayRun`, `acceptHandOver`, `cancelOperation`)
   gains a `503` response, problem code `Unavailable`, with `Retry-After`: the engine could not
   record the operation durably, so nothing was accepted and the same request may be sent again
   with the same Idempotency-Key (FR-008, research R6). Queries do not gain it.
3. The info description gains: an operation is followed by its ID while its record is kept,
   until `x-limits.idempotency-key-retention-hours` after it ends; then `getOperation` answers
   404 `NotFound`. Operation IDs are never given twice, across restarts too (FR-011). An
   Idempotency-Key and its first result survive a restart within that window.
4. `resumeRun` is unchanged: a run is `interrupted` only when it waits for the developer (the
   model unavailable on every attempt) or for its step workers; a run a platform stop interrupted
   continues by itself (FR-005), so its `resumeRun` answers `RunNotInterrupted` once it runs.

## Run record (`run-event.schema.json`)

No change. `RunInterrupted` and `RunResumed` carry the stop in `detail` (data model, Run record).

## Terminal contract (`src/cli/contract.md`)

1. "Idempotency and the run ID": the resend waits are `x-limits`'s retry rule (1, 2 and 4 s, each
   with up to half again of random jitter), the same values as today, now read from the engine
   contract.
2. The launcher's `resume` paragraph: after a host reboot `ap build-workers --resume <run>` alone
   lets the run continue (it registers the step workers, and the engine continues the run by
   itself), so no `ap resume` is needed. The exit-3 message becomes `resume: the step workers
   of <slice> for the project <project> are not running; run ap build-workers --resume <run>`
   (additive for a human reader; the exit code is unchanged).
3. "Run states and exit codes": `interrupted` means the run waits for the developer or for its
   step workers; a platform stop alone no longer leaves a run `interrupted`.
