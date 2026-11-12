# Decision

* [ADR 0001: Adopt OKF for project knowledge](0001-adopt-okf.md) - All project documentation is kept as an OKF v0.2 knowledge bundle in docs/.
* [ADR 0002: Build the platform on Docker Agent](0002-build-on-docker-agent.md) - Docker Agent is the agent definition format and runtime the platform is designed around; the platform adds what multi-tenant use needs.
* [ADR 0003: AHP is the client-facing session protocol](0003-ahp-client-session-protocol.md) - Clients (web console, IDE, CLI, mobile) share and steer live agent sessions through the Agent Host Protocol; the platform's host bridges AHP to Docker Agent.
* [ADR 0004: Contract-first toolchain, service-owned APIs and versioning](0004-contract-first-toolchain.md) - Each microservice owns its OpenAPI 3.1 contract and operations list; APIs use ARM-style date-based stable and preview api-versions per service; open-source generators and a CI tool set enforce contract, compatibility, conformance and drift.
* [ADR 0005: Principle I - Needs Before Solutions](0005-principle-i-needs-before-solutions.md) - Why the constitution adopts Principle I, 'Needs Before Solutions'.
* [ADR 0006: Principle II - Two Planes, Agent Work Stays in the Runtime Plane](0006-principle-ii-two-planes-agent-work-stays-in-the-runtime-plane.md) - Why the constitution adopts Principle II, 'Two Planes, Agent Work Stays in the Runtime Plane'.
* [ADR 0007: Principle III - API-First, Declarative Management](0007-principle-iii-api-first-declarative-management.md) - Why the constitution adopts Principle III, 'API-First, Declarative Management'.
* [ADR 0008: Principle IV - Asynchronous Operations on Worker Pools](0008-principle-iv-asynchronous-operations-on-worker-pools.md) - Why the constitution adopts Principle IV, 'Asynchronous Operations on Worker Pools'.
* [ADR 0009: Principle V - Zero-Trust Identity and Separate Permissions](0009-principle-v-zero-trust-identity-and-separate-permissions.md) - Why the constitution adopts Principle V, 'Zero-Trust Identity and Separate Permissions'.
* [ADR 0010: Principle VI - Adopt Mature, Free Open Source; Never Rewrite It](0010-principle-vi-adopt-mature-free-open-source-never-rewrite-it.md) - Why the constitution adopts Principle VI, 'Adopt Mature, Free Open Source; Never Rewrite It'.
* [ADR 0011: Principle VII - Pluggable Providers](0011-principle-vii-pluggable-providers.md) - Why the constitution adopts Principle VII, 'Pluggable Providers'.
* [ADR 0012: Principle VIII - Containers and Sandboxed Execution](0012-principle-viii-containers-and-sandboxed-execution.md) - Why the constitution adopts Principle VIII, 'Containers and Sandboxed Execution'.
* [ADR 0013: Principle IX - Knowledge Stays True](0013-principle-ix-knowledge-stays-true.md) - Why the constitution adopts Principle IX, 'Knowledge Stays True'.
* [ADR 0014: Principle X - No Silent Loss of Accepted Runs](0014-principle-x-no-silent-loss-of-accepted-runs.md) - Why the constitution adopts Principle X, 'No Silent Loss of Accepted Runs'.
* [ADR 0015: Principle XI - Agent Data Privacy](0015-principle-xi-agent-data-privacy.md) - Why the constitution adopts Principle XI, 'Agent Data Privacy'.
* [ADR 0016: Principle XII - Tenant Isolation](0016-principle-xii-tenant-isolation.md) - Why the constitution adopts Principle XII, 'Tenant Isolation'.
* [ADR 0017: Principle XIII - Secure Supply Chain](0017-principle-xiii-secure-supply-chain.md) - Why the constitution adopts Principle XIII, 'Secure Supply Chain'.
* [ADR 0018: Principle XIV - Safe Defaults and Least Agency](0018-principle-xiv-safe-defaults-and-least-agency.md) - Why the constitution adopts Principle XIV, 'Safe Defaults and Least Agency'.
* [ADR 0019: Principle XV - Compatible Public APIs](0019-principle-xv-compatible-public-apis.md) - Why the constitution adopts Principle XV, 'Compatible Public APIs'.
* [ADR 0020: Principle XVI - Human Control of Agents](0020-principle-xvi-human-control-of-agents.md) - Why the constitution adopts Principle XVI, 'Human Control of Agents'.
* [ADR 0021: Principle XVII - Declare, Derive, Converge](0021-principle-xvii-declare-derive-converge.md) - Why the constitution adopts Principle XVII, 'Declare, Derive, Converge'.
* [ADR 0022: Principle XVIII - Abstractions Say What, Not How](0022-principle-xviii-abstractions-say-what-not-how.md) - Why the constitution adopts Principle XVIII, 'Abstractions Say What, Not How'.
* [ADR 0023: Principle XIX - Behaviour Is Proven by Evals](0023-principle-xix-behaviour-is-proven-by-evals.md) - Why the constitution adopts Principle XIX, 'Behaviour Is Proven by Evals'.
* [ADR 0024: Principle XX - Deterministic Acceptance of Agent Work](0024-principle-xx-deterministic-acceptance-of-agent-work.md) - Why the constitution adopts Principle XX, 'Deterministic Acceptance of Agent Work'.
* [ADR 0025: Engineering Principles](0025-engineering-principles.md) - Why the constitution adopts the Engineering Principles.
* [ADR 0026: Deterministic workflow engine with agents as state workers](0026-deterministic-workflow-engine.md) - Candidate approach in which a declared, durable state machine runs each piece of work, agents act only inside its states, and deterministic guards decide every transition; includes the spike that tests it.
* [ADR 0027: Contract-driven frontend](0027-contract-driven-frontend.md) - The console is a shell with one UI module per service, modelled on the Azure portal, built against the API contracts and AHP in parallel with the backend; every known weakness of the stack is removed or mitigated.
* [ADR 0028: Sign the platform's images locally with cosign](0028-sign-platform-images-locally.md) - Images the platform builds are pushed to a local registry with an SBOM and provenance, signed with cosign and a key pair kept outside the repository, and verified offline by the launcher before anything starts.
