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
...) through `recording write`, which keeps only the response's `Content-Type`, so provider request
and organisation ids never reach the repository. If writing fails, `stop` keeps the exchanges in a file in the recorder's state folder and leaves
the lab running; run `stop` again after fixing the cause. Review the files before committing them. Every
model request of the session is recorded, including retries after a refused answer.

## Checks

`sh src/tests/recorded/smoke.sh` (Docker and python3, no key) starts the replay server and shows
that the analyst gets the recorded proposal for the example, that an unrecorded description fails
with 404 and is listed by `misses`, that `count_tokens` without a recording gets 404, that the
admin API refuses a request without the credential, and that `start` refuses a network with egress.
It is a smoke check. The test suite is `replay_test.go` (Docker, no key; behind the `docker` build
tag, which `scripts/check.sh` sets) and `recording/main_test.go` (no Docker). Run both with
`cd src/tests && go test -tags docker ./recorded/...`.
