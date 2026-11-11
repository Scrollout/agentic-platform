---
type: Decision
title: "ADR 0004: Contract-first toolchain, service-owned APIs and versioning"
description: Each microservice owns its OpenAPI 3.1 contract and operations list; APIs use ARM-style date-based stable and preview api-versions per service; open-source generators and a CI tool set enforce contract, compatibility, conformance and drift.
tags: [decisions, apis, contracts, openapi, sdk, versioning]
status: draft
decision: accepted
sources:
  - id: research
    resource: /references/contract-first-development.md
    title: Contract-first API development reference
  - id: arm
    resource: /references/arm-api-versioning.md
    title: Azure Resource Manager API versioning reference
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's requests and acceptance, 2026-11-11 (each microservice carries its own API operations; versioning follows Azure RM stable and preview; ADR accepted)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T22:45:00Z }
---

# Origin

The constitution's Contract-first rule needs concrete formats and tools. The project
owner asked for two more things:

- each microservice carries its own API operations;
- API versioning follows Azure Resource Manager's stable and preview model.

This record also serves as the versioning and deprecation policy that Principle XV requires.
The project owner accepted it on 2026-11-11.

Sources: [Contract-first API development reference](/references/contract-first-development.md); [Project owner's requests, 2026-11-11 (each microservice carries its own API operations; versioning follows Azure RM stable and preview)](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).

# Context

- Platform APIs are designed contract-first: contract, generated SDKs and server interfaces,
  failing tests, then code.
- Generators must be open-source (Principle VI).
- The runtime is Go (Docker Agent). Interactive clients use AHP, which publishes JSON Schema
  2020-12 and has official SDKs.
- **Azure's model:**
  - Date-based `api-version`s.
  - Stable versions never break, and previews are short-lived.
  - Microsoft now makes the service, not the resource type, the unit that versions.
- **Where ARM falls short:** a quarter of its resource types never left preview, defaults are years
  old, and versions drift per type. Rules kept only as policy decayed.

Sources: [Azure Resource Manager API versioning reference](/references/arm-api-versioning.md).

# Decision

## Each microservice owns its API

- **A microservice is a provider** with its own namespace (Principle VII). It owns exactly one
  versioned OpenAPI 3.1 contract and the operations in it.
- **Contracts live with the service** at `src/<service>/api/stable/<YYYY-MM-DD>/openapi.yaml` and
  `src/<service>/api/preview/<YYYY-MM-DD-preview>/openapi.yaml`. Contracts are never shared between
  services; common parts are copied or published as a versioned schema.
- **Operations list:** every service serves `GET /providers/{namespace}/operations`.
  - It does not take an api-version.
  - It lists `{namespace}/{type}/{read|write|delete|action}` with display text.
  - It is the vocabulary for permissions (Principle V).
- **Registration:** every service registers a manifest with the control plane's provider registry.
  - The manifest holds its namespace, versions with their stage (stable or preview) and sunset
    dates, its routes, and its operations list.
  - The gateway checks the namespace and api-version against the registry before routing.
  - It serves the combined `/providers` list and a merged OpenAPI catalogue.

## Versioning follows Azure's stable and preview model

| Rule | Choice |
|------|--------|
| Format | `YYYY-MM-DD` (stable) or `YYYY-MM-DD-preview`, nothing else. Dates rise strictly within a service, and a stable version never reuses a preview's date. |
| Carrier | Required `api-version` query parameter on every operation, including polling and next-page URLs. Not a header, so the version has one home, and never in the path (owner, 2026-11-11). |
| Unit | The service. All operations of a service version together, and one version is one contract and one SDK version. No per-resource-type versions. |
| Missing or unknown version | 400 `MissingApiVersionParameter`, or 400 `UnsupportedApiVersionValue` listing supported stable versions and the latest preview. A retired version gets the same error. |
| No default | There is no default api-version. Tools that need one use the latest stable, computed, never set by hand. |
| Stable | Never breaks, in shape or behaviour: status codes, error codes, required fields, pagination, rate limits and permissions. A published version is never edited, except for a bug fix that a named reviewer approves. |
| Preview | A superset of the latest stable, which may break its own preview-only features from one preview to the next. A feature leaves preview within 12 months, as stable or removed. When a stable version ships, unfinished features move to a later-dated preview. |
| Versions served | Every stable version not yet retired, plus the latest preview only. Older previews retire 90 days after a newer version ships. |
| Retiring a stable version | Only after a breaking successor exists, with at least 12 months' notice (owner, 2026-11-11). Security fixes may shorten it, with the owner's approval. |
| Signalling | `Deprecation` (RFC 9745) and `Sunset` (RFC 8594) headers plus `Link: rel="deprecation"`, mirrored in the contract as `deprecated: true` and `x-sunset`. |

## Toolchain

| Concern | Choice |
|---------|--------|
| HTTP contract format | OpenAPI 3.1.x, written as YAML split across files, bundled with Redocly CLI |
| First SDK languages | TypeScript (web console, VS Code) and Go (CLI, server, Docker Agent integration) |
| Go server interfaces and client | oapi-codegen, strict-server mode |
| TypeScript client | Orval: fetch client, TanStack Query hooks, MSW mocks and Zod schemas in one step. Amended by [ADR 0027](/decisions/0027-contract-driven-frontend.md), which replaced openapi-typescript and openapi-fetch with Hey API as fallback. |
| Event payloads | JSON Schema 2020-12, shared with OpenAPI by `$ref` |
| Event envelope | CloudEvents 1.0.2 for domain events and webhooks |
| Event channel description | AsyncAPI 3.1, only where a non-AHP channel needs one |
| AHP | AHP's published schema is the contract. It is not re-modelled. |
| Lint | Spectral, with a project ruleset |
| Breaking-change gate | oasdiff, keyed to stable vs preview (below) |
| Conformance and fuzz tests | Schemathesis against the running server |
| Mocks | Prism |
| Drift check | Regenerate in CI, then `git diff --exit-code` |

## CI enforcement

Rules that live only in policy decay, as ARM shows, so CI checks every one of them:

- **New stable version:** `oasdiff breaking --fail-on ERR` against the previous stable blocks the
  merge. Against the previous preview it only warns.
- **Published versions:** any edit to a published version fails unless a named reviewer approves
  it as a bug fix.
- **Removal before sunset:** removing anything before its `x-sunset` date fails. oasdiff runs with
  `--deprecation-days-stable=365` and `--deprecation-days-beta=90`.
- **Layout:** folder names must match the version format, and no preview may sit under `stable/`.
- **Preview age:** a preview older than 12 months with no stable successor fails the build.
- **Operations list:** each service's operations list must match the operations in its contract.

Sources: [Azure Resource Manager API versioning reference](/references/arm-api-versioning.md).

# Consequences

- **Simpler clients:** versioning by service means a small change to one resource bumps the whole
  service, but SDKs and docs stay simple and versions cannot drift per type.
- **Query parameter, not header or path:**
  - It matches ARM, and HTTP caches key on it by default.
  - Polling and next-page URLs carry their own version, and it is easy to use from a browser or
    curl.
  - Header versioning (GitHub, Stripe) keeps URLs identical, but caches must vary on the header,
    and both services fall back to a default version when it is missing.
  - Path versioning (Google) only fits major versions.
  - Caching matters little here: management responses are authenticated and per caller.
  - The cost: every generated URL must carry the parameter, which CI and the generated clients
    take care of.
- **Notice period:** the owner chose 12 months for stable versions, Microsoft's lifecycle floor.
  Azure gives at least 3 years for its own services. A service may give longer notice.
- **Tools:** every listed tool is OSI-licensed (MIT or Apache-2.0) and runs locally or in CI,
  without a hosted service.
- **OpenAPI 3.2:** we stay on 3.1 until our generators support 3.2 fully. Validators already
  support it.
- **TypeScript generator:** the original choice, openapi-typescript, stopped releasing in
  February 2026 and its fallback, Hey API, stalled in June 2026. ADR 0027 replaced both with Orval,
  which releases weekly and has many maintainers.
- **Mobile schemas:** schemas meant for a later Kotlin SDK should avoid `oneOf` and `anyOf`, which
  the Kotlin generator does not support.
- **AsyncAPI and JSON Schema 2020-12:** AsyncAPI's schemas are based on JSON Schema draft-07. Using
  2020-12 payloads needs a custom `schemaFormat`, and tool support for that must be checked before
  the first AsyncAPI document.
- **Authoring format:** TypeSpec would be shorter, but it adds a language, and its server emitters
  are alpha. Revisit it if several APIs or versioned variants appear.
- **Not chosen:**
  - Smithy: lossy OpenAPI output and a Java toolchain.
  - OpenAPI Generator for Go and TypeScript: no `oneOf`/`anyOf` support.
  - Dredd: archived.
  - AWS API Gateway SDK generation: works only after an API is deployed.
  - Per-resource-type versions and default versions: the main sources of ARM's drift.

Sources: [Azure Resource Manager API versioning reference](/references/arm-api-versioning.md).

# Follow-up

- Python, Swift and Kotlin SDKs, when there is demand: openapi-python-client,
  swift-openapi-generator, and OpenAPI Generator `kotlin` (multiplatform).
- Write the Spectral ruleset and the CI checks above.
- Define the provider manifest's schema and the registry's API, contract-first.
- Add a `docs/components/` concept for each tool when the first API adopts it.
