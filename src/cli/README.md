# src/cli

The terminal client: a client of the engine API that adds no behaviour of its own.

Generated engine API client: `api/gen/`, made by `scripts/generate.sh` from `api/engine.oapi-codegen.yaml`; never edited or committed.

Layout: `cli.go` holds `Run` (arguments, environment and streams in, exit code out), the engine
session and the following of operations; `slices.go` the Story 1 commands (`describe`, `questions`,
`show`, `edit`, `approve`, `slices`, `cancel`, `delete`); `cmd/cli/main.go` the binary, which reads
`ENGINE_URL`, `ENGINE_TOKEN` and `EDITOR` and exits with `Run`'s code. Every engine call goes through
the generated client. `runs.go` holds the run commands (`build`, `resume`, `replay`, `take-over`,
`runs`, `run`, `compare`, `artifact`): each mutating call carries one `Idempotency-Key` per
invocation and is resent unchanged after a lost answer (no reply, 502, 503, 504, 429) up to 3 more
times after 1, 2 and 4 s with jitter; following reads every page of `listRunEvents`, prints one line
per event and an end line, and exits by the run's state; store text is printed with its control
characters escaped, except `artifact`, which writes the raw bytes.

`captured.go` holds the captured-pattern commands (`capture`, `patterns`, `pattern` with `--drop`
and `--add`, `proof-slice`, `approve-pattern`, `withdraw-pattern`, and `erase-pattern`, which erases only after the
captured pattern's ID is typed), each mutating call with its own `Idempotency-Key`,
and the proof outcome line that `build`, `replay`, `resume` and `run` print after a proof run's end
line (`Proof of <id>: passed`, `failed: ...` with exit 1, or `running`).

`trials.go` holds the trial-set commands (feature 012): `trials <slice> <prefix> [--trials <k>]`
starts a set with one `Idempotency-Key` (k from 1 to the engine contract's `x-limits.trials-max`,
read from the embedded contract; without `--trials` the engine's default applies) and follows it as
`trial-set <id>` does: one line per trial as it ends, then the set's first line, its trials' and
steps' tables (`-` for a measure the engine does not give) and the comparison line, and the exit
code of the set's state. `trial-set` of an ended set prints the trial lines too. A `failed` set's
first line ends its state with `failed <failed_because>`, and the client exits 4 with that reason,
read from the set (not its operation).

Signals reach `Run` through `Env.Signals`, which `main` fills with `signal.Notify` for SIGINT and
SIGTERM: the first one stops following, never the work, prints how to follow or cancel it, and
exits 130 or 143.

`describe` and `show` say the pattern does not fit when a check result with `pattern` true fails (contract.md); the client keeps no list of the pattern's conditions.

`delete` does not yet show the number of stored artifacts: no engine operation reports it.
