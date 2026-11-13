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
oapi-codegen, Redocly CLI (contract bundling), Spectral (also as the contract-lint service, D28),
oasdiff (as a Go library in the engine), santhosh-tekuri/jsonschema, Schemathesis, nginx (gateway),
gVisor, WireMock (the recorded model, D26), cosign and the Distribution registry (local signing,
D24); Docker Agent `serve api` runs the analyst (D27) and each agent step (D29);
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
| XIII. Secure supply chain | Exception (approved, partial) | Images pinned by version and digest, except git in the project worker image, pinned by apk package version only (`alpine-git`, see Complexity Tracking); Go tools by version, verified by the Go checksum database; Node tools with their whole dependency tree in `scripts/tools/package-lock.json`; local scans in `scripts/check.sh`; signature verification at start; automated update pull requests deferred while there is no hosted forge |
| XIV. Safe defaults and least agency | Pass | Default-deny egress; per-step writable paths and allowed dependencies; checks fail closed; budgets; rate limits |
| XV. Compatible public APIs | Pass | Engine API: `api-version` query parameter, preview `2026-11-12-preview`, version errors, idempotency keys kept 24 hours, paginated lists. The check service (`2026-11-13-preview`, D29) is internal, called only by the engine with its own token, not a public API: `runProcedure` takes no idempotency key and is repeat-safe by contract (the same request over the same project leaves the same files); so are the project worker's `restoreStep` and `commitStep`, which finds its own commit and makes no second one (D30) |
| XVI. Human control of agents | Exception (approved, partial) | Feature branch; local hooks run every check and refuse protected-path changes without the owner's `Approved-by:` trailer; agent commits carry an attribution trailer. Hosted branch protection when a forge is used |
| XVII. Declare, derive, converge | Pass | Slices and patterns are declarations; EARS text, code and tests are derived |
| XVIII. Abstractions say what | Pass | The API and CLI describe outcomes, not mechanisms |
| XIX. Evals | Pass | Each agent's eval suite exists before the agent; adversarial cases; pass^3 (D19) |
| XX. Deterministic acceptance | Pass | D2, D3, D5, D8, D10, D11; review findings confirmed by rerunning the named rule's tests ([ADR 0031](../../docs/decisions/0031-review-findings-confirmed-by-rerunning-rule-tests.md)) |
| Contract-first | Pass | Contracts reviewed and versioned first, authoritative with their provider; failing tests before code |
| Fresh review | Pass | A fresh reviewer per story and before done, on a model other than the author's |
| Layout | Pass | All code under `src/`; layout check in `scripts/check.sh` |

## Decisions

Each decision that outlives this feature has a record in `docs/decisions/`, linked back here.

| Decision | Research | Record |
|----------|----------|--------|
| Agent steps run on Docker Agent's API server; checks and procedures run in a check service with its own contract | D29 | [ADR 0030](../../docs/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md) |
| A review finding blocks only when its rule's tests, rerun against the current code, fail at the file it names | D29 (closed by T054e) | [ADR 0031](../../docs/decisions/0031-review-findings-confirmed-by-rerunning-rule-tests.md) |
| The new project's version history is a git repository kept by a project worker; the project worker in its own Alpine image with git pinned by exact package version (the Debian check image first chosen, then replaced the same day); only the generate worker has the project root writable, behind a history guard | D30 | [ADR 0032](../../docs/decisions/0032-project-version-history-kept-by-a-project-worker.md), [ADR 0033](../../docs/decisions/0033-project-worker-in-its-own-small-image.md) (the image) |
| Step worker grants made after the engine started are files in a folder only the engine mounts, named by the SHA-256 of the worker's token and read at each authentication; build-workers and startRun accept a project folder of empty folders only | D31 | [ADR 0034](../../docs/decisions/0034-step-worker-grants-as-files-read-at-each-authentication.md) |

## Threat model

Elements and trust boundaries: developer; terminal client; launcher (the only part with
container-engine access); engine; analyst and step agents; the model (provider); gateway; check
container; the check service API (engine token; `runCheck`, `readStepFiles`); procedure step
workers (writable project mounts, serving `runProcedure`); run store; run page; the new project;
artifacts passed from one step into the next agent's context (a trust boundary).

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| Description and answers into the model | | ✓ | | | | | LLM01, ASI01 | The model only proposes; schema, traceability and fit checks; the developer approves meaning; injection cases in evals |
| Model output (analyst, step agents) | | ✓ | | ✓ | ✓ | | LLM05 improper output, LLM10 unbounded consumption | Proposal schema with no platform fields (D17); budgets per run and step (D14) |
| Step-to-step artifacts entering a context | | ✓ | | | | ✓ | LLM01 indirect, ASI06 memory poisoning | Accepted artifacts only, by hash; read-only mounts; injection cases in evals |
| Step agents | | ✓ | | | ✓ | ✓ | ASI02 tool misuse, LLM06 excessive agency | gVisor, read-only outside scope, no capabilities, no egress, resource limits |
| Check container running generated code | | ✓ | | ✓ | ✓ | ✓ | ASI05 code execution | gVisor, no network, time and resource limits, ephemeral file system |
| Check service API (`runCheck`, `readStepFiles`) | ✓ | ✓ | ✓ | ✓ | ✓ | | ASI03 identity abuse, LLM10 unbounded consumption | Accepts only the bearer token issued to the engine (`engineToken`; any other gets 401), on a network shared only with the engine; per-token and per-address rate limits, request and response size caps, `calls-at-once` (503 Busy) and `time_limit_seconds` (504, the process tree stopped), all in the contract's `x-limits`; the project mounted read-only; every path a ProjectPath, nothing read through a symbolic link; input hashes checked (InputHashMismatch); the engine accepts an attempt only when every verdict's `project_files` equals the files it stored by hash; the service keeps no state and the engine records every verdict (D29, `src/check/api/preview/2026-11-13-preview/openapi.yaml`) |
| Procedure step workers (`runProcedure`) | ✓ | ✓ | | | ✓ | ✓ | ASI02 tool misuse, LLM06 excessive agency, ASI03 identity abuse | One worker per procedure step, started by the launcher with a token it creates for that worker and gives only to the worker and the engine (T053a); only that step's writable paths mounted writable; gVisor, no capabilities, no egress, resource limits; a fixed procedure, no model; other procedures and steps answer 404; inputs by content checked against their hashes; writes only ProjectPaths, never through a symbolic link, converging on a repeat; the files it returns are stored by hash and judged by the check service's checks (scope among them); a rejected attempt's writes are undone before the next attempt (D29, D30). Stated limit (owner, 2026-11-13): the generate worker has the project root writable, because its `go.mod` and `go.sum` are in the root; it is fixed code, not an agent, and ScopeKept rejects its writes outside its globs; `.git` is bound read-only on top of it, as in every step container |
| Project worker (`commitStep`, `restoreStep`) | ✓ | ✓ | ✓ | | ✓ | ✓ | ASI03 identity abuse, ASI02 tool misuse, LLM10 unbounded consumption | Accepts only its own token issued to the engine, on `checks` only; gVisor, no capabilities, no egress, resource limits; the contract's rate, size, `calls-at-once` and time limits; requests carry paths, hashes and stored artifacts, never commands, and every git option is fixed in code (no hooks, signing, outside configuration, attribute filters or line-ending conversion); commits only when the last commit holds `before` and the folder holds the accepted `project_files`, otherwise 409 and nothing committed; reused outputs checked against their stored hashes; nothing through a symbolic link; deterministic commits whose trailers name run, step, round and attempt; `.git` is created empty by the launcher and bound read-only into every step container, so only the project worker writes it; as a second layer, a history guard checks `.git` against the worker's own record (last commit, parent, trailers, config hash; atomically replaced, on a volume only it mounts) before any git command, refusing symbolic links, `commondir`, `gitdir`, alternates, `shallow`, grafts and a packed `main`, with 409 HistoryChanged; git runs only through filter-free plumbing with replace objects, fsmonitor, hooks and automatic gc off, and paths reach it only on standard input or after `--`; a path part `.git` in any letter case is not a ProjectPath (D30). Remaining: the developer and host processes can still change `.git`, and the guard then stops the run. Stated limit: it can write any project under the projects folder, and only the engine, naming its run's new project, calls it |
| Gateway | ✓ | | | ✓ | ✓ | | LLM02 | Only holder of credentials; only the provider host; no header or body logging; budget caps |
| Host-reachable bridges: page forwarder, gateway, image registry | | | | ✓ | | ✓ | — | Stated limit (T022d): only these three can reach the host's services, since Docker publishes no port from an `isolated` network and the gateway needs egress; each is a pinned, static component with a fixed configuration and nothing else on its network. Mitigation path: an optional host firewall rule, added by the owner with root and never by `ap`, drops new connections from the platform's subnets (`10.231.0.0/16`) to the host ([local environment](../../docs/operations/local.md#optional-host-firewall-rule)) |
| Launcher | | ✓ | | | | ✓ | LLM03 supply chain | Verifies image signatures at start; pins by digest; never passes the socket to a container |
| Engine API | ✓ | | ✓ | | ✓ | | ASI03 identity abuse | Per-container tokens; rate limit; idempotency; every action recorded |
| Run store | | ✓ | ✓ | ✓ | | | — | Append-only records; content addressed by hash; erasure on delete |
| Run page | ✓ | | | ✓ | ✓ | | — | Bound to `127.0.0.1`; own token; Host header checked against DNS rebinding; GET only; rate limit |
| Terminal client | ✓ | | ✓ | | | | — | Own token; every action recorded by the engine |
| New project | | ✓ | | | | | — | Written only by steps within scope; restored to its last commit before every attempt; one commit per accepted step; handed over with its git version history (D30) |
| Pinned images and tools | | ✓ | | | | | LLM03 | Digest pins and lock files; licence and Scorecard checks; dependency, licence and secret scans in `scripts/check.sh`; a monthly manual update review while there is no forge (XIII exception) |

## Project Structure

### Documentation (this feature)

```text
specs/001-prose-to-accepted-slice/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/           # review record only; contracts live with their provider
└── tasks.md
```

### Source Code (repository root)

```text
src/
├── engine/
│   ├── api/preview/2026-11-12-preview/   # contract and schemas: openapi.yaml, slice, slice-defs,
│   │                                     #   slice-proposal, pattern, run-event (provider: engine)
│   ├── api/                              # handlers, auth, rate limit; generation config, overlay
│   ├── api/gen/                          # generated server and types (not committed)
│   ├── slices/                           # proposal, answers, changes, approvals
│   ├── machine/                          # state machine, reuse, resume, budgets
│   ├── checks/                           # check implementations, with planted fixtures
│   ├── procedures/                       # fixed steps: contract check, generate, review gate
│   ├── cmd/check/                        # serves the check contract (D29): checks/ in the check
│   │                                     #   service, procedures/ in each procedure step worker
│   └── store/                            # run records and artifact store
├── check/          # check service contract module (D29), provider: engine/cmd/check
│   ├── api/preview/2026-11-13-preview/   # openapi.yaml: runCheck, readStepFiles, runProcedure
│   ├── api/check.oapi-codegen.yaml       # generation config: server, client and types
│   └── api/gen/                          # generated (not committed); used by cmd/check and the engine
├── slice/          # shared Go package: slice model, schemas, traceability, EARS derivation
├── catalogue/
│   └── api-feature-go/pattern.yaml       # the one pattern (declaration only)
├── agents/         # Docker Agent definitions: analyst, tests, code, review
├── cli/            # terminal client
├── page/           # read-only run page
├── deploy/         # launcher, Compose, Dockerfiles, nginx gateway template, image lock
└── tests/          # contract, acceptance, planted failures, evals (Go module)
```

**Structure Decision**: Go modules `src/engine`, `src/slice`, `src/cli`, `src/page`, `src/check`
and `src/tests`; declarations in `src/catalogue` and `src/agents`; packaging in `src/deploy`. The
engine's contract lives with the engine, as ADR 0004 requires. The check service's contract and
its generated code live in `src/check`, a module that needs only `oapi-codegen/runtime`; its
command `src/engine/cmd/check` is in the engine module so it reuses `engine/checks` and
`engine/procedures` without copying them. It runs in three deployments: as the check service, with
the project mounted read-only (checks, an agent step's files); as each procedure step's
worker, which is not read-only: its step's writable paths are mounted writable and the rest of
the project read-only (its procedure); and as the project worker, with the projects folder
writable, which keeps each project's git version history (D30, [ADR 0032](../../docs/decisions/0032-project-version-history-kept-by-a-project-worker.md)). The engine's check client uses the same generated
package (research D29, [ADR 0030](../../docs/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md)).
Agent steps run the pinned Docker Agent `serve api`, whose API is their contract (D29). Built
images exclude `src/tests`,
test files and evals (one Docker ignore rule per image).

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Principle II: no separate control and runtime planes | One developer on one machine; the engine both manages and runs | Two planes would double the deployables before any user exists; they come with hosting |
| Principle V: per-session tokens instead of workload identity | Calls stay authenticated without an identity provider on a laptop | An identity provider for a single-user local tool; deferred to hosting |
| Principle XI (partial): no encryption between local containers or at rest | Containers share one internal network on one machine; data stays on the developer's disk | TLS and disk encryption for a local tool; required once anything is hosted |
| Principle XIII (partial): git in the project worker image pinned by package version, not digest (`images.lock` `alpine-git` `2.49.1-r0`, `src/deploy/Dockerfile.project`; ADR 0033) | The project worker needs git; an Alpine package has no digest, and the base image is pinned by digest, so a new package release fails the build until the pin moves. Approved by the project owner on 2026-11-13 | The Debian base with git, pinned by digest, made the check image 1.29 GB, too large for the owner's host (ADR 0033) |
| Principle XII: no tenant isolation | Single developer by scope (FR-021) | Not applicable until the hosted version |
| Principle XVI (partial): local git hooks instead of branch protection and code owners | No hosted forge for now (owner, 2026-11-12); hooks run every check and require `Approved-by:` and `Tests-approved-by:` trailers | Hooks can be skipped with `--no-verify`, so they record approval rather than enforce it; hosted branch protection returns with a forge |
| Principle XIII (partial): no automated dependency update pull requests | No hosted forge for now (owner, 2026-11-12) | A self-hosted update bot for one local repository; a monthly manual review instead, until a forge is used |

The project owner approved the exceptions for Principles II, V and XII on 2026-11-12, the
Principle XI exception on 2026-11-12 with the revised plan, and the Principle XIII and XVI exceptions on
2026-11-12 when GitHub was set aside for now. `scripts/check.sh` with the git hooks is the
project's CI while there is no forge. They hold for this local,
single-developer version only and are recorded in the commit that carries this plan.

## Exceptions to Engineering Principles

| Rule | Location | Reason |
|------|----------|--------|
| Test-first task split (tasks.md rules; Outside-in, test-first) | T005, T012 | Each task holds a check and its planted-violation tests. The tests were written by a separate test author (commits `c3bb1ff` and `66de0d3`), so authorship is separate; only the task split is not. Later tasks keep the split. |
| Contract-first step 3 and Outside-in, test-first ("see them fail before writing the minimal code") | T030, T031 against T038, T038c, T039 (commits `65b82c8`, `044fe8d`, then `a0b5688`) | In Story 1 the API conformance and acceptance suites were written after the slice handlers and the terminal client they judge; the unit tests of each unit came first, by separate authors. The late suites found two engine defects, fixed with regression tests in `a0b5688`. Approved by the project owner on 2026-11-13. From Story 2 on, T045, T046 and T067 are written and seen failing before T054 and T055 exist. |
| Tasks dependencies ("T050 before T051") | T051 (the state machine) built before T050 (the fixed procedures) | The machine takes every step through injected `machine.Worker` and `machine.Checker` interfaces, so its tests (T043) and code needed no procedure; T050 is still open. Approved by the project owner on 2026-11-13. |
| Outside-in, test-first ("see them fail before writing the minimal code"); the previous row's "T046 before T054" | T054 (the run operations) built before T046 (the planted-failure and Story 2 acceptance suites) | T054 was written against T045's conformance suite and its unit tests, by separate authors; T046 did not exist. The owner chose on 2026-11-13 to write T046 now and judge T054 with it before anything else; any T054 defect it finds is fixed first, with a regression test. Approved by the project owner on 2026-11-13. |
| Tasks dependencies ("T054c after T050 and T053a's token wiring"; "T054b after T054, taken before T054c") | T054c (the engine calls its step workers and the check service, `b331c72`) built before T053a's token wiring and before T054b | T054c added the `WorkerGrant.CallToken` seam, through which the engine calls a procedure step worker with the token the launcher issues; no procedure step can run until T053a issues those tokens, so building T054c first changes nothing a run can do. Approved by the project owner on 2026-11-13. |
| Outside-in, test-first; the T046 note ("judge T054 with it before anything else") | T050 (`5427238`), T054e (`2d3ca99`), T054c (`b331c72`) and T049 with T041 (`5fb28ab`) built while T046 (the planted-failure and Story 2 acceptance suites, written, uncommitted) cannot yet pass | T046 needs T053a's step containers, T055's CLI and the lab seam, so it cannot run until they exist; each unit above was written against its own tests and evals by separate authors and passed a fresh review. T046 must pass before T055 is done and before T053a's end-to-end run; a defect it finds in these units is fixed first, with a regression test. Found by the analyze re-run after T049 (I1). Approved by the project owner on 2026-11-13. |
| Outside-in, test-first ("see them fail before writing the minimal code") | T053c (the project worker, `6ee3311`) | The 28 tests by a separate author were written from the contract alone, but the implementer, working in parallel, landed the code before their first run, so they were never seen failing. A fresh review then broke the code 33 ways in its own copy; every history guard is caught by a test (one missing test, the read-versus-commit race, was added and seen failing against its mutation). Approved by the project owner on 2026-11-13. |
| Fix the cause, not the check (a deliberate change to a check) | `src/engine/api/runs_cancel_race_test.go` (`6ee3311`); the `waitRun` helper in `src/engine/api/runs_ops_test.go` (T053d) | Its wall-clock budget for 30 cancel-right-after-start runs went from 15 s to 60 s: under `-race` on the owner's small host, with the pre-commit hook running every module, it finished 26 of 30 in 15 s. The race assertion (every run ends `cancelled`) is unchanged; only the time allowed changed. The same host load made `TestGetArtifactReturnsTheBytesTheRunProduced` see its run still running after `waitRun`'s 5 s, so that helper's limit went to 30 s (alone it passes in about 1 s); passing tests take no longer. Approved by the project owner on 2026-11-13. |
| Outside-in, test-first ("see them fail before writing the minimal code") | T053d's grant-file tests (`src/engine/api/grants_dir_test.go`) | Written by a separate author in parallel with the implementer, whose `GrantsDir` code was in the tree before their first run, so they were never seen failing. A fresh review broke the grant and skeleton code 31 ways in its own copy; after round 1 the author added cases for every surviving guard, and round 2 confirmed each is caught (one trailing-`}` case is filed in T053g). The skeleton, refusal and token-default tests were seen failing first. From T053e on, the test author finishes before the implementer starts. Approved by the project owner on 2026-11-14. |
