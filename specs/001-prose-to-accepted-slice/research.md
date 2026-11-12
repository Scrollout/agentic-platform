# Research: From a description to accepted working software, one slice at a time

Each decision names its deciding source: an accepted decision record, the ADR 0026 spike (a
reproduced experiment), or a primary source checked on 2026-11-11 or 2026-11-12. Revised after
fresh review round 1.

## Sources

| ID | Source |
|----|--------|
| S1 | [ADR 0002: Build the platform on Docker Agent](../../docs/decisions/0002-build-on-docker-agent.md) (containers only, Haiku for the MVP) |
| S2 | [ADR 0004: Contract-first toolchain, service-owned APIs and versioning](../../docs/decisions/0004-contract-first-toolchain.md) |
| S3 | [ADR 0026: Deterministic workflow engine](../../docs/decisions/0026-deterministic-workflow-engine.md), including its spike results |
| S4 | [From prose to software through checked layers](../../docs/references/prose-to-software-pipeline.md) |
| S5 | [Docker Agent reference](../../docs/references/docker-agent.md) |
| S6 | `gh api` checks, 2026-11-11: cucumber/godog (MIT, v0.16.0), cucumber/gherkin (MIT, v42.0.1), stoplightio/spectral, oasdiff (Apache-2.0, v1.37.0), santhosh-tekuri/jsonschema (Apache-2.0, v6.0.3), oapi-codegen (Apache-2.0, v2.8.2), schemathesis (MIT). 2026-11-12: Redocly CLI (`@redocly/cli`, MIT, v2.72.1 of 2026-10-29), oapi-codegen/runtime (Apache-2.0, v1.8.0 of 2026-10-23), from the npm registry and the Go module proxy |
| S7 | godog `internal/flags/flags.go`: `--strict` "fail suite when there are pending or undefined or ambiguous steps" |
| S8 | [EARS official guide](https://alistairmavin.com/ears/) |
| S9 | [Gherkin reference](https://cucumber.io/docs/gherkin/reference/): `Rule` represents one business rule |
| S10 | OpenSSF Scorecard API, 2026-11-12 (scores in D22) |
| S11 | [Anthropic Commercial Terms of Service](https://www.anthropic.com/legal/commercial-terms), effective 2025-06-17: "Anthropic may not train models on Customer Content from Services." |
| S12 | [gVisor Docker Quick Start](https://gvisor.dev/docs/user_guide/quick_start/docker/): `runsc install` adds a Docker runtime selected per container |
| S13 | [ADR 0027: Contract-driven frontend](../../docs/decisions/0027-contract-driven-frontend.md) |
| S14 | RFC 8785, JSON Canonicalization Scheme |
| S15 | [Sigstore cosign `sign`](https://github.com/sigstore/cosign/blob/main/doc/cosign_sign.md) and [`verify`](https://github.com/sigstore/cosign/blob/main/doc/cosign_verify.md) references; [Docker: DCT retirement and migration guidance](https://www.docker.com/blog/docker-content-trust-retirement-and-migration-guidance/) ("Digest pinning verifies content integrity ... it does not by itself prove publisher identity"); [CNCF Distribution: deploying a registry](https://distribution.github.io/distribution/about/deploying/) |
| S16 | Experiment, 2026-11-12, in the session scratchpad: `registry:3` on 127.0.0.1 plus `ghcr.io/sigstore/cosign/cosign:v3.1.4` (by digest) in containers; results in D24 |

## Decisions

### D1. One language for the platform: Go 1.26

- **Decision**: all platform code is Go 1.26.
- **Rationale**: Docker Agent and its SDK are Go (S5); the spike's engine, guards and generator ran
  on Go 1.26 (S3). One stack keeps tooling small (constitution, Stack).
- **Alternatives**: Python (the spike's driver) and TypeScript: a second stack for no gain.
- **Follow-up**: a stack that sets a pattern needs a decision record (task in tasks.md).

### D2. Rules are structured records; EARS text is derived

- **Decision**: a rule is a structured record (pattern, precondition, trigger, feature, system,
  response, term references) validated by JSON Schema; its EARS sentence is generated, never parsed.
- **Rationale**: grammar and term checks become schema and reference checks (FR-004). EARS fixes
  clause order and keywords (S8); models produce valid structure reliably but are weakest at rule
  meaning, which the developer approves (S4).
- **Alternatives**: parsing EARS prose (fuzzy input, a parser of our own); FRET (heavy for the MVP).

### D3. Examples are Gherkin scenarios under a `Rule`; they run with godog `--strict`

- **Decision**: each rule's examples are scenarios under a `Rule:` block tagged with the rule's ID;
  the build runs them with godog `--strict`.
- **Rationale**: `Rule` represents one business rule (S9); the parser is generated from a grammar;
  `--strict` fails on undefined steps, proving every example is bound to a test (S7).
- **Alternatives**: plain Go table tests lose the rule-to-example link.

### D4. Contracts: OpenAPI 3.1, linted with Spectral, diffed with oasdiff

- **Decision**: the slice's contract and the engine's contract are OpenAPI 3.1, linted with Spectral
  using the project ruleset (slice contracts use `spectral:oas` instead, D28), checked with oasdiff, and Go code is generated with oapi-codegen.
- **Generation (T012)**: the engine contract is bundled into one document with Redocly CLI, then
  oapi-codegen generates the engine's strict `net/http` server and the client used by the terminal
  client and the run page. An OpenAPI Overlay with the provider (`src/engine/api/engine.overlay.yaml`)
  gives Go names to the few bundled schemas whose generated names would collide, so the contract
  carries no Go names. Generated code is not committed (constitution, Clean tree):
  `scripts/generate.sh` makes it, and `--check` proves it matches the contract.
- **Node tools**: Redocly CLI, Spectral and markdownlint-cli2 are pinned with their whole dependency
  tree, with integrity hashes, in `scripts/tools/package-lock.json`, installed with `npm ci` and run
  without network in the pinned Node image by `scripts/npm-tool.sh` (Principle XIII; analyze review
  2026-11-12, finding A3).
- **Breaking changes**: oasdiff checks a slice's contract against its previously approved version
  (FR-004; T029, T037), and the engine's newest contract against its previous version once a
  second exists (T024a).
- **Rationale**: ADR 0004 names exactly these tools (S2). The plan's first draft used vacuum; that
  deviated from an accepted decision without a superseding one and is corrected here.
- **Compatibility baseline**: a slice's first contract is checked against an empty document; a
  changed contract against its previously approved version (FR-004).

### D5. Step containers: one set per slice, started by the launcher, registered with the engine

- **Decision**: when a slice is approved and the developer asks to build, the launcher starts that
  slice's step containers, each with only its step's writable paths mounted writable, under gVisor
  (D13); it does the same before `resume` and `replay`. Each container registers with the engine
  using its own token, which names its slice and step. `startRun`, `resumeRun` and `replayRun` are
  refused with `WorkersNotReady` until every step of the pattern has a registered worker. Checks
  run in a separate check container under gVisor, never in the engine.
- **Rationale**: read-only mounts per step are a required guard (S3); the engine never receives the
  container engine's socket (S1); an API client without the launcher gets an explicit refusal, so
  the API stays the only management path (Principle III).
- **Alternatives**: the engine starting containers itself needs the socket (forbidden); generic
  pre-started containers cannot narrow writable paths per slice.

### D6. A host launcher that only drives Compose

- **Decision**: `src/deploy/ap` installs nothing and starts nothing outside `docker compose`; besides
  calling it, it verifies image signatures and generates tokens. Its own commands are `up`, `down`,
  `build-workers`, `page` and `test`; it runs `build-workers` before passing `build`, `resume` or
  `replay` on; every other command is passed to the terminal client container.
- **Rationale**: the platform needs nothing from the host beyond a container runtime (Principle
  VIII), with the gVisor runtime installed as part of it (D13).

### D7. Management through the engine API, all state changes asynchronous

- **Decision**: the engine API (contract at
  `src/engine/api/preview/2026-11-12-preview/openapi.yaml`) is the only management path. Every
  state-changing operation, including approvals and part changes, returns an operation that can be
  followed and cancelled; the one exception is a worker's registration, which takes effect at
  once. Failed operations are retried with exponential backoff and jitter up to 3 times, then kept
  visibly as `failed` with the reason (the local form of a dead letter). The engine also serves `GET /providers/Agentic.Engine/operations` and the ADR 0004 version errors.
- **Rationale**: Principles III and IV; ADR 0004's operations list and errors (S2).

### D8. Run records and reuse: append-only files and a content-addressed store

- **Decision**: JSON Lines run records, artifacts stored under their SHA-256, in a named volume.
  Every event records the run, step, attempt, model, agent definition digest, pattern version,
  input hashes, reuse key and the goal, rule or example it concerns. A step's reuse key is the hash
  of pattern version, step, input hashes, agent definition digest and model id.
- **Rationale**: FR-018 to FR-020, SC-005, SC-006; agent output differs between runs while fixed
  inputs give identical generation (S3), so reuse is what makes a replay identical.
- **Alternatives**: SQLite (not needed for one writer); a durable-execution engine (for hosting).
- **Follow-up**: record the store format in a decision record.

### D9. Model access through an nginx gateway; Haiku; no other egress

- **Decision**: agents call Claude Haiku (`claude-haiku-5-5`) only through a gateway built on the
  official nginx image, which injects the credential from a template at start, allows only the
  provider's host and logs no headers or bodies. No other container has egress.
- **Rationale**: S1 and S3; Principle VI prefers a mature project to custom code: nginx (BSD-2,
  Scorecard 7.9) replaces the spike's custom Go gateway. Docker Agent's `--models-gateway` only
  points clients at a gateway; it is not one (S5).

### D10. Pattern fit is a deterministic rule

- **Decision**: a pattern declares conditions (for example: the contract is OpenAPI 3.1 with only
  HTTP operations; every rule's system is the one service). A slice fits when every condition
  passes; the check runs at approval of the contract and again at `startRun`.
- **Rationale**: a model deciding fit would breach Principle XX (fresh review A1).

### D11. Review findings must carry evidence; later rounds narrow

- **Decision**: a finding blocks only when it names a rule ID, file and line and a rule test
  confirms it. Rounds after the first check only open findings and lines the fixes changed.
- **Rationale**: the spike's citation-only guard failed (S3); constitution, Review converges.

### D12. Tokens per session and per container

- **Decision**: `ap up` creates a session; the launcher generates a random token per session and per
  container; each container accepts only calls with the caller's token, on an internal network.
  The engine API has three token kinds: the client token may call every developer operation, the
  page token only GET operations, and a worker token only `registerWorker`; others get 403.
- **Rationale**: Principle V within a local version; full workload identity is an approved
  exception (plan, Complexity Tracking). "Session" here is the platform's start, distinct from a
  build run.

### D13. gVisor sandbox for every agent step and every check

- **Decision**: step and check containers run with the gVisor runtime (`runtime: runsc`), with CPU,
  memory and process limits and an ephemeral file system outside their mounts.
- **Rationale**: Principle VIII requires agent-generated code and command-running tools to run in a
  sandbox stronger than a plain container; gVisor is selected per container in Docker (S12),
  Apache-2.0, Scorecard 6.2 (S10).
- **Cost**: the developer installs the gVisor runtime once (`runsc install`), as part of the
  container runtime.
- **Tested** (2026-11-12, runsc release-20261109.0, Docker 29 with cgroup v2 and the systemd
  driver, no `--systemd-cgroup` flag needed): a container with the step and check workers' Compose
  settings (gVisor, UID 65532, read-only root, all capabilities dropped, `no-new-privileges`, an
  executable `/tmp` tmpfs, 2 CPUs, 2 GB, 512 processes, an internal network) built and ran a Go
  program, could not write its root file system and had no egress. Limits apply to the whole
  sandbox: over 64 MB of memory it was stopped with exit 137; over 64 processes it was stopped; with
  a 16-process limit it could not start, because the sandbox's own threads count.

### D14. Budgets, timeouts and rate limits

- **Decision**: the pattern declares per-run budgets (model tokens, cost in USD, wall-clock minutes:
  30) and per-step budgets, enforced through Docker Agent budgets and the engine's clock. A run
  waiting for the developer ends `handed-over` when the developer accepts the hand-over, or
  `expired` after 14 days. The engine API and page allow 20 requests per second
  per token; requests without an accepted token are limited to 20 per second per source address
  (T018a).
- **Rationale**: Principles X and XIV; Docker Agent budgets (S5).

### D15. Idempotency keys

- **Decision**: every mutating call accepts `Idempotency-Key`, kept 24 hours; a reused key with a
  different body returns 409 `IdempotencyKeyReused`.
- **Rationale**: Principle XV.

### D16. Data handling for the local version

- **Decision**: prose and code go only to Anthropic's API through the gateway; Anthropic may not
  train on API content (S11). Run records and projects are kept until the developer deletes them
  with `ap delete`; product telemetry and trace content capture are off. Encryption between local
  containers and at rest is not provided in this version (approved exception).
- **Rationale**: Principle XI; S11.

### D17. Canonical hashes for approvals; a separate proposal schema

- **Decision**: each part's hash is the SHA-256 of its RFC 8785 canonical JSON (S14). The `rules`
  part covers rules, examples and terms. The slice exposes each part's current hash. The analyst's
  output is validated against `slice-proposal.schema.json`, which has no platform-owned fields
  (`id`, `state`, approvals, checks, history, derived text, question IDs and answers). Both schemas
  take their parts from `slice-defs.schema.json`.
- **Rationale**: fresh review U2 and U3.

### D18. The read-only page and ADR 0027

- **Decision**: the run page is a small server-rendered Go page that calls the engine API's GET
  operations with its own token. It is not the web console of ADR 0027 (S13), which comes in a later
  feature.
- **Rationale**: FR-024 needs inspection only; building ADR 0027's console now would exceed the MVP.

### D19. Testing without the live model, and evals with it

- **Decision**: unit, contract and acceptance tests run against a recorded model (the gateway
  replaced by a replay server returning recorded responses), so CI needs no key. Live evals run the
  example slice and the planted failures, plus prompt-injection cases in the description and in
  earlier artifacts, 3 times each (pass^3), with a test-scoped API key that has a provider spending
  limit. That key (owner, 2026-11-12) belongs to its own Claude Console workspace with a $50 monthly
  spend limit, lives outside the repository in a file holding only the key, and is read only by the
  gateway. Recording the model's responses and every live eval of the MVP use Claude Haiku
  (`claude-haiku-5-5`, D9), never a larger model.
- **Rationale**: Principle XIX; the constitution's test rules.

### D20. End to end, locally

- **Decision**: no hosted forge or CI for now (owner, 2026-11-12). `scripts/check.sh` and
  `scripts/e2e.sh` run every check and the whole Compose stack with gVisor locally, against the
  recorded model, never mounting the engine socket into a container; git hooks run them before
  every commit.
- **Rationale**: the first feature must run end to end through its checks (constitution,
  Outside-in); without a forge, local hooks are where they run.

### D21. What Docker Agent provides and what the platform adds

| Docker Agent provides | The platform adds |
|-----------------------|-------------------|
| Agent definitions, model access, budgets, structured output, the API server per agent | The workflow engine, its checks, reuse and run records |
| Toolsets (filesystem, shell) inside a step container | Per-step scope through mounts, and the gVisor sandbox |
| — | The slice model, traceability checks, approvals and the terminal client |

### D22. OSS evaluation (Scorecard, S10)

| Project | Licence | Scorecard | Note |
|---------|---------|-----------|------|
| Docker Agent | Apache-2.0 | not published | adopted by ADR 0002; run the scorecard locally (task) |
| godog | MIT | 6.2 | |
| cucumber/gherkin | MIT | 7.2 | |
| cucumber/messages | MIT | 7.2 (2026-11-09) | required by cucumber/gherkin (v34.2.0); owner approved 2026-11-12 |
| google/uuid | BSD-3-Clause | 6.9 (2026-10-26) | required by cucumber/messages (v1.6.0, indirect); stable, last change 2024-11 |
| yaml/go-yaml (`go.yaml.in/yaml/v3`) | Apache-2.0 | not published | the engine's YAML reader since T018 and `src/slice`'s for contracts (v3.0.4); run the scorecard locally (T063) |
| oapi-codegen | Apache-2.0 | 7.5 | |
| oapi-codegen/runtime | Apache-2.0 | 7.2 (2026-10-26) | library imported by the generated code |
| Redocly CLI | MIT | not published | bundles contracts only; run locally (T063) |
| Spectral | Apache-2.0 | 4.7 | low score; pinned by digest; watch |
| oasdiff | Apache-2.0 | not published | run locally (task) |
| santhosh-tekuri/jsonschema | Apache-2.0 | 3.2 | low score; pinned; small and replaceable |
| Schemathesis | MIT | 6.2 | |
| nginx | BSD-2 | 7.9 | |
| gVisor | Apache-2.0 | 6.2 | |
| cosign | Apache-2.0 | 8.5 (2026-11-11) | signs and verifies our images (ADR 0028) |
| CNCF Distribution (`registry`) | Apache-2.0 | 7.6 (2026-11-11) | local registry for signed images (ADR 0028) |
| WireMock | Apache-2.0 | 5.8 (2026-11-09) | the recorded-model replay server and recorder for tests (D26) |
| buildkit-syft-scanner | Apache-2.0 | not published | BuildKit's SBOM generator, a wrapper of syft (8.5, 2026-11-09); run locally (T063) |

Custom code is limited to the engine, its checks, the slice model, the terminal client and the run
page, none of which an existing project provides.

### D23. Event names

- **Decision**: run-event kinds are named noun plus past-tense verb: `RunStarted`, `RunResumed`,
  `RunInterrupted`, `StepStarted`, `AttemptMade`, `CheckPassed`, `CheckFailed`, `StepAccepted`,
  `StepRejected`, `ResultReused`, `FindingRecorded`, `RunHandedOver`, `HandOverAccepted`,
  `RunExpired`, `RunCancelled`, `RunCompleted`.
- **Rationale**: constitution, Names say what things do.

### D24. Signing our images and verifying them at start

- **Decision** (accepted by the project owner on 2026-11-12; [ADR 0028](../../docs/decisions/0028-sign-platform-images-locally.md)): `src/deploy/build.sh` builds each image with an SBOM and build provenance
  (`docker buildx build --sbom=true --provenance=mode=max`, SBOM generator pinned by digest) and
  pushes it to a local registry (`registry:3`, pinned, published on 127.0.0.1 only, data in a
  volume). It signs each pushed digest with cosign, run from its pinned image, using a key pair
  made once and kept outside the repository (`~/.config/agentic-platform/signing/`, private key
  encrypted and mode 600) and a signing config with no transparency log, so no image name or
  digest leaves the machine. `ap up` verifies every digest with the public key, from a container on
  an internal network, before it starts anything; any failure stops `up` (XIV, fails closed), and
  Compose starts images by digest. Upstream images (Docker Agent, nginx, Go) stay pinned by digest
  only: neither the Docker Agent nor the nginx pin carries a cosign signature or attestation, and
  Docker Content Trust is retired.
- **Evidence** (S15, S16):
  - Signing and verifying worked offline: a signed digest verified on an internal network with no
    internet; an unsigned digest pushed under the same tag failed (`no signatures found`); a
    different public key failed; a signature verified only with the matching key.
  - cosign v3 rejects `--tlog-upload=false`; a signing config without transparency-log services
    (`cosign signing-config create`) replaces it, and `verify` then needs `--insecure-ignore-tlog`.
    Without that flag, verification tries to reach Sigstore's public TUF server.
  - `docker buildx build --sbom=true --provenance=mode=max --push` attached both attestations on
    this machine's Docker 29 (containerd image store) and pushed them to the local registry. It
    pulls an SBOM scanner image, which must then be pinned.
  - `cosign tree` found no signatures or attestations for the pinned `docker/docker-agent` and
    `nginx` images.
- **Limit**: the key sits on the same machine that builds and runs the images. A process running
  as the developer could read the key and sign its own image. What signing proves locally is that
  `ap up` starts exactly the images `build.sh` built, not who built them. Publisher identity
  arrives with hosted CI and keyless signing.
- **Alternatives rejected**: Docker Content Trust (retired; data deleted in 2028); Notation (also
  registry-based, a second tool for the same result); a cosign bundle without a registry
  (`--upload=false`) plus a local OCI layout, which Compose cannot start from; digest pins alone
  for our images (they do not meet Principle VIII's "signed and verified at deploy time").
- **Cost**: two more pins (`registry`, `cosign`, plus the SBOM scanner); a registry container
  while building and starting; one decision record when adopted (hard to reverse), and component
  concepts for cosign and Distribution.

### D25. RFC 8785 canonical JSON is written in the engine, not adopted

- **Decision**: part hashes (D17) use a small RFC 8785 canonicaliser in `src/engine/slices`
  (`canonical.go`) instead of a library. It covers objects, arrays and strings, which is all a
  slice part contains under the schemas (no numbers, booleans or nulls), and refuses anything else.
- **Evidence** (2026-11-12): the reference implementation `github.com/cyberphone/json-canonicalization`
  has an OpenSSF Scorecard of 2.2, its licence is reported as NOASSERTION by GitHub, and it has no
  tagged Go release (only a pseudo-version); Principle VI asks for mature, stable projects. The
  engine's canonicaliser matched an independent Python reference on 3,000 random nested inputs,
  including control characters, `<>&`, U+2028/U+2029 and characters outside the BMP (T037).
- **Limit**: numbers are not supported; a schema change that puts a number into a part must
  replace this with a full implementation or an adopted library first.
- **Principle VI**: this is the written justification for custom code that an OSS project covers.

### D26. The recorded model is WireMock; the custom code is a small recording tool

- **Decision**: the replay server of D19 is WireMock 3.13.2 (`wiremock/wiremock:3.13.2-3-alpine`,
  pinned by digest in `src/deploy/images.lock`), configured only by its own stub-mapping JSON files
  in `src/tests/recorded/recordings/`. A recording matches on the method, the exact URL (path and
  query as one string, so an extra query parameter misses) and the request body as JSON (`equalToJson`, nothing ignored); any other request gets WireMock's 404 and is never
  forwarded, and the server runs only on an internal network. Recording is WireMock's own recorder
  (`/__admin/recordings`) proxying to the real gateway, run only by the owner
  (`src/tests/recorded/record.sh`). The admin API takes a random per-container credential
  (`--admin-api-basic-auth`), so the agent under test cannot add recordings.
- **Evidence** (2026-11-12): against the pinned Docker Agent, the analyst sent a byte-identical
  request body twice for the same description, so exact body matching is deterministic; with no
  recording, WireMock answered 404 and the analyst exited 1 naming the 404; WireMock's recorder,
  put in front of a replay server, wrote a matcher equal to the synthesised one, and its event
  stream was replayed byte for byte (`smoke.sh`). Scorecard and licences: WireMock 5.8 (Apache-2.0,
  7.4k stars, pushed daily; 3.13.2 is the latest stable release, 4.0 is in beta), mitmproxy 6.0
  (MIT), Hoverfly 4.7 (Apache-2.0), scored 2026-11-09 and read 2026-11-12 from the OpenSSF Scorecard API
  (`https://api.securityscorecards.dev/projects/github.com/wiremock/wiremock`,
  `.../github.com/mitmproxy/mitmproxy`, `.../github.com/SpectoLabs/hoverfly`). WireMock's recorder
  writes the path and each query parameter's exact values, which would also answer a request with
  an extra parameter, so `recording write` turns them into one exact `url` (checked 2026-11-12).
- **Alternatives rejected**: mitmproxy's server replay (`--server-replay`, with
  `--server-replay-extra` to refuse unknown requests) stores flows in a binary format that cannot
  be reviewed in a diff or written without its Python API, and matches the body only as bytes;
  Hoverfly scores lower and records only as a forward proxy, which Docker Agent's base URL does not
  use; an own Go replay server would rewrite what WireMock does (Principle VI).
- **Custom code** (Principle VI justification): `src/tests/recorded/recording` (one Go command,
  no dependencies). `write` keeps only the response's `Content-Type` from WireMock's recorder
  output, because WireMock has no setting to drop recorded response headers and the provider's
  request and organisation ids must not reach the repository; it also moves the event stream into
  its own file and refuses a recording that proxies. `synth` builds a recording without a model
  call (a structured-output tool call answering a given request), which WireMock cannot do. The
  start and record scripts are plain `docker` commands.
- **Limit**: a changed prompt, schema, model setting or Docker Agent version misses every
  recording of that agent until the owner records again, which costs money; this is by design, so
  that a stale answer is never served. The first recording, `analyst-example-notes`, is
  synthesised from `src/tests/evals/analyst/testdata/proposals/good-notes.json`, not recorded
  from the provider.
- **Cost**: one more pin (about 300 MB, a JVM); a component concept for WireMock.

### D27. The analyst runs as a long-lived Docker Agent `serve api` service

- **Decision** (owner-approved 2026-11-12): the engine has no Docker socket (D5, D6), so the analyst
  runs as an `analyst` service started by `ap up`: the pinned Docker Agent running
  `serve api /agents/analyst.yaml` with the agent definitions mounted read-only, under gVisor,
  read-only root, user 65532, all capabilities dropped, a static bearer token from `ap up`, and the
  session database on a tmpfs. It is on its own network, shared only with the engine and the
  gateway. The engine calls it with the standard library: one session per analyst call
  (`POST /api/sessions` with a title and `safety_policy: restricted`, then
  `POST /api/sessions/{id}/agent/analyst`, reading the event stream until `stream_stopped`), the
  proposal being the last `agent_choice` content and the model the `agent_info` model; the session
  is deleted afterwards, and the call is never retried by the client (`slices.Service` retries).
- **Evidence** (2026-11-12, pinned Docker Agent 1.160.0, recorded model, no key): it ran under
  `runsc` read-only as 65532 (76 MiB, 48 processes); 8 concurrent sessions all returned the
  proposal; its model request matched the `analyst-example-notes` recording unchanged, so tests
  replay it. A session without a title makes a second model call to name it, so the engine
  always sends one.
- **Alternatives rejected**: the Docker Agent Go SDK inside the engine (its whole runtime in the
  engine process, outside a sandbox); analysts started by the launcher per call (changes the
  createSlice contract).
- Sources: <https://docker.github.io/docker-agent/features/api-server/>;
  <https://github.com/docker/docker-agent/blob/3c9f6d45b8682e02713e85330fc98b5157e4896d/pkg/server/session_manager.go>.

### D28. Slice contracts are linted by a small Spectral service with `spectral:oas`

- **Decision** (owner-approved 2026-11-12): a `contract-lint` service started by `ap up` runs a
  Node server of about 30 lines (`node:http` and the Spectral packages already in
  `scripts/tools/package-lock.json`, Apache-2.0) on the pinned Node image, under gVisor, reachable
  only by the engine, with a bearer token, a 1 MiB body cap and a 30 s limit. It lints with
  `spectral:oas` and a resolver with no file or network resolvers, so a `$ref` outside the
  document is an error and nothing is fetched. This amends D4: the engine's own contracts use the
  project ruleset; slice contracts use `spectral:oas`, because the project ruleset encodes the
  engine API's conventions (api-version, `x-limits`, problem codes) and failed the example slice
  contract with 5 errors, while `spectral:oas` gave none.
- **Evidence** (2026-11-12): the prototype gave the same 7 warnings as `spectral lint` on the
  example contract, ran under `runsc` read-only as 65532, and refused `file://`, relative and
  `http://` references with `invalid-ref` without fetching them (the CLI's default resolver
  would fetch them).
- **Alternatives rejected**: Spectral has no server mode; adnanh/webhook (no Scorecard entry, no
  pinned image, a Node process and a temp file per request); vacuum in the engine (contradicts
  D4 and ADR 0004).
- Sources: <https://github.com/stoplightio/spectral/blob/develop/docs/guides/3-javascript.md>.
