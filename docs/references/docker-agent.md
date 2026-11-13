---
type: Reference
title: Docker Agent
description: What Docker Agent (formerly cagent) provides, how it works, and what a multi-tenant platform built on it must add, as of v1.160.0.
resource: https://github.com/docker/docker-agent
tags: [references, docker-agent, agents, runtime]
status: draft
stale_after: 2027-02-11T00:00:00Z
sources:
  - id: da-repo
    resource: https://github.com/docker/docker-agent/tree/3c9f6d45b8682e02713e85330fc98b5157e4896d
    title: docker/docker-agent at commit 3c9f6d45 (release v1.160.0)
    last_modified: 2026-11-11T15:18:14Z
  - id: da-docker-docs
    resource: https://docs.docker.com/ai/docker-agent/
    title: Docker Agent (Docker Docs)
  - id: da-agents
    resource: https://docker.github.io/docker-agent/concepts/agents/
    title: Docker Agent - Agents
  - id: da-multi
    resource: https://docker.github.io/docker-agent/concepts/multi-agent/
    title: Docker Agent - Multi-agent
  - id: da-dist
    resource: https://docker.github.io/docker-agent/concepts/distribution/
    title: Docker Agent - Distribution
  - id: da-config
    resource: https://docker.github.io/docker-agent/configuration/overview/
    title: Docker Agent - Configuration overview
  - id: da-models
    resource: https://docker.github.io/docker-agent/configuration/models/
    title: Docker Agent - Model configuration
  - id: da-tools
    resource: https://docker.github.io/docker-agent/configuration/tools/
    title: Docker Agent - Tool configuration
  - id: da-budget
    resource: https://docker.github.io/docker-agent/configuration/budget/
    title: Docker Agent - Budgets
  - id: da-perm
    resource: https://docker.github.io/docker-agent/configuration/permissions/
    title: Docker Agent - Permissions
  - id: da-sandbox
    resource: https://docker.github.io/docker-agent/configuration/sandbox/
    title: Docker Agent - Sandbox mode
  - id: da-hooks
    resource: https://docker.github.io/docker-agent/configuration/hooks/
    title: Docker Agent - Hooks
  - id: da-secrets
    resource: https://docker.github.io/docker-agent/guides/secrets/
    title: Docker Agent - Managing secrets
  - id: da-telemetry
    resource: https://docker.github.io/docker-agent/configuration/telemetry/
    title: Docker Agent - Telemetry
  - id: da-otel
    resource: https://docker.github.io/docker-agent/configuration/opentelemetry/
    title: Docker Agent - OpenTelemetry
  - id: da-api
    resource: https://docker.github.io/docker-agent/features/api-server/
    title: Docker Agent - API server
  - id: da-chat
    resource: https://docker.github.io/docker-agent/features/chat-server/
    title: Docker Agent - Chat server
  - id: da-a2a
    resource: https://docker.github.io/docker-agent/features/a2a/
    title: Docker Agent - A2A
  - id: da-acp
    resource: https://docker.github.io/docker-agent/features/acp/
    title: Docker Agent - ACP
  - id: da-mcp-mode
    resource: https://docker.github.io/docker-agent/features/mcp-mode/
    title: Docker Agent - MCP mode
  - id: da-sessions
    resource: https://docker.github.io/docker-agent/features/sessions/
    title: Docker Agent - Sessions
  - id: da-harness
    resource: https://docker.github.io/docker-agent/features/harnesses/
    title: Docker Agent - Harnesses
  - id: da-eval
    resource: https://docker.github.io/docker-agent/features/evaluation/
    title: Docker Agent - Evaluation
  - id: da-gosdk
    resource: https://docker.github.io/docker-agent/guides/go-sdk/
    title: Docker Agent - Go SDK
  - id: da-headless
    resource: https://docker.github.io/docker-agent/guides/headless/
    title: Docker Agent - Headless mode
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T15:00:00Z }
---

# Summary

Docker Agent is Docker's open-source builder and runtime for AI agents. Agents are declared
in YAML or HCL, run from the terminal or as servers, and are shared as OCI artifacts.

- **Project:** Apache-2.0, written in Go, about 4.7k stars, created 2025-09-01.
- **Releases:** 165 releases between v1.19.5 (2026-01-22) and v1.160.0 (2026-11-11), so roughly three or four a week.
  - Minor releases sometimes carry breaking changes.
  - No LTS or deprecation-window policy is published.
- **Install:** included in Docker Desktop 4.63 and later as `docker agent`. Desktop 4.49 to 4.62 called it `cagent`, and the data dir is still `~/.cagent`.
  - Also installable through Homebrew or release binaries.
- **Security reports:** sent privately to `security@docker.com`, with acknowledgement within 72 hours.

This concept was written from the repository at the pinned commit and from its docs site.
It describes Docker Agent, not our design. For how we use it, see
[ADR 0002](/decisions/0002-build-on-docker-agent.md).

Sources: [Docker Agent (Docker Docs)](https://docs.docker.com/ai/docker-agent/); [docker/docker-agent at commit 3c9f6d45 (release v1.160.0)](https://github.com/docker/docker-agent/tree/3c9f6d45b8682e02713e85330fc98b5157e4896d).

# Agent definition model

- **Agent:** a model, a `description`, an `instruction` (the system prompt), `toolsets`, and optional `sub_agents`.
  - Other agents read `description` to decide when to delegate.
  - The root agent is the one named `root`, or else the first one defined.
- **Config file:**
  - Top-level keys: `version`, `metadata`, `agents`, `models`, `providers`, `mcps`, `rag`, `commands`, `skills`, `toolsets`, `permissions`, `runtime`, `budget`, `budgets`, `flavors`, `evaluators`.
  - Unknown keys are rejected.
  - The schema version is 16. Older files are migrated automatically, one way.
  - A JSON Schema (`agent-schema.json`) is available.
- **Variables:** `${env.VAR}` everywhere.
  - Prompt and header fields also run a small JavaScript evaluator.
  - An undefined variable becomes an empty string, except in `model` and `base_url`.
- **Flavors:** JSON Merge Patch variants of one file, selected with `--flavor`.
- **Models:** written inline as `provider/model`, or as named entries.
  - About 40 built-in providers, including OpenAI, Anthropic, Google, Bedrock, Azure, Mistral, Ollama and Docker Model Runner (`dmr`).
  - Any OpenAI-compatible endpoint can be added through `providers:` with a `base_url`.
  - Per-agent `fallback` retries 5xx errors and timeouts on the same model, and moves to the next model on 429 or another 4xx.
  - `first_available` picks the first model that has credentials.
  - Model `routing` matches the last user message with BM25, a lexical method that uses no embeddings.
  - `--models-gateway` sends model traffic through a gateway. A model can opt out with `bypass_models_gateway`.
- **Toolsets:** about 30 built-in types, such as shell, filesystem, fetch, api, openapi, memory, rag, todo, plan, a2a, lsp, scheduler and webhook, plus `mcp`.
  - MCP servers are reached through the Docker MCP Gateway (`ref: docker:<name>`), as a local stdio process, or as a remote Streamable HTTP/SSE server with OAuth.
  - A `tools:` filter limits which tools a toolset exposes.
- **Budgets:** `max_cost` (USD), `max_tokens` and `max_time`, applied per session or as named pools shared by agents.
  - They are checked at turn boundaries, so a run can overshoot by one turn.
  - The counters live in memory and reset when the process restarts. They are execution limits, not persistent quotas.
  - Per-agent `max_iterations` and `max_consecutive_tool_calls` add further limits.

Sources: [Docker Agent - Agents](https://docker.github.io/docker-agent/concepts/agents/); [Docker Agent - Configuration overview](https://docker.github.io/docker-agent/configuration/overview/); [Docker Agent - Model configuration](https://docker.github.io/docker-agent/configuration/models/); [Docker Agent - Tool configuration](https://docker.github.io/docker-agent/configuration/tools/); [Docker Agent - Budgets](https://docker.github.io/docker-agent/configuration/budget/).

# Multi-agent patterns

- **Delegation (`sub_agents` and `transfer_task`):** the child runs in an isolated sub-session and receives only a task description.
  - The parent waits, and only the result comes back.
- **Handoffs:** a peer graph inside one session in which the next agent sees the full history.
  - `force_handoff` makes a deterministic pipeline.
  - Hooks can route between agents within `routing.allowed_agents`.
- **Background agents:** parallel fan-out managed as tasks (run, list, view, wait, stop).
- **External sub-agents:** other agents referenced by OCI ref.
  - Tags are resolved again on every run.
  - Digests (`@sha256:`) are cached, and unpinned refs log a warning.
- **Harnesses:** an agent can delegate to an external coding CLI, such as Claude Code, Codex or opencode.
  - Its toolsets, approvals and budgets do not apply.
  - Claude Code runs with `--dangerously-skip-permissions`.

Sources: [Docker Agent - Multi-agent](https://docker.github.io/docker-agent/concepts/multi-agent/); [Docker Agent - Harnesses](https://docker.github.io/docker-agent/features/harnesses/).

# Distribution

- **Packaging:** `docker agent share push|pull` stores an agent as an OCI artifact in any registry.
  - `docker agent run <ref>` runs one straight from a registry.
  - The artifact has a single `application/yaml` layer, with config media type `application/vnd.docker.agent.config.v1+json`.
  - `instruction_file` contents are inlined. Prompt files, scripts, RAG sources and MCP binaries are not packaged.
- **Signing:** optional, with `--key`. The proof is a DSSE in-toto statement stored in a manifest annotation.
  - It covers the YAML layer only, and the annotations themselves are not authenticated.
  - A rollback under the same tag is not detected.
  - `cosign verify-attestation` does not find it, and no keyless (Sigstore) signing is offered.
  - Pin by digest.

Sources: [Docker Agent - Distribution](https://docker.github.io/docker-agent/concepts/distribution/); [docker/docker-agent at commit 3c9f6d45 (release v1.160.0)](https://github.com/docker/docker-agent/tree/3c9f6d45b8682e02713e85330fc98b5157e4896d).

# Runtime modes

| Mode | Command | Protocol | Notes |
|------|---------|----------|-------|
| TUI | `docker agent run` | terminal | Default. `--listen` exposes a local, unauthenticated control API. |
| Headless | `run --exec` | stdout, `--json` NDJSON events | One-shot or multi-turn. `structured_output` for JSON answers. |
| API server | `serve api` | REST under `/api`, SSE | Full session lifecycle. Default `127.0.0.1:8080`. |
| Chat server | `serve chat` | OpenAI-compatible `/v1/chat/completions` | Stateless by default. Agent name is the model name. |
| A2A | `serve a2a` | A2A agent card and JSON-RPC | "Early support". No tool-call events or artifacts. |
| ACP | `serve acp` | Agent Client Protocol, stdio only | For IDEs. Uses the host's credentials. |
| MCP | `serve mcp` | MCP over stdio or stateless HTTP | Each agent becomes an MCP tool. Default safety `restricted`. |

- **The API server is the richest surface:**
  - Agents can be loaded from a file, a directory, an OCI ref or a URL, with `--pull-interval` hot reload.
  - Session endpoints cover create, list, get, delete, fork, steer, follow-up (with `Idempotency-Key`), tool-call approval (`/resume`), elicitation and per-session safety policy.
  - Each turn streams typed SSE events, for example `agent_choice`, `tool_call`, `tool_call_confirmation`, `token_usage` and `budget_exceeded`.
  - Auth is one shared bearer token, which is mandatory on non-loopback addresses. There is no TLS, and the request body cap is 1 MiB by default.
  - One turn is `POST /api/sessions` (with a title, or a second model call names the session), then `POST /api/sessions/{id}/agent/{agent}`, whose stream ends with `stream_stopped`; `agent_info` names the model the agent runs and the last `agent_choice` holds its answer (seen with v1.160.0 in feature 001, research D27).
- **Sessions are stored in SQLite (`session.db`), with migrations.**
  - One run per session at a time.
  - Live run state, budgets and idempotency keys are held in memory.
  - The docs say several instances can share a database, but they do not describe how concurrent writers are coordinated.
- **Go SDK:** the runtime, team loader, API server and `session.Store` interface can be embedded, so the caller can supply its own session store.
  - A remote client for `serve api` exists.
  - The TypeScript SDK contains types only.
- **Evaluation:** `docker agent eval` runs recorded sessions in containers, scored with assertions, tool-call F1 and an LLM or evaluator judge.
  - `--repeat` reports pass@k and pass^k.
  - `--baseline` with `--regression-tolerance` exits non-zero on a regression, so it can be a CI gate.

Sources: [Docker Agent - API server](https://docker.github.io/docker-agent/features/api-server/); [Docker Agent - Headless mode](https://docker.github.io/docker-agent/guides/headless/); [Docker Agent - Chat server](https://docker.github.io/docker-agent/features/chat-server/); [Docker Agent - A2A](https://docker.github.io/docker-agent/features/a2a/); [Docker Agent - ACP](https://docker.github.io/docker-agent/features/acp/); [Docker Agent - MCP mode](https://docker.github.io/docker-agent/features/mcp-mode/); [Docker Agent - Sessions](https://docker.github.io/docker-agent/features/sessions/); [Docker Agent - Go SDK](https://docker.github.io/docker-agent/guides/go-sdk/); [docker/docker-agent at commit 3c9f6d45 (release v1.160.0)](https://github.com/docker/docker-agent/tree/3c9f6d45b8682e02713e85330fc98b5157e4896d); [Docker Agent - Evaluation](https://docker.github.io/docker-agent/features/evaluation/).

# Security model

- **Permissions:** `deny` rules are checked first, then `allow`, then `ask`, matching on tool-name globs and arguments.
  - Calls that match no rule fall to a safety mode: `strict`, `balanced`, `restricted` (denies without asking) or `autonomous` (formerly `--yolo`).
  - The docs state that permissions "are enforced client-side" and "should not be relied upon as a security boundary for untrusted agents."
  - A config pulled from a registry can declare `autonomous`. It applies when the user has set nothing.
- **Sandbox:** `--sandbox` runs the whole agent in a Docker Sandboxes microVM.
  - The working dir is mounted read-write and the config is mounted read-only.
  - Egress goes through a deny-by-default proxy that allows the model providers, and the proxy injects credentials.
  - Author YAML can widen egress with `runtime.network_allowlist`.
  - A `--cloud` variant runs in a remote workspace.
  - The docs do not describe the hypervisor or the strength of the isolation.
- **Hooks:** about 35 events, covering tool input and output, guards, user input, LLM calls, compaction, agent routing and the session lifecycle.
  - Hooks can be commands, in-process builtins, LLM judges or evaluators.
  - `tool_guard` and `pre_tool_use` fail closed.
  - Global hooks (`settings.hooks`, `hooks.d/`) apply to every agent and cannot be opted out of.
  - A drop-in file that fails to parse is skipped with a warning.
- **Secrets:** taken from environment variables, `/run/secrets/` (Compose), `~/.config/cagent/.env`, 1Password `op://` refs and the Docker Desktop secrets backend.
  - Pattern-based `redact_secrets` is on by default and misses some secrets.
  - The only per-caller credential mechanism is `${headers.NAME}`, which forwards a header from the incoming request in server modes.
- **Tool risk surface:**
  - The filesystem tool can reach any path the process can, unless `allow_list` is set.
  - Shell and stdio MCP servers inherit the host environment.
  - `fetch`, `api` and `openapi` block private IPs by default.
  - `mcp_catalog` lets the model enable remote MCP servers at runtime unless restricted.
  - Memory is a shared SQLite file per config, with no access control.
- **Telemetry:** Docker product telemetry is **on by default**, opt out with `TELEMETRY_ENABLED=false`.
  - It can include command arguments, prompts and file paths.
- **OpenTelemetry:** opt-in with `--otel`, OTLP over HTTP, using the `gen_ai.*` and `mcp.*` conventions.
  - Message content is captured only when `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=true`.

Sources: [Docker Agent - Permissions](https://docker.github.io/docker-agent/configuration/permissions/); [Docker Agent - Sandbox mode](https://docker.github.io/docker-agent/configuration/sandbox/); [Docker Agent - Hooks](https://docker.github.io/docker-agent/configuration/hooks/); [Docker Agent - Managing secrets](https://docker.github.io/docker-agent/guides/secrets/); [Docker Agent - Tool configuration](https://docker.github.io/docker-agent/configuration/tools/); [Docker Agent - Telemetry](https://docker.github.io/docker-agent/configuration/telemetry/); [Docker Agent - OpenTelemetry](https://docker.github.io/docker-agent/configuration/opentelemetry/).

# Gaps for a multi-tenant platform

Docker Agent is built for one user on one machine. Its docs never claim that the API
server is safe to run for several tenants. A platform built on it must add:

- **Identity and authorization:** users, tenants, roles, per-session ownership and delegated credentials.
  - The server knows only one static bearer token, and `GET /api/sessions` lists every session.
- **Tenant isolation:** a separate runtime or sandbox per tenant or session, with its own data dir, memory, RAG index and credentials.
  - Memory, RAG and provider keys are shared within one process today.
- **Policy authority:** the platform, not the agent author, must own safety mode, permissions, hooks and egress.
  - It must reject author attempts to widen them, and make its policy config fail closed.
- **Durability and scale:**
  - A shared session store (for example by implementing `session.Store`).
  - Session-affinity routing or a job model.
  - Recovery of runs a restart interrupts, since in-memory run state is lost.
- **Quotas and metering:** persistent per-tenant budgets and rate limits.
  - Session budgets reset when the process restarts.
- **Audit:** an append-only audit log built from hooks and traces. There is none built in.
- **Supply chain:**
  - Allowlisted registries, digest pinning and mandatory signature verification for agent artifacts.
  - The same for the MCP images and sandbox templates they reference.
- **Version control of the dependency:** pin one Docker Agent version and re-validate before each upgrade, given the release pace and breaking minor releases.
- **Data handling:** disable product telemetry platform-wide, keep OTel content capture off by default, and add retention and erasure for sessions.

Sources: [Docker Agent - API server](https://docker.github.io/docker-agent/features/api-server/); [Docker Agent - Tool configuration](https://docker.github.io/docker-agent/configuration/tools/); [Docker Agent - Managing secrets](https://docker.github.io/docker-agent/guides/secrets/); [Docker Agent - Permissions](https://docker.github.io/docker-agent/configuration/permissions/); [Docker Agent - Hooks](https://docker.github.io/docker-agent/configuration/hooks/); [Docker Agent - Go SDK](https://docker.github.io/docker-agent/guides/go-sdk/); [Docker Agent - Budgets](https://docker.github.io/docker-agent/configuration/budget/); [Docker Agent - Distribution](https://docker.github.io/docker-agent/concepts/distribution/); [Docker Agent - Sandbox mode](https://docker.github.io/docker-agent/configuration/sandbox/); [docker/docker-agent at commit 3c9f6d45 (release v1.160.0)](https://github.com/docker/docker-agent/tree/3c9f6d45b8682e02713e85330fc98b5157e4896d); [Docker Agent - Telemetry](https://docker.github.io/docker-agent/configuration/telemetry/); [Docker Agent - OpenTelemetry](https://docker.github.io/docker-agent/configuration/opentelemetry/).
