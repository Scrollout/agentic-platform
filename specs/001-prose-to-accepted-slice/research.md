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
| S6 | `gh api` checks, 2026-11-11: cucumber/godog (MIT, v0.16.0), cucumber/gherkin (MIT, v42.0.1), stoplightio/spectral, oasdiff (Apache-2.0, v1.37.0), santhosh-tekuri/jsonschema (Apache-2.0, v6.0.3), oapi-codegen (Apache-2.0, v2.8.2), schemathesis (MIT) |
| S7 | godog `internal/flags/flags.go`: `--strict` "fail suite when there are pending or undefined or ambiguous steps" |
| S8 | [EARS official guide](https://alistairmavin.com/ears/) |
| S9 | [Gherkin reference](https://cucumber.io/docs/gherkin/reference/): `Rule` represents one business rule |
| S10 | OpenSSF Scorecard API, 2026-11-12 (scores in D22) |
| S11 | [Anthropic Commercial Terms of Service](https://www.anthropic.com/legal/commercial-terms), effective 2025-06-17: "Anthropic may not train models on Customer Content from Services." |
| S12 | [gVisor Docker Quick Start](https://gvisor.dev/docs/user_guide/quick_start/docker/): `runsc install` adds a Docker runtime selected per container |
| S13 | [ADR 0027: Contract-driven frontend](../../docs/decisions/0027-contract-driven-frontend.md) |
| S14 | RFC 8785, JSON Canonicalization Scheme |

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
  using the project ruleset, checked with oasdiff, and Go code is generated with oapi-codegen.
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

### D14. Budgets, timeouts and rate limits

- **Decision**: the pattern declares per-run budgets (model tokens, cost in USD, wall-clock minutes:
  30) and per-step budgets, enforced through Docker Agent budgets and the engine's clock. A run
  waiting for the developer ends `handed-over` when the developer accepts the hand-over, or
  `expired` after 14 days. The engine API and page allow 20 requests per second
  per token.
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
  limit.
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
| oapi-codegen | Apache-2.0 | 7.5 | |
| Spectral | Apache-2.0 | 4.7 | low score; pinned by digest; watch |
| oasdiff | Apache-2.0 | not published | run locally (task) |
| santhosh-tekuri/jsonschema | Apache-2.0 | 3.2 | low score; pinned; small and replaceable |
| Schemathesis | MIT | 6.2 | |
| nginx | BSD-2 | 7.9 | |
| gVisor | Apache-2.0 | 6.2 | |

Custom code is limited to the engine, its checks, the slice model, the terminal client and the run
page, none of which an existing project provides.

### D23. Event names

- **Decision**: run-event kinds are named noun plus past-tense verb: `RunStarted`, `RunResumed`,
  `RunInterrupted`, `StepStarted`, `AttemptMade`, `CheckPassed`, `CheckFailed`, `StepAccepted`,
  `StepRejected`, `ResultReused`, `FindingRecorded`, `RunHandedOver`, `HandOverAccepted`,
  `RunExpired`, `RunCancelled`, `RunCompleted`.
- **Rationale**: constitution, Names say what things do.
