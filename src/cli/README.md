# src/cli

The terminal client: a client of the engine API that adds no behaviour of its own.

Generated engine API client: `api/gen/`, made by `scripts/generate.sh` from `api/engine.oapi-codegen.yaml`; never edited or committed.

Layout: `cli.go` holds `Run` (arguments, environment and streams in, exit code out), the engine
session and the following of operations; `slices.go` the Story 1 commands (`describe`, `questions`,
`show`, `edit`, `approve`, `slices`, `cancel`, `delete`); `cmd/cli/main.go` the binary, which reads
`ENGINE_URL`, `ENGINE_TOKEN` and `EDITOR` and exits with `Run`'s code. Every engine call goes through
the generated client. The run commands (`build`, `resume`, `replay`, `take-over`, `compare`, `runs`,
`run`, `artifact`) exit 3 with `not available yet: T055`.

`describe` and `show` say the pattern does not fit when a check result with `pattern` true fails (contract.md); the client keeps no list of the pattern's conditions.

`delete` does not yet show the number of stored artifacts: no engine operation reports it.
