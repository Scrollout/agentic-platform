# Recorded model (T020)

Tests run against a recorded model, not the provider (research D19): a replay server stands in for
the [model gateway](../../deploy/gateway/README.md) and answers from the recordings here. It needs
no key, costs nothing and has no egress. It is [WireMock](../../../docs/components/wiremock.md)
from its pin in `../../deploy/images.lock`, configured only by its own stub-mapping files
(research D26).

## The replay server

```sh
sh src/tests/recorded/replay.sh start <network> [<name>]   # prints the container name
sh src/tests/recorded/replay.sh misses <name>              # requests no recording matched (JSON)
sh src/tests/recorded/replay.sh stop <name>
```

- It joins `<network>` under the alias `gateway`, so an agent uses it exactly as it uses the
  gateway: `MODEL_GATEWAY_URL=http://gateway:8080`, with any placeholder key.
- `<network>` must be an internal Docker network; `start` refuses any other, and refuses a
  recording that would proxy instead of answering. Nothing is ever passed through to the provider.
- It serves `POST /v1/messages` and `POST /v1/messages/count_tokens` (the gateway's two paths) only
  where a recording matches. A request matches when its method is equal, its path and query equal
  the recording's `url` as one string (an extra or missing query parameter misses; `start` refuses
  a recording that matches by path or pattern), and its body is equal as JSON (WireMock
  `equalToJson`, nothing ignored); no header is matched. Docker
  Agent's requests are deterministic: the same agent definition and input give a byte-identical
  body (checked 2026-11-12 with the analyst, `add_date: false`).
- Any other request gets **404** with WireMock's message and the closest recording's differences,
  and the agent fails (`docker-agent run` exits 1 with `HTTP 404`). `misses` lists such requests
  with their bodies. A changed prompt, schema, model setting or Docker Agent version changes the
  body, so every recording of that agent misses until it is recorded again; a stale answer is
  never served.
- The admin API (`/__admin`) needs a random per-container credential that lives only in that
  container's environment, so the agent under test cannot add or change recordings.
- Hardening as the gateway: read-only root, user 65532, all capabilities dropped,
  `no-new-privileges`, 512 MB memory and 256 processes, the recordings mounted read-only.

## Recordings

`recordings/mappings/<name>.json` is one exchange: the request matcher and the response status and
`Content-Type`. `recordings/__files/<name>.sse` is the response body (an Anthropic Messages event
stream). Do not edit either by hand; record or synthesise again.

| Recording | Agent and input | Answer | Origin |
|-----------|-----------------|--------|--------|
| `analyst-example-notes` | The analyst (`src/agents/analyst.yaml`) with the quickstart example description (`Example` in `../evals/analyst/cases.go`) | One call of the structured-output tool with `../evals/analyst/testdata/proposals/good-notes.json` | Synthesised, no model call (below) |

`analyst-example-notes` was not recorded from the provider. Its request is the one the analyst sent
to an empty replay server (taken from `misses`), and its answer was built by
`go run ./recorded/recording synth` (from `src/tests`): an event stream in which the model calls
`__structured_output__` once with the proposal as its arguments. The analyst accepts it and prints
the proposal exactly (`smoke.sh`). It shows the platform's handling of a good proposal, not the
model's behaviour; the analyst's live evals judge the model. To build it again:

```sh
cd src/tests
go run ./recorded/recording synth -request <request-body.json> \
  -proposal evals/analyst/testdata/proposals/good-notes.json \
  | go run ./recorded/recording write -dir recorded/recordings -name analyst-example-notes
```

## Recording from the provider (owner only, costs money)

Only the project owner records, by hand; no check, hook or test runs `record.sh`. It uses the
test-scoped key (`~/.config/agentic-platform/anthropic_api_key`, override `EVAL_KEY_FILE`), which
only the gateway reads, and Claude Haiku, as the agent definition pins it (D19).

```sh
sh src/tests/recorded/record.sh start          # prints the network to use
docker run --rm -i --network <printed network> --read-only --user 65532:65532 --cap-drop ALL \
  --security-opt no-new-privileges --tmpfs /tmp:uid=65532,gid=65532 \
  -e HOME=/tmp/home -e TELEMETRY_ENABLED=false -e MODEL_GATEWAY_URL=http://recorder:8080 \
  -e ANTHROPIC_API_KEY=placeholder-not-a-key -v "$PWD/src/agents:/agents:ro" \
  "$(awk '$1=="docker-agent"{print $2}' src/deploy/images.lock)" \
  run --exec --last --safety restricted /agents/analyst.yaml - < description.txt
sh src/tests/recorded/record.sh stop <name>     # writes recordings/*/<name>.*, removes the lab
```

`start` makes a throwaway internal network with the real gateway (as in the analyst evals) and a
WireMock recorder, alias `recorder`, that forwards to it and records each request body for exact
matching and no request header. WireMock's recorder writes the path and each query parameter's
exact values; `recording write` turns them into the one exact `url`. `stop` writes every exchange of the session (`<name>`, `<name>-2`,
...) through `recording write`, which keeps only the response's `Content-Type` (and a `fixedDelayMilliseconds`), so provider request
and organisation ids never reach the repository. If writing fails, `stop` keeps the exchanges in a file in the recorder's state folder and leaves
the lab running; run `stop` again after fixing the cause. Review the files before committing them. Every
model request of the session is recorded, including retries after a refused answer.

## Step recording sets and the generator (T054k)

The step agents' recordings are never recorded from the provider: a live session's later requests
carry `go test` output with elapsed times, so they would never match exactly (research D34,
[ADR 0038](../../../docs/decisions/0038-lab-runs-the-build-path-with-synthesised-step-recordings.md)).
They are synthesised, free, from reviewed **answer scripts**. A recording set is a folder
`steps/<set>/` (`steps/happy` for `TestUS2`, `steps/resume` for US2.10, the happy answers with the
code step's first turn held 30 s, and `steps/planted/<scenario>` for `TestPlanted` and US2.7) holding:

- `answers.json`, the answer script (the reviewed source), and its content files (`files/...`);
  the correct answers start from `src/tests/evals/steps/testdata/ref` and are adapted to the lab's
  example slice (in `happy`: a local `Note` type, `TrimSpace`, and the slice's step phrases);
- `mappings/<name>.json` and `__files/<name>.sse`, the exchanges the generator made from it, named
  `<run>-<step>-a<attempt>[-r<round>]-t<turn>` (lowercase). Do not edit them by hand.

```json
{"answers": [
  {"step": "code", "attempt": 1, "turns": [
    {"delayMs": 30000, "write": [{"path": "internal/notes/notes.go", "content": "files/notes.go.txt"}]},
    {"text": "Done."}]},
  {"step": "review", "round": 1, "turns": [{"output": {"findings": []}}]}
]}
```

- An answer is for one `step` and review `round` (absent or 0: none), and for one `attempt` or,
  with `attempt` absent or 0, any attempt; an answer for the attempt wins. Turn k is `turns[k-1]`,
  counted by the assistant messages in the request.
- A turn is exactly one of: `write` (one `write_file` call per file, the path the project folder
  the engine's message names plus the relative `path`, the content file's exact bytes, UTF-8),
  `output` (the structured-output answer, any JSON) or `text` (the final text). `delayMs` (0 to
  60000) holds the answer; it is kept in the mapping as WireMock's `fixedDelayMilliseconds`.
- The step sets write one file per turn: after a turn with two `write_file` calls, Docker Agent's
  next request did not list the tool results in the same order from run to run (seen in T054l),
  so its recording missed.
- Unknown fields, two answers for the same step, attempt and round, an absolute or `..` path and
  a missing content file are refused when the script loads.

The scripted server `scripted` (`go run ./recorded/scripted -set <dir> -out <dir> [-listen :8080]`
from `src/tests`) serves `POST /v1/messages` from the script, answers anything else with 404 and
lists it at `GET /__scripted/misses`, and writes each answered exchange into `-out` as a snapshot
that `recording write` accepts. It never calls a model or the network.

The lab chooses the step workers' model with `AP_LAB_MODEL`: unset or `replay` is the replay
server, which then also serves the set the test names with `lab.Steps(<set>)`; `scripted` runs
the scripted server for that set as a lab container at `l.StepModel()` (the analyst keeps the
replay server), counts its misses in `l.Misses`, and when the lab ends writes every exchange it
answered into the set with `recording write`. To make a suite's recordings again:

```sh
rm -rf src/tests/recorded/steps/<set>/mappings src/tests/recorded/steps/<set>/__files   # when its answers changed
sh src/tests/recorded/synth-steps.sh '<test pattern>'    # e.g. '^TestPlanted$'; Docker, no key
```

`synth-steps.sh` builds the acceptance suite in the pinned Go container and runs the tests matching
the pattern with `AP_LAB_MODEL=scripted`. Review the new mappings with their scripts before
committing them; the acceptance run itself always replays them through WireMock with exact
matching and ends with no miss.

## Suite times (T054l, measured 2026-11-15)

On the development host, images already built (the lab's image builds took 3 s; a cold
`aptest-check:lab` build takes about 80 s), one suite at a time:

| Run | Wall time | Notes |
|-----|-----------|-------|
| `src/deploy/ap test planted` (replay) | 20 min 16 s | test binary build, 5 labs, 15 trials, teardown; 5 of 5 scenarios stopped in 3 of 3 trials, no miss |
| `src/deploy/ap test us2` (replay) | 9 min 16 s | build, 5 labs; 5 of 5 tests passed, no miss (an earlier run, 7 min 20 s, failed `TestUS2` on two test checks since corrected) |
| `synth-steps.sh '^TestPlanted$'` | 19 min 23 s | per scenario 2.7 to 4.5 min (lab start and slice approval about 15 s, first trial 0.9 to 1.3 min, later trials 0.6 to 2.2 min) |
| `synth-steps.sh '^(TestUS2\|TestUS2_7_.*\|TestUS2_10_.*)$'` | 8 min 20 s | `TestUS2` 3.9 min, US2.7 1.5 min, US2.10 3.0 min |

`ap test` prints no per-test durations, so the replay runs are timed as a whole; setup and
teardown are inside each test's time (a lab starts and approves its slice in about 15 s and is removed in a few
seconds).

## Checks

`sh src/tests/recorded/smoke.sh` (Docker and python3, no key) starts the replay server and shows
that the analyst gets the recorded proposal for the example, that an unrecorded description fails
with 404 and is listed by `misses`, that `count_tokens` without a recording gets 404, that the
admin API refuses a request without the credential, and that `start` refuses a network with egress.
It is a smoke check. The test suite is `replay_test.go` (Docker, no key; behind the `docker` build
tag, which `scripts/check.sh` sets), `recording/main_test.go`, `recording/delay_test.go` and
`scripted/scripted_test.go` (no Docker). Run both with
`cd src/tests && go test -tags docker ./recorded/...`.
