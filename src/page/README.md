# src/page

The read-only run page lets a developer read a run's record, and the differences between two runs, in a browser; it calls only the engine API's GET operations, with its own token (research D18).

Settings, from the environment only, all required (the page refuses to start without one):

| Variable | Meaning |
|----------|---------|
| `PAGE_LISTEN_ADDRESS` | Address to listen on, such as `:8080` |
| `PAGE_HOST` | The one Host header the page answers, such as `127.0.0.1:8123`; `localhost` with the same port is accepted too, any other Host gets 403 (DNS rebinding guard) |
| `ENGINE_URL` | The engine's base URL, such as `http://engine:8080` |
| `ENGINE_TOKEN` | The page's own engine token |

Routes, GET only (any other method gets 405):

| Route | Shows |
|-------|-------|
| `/runs/{runId}` | The run's events in order (engine `listRunEvents`, every page up to the page's limit) |
| `/compare?run={a}&with={b}` | How run a differs from run b (engine `compareRuns`) |

Engine errors become a plain error page: 404 for an unknown run, 400 for a refused request, 503 when the engine is busy, 502 otherwise.

Generated engine API client: `api/gen/`, made by `scripts/generate.sh` from `api/engine.oapi-codegen.yaml`; never edited or committed.
