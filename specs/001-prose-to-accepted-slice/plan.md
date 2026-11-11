# Implementation Plan: From a description to accepted working software, one slice at a time

**Branch**: `001-prose-to-accepted-slice` | **Date**: 2026-11-12 (revised after fresh review round 1) | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/001-prose-to-accepted-slice/spec.md`

## Summary

A developer describes one use case in prose; the platform proposes a slice (goals, terms,
structured business rules, Gherkin examples, an OpenAPI contract), checks its form, traceability
and fit with the catalogue's one pattern, asks targeted questions, and takes the developer's
approval. It then builds the slice into a new Go project as a declared state machine whose steps
run in their own gVisor-sandboxed containers with read-only mounts, and whose transitions are
decided by deterministic checks. Every run is recorded, and accepted results are reused, so a
replay is byte-identical. The approach is the ADR 0026 spike, made general and durable
([research](research.md)). Proposing new patterns is a later feature.

## Technical Context

**Language/Version**: Go 1.26 for all platform code; the built slices are Go (D1)

**Primary Dependencies**: Docker Agent v1.160.0 (pinned image digest), godog, cucumber/gherkin,
oapi-codegen, Spectral, oasdiff, santhosh-tekuri/jsonschema, Schemathesis, nginx (gateway), gVisor;
Claude Haiku via the gateway (D2–D4, D9, D13)

**Storage**: append-only JSON Lines run records and a content-addressed artifact store in a named
volume (D8)

**Testing**: Go tests; godog for examples; Spectral and oasdiff on contracts; Schemathesis
conformance against a recorded model; planted-failure acceptance; live evals with pass^3 (D19)

**Target Platform**: Linux with a container runtime, Compose and the gVisor runtime

**Project Type**: local multi-container tool with a terminal client and a read-only web page

**Performance Goals**: a successful build in under 30 minutes (SC-007), enforced as the run budget

**Constraints**: no egress except through the gateway; no container-engine socket in any container;
3 attempts per step and 3 review rounds; budgets for tokens, cost and time; 20 requests per second
per token on the API

**Scale/Scope**: one developer, one machine, one active run; one catalogue pattern

## Constitution Check

*Evaluated before research and again after design: PASS with the approved exceptions below.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Needs before solutions | Pass | The spec links only the needs it serves |
| II. Two planes | Exception (approved) | One local process group; see Complexity Tracking |
| III. API-first | Pass | The engine API is the only management path; the CLI and page are clients; an API client without workers gets an explicit refusal (D5, D7) |
| IV. Asynchronous operations | Pass | Every state change returns an operation that can be followed and cancelled; failures are retried with backoff and kept visibly as failed (D7) |
| V. Zero-trust identity | Exception (approved, partial) | Per-session, per-container tokens; full workload identity deferred (D12) |
| VI. Adopt mature OSS | Pass | Scorecard and licence table (D22); nginx replaces a custom gateway; custom code only where no project fits (D21) |
| VII. Pluggable providers | Pass | Model reached only through the gateway; the pattern is a declaration |
| VIII. Containers and sandboxed execution | Pass | Every part in a container; non-root, read-only root, no capabilities; agent steps and checks under gVisor with resource limits (D13); images signed with SBOM and provenance, verified by the launcher at start |
| IX. Knowledge stays true | Pass | Tasks record decisions, components, glossary terms and the log |
| X. No silent loss | Pass | One recorded outcome per run; resume; budgets; hand-overs expire after 14 days (D14) |
| XI. Agent data privacy | Exception (approved, partial) | Provider does not train on API content (D16); retention until deleted; no telemetry; no encryption between local containers or at rest in this version |
| XII. Tenant isolation | Not applicable | One developer by scope (FR-021); listed in Complexity Tracking for the record |
| XIII. Secure supply chain | Exception (approved, partial) | Images and tools pinned by version and digest; local scans in `scripts/check.sh`; signature verification at start; automated update pull requests deferred while there is no hosted forge |
| XIV. Safe defaults and least agency | Pass | Default-deny egress; per-step writable paths and allowed dependencies; checks fail closed; budgets; rate limits |
| XV. Compatible public APIs | Pass | `api-version` query parameter, preview `2026-11-12-preview`, version errors, idempotency keys kept 24 hours, paginated lists |
| XVI. Human control of agents | Exception (approved, partial) | Feature branch; local hooks run every check and refuse protected-path changes without the owner's `Approved-by:` trailer; agent commits carry an attribution trailer. Hosted branch protection when a forge is used |
| XVII. Declare, derive, converge | Pass | Slices and patterns are declarations; EARS text, code and tests are derived |
| XVIII. Abstractions say what | Pass | The API and CLI describe outcomes, not mechanisms |
| XIX. Evals | Pass | Each agent's eval suite exists before the agent; adversarial cases; pass^3 (D19) |
| XX. Deterministic acceptance | Pass | D2, D3, D5, D8, D10, D11 |
| Contract-first | Pass | Contracts reviewed and versioned first, authoritative with their provider; failing tests before code |
| Fresh review | Pass | A fresh reviewer per story and before done, on a model other than the author's |
| Layout | Pass | All code under `src/`; layout check in CI |

## Threat model

Elements and trust boundaries: developer; terminal client; launcher (the only part with
container-engine access); engine; analyst and step agents; the model (provider); gateway; check
container; run store; run page; the new project; artifacts passed from one step into the next
agent's context (a trust boundary).

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| Description and answers into the model | | ✓ | | | | | LLM01, ASI01 | The model only proposes; schema, traceability and fit checks; the developer approves meaning; injection cases in evals |
| Model output (analyst, step agents) | | ✓ | | ✓ | ✓ | | LLM05 improper output, LLM10 unbounded consumption | Proposal schema with no platform fields (D17); budgets per run and step (D14) |
| Step-to-step artifacts entering a context | | ✓ | | | | ✓ | LLM01 indirect, ASI06 memory poisoning | Accepted artifacts only, by hash; read-only mounts; injection cases in evals |
| Step agents | | ✓ | | | ✓ | ✓ | ASI02 tool misuse, LLM06 excessive agency | gVisor, read-only outside scope, no capabilities, no egress, resource limits |
| Check container running generated code | | ✓ | | ✓ | ✓ | ✓ | ASI05 code execution | gVisor, no network, time and resource limits, ephemeral file system |
| Gateway | ✓ | | | ✓ | ✓ | | LLM02 | Only holder of credentials; only the provider host; no header or body logging; budget caps |
| Launcher | | ✓ | | | | ✓ | LLM03 supply chain | Verifies image signatures at start; pins by digest; never passes the socket to a container |
| Engine API | ✓ | | ✓ | | ✓ | | ASI03 identity abuse | Per-container tokens; rate limit; idempotency; every action recorded |
| Run store | | ✓ | ✓ | ✓ | | | — | Append-only records; content addressed by hash; erasure on delete |
| Run page | ✓ | | | ✓ | ✓ | | — | Bound to `127.0.0.1`; own token; Host header checked against DNS rebinding; GET only; rate limit |
| Terminal client | ✓ | | ✓ | | | | — | Own token; every action recorded by the engine |
| New project | | ✓ | | | | | — | Written only by steps within scope; handed over with version history |
| Pinned images and tools | | ✓ | | | | | LLM03 | Digest pins; licence and Scorecard checks; automated updates; CI scanning |

## Project Structure

### Documentation (this feature)

```text
specs/001-prose-to-accepted-slice/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/           # drafts; authoritative copies live with their provider
│   ├── engine-api.openapi.yaml
│   ├── slice.schema.json
│   ├── slice-proposal.schema.json
│   ├── pattern.schema.json
│   ├── run-event.schema.json
│   └── cli.md
└── tasks.md
```

### Source Code (repository root)

```text
src/
├── engine/
│   ├── api/preview/2026-11-12-preview/   # authoritative contract and schemas (provider: engine)
│   ├── api/                              # generated server, handlers, auth, rate limit
│   ├── slices/                           # proposal, answers, changes, approvals
│   ├── machine/                          # state machine, reuse, resume, budgets
│   ├── checks/                           # check implementations, with planted fixtures
│   ├── procedures/                       # fixed steps: contract check, generate, review gate
│   └── store/                            # run records and artifact store
├── slice/          # shared Go package: slice model, schemas, traceability, EARS derivation
├── catalogue/
│   └── api-feature-go/pattern.yaml       # the one pattern (declaration only)
├── agents/         # Docker Agent definitions: analyst, tests, code, review
├── cli/            # terminal client
├── page/           # read-only run page
├── deploy/         # launcher, Compose, Dockerfiles, nginx gateway template, image lock
└── tests/          # contract, acceptance, planted failures, evals (Go module)
```

**Structure Decision**: Go modules `src/engine`, `src/slice`, `src/cli`, `src/page` and
`src/tests`; declarations in `src/catalogue` and `src/agents`; packaging in `src/deploy`. The
engine's contract lives with the engine, as ADR 0004 requires. Built images exclude `src/tests`,
test files and evals (one Docker ignore rule per image).

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Principle II: no separate control and runtime planes | One developer on one machine; the engine both manages and runs | Two planes would double the deployables before any user exists; they come with hosting |
| Principle V: per-session tokens instead of workload identity | Calls stay authenticated without an identity provider on a laptop | An identity provider for a single-user local tool; deferred to hosting |
| Principle XI (partial): no encryption between local containers or at rest | Containers share one internal network on one machine; data stays on the developer's disk | TLS and disk encryption for a local tool; required once anything is hosted |
| Principle XII: no tenant isolation | Single developer by scope (FR-021) | Not applicable until the hosted version |
| Principle XVI (partial): local git hooks instead of branch protection and code owners | No hosted forge for now (owner, 2026-11-12); hooks run every check and require `Approved-by:` and `Tests-approved-by:` trailers | Hooks can be skipped with `--no-verify`, so they record approval rather than enforce it; hosted branch protection returns with a forge |
| Principle XIII (partial): no automated dependency update pull requests | No hosted forge for now (owner, 2026-11-12) | A self-hosted update bot for one local repository; a monthly manual review instead, until a forge is used |

The project owner approved the exceptions for Principles II, V and XII on 2026-11-12, the
Principle XI exception on 2026-11-12 with the revised plan, and the Principle XIII and XVI exceptions on
2026-11-12 when GitHub was set aside for now. `scripts/check.sh` with the git hooks is the
project's CI while there is no forge. They hold for this local,
single-developer version only and are recorded in the commit that carries this plan.

## Exceptions to Engineering Principles

None.
