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
  (D13); it does the same before `resume` and `replay`. The launcher registers each container with
  the engine on its behalf, with a worker token that names its slice and step (D29: Docker Agent's
  `serve api` cannot call `registerWorker`; a procedure step worker's own token, which the engine
  uses to call it, comes with that grant). `startRun`, `resumeRun` and `replayRun` are refused
  with `WorkersNotReady` until every step of the pattern has a registered worker. Checks run in a
  separate check service under gVisor, never in the engine (D29).
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
  of pattern version, step, input hashes, agent definition digest and model id, and for a review
  round also its round. An open review round's result is kept and reused like an accepted one
  (owner, 2026-11-15; D36).
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
  page token only GET operations, and a worker token only `registerWorker`; others get 403. Since
  D29 the launcher, not the container, uses the worker token to register each step container. The
  engine holds its own token for each deployment it calls: the check service's, and each procedure
  step worker's, which the launcher creates and passes with the worker's grant (T053a).
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
- **The analyst's budget** (Principle XIV: every agent has token, cost and time budgets): the
  analyst works before any run exists, so its budget is its own, declared once in
  `src/agents/analyst.yaml` and enforced by Docker Agent per session, one session per proposal or
  revision (D27): 200,000 tokens, $0.10 and 5 minutes. Its step-like bounds are the pattern's
  `limits.attempts` (3) proposals per call, at most 4 analyst calls at once and a description of at
  most 4,000 characters.
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
| go-arch-lint | MIT | 4.7 (2026-10-19) | the module-boundary check (D40); low score, built from source by the checksum database and a pinned module hash; watch |

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

- **Decision**: part hashes (D17) use a small RFC 8785 canonicaliser in the engine
  (`src/engine/canonical/`, shared by `slices` and the step checks) instead of a library. It
  covers objects, arrays and strings, which is all a
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
- **Agent digest**: each proposal's `agent_digest` is the SHA-256 of `analyst.yaml` and
  `analyst/instruction.md` as mounted under `/agents`, each framed as its path, a line feed, its
  length in decimal, a line feed and its bytes, in byte order of path (`src/engine/README.md`).
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

### D29. Agent steps run on Docker Agent `serve api`; checks and procedures run in a check service with its own contract

- **Decision** (owner-approved 2026-11-13; T054a):
  - **Agent steps**: each agent step container runs the pinned Docker Agent `serve api` with that
    step's agent definition, under gVisor with only the step's writable paths mounted writable
    (D5, D13). The engine's `machine.Worker` calls it with the analyst's session flow (D27): one
    titled session per attempt with `safety_policy: autonomous` (T049b; the analyst's stays
    `restricted`), the event stream read until
    `stream_stopped`, the session deleted afterwards. The launcher registers each container with
    the engine on its behalf (T053a), since `serve api` cannot call `registerWorker`. No new
    contract: Docker Agent's API is the adopted contract (Contract-first, "for an adopted
    protocol, its published schema is the contract").
  - **An agent attempt's usage and changed lines** (owner, 2026-11-13; T054c): the usage is read
    from the session's `token_usage` events. Each event's `usage` (`input_tokens`,
    `output_tokens`, `cost` in USD) is its session's running total; the per-message figures are
    only in `last_message`. A sub-session sends its own events under its own `session_id`, and
    its cost is not folded into its parent's. No summary event exists, and `stream_stopped`
    carries no usage. So the engine takes the last event of each `session_id` (the root's and
    each sub-session's) and adds those up, never summing events of one session. The changed
    lines are not taken from the agent's answer (Principle XX): they are a line diff of the
    files `readStepFiles` returns against `before`, with each earlier file's content found in
    the run's accepted outputs in the store, so the diff survives a restart.
    Sources: <https://github.com/docker/docker-agent/blob/v1.160.0/pkg/runtime/event.go>.
  - **Reuse until T053a writes reused files** (owner, 2026-11-13; T054c): an accepted result that
    left project files is reused only within the project its files were written into. Nothing
    yet writes a reused step's files into a new project, so in a replay that step runs again
    rather than hand the next step a `before` the folder does not hold. Results without files
    (none today) may still be reused. T053a lifts this guard (`machine` `reusable`) once it writes
    the reused files into the new project; US2.8's byte-identical replay depends on that. How
    they are written: D30 (`commitStep` with `reused_from`, T053e).
  - **Model for reuse keys**: the model each agent definition pins (Principle XV) is the model in
    the step's reuse key (D8). The engine checks it against the model the session reports in
    `agent_info`; a different model fails the attempt.
  - **Checks and fixed procedures**: one small Go program with its own versioned OpenAPI
    contract, `src/check/api/preview/2026-11-13-preview/openapi.yaml`, of which it is the
    provider, serves two deployments, each accepting only a bearer token issued to the engine.
    Never in the engine (D5).
    - The **check service**, in the check container with the project mounted read-only, serves
      `runCheck` and `readStepFiles`.
    - A **procedure step worker**, one per procedure step (owner, 2026-11-13), started and
      registered by the launcher like an agent step's container (T053a) with only that step's
      writable paths mounted writable, serves `runProcedure` for its step's procedure: it writes
      the step's files into the project and returns them. The engine calls the endpoint the
      worker registered.
    - Each deployment answers the operations it does not serve with 404 (`UnknownCheck`,
      `UnknownProcedure`, `UnknownStep`).
  - **Agent step outputs** (owner, 2026-11-13): the check service returns them with a separate
    read operation, `readStepFiles`, not inside `runCheck`'s verdict: it is called once per
    attempt while `runCheck` is called once per check, so the files (at most
    `x-limits.step-files-max-bytes`, the contract's value) are not sent again with every verdict, and a verdict stays
    small. It returns every file the step's writable paths hold, not only those that differ from
    `before`, so the artifact holds the step's files on its own; the engine stores it by hash.
  - **How the tests reach the code, and two check scopes** (owner, 2026-11-13; T049b, found by
    T041): the tests always serve `gen.Handler(app.New())` from `internal/app`. The generate step
    writes the seam `internal/app/app.go` with `New` returning the
    generated stub, so the tests build and fail before any code; the code step replaces it, keeping
    `func New() gen.ServerInterface`, and may add packages under `internal/`. The code step's
    "unchanged" checks skip a file its own `writable` matches. EveryExampleBound judges binding,
    not results: no undefined, pending or ambiguous step and every scenario run, whatever the exit
    code, since the tests step's tests must fail against the stub. "Every scenario run" compares
    godog's own summary count (not any printed line shaped like one) with the pickles of
    `features/slice.feature`, each Scenario Outline Examples row one scenario (T049c). DependenciesAllowed judges only
    the step's own writable files (go.mod's requires only when go.mod is one of them). Agent step
    sessions use `safety_policy: autonomous` inside gVisor (D13), the analyst staying
    `restricted`.
  - **ContractLintPasses** (owner, 2026-11-13): the engine judges it by calling the contract-lint
    service (D28); the check service does not serve it (404 `UnknownCheck`).
  - **Synchronous calls**: every operation answers in the call, bounded by `time_limit_seconds`
    (at most 600 s, the step budget, D14). `runCheck` and `readStepFiles` are queries, which
    Principle IV lets answer synchronously. `runProcedure` writes files, but it is one attempt of
    a step inside a run, not a management operation: the run is the operation `startRun` already
    returns and lets the client follow and cancel. Nothing keeps state between calls, and the same
    request over the same project gives the same response and leaves the same files, so no
    operation takes an `Idempotency-Key`. The `idempotency-key-retention-hours` the project
    ruleset requires of every contract is declared as 1, with a comment that it promises nothing,
    until T054d scopes that rule to services that take the key (owner, 2026-11-13).
  - **What travels**: a check call carries the step's facts copied from the pattern (writable
    entries in the catalogue's glob dialect, `dir/**` or one exact path; dependencies; outputs;
    and the earlier steps' writable entries and outputs), `approved` (every approved part of the slice,
    by its stored hash and canonical JSON, filled by the engine from the slice record whatever the
    step's inputs: approval is platform data, not a step input), the inputs and the attempt's
    outputs by hash, the worker's findings and changed lines, and `before`. A procedure call carries its inputs by content,
    checked against their hashes. Outputs come back by content (base64), not by reference,
    because the service has no store and the engine records every artifact by hash (D8).
  - **Hash forms**: an approved part's hash, and a `slice/<part>` input's, is the part hash the
    slice stores (SHA-256 of the part's RFC 8785 canonical JSON). ContractMatchesApproval compares
    `canonical.ContractHash` of `api/openapi.yaml`'s text with `approved`'s contract hash, and
    ExamplesMatchApproval compares `features/slice.feature` with the examples in `approved`'s
    rules part, so the code step, whose inputs hold no `slice/contract`, is judged against the
    approval and not against another step's output. Every step
    output is a set of files hashed in framed form (path, line feed, length, line feed, bytes, in
    byte order of path, as the agent digest in D27). A one-output step's output is all files under
    its writable entries; a step with several outputs pairs output i with writable entry i
    (contract-check: `contract` = `api/openapi.yaml`, `examples` = `features/slice.feature`). An
    agent step has exactly one output.
  - **Binding verdicts to stored files** (Principle XX): the engine takes an attempt's files once,
    from `readStepFiles` (agent step) or `runProcedure` (procedure step), and stores them by hash;
    each `runCheck` recomputes the framed hash of every output from the folder and fails on a
    mismatch (outputs that are not file sets, such as the review step's `findings`, are exempt).
    An earlier step's output given as an input is bound through `before`, the engine's stored list
    of the last accepted step: each of the input's files in the folder must equal its entry there,
    except a path the judged step may itself write, which is judged as its output; and when no
    step after the input's producer, nor the judged step, may write any of its paths, its framed
    hash is also recomputed from the folder and must equal the one the request names (owner,
    2026-11-14). Recomputing the hash of every input could never pass once the code step replaced the seam
    `internal/app/app.go`, which generate's output holds (found by T054l; owner, 2026-11-14).
    The engine accepts the attempt only when every
    verdict's `project_files` equals the list that first call returned. That list becomes the next
    step's `before`.
  - **Paths**: every path is ASCII letters, digits and `._~+@=,-` in parts joined by single `/`,
    at most 256 characters, with no `.` or `..` part, so no control character can make a framed
    hash ambiguous or a path escape the folder. Nothing is read, written or removed through a
    symbolic link (`O_NOFOLLOW` on every part); a procedure's write converges on a repeat, and a
    write that fails after the procedure produced its outputs answers 500 `WriteFailed`.
  - **Limits, derived from each other**: 10,000 files per list (about 3.5 MB), 4 inputs with
    content of at most 1 MiB each (about 5.6 MB in base64), 100 findings of at most 4,096
    characters and 2,000 changed lines give a request of at most 12 MiB; 8 MiB of files (about
    11.2 MB in base64) with their lists give a response of at most 24 MiB. The time limit is the
    pattern's step budget, `budgets.step.minutes` (D14).
  - **Checks served**: of the pattern's 15 check names, the check service serves 14; every name
    but ContractLintPasses. Implemented in `engine/checks` today: ScopeKept (`Scope`),
    DependenciesAllowed (`Dependencies`), ContractMatchesApproval (`ContractHash`),
    GenerationRepeatable (`GenerationDrift`), EveryExampleBound (`GodogStrict`),
    TestsFailAgainstStub, AllTestsPass, TestsUnchanged and FindingsCarryEvidence. T054e added
    ExamplesMatchApproval, GeneratedCodeBuilds, TestsBuild, GeneratedCodeUnchanged and
    NoBlockingFindingOpen, and made the comparing checks work from `before`'s hash list.
  - **The run as a process across components** (constitution, Reliable events and side
    effects): a run spans the engine, the step workers and the check service, and agent steps and
    `runProcedure` write project files.
    - **Pivot**: the run's outcome recorded with the project handed over to the developer (`done`
      or `handed-over`). After it nothing is undone.
    - **Compensation**: before the pivot, the state-changing steps are an attempt's writes into
      the project. For a rejected attempt, the step's writable paths are restored to the last
      accepted state, from the artifacts the engine stored by hash, before the next attempt; who
      restores them, and how: the project worker's `restoreStep`, before every attempt (D30). An accepted
      attempt's files are kept: they are the state resume and later steps build on. Run-record
      events are not undone: they are the append-only record (D8).
    - **Timeout**: the step and run time budgets (D14), with each call bounded by
      `time_limit_seconds` (at most the step budget); a run waiting for the developer expires
      after 14 days.
  - **Tokens**: the engine holds one token per deployment it calls. For a procedure step worker
    the launcher creates the token, gives it to the worker in its environment and to the engine
    with the worker's grant; that wiring belongs to T053a.
  - **Layout**: the contract and its generated server, client and types live in a new Go module
    `src/check` (`src/check/api/check.oapi-codegen.yaml` → `src/check/api/gen/`); the service's
    command is `src/engine/cmd/check`, in the engine module, so it serves `engine/checks` and the
    T050 procedures without copying them, and the engine's `machine.Checker` uses the same
    generated client. `scripts/generate.sh` finds a contract only at
    `src/<service>/api/{stable,preview}/<date>[-preview]/` and writes one generated file per
    target to `src/<module>/api/gen/`, so a check client generated inside the engine module would
    share the package `src/engine/api/gen` with the engine's own server types (`Problem`,
    `ErrorCode`) and collide; a `src/check` module that also holds the command would have to
    import the engine module while the engine imports it, a module cycle. The engine module
    requires `src/check`, which requires only `oapi-codegen/runtime`.
  - **Changes this needs on protected paths** (T054c, each with the owner's `Approved-by:`):
    `src/check/go.mod` (new), `src/go.work` (`./check`), `src/engine/go.mod` (require the check
    module), `scripts/check.sh` (`GO_MODULES` gains `check`), `src/deploy/Dockerfile.check` (build
    and run `cmd/check`), `src/deploy/Dockerfile.engine` (the check module in its build context)
    and `src/deploy/compose.yaml` (the check service's command and its own token, the engine's
    `AP_CHECK_URL` and `AP_CHECK_TOKEN`; the check container no longer needs the worker token).
    The procedure step worker template in `compose.yaml`, like `step-agent` but running the same
    image as a `procedure-worker` deployment, was not added by T054c; it moved to T053a.
- **Rationale**: an agent step is the same Docker Agent call the analyst already makes, tested
  under gVisor (D27), so the engine keeps one client for agents and no shim is needed. Checks
  run code an agent wrote and must not run in the engine (Principle VIII, D5, D13); a contract
  between engine and check service is required between components (Contracts between
  components). Comparing against a list of hashes keeps the service stateless and independent of
  how the project's version history is kept (T053a, open). The status codes follow Google's
  mapping of `NOT_FOUND` to 404, `UNAVAILABLE` to 503 and `DEADLINE_EXCEEDED` to 504.
- **Alternatives rejected**: step and check workers pulling work through new engine operations,
  with a shim in each container to fetch work and report results (a second, home-made protocol
  beside Docker Agent's, more engine surface, and a shim per image); one check container started
  per call (it needs the container engine's socket, which no container may hold, D5, D6); checks
  in the engine (D5); the check service's command in its own module (the module cycle above);
  procedures in the read-only check service returning files for someone else to write (no
  component could write them, owner, 2026-11-13); agent step files inside every `runCheck`
  verdict (sent once per check instead of once per attempt).
- **Closed for T053a by D30** (owner, 2026-11-13): the project's version history is a git
  repository kept by a project worker, a third deployment of the check program; it commits each
  accepted step, restores the project to the last commit before every attempt (the compensation
  above), and writes a replay's reused step files into the new project (D30). **Closed by T054e** (owner, 2026-11-13): no
  other check confirms a review finding. FindingsCarryEvidence and NoBlockingFindingOpen rerun the
  tests of the rule the finding names (godog scenarios tagged `@R-<n>`, and Go tests whose name
  names the rule) against the current code, with `go test -json`, and a finding blocks only when
  one of those tests fails with output naming the finding's file as a project path followed by
  `:` and a line (the check contract's runCheck states the exact rule). FindingsCarryEvidence
  asks of each blocking finding a rule, a file the project holds and a line of at least 1, and
  fails only when that evidence is missing or the rerun cannot be judged (tests that do not
  build); a blocking finding the rerun does not confirm is a suggestion. NoBlockingFindingOpen
  fails on each confirmed one. `confirmations` is gone from the contract; the `check` a finding
  names is not consulted, and `run-event.schema.json` no longer requires it of a blocking finding
  (informational; oasdiff finds no breaking change). Removing `confirmations` within
  `2026-11-13-preview` is accepted because the check service is internal and the engine is its
  only client (plan, Constitution Check, XV). **Known limits** (accepted, 2026-11-13): only a
  path's last part is protected against a symbolic link swapped in after the folder is walked
  (`O_NOFOLLOW`); a child process that calls `setsid` leaves the process group the time limit
  kills, and the deployment's container (Principle VIII) contains it; and the rerun trusts the
  project's tests, so tests can fake or avoid a confirmation (the code and fix steps cannot
  change `tests/**`, which TestsUnchanged checks). Child processes get a minimal environment (no token, no
  CHECK_* setting), each project file is read up to `x-limits.project-file-max-bytes`, and a
  response above `response-body-max-bytes` is 500 ResponseTooLarge.
- Sources: <https://docker.github.io/docker-agent/features/api-server/>;
  <https://google.aip.dev/193> (error model: status code mapping);
  <https://go.dev/ref/mod#workspaces>; `scripts/generate.sh` and `scripts/contract-versions.sh`
  (read 2026-11-13).

### D30. The project's version history is a git repository kept by a project worker

- **Decision** (owner, 2026-11-13; T053a):
  - **Version history**: the new project's version history (FR-025) is a git repository in the
    project folder, `.git` at its root, on the one branch `main`. The developer receives a plain
    folder holding a normal git repository with one commit per accepted step.
  - **The project worker**: a third deployment of the check program (D29), `project-worker`,
    started by `ap up` with the check service, under gVisor (D13), running as the developer's
    uid:gid (`AP_HOST_USER`, as the step agents) so the developer owns the files and the
    repository, with the projects folder mounted read-write and no other network than `checks`.
    It runs the git CLI, and no other component writes the version history (step containers
    mount `.git` read-only). It serves two operations of the check
    contract, `commitStep` and `restoreStep`, accepts only its own token issued to the engine
    (`AP_PROJECT_TOKEN`, created by the launcher), and answers every other operation with 404,
    while the check service and the procedure step workers answer these two with 404
    `VersionHistoryNotServed`. The engine still has no git and no project mount, and the check
    service stays read-only.
  - **Its three jobs**:
    1. `commitStep` commits each accepted step (`StepAccepted`) and each reused one
       (`ResultReused`): one commit per accepted step, a step that changed no file included, with
       the run, step, round and attempt (or the run it was reused from) as trailers. It commits
       only when the last commit holds `before` and the folder holds the accepted
       `project_files`, so the history holds exactly the files the checks judged (Principle XX).
    2. `restoreStep`, before every attempt of every step, makes the folder hold `before` again
       from the last commit: whatever a rejected or interrupted attempt wrote is undone. This is
       the run's compensation (constitution, Reliable events and side effects).
    3. `commitStep` with `reused_from` and the reused outputs' stored artifacts (their framed
       form, checked against their hashes) first writes a replay's reused step files into the new
       project, as `runProcedure` writes a completed result, then commits them.
  - **Requests carry hashes, paths and stored artifacts, never commands.** Every git option the
    worker uses is fixed in its code: git always runs with `GIT_NO_REPLACE_OBJECTS=1`,
    `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, `-c core.fsmonitor=false`,
    `-c core.hooksPath=/dev/null`, `-c gc.auto=0` and `-c commit.gpgSign=false`, and only through
    plumbing that applies no filter: `hash-object --no-filters` for blobs, `update-index
    --index-info` for the index, `write-tree`, `commit-tree`, `update-ref` with the old value, and
    `cat-file` to read files back. So `.gitattributes`, `.gitignore` and `info/exclude` change
    nothing, and nothing is added to the repository's `config` or `info/`. Every file is
    committed with its exact bytes as a non-executable regular file. A path reaches git only on
    standard input or after `--`, so no path is read as an option.
  - **Deterministic commits**: author and committer `agentic-platform <noreply@localhost>`,
    dated the `at` of the run event that accepted the step, in whole seconds, UTC; message
    `Step <step> accepted in <run>` (or `reused in`) with trailers `Run`, `Step`, `Round`,
    `Attempt` or `Reused-from`. So a retried `commitStep` finds its commit (the last commit's
    trailers) and makes no second one, and equal files give the same git tree in any run. A call
    with the same trailers but another payload (a commit other than the one it would make) is
    409 `ProjectStateDiffers`.
  - **What US2.8 makes identical**: a replay reuses every step, so its artifacts are the
    original's stored artifacts, hash for hash; each step's `project_files`, and so each
    commit's tree and the final folder outside `.git`, are byte-identical to the original's.
    The commit IDs differ, since each commit names its own run and is dated from its own run's
    record; US2.8 and SC-005 judge artifacts, not commit IDs.
  - **The version history is not part of the project**: no check-contract operation lists,
    reads, judges or writes a path under `.git` except `commitStep` and `restoreStep`; a
    path with any part equal to `.git` in any letter case is not a ProjectPath, so neither a
    `writable` entry nor a project file can name one. This is a tightening within
    `2026-11-13-preview` that affects no client: the engine is its only client and no project
    holds such a path before T053c (plan, Constitution Check, XV). No operation removes or
    replaces a folder, since step containers mount the project's folders.
  - **Sizes**: a reused step's request carries up to 8 MiB of files in framed form, so
    `commitStep` has its own body limit, `x-limits.commit-request-body-max-bytes` (24 MiB); every
    other limit is the contract's existing one.
  - **The git CLI in the project worker's own image** (owner, 2026-11-13, changed the same day;
    [ADR 0033](../../docs/decisions/0033-project-worker-in-its-own-small-image.md)):
    the owner first chose to build the check image on the Debian pin `golang:1.26.11-bookworm`
    (git 2.39.5), but the built `ap-check` grew from 365 MB to 1.29 GB, too large for the host.
    So the check image stays on its Alpine base, `golang:1.26-alpine`, which has no git
    (`command -v git` exits 127 in it and in a built `ap-check`, both reproduced 2026-11-13), and
    the project worker runs the same check binary in its own image, `ap-project`
    (`src/deploy/Dockerfile.project`): a static build on the golang pin, then the `alpine` pin by
    digest with `apk add git=2.49.1-r0`, git's exact package version in Alpine 3.22, pinned in
    images.lock as `alpine-git`. An Alpine package has no digest, so git is pinned by exact
    version only, an owner-approved exception (plan.md, Complexity Tracking). The image is 45.3 MB, and
    the project worker's tests pass with its git 2.49.1 as with the host's (2026-11-13).
  - **The generate step's mount** (owner, 2026-11-13): only the generate procedure worker gets
    the project root writable, since its `writable` names `go.mod` and `go.sum` in the root and
    Docker binds a file only when it exists (creating it empty before the run would put it in
    the earlier step's folder). This is a stated limit: generate is fixed code, not an agent, and
    ScopeKept still rejects any write outside its globs. Every other step's container gets only
    its own writable paths.
  - **`.git` is read-only in every step container** (owner, 2026-11-13, review F1):
    `ap build-workers` creates the project folder with an empty `.git` folder in it and binds that
    folder read-only into every step container, on top of the generate worker's writable root, so
    no step container can write `.git`. The project worker creates the repository in that folder
    at the run's first `commitStep`, after contract-check is accepted, before generate runs. A
    bind mount needs its source to exist, which is why the launcher creates the empty folder.
  - **History guard, a second layer** (owner, 2026-11-13): the project worker keeps a record per
    project on a named volume only it mounts (`project-worker-state`). The record holds the ID of
    the last commit it made, that commit's parent ID, the run, step, round and attempt or
    `reused_from` its trailers name, and the SHA-256 of the `.git/config` it wrote. It is
    replaced atomically (temporary file, fsync, rename): provisionally right after `git init`
    (the config hash, no commit yet), then after each commit object is made and before
    `update-ref` moves the branch to it. With no record, `commitStep` empties the contents of
    `.git` (never the folder, a mount source) and runs `git init --template=` again: since no
    step container can write `.git`, anything there can only be the worker's own unfinished
    first call (a crash after `init` or after writing objects), so it never blocks a run for
    good. With a record, before any other git command, `commitStep` and `restoreStep` inspect
    `.git` as files:
    - no symbolic link and no `commondir`, `gitdir`, `shallow`, `info/grafts` or
      `objects/info/alternates`;
    - `.git/HEAD` exactly `ref: refs/heads/main` and one line feed, and the branch a loose ref
      with no `main` entry in `packed-refs`;
    - `.git/config` with the recorded hash;
    - the branch at the recorded commit, or at its recorded parent (missing, for a first commit,
      and always missing with a provisional record) only when the recorded commit is exactly
      the one the request makes (a call that stopped between the two writes).
    Then git must find the recorded commit with the recorded parent and trailers. Anything else is
    409 `HistoryChanged`, and nothing changes. The config check matters because a repository's
    own configuration can name commands git runs. What remains after the read-only mount and
    the guard: whoever can write the projects folder on the host (the developer, host processes)
    and the project worker itself can still change `.git`, and the guard then stops the run
    instead of trusting it; agents can read `.git`, which holds only this run's accepted files.
- **Rationale**: git is the version history developers already use, so the developer receives a
  normal repository with nothing to install. Putting it in the check program keeps one program
  and one contract for every component that touches the project folder, with the engine as its
  only caller; the engine needs no project mount and stays free of code an agent can influence
  (D5). Restoring before every attempt, not only after a rejection, also undoes what an
  interrupted attempt left before a resume (FR-020) with the same call, and makes every attempt
  start from the files its checks compare with (`before`). Reused files travel as the stored
  artifacts, so the replay's files are the artifacts by construction.
- **Alternatives rejected**: the go-git library in the engine or the check program (a large new
  dependency tree for what the git CLI already does, and the engine would need the project
  mount); the host launcher running git (an engine-to-launcher call the platform does not have,
  and the launcher would have to stay up during every run, while today it only drives Compose,
  D6); restoring from the stored artifacts without git (a second copy of what the last commit
  holds); restoring only after a rejected attempt (an interrupted attempt's files would need a
  second path).
- **Known limit** (analyze, 2026-11-15): `govulncheck` and `go-licenses` scan the platform modules, not go-arch-lint's own dependencies; the tool runs offline with `src/` read-only, and the same holds for the other installed tools (oapi-codegen, govulncheck, go-licenses).
- **Known limits**: the project worker can write any project under the projects folder; only the
  engine can call it, and the engine names only its run's project, a new folder (FR-025). The
  generate worker has the project root writable except `.git`, which is mounted read-only on top
  of it; ScopeKept judges its files.
- **Alternatives rejected by the owner** (2026-11-13): the check image on the Debian pin (1.29 GB,
  too large for the host; chosen first, then replaced the same day by the project worker's own
  Alpine image with git pinned by exact package version); creating root-level writable files empty before
  the run so they can be bound one by one (they would be in the earlier step's folder).
- Sources: <https://git-scm.com/docs/git-commit> (author and committer identity and dates through
  `GIT_AUTHOR_*` and `GIT_COMMITTER_*`); <https://git-scm.com/docs/git-hash-object>
  (`--no-filters`); <https://git-scm.com/docs/git-update-index> (`--index-info`);
  <https://git-scm.com/docs/git-update-ref> (old-value check);
  <https://git-scm.com/docs/git-interpret-trailers>; <https://git-scm.com/docs/git>
  (`GIT_NO_REPLACE_OBJECTS`); <https://git-scm.com/docs/gitrepository-layout> (`commondir`,
  `objects/info/alternates`, `shallow`, `info/grafts`, `packed-refs`);
  <https://git-scm.com/docs/git-config> (`GIT_CONFIG_NOSYSTEM`, `GIT_CONFIG_GLOBAL`,
  `core.hooksPath`, `core.fsmonitor`, `gc.auto`); <https://docs.docker.com/engine/storage/bind-mounts/> (a bind source must
  exist with `--mount`); experiment 2026-11-13 (git in the pinned golang images, above);
  OpenSSF Scorecard API for github.com/git/git, 5.7 on 2026-11-09.

### D31. Step worker grants reach the engine through a folder it reads at each authentication

- **Decision** (owner, 2026-11-13; T053d):
  - **Grants after start**: `api.Config.WorkerGrants` is read when the engine starts, but `ap
    build-workers` makes each slice's grants later. So `ap build-workers` writes one file per grant
    into a host folder that only the engine mounts, read-only
    (`${XDG_STATE_HOME:-~/.local/state}/agentic-platform/grants`, `AP_GRANTS_DIR`, mounted at
    `ENGINE_GRANTS_DIR`). The file is named the lowercase hex SHA-256 of the worker's token and
    holds the JSON `{"slice":"S-1","step":"code","call_token":"…"}`, where `call_token` is the
    token the engine calls that worker with. The files are mode 0600 and owned by the engine's
    user, and so is the folder (mode 700): `ap up` hands it over with a one-off container that has
    only the CHOWN capability, and `ap` writes the files from a one-off container running as that
    user, with the tokens on standard input only.
  - **Reading**: the engine reads the folder at each authentication of a token that is no session
    token and not in `WorkerGrants` (`api.Config.GrantsDir`), so a new file grants at once and a
    removed one revokes. An unreadable, non-regular, larger than 4 KiB or malformed file grants
    nothing (fail closed). `WorkerGrants` keeps working beside it. `ap down` removes every grant.
  - **Tokens**: each step container gets two tokens, both made by the launcher: the worker's
    token, which the launcher registers the container with (`serve api` cannot register itself,
    D29), and the call token, which the container accepts and the engine sends. The container holds
    only its call token, in its environment. The tokens last as long as the session and are kept in
    `.../agentic-platform/workers/<slice>` (mode 600), so running `build-workers` again changes
    nothing.
  - **A project folder of empty folders is not taken** (owner, 2026-11-13): `startRun` refuses a
    project with `ProjectExists` only when its folder holds a file (a regular file or a symbolic
    link, at any depth) or a `.git` that holds anything, even an empty folder. A folder holding
    only empty folders, such as the skeleton `build-workers` makes before the build, is accepted.
- **Rationale**: a file per grant needs no new engine operation and no restart, and the engine
  stays the only reader. Naming it by the token's hash keeps the token out of every file name,
  and reading it at each authentication makes revoking a removal.
- **Refusals and defaults** (owner, 2026-11-13, after review): `build-workers` refuses a
  project folder by the same rule as ProjectExists (a file or symbolic link at any depth, or a
  non-empty `.git`), naming the folder and changing nothing; and the `step-agent` and
  `procedure-worker` templates hold no token by default (`ENGINE_TOKEN` and `CHECK_TOKEN`
  empty), so only `build-workers` gives a step container a token. The deployed engine has no projects folder, so its startRun does not see the folder; the project worker, with no record, refuses the run's first touch of an existing folder (restoreStep for attempt 1, or a commitStep with `reused_from`) with 409 `ProjectExists`; a first touch that passes writes a `claimed` record on the worker's volume before `git init` or any file, so a retry after a crash recovers by the no-record rules (owner, 2026-11-14; T053e).
- **Alternatives rejected**: restarting the engine with a new `WorkerGrants` (drops in-memory
  registrations and running operations); a new engine operation to add grants (a management call
  the launcher would have to authenticate as something stronger than a worker).
- **Known limits**: `compose.yaml` gives `AP_GRANTS_DIR` a default naming a folder that does not
  exist, rather than `:?`, so the Compose tests' variable list stays unchanged; without the
  variable the engine does not start (the bind source is missing). Docker Agent takes the call
  token as its `--auth-token` flag, so inside an agent step's sandbox it is on docker-agent's
  command line, readable by the agent's shell tool. Re-running `build-workers` for another
  project replaces the slice's containers, also during a run (a follow-up task; resolved by D32).
- **Recorded as** [ADR 0034](../../docs/decisions/0034-step-worker-grants-as-files-read-at-each-authentication.md).
- Sources: owner's decisions, 2026-11-13; <https://docs.docker.com/engine/storage/bind-mounts/>
  (a bind source must exist).

### D32. Step containers are kept per project; resume reuses them and replay builds a new project

- **Decision** (owner, 2026-11-14; T053g, after the T053d review):
  - **Per-project containers**: a step container's name holds the slice, the step and the project:
    `<compose project>-worker-<slice, lowercase>-<step>-<p>`, where `<compose project>` is the
    Compose project name from `compose.yaml` (`agentic-platform` in a real session; tests may run
    under another) and `<p>` is the first 12 hex digits of the SHA-256 of the project name. A project name may be 63 characters, and the name is
    the host name the engine calls, which must fit one DNS label of at most 63 characters, so the
    name holds the project's hash and the labels hold the project in full:
    `org.agentic-platform.slice`, `org.agentic-platform.step` and `org.agentic-platform.project`, beside
    the existing `org.agentic-platform.worker` (which already holds the project). `build-workers`
    for project B starts B's own containers and leaves project A's running; it never removes or
    replaces a container whose project label names another project. A container with the name it
    wants whose project label names another project (a hash collision) is refused with exit 1,
    changing nothing. Every step container stays up until `ap down`.
  - **One registration per slice and step**: the engine keeps one worker address per slice and step,
    in memory, and registering again replaces it (registerWorker, D29, D31). The engine is not
    changed. So that a registration never moves away from a run that still needs it, `build-workers`
    refuses while a run of the slice is active (has not ended, data-model "Active run", FR-021),
    before it writes a grant, registers, makes a folder or starts a container: exit 4,
    `build-workers: the run <run> of <slice> has not ended (RunActive); resume, take over or cancel
    it first`. It learns this from the engine's `listRuns` (`GET /runs`, every page) with the
    session's client token, which `ap` already holds (`AP_CLIENT_TOKEN`, the token the terminal
    client gets), sent from a one-off container on the `agents` network as `register_worker`
    does: a run whose `slice` is the slice and whose `status` is not `ended` is active. An answer
    it cannot read, or no answer, refuses with exit 1 (fail closed).
  - **Resume**: `ap resume <run>` reads the run with `getRun` (`GET /runs/{runId}`, client token),
    which gives its `slice`, `project` and `status`. An unknown run is exit 4 `NotFound`. A run whose
    status is not `interrupted` is passed on unchanged, and the engine refuses it
    (`RunNotInterrupted`). For an interrupted run, every step of the pattern must have its
    container for that slice and project running, with the labels above and the session's tokens
    id; then the launcher registers each again with its worker token (the same `registerWorker`
    call `build-workers` makes, not `build-workers` itself, so no folder is made or checked and no
    grant is written) and passes `resume <run>` on. A run is interrupted when the platform stopped,
    such as an engine restart, which loses the engine's in-memory registrations, so registering
    again is what makes the workers ready. When a container is missing or stopped, or the slice's
    tokens file is gone, resume refuses with exit 3 and changes nothing: `resume: the step workers
    of <slice> for the project <project> are not running; run ap build-workers --resume <run>,
    then ap resume <run>`.
  - **Rebuilding for resume** (owner, 2026-11-14, after `/speckit-analyze` finding G1, FR-020; task
    T053j): a run interrupted by `ap down` or a host reboot must stay resumable, so `ap
    build-workers --resume <run>` rebuilds its workers. It reads the run with `getRun` for its
    slice, project and status, and refuses (exit 4, `RunNotInterrupted`, changing nothing) a run
    that is not `interrupted`, an unknown run with exit 4 `NotFound`, and, by the active-run rule,
    any other run of the slice that has not ended. It accepts the existing project folder only for
    that run: the folder named by the run's `project`, never one given on the command line. It
    then makes the slice's tokens if they are gone, writes the grants, starts the project's step
    containers as `build-workers` does (same names, labels and mounts) and registers them. It
    creates nothing in the project folder: a missing or non-folder project folder, `.git` or step
    folder is refused with exit 3, naming it. Running it again changes nothing; then `ap resume
    <run>` finds the containers running.
  - **Why it is safe under FR-025**: it touches only the folder of the interrupted run it names,
    which that run created as a new project, and it writes nothing there itself. Its containers
    mount `.git` read-only as always, and the project worker's history guard (D30) still checks
    `.git` against the worker's record, kept on the `project-worker-state` volume, which `ap down`
    keeps; a history changed while the platform was down stops the run with 409 HistoryChanged.
    It reads the run's record through `getRun`, not the project worker's record, so it adds no
    record-reading path.
  - **Replay**: `ap replay <run> <project>` reads the run with `getRun` for its slice, then runs
    `build-workers <slice> <project>` unchanged and passes the command on. The project must be new
    and empty by the ProjectExists rule (D31); an existing folder holding a file, a symbolic link or
    a non-empty `.git` is refused by `build-workers` (exit 3) as now.
- **Rationale**: keeping one container set per project means building a second project of a slice
  never stops the first one's containers, and the active-run refusal keeps the engine's single
  registration on the only run that can use it, since at most one run on the platform is active
  (FR-021, D38). Resume
  then needs only a registration, which needs no folder check, so the "folder already holds files"
  refusal of `build-workers` does not apply to it. Replay always writes a new project, so it fits
  `build-workers` as it is. No engine or contract change is needed.
- **Alternatives rejected**: a `build-workers --resume <run>` that reads the project worker's
  record through a one-off container to learn the run's project (more launcher code and a new path
  for reading that record; the adopted T053j reads the run with `getRun` instead); registrations
  per slice, step and project in the engine (a change to the engine, the grant files and
  registerWorker for a case the active-run refusal already covers); the project's full name in the
  container name (does not fit one DNS label for long project names).
- **Known limits**: step containers of every project built in the session use resources until
  `ap down`. Until T053j is done, an interrupted run whose containers are gone cannot be resumed
  (it can be cancelled, or it expires after 14 days). `build-workers` for another slice while a run
  of a different slice is active starts that slice's containers, which then wait: `build-workers`'
  active-run refusal is per slice, and the following `startRun` is refused with `RunActive` while
  any run of any slice has not ended (FR-021, D38).
- **Recorded as** [ADR 0036](../../docs/decisions/0036-step-containers-per-project-resume-reuses-them.md).
- Sources: owner's decisions, 2026-11-14; engine contract `registerWorker`, `listRuns`, `getRun`
  (`src/engine/api/preview/2026-11-12-preview/openapi.yaml`);
  <https://www.rfc-editor.org/rfc/rfc1035#section-2.3.4> (a label is at most 63 octets);
  <https://docs.docker.com/engine/network/#dns-services> (containers on a network resolve each
  other by container name).

### D33. The terminal client's run commands: following, exit codes and output

- **Decision** (T055 design, 2026-11-14, after the `/speckit-analyze` readiness check; the two
  owner's decisions are marked): the full contract is in
  [`src/cli/contract.md`](../../src/cli/contract.md). In short:
  - **Following**: `build`, `resume` and `replay` print the run ID line first, right after the
    202, then the whole record, one line per event in `seq` order, on standard output, then one
    end line, and return when the run is no longer `running`. They read `listRunEvents` with its
    page token, wait 100 ms doubling to 2 s between empty pages, and read `getRun` and then
    `getOperation` to end. `run <run>` prints the same lines, and follows a run that is still
    running. The launcher's own lines go to standard error.
  - **Ctrl-C detaches** (owner, 2026-11-14): the run continues; the client names `run <run>` and
    `cancel <run>` and exits 130 (128 + SIGINT, the shell convention); SIGTERM detaches the same
    way and exits 143 (owner, 2026-11-14). Following any other operation detaches the same way, so
    Story 1's "stopped following" changes from exit 1 to 130 or 143.
  - **Exit codes per run state**: `done` 0; `waiting-for-developer`, `interrupted` and
    `handed-over` 2; `expired` 5; `cancelled` 6; the run's operation `failed` 4 with its problem;
    the engine lost or unreadable 1. `interrupted` is 2 because the run is kept and goes on only
    when the developer acts, like a waiting run; the end line names the command. `take-over`
    that succeeds exits 0 (the developer's own request, nothing left waiting).
  - **`compare`**: 0 when the runs do not differ, **7** when they differ (owner, 2026-11-14; a new
    general exit code), a table of step, field and both values otherwise.
  - **Idempotency**: one random `Idempotency-Key` per invocation on its one mutating call; a
    lost answer (no answer, 502, 503, 504) is resent with the same key and body up to 3 more times
    with backoff and jitter (D7, D15), so a retry never starts a second run.
  - **Store text** from a model is printed with control characters escaped, so it cannot drive
    the terminal; `artifact` alone writes raw bytes.
  - **`ap test <suite>`**: suites `planted` and `us2`, a `PASS`/`FAIL` line per trial and per
    scenario (`stopped in 3 out of 3 trials`) and a total; 0 all pass, 1 one fails or the suite
    does not build, 3 usage.
- **Rationale**: one table maps every run state to one code, so a script can tell "done",
  "needs me" and "lost" apart without parsing text, and the end line tells a person what to type
  next. Retrying with the same key is how the contract makes a lost 202 safe (Principle X, D15).
  Printing every event in full, with full hashes and refs, lets `run` answer US2.9 in the terminal
  as the run page does in the browser (FR-023, FR-024).
- **Alternatives rejected**: a separate exit code for `interrupted` (a third "needs the developer"
  code that scripts would treat as 2 anyway); Ctrl-C cancelling the run (a run is long and costs
  money; cancelling is its own explicit command); printing only new events on `resume` (needs the
  last `seq` before the call; the whole record is the plainer rule); short hashes (US2.9 asks for
  every artifact, and a short hash cannot be passed to `artifact`).
- **`ap test` runs on the host** (owner, 2026-11-14, option (a) of two): the T046 suites start
  their own labs with the container engine (`src/tests/lab`), so they cannot run in a container
  without the container-engine socket. `ap test` builds the suite in the pinned Go container,
  offline, and runs its test binary on the host, as `scripts/check.sh` runs its `docker` suites.
  The binary uses the developer's own Docker access, which the constitution's test-security rule
  ("no raw container-engine socket") does not allow: it is an owner-approved exception, recorded
  in the plan's Exceptions to Engineering Principles (2026-11-14). No container ever holds the
  socket. No tests image is built, the suites stay as written, and `ap test` needs no started
  session, since the suites build the engine from source and use only `images.lock`'s pins. Each trial of
  `TestPlanted` becomes a subtest `TestPlanted/<scenario>/trial-<n>` so the per-trial lines can be
  read from its events. Rejected: (b) a tests image as a client of an engine, recorded model and
  workers the launcher starts through Compose (a second deployment of the platform for tests, and
  a rewrite of the suites).
- **Recorded as** [ADR 0037](../../docs/decisions/0037-terminal-run-commands-exit-codes-and-test-on-host.md).
- Sources: engine contract `listRunEvents`, `getRun`, `getOperation`, `compareRuns`,
  `getArtifact`, `acceptHandOver`, `Idempotency-Key`, `RunStatus`, `RunOutcome`
  (`src/engine/api/preview/2026-11-12-preview/openapi.yaml`); the T046 suites
  `src/tests/acceptance/us2_test.go`, `planted_test.go`, `buildrig_test.go`;
  <https://www.gnu.org/software/bash/manual/html_node/Exit-Status.html> (a command killed by
  signal N exits 128 + N);
  <https://pkg.go.dev/strconv#Quote>.

### D34. The lab runs the build path as the platform does, with step agents answering from synthesised recordings

- **Decision** (T054f design, 2026-11-14; the owner said go ahead and answered the three open
  questions on 2026-11-14, under "Owner's answers"):
  - **What the first `ap test planted` showed**: all five scenarios stopped at the readiness check
    for three causes: the rig looks for `src/check/cmd/check` (the command is
    `src/engine/cmd/check`, D29), no recording of a step agent exists, and the lab starts no check
    service, project worker or step worker (`startStepWorkers` is a placeholder). A fourth cause
    was hidden behind them: since T054c and T053e the engine refuses to start without
    `AP_CHECK_URL`, `AP_CHECK_TOKEN`, `AP_PROJECT_URL` and `AP_PROJECT_TOKEN`
    (`src/engine/cmd/engine/setup.go`), and `lab.Start` passes none, so every lab, the contract
    suites (T030) and Story 1's acceptance (T031) included, cannot start its engine today. That is
    a defect against agreed intent, fixed in the lab with a regression test (T054i).
  - **The lab, not Compose**: the lab keeps starting its containers with `docker run` on one
    internal network of its own, addressed by IP, except the step workers, which the engine (started without runsc, as in `compose.yaml`) reaches by container name, since worker addresses must be host names (T054j). It does not run `compose.yaml` or `ap
    build-workers`: Compose's networks have fixed subnets in `10.231.0.0/16`, so a lab would
    collide with the developer's running session and with another lab; `ap` needs signed images
    and a session; and the lab must put the recorded model where the gateway is. What it reuses is
    the declarations: the step workers are started from the same settings as the `step-agent`,
    `procedure-worker`, `check` and `project-worker` services and `x-sandboxed` (gVisor when the
    host has `runsc`, read-only root, all capabilities dropped, `no-new-privileges`, an executable
    `/tmp` tmpfs of 1 GiB, 2 CPUs, 2 GiB and 512 processes, the developer's uid:gid, the same
    environment and entrypoint), and the same mounts as `ap build-workers` (the project read-only,
    each step's writable folders writable, the whole root for generate, `.git` read-only on top,
    the review step wholly read-only, D30). A new test without Docker proves the agreement: it
    reads `compose.yaml` and the pattern and compares them with the lab's settings and mounts
    (constitution, "Prove agreement").
  - **Images**: the lab builds `Dockerfile.agent`, `Dockerfile.check` and `Dockerfile.project`
    with `docker buildx build --load` and the build arguments from `images.lock` that `build.sh`
    passes, once per test process, tagged `aptest-<name>:lab`, never pushed or signed, so the
    workers run the source under test as the engine does. The first build takes minutes
    (`Dockerfile.check` downloads the template's modules); later ones are cache hits.
  - **Containers a lab starts**: the existing client, replay server (`gateway`), analyst,
    contract-lint and engine; with the build option `lab.Build()`, also the check service and the
    project worker (each as its Compose service, the projects folder a host folder of the test,
    read-only for the check service and read-write for the project worker, the project worker's
    records on a lab volume). Without that option the engine gets addresses where nothing listens
    for the check service and the project worker, which Story 1 never calls, so the contract and
    Story 1 labs start no extra container. Per project, `l.StepWorkers(t, slice, project)` makes the
    project's skeleton (an empty `.git` and each step's writable folders, as `build-workers` does),
    starts one container per step of the pattern (three `serve api` step agents and the two
    procedure workers), writes their grants and registers them, and waits until each answers its
    health check. `l.StopStepWorkers` removes them once that project's run has ended. A planted
    trial therefore uses 7 lab containers and 5 step containers at a time, 12 at most.
  - **Grants, tokens and the engine's user**: the lab writes one grant file per step container
    into a host folder mounted read-only at `ENGINE_GRANTS_DIR`, named by the SHA-256 of the
    worker's token and holding the slice, step and call token, exactly as D31 says, and registers
    each container with `registerWorker` through its client container. Every token is a lab
    constant made for the test installation, never a session's or a provider's. The lab's engine
    runs as the developer's uid:gid, as the project worker and the step containers do in a real
    session, so the host-written grant files (mode 600) and store need no ownership helper; the
    engine does not depend on its uid.
  - **The durable `/store`**: the engine's store is a host folder of the test bind-mounted at
    `/store`, not a tmpfs, because Docker empties a tmpfs mount when its container stops, so
    US2.10's `docker kill` and `docker start` would lose every run. `l.RestartEngine(t)` kills and
    starts the engine, reads its address again (Docker may give it another), waits until it
    answers, and registers every running step container again, as `ap resume` does (D32), since
    registrations are held in memory.
  - **Recordings: synthesised, not recorded from the provider**: a step agent is a session of
    several model turns, and each turn's request holds every earlier tool result. The live agents
    run `go build` and `go test`, whose output carries elapsed times, so a live session's later
    requests differ on every run and an exact recording (D26) would miss. A live recording is
    therefore not replayable, whatever it costs, and planted failures cannot be had from the live
    model on demand anyway. Every step recording is synthesised, at no model cost: no Haiku
    recording session is needed for T046. The engine's message names the run, the step, the
    attempt, the round and the project folder (`steps.worker.message`), so each run and project
    needs its own exchanges, and a trial's exchanges are exact for that trial alone.
  - **How they are made**: a new command `src/tests/recorded/scripted` serves the gateway's
    `POST /v1/messages` from **answer scripts**, the reviewed source of every step recording.
    A script `src/tests/recorded/steps/<set>/answers.json` lists, per step, attempt and round
    (or any attempt), the turns of the answer: files to write (`write_file` tool calls, each path
    relative to the project, prefixed with the folder the request's message names, each file's
    content from a file beside the script), a structured-output answer (the review agent's
    findings), or a final text, with an optional delay. The server picks the answer from the
    first line of the message (`Run <run>, step <step>, attempt <n>[, review round <r>]`) and the
    turn from the number of assistant messages in the request, answers with an Anthropic Messages
    event stream, and writes each exchange as a WireMock snapshot that the existing `recording
    write` turns into `mappings/<name>.json` and `__files/<name>.sse`, named
    `<run>-<step>-a<attempt>[-r<round>]-t<turn>`. A request no script answers gets 404 and is
    listed, as a miss is. `src/tests/recorded/synth-steps.sh <test pattern>` runs the acceptance
    suite with the lab's model set to `scripted` (`AP_LAB_MODEL=scripted`) instead of the replay
    server, and writes each lab's exchanges into the last recording set it names. A changed agent
    definition, instruction, engine message or Docker Agent version makes every recording of that
    agent miss, as D26 intends; running the generator again makes them anew, free. The
    acceptance run itself always replays through WireMock with exact matching; it never uses the
    scripted server, and `l.Misses` is 0 at the end of each lab.
  - **Recording sets**: `steps/happy` (the example slice answered correctly, for `TestUS2`),
    `steps/resume` (the same answers for US2.10, with the hold below) and
    `steps/planted/<scenario>` (each holding its own correct tests-step answer and its planted
    answers, for all three trials). The file contents of correct answers start from the
    step evals' reference outputs (`src/tests/evals/steps/testdata/ref`), checked against the
    lab's example slice. In trials 2 and 3 the earlier steps are reused (FR-019, T053e), so only
    the failing step's agent is asked again. About 115 exchanges, some 3 MB of mappings.
  - **The planted failures**: each is one answer of the step agent, repeated in each of its three
    attempts: a code answer that imports `os/exec` (DependenciesAllowed); one that writes nothing
    and says it is done (AllTestsPass); for US2.7, an implementation that builds but fails one
    example (AllTestsPass); one that rewrites a file under `tests/` (TestsUnchanged) and one that
    writes `README.md` (ScopeKept), each with the test-only mount under "Owner's answers". The
    non-converging review waits for its requirement to be fixed upstream.
  - **US2.10 at a fixed point**: US2.10's own set `steps/resume` (the engine numbers runs
    `RUN-<n>` per lab, so one set serves one lab) has the code answer hold its first turn for
    30 s (WireMock's `fixedDelayMilliseconds`, which `recording write` must keep), and the test kills the engine once the step model has received that request
    (`l.WaitModelRequest`: the replay server's request journal, or in scripted mode the snapshot
    the scripted server writes before it holds the answer), so the kill always falls inside the same model call and both the
    generating run and the replay make the same requests.
  - **Cleanup**: every container, network and volume a lab makes is named
    `aptest-lab-<what>-<pid>-<n>` and labelled `org.agentic-platform.lab=<pid>`, and removed by
    `t.Cleanup`. A test binary that times out panics without running its cleanups, so `lab.Start`
    first removes every lab resource whose process is no longer running. The host folders
    (projects, store, grants) are the test's temporary folders, written only by the developer's
    uid, so the test removes them.
  - **Time limits**: `ap test` runs the test binary directly, whose `-test.timeout` is 0 (none),
    so it gains a cap per suite: 60 minutes for `planted` and 75 for `us2`. Inside, a lab waits
    at most 3 minutes for its containers (plus up to 10 minutes for a cold image build), a planted
    trial at most 10 minutes (the suites' `waitRun` limit), and each step at most its budget (10
    minutes, D14). Expected, to be measured in T054i and T054l (setup, test and teardown apart):
    a lab starts in about 1 minute; a scenario's first trial takes about 4 minutes, its second and
    third about 1.5 minutes each; `ap test planted` about 35 to 40 minutes; `ap test us2` about 20
    to 25 minutes, without the five one-trial copies of the planted scenarios (owner, below).
  - **Host resources**: scenarios and trials run one after another, never in parallel; at most 12
    containers of a lab at a time plus a short-lived one-off; each step container is capped at 2
    CPUs and 2 GiB as in Compose, and only one check runs at a time.
- **Owner's answers** (2026-11-14, to the questions of the T054f report):
  - **US2.2 and US2.3, a test-only mount**: the code step's container mounts only `internal/`
    and `cmd/` writable (D5, D30), so a recorded answer that rewrites `tests/` or writes
    `README.md` would fail at the tool before any check saw it. For these two scenarios only, the
    lab mounts the code step's project root writable, `.git` still read-only, standing in for a
    sandbox that let the write through, so TestsUnchanged and ScopeKept are what stop the run.
    This is a test-only deviation that proves the second guard; the production mounts
    (`compose.yaml`, `ap build-workers`) are unchanged, and the lab's agreement test still
    compares every other scenario's mounts with them.
  - **US2.6, a requirement and design gap**: the code step is accepted only when AllTestsPass
    passes, and a review finding blocks only when its rule's tests, rerun, fail at its file
    (ADR 0031), so after an accepted code step no finding can be confirmed and no review round
    can fail. The gap is in FR-016 and ADR 0031 and is fixed upstream through `/speckit-clarify`,
    which the coordinating session runs. The planted non-converging-review scenario waits until
    then; no recording is made for it, and no contrived test stands in for it.
  - **`ap test us2` drops its five one-trial copies** of the planted scenarios
    (`TestUS2_2` to `TestUS2_6`), which replayed exactly `TestPlanted`'s first trials; US2.2 to
    US2.6 are judged by `TestPlanted`. US2.7 (`attempts-exhausted`) stays in `us2`.
- **Rationale**: the lab is the platform's build path with the model replaced and nothing else:
  the same images from source, the same worker settings proved equal to `compose.yaml`, the same
  grants, registrations and per-project containers, so a T046 pass says what a session would do.
  The test-only mount keeps the second guard under test without widening
  production. Synthesising is the only way to get exact, repeatable model exchanges for agents that use
  tools, and an answer script is a small reviewable file where a planted failure is visible.
  Generating the mappings in one run of the suite, instead of one lab run per miss, keeps the
  cost of a changed prompt at one free generator run.
- **Alternatives rejected**: running `compose.yaml` or `ap build-workers` from the lab (fixed
  subnets collide with a running session and with other labs, and it needs signed images and a
  session); recording the step agents live with the Haiku test key (not replayable: tool results
  carry elapsed times; it would cost about $0.04 to $0.12 per run by the pattern's estimate, so a
  few dollars for the suites, but buys nothing); WireMock matching that ignores the run, attempt
  or project in the body, or answering through WireMock's response templates (a stale answer
  would match, against D26; the event stream would be a template of escaped JSON); finding each
  next request from misses, one lab run per turn (about 115 lab runs); one lab per trial so that
  trials repeat each other's requests (about 10 more lab starts, and the trials lose the reuse
  of earlier steps); the images `build.sh` signed (they may be older than the source under test);
  a named volume for `/store` (needs an ownership helper, and the host folder is removed with the
  test).
- Sources: `src/engine/cmd/engine/setup.go` (required variables, read 2026-11-14);
  `src/engine/steps/steps.go` (`message`); `src/deploy/compose.yaml` and `src/deploy/ap`
  (`build_workers`, `register_worker`, `cmd_test`); `src/tests/lab/lab.go`;
  `src/tests/recorded/` (README, `record.sh`, `recording/main.go`); the T046 suites;
  <https://docs.docker.com/engine/storage/tmpfs/> (a tmpfs mount is removed when its container
  stops); <https://wiremock.org/docs/simulating-faults/> (`fixedDelayMilliseconds`);
  <https://pkg.go.dev/cmd/go#hdr-Testing_flags> (`go test` passes a 10-minute timeout; the
  binary's own `-test.timeout` defaults to none);
  <https://docs.anthropic.com/en/docs/build-with-claude/streaming> (the Messages event stream).

### D35. A blocking review finding carries a new failing witness test in the review's own folder

- **Decision** (owner, 2026-11-14, `/speckit-clarify`, spec Clarifications Session 2026-11-14,
  FR-017 and US2.6; the design below is T074 to T077):
  - **Why**: after D34 found it, the owner confirmed the gap: the code step is accepted only when
    AllTestsPass passes, and ADR 0031 confirmed a finding only by rerunning the same rule tests, so
    no finding could block and no review round could fail with deterministic tests.
  - **The witness folder is `review/**`**, a top-level folder of the project, one folder per rule:
    `review/r<n>/` for rule `R-<n>`. Not `tests/review/**`: every later step's checks recompute
    the framed hash of the tests step's output `tests` (every file under `tests/**`) from the
    folder (D29), so a witness under `tests/` would make that hash differ and fail every later
    step with InputHashMismatch. `go test ./...` finds `review/r<n>/` as a package (a folder name
    not starting with `.` or `_` and not `testdata`); godog runs only `features/slice.feature`
    from `tests/`, so witnesses are plain Go tests and add no scenario to EveryExampleBound.
  - **The review step's pattern entry**: `writable: [review/**]`, `outputs: [findings,
    witnesses]` (`findings` from the agent's answer, `witnesses` the files under `review/**` that
    readStepFiles returns; D29's "an agent step has exactly one output" becomes "one file output",
    the review's `findings` being its non-file output), `dependencies` as the tests step's minus
    godog (std, the oapi-codegen runtime; the project's own packages are always allowed), and the
    checks ScopeKept, DependenciesAllowed, TestsBuild, TestsUnchanged, FindingsCarryEvidence and
    NoBlockingFindingOpen. `ap build-workers` then mounts `review/` writable in the review
    container (it is no longer wholly read-only) and makes the folder in the skeleton; every other
    step's container sees it read-only, so the fix step cannot write it, and ScopeKept judges it.
  - **A witness**: a new Go test file in `review/r<n>/` (a test-only package), declaring a test
    function whose name starts with `TestR<n>` not followed by a digit, written by the review in
    the round that raises the finding, absent from `before`. A blocking finding names it as
    `witness: {file, test}` (engine run-event schema `finding.witness`, check contract `Finding`;
    the review agent's findings schema requires it when `blocking` is true).
  - **Confirmation** (replaces ADR 0031's rule-test rerun; amended after `/speckit-analyze` W1,
    2026-11-14): FindingsCarryEvidence and NoBlockingFindingOpen run only the witness test,
    `go test -json -count=1 -covermode=set -coverpkg=./... -coverprofile=<outside the project>
    -run '^<test>$' ./review/r<n>/`, offline, against the code as it stands. The finding is
    confirmed only when that test ends `fail` and the coverage profile of that run has a block of
    the finding's file covering its line with a count above 0, and a baseline run of the same
    package with `-run '^$'` does not cover it: the witness failed, having run the finding's
    line. The baseline removes what package initialisation runs before any test (`init`,
    package-level initialisers, `TestMain`; analyze round 2), so a witness that always fails
    cannot claim an init-time line. Printed text is never evidence, since a witness could `t.Fatal("file:line")`;
    the coverage profile is written by the go tool, not by the test. A witness that only prints
    the location, or fails before it runs that line (an unconditional failure), confirms nothing,
    so its finding is a suggestion. A blocking finding whose file no `writable` entry of the fix
    step (the code step) matches is a suggestion, since the fix could not change it (analyze W2).
    A blocking finding without a valid new witness, or whose witness passes, is a suggestion. A
    witness that does not build fails FindingsCarryEvidence (it cannot be judged).
  - **Unchangeable after its round**: TestsUnchanged also covers the witnesses in `before`: no
    step may change or remove one, the review step of a later round included (it may only add new
    ones), and no other step may add one. The fix step (the code step run again) must make the
    witness pass without changing it, which its AllTestsPass judges, since `go test ./...` runs
    `review/`. Witnesses stay in the delivered project and its commits. Generate runs before
    the review with the project root writable (D30); if it wrote under `review/`, ScopeKept stops
    it (no glob of its `writable` matches), not TestsUnchanged, which covers witnesses only once
    the review has written them (analyze W4).
- **Rationale**: a new test that fails having run the named line, as the go tool's coverage profile shows, is evidence a deterministic check
  produces from the current code, while the derived tests have already passed; keeping it
  unchangeable stops the fix step from passing by weakening it, as TestsUnchanged does for the
  derived tests.
- **Alternatives rejected**: `tests/review/**` (breaks the `tests` output's hash, above); a
  witness only for the round, removed afterwards (the owner chose to keep them, FR-017); the
  reviewer's word or other check verdicts as confirmation (ADR 0031's reasons).
- **Known limits**: a witness can test something other than the rule it names; the check proves
  only that it is new, is in that rule's folder, and failed having run the named line. A witness
  that runs the line and then fails whatever the code does cannot be told apart from a real one;
  the fix step then cannot make it pass, its attempts fail AllTestsPass, and the run is handed over
  with that reason, so a false witness costs a hand-over, never an accepted wrong result. In
  `set` mode the profile marks a whole block once it is entered, so a line in a block the witness
  entered counts as run even when the block stopped (a panic or an early exit) before it. The review agent's
  instructions and evals change, and the live step evals must pass again (T041, about $1 to $2
  with Haiku on the test key).
- **Recorded as** [ADR 0039](../../docs/decisions/0039-review-findings-confirmed-by-a-new-witness-test.md),
  superseding the confirmation part of ADR 0031.
- Sources: spec.md (Clarifications Session 2026-11-14, FR-017, US2.6); check contract
  `src/check/api/preview/2026-11-13-preview/openapi.yaml` (runCheck, EarlierStep, Finding);
  `run-event.schema.json` (`finding`); <https://pkg.go.dev/cmd/go#hdr-Testing_flags> (`-covermode`, `-coverpkg`,
  `-coverprofile`); <https://go.dev/blog/cover> (the profile's blocks, lines and counts);
  <https://pkg.go.dev/cmd/go#hdr-Package_lists_and_patterns>
  (`./...` ignores folders starting with `.` or `_` and `testdata`).

### D36. An open review round's result is kept and reused like an accepted step's

- **Decision** (owner, 2026-11-15; found by T053a's end-to-end run; the design is T089 and T088,
  data-model "An open round reused"):
  - **Why**: in RUN-1 review round 1 raised a blocking finding, the fix was accepted and round 2
    was accepted. The replay RUN-2 reused contract-check, generate, tests and code, but ran round
    1, the fix and round 2 live, and `compare` differed (a witness came out as
    `review/r2/long_text_test.go` instead of `large_text_test.go`). `keepOpenRound` in
    `src/engine/machine/drive.go` kept no result for an open round, so the replay ran it live,
    and its new finding text changed the fix's key, which includes the hash of the open findings;
    the fix and the later rounds then missed reuse too. So US2.8, SC-005 and FR-019 failed for
    every run with more than one review round.
  - **Kept**: the open round's `StepRejected` carries `open_round: true`, `reuse_key` and
    `artifacts`; the accepted-result store keeps, under the round's key, its artifacts, run,
    project files, findings in recorded order, NoBlockingFindingOpen result and the mark open; a
    restarted engine rebuilds it from the record. The review step's key also covers its round
    (owner, 2026-11-15).
  - **Recorded on reuse**: the kept findings as `FindingRecorded` events (`round`, no `attempt`),
    then `ResultReused` with `round`, `open_round: true`, `check` (the kept NoBlockingFindingOpen
    result) and `detail`. Not accepted: the run's artifacts and accepted steps are unchanged, as
    after a live open round.
  - **The fix**: its open findings are the blocking ones among the kept findings, in order, so its
    key, and then the next round's, match the original's.
  - **Commit**: `commitStep` with `round`, `open_round: true`, `reused_from` and `outputs`
    (`witnesses`); subject `Step <step> round <round> left open in <run>`, trailer `Reused-from`.
  - **Resume** (owner, 2026-11-15): after the event that left a round open, the commit is made again, then the fix
    (or, at the last round, the hand-over) follows; the round is not run again.
  - **Checks** (owner, 2026-11-15): reuse runs no check, as for a reused accepted step today; the
    witnesses are not re-confirmed: they were confirmed in the original run, whose record holds
    those checks.
- **Rationale**: FR-019 and SC-005: a replay is identical only if every result it depends on is
  reused, and a fix depends on the round's findings. The key binds a kept result to the same
  inputs, agent, model and round, so rerunning its checks would judge the same files again. An
  open round reused only leads to more work (the fix, a later round); it never makes a run
  accepted. The round is in the review key because a result is judged against that round's
  `before`: a later round with the same inputs and context must not get blocking findings whose
  witnesses are already there.
- **Alternatives rejected**: a reused open round recorded as `StepRejected` (it says an attempt
  was rejected, and none ran); a new event kind such as `RoundReused` (D23's list and every
  reader, `load`, `compare`, the terminal client, would gain a kind for what `ResultReused`
  already means: a kept result used again); rerunning FindingsCarryEvidence and
  NoBlockingFindingOpen on reuse (no reused step's checks run again, D8; the same key gives the
  same files); keeping the findings only in the `findings` artifact (the machine reads no stored
  artifact; the record already holds them as `FindingRecorded` events); leaving the round out of
  the key (above).
- **Recorded as** [ADR 0040](../../docs/decisions/0040-open-review-round-result-kept-and-reused.md).
- Sources: T053a's end-to-end run through `ap`, 2026-11-15 (tasks.md T053a); `src/engine/machine/drive.go`
  (`keepOpenRound`, `stepKey`), `src/engine/machine/record.go` (`load`), `src/engine/api/runs.go`
  (`reloadResults`); `run-event.schema.json`; check contract
  `src/check/api/preview/2026-11-13-preview/openapi.yaml` (commitStep, CommitRequest).

### D37. A test check passes only on a result event for every declared test, in a fresh folder per call

- **Decision** (owner, 2026-11-15; found by T057's review, R1 and R2; the design is T092, T093
  and T094):
  - **Why (R1)**: AllTestsPass passed on exit code 0 and one `ok` line (`testsRan` in
    `src/engine/checks/steps.go`). A probe whose application `init()` calls `os.Exit(0)` when
    `os.Args[0]` ends in `.test` passed with a failing test: the test binary exits 0 before any
    test runs, and `go test` still prints `ok`.
  - **Declared tests**: the top-level test functions of the project's `_test.go` files under
    `tests/**` and `review/**`, read with `go/parser` (a function `TestXxx(t *testing.T)` whose
    name is a test name by the `go test` rule: `Test` followed by nothing or by a character that is
    not a lower-case letter), each with its package's import path in the module. Subtests are not
    declared tests. Other `_test.go` files (the code step's own) are run but declare nothing.
  - **AllTestsPass**: `go test -json -count=1 ./...`; it passes only when the command exits 0, no
    package fails to build, and every declared test has a `pass` event for its package and name.
    A declared test with no event (it vanished), with `fail`, or with `skip` fails the check,
    naming the test. No declared test at all fails it, as "ran no test" does today.
  - **TestsFailAgainstStub** (owner, 2026-11-15): `go test -json -count=1 ./tests/...`; it passes
    only when the tests build, every declared test under `tests/**` has a result event (`pass`,
    `fail` or `skip`), so none vanishes, and at least one has `fail`.
  - **A result event** is a `pass`, `fail` or `skip` event for the test's package and name, in
    every rule here that asks for one (TestsFailAgainstStub, EveryExampleBound); AllTestsPass
    alone needs `pass` (owner's recommendation taken, 2026-11-15, `/speckit-analyze` L2).
  - **EveryExampleBound** keeps its scenario rule; its fallback (`testsRan`) becomes the same
    event rule: every declared test under `tests/**` has a result event.
  - **Witness runs** already use `-json -count=1` and read the witness's own `fail` event
    (D35). The baseline `-run '^$'` has no test event by design: it needs only the package's end
    event and its coverage profile, both written with `-json`, so the baseline rule holds. A
    witness run whose binary exits before its test ran has no result event for that test, so
    FindingsCarryEvidence fails closed (a binary that exits in `init` still leaves a profile
    holding only its `mode:` line, so the profile alone does not show it).
  - **Why (R2)**: the check service lives as long as the session and every call shared
    `GOCACHE=/tmp/go-cache` on its `/tmp` tmpfs (`Dockerfile.check`, `compose.yaml`), without
    `-count=1`, so a result cached from one attempt or project could answer another.
  - **A fresh folder per call** (owner, 2026-11-15): each call of the check program (runCheck, with
    its witness runs, and runProcedure) gets a new temporary folder under `/tmp`, whose `GOCACHE` starts as a copy of the read-only warm base cache (the standard library) and whose rest is empty, removed
    when the call ends, whatever its outcome. Every command of the call has `GOCACHE`,
    `GOTMPDIR`, `TMPDIR`, `HOME` and `GOPATH` inside it, so nothing a check writes there survives
    the call; every `go test` carries `-count=1`. The module cache of the pinned modules,
    `/opt/gomodcache`, is shared read-only: it is in the image, owned by root, on the read-only
    root file system. `Dockerfile.check` drops its `GOCACHE` and `GOPATH` settings (the call sets
    them); `compose.yaml` keeps the `/tmp` tmpfs, which holds the call folders; each call's `GOCACHE`
    starts as a copy of the image's read-only warm base cache (see Cost).
- **Rationale**: FR-017, constitution "Checks prove they can fail": a check must judge the
  tests it names, not a summary line the code under test can print or suppress. `go test -json`
  (test2json) gives one event per test with its action, so a test binary that ends before its
  tests run leaves no event for them. A per-call cache removes any path from one call's build or test results to a later call's; calls running at the same time are a stated limit (Known limit below).
- **Cost**: with an empty cache each call compiles the standard library and the project from
  scratch: T094 measured one AllTestsPass call on the lab's example project at 0.6 s with the old
  shared cache and about 13 s with an empty one, too slow for the planted and us2 suites (caps 60
  and 75 minutes). So (owner, 2026-11-15) the image holds a **warm base cache**: `Dockerfile.check`
  builds, from the pinned sources only, the standard library packages the pattern's tests and
  vendored tools use into `/opt/gocache-base` (the vendored modules themselves cannot be shared:
  without `-trimpath` their cache keys hold the project's own folder, so they compile per call;
  T094 measured about 3 s per call with the base), owned by root on the read-only root file system; each call
  copies it into its own folder as its `GOCACHE` and discards the copy with the folder. Nothing a
  check writes reaches a later call, and the base holds only what the image build compiled from
  pinned sources. T094 measured one call; its `ap test planted` run found T100, so both suites are timed against the caps with T100's run.
- **Known limit** (T094's review, 2026-11-15): calls that run at the same time share one user and
  one `/tmp`, so a test in one call can reach a concurrent call's folder and write into its cache;
  this falls under the deliberate-forgery limit below, and hardening (a user or a mount namespace
  per call) is a follow-up (T097).
- **Known limit** (T093's review, 2026-11-15): the events are what test2json makes of the test
  binary's output, so code in the binary can still print lines that become events, or write to the
  go command's own output, and forge a result. Deliberate forgery is not stopped; it adds nothing
  to what code that detects its test binary can already do (answer the tests and behave otherwise
  in production), which no test check can stop. Hardening is a follow-up (T097).
- **Alternatives rejected**: every declared test under `tests/**` failing against the stub (a
  test that rightly passes against the stub would fail the check; owner, 2026-11-15); counting
  `ok` lines or `--- PASS` lines (the test prints them, or
  `init` stops before them); `-count=1` alone (the cache still holds build results other calls
  wrote); one cache per project (still shared across attempts and the long-lived service).
- **Known limits**: a test that writes to an absolute path elsewhere under `/tmp`, outside its
  call's folder, leaves the file for later calls of the service (the tmpfs is shared); the
  variables close the paths the go tool and the tests are given, not every path. A test that
  declares itself through `TestMain` alone is not a declared test. If compiling from scratch
  proves too slow, the owner decides then (owner, 2026-11-15).
- Sources: T057 review findings R1 and R2, 2026-11-15; `src/engine/checks/steps.go`
  (`testsRan`, `AllTestsPass`, `TestsFailAgainstStub`), `src/engine/checks/witness.go`
  (`readTestEvents`); `src/deploy/Dockerfile.check`, `src/deploy/compose.yaml`;
  <https://pkg.go.dev/cmd/test2json> (the event actions); <https://pkg.go.dev/cmd/go#hdr-Testing_flags>
  (`-json`, `-count`); <https://pkg.go.dev/cmd/go#hdr-Build_and_test_caching> (`GOCACHE`, cached
  test results); <https://pkg.go.dev/testing> (test function names).
- **Recorded as** [ADR 0041](../../docs/decisions/0041-test-checks-need-an-event-per-declared-test-in-a-fresh-folder-per-call.md).

### D38. One active run on the platform, not one per slice

- **Decision** (owner, 2026-11-15; found by T057's review, R3; the design is T091 and T095):
  startRun, resumeRun and replayRun are refused with `RunActive` while any run of any slice has
  not ended (running, interrupted or waiting for the developer), naming that run and its slice.
  The slice-editing operations keep their per-slice refusal (a slice's parts cannot change while
  its own run is active), and so does `ap build-workers` (it starts containers, not a run; the
  `startRun` after it is refused).
  - **Why**: `active()` in `src/engine/api/runs.go` looked only at the slice's own runs, so two
    slices could have active runs at once, against FR-021 ("at most one active run at a time")
    and the limit D32 states.
  - **`steps.state.remember`**: the engine keeps one step worker, and so one `state`, per slice
    (`sliceWorkers` in `src/engine/cmd/engine/setup.go`), and a project gets one run, so its
    "runs never overlap" assumption held even per slice; with one active run on the platform it
    holds for the whole engine. Only its comment changes, to cite FR-021.
- **Rationale**: FR-021 and the local environment's "one active run"; the per-slice machines
  stay, the check reads every slice's runs.
- **Alternatives rejected**: changing FR-021 to one run per slice (the owner decided the defect is
  the code).
- **Recorded in** ADR 0036 and D32, which T095 updates to the platform-wide rule; no ADR of its
  own (owner, 2026-11-15).
- Sources: T057 review finding R3, 2026-11-15; spec FR-021; D32, Known limits; engine contract
  startRun (`src/engine/api/preview/2026-11-12-preview/openapi.yaml`); `src/engine/api/runs.go`
  (`active`), `src/engine/api/slices.go` (`activeRun`), `src/engine/steps/steps.go` (`remember`).

### D39. Only a finding the check confirmed is recorded as blocking

- **Decision** (owner, 2026-11-15; found by T057's review, R5; the design is T092 and T096):
  - **Why**: the machine built the open list from the agent's own `blocking: true`
    (`src/engine/machine/drive.go`, the review loop), so a claim no witness confirmed was given to
    the fix, entered the next round's key and appeared in the hand-over, against FR-017.
  - **How the engine learns the confirmed set**: NoBlockingFindingOpen's verdict gains
    `confirmed`, the 0-based positions in the request's `findings` of the findings whose witness
    run confirmed them (check contract change, T092). It is sent on every NoBlockingFindingOpen
    verdict, empty when it passes. The step adapter maps the positions back to the attempt's
    findings (it drops some before the call, `projectFindings`), and the machine's `CheckResult`
    carries them.
  - **Recorded**: after a review attempt's checks, each finding is recorded with `blocking` true
    exactly when NoBlockingFindingOpen confirmed it; every other finding, the agent's unconfirmed
    claims included, is recorded with `blocking: false`, a suggestion. A round is open only when
    NoBlockingFindingOpen is the only failed check and it confirmed at least one finding;
    otherwise the attempt is rejected as now.
  - **Used**: only recorded blocking findings, so only confirmed ones, are given to the fix,
    enter the next round's key (`contextHash`), and appear in the hand-over reason, `Run.Open`
    and the terminal client's `FindingRecorded` line.
  - **With D36**: the kept findings of an open round are the recorded ones, so their `blocking`
    is the verdict, and a reused round gives its fix the same confirmed list. The `findings`
    artifact stays the agent's answer as given (its claims); the record holds the verdict.
- **Rationale**: FR-017 says a finding blocks only with evidence a check confirms; the check
  service is the one that runs the witnesses, so its verdict, not a second judgement in the
  engine, says which. Positions are exact where matching findings by their fields could confuse
  two equal claims.
- **Alternatives rejected**: the engine reading the confirmed set from the verdict's `reason` text
  (free text, not a contract); returning the confirmed findings as objects (two equal findings
  cannot be told apart); the engine running the witnesses itself (the check service owns them).
- **Known limits**: results kept before T096 hold the agent's claims as `blocking`; a replay of
  such a run reproduces them.
- **Recorded as** [ADR 0042](../../docs/decisions/0042-only-a-confirmed-finding-is-recorded-as-blocking.md),
  amending what [ADR 0039](../../docs/decisions/0039-review-findings-confirmed-by-a-new-witness-test.md)
  means by "blocking" in the record.
- Sources: T057 review finding R5, 2026-11-15; spec FR-017; D35, D36; check contract
  `CheckVerdict` and runCheck (NoBlockingFindingOpen); `src/engine/machine/drive.go`,
  `src/engine/steps/steps.go` (`projectFindings`, the verdict).

### D40. Module boundaries are checked by go-arch-lint, one archfile per module

- **Decision** (owner, 2026-11-15; the work is T098, and T099 for generated projects):
  `fe3dback/go-arch-lint` v1.19.1 checks the imports of each Go module in `src/go.work` against
  a `src/<module>/.go-arch-lint.yml` (archfile schema version 3). The archfile declares the
  module's packages as components and, for each one, the components (`mayDependOn`) and vendor
  packages (`canUse`) it may import. Every other import fails, and so does a package that no
  component maps. This is how the platform applies Principle XVII's "reject a change that … adds a
  dependency that crosses a boundary the project's design forbids" to its own code. Layering
  inside generated projects is a later pattern-level check (T099, not blocking the MVP).
- **Facts** (checked 2026-11-15; [go-arch-lint reference](../../docs/references/go-arch-lint.md)):
  - Release: v1.19.1 of 2026-10-29, commit `a34dbfc8`, module hash
    `h1:8cGotGwAxpuKpOw9H8hhA6+EN2+HGU1eHcrwMvL4dC4=`. Its `go.mod` says `go 1.25.0`, with no
    `replace`.
  - Project: MIT licence, Scorecard 4.7 (2026-10-19; Maintained 8, Pinned-Dependencies and
    Signed-Releases 0). Releases every one to two months in 2026, one main maintainer merging
    outside contributions. No open issue names Go 1.26, and the binary builds and runs under the
    pinned Go 1.26.11.
  - Scope: one module per run (`--project-path` needs a `go.mod`; a `go.work` is not read).
    Imports of the other workspace modules (for example `github.com/agentic-platform/slice` from
    the engine) are vendor imports. The standard library is always allowed.
  - What it reads: every `.go` file's import block, `_test.go` files and files behind build tags
    included. So it sees the `evals`-tagged imports that a plain `go list` misses.
  - Exit codes: 0 when clean, 1 for anything else (a violation, an unmatched file, an archfile
    notice or a setup error). `--json` gives the warnings as data.
- **Install pin and offline build**: `images.lock` gains
  `go-arch-lint github.com/fe3dback/go-arch-lint@v1.19.1` and `go-arch-lint-sum
  h1:8cGotGwAxpuKpOw9H8hhA6+EN2+HGU1eHcrwMvL4dC4=`. The tool is built from source in the pinned
  `golang` image into `/gopath/bin`, as oapi-codegen, govulncheck and go-licenses are. Those three
  run `go install` with network each time; this one runs offline as soon as the cache holds it.
  The experiment, on this host:
  - `go install …@v1.19.1` with network: about 35 s; about 250 MB of module cache and 265 MB of
    build cache.
  - The same with `--network none` and `GOPROXY=off` fails: "loading deprecation for
    github.com/fe3dback/go-arch-lint: module lookup disabled by GOPROXY=off".
  - With `--network none` and `GOPROXY=file:///gopath/pkg/mod/cache/download`: succeeds in under
    2 s.

  So `scripts/check.sh` installs it offline through that file proxy. Only when the module is not
  cached does it fetch it once with network, where the checksum database verifies it. It then
  refuses a binary whose `go version -m` module line is not the pinned version and hash. The
  owner accepted this (owner, 2026-11-15): the first fetch uses the network in the arch step's
  setup, and every later run is offline through `GOPROXY=file://…`.
- **Offline running**: `check` runs with `--network none`, `GOPROXY=off`,
  `GOFLAGS=-mod=readonly` and `src/` mounted read-only. It needs only `go` on `PATH`, for `go list
  std`. The archfile is always a local path, because a URL would be fetched with `http.Get`. With
  deep scan off it takes 1 to 2 s per module.
- **Derived dependency graph** (2026-11-15, from every `.go` file's imports outside `testdata`, as
  the tool reads them, cross-checked with `go list -tags docker` for Imports, TestImports and
  XTestImports; own-package `_test` imports left out):
  - engine (`github.com/agentic-platform/engine`), project edges:
    - `canonical`, `agentcall`, `machine`: none (leaves).
    - `checks` → canonical.
    - `procedures` → canonical, checks.
    - `checksvc` → canonical, checks, procedures.
    - `slices` → canonical, checks.
    - `contracts` → slices.
    - `analyst` → agentcall, slices.
    - `steps` → agentcall, canonical, checksvc, machine, procedures.
    - `store` → none.
    - `api` → api/gen, machine, slices, store.
    - `cmd/engine` → analyst, api, canonical, contracts, machine, slices, steps, store.
    - `cmd/check` → checksvc.
  - engine, vendor edges: `slice` module (store, slices, api, cmd/engine); `check/api/gen`
    (checksvc, steps); yaml (checks, checksvc, slices, api, cmd/engine); jsonschema (checksvc,
    slices, api; machine in tests); gherkin and messages (checks); `golang.org/x/mod/modfile`
    (checks, procedures); kin-openapi and oasdiff (contracts); oapi-codegen runtime (api/gen).
  - cli and page: root package → api/gen; `cmd/<name>` → root package; api/gen → oapi-codegen
    runtime. check: only api/gen → oapi-codegen runtime. slice: one package → yaml, jsonschema,
    gherkin, messages.
  - tests: acceptance → lab; contract → lab (and yaml, jsonschema); analystsvc → evals/analyst
    and engine/slices; analystsvc/client → engine/analyst, engine/slices; evals/analyst →
    engine/checks, engine/slices, slice, gherkin, yaml; evals/steps → engine/canonical,
    engine/checks, engine/procedures, yaml; lab → yaml. lab/httpc, lint, recorded,
    recorded/recording and recorded/scripted import no project or vendor package.
- **Rules** (the archfiles T098 writes; each component's allowed set is exactly its edges above,
  plus itself where its external tests import it):
  - Every package of every module is one component with one exact folder (no `**`). A new
    package fails until its archfile declares it.
  - `allow.depOnAnyVendor: false`: every vendor package is declared and listed per component.
  - `excludeFiles: ["^.*/testdata/.*$"]`: the fixture modules are other projects.
  - `allow.deepScan: false` (owner, 2026-11-15: deep scan stays off).
  - These rules encode the intended direction, so these imports fail:
    - the leaves (`canonical`, `agentcall`, `machine`) import nothing of the engine;
    - `machine` never imports `api`, `steps`, `checks`, `checksvc` or `store`;
    - `checks` imports only `canonical`, never `api`, `machine`, `steps`, `checksvc`,
      `procedures` or `slices`;
    - `procedures` and `checksvc` never import `api`, `machine`, `slices` or `steps`;
    - only `api` and `cmd/engine` import `store`;
    - only `cmd/engine` imports `api`, and no package imports a `cmd/*`;
    - `cmd/check` imports only `checksvc`;
    - in cli and page, `api/gen` imports only the oapi-codegen runtime, and `cmd/<name>` only the
      root package;
    - `slice` imports no platform module;
    - `tests` packages import only the engine packages listed above.
  - With these archfiles every module passed on 2026-11-15, offline, in the pinned image. In a
    copy of the engine, a planted `machine` → `api` import failed with exit 1, naming component,
    file, line and import. An unmapped new package failed with exit 1 and
    `ArchWarningsNotMatched`.
- **Edges that look like boundary violations** (owner, 2026-11-15: both are allowed now as named
  exceptions in `src/engine/.go-arch-lint.yml`, each with a comment naming its follow-up in T097,
  so the check passes on day one):
  - F1 `steps` → `checksvc`: the engine's step driver imports the check service's implementation
    package only for `checksvc.APIVersion()` and `checksvc.CallTimeLimitMax()`, the contract's
    version and `x-limits`, in `steps/steps.go` and `steps/history.go`. A client should get those
    from the check contract, as it already gets `check/api/gen`, not from the server it calls.
    Follow-up (T097): move both into the check contract's generated package or a small shared
    package.
  - F2 `cmd/engine` holds logic, not only wiring: `setup.go`'s `sliceWorkers` routes each
    request to a per-slice `machine.Worker`, and it also computes the agent-definition digest
    (`canonical.FramedHash`). An import rule cannot say "cmd only wires". Follow-up (T097): move
    `sliceWorkers` and the digest into a package, so `cmd` only wires.
  - Not violations, recorded for clarity:
    - `slices` → `checks` (`checks.Fit`, PatternFit) and `steps` → `procedures`
      (`procedures.IsProjectPath`) follow "each rule has one owner".
    - The tests module's evals and the analyst service client import engine packages, which is
      their purpose.
- **Archfile changes need recorded approval** (owner, 2026-11-15): an archfile is a check
  (Principle XVII), so `scripts/hooks/commit-msg` treats `**/.go-arch-lint.yml` as a protected
  path (its `protected` pattern, T010e, ADR 0029). A commit that adds, changes or deletes one
  needs an `Approved-by:` trailer.
- **Scorecard 4.7** (owner, 2026-11-15: accepted): mitigated by the version and module-hash pin,
  the build from source against the checksum database, and manual review of every update.
- **Rationale**: an architecture rule is a deterministic check, fixed before the change it judges
  (Principle XVII), not a reviewer's judgement. go-arch-lint states it as data per module, fails
  closed on any package or import it was not told about, and is mature, free and small enough to
  adopt rather than write (Principle VI).
- **Alternatives rejected**:
  - **arch-go** (`github.com/arch-go/arch-go/v2`, MIT, v2.1.2 of 2026-02-03): rules by package
    patterns (dependencies, contents, functions, naming) with compliance and coverage thresholds.
    Its thresholds allow less than full compliance, it has no "unmapped package fails" by default
    beyond the coverage rate, and it is released less often.
  - **depguard through golangci-lint** (depguard v2.3.0 of 2026-10-27, GPL-3.0; golangci-lint
    v2.15.0 of 2026-10-16): allow and deny lists of import prefixes per file glob. It has no
    components, so every boundary becomes hand-kept prefix lists per folder. A package missing from
    every list is not flagged. It also brings in golangci-lint, a large tool the project does not
    otherwise run.
  - **An own `go list` test**: this rewrites a mature tool (Principle VI). `go list` follows build
    constraints, so it misses tagged files; it missed the `evals`-tagged `engine/slices` import in
    `tests/evals/analyst` that go-arch-lint found.
  - **The tool's Docker image** (`fe3dback/go-arch-lint`): unsigned, and it failed to load the
    std packages until v1.19.1 (issue 75). Building from source in the pinned Go image reuses an
    existing pin.
  - **Deep scan on**: it type-loads every package through `go/packages` (engine about 9 s, against
    about 1 s), and checks dependency injection through method arguments, which the boundary rule
    does not need. The owner keeps it off (owner, 2026-11-15).
- **Known limits**:
  - It checks imports only, not `//go:embed` reach, `os/exec`, reflection or `go:linkname`.
  - One module at a time: a sibling module counts as a vendor, checked by import path only.
  - Exit 1 does not tell a violation from a broken setup. The tests read `--json` or the
    message.
  - A component's own `_test` package needs the component in its own `mayDependOn` (issue 59).
  - A vendor glob `a/**` does not match `a`, so both are listed.
  - Every new package means an archfile change, which is a change to a check (Principle XVII), so it
    needs the owner's `Approved-by:`.
- **Recorded as** [ADR 0043](../../docs/decisions/0043-module-boundaries-checked-by-go-arch-lint.md);
  component [go-arch-lint](../../docs/components/go-arch-lint.md).
- Sources: [go-arch-lint at v1.19.1](https://github.com/fe3dback/go-arch-lint/tree/a34dbfc83fcf68b5b76aa6b4d7e959244f87f739);
  [archfile syntax](https://github.com/fe3dback/go-arch-lint/blob/v1.19.1/docs/syntax/README.md);
  [releases](https://github.com/fe3dback/go-arch-lint/releases);
  [Go module proxy, v1.19.1](https://proxy.golang.org/github.com/fe3dback/go-arch-lint/@v/v1.19.1.info);
  [OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/fe3dback/go-arch-lint);
  [issue 59](https://github.com/fe3dback/go-arch-lint/issues/59);
  [PR 91](https://github.com/fe3dback/go-arch-lint/pull/91);
  [arch-go](https://github.com/arch-go/arch-go); [depguard](https://github.com/OpenPeeDeeP/depguard);
  [Go modules reference, GOPROXY protocol](https://go.dev/ref/mod#goproxy-protocol); the
  experiment of 2026-11-15 in the session scratchpad (pinned `golang` image, archfiles drafted
  there, `src/` read-only).
