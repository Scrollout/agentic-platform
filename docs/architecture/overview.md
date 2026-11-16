---
type: Architecture
title: Architecture overview
description: System context, building blocks, and cross-cutting design of the platform.
tags: [architecture]
status: draft
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D5 to D13, D24, D27, D28, D29, D30
  - id: adr-0028
    resource: /decisions/0028-sign-platform-images-locally.md
    title: "ADR 0028: Sign the platform's images locally with cosign"
  - id: adr-0030
    resource: /decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md
    title: "ADR 0030: Agent steps run on Docker Agent's API server; checks and procedures run in a check service with its own contract"
  - id: adr-0031
    resource: /decisions/0031-review-findings-confirmed-by-rerunning-rule-tests.md
    title: "ADR 0031: A review finding blocks only when its rule's tests, rerun against the current code, fail at the file it names"
  - id: adr-0032
    resource: /decisions/0032-project-version-history-kept-by-a-project-worker.md
    title: "ADR 0032: The new project's version history is a git repository kept by a project worker"
  - id: adr-0039
    resource: /decisions/0039-review-findings-confirmed-by-a-new-witness-test.md
    title: "ADR 0039: A blocking review finding is confirmed by a new failing witness test the review writes"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T20:00:00Z }
---

# Context

A developer on one machine describes a use case in prose and the platform takes it to approved,
tested software; the model proposes, deterministic checks decide. The users and boundary come from
the [platform purpose](/overview/platform.md).

# Building blocks

Agents run on [Docker Agent](/references/docker-agent.md)
([ADR 0002](/decisions/0002-build-on-docker-agent.md)); interactive clients will share sessions
through the [Agent Host Protocol](/references/agent-host-protocol.md)
([ADR 0003](/decisions/0003-ahp-client-session-protocol.md)). Feature 001 builds one slice of the
platform on one machine ([ADR 0026](/decisions/0026-deterministic-workflow-engine.md)).

Shipped by Story 1:

- **Launcher `ap`** (`src/deploy/ap`): starts and stops the containers and passes each its token.
  It verifies the cosign signature of every platform image against the local registry before
  anything starts ([ADR 0028](/decisions/0028-sign-platform-images-locally.md),
  [cosign](/components/cosign.md), [registry](/components/distribution-registry.md)).
- **Engine** (`src/engine`): the slice service (proposes, changes and approves slices), the run store
  (slices and runs on a volume only it mounts) and the contract checks (schema, traceability,
  pattern fit, [oasdiff](/components/oasdiff.md) compatibility, lint). It serves the contract-first
  engine API.
- **Analyst service**: [Docker Agent](/components/docker-agent.md) running `serve api` on the
  analyst agent; the engine calls it for each proposal (research D27).
- **Contract-lint service**: a small [Spectral](/components/spectral.md) server that lints a slice's
  contract; only the engine reaches it (research D28).
- **Model gateway**: [nginx](/components/nginx.md), the only container with egress and the only one
  that holds the provider key.
- **Terminal client** (`src/cli`): calls the engine API. The read-only **run page** (`src/page`)
  arrives with Story 2 (T056); until then `ap up` starts without it.

Shipped by Story 2 so far:

- **Check service** (`src/engine/cmd/check`, contract in `src/check`): serves one
  [contract](../../src/check/api/preview/2026-11-13-preview/openapi.yaml) that only the engine
  calls. In the check container it reads the project, never writes it, runs a step's checks and
  returns an agent step's files for the engine to store. It has its own token, cannot reach the
  gateway, and `ap up` starts it with the engine. The same program as a `procedure-worker`
  deployment is a procedure step's worker, which runs one fixed procedure and writes only that
  step's files; the launcher does not start these yet (T053a).
- **Engine run steps**: the engine calls each agent step as a Docker Agent `serve api` session,
  one per attempt, as it calls the analyst but with `safety_policy: autonomous` (the analyst stays
  `restricted`; the step container is the boundary), and calls each procedure step's registered worker. It
  sends every check to the check service, except ContractLintPasses, which goes to contract-lint.
  A review finding blocks only when the new witness test it names, run alone by the check
  service with coverage, fails having run the line it names, in a file the fix step may change;
  any other finding is a suggestion
  ([ADR 0039](/decisions/0039-review-findings-confirmed-by-a-new-witness-test.md), superseding the
  rule-test rerun of [ADR 0031](/decisions/0031-review-findings-confirmed-by-rerunning-rule-tests.md)).
  Before every attempt it calls the project worker's `restoreStep`, and after every accepted or
  reused step its `commitStep`, retried with backoff with jitter and otherwise interrupting the
  run (T053e).
  A reused result is reused in any project: `commitStep` writes its stored files there.
  A review round that leaves blocking findings open, with every check but NoBlockingFindingOpen
  passing, is recorded as rejected and carries its files, the witnesses among them; when the
  review step has writable entries, the engine commits them with `commitStep` before the fix's
  `restoreStep`, so the fix and the next round start from them (data-model, "A review round";
  T076).
  Such an open round keeps its result under its reuse key, which covers the round, like an
  accepted step's; a later run of the same input reuses it without running a check, so the fix
  and the later rounds are reused too, and resume after an open round goes straight to the fix
  ([ADR 0040](/decisions/0040-open-review-round-result-kept-and-reused.md); T088).

Built for Story 2 (T053a, split into T053c to T053e, all done):

- **Project worker** (T053c): a third deployment of the check program, which `ap up` starts
  under gVisor with the projects folder writable and its own token. It keeps each new project's
  version history as a git repository in the project folder, with [git](/components/git.md):
  `commitStep` makes one deterministic commit per accepted step (and first writes a reused step's
  stored files during a replay), and `restoreStep` restores the project to its last commit before
  every attempt, which is the run's compensation. The engine calls both (T053e). The engine runs
  no git and mounts no project, and the check service stays read-only
  ([ADR 0032](/decisions/0032-project-version-history-kept-by-a-project-worker.md)).
- **Step containers** (T053d, T053g, T053j): `ap build-workers` starts the step containers of a slice
  and project when a build or replay is asked for, each with only its step's writable paths
  writable (research D5). The review container has `review/`, its witness tests, writable and
  nothing else; every other container mounts `review/` read-only, generate included, on top of its
  writable project root, as `.git` (research D35, T076). It registers them with the engine and wires each procedure step worker's
  token. Each project keeps its own containers, named by slice, step and the project's hash and
  labelled with the project, until `ap down`; `build-workers` refuses while a run of the slice has
  not ended, and `ap resume` registers the run's own containers again; after `ap down` or a reboot,
  `ap build-workers --resume <run>` rebuilds an interrupted run's containers for its own folder,
  writing nothing there
  ([ADR 0036](/decisions/0036-step-containers-per-project-resume-reuses-them.md)).

Still planned: T053a's end-to-end run through `ap build-workers`, a real build and the hand-over
(research D29 and D30, [ADR 0030](/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md)).

Every container runs read-only with all capabilities dropped; the analyst, contract-lint, the check
service and the workers run under [gVisor](/components/gvisor.md). Networks are internal and isolated: `clients`,
`agents` (engine, gateway, step workers), `checks`, `lint` and `analyst` (engine, gateway). Only the
gateway joins `egress`, and only the page's forwarder joins `page-host` to publish 127.0.0.1.
See [Compose](../../src/deploy/compose.yaml) and the [component list](/components/index.md).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md),
[ADR 0028](/decisions/0028-sign-platform-images-locally.md),
[ADR 0030](/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md),
[ADR 0031](/decisions/0031-review-findings-confirmed-by-rerunning-rule-tests.md),
[ADR 0032](/decisions/0032-project-version-history-kept-by-a-project-worker.md),
[ADR 0039](/decisions/0039-review-findings-confirmed-by-a-new-witness-test.md).

Each further building block gets its own concept:

- Agents go in [agents/](/agents/conventions.md).
- Components go in [components/](/components/conventions.md).
- APIs go in [apis/](/apis/conventions.md).
- Data stores go in [data/](/data/conventions.md).

# Cross-cutting concerns

- Security: [security conventions](/security/conventions.md).
- Operations: [operations conventions](/operations/conventions.md).

# Decisions

- [ADR 0001: Adopt OKF for project knowledge](/decisions/0001-adopt-okf.md)
- [ADR 0002: Build the platform on Docker Agent](/decisions/0002-build-on-docker-agent.md)
- [ADR 0003: AHP is the client-facing session protocol](/decisions/0003-ahp-client-session-protocol.md)
- [ADR 0004: Contract-first toolchain, service-owned APIs and versioning](/decisions/0004-contract-first-toolchain.md)
- [ADR 0026: Deterministic workflow engine with agents as state workers](/decisions/0026-deterministic-workflow-engine.md)
- [ADR 0027: Contract-driven frontend](/decisions/0027-contract-driven-frontend.md)
- [ADR 0030: Agent steps on Docker Agent's API server; checks and procedures in a check service](/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md)
- [ADR 0031: A review finding blocks only when its rule's tests fail at the file it names](/decisions/0031-review-findings-confirmed-by-rerunning-rule-tests.md)
- [ADR 0032: The new project's version history is a git repository kept by a project worker](/decisions/0032-project-version-history-kept-by-a-project-worker.md)
- [ADR 0039: A blocking review finding is confirmed by a new failing witness test the review writes](/decisions/0039-review-findings-confirmed-by-a-new-witness-test.md)
- ADRs 0005 to 0025: why each constitution principle exists, starting with [ADR 0005](/decisions/0005-principle-i-needs-before-solutions.md)
