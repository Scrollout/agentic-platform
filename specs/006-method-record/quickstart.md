# Quickstart: Results are reused only under the method that judged them

Validation of the [spec](spec.md)'s stories once implemented. The commands judge; nothing here is
run by hand during the feature except the first section (seconds, no Docker).

## The identity (US3, SC-002, FR-010)

```sh
scripts/method-digest.py            # method: <identity> (<n> files, 6 pins) unchanged (committed)
scripts/method-digest.py --check    # exit 0
echo '// x' >> src/engine/checks/shared.go
scripts/method-digest.py --check    # exit 1: the stored and the computed identity differ
git checkout src/engine/checks/shared.go
echo x >> src/engine/README.md
scripts/method-digest.py --check    # exit 0: documentation is not the method
git checkout src/engine/README.md
```

`python3 scripts/testrun.py scripts/tests/test_method_digest.py` proves the same on a fixture
repository, with planted violations (a glob that matches nothing, an unknown pin, an unknown key);
`scripts/tests/test_method_identity_current.py` fails the test command while the committed
identity is stale.

## Reuse under the method (US1, SC-001)

The engine's Go tests (`go test ./machine/... ./durable/...` in `src/engine`, after
`scripts/check.sh --prepare`):

- a result accepted under identity A is reused by a later run under A and not under B;
- a result recorded before feature 006 (key without a method) is not reused;
- a durable run interrupted under A and resumed by an engine with B looks up its remaining steps
  under B, and its events name B;
- every event that carries a `reuse_key` names the method, and its `reuse_key` is the key of its
  own fields;
- the recorded Temporal histories still replay.

## Slice checks under the method (US2)

`go test ./slices/... ./api/...`: a slice stored under A, read under A, shows its stored checks;
read under B (or stored with none), its checks run again before the answer, `checks_method` is
B, a part whose check now fails loses its approval, `Create` does not offer it as approved unless
it still is, a check that could not run leaves it to be checked again, and `startRun` is refused
while a check fails.

## The whole platform

CI's check run of the pushed commit (all jobs green) is the full test command; the step evals do
not change (no prompt, model or agent definition changes), so no eval suite is pending.
