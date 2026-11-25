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
  - id: research-003
    resource: ../../specs/003-one-command-start/research.md
    title: Feature 003 research, R1, R2, R4 and R5
  - id: adr-0061
    resource: /decisions/0061-up-builds-only-changed-images-by-reproducible-digest.md
    title: "ADR 0061: ap up builds only the images that changed, judged by a reproducible digest"
  - id: adr-0062
    resource: /decisions/0062-run-page-reached-remotely-over-tls-behind-a-sign-in-secret.md
    title: "ADR 0062: The run page is reached from other machines only over TLS behind a sign-in secret"
  - id: adr-0063
    resource: /decisions/0063-ap-start-is-the-start-command-ap-up-unchanged.md
    title: "ADR 0063: ap start is the start command; ap up stays unchanged"
  - id: adr-0064
    resource: /decisions/0064-a-cached-digest-spares-the-reproducible-rebuild.md
    title: "ADR 0064: A cached digest spares the reproducible candidate build when nothing changed"
  - id: research-002
    resource: ../../specs/002-durable-execution/research.md
    title: Feature 002 research, R1 to R5, R8 and R10
  - id: adr-0065
    resource: /decisions/0065-temporal-and-postgresql-in-compose.md
    title: "ADR 0065: Temporal's server and PostgreSQL run in Compose, with a schema job that converges them"
  - id: adr-0066
    resource: /decisions/0066-the-run-record-stays-the-engines.md
    title: "ADR 0066: The run record stays the engine's; Temporal keeps only execution state"
  - id: adr-0067
    resource: /decisions/0067-a-run-is-one-temporal-workflow.md
    title: "ADR 0067: A run is one Temporal workflow running the engine's step rules"
  - id: adr-0068
    resource: /decisions/0068-operations-recorded-then-run-as-workflows.md
    title: "ADR 0068: An operation is recorded before it is acknowledged, then runs as a workflow"
  - id: adr-0069
    resource: /decisions/0069-one-retry-rule-declared-in-the-engine-contract.md
    title: "ADR 0069: One retry rule, declared in the engine contract"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-20T23:30:00Z }
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
platform on one machine ([ADR 0026](/decisions/0026-deterministic-workflow-engine.md)). Its durable
execution (resume, checkpoints, retries, the job queue) was the engine's own for the MVP; feature
002 moved it to [Temporal](/components/temporal.md), as
[ADR 0056](/decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md) planned (see
"Shipped by feature 002" below).

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
  analyst agent; the engine calls it for each proposal (research D27). The engine reads each answer
  itself and refuses one that repeats a key, and a revision that drops or moves an item of the
  current proposal, asking the analyst again within the pattern's attempts
  ([ADR 0081](/decisions/0081-platform-refuses-ambiguous-answers-and-revisions-that-lose-items.md)).
- **Contract-lint service**: a small [Spectral](/components/spectral.md) server that lints a slice's
  contract; only the engine reaches it (research D28).
- **Model gateway**: [nginx](/components/nginx.md), the only container with egress and the only one
  that holds the provider key.
- **Terminal client** (`src/cli`): calls the engine API. The read-only **run page** (`src/page`)
  shows a run's record through `page-proxy` on 127.0.0.1 (`ap page` prints its address); the
  end-to-end job builds and checks it (T156).

Shipped by Story 2:

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
  In a review round after the first, the check keeps a confirmed finding blocking only when it
  repeats an open finding or sits on a line the fix before the round changed; the engine sends it
  that scope from the run ([ADR 0055](/decisions/0055-a-later-review-round-blocks-only-within-its-scope.md)).
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
  A result is reused, and a slice's check results are shown, only under the method that judged
  them (feature 006): the method identity is the SHA-256 of a manifest of the files and pins
  declared in `src/engine/method/method.json`, derived by `scripts/method-digest.py`, committed
  and embedded in the engine, which refuses to start without a valid one
  ([ADR 0079](/decisions/0079-method-identity-is-a-digest-of-declared-sources.md)). The reuse key
  covers it, every step outcome event that carries a reuse key names it (`method`), a durable run
  takes it from the engine at each lookup, so a run resumed after an upgrade judges its remaining
  steps under the new method, and a slice whose checks ran under another method, or none, runs
  them again before any read and stores `checks_method`
  ([ADR 0080](/decisions/0080-results-reused-and-checks-kept-only-under-their-method.md)).
  A value the approved contract declares (a limit, bound, default or enum value) has one home in
  the generated code: the generate procedure writes `gen/declared.go`, whose constants
  DeclaredValuesGenerated keeps equal to the approved contract, and the code step is rejected by
  DeclaredValuesNotRepeated when its code repeats one as a literal. Deployment values (addresses,
  credentials) come only from the configuration the template declares in `gen/config.go`
  (`LISTEN_ADDRESS`, default `:8080`), and DeploymentValuesConfigured rejects an address, a
  credential or an environment read in the code step's code; pattern `api-feature-go` runs the
  three checks from 1.1.0 on (feature 009;
  [ADR 0084](/decisions/0084-declared-values-live-in-generated-code-and-are-never-repeated.md),
  [ADR 0085](/decisions/0085-deployment-values-come-only-from-declared-configuration.md),
  [ADR 0086](/decisions/0086-declared-values-file-has-its-own-drift-check.md)).
  The delivered service refuses a request body its contract does not allow before the code step's
  code runs: the pattern overrides oapi-codegen's handler template so every operation is wrapped
  by the template's request check (`gen/requests.go`, standard library only), which reads the
  request shapes the generate procedure derives from the approved contract into
  `gen/request_shapes.go`, kept equal to it by RequestShapesGenerated. It answers a missing body,
  invalid JSON, a name twice, a missing required member, a wrong type or, where the contract
  closes an object, an undeclared member with `400 {"message": ...}`, and passes on only the
  declared members under their exact names. Two fit conditions keep contracts to what it can
  serve: RefusalDeclared (every operation with a request body declares that 400 body) and
  RequestBodiesSupported (JSON bodies of simple shapes). Pattern `api-feature-go` runs them from
  1.2.0 on, and the step evals' MalformedRequestsRefused grader judges the built service (feature
  010; [ADR 0087](/decisions/0087-delivered-service-refuses-malformed-requests-before-its-code.md),
  [ADR 0088](/decisions/0088-request-shapes-are-generated-and-drift-checked.md),
  [ADR 0089](/decisions/0089-contracts-declare-the-refusal-and-simple-request-shapes.md)).

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

The end-to-end build through `ap build-workers` ran on live Haiku on 2026-11-15 (T053a): a run
ended done, and its replay reused every step with no model call (research D29 and D30,
[ADR 0030](/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md)).

Shipped by feature 003 ([spec](../../specs/003-one-command-start/spec.md)):

- **Start command `ap start`**: one command takes a fresh clone to a started platform and is safe
  to run again. It holds a start lock, checks the machine (Docker 28, gVisor's `runsc`, the key
  file and the projects folder, both with defaults) and names every failing prerequisite with its
  fix, then builds only what changed and runs `ap up` unchanged; `ap start --check` shows what it
  would do and changes nothing. `ap up` stays the start command of CI and the test suites
  ([ADR 0063](/decisions/0063-ap-start-is-the-start-command-ap-up-unchanged.md)).
- **Build only what changed** (`src/deploy/build.sh --changed`): each image's reproducible
  candidate build gives its fingerprint; only an image whose fingerprint differs from the one
  recorded at its last signed build in `built.env`, or whose recorded digest no longer verifies,
  is built, signed and recorded. The signing password is asked once, only when something must be
  signed, and never kept. A quick build without the timestamp rewrite, whose digest each signed
  build records paired with the fingerprint, spares the reproducible build when nothing changed
  ([ADR 0064](/decisions/0064-a-cached-digest-spares-the-reproducible-rebuild.md))
  ([ADR 0061](/decisions/0061-up-builds-only-changed-images-by-reproducible-digest.md),
  [BuildKit reproducible builds](/references/buildkit-reproducible-builds.md)).
- **Remote run page** (`ap up --listen`, passed on by `start`): a Compose override
  (`compose.remote.yaml`) changes the page forwarder alone, which then publishes the read-only
  run page on the given address over TLS, behind HTTP Basic authentication with a secret new per
  session, rate-limited per address and answering only the given names. Each change of remote
  access is appended to `access.log`, without the secret. The engine API and the terminal client
  stay on the machine
  ([ADR 0062](/decisions/0062-run-page-reached-remotely-over-tls-behind-a-sign-in-secret.md),
  [nginx](/components/nginx.md)).

Sources: [Feature 003 research](../../specs/003-one-command-start/research.md),
[ADR 0061](/decisions/0061-up-builds-only-changed-images-by-reproducible-digest.md),
[ADR 0062](/decisions/0062-run-page-reached-remotely-over-tls-behind-a-sign-in-secret.md),
[ADR 0063](/decisions/0063-ap-start-is-the-start-command-ap-up-unchanged.md),
[ADR 0064](/decisions/0064-a-cached-digest-spares-the-reproducible-rebuild.md).

Shipped by feature 002 ([spec](../../specs/002-durable-execution/spec.md)):

- **Durable services**: [Temporal](/components/temporal.md)'s server (`durable`, all its services
  in one process) and [PostgreSQL](/components/postgresql.md) (`durable-store`, its data on the
  volume `durable-store`, which `ap down` keeps), pinned by digest; the schema job
  (`durable-schema`) converges both databases before the server starts, and the engine registers
  the namespace `ap` when it connects. `ap up` makes PostgreSQL's password once per install
  ([ADR 0065](/decisions/0065-temporal-and-postgresql-in-compose.md),
  [ADR 0070](/decisions/0070-engine-registers-the-namespace-password-folder-private.md)).
- **A run is one workflow** (`src/engine/durable`): the engine's step rules as workflow code on
  task queue `runs`; every call it makes (step workers, checks, the project worker, the record, the
  reuse store) is an activity with a heartbeat. When the engine stops, the run continues by itself
  once it is back, making only the interrupted attempt again with the same number and never an
  accepted step twice; nobody types `resume`. Step worker registrations are kept in the engine
  store, without tokens, so a run continues after an engine restart
  ([ADR 0067](/decisions/0067-a-run-is-one-temporal-workflow.md),
  [ADR 0071](/decisions/0071-step-worker-registrations-kept-in-the-engine-store.md)).
- **The run record stays the engine's**, with its published schema unchanged: each event is
  written once per sequence number by an idempotent activity, and Temporal holds IDs, hashes,
  numbers and short reasons only, kept 24 h
  ([ADR 0066](/decisions/0066-the-run-record-stays-the-engines.md)).
- **Operations**: every state-changing request is recorded in the engine store with its request
  body before its 202, then runs as a workflow named by the operation's ID on task queue
  `operations`; after a stop its work is rebuilt from the record and the body, and its end is
  written to the record. An ended operation stays followable 24 h; its Idempotency-Key is held 24 h
  from acceptance. While Temporal cannot be reached, a state-changing request answers 503 with
  `Retry-After` and writes nothing
  ([ADR 0068](/decisions/0068-operations-recorded-then-run-as-workflows.md),
  [ADR 0072](/decisions/0072-operation-request-bodies-kept-in-the-engine-store.md),
  [ADR 0073](/decisions/0073-idempotency-key-held-24-h-from-acceptance.md)).
- **One retry rule** (`src/retry`): capped exponential backoff with jitter, declared once in the
  engine contract's `x-limits`, used by the workflows (with durable timers), the in-process
  operations and the terminal client's resend wait; Temporal's own activity retries are off
  ([ADR 0069](/decisions/0069-one-retry-rule-declared-in-the-engine-contract.md)).
- **Erasing a slice** deletes its runs' and operations' workflows from Temporal, then their
  operation records, then the slice from the engine store.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md),
[ADR 0065](/decisions/0065-temporal-and-postgresql-in-compose.md),
[ADR 0066](/decisions/0066-the-run-record-stays-the-engines.md),
[ADR 0067](/decisions/0067-a-run-is-one-temporal-workflow.md),
[ADR 0068](/decisions/0068-operations-recorded-then-run-as-workflows.md),
[ADR 0069](/decisions/0069-one-retry-rule-declared-in-the-engine-contract.md).

Every container runs read-only with all capabilities dropped; the analyst, contract-lint, the check
service and the workers run under [gVisor](/components/gvisor.md). Networks are internal and isolated: `clients`,
`agents` (engine, gateway, step workers), `checks`, `lint` and `analyst` (engine, gateway), `durable` (engine, Temporal's server) and
`durable-store` (the server, the schema job, PostgreSQL; feature 002). Only the
gateway joins `egress`, and only the page's forwarder joins `page-host` to publish 127.0.0.1, or,
with `ap up --listen`, the given address (feature 003).
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
- [ADR 0061: ap up builds only the images that changed, judged by a reproducible digest](/decisions/0061-up-builds-only-changed-images-by-reproducible-digest.md) (superseded in part by ADR 0063 and ADR 0064)
- [ADR 0062: The run page is reached from other machines only over TLS behind a sign-in secret](/decisions/0062-run-page-reached-remotely-over-tls-behind-a-sign-in-secret.md)
- [ADR 0063: ap start is the start command; ap up stays unchanged](/decisions/0063-ap-start-is-the-start-command-ap-up-unchanged.md)
- [ADR 0064: A cached digest spares the reproducible candidate build when nothing changed](/decisions/0064-a-cached-digest-spares-the-reproducible-rebuild.md)
- [ADR 0065: Temporal's server and PostgreSQL run in Compose, with a schema job that converges them](/decisions/0065-temporal-and-postgresql-in-compose.md)
- [ADR 0066: The run record stays the engine's; Temporal keeps only execution state](/decisions/0066-the-run-record-stays-the-engines.md)
- [ADR 0067: A run is one Temporal workflow running the engine's step rules](/decisions/0067-a-run-is-one-temporal-workflow.md)
- [ADR 0068: An operation is recorded before it is acknowledged, then runs as a workflow](/decisions/0068-operations-recorded-then-run-as-workflows.md)
- [ADR 0069: One retry rule, declared in the engine contract](/decisions/0069-one-retry-rule-declared-in-the-engine-contract.md)
- [ADR 0070: The engine registers Temporal's namespace; the store's password file is private by its folder](/decisions/0070-engine-registers-the-namespace-password-folder-private.md)
- [ADR 0071: Step worker registrations are kept in the engine store](/decisions/0071-step-worker-registrations-kept-in-the-engine-store.md)
- [ADR 0072: An operation's request body is kept in the engine store, and its work is rebuilt from its record](/decisions/0072-operation-request-bodies-kept-in-the-engine-store.md)
- [ADR 0073: An Idempotency-Key is held 24 h from acceptance; an ended operation stays followable 24 h after its end](/decisions/0073-idempotency-key-held-24-h-from-acceptance.md)
- [ADR 0074: Long acceptance suites run once, split across parallel CI jobs](/decisions/0074-long-ci-suites-split-across-parallel-jobs.md)
- ADRs 0005 to 0025: why each constitution principle exists, starting with [ADR 0005](/decisions/0005-principle-i-needs-before-solutions.md)
