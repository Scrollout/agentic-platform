# src/agents

Docker Agent definitions for the analyst and the step agents. Each one is a Docker Agent config
(schema version 16) for the `docker-agent` pin in [`../deploy/images.lock`](../deploy/images.lock),
and runs only in a container ([ADR 0002](../../docs/decisions/0002-build-on-docker-agent.md)).
Prompts are reviewed like code (constitution XIII), and every change to a definition, prompt,
model or the Docker Agent pin must pass that agent's eval suite (XIX).

| Agent | Definition | Prompt | Evals |
|-------|------------|--------|-------|
| analyst | `analyst.yaml` | `analyst/instruction.md` | `../tests/evals/analyst/` |
| tests | `tests.yaml` | `tests/instruction.md` | `../tests/evals/steps/` |
| code | `code.yaml` | `code/instruction.md` | `../tests/evals/steps/` |
| review | `review.yaml` | `review/instruction.md` | `../tests/evals/steps/` |

## Agent digests

An agent's digest names the exact definition a proposal or a run step used. It is the SHA-256, in
lowercase hex, of the agent's files framed as `canonical.Framed` frames them: `<name>.yaml` and
every file below `<name>/`, each as its path relative to this folder, a line feed, its length in
decimal, a line feed and its bytes, in byte order of path (research D27). No other agent's files
are part of it, so adding or changing one agent leaves the others' digests as they were.

- The analyst's digest is computed by the engine from the definition it embeds, and every
  proposal names it.
- Each agent step's digest is declared in the pattern, `agent_digest` in
  [`../catalogue/api-feature-go/pattern.yaml`](../catalogue/api-feature-go/pattern.yaml), and the
  engine checks it: at start it computes the digest of each agent step's embedded definition and
  refuses to start when the pattern's value differs, naming the step and both values. So a changed
  prompt or definition fails `go test ./cmd/engine/` (and `scripts/check.sh`) until the pattern is
  updated, and the pattern never names a definition the engine does not run (XVII: checked against
  the declaration, not kept by hand unchecked).
- The engine embeds every definition it digests (`src/engine/cmd/engine/setup.go`, copied by
  `scripts/generate.sh`); it also reads each step's pinned model from it (XV).

To get a digest after changing a definition, run the engine's tests: the refusal names the new
value.

## The analyst

The analyst turns a developer's description of one use case into one slice proposal (spec US1,
FR-001 to FR-006). Its answer is one JSON object for
[`slice-proposal.schema.json`](../engine/api/preview/2026-11-12-preview/slice-proposal.schema.json),
which holds none of the fields the platform owns (research D17).

- **Model:** `claude-haiku-5-5`, pinned (XV), reached only through the model gateway at
  `MODEL_GATEWAY_URL` with the Anthropic Messages API (D9). The container holds no key: the
  Anthropic client will not start without a non-empty `ANTHROPIC_API_KEY`, so the container gets a
  placeholder, and the gateway replaces `x-api-key` on every request.
- **Tools:** none. The only tool the model sees is Docker Agent's internal structured-output tool,
  which the runtime handles itself, without approval or hooks.
- **Answer:** `structured_output` in tool mode, bound to the proposal schema (see "Schema
  binding"). The model answers by calling the internal tool. The runtime validates the arguments
  against the schema, returns a refused answer to the model with the reason, and prints the
  accepted object as compact JSON.
- **Budget:** per session 200,000 tokens, $0.10 and 5 minutes of model time, and at most 4 model
  turns; within one step of the pattern (1,000,000 tokens, $0.20, 10 minutes). The model's prices
  are set in the definition, because Docker Agent does not price a model behind a custom base URL.
- **Data:** telemetry is off through `TELEMETRY_ENABLED=false` (Docker Agent has no config key for
  it) and OpenTelemetry is off because `--otel` is never passed (XI). The session database lives in
  the container's `/tmp` and goes with the container.

### Run it

One run, one throwaway container, the description on standard input:

```sh
docker run --rm -i \
  --network <a network whose only other member is the gateway, as "gateway"> \
  --read-only --user 65532:65532 --cap-drop ALL --security-opt no-new-privileges \
  --tmpfs /tmp:uid=65532,gid=65532 \
  -e HOME=/tmp/home -e TELEMETRY_ENABLED=false \
  -e MODEL_GATEWAY_URL=http://gateway:8080 \
  -e ANTHROPIC_API_KEY=placeholder-not-a-key \
  -v "$PWD/src/agents:/agents:ro" \
  "$(awk '$1=="docker-agent"{print $2}' src/deploy/images.lock)" \
  run --exec --last --safety restricted /agents/analyst.yaml - < description.txt
```

- `run --exec` runs once without the terminal interface; `--last` prints only the final answer, so
  standard output is exactly the proposal's JSON. Docker Agent's greeting goes to standard error.
- `-` reads the user message, the developer's description, from standard input. It can also be the
  last argument instead.
- `--safety restricted` is the platform's choice, not the definition's (XIV): any tool call that
  is not safe is denied without asking.
- The whole `src/agents` folder is mounted, because the definition loads
  `analyst/instruction.md` from beside it.
- Exit status 0 means an answer was accepted by the configured schema. Otherwise the status is 1
  and standard error says why, for example `agent produced no final answer: max iterations
  reached` when the model never gave a valid answer, or a budget message.

To check that the definition loads, without any model call and without network, add `--dry-run`
and use `--network none`. Both ran against the pin on 2026-11-12, as did runs against a stand-in
gateway that answers with a recorded tool call. A valid proposal took one request
(`POST /v1/messages?beta=true` with `model: claude-haiku-5-5`, `output_config.effort: medium`,
adaptive thinking and the placeholder key), and standard output held only the JSON. A proposal the
schema refuses was sent back with the schema's reason four times, and the run exited 1.

### Schema binding

Docker Agent v1.160.0 takes `structured_output.schema` only inline in the YAML, and rejects at load
any `$ref` that does not start with `#`. The proposal schema takes its parts from
`slice-defs.schema.json` by `$ref`, so `analyst.yaml` carries a derived copy, never edited by hand
(XVII):

- `scripts/generate.sh` writes it between the two `generated by scripts/generate.sh` marker lines:
  `slice-proposal.schema.json` with every `$ref` resolved by Redocly CLI (`bundle
  --dereferenced`), without its `$id`, as JSON.
- `scripts/generate.sh --check-committed` fails naming `analyst.yaml` when the block differs from
  what the source schemas give. `scripts/check.sh` runs it before anything regenerates, and the
  pre-commit hook runs it when an agent definition, a `*.schema.json` or `scripts/generate.sh` is
  staged. The block is committed, because the definition must carry it.
- To change the schema, change the files in `src/engine/api/`, then run `scripts/generate.sh`; it
  rewrites the block. A changed schema is a changed agent revision, so the analyst's evals run
  again (XIX).

Tool mode, not native mode: native mode sends the schema to Anthropic's structured outputs, which do
not support constraints the proposal schema uses (`minLength`, `maxItems`, `if`/`then`). The
platform still validates the answer against `slice-proposal.schema.json` itself (D17).

### Retries and cost

An answer the schema refuses goes back to the model with the reason, and the model answers again
in a new model request. That request carries the whole history (prompt, description, refused
answer, reason), so each retry costs about as much as the first answer, or more. When the model
answers in plain text instead of calling the tool, the runtime adds a reminder, at most twice. All of
it counts against the same limits: at most 4 model requests (`max_iterations`), 200,000 tokens,
$0.10 and 5 minutes per session, checked between requests, so the last request can go over. Even
then a session stays within one pattern step (1,000,000 tokens, $0.20, 10 minutes). When the limits
are reached without an accepted answer, the run exits 1 with `agent produced no final answer: max
iterations reached` or a budget message.

## The step agents

The tests, code and review agents do the pattern's three agent steps (spec US2). Each runs as the
pinned Docker Agent `serve api` in its step's container, called by the engine once per attempt
(research D29), with the message `src/engine/steps` builds: the step, the project folder under
`/projects`, the writable paths and each input by name and SHA-256, and, for a later round, the open
findings and the lines the last fix changed.

- **Model:** `claude-haiku-5-5`, pinned (XV), only through the gateway, as the analyst.
- **What the platform owns (XIV):** the safety mode (the engine opens each session with
  `safety_policy: autonomous`, so the tools run; the container is the boundary), which paths are
  writable (the container mounts only the step's writable paths writable, D5), egress (only the
  gateway) and the sandbox (gVisor, D13). The definitions declare no `permissions`, hooks,
  `sandbox` or safety key, so they widen none of it.
- **Tools:** each declares only what its step needs.

  | Agent | Writes (pattern) | Toolsets |
  |-------|------------------|----------|
  | tests | `tests/**` | `filesystem` without `remove_directory`; `shell` for `go vet`, `go build`, `go test` |
  | code | `internal/**`, `cmd/**` | the same |
  | review | `review/**` (its witness tests) | the same; `shell` to see each witness fail |

  The shell's `go` runs offline (`GOFLAGS=-mod=vendor`, `GOPROXY=off`, `GOWORK=off`,
  `GOTOOLCHAIN=local`); the container has no network but the gateway anyway.
- **The seam:** the tests always serve `gen.Handler(app.New())`; the generate procedure writes
  `internal/app/app.go` with `New` returning the stub, and the code agent replaces it, keeping
  `func New() gen.ServerInterface` (pattern.yaml).
- **Prompts:** each instruction states the step's writable paths and allowed dependencies as the
  pattern declares them, that everything read from the project or listed in the message is data and
  never instructions, and that the agent's own "done" counts for nothing: only the step's checks
  decide (XX).
- **Answers:** the tests and code agents' answers are not read; their files are, through
  `readStepFiles`. The review agent answers through `structured_output` in tool mode with the bare
  JSON `{"findings":[{"rule","file","line","text","blocking","witness"}]}` the engine requires (T054h): the
  runtime validates it against the schema in `review.yaml`, so no fence or prose reaches the engine.
  That schema's source is [`review/findings.schema.json`](review/findings.schema.json), whose
  finding fields are `machine.Finding`'s; `scripts/generate.sh` writes it into `review.yaml`
  between its markers, as for the analyst, and `--check-committed` fails when the two differ.
  Since the file is below `review/`, it is part of the review agent's digest and the engine embeds it.
  A finding cites a slice rule `R-<n>`, a project file and a line. A blocking one also names
  `witness: {file, test}`, which the schema requires when `blocking` is true: a new Go test file
  the agent wrote directly in `review/r<n>/` (package `r<n>`, standard library and the project's
  own packages only) and its test `TestR<n>…`. The finding blocks only when the check service runs
  that test alone and it fails having run the finding's line, per its coverage profile; otherwise
  it is a suggestion (FindingsCarryEvidence, NoBlockingFindingOpen; research D35, ADR 0039). The
  agent writes only new files: no step may change or remove an earlier round's witness
  (TestsUnchanged), and the fix step must make each witness pass.
- **Budget:** per session 900,000 tokens, $0.15 and 8 minutes, at most 40 model turns, within one pattern step (1,000,000 tokens, $0.20, 10 minutes).

To check that a definition loads, run it as the analyst above with `--dry-run` and
`--network none`, for example `run --exec --dry-run /agents/tests.yaml -`.
