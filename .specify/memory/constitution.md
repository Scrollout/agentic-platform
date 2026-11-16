# Agentic Platform Constitution

## Core Principles

### I. Needs Before Solutions

The Agentic Platform is a new system with its own architecture, design, and code.

- A component, layout, term, or rule MUST NOT be adopted because a sibling or predecessor project
  (such as the Email Gateway) or another product has it.
- Such material MAY be cited only as background, never as a requirement or as the reason for a
  design choice.
- Requirements MUST be stated as needs, each with an ID, in `docs/overview/problem-space.md`, and
  settled by specs. Before the first design spec, the platform's purpose and non-goals MUST be
  stated there as needs.

Rationale: [ADR 0005](../../docs/decisions/0005-principle-i-needs-before-solutions.md).

### II. Two Planes, Agent Work Stays in the Runtime Plane

The system is split into a control plane, which owns desired state and every management
operation, and a runtime plane, which executes agent runs and carries model, tool, and memory
traffic.

- The runtime plane MUST stay up and keep executing and serving runs while the control plane is
  not operational, on its last distributed configuration, policy, and credentials.
- For every distributed item that can expire during an outage (credentials, tokens, policy), the
  owning spec MUST state whether the runtime plane fails closed or continues on last-known state.
- Prompts, completions, tool arguments and results, retrieved documents, memory, and run content
  MUST live in the runtime plane and MUST NOT pass through the control plane.
- Only these flows MAY cross between the planes: configuration and policy from the control plane;
  status from the runtime plane; audit events that carry metadata only; and the endpoint
  information clients use to discover runtime endpoints.
- Every component MUST belong to exactly one plane. A capability that needs both MUST be split
  into two components joined by a contract.

Rationale: [ADR 0006](../../docs/decisions/0006-principle-ii-two-planes-agent-work-stays-in-the-runtime-plane.md).

### III. API-First, Declarative Management

- Every management action MUST go through the control-plane API. Nothing reads management
  configuration from a host file system.
- The Portal, CLI, SDKs, direct API calls, and declarative templates MUST be clients of that same
  API. An interface MUST NOT exist without the API operation behind it, and MUST NOT bypass or
  reimplement the API's validation, authorization, or auditing.
- Configuration MUST be declarative. Agents, tool bindings, model routes, guardrail policies, and
  memory stores MUST be resources with published, versioned schemas, and every submission MUST be
  validated against its schema before it is accepted.
- An agent definition MUST be a Docker Agent configuration
  ([0002](../../docs/decisions/0002-build-on-docker-agent.md)), stored as an immutable, versioned
  revision. Every run MUST record the exact revision (by digest) it executed.
- Schema validation at the API and the domain types behind it MUST derive from the same
  contract-declared rule: either the type reads the rule from the contract, or a contract test
  proves they match.
- Resources MUST separate desired state from observed state, and the control plane MUST reconcile
  them and report drift.

Rationale: [ADR 0007](../../docs/decisions/0007-principle-iii-api-first-declarative-management.md).

### IV. Asynchronous Operations on Worker Pools

- Every state-changing management operation MUST be accepted by the API, recorded, queued, and
  answered with an operation reference. Workers MUST execute it. Queries MAY be answered
  synchronously.
- Every operation MUST have a queryable lifecycle covering acceptance, progress, outcome, and
  cancellation. Clients MUST follow it instead of blocking on one call.
- Workers MUST be idempotent. Failed work MUST be retried with exponential backoff and jitter and
  then dead-lettered visibly. Operations on the same resource MUST follow defined ordering and
  concurrency rules.
- Workers MUST be grouped into pools, one per workload class (for example management operations,
  agent runs, evals, ingestion and embedding). A pool consumes one queue, uses its own workload
  identity, and scales on its own.

Rationale: [ADR 0008](../../docs/decisions/0008-principle-iv-asynchronous-operations-on-worker-pools.md).

### V. Zero-Trust Identity and Separate Permissions

- Components MUST communicate only through published, versioned, authenticated APIs and MUST NOT
  reach into another component's store.
- Every component, worker pool, and agent run MUST have its own workload identity. Every
  service-to-service call MUST be authenticated with it and authorized against what it may do.
  Network location and shared static secrets MUST NOT be a basis for trust. A component that
  cannot use workload identity natively MUST be fronted by an adapter that does.
- An agent acting for a user MUST use a delegated token that names both the user and the agent.
  It MUST NOT hold or replay the user's own credential.
- Tool credentials MUST be per tool, audience-bound, short-lived, scoped to tenant and user, and
  obtained when needed. A service MUST NOT accept a token that was not issued to it.
- Roles MUST carry management permissions, data permissions, and agent-action permissions (which
  tools an agent may call) separately. A management role, however broad, MUST NOT imply data
  access.
- Access to run content MUST require the data permission and MUST emit an audit event that
  carries metadata only.
- Each plane MUST throttle per principal, independently of the other.

Rationale: [ADR 0009](../../docs/decisions/0009-principle-v-zero-trust-identity-and-separate-permissions.md).

### VI. Adopt Mature, Free Open Source; Never Rewrite It

- Docker Agent is the agent definition format and runtime
  ([0002](../../docs/decisions/0002-build-on-docker-agent.md)). Missing capabilities MUST be added
  through its extension points or upstream, not by forking it or rebuilding what it provides.
- Where another mature, stable open-source project does the job (gateway, queue, store, sandbox,
  scanner, and so on), it MUST be used and MUST NOT be rewritten.
- Every candidate MUST be evaluated with the OpenSSF Concise Guide, its Scorecard result, and
  project-health signals before custom code is proposed. Custom code for a capability that OSS
  covers MUST carry a written justification in the plan. A choice that is hard to reverse MUST
  also get a decision.
- Every component MUST be under an OSI-approved license and MUST NOT require a paid license,
  edition, or subscription for a feature we depend on. Model weights and datasets MUST have their
  license checked against intended use; a non-OSI model license MUST be approved in a decision.
- A paid model API MAY be used only behind the provider interface (Principle VII), so it stays
  replaceable.

Rationale: [ADR 0010](../../docs/decisions/0010-principle-vi-adopt-mature-free-open-source-never-rewrite-it.md).

### VII. Pluggable Providers

The platform is extended through providers that the control plane registers and governs.

- Provider kinds are at least model, tool, memory and retrieval, and sandbox. Each kind MUST have
  a published provider-kind contract. Tool providers MUST use the Model Context Protocol rather
  than a contract of our own.
- A provider MUST be registered through the control plane under its own namespace, with a pinned
  version or digest, a trust level, and the scoped credentials it may use. It MUST NOT be
  reachable for management except through the control plane.
- Each agent MUST be given an explicit allowlist of the tools it may call. Tool names,
  descriptions, and annotations from a provider MUST be treated as untrusted unless the provider
  is trusted, and MUST be re-reviewed when they change.
- Providers MUST be addable, replaceable, and removable by registration and configuration without
  changing the platform's core. Where several providers of one kind run side by side, the rule
  that combines or chooses between them (routing, fallback) MUST be specified.
- Provider-neutral settings MUST be modeled by the core. Provider-specific settings MUST go
  through the provider's own resource types or an explicit extension point.

Rationale: [ADR 0011](../../docs/decisions/0011-principle-vii-pluggable-providers.md).

### VIII. Containers and Sandboxed Execution

- Every component, worker pool, provider, and adopted service MUST run as an OCI container. There
  MUST be no install directly on a host.
- The platform MUST run on a plain container engine with Compose. Kubernetes is optional, and a
  core function MUST NOT depend on it. Packaging for every target MUST come from one definition,
  and its security settings MUST map onto the Kubernetes Restricted Pod Security profile.
- Images MUST be immutable, minimal, and free of environment-specific configuration. They MUST
  run as non-root with a read-only root file system wherever the component allows it, keep state
  only in declared volumes or external stores, and be updated by replacement.
- Containers MUST NOT run privileged. A needed host capability MUST be granted alone and declared.
  The platform MUST need nothing from the host beyond a container runtime.
- Code that an agent generates or runs, and any tool that executes commands, MUST run in a
  sandbox stronger than a plain container (microVM or user-space kernel), with no egress and no
  credentials by default, CPU, memory and time limits, and an ephemeral file system.
- Every image we publish MUST be signed, carry an SBOM and build provenance, and be verified at
  deploy time.

Rationale: [ADR 0012](../../docs/decisions/0012-principle-viii-containers-and-sandboxed-execution.md).

### IX. Knowledge Stays True

- `docs/` is the durable record of the system, kept as an OKF knowledge bundle
  ([0001](../../docs/decisions/0001-adopt-okf.md)). A change that introduces or alters a durable
  fact MUST update the affected concept in the same change.
- Every architecture choice that outlives a feature MUST be recorded in `docs/decisions/`, linked
  to its origin: the plan that made it, or the project owner's request. An accepted decision MUST
  NOT be rewritten; it is superseded by a new one that links back.
- Agents MUST NOT mark their own work as human-verified.

Rationale: [ADR 0013](../../docs/decisions/0013-principle-ix-knowledge-stays-true.md).

### X. No Silent Loss of Accepted Runs

- A run MUST be acknowledged only after it is durably recorded. Any failure before that MUST be
  returned to the caller as retryable.
- Every accepted run MUST end in exactly one recorded terminal state: succeeded, failed,
  cancelled, timed out, or rejected by policy.
- Run progress MUST be checkpointed after each model or tool step, so that a restart resumes the
  run and never repeats a completed side effect.
- Overload, provider rate limits, and quota exhaustion MUST cause back-pressure (queueing,
  deferral, or a retryable rejection), never loss.
- Every run MUST be bounded by step, token, cost, and time budgets. Waiting for a human MUST be a
  durable state with a timeout.
- Upgrades and restarts MUST NOT drop accepted runs.

Rationale: [ADR 0014](../../docs/decisions/0014-principle-x-no-silent-loss-of-accepted-runs.md).

### XI. Agent Data Privacy

Protected content means prompts, system instructions, conversations, tool arguments and results,
retrieved documents, agent memory, embeddings, files, and traces that contain any of these.

- Protected content MUST be kept only as long as a configured retention allows.
- It MUST be encrypted in transit, between all components and to all clients, and at rest.
- Logs, metrics, traces, and audit records MUST NOT contain protected content by default.
  Capturing it MUST be an explicit per-tenant choice, stored separately with its own access
  control and retention, and referenced from telemetry rather than embedded in it.
- Retention and erasure MUST be enforceable per tenant and per user, across every store that holds
  protected content, including memory, vector indexes, caches, eval datasets, and trace stores.
- Protected content MUST NOT be sent to a model provider that trains on it or keeps it beyond the
  configured retention. Vendor product telemetry that can carry it MUST be disabled.

Rationale: [ADR 0015](../../docs/decisions/0015-principle-xi-agent-data-privacy.md).

### XII. Tenant Isolation

- Every configuration, run, and data access MUST be scoped to exactly one tenant, in both planes.
- The tenant MUST come from the caller's authenticated identity, never from a request parameter
  alone, and every store query MUST be scoped by tenant.
- Retrieval and memory MUST be isolated by tenant in the store itself (namespace, collection, or
  row-level security), not by a filter in the prompt. Every cache that holds model input or output
  MUST include the tenant, and the user where relevant, in its key.
- Agent runs of different tenants MUST NOT share a runtime process, sandbox, working directory, or
  credential set.
- Every API that reads or changes tenant data MUST have a test proving that another tenant's data
  cannot be reached, including through retrieval, memory, caches, and an agent instructed to fetch
  it.
- Roles that act across tenants MUST be separate from tenant roles, explicitly granted, and
  audited.

Rationale: [ADR 0016](../../docs/decisions/0016-principle-xii-tenant-isolation.md).

### XIII. Secure Supply Chain

- Dependencies, base images, Docker Agent itself, models, agent artifacts, and MCP servers MUST be
  pinned by version and digest, and updated through automated pull requests.
- CI MUST run dependency, license, secret, and static analysis on every change.
- Builds MUST be reproducible from source and record their provenance, targeting SLSA Build
  Level 3. Published artifacts MUST be signed and carry an SBOM.
- Agent artifacts, models, and MCP servers MUST come from an allowlisted source and have their
  signature or provenance verified before use. Prompts, system instructions, and tool descriptions
  MUST be versioned and reviewed like code.
- A known exploitable vulnerability in a shipped component MUST be fixed or mitigated within the
  time a decision sets per severity, with vulnerabilities in the CISA KEV catalog fixed first.

Rationale: [ADR 0017](../../docs/decisions/0017-principle-xiii-secure-supply-chain.md).

### XIV. Safe Defaults and Least Agency

- Tools MUST be denied by default and allowlisted per agent. Read-only access MUST be the
  default.
- A high-impact or irreversible action MUST require human approval, enforced by the platform
  outside the model and bound to the exact call and arguments.
- Content from tools, retrieval, the web, files, or other agents MUST be treated as data, never as
  instructions. Defences MUST assume prompt injection will succeed and limit what it can reach
  through authorization, not through filters alone.
- Agent egress MUST be denied by default and allowlisted per destination. Private address ranges
  and cloud metadata endpoints MUST be blocked.
- The platform, not the agent author, MUST own the safety mode, permissions, hooks, and egress
  policy. An agent definition MUST NOT widen them.
- When a guardrail, classifier, or policy check fails or times out, the action MUST be blocked.
- Every network-facing endpoint MUST be rate-limited, and every tenant, agent, and run MUST have
  token, cost, step, and time budgets.
- A setting that is less safe than the default MUST be an explicit, audited choice.

Rationale: [ADR 0018](../../docs/decisions/0018-principle-xiv-safe-defaults-and-least-agency.md).

### XV. Compatible Public APIs

- The public API, CLI, SDKs, declarative schemas, streaming event formats, and agent protocol
  surfaces (such as AHP, A2A, and MCP) MUST follow a published versioning and deprecation policy. A
  breaking change MUST get a new version and a deprecation period for the old one, signalled with
  the `Deprecation` and `Sunset` headers.
- Every mutating call MUST accept an idempotency key with a stated retention window and a defined
  response to a reused key with a different payload. Every list endpoint MUST be paginated with
  opaque page tokens.
- A model version change is a behaviour change. Each agent revision MUST pin its model versions.

Rationale: [ADR 0019](../../docs/decisions/0019-principle-xv-compatible-public-apis.md).

### XVI. Human Control of Agents

- An agent MUST NOT merge to the default branch, change CI or security checks, add a dependency,
  change secrets, or amend this constitution without a human approval recorded in the pull
  request or commit. Branch protection and CODEOWNERS MUST enforce this.
- Every agent change MUST identify the agent and model in a commit trailer. An agent MUST NOT
  write a human's sign-off.
- Every dependency an agent adds MUST be checked to exist, be the intended package, and be
  maintained.
- Agent credentials in the repository and CI MUST have least privilege and no access to secrets.

Rationale: [ADR 0020](../../docs/decisions/0020-principle-xvi-human-control-of-agents.md).

### XVII. Declare, Derive, Converge

This principle governs how the project is built and run: the repository, deployment packaging,
operational routines, checks, and developer tooling. Principle III governs the product's
management configuration.

- **Declare the what once.** Every fact MUST have one home, a structured file that states the
  desired state. Declarations MUST use an existing format with a schema, such as YAML or JSON. A
  new configuration language MUST NOT be invented. Prompts, tool schemas, eval suites, and
  guardrail policies are declarations.
- **Derive the rest.** Anything that follows from a declaration, such as documents, identifiers,
  grants, SDKs, or lists, MUST be generated from it or checked against it. It MUST NOT be kept by
  hand in a second place. Code that follows from a contract MUST be generated when a mature
  generator exists for the stack; generated code MUST NOT be edited by hand, and a drift check
  MUST prove it matches its contract.
- **Converge, don't sequence.** An operation MUST be one command that makes reality match the
  declarations and is safe to run again. Running it twice MUST change nothing the second time.
  Using it MUST NOT require knowing the order of its steps or when a step is needed.
- **Prove agreement.** The test command MUST prove that declarations agree with each other. Every
  converging command MUST be able to show, without changing anything, how the running system
  differs from the declarations.
- **Keep the how small.** Imperative code MUST be a generic engine, one per kind of thing, with
  tests. What varies MUST live in declarations, not in more code. Every engine MUST say what it
  will do and what it did.
- **A person decides only what cannot be undone.** A step that destroys data MUST be shown before
  it runs and MUST need an explicit confirmation. Every other step MUST run without one.
- **The tool's mechanism first.** A tool's own declarative mechanism, such as Compose, Docker
  Agent configuration, migrations, or Kubernetes, MUST be used before writing an engine of our
  own.
- **Limits.** Declaring is required only for what varies or repeats; a one-off stays a plain
  command. State that is history, such as database migrations, stays an ordered history, and the
  migration runner converges it.

Rationale: [ADR 0021](../../docs/decisions/0021-principle-xvii-declare-derive-converge.md).

### XVIII. Abstractions Say What, Not How

An abstraction is an interface that lets its caller get something done without knowing how it is
done. This principle governs every interface we write: contracts, READMEs, signatures, commands,
schemas, and tools given to models. An adopted component's interface is given.

- **Simple.** The interface MUST open with what it does for the caller, in one sentence that
  names no part of the implementation. A name, parameter, error, or option that exists only
  because of how it is built MUST NOT appear in it.
- **Sound.** Every promise the interface makes MUST hold for every input it accepts. An edge case
  MUST be stated as a limit of the interface, never left to a path the caller cannot see.
- **Instructive.** A reader MUST be able to use the interface correctly, and write its tests, from
  the interface alone: what to call, when, what comes back, and how it fails. A tool's name,
  description, and schema MUST meet this bar for a model as its caller.
- **Composable.** Each interface MUST do one whole thing, and a caller MUST be able to use it
  without knowing its neighbours or the order they run in.
- **A layer pays for itself.** A new layer, wrapper, base class, indirection, or option MUST hide
  something that varies, is hard, or crosses a boundary, and MUST leave the reader fewer names,
  parameters, and files than before. Otherwise the caller uses the thing underneath directly.
- **Limits.** One implementation with one caller needs no layer; the second earns it. A boundary
  another principle requires (a contract between components, a provider-kind contract, an
  identity adapter) always hides something.

Rationale: [ADR 0022](../../docs/decisions/0022-principle-xviii-abstractions-say-what-not-how.md).

### XIX. Behaviour Is Proven by Evals

- Every agent, and every change to a prompt, model, tool, policy, or agent logic, MUST pass a
  regression eval suite before it is merged or released. A Docker Agent upgrade MUST pass the same
  suites.
- Eval suites MUST include adversarial cases for prompt injection, tool-permission escape, and
  cross-tenant access.
- Deterministic graders MUST be preferred. A model-judge grader MUST be calibrated against human
  labels. Behaviour users rely on MUST be reported as pass^k over repeated trials.
- Every fixed agent failure MUST add an eval case.

Rationale: [ADR 0023](../../docs/decisions/0023-principle-xix-behaviour-is-proven-by-evals.md).

### XX. Deterministic Acceptance of Agent Work

This principle governs the product: how the platform treats the work agents do in its users'
projects. The model proposes; deterministic checks decide
([platform overview](../../docs/overview/platform.md)).

- The platform MUST accept an agent's result only when every check the project declares for it
  passes. A model's judgement MAY inform a check but MUST NOT be its only basis, and an agent's
  own claim that work is done MUST NOT count as a check.
- The checks that judge a change, and their thresholds, MUST be fixed before the change is made.
  The agent that made the change MUST NOT add, alter, skip, or remove them; only a recorded,
  independent decision MAY change them.
- Every task MUST declare the files and areas it may change and the dependencies it may add. The
  platform MUST reject a change that touches anything outside that scope or adds a dependency that
  crosses a boundary the project's design forbids.
- The project's rules (principles, specs, decisions, validations, tests, reviews, and values kept
  as data) MUST be applied by the platform at every step of the lifecycle, not left to the
  agent's instructions.
- Work status MUST come from the platform's checks, never from the agent's report.
- Every accepted result MUST record the method, inputs, model, and rule versions that produced it,
  so that applying the same method to the same input can be repeated and its result compared.
- A proven solution MUST be capturable as a deterministic asset (template, generator,
  declaration, or script) and reused from then on without the model generating it again.
- Reviews the platform runs MUST follow "Review converges": blocking findings cite a rule, later
  rounds check only open findings and the changed lines, and the change escalates to a person
  after a bounded number of rounds.

Rationale: [ADR 0024](../../docs/decisions/0024-principle-xx-deterministic-acceptance-of-agent-work.md).

## Engineering Principles

These principles add to Core Principles. Where one of them and a Core Principle seem to conflict,
the Core Principle wins and the conflict MUST be raised for an amendment.

- **Values that check themselves**: A domain value with its own rule MUST be a type that rejects
  an invalid value when it is created. Input MUST be parsed into these types at every trust
  boundary, before any decision, and the same rule MUST NOT be checked anywhere else. Model output,
  tool calls, and structured output MUST be parsed against their schema, never trusted.
- **Single owner for rules**: A domain rule MUST have one owner and MUST NOT be duplicated. State
  guarded by a rule MUST change only through its owner.
- **Contracts between components**: Components MUST interact only through named, versioned,
  language-neutral contracts. A contract document MUST live with its provider. `specs/` MAY hold
  drafts of it or links to it, never the authoritative copy.
- **Contract-first**: Platform development (not client apps) MUST follow this order:
  1. **Contract.** Write or extend the contract first: an OpenAPI document for HTTP APIs and a
     versioned schema for events and tools. It MUST be reviewed and versioned before any
     implementation. For an adopted protocol, such as AHP, its published schema is the contract.
  2. **Generate.** A mature, open-source generator, tried against our stack before adoption,
     MUST produce the SDK for each supported stack and the server interfaces and types. Generated
     code MUST NOT be edited by hand, and a drift check MUST prove it matches the contract.
  3. **Tests.** Write schema-conformance tests that run against the contract and acceptance tests
     that use the generated SDKs, and see both fail.
  4. **Code.** Write the minimal implementation behind the generated interfaces.

  An API's shape MUST change only by changing its contract first. A contract starts as a draft for
  its first consumer and then grows only additively, within Principle XV
  ([research](../../docs/references/contract-first-development.md)).
- **Values have one home**:
  - Deploy configuration (endpoints, ports, store addresses) MUST come from environment variables.
  - Credentials MUST stay outside the repository, images, and agent context, and secret scanning
    MUST run in CI.
  - Facts a client sees, such as versions, limits, and error codes, MUST be declared in the
    contract document.
  - Everything users manage (agents, tools, policies, roles, providers) MUST enter through the
    control-plane API or a seed data file and live in the owning component's store.
  - Only internal values that no client sees and no deploy changes MAY stay in code, each named
    and defined once.
- **Names say what things do**: Code, commands, and tools MUST be named for what they do, never
  for what triggers them. Events MUST be named as a noun and a past-tense verb.
- **Outside-in, test-first**: The first feature MUST be a walking skeleton that runs end to end
  through CI and deployment. Feature work MUST follow Contract-first, and MUST write acceptance
  tests, or eval cases for agent behaviour, and see them fail before writing the minimal code. A
  contract, schema, or
  shared model MUST NOT be designed ahead of its first consumer; cross-cutting platform contracts
  are designed in the first feature that needs them and then grow only additively.
- **Evidence before action**: Decisions MUST be recorded with their deciding source in the plan's
  `research.md`. A decision MUST rest on a cited primary source or a reproduced experiment;
  otherwise it is marked `NEEDS CLARIFICATION` and the project owner decides.
- **Checks prove they can fail**: Every check, including `scripts/okf.py check` and every eval
  grader, MUST find its targets from the repository, report what it examined, pass on a clean
  fixture, and fail on a planted violation. A change that moves, renames, or restructures what a
  check targets MUST run the check's clean and planted-violation tests before it is committed.
- **Fix the cause**: A failing check, test, or eval MUST be resolved by fixing whichever of the
  code, the check, or the test is wrong, never by weakening the one that is right. A flaky test or
  eval MUST be quarantined with a linked task, never retried until it passes. A deliberate change
  to a check MUST be recorded with its reason in the plan's Exceptions table.
- **Fresh review**: The author MUST NOT review its own work, or write the tests that judge it. A
  fresh reviewer is a person, or an agent session that never saw the work being built, made
  read-only by tool permissions, not by instruction. It MUST work in its own copy of the
  repository, including the working tree's uncommitted and untracked changes, and run the test
  command there. Parallel fresh reviewers MUST NOT share a copy. A fresh agent review MUST NOT
  replace the human approval that Principle XVI requires.
- **Existing tests change only with the owner's approval**: An existing test, test helper,
  fixture, test data, planted case, grader, threshold, or eval case MUST be modified, renamed,
  skipped, or removed only after the project owner approves that specific change, recorded in the
  commit that makes it with a `Tests-approved-by:` trailer naming the owner. No standing approval
  covers it. An agent MUST refuse a brief, or the part of one, that would change an existing test,
  and MUST report the requested change to the owner instead; a coordinating agent MUST NOT brief
  such a change without that approval. The author of code MUST NOT change a test that judges it. A
  fresh agent MAY propose such a change, judged from the spec and the contract, never from the
  author's argument, but its judgement MUST NOT replace the owner's approval. Adding a new test
  file does not change an existing test. The commit hook MUST reject a change to an existing test
  file without that trailer. A requirement MUST change only through a recorded clarification. A
  test MAY be skipped only as a strict expected failure that names an open task
  ([ADR 0029](../../docs/decisions/0029-existing-tests-change-only-with-owner-approval.md)).
- **Findings have a home**: Every review finding, and every case a fresh reviewer reports as left
  untested, MUST be fixed, or recorded as a task, or as a checked task ending `closed: <reason>`,
  before the work it concerns is reported done.
- **Review converges**: A blocking finding MUST cite the rule, spec, contract, or test it
  violates; a finding that cites none is a suggestion and MUST NOT block. A review round after the
  first MUST check only the open findings and the lines the fixes changed, and MAY raise a new
  blocking finding only for a regression those fixes caused. After three review rounds, the change
  MUST be escalated to a person with its open findings, and agents MUST NOT start another round.
- **Reliable events and side effects**: A published event MUST be a versioned contract written
  through an outbox in the same transaction as the state change. A consumer MUST be idempotent,
  recording each message's key in the same transaction as its state change. A process that spans
  components MUST name its pivot, a compensation for every step before the pivot that changes
  state, and a timeout. A tool call with side effects is such a step: it MUST carry an idempotency
  key and declare its compensation or that it cannot be reversed. Large payloads MUST travel by
  reference, not inside events.

Rationale: [ADR 0025](../../docs/decisions/0025-engineering-principles.md).

## Technology and Repository Constraints

- **License**: The project is MIT-licensed. `LICENSE` at the repository root is authoritative.
  Notices that adopted components' licenses require, such as Apache-2.0's, MUST be kept in
  `NOTICE`.
- **Docker Agent**: One Docker Agent release and its config schema version MUST be pinned by
  version and digest. An upgrade MUST pass the regression eval suites (XIX) and the test command
  before it is merged.
- **Stack**: Custom code MUST use the best stack for its job, recorded with its reasons in the
  plan's `research.md`. Fit with Docker Agent's Go SDK and whether a mature generator turns the
  stack's contracts into code count among those reasons. Each additional stack's cost in tooling,
  skills, and patching counts against it, so an existing stack MUST be reused when it fits about as
  well. A stack that sets a pattern for several components MUST also get a decision.
- **Layout**: All product code, tests, contracts, schemas, agent definitions, evals, and
  deployment packaging MUST live under `src/`. The repository root MUST hold only `src/`,
  `LICENSE`, `NOTICE`, `README.md`, `SECURITY.md`, `CONTRIBUTING.md`, `AGENTS.md`, `CLAUDE.md`,
  `.specify/`, `specs/`, `docs/`, `scripts/`, and tool dotfiles, and a check MUST enforce it.
- **Clean tree**: Build output, dependency folders, regenerable code, logs, coverage, local state,
  scratch files, editor files, and secrets MUST NOT be committed. Each new stack MUST add its
  ignore patterns to `.gitignore` in the change that introduces it.
- **Built artifacts**: Every image or package we build MUST exclude tests and evals, by one rule
  per stack named in the plan.
- **Spec Kit**: Spec Kit MUST be customized only with `specify preset` and `specify extension`.
  `.specify/scripts/`, `.specify/templates/`, and `.specify/presets/` MUST NOT be hand-edited.

## Development Workflow and Quality Gates

- Feature work MUST go through Spec Kit: `/speckit-specify`, `/speckit-plan`, `/speckit-tasks`,
  `/speckit-analyze`, and `/speckit-implement`, with `/speckit-clarify`, `/speckit-checklist`, and
  `/speckit-converge` as needed. `/speckit-analyze` MUST pass before `/speckit-implement`.
- During implementation, Spec Kit MUST be used as its own guides describe
  ([ADR 0035](../../docs/decisions/0035-spec-kit-during-implementation.md)):
  - **Assess a discovery first**: a defect against agreed intent is fixed in code with a
    regression test; anything else is a change of intent, design, or task breakdown. Code and
    tests are evidence, never a replacement source of requirements.
  - **Fix at the source**: a requirement gap goes to `/speckit-specify` or `/speckit-clarify`, a
    design gap to the plan and its design artifacts, a task gap to `tasks.md`. A gap that
    `/speckit-analyze` or a checklist reports MUST be fixed in the artifacts, never patched in
    code.
  - **Intent before code**: a change of intent MUST update the spec, then the plan and design
    artifacts, then `tasks.md`, in that order, committed as its own reviewable change before the
    code that delivers it. Rationale a replaced artifact held MUST be kept before it is replaced.
  - **Analyze until clean**: `/speckit-analyze` MUST be re-run after every change to `spec.md`,
    `plan.md`, `tasks.md`, or a design artifact, and its findings fixed at their source until it
    reports no CRITICAL or HIGH finding. Analyze only reports; artifacts are reconciled by hand.
  - **Validated stages**: `/speckit-implement` MUST run one unit of tasks at a time, each
    validated before the next starts. With unchecked checklist items it MUST ask the owner before
    continuing, and it MUST NOT change a checklist marker or approve its own work.
  - **Converge**: `/speckit-converge` MUST run after the implementation of each user story and
    before a feature is done. It only appends tasks; a gap becomes a task, never a direct code
    edit, and implement and converge repeat until converge finds nothing.
- Every spec MUST link the problem-space needs it addresses and the glossary terms it uses. Every
  plan MUST show the OSS research that Principle VI requires, including what Docker Agent already
  provides and what the platform adds.
- Every plan that adds an external interface, a trust boundary, a tool, a model, memory, or access
  to protected content MUST include a threat model: STRIDE per element, mapped to the OWASP Top 10
  for LLM Applications and the OWASP Top 10 for Agentic Applications, with the mitigation for each
  threat. The model, tool runner, memory, retrieval index, and each MCP server are elements, and
  untrusted content entering a model's context is a trust boundary
  ([OWASP Top 10 for Agentic Applications](https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026/);
  [CSA MAESTRO](https://cloudsecurityalliance.org/blog/2025/02/06/agentic-ai-threat-modeling-framework-maestro);
  [MITRE ATLAS](https://atlas.mitre.org/)).
- What a test or eval may hold or do MUST meet named security standards: credentials only from a
  trust root created for the test installation, authorization tested with accepted identities,
  least privilege for the harness, no raw container-engine socket, model access only through a
  test-scoped key with a spending cap or a recorded model, and never production keys or tenant
  data ([OWASP Docker Security](https://cheatsheetseries.owasp.org/cheatsheets/Docker_Security_Cheat_Sheet.html)).
- A change is done only when:
  - for feature work, `tasks.md` is current with completed items checked off, and the project's
    test command and regression eval suites pass
  - `docs/` reflects every durable fact the change introduced or changed, including the
    architecture overview for any architectural choice, and `docs/log.md` records the change
  - the problem-space needs the change addresses show their current scope, spec, and state
  - every newly adopted OSS project has a `docs/components/` concept
  - `python3 scripts/okf.py check` reports `OK`
  - every finding has a home, no file sits outside the allowed paths, and nothing generated or
    stray is committed
- Every plan's Constitution Check MUST verify compliance. A violation of a Core Principle MUST be
  justified in the plan's Complexity Tracking table and approved by the project owner. An
  exception to an Engineering Principle MUST be recorded in the plan's Exceptions table with the
  rule, location, and reason, and is reviewed in `/speckit-analyze`.

## Governance

- This constitution is the top authority in the repository. `AGENTS.md` only wires agents to it,
  to the knowledge bundle's playbook, and to the project's tools, and MUST NOT add or change
  rules.
- The principles derive from the decisions in `docs/decisions/`. A change to a principle MUST be
  recorded as a decision first and reflected here in the same change. When a principle and a
  decision disagree, work on the affected area MUST stop until one of them is amended.
- The project owner approves every amendment and every Core Principle exception. Approval is
  recorded in the decision's Origin section, or in the commit or pull request that carries the
  change.
- Versioning follows semantic versioning:
  - MAJOR for removing a principle or redefining one incompatibly
  - MINOR for adding a principle or section, or materially expanding guidance
  - PATCH for clarifications and wording that do not change meaning
- Compliance is checked in every plan's Constitution Check and in `/speckit-analyze`.

**Version**: 2.1.0 | **Ratified**: 2026-11-11 | **Last Amended**: 2026-11-14
