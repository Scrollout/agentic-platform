# src/check

The check service's contract (`api/preview/2026-11-13-preview/openapi.yaml`, research D29) and the Go module of its generated code. One program, `src/engine/cmd/check` (package `engine/checksvc`), serves it in two deployments: the check service, which reads the project folder and runs the pattern's step checks (runCheck) and returns an agent step's files (readStepFiles), and a procedure step's worker, which runs that step's fixed procedure and writes its files (runProcedure).

A review finding is confirmed only by rerunning the tests of the rule it names against the current code (owner, 2026-11-13): FindingsCarryEvidence and NoBlockingFindingOpen run `go test -json` themselves, and runCheck's description states how a rule's tests are found and how "fails at the named file" is decided. A finding's `check` is informational (the engine's `run-event.schema.json` no longer requires it) and not consulted.

Known limits (research D29): only a path's last part is protected against a symbolic-link race; a child process that calls `setsid` escapes the process group a time limit kills, and the deployment's container (constitution VIII) contains it; and the rerun trusts the project's tests, so tests can fake or avoid a confirmation.

Generated server interfaces, client and types: `api/gen/check.gen.go`, made by `scripts/generate.sh` from `api/check.oapi-codegen.yaml` (Go names from `api/check.overlay.yaml`); never edited or committed. The engine's `machine.Checker` will use the client (T054c).

The contract is linted with the project ruleset (`scripts/check.sh`, `s_contracts`) and its limits live once, in its root `x-limits`; the service reads them from its embedded copy.
