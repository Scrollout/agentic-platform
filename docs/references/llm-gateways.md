---
type: Reference
title: LLM gateways
description: LiteLLM, agentgateway, Bifrost and Envoy AI Gateway compared with the nginx model gateway against the constitution's adoption rules, as of 2026-11-27; research only, nothing adopted.
resource: https://github.com/BerriAI/litellm
tags: [references, gateway, models, providers, supply-chain]
status: draft
stale_after: 2027-05-27T00:00:00Z
sources:
  - id: litellm-repo
    resource: https://github.com/BerriAI/litellm
    title: BerriAI/litellm repository and LICENSE
  - id: litellm-incident
    resource: https://docs.litellm.ai/blog/security-update-march-2026
    title: "LiteLLM: Security update, suspected supply chain incident (March 2026)"
  - id: litellm-oss-security
    resource: https://www.openwall.com/lists/oss-security/2026/03/24/6
    title: oss-security list, malicious litellm 1.82.7 and 1.82.8
  - id: litellm-snyk
    resource: https://snyk.io/articles/poisoned-security-scanner-backdooring-litellm
    title: "Snyk: How a poisoned security scanner became the key to backdooring LiteLLM"
  - id: litellm-enterprise
    resource: https://docs.litellm.ai/docs/enterprise
    title: LiteLLM Enterprise
  - id: litellm-pricing
    resource: https://www.litellm.ai/pricing
    title: LiteLLM pricing (open source and enterprise features)
  - id: litellm-passthrough
    resource: https://docs.litellm.ai/docs/pass_through/anthropic_completion
    title: "LiteLLM: Anthropic passthrough"
  - id: litellm-logs
    resource: https://docs.litellm.ai/docs/proxy/ui_logs
    title: "LiteLLM: request and response content in logs"
  - id: litellm-telemetry
    resource: https://docs.litellm.ai/docs/proxy/telemetry
    title: "LiteLLM: usage telemetry"
  - id: agw-repo
    resource: https://github.com/agentgateway/agentgateway
    title: agentgateway/agentgateway repository
  - id: agw-aaif
    resource: https://aaif.io/blog/agentgateway-joins-aaif-as-an-open-gateway-for-agentic-ai-infrastructure
    title: "Agentic AI Foundation: agentgateway joins AAIF"
  - id: agw-anthropic
    resource: https://agentgateway.dev/docs/standalone/main/llm/providers/anthropic/
    title: "agentgateway (standalone): Anthropic provider"
  - id: agw-budgets
    resource: https://agentgateway.dev/docs/standalone/main/llm/cost-controls/budget-limits/
    title: "agentgateway (standalone): budget limits"
  - id: agw-jwt
    resource: https://agentgateway.dev/docs/standalone/main/configuration/security/jwt-authn/
    title: "agentgateway (standalone): JWT authentication"
  - id: bifrost-repo
    resource: https://github.com/maximhq/bifrost
    title: maximhq/bifrost repository
  - id: bifrost-setup
    resource: https://docs.getbifrost.ai/quickstart/gateway/setting-up
    title: "Bifrost: setting up the gateway (file-only mode)"
  - id: eaig-repo
    resource: https://github.com/envoyproxy/ai-gateway
    title: envoyproxy/ai-gateway repository
  - id: eaig-standalone
    resource: https://aigateway.envoyproxy.io/docs/cli/aigwrun
    title: "Envoy AI Gateway: aigw run (standalone)"
  - id: eaig-endpoints
    resource: https://aigateway.envoyproxy.io/docs/capabilities/llm-integrations/supported-endpoints
    title: "Envoy AI Gateway: supported API endpoints"
  - id: eaig-ratelimit
    resource: https://aigateway.envoyproxy.io/docs/capabilities/traffic/usage-based-ratelimiting
    title: "Envoy AI Gateway: usage-based rate limiting"
  - id: scorecard-api
    resource: https://api.securityscorecards.dev/
    title: OpenSSF Scorecard API, results read 2026-11-27
  - id: deps-dev
    resource: https://api.deps.dev/
    title: deps.dev API, project data read 2026-11-27
  - id: docker-agent
    resource: /references/docker-agent.md
    title: Docker Agent reference (models, fallback, budgets)
  - id: nginx
    resource: /components/nginx.md
    title: nginx component (the current model gateway)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-27T13:00:00Z }
---

# Why this exists

The platform's model gateway is [nginx](/components/nginx.md) (feature 001, research D9): the only
container with egress and the only holder of the provider key, passing `POST /v1/messages` and
`count_tokens` to Anthropic and refusing everything else. It has no routing, fallback, budgets or
metering. Those would serve N-011 (more than one model provider; `undecided`), Principle VII's
routing and fallback rule and Principle XIV's tenant, agent and run budgets. This concept records
the open-source LLM gateways a later plan would evaluate under Principle VI. Nothing is adopted,
and no decision is recorded: that belongs to the plan of the feature that takes on those needs.

Sources: [nginx component](/components/nginx.md), [Docker Agent reference](/references/docker-agent.md).

# What Docker Agent already provides

Principle VI asks what Docker Agent provides before adding anything. Per agent it has about 40
built-in providers, `fallback` to other models (5xx and timeouts retried, 429 or another 4xx moves
on), budgets `max_cost`, `max_tokens` and `max_time` per session or as named pools, and
`--models-gateway`, which only points model traffic at a gateway. Its budgets reset when the
process restarts, and persistent per-tenant quotas and metering are not provided. So routing and
fallback across providers can live in the agent definition; persistent budgets and metering
across runs are what a gateway would add.

Sources: [Docker Agent reference](/references/docker-agent.md).

# Candidates

Measured 2026-11-27: OpenSSF Scorecard from its API (agentgateway and Envoy AI Gateway have no
published result, so a Scorecard run is still owed for them); stars and licence from deps.dev;
releases and repository facts from GitHub; image size is the compressed linux/amd64 layers.

| | LiteLLM | agentgateway | Bifrost | Envoy AI Gateway | nginx (current) |
|---|---|---|---|---|---|
| Language | Python | Rust | Go | Go (on Envoy) | C |
| Licence | MIT, except `litellm/enterprise/` under a commercial licence | Apache-2.0 | Apache-2.0 open core; a separate paid enterprise build | Apache-2.0 | BSD-2 |
| Governance | one company (BerriAI) | Linux Foundation, Agentic AI Foundation (donated by Solo.io, 2025-08) | one company (Maxim) | Envoy project | F5 / nginx |
| Scorecard | 5.7 (Token-Permissions 0; Vulnerabilities 0: 123 open; Branch-Protection 4) | not published | 7.3 (Code-Review 4; Token-Permissions 0; Vulnerabilities 0: 13 open) | not published | 8 (deps.dev, 2026-08) |
| Health | created 2023-07; releases several per day; 5607 open issues; 30 published advisories | created 2025-03; v1.7.0 2026-10-27, monthly minors; 381 open issues; 7 advisories | created 2025-03; v2.6.0 2026-11-19; 1665 open issues; 3 advisories | created 2024-10; v1.3.0 2026-11-18; 459 open issues; 24 advisories | mature |
| Image | about 674 MiB | about 39 MiB | about 120 MiB | about 128 MiB (`aigw` CLI image) | about 24 MiB |
| Image signature | sigstore `.sig` tag and SLSA provenance found | sigstore `.sig` tag and SLSA provenance found | none found | SLSA provenance found, no `.sig` tag | not signed (research D24) |
| Anthropic Messages, native | `/anthropic/v1/messages` passthrough | `messages` API type; adds `x-api-key` and `anthropic-version` | `/anthropic` route | `POST /anthropic/v1/messages` | `/v1/messages` |
| Budgets and limits | virtual keys with budgets, rate limits, spend per key, team, user (open source); needs PostgreSQL | USD budgets per key (needs SQLite or PostgreSQL and a cost catalogue); token-bucket limits without a database | budgets per customer, team and virtual key (open source) | token-based rate limits through Envoy Gateway's global rate limit (Redis) | none |
| Configuration | `config.yaml` plus a database and an admin API and UI for keys and budgets | one YAML file, or xDS from a control plane; admin UI on its own port | `config.json` file-only mode (no config store), or a database with UI and API | the Kubernetes resources' API, also read by `aigw run` from one file | templates |
| Also | — | MCP and A2A gateway | MCP gateway | Kubernetes-first; `aigw run` standalone | — |

Sources: [LiteLLM repository](https://github.com/BerriAI/litellm), [LiteLLM Enterprise](https://docs.litellm.ai/docs/enterprise), [LiteLLM pricing](https://www.litellm.ai/pricing), [LiteLLM Anthropic passthrough](https://docs.litellm.ai/docs/pass_through/anthropic_completion), [agentgateway repository](https://github.com/agentgateway/agentgateway), [AAIF announcement](https://aaif.io/blog/agentgateway-joins-aaif-as-an-open-gateway-for-agentic-ai-infrastructure), [agentgateway Anthropic provider](https://agentgateway.dev/docs/standalone/main/llm/providers/anthropic/), [agentgateway budget limits](https://agentgateway.dev/docs/standalone/main/llm/cost-controls/budget-limits/), [Bifrost repository](https://github.com/maximhq/bifrost), [Bifrost setup](https://docs.getbifrost.ai/quickstart/gateway/setting-up), [Envoy AI Gateway repository](https://github.com/envoyproxy/ai-gateway), [aigw run](https://aigateway.envoyproxy.io/docs/cli/aigwrun), [Envoy AI Gateway endpoints](https://aigateway.envoyproxy.io/docs/capabilities/llm-integrations/supported-endpoints), [usage-based rate limiting](https://aigateway.envoyproxy.io/docs/capabilities/traffic/usage-based-ratelimiting), [OpenSSF Scorecard API](https://api.securityscorecards.dev/), [deps.dev API](https://api.deps.dev/).

# LiteLLM's supply-chain incident

On 2026-03-24 the PyPI releases `litellm` 1.82.7 and 1.82.8 carried a multi-stage credential
stealer for about 40 minutes before PyPI quarantined them (1.82.8 through a `.pth` file that runs
at every Python start). The attacker (TeamPCP) had first compromised the Trivy scanner used in
LiteLLM's CI and took the PyPI publish token from the runner; the malicious code was not in the
GitHub repository. A gateway holds the provider key and is the only container with egress, so a
compromise there reaches the most. Pinning by digest (Principle XIII) protects a pinned install
but not the update that brings a bad release in.

Sources: [LiteLLM security update](https://docs.litellm.ai/blog/security-update-march-2026), [oss-security](https://www.openwall.com/lists/oss-security/2026/03/24/6), [Snyk analysis](https://snyk.io/articles/poisoned-security-scanner-backdooring-litellm).

# Fit with the constitution

- **Principle VI, paid features.** LiteLLM's enterprise features are turned on by a licence key on
  the same image: SSO beyond 5 users, OIDC/JWT authentication, audit logs, secret managers with
  key rotation and some guardrails. Bifrost keeps guardrails, clustering, SSO, audit logs, roles
  and secret managers in its paid build. A plan must check that every feature it relies on is in
  the open-source part. agentgateway's budgets, JWT authentication and MCP features are in the
  Apache-2.0 project.
- **Principle III and XVII, declarative management.** A gateway's keys, routes and budgets would be
  management configuration, so they must come from the control plane, never from the gateway's own
  admin UI. A single declarative file or xDS (agentgateway, Bifrost's file-only mode, `aigw run`)
  fits this directly; LiteLLM's keys and budgets live in its database behind its admin API, which
  the control plane would have to drive, with its UI off.
- **Principle V, workload identity.** Each agent run needs its own identity at the gateway.
  agentgateway validates JWTs against an issuer, audiences and JWKS (mode `strict` must be set:
  its default `optional` lets a request without a token through); LiteLLM's JWT authentication is an enterprise feature; virtual keys
  are shared static secrets unless issued per run.
- **Principle XI, privacy.** Prompts and completions pass through the gateway. LiteLLM stores no
  request or response content by default (`store_prompts_in_spend_logs` is off), but the admin UI
  can turn it on over the configuration; its usage telemetry has no default endpoint. Each
  candidate's content logging and vendor telemetry must be proven off by a test.
- **Principle VIII, Compose first.** All four run on Compose; Envoy AI Gateway's rate limits need
  Envoy Gateway's global rate limit service and Redis, and its documentation targets Kubernetes
  first.
- **Principle XIII, supply chain.** LiteLLM's March 2026 compromise and its Scorecard of 5.7, with
  123 known vulnerabilities in its dependencies, weigh against it at the one place that holds the
  key. agentgateway and Envoy AI Gateway need a Scorecard run before any choice.
- **Host resources.** LiteLLM's image is about 28 times nginx's and needs PostgreSQL for budgets;
  agentgateway's is about 1.6 times.

Sources: [LiteLLM Enterprise](https://docs.litellm.ai/docs/enterprise), [LiteLLM pricing](https://www.litellm.ai/pricing), [LiteLLM logs](https://docs.litellm.ai/docs/proxy/ui_logs), [LiteLLM telemetry](https://docs.litellm.ai/docs/proxy/telemetry), [agentgateway budget limits](https://agentgateway.dev/docs/standalone/main/llm/cost-controls/budget-limits/), [agentgateway JWT authentication](https://agentgateway.dev/docs/standalone/main/configuration/security/jwt-authn/), [Bifrost setup](https://docs.getbifrost.ai/quickstart/gateway/setting-up), [usage-based rate limiting](https://aigateway.envoyproxy.io/docs/capabilities/traffic/usage-based-ratelimiting), [OpenSSF Scorecard API](https://api.securityscorecards.dev/).

# Still open

- A Scorecard run for agentgateway and Envoy AI Gateway, and the OpenSSF Concise Guide review for
  every candidate.
- Each candidate's vendor telemetry default, proven by an egress test on the pinned image.
- Whether per-run workload identity reaches each gateway without a shared secret.
- A spike on the clean VM: memory under the analyst and step load, and Docker Agent's Anthropic
  provider pointed at each gateway's native Messages route.

Sources: [OpenSSF Scorecard API](https://api.securityscorecards.dev/).
