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
  - id: plan-011
    resource: ../../specs/011-capture-pattern/plan.md
    title: Feature 011 plan, research R1 to R7
  - id: data-model-011
    resource: ../../specs/011-capture-pattern/data-model.md
    title: Feature 011 data model
  - id: adr-0090
    resource: /decisions/0090-captured-pattern-fills-agent-steps-from-accepted-files.md
    title: "ADR 0090: A captured pattern fills its agent steps from the accepted files"
  - id: adr-0093
    resource: /decisions/0093-captured-pattern-used-only-under-its-proven-method.md
    title: "ADR 0093: A captured pattern is used only under its proven method"
  - id: adr-0097
    resource: /decisions/0097-proof-builds-a-slice-made-from-the-captured-copy.md
    title: "ADR 0097: A proof builds a slice made from the captured pattern's copy"
  - id: plan-012
    resource: ../../specs/012-run-variance/plan.md
    title: Feature 012 plan, research R1 to R9
  - id: data-model-012
    resource: ../../specs/012-run-variance/data-model.md
    title: Feature 012 data model
  - id: adr-0102
    resource: /decisions/0102-trial-set-runs-fresh-builds-and-derives-its-result.md
    title: "ADR 0102: A trial set runs fresh builds of a slice and derives its result from their records"
  - id: adr-0103
    resource: /decisions/0103-worker-registrations-name-their-project.md
    title: "ADR 0103: Step-worker registrations name their project"
  - id: adr-0106
    resource: /decisions/0106-a-failed-trial-set-ends-failed.md
    title: "ADR 0106: A trial set whose operation fails ends failed"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-28T15:00:00Z }
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

Shipped by feature 011 ([spec](../../specs/011-capture-pattern/spec.md)):

- **Captured patterns** (`src/engine/captured`, `/captured-patterns` in the engine API): a run of
  the base pattern that ended done is captured as a pattern that builds later slices of the same
  kind with no model call. Its declaration is the base pattern with each agent step's worker
  replaced by the fixed procedure `fill`, one attempt and one review round; the review step keeps
  only the checks that need no findings and adds AllTestsPass, so the witness tests it writes run
  against the filled code ([ADR 0101](/decisions/0101-filled-review-step-runs-every-test.md)). Its template is the run's accepted `tests`, `code` and
  `witnesses` outputs; `gen/**` is derived again from each slice's contract by the generate
  procedure. Its parameters are the source slice's names (lowercase, capitalised and uppercase
  forms), declared values and any other literal value the developer adds, each with every place it
  occurs in the slice and in the template's contents and paths; at most 32 parameters, 10000
  places each, values of at most 64 characters. A captured pattern never changes (always
  `1.0.0`; a change is a new capture), and an erased ID is never given again
  ([ADR 0090](/decisions/0090-captured-pattern-fills-agent-steps-from-accepted-files.md),
  [ADR 0094](/decisions/0094-any-literal-value-may-be-a-parameter.md),
  [ADR 0095](/decisions/0095-captured-patterns-never-change-values-keep-their-kind.md)).
- **Kept in the engine store** as the developer's data: one record per captured pattern, with a
  copy of the source slice's approved parts, and the template's artifacts held by the captured
  pattern as a second holder, so they stay when the source slice is erased. Capture, parameters,
  the proof slice, approval, withdrawal and erasure are operations, one at a time per captured
  pattern ([ADR 0091](/decisions/0091-captured-patterns-kept-in-the-engine-store.md)).
- **Fit by exact substitution**: one package, `engine/captured`, owns the rules. A slice fits a
  captured pattern when filling the captured copy with one value per parameter, each of its
  parameter's kind, gives exactly the approved slice, no two values are equal and every filled
  path is distinct and inside its step's writable entries; a later name value that is a Go keyword
  is refused. A fit picks values, never code
  ([ADR 0092](/decisions/0092-captured-pattern-fit-is-exact-substitution.md)).
- **The fill worker**: a `fill` step runs in the run workflow as the activity `WriteProject`, with
  no step container: the engine fills the template outputs with the run's values and the project
  worker writes them with the check contract's new `writeStep`; the check service reads them back
  with `readStepFiles`, the base pattern's checks judge them, and `commitStep` records them, as on
  the agent path. A refusal a repeat cannot pass hands the run over; nothing repairs it with a
  model. The reuse key of a `fill` step also covers the hash of the run's values.
- **The pattern choice** at `startRun`: a request may name a captured pattern or the base pattern;
  otherwise the most recently approved usable captured pattern that fits is used, else the base
  pattern. A captured pattern is usable only while it is approved and its latest proof passed
  under the current method. The run and its `RunStarted` record the pattern, the reason (`base`,
  `fits`, `chosen`), the values, the source run and each fitting captured pattern not used, with
  why (`needs-proof`, `refused-value`); the run page and the terminal client show them. Resume and
  take-over keep the run's pattern; a replay uses it again only while it is usable
  ([ADR 0093](/decisions/0093-captured-pattern-used-only-under-its-proven-method.md),
  [ADR 0098](/decisions/0098-proof-slice-marked-and-runs-keep-their-pattern.md)).
- **The proof slice and the proof**: a captured pattern's proof slice is an approved slice the
  engine makes from its copy (`Proof of <captured pattern>`, `proof_of`; history kind
  `made-for-proof`), never reused by `createSlice`; asking again restores any part changed since.
  A proof is an ordinary build of that slice under the captured pattern with the source values,
  reusing no earlier result, into a new project folder (`ap prove <captured pattern> <project>`).
  It passes when the run ends done and its `tests`, `code` and `witnesses` outputs equal the
  template; generated code is judged by its checks, and a hand-over fails it at once. The
  developer approves only after a passing proof under the current method
  ([ADR 0097](/decisions/0097-proof-builds-a-slice-made-from-the-captured-copy.md), superseding
  [ADR 0096](/decisions/0096-proof-step-workers-keyed-by-captured-pattern.md);
  [ADR 0099](/decisions/0099-proof-compares-filled-outputs-and-paths-are-filled.md)).
- **Withdrawal and erasure**: a withdrawn captured pattern is used by no new run; runs already
  started finish on it. Erasure is refused while a run built from it or its proof has not ended;
  it removes its proof slice, that slice's runs and the artifacts only it holds, and keeps the
  records of other runs built from it.
- **Contract changes**: additive, except the engine contract's new error codes, operation kinds
  and slice history kind, added in place to `2026-11-12-preview`
  ([ADR 0100](/decisions/0100-preview-contract-gains-enum-values-in-place.md)).

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md),
[Feature 011 data model](../../specs/011-capture-pattern/data-model.md),
[ADR 0090](/decisions/0090-captured-pattern-fills-agent-steps-from-accepted-files.md),
[ADR 0093](/decisions/0093-captured-pattern-used-only-under-its-proven-method.md),
[ADR 0097](/decisions/0097-proof-builds-a-slice-made-from-the-captured-copy.md).

Shipped by feature 012 ([spec](../../specs/012-run-variance/spec.md)):

- **Trial sets** (`src/engine/trials`, `/slices/{sliceId}/trial-sets` and `/trial-sets/{trialSetId}`
  in the engine API, `ap trials`): a trial set builds an approved slice k times (1 to 5, default 3)
  into the new projects `<prefix>-1` to `<prefix>-<k>`, one run after another, so the developer
  sees how much the builds vary. It is one operation of kind `run-trials`; each trial is an
  ordinary run, as `startRun` without a pattern starts one, that reuses no earlier accepted result
  and carries `trial_set` and `trial` on its `RunStarted`. The trial's run ID is written into the
  set before the run starts, so a repeat after a stop follows or starts that same run
  ([ADR 0102](/decisions/0102-trial-set-runs-fresh-builds-and-derives-its-result.md)).
- **Bounds**: a trial starts only while the set's cost so far plus one run's budget stays within
  $2; a set still running after 24 hours cancels its trial and stops. Before each trial the set
  also stops when the slice's approved parts changed since acceptance or a project's step workers
  are gone. The limits are the engine contract's `x-limits` (`trials-max`, `trials-default`,
  `trial-set-cost-usd`, `trial-set-hours`), pinned to the engine by a contract test.
- **A waiting trial is handed over**: a trial that runs out of attempts, review rounds or budget is
  handed over by its set, counted as not done, and the next trial starts; an interrupted trial is
  waited for within the 24 hours
  ([ADR 0104](/decisions/0104-trial-waiting-for-developer-is-handed-over.md)).
- **A failed set ends failed**: when the set's operation fails, its running trial is cancelled and
  the set ends `failed` with the operation's reason. A set a crash left `running` is ended so by
  the next start, delete or engine start; reads write nothing
  ([ADR 0106](/decisions/0106-a-failed-trial-set-ends-failed.md)).
- **The result is derived, never stored**: package `trials` computes it from the trials' run
  records on each read: done out of ended, the unstable mark, per step and per trial the lowest,
  median and highest attempts, review rounds, seconds without interrupted time, tokens and cost,
  which steps' files differ, and the change since the slice's latest earlier set that ended or
  stopped with an ended trial, naming both methods when they differ. A measure no trial carries is not available, never
  0; a step whose worker is a procedure records no usage
  ([ADR 0107](/decisions/0107-run-records-carry-usage-only-for-model-steps.md)). Sets are kept
  in the engine store with their slice and erased with it.
- **Registrations by project**: a step worker registers for a slice, step and project, and a run
  uses only its own project's workers, so the k projects of a set have ready workers at once
  ([ADR 0103](/decisions/0103-worker-registrations-name-their-project.md)).
- **Contract changes**: additive, except the operation kind `run-trials`, added in place to
  `2026-11-12-preview`
  ([ADR 0105](/decisions/0105-preview-contract-gains-run-trials-in-place.md)).

Sources: [Feature 012 plan](../../specs/012-run-variance/plan.md),
[Feature 012 data model](../../specs/012-run-variance/data-model.md),
[ADR 0102](/decisions/0102-trial-set-runs-fresh-builds-and-derives-its-result.md),
[ADR 0103](/decisions/0103-worker-registrations-name-their-project.md),
[ADR 0106](/decisions/0106-a-failed-trial-set-ends-failed.md).

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
- [ADR 0090: A captured pattern fills its agent steps from the accepted files](/decisions/0090-captured-pattern-fills-agent-steps-from-accepted-files.md)
- [ADR 0091: Captured patterns are kept in the engine store](/decisions/0091-captured-patterns-kept-in-the-engine-store.md) (superseded in part by ADR 0095)
- [ADR 0092: A slice fits a captured pattern by exact substitution](/decisions/0092-captured-pattern-fit-is-exact-substitution.md) (superseded in part by ADR 0094 and ADR 0095)
- [ADR 0093: A captured pattern is used only under its proven method](/decisions/0093-captured-pattern-used-only-under-its-proven-method.md)
- [ADR 0094: Any literal value of a slice may be a captured pattern's parameter](/decisions/0094-any-literal-value-may-be-a-parameter.md)
- [ADR 0095: Captured patterns never change, and parameter values keep their kind](/decisions/0095-captured-patterns-never-change-values-keep-their-kind.md)
- [ADR 0096: A proof's step workers are keyed by its captured pattern](/decisions/0096-proof-step-workers-keyed-by-captured-pattern.md) (superseded by ADR 0097)
- [ADR 0097: A proof builds a slice made from the captured pattern's copy](/decisions/0097-proof-builds-a-slice-made-from-the-captured-copy.md)
- [ADR 0098: A proof slice is marked, and continued runs keep their pattern](/decisions/0098-proof-slice-marked-and-runs-keep-their-pattern.md)
- [ADR 0099: A proof compares the filled outputs, and template paths are filled](/decisions/0099-proof-compares-filled-outputs-and-paths-are-filled.md)
- [ADR 0100: The engine's preview contract gains enum values in place](/decisions/0100-preview-contract-gains-enum-values-in-place.md)
- [ADR 0101: A captured pattern's filled review step runs every test](/decisions/0101-filled-review-step-runs-every-test.md)
- [ADR 0102: A trial set runs fresh builds of a slice and derives its result from their records](/decisions/0102-trial-set-runs-fresh-builds-and-derives-its-result.md)
- [ADR 0103: Step-worker registrations name their project](/decisions/0103-worker-registrations-name-their-project.md)
- [ADR 0104: A trial that waits for the developer is handed over by its set](/decisions/0104-trial-waiting-for-developer-is-handed-over.md)
- [ADR 0105: The engine's preview contract gains the operation kind run-trials in place](/decisions/0105-preview-contract-gains-run-trials-in-place.md)
- [ADR 0106: A trial set whose operation fails ends failed](/decisions/0106-a-failed-trial-set-ends-failed.md)
- [ADR 0107: A run's events carry usage only for steps that reach a model](/decisions/0107-run-records-carry-usage-only-for-model-steps.md)
- ADRs 0005 to 0025: why each constitution principle exists, starting with [ADR 0005](/decisions/0005-principle-i-needs-before-solutions.md)
