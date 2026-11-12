# Directory Update Log

## 2026-11-12

* **Guide**: [Install the gVisor runtime for Docker](/guides/install-gvisor.md), from gVisor's install, Docker and systemd cgroup pages. Feature 001 research D24 records a tested recommendation for signing the platform's images (local registry, cosign key pair outside the repository, no transparency log), for the owner to decide.
* **Implementation**: Feature 001, T022 and T023 (in progress): `src/deploy/compose.yaml` declares the gateway (the only container with egress and the key, as a Compose secret), engine, run page (on `127.0.0.1` only), terminal client, test harness, and gVisor step and check workers with CPU, memory and process limits, all non-root with read-only root file systems and no capabilities; the launcher `src/deploy/ap` checks the credentials file and projects folder, keeps a started session's tokens on a repeated `up` (new ones only after `down`) and passes them to Compose only through the environment. Image signature verification and per-slice workers are not done yet. See the [local environment](/operations/local.md).
* **Implementation**: Feature 001, T018a: the engine counts requests without an accepted token by the connection's peer address (never a client header), 401 up to `x-limits.requests-per-second-per-address-without-token` and 429 beyond it; the rate limiter now drops windows idle for a second, so its memory is bounded by the distinct callers of any one second. Tests by a separate author; fresh review round 1 rejected (eviction and start-up validation untested), round 2 approved.
* **Implementation**: Feature 001, T009a: `scripts/hooks/pre-commit` now checks the staged snapshot, exported from the index into a temporary directory, so untracked files and unstaged changes no longer affect a commit; `scripts/check.sh` runs `go test -race` with cgo in a Debian-based golang image of the same Go release, pinned as `golang-race` in `src/deploy/images.lock`. See the [protect-changes-locally playbook](/playbooks/protect-changes-locally.md).
* **Contract**: the engine contract now also rate-limits requests without an accepted token, per source address (`x-limits.requests-per-second-per-address-without-token`, Principle XIV, T018a); up to the limit they get 401, beyond it 429. Fresh contract review: round 1 rejected (stale `RateLimited` description), round 2 approved.
* **Implementation**: Feature 001, T015 and T018: the engine skeleton serves the generated API with version check, per-token authentication and authorization, a sliding one-second rate limit, idempotency keys kept for the contract's retention, and operations with retries, cancellation and visible failures; every client-visible fact is read from the contract with go-yaml.
* **Contract**: the engine contract now declares its client-visible limits once in a root `x-limits` block (requests per second per token, idempotency-key retention, body size), adds the universal error `OperationFailed`, defines the operations list as one entry per permission name, and returns 400 for an unissued page token; two fresh contract review rounds (round 1 rejected, round 2 approved).
* **Implementation**: Feature 001, T019: the model gateway on the pinned nginx image forwards only `/v1/messages` and `/v1/messages/count_tokens` to Anthropic's API, injects the key from a mounted file at start, logs no headers or bodies, and runs non-root on a read-only root file system; its test runs in `scripts/check.sh`.
* **Implementation**: Feature 001, T011e: every repeated value rule in the engine contract (IDs, step, check, project and pattern names, worker references and endpoint, api-version, hash) is defined once in `slice-defs.schema.json`; a fresh review confirmed no rule changed.
* **Implementation**: Feature 001, T014 and T017: the engine's run store keeps append-only JSON Lines run records per slice and a content-addressed artifact store, durable before it acknowledges, with erasure per slice.
* **Implementation**: Feature 001, T013 and T016: `src/slice` checks slice and proposal documents against the engine's JSON Schemas (santhosh-tekuri/jsonschema v6.0.3), with planted-violation fixtures by a separate test author.
* **Implementation**: Feature 001, T012a: a separate test author added 9 cases for `scripts/generate.sh` (newest contract version, no targets, missing pins) and `scripts/npm-tool.sh` (no network, no reinstall, a new cache per lock, markdownlint with the repository config).
* **Implementation**: Feature 001, T011b and T011d: the engine contract now references the slice, run and hash rules in `slice-defs.schema.json` instead of repeating them, and applies the T011a review's suggestions (`registerWorker` 404, `Difference` sides, `Worker.endpoint` as a base address, the client's stop rule for run events); a fresh contract review approved them. Remaining repeated rules are task T011e.
* **Implementation**: Feature 001, after the re-run analyze review (round 1 failed with 3 HIGH findings): the spec now lets a developer cancel a run, which ends `cancelled` (clarification); oasdiff breaking-change checks are planned in T029, T037 and T024a; the Node tools (Redocly CLI, Spectral, markdownlint-cli2) are pinned with their whole dependency tree in `scripts/tools/package-lock.json` and run without network in the pinned Node image by `scripts/npm-tool.sh`; the draft contract copies in `specs/` were removed and the terminal contract moved to `src/cli/contract.md`.
* **Implementation**: Feature 001, T010a and T011a: tests for the commit-msg hook by a separate author, and the eight round-2 suggestions applied to the engine contract.
* **Implementation**: Feature 001, T012: `scripts/generate.sh` bundles the engine contract with Redocly CLI and generates the engine's strict server and the client's Go code with oapi-codegen. Generated code is not committed; `scripts/check.sh` regenerates it and fails on any difference from the contract. Added Redocly CLI to `src/deploy/images.lock` and oapi-codegen/runtime to the Go modules.
* **Implementation**: Feature 001, T011: the engine's contracts passed review (round 1 rejected with 18 blocking findings, round 2 approved) and moved to `src/engine/api/preview/2026-11-12-preview/`. `scripts/check.sh` with the git hooks now serves as the project's CI (dependency and licence scans; code-plus-tests rule); Principle XVI recorded as a partial exception.
* **Implementation**: Feature 001, Phase 1 (Setup): the `src/` layout and Go modules, the root-layout check and the Spectral ruleset with planted-violation tests, the image lock, local checks and git hooks instead of hosted CI (no GitHub for now), `NOTICE`, and the [protect-changes-locally playbook](/playbooks/protect-changes-locally.md).
* **Specs**: Wrote the first feature spec, `specs/001-prose-to-accepted-slice/spec.md`, and linked it from needs N-003 and N-050 to N-063 in the [problem space](/overview/problem-space.md), now marked `specified`.
* **Research**: Added [From prose to software through checked layers](/references/prose-to-software-pipeline.md), the prior art for the platform's prose-to-code pipeline.
* **Decisions**: The owner accepted [ADR 0026](/decisions/0026-deterministic-workflow-engine.md) with the spike's three changes: read-only mounts per state are a required guard, blocking review findings need checkable evidence, and accepted state outputs are reused as assets.

## 2026-11-11

* **Workflow engine**: Ran the [ADR 0026](/decisions/0026-deterministic-workflow-engine.md) spike on Haiku, all in containers (4.5 minutes, about $0.04). Guards held in every planted scenario; results and recommendation added to the ADR, which stays proposed.
* **Docker Agent**: The owner set Claude Haiku as the model for the MVP, demos and drafts ([ADR 0002](/decisions/0002-build-on-docker-agent.md)).
* **Workflow engine**: [ADR 0026](/decisions/0026-deterministic-workflow-engine.md)'s spike now keeps model credentials (the project API key and the owner's user token, local testing only) in a single gateway container, out of every agent container.
* **Decisions**: The owner confirmed [ADR 0002](/decisions/0002-build-on-docker-agent.md) and [ADR 0003](/decisions/0003-ahp-client-session-protocol.md), and accepted [ADR 0027](/decisions/0027-contract-driven-frontend.md), including starting the console as one app and one build.
  * ADR 0027 amends [ADR 0004](/decisions/0004-contract-first-toolchain.md): Orval is now the TypeScript client generator.
* **Frontend**: Removed or mitigated every known weakness of the frontend stack in [ADR 0027](/decisions/0027-contract-driven-frontend.md) (proposed) and the [frontend toolchain reference](/references/frontend-toolchain.md).
  * Orval replaces openapi-typescript, openapi-msw and the Hey API fallback; ADR 0004 now points to the proposed change.
  * Pact OSS checks the UI's real API usage, ClickStack replaces Perses and Recharts replaces ECharts.
  * shadcn/ui moves to its React Aria base, and a CSP gate enforces the webview policy.
* **Docker Agent**: The owner ruled that Docker Agent runs only in containers, never directly on a host. This is recorded in [ADR 0002](/decisions/0002-build-on-docker-agent.md) and applied to the [ADR 0026](/decisions/0026-deterministic-workflow-engine.md) spike.
* **Frontend**: [ADR 0027](/decisions/0027-contract-driven-frontend.md) now bases the design system on shadcn/ui with Base UI primitives, plus React Aria Components where accessibility or localisation needs them; no packaged vendor design system such as Fluent UI. See the [frontend toolchain reference](/references/frontend-toolchain.md).
* **Frontend**: [ADR 0027](/decisions/0027-contract-driven-frontend.md) now answers each Azure portal weakness, adding a module conformance suite as a release gate and a round-trip test for exports.
* **Frontend**: Added the [Azure portal architecture reference](/references/azure-portal.md). [ADR 0027](/decisions/0027-contract-driven-frontend.md) (proposed) now models the console on the portal: a shell with one UI module per service, a UI manifest in the registry, lists from a query service, and permission-aware actions.
* **Frontend**: Added the [frontend toolchain reference](/references/frontend-toolchain.md) and [ADR 0027](/decisions/0027-contract-driven-frontend.md) (proposed). The frontend builds against the contracts and AHP, in parallel with the backend.
* **Decisions**: The owner accepted [ADR 0004](/decisions/0004-contract-first-toolchain.md) (contract-first toolchain, service-owned APIs and versioning).
* **Formatting**: Replaced inline footnote markers in every concept with one `Sources:` line per section, and updated the [maintenance playbook](/playbooks/maintain-knowledge-bundle.md) rule to match.
* **Decisions**: The owner approved [ADR 0001](/decisions/0001-adopt-okf.md) (adopt OKF), so it is now `accepted`.
* **API versioning**: The owner agreed to carry the version in the `api-version` query parameter ([ADR 0004](/decisions/0004-contract-first-toolchain.md)).
* **API versioning**: The owner set 12 months' notice for retiring a stable version. Researched where the version goes (query, header or path) in the [ARM API versioning reference](/references/arm-api-versioning.md); [ADR 0004](/decisions/0004-contract-first-toolchain.md) keeps the `api-version` query parameter.
* **API versioning**: Added the [ARM API versioning reference](/references/arm-api-versioning.md), from Microsoft's guidelines and the owner's ARM probes.
  * Extended [ADR 0004](/decisions/0004-contract-first-toolchain.md) (proposed): each microservice owns its contract and operations list; versions are date-based stable and preview, per service; CI enforces every rule.
* **Workflow engine**: Added [ADR 0026](/decisions/0026-deterministic-workflow-engine.md) (proposed): declared state machines decide, agents act inside states, and deterministic guards gate transitions. It includes a spike plan with six planted failures.
  * Added the terms Guard and State worker to the [glossary](/overview/glossary.md).
* **Constitution v1.4.0**: Moved every principle's rationale and citations into its own decision record, ADRs 0005 to 0025 (one per Core Principle I to XX, plus one for the Engineering Principles).
  * The rules are unchanged, and the constitution shrank from 6,166 to about 5,540 words.
  * This settles the follow-up about decision records behind the principles.
* **Constitution ratified**: The owner approved `.specify/memory/constitution.md` v1.3.0, ratified 2026-11-11. The review-only Sync Impact Report was removed.
  * Approving Principle XX put needs N-051, N-052, N-053 and N-061 in scope in the [problem space](/overview/problem-space.md).
  * Follow-ups: decision records behind the principles, a vulnerability fix-time decision (Principle XIII), and the API versioning and deprecation policy (Principle XV).
* **Constitution**: The draft is now v1.3.0. It adds Core Principle XX, Deterministic Acceptance of Agent Work, which serves needs N-050 to N-063 in the [problem space](/overview/problem-space.md).
* **Constitution**: The draft is now v1.2.0. It adds the "Review converges" Engineering Principle for need N-063 in the [problem space](/overview/problem-space.md).
* **Problem space**: Added N-063 (review and fixing converge) and the non-converging review loop to the problem in the [platform overview](/overview/platform.md).
* **Purpose**: Stated the platform's purpose, problem, approach, users, scope, delivery and non-goals in the [platform overview](/overview/platform.md), in the owner's wording.
  * Settled N-001 in the [problem space](/overview/problem-space.md) and added N-002 to N-005 and the reliable-agent-work needs N-050 to N-062.
  * N-030 (several organizations) is now in scope.
* **Contract-first**: Rewrote the [contract-first reference](/references/contract-first-development.md).
  * AWS evidence (Smithy, Well-Architected REL03-BP03, DevOps Guidance QA.NT.7, EventBridge) replaces Zalando as a source.
  * The open choices are resolved by research and proposed in [ADR 0004](/decisions/0004-contract-first-toolchain.md).
* **Problem space**: Added the [problem space](/overview/problem-space.md) with the first needs. Only N-001 (purpose) and N-020 (shared live sessions, [ADR 0003](/decisions/0003-ahp-client-session-protocol.md)) are in scope so far.
* **Governance**: The project is MIT-licensed (`LICENSE` added).
  * The knowledge bundle rules moved from `AGENTS.md` into the [maintenance playbook](/playbooks/maintain-knowledge-bundle.md). `AGENTS.md` now only points to the constitution, the playbook and the tools.
* **Agent Host Protocol**: Researched AHP spec v1.0.0.
  * Added the [AHP reference](/references/agent-host-protocol.md) and [ADR 0003](/decisions/0003-ahp-client-session-protocol.md).
  * Updated the [architecture overview](/architecture/overview.md), [platform overview](/overview/platform.md) and [glossary](/overview/glossary.md).
* **Contract-first**: Researched contract-first API development in the [contract-first reference](/references/contract-first-development.md).
  * The draft constitution's Contract-first rule (v1.1.0) is based on it.
* **Constitution**: Drafted `.specify/memory/constitution.md` v1.0.0, adapted from the Email Gateway constitution without its vertical-slice rules. It is outside the knowledge bundle (`docs/`) and awaits owner ratification.
* **Docker Agent**: Researched Docker Agent v1.160.0 as the platform's foundation.
  * Added the [Docker Agent reference](/references/docker-agent.md) and [ADR 0002](/decisions/0002-build-on-docker-agent.md).
  * Updated the [platform overview](/overview/platform.md), [architecture overview](/architecture/overview.md) and [glossary](/overview/glossary.md).
* **Tooling**: Added GitHub Spec Kit (`specify` 1.2.6.dev0, Claude integration). Its files live in `.specify/` and `.claude/skills/speckit-*`, outside the knowledge bundle (`docs/`).
* **Initialization**: Created the OKF v0.2 knowledge bundle in `docs/`.
  * Added the [platform overview](/overview/platform.md), [glossary](/overview/glossary.md) and [architecture overview](/architecture/overview.md).
  * Added the [maintenance playbook](/playbooks/maintain-knowledge-bundle.md), [ADR 0001](/decisions/0001-adopt-okf.md) and the [OKF spec reference](/references/okf-spec.md).
  * Added documentation conventions for agents, components, apis, data, operations, security and guides.
