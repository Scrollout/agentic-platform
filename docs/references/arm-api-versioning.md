---
type: Reference
title: Azure Resource Manager API versioning
description: How Azure versions APIs (date-based stable and preview api-versions, per-service ownership, breaking-change gates), where ARM falls short of its own rules, and what that means for us.
tags: [references, apis, versioning, azure]
status: draft
stale_after: 2027-05-11T00:00:00Z
sources:
  - id: guidelines
    resource: https://github.com/microsoft/api-guidelines/blob/vNext/azure/Guidelines.md
    title: Microsoft Azure REST API Guidelines
  - id: csd
    resource: https://github.com/microsoft/api-guidelines/blob/vNext/azure/ConsiderationsForServiceDesign.md
    title: Considerations for Service Design
  - id: vg
    resource: https://github.com/microsoft/api-guidelines/blob/vNext/azure/VersioningGuidelines.md
    title: Azure Versioning Guidelines
  - id: svg
    resource: https://github.com/Azure/azure-rest-api-specs/wiki/Azure-Service-Versioning-Guideline
    title: Azure Service Versioning Guide
  - id: learn-policy
    resource: https://learn.microsoft.com/en-us/azure/developer/intro/azure-service-sdk-tool-versioning
    title: Versioning policy for Azure services, SDKs, and CLI tools
  - id: lifecycle
    resource: https://learn.microsoft.com/en-us/lifecycle/policies/modern
    title: Modern Lifecycle Policy
  - id: register-errors
    resource: https://learn.microsoft.com/en-us/azure/azure-resource-manager/troubleshooting/error-register-resource-provider
    title: Resource provider registration errors
  - id: rpc
    resource: https://github.com/brrusino/azure-resource-manager-rpc
    title: Azure Resource Manager resource provider contract (public fork, last updated 2021)
  - id: specs-ci
    resource: https://github.com/Azure/azure-rest-api-specs/blob/main/documentation/ci-fix.md
    title: azure-rest-api-specs CI checks
  - id: specs-rules
    resource: https://github.com/Azure/azure-rest-api-specs/tree/main/eng/tools/openapi-diff-runner
    title: azure-rest-api-specs openapi-diff runner (breaking-change detection and rule application)
  - id: rfc9745
    resource: https://www.rfc-editor.org/rfc/rfc9745
    title: "RFC 9745: The Deprecation HTTP Response Header Field"
  - id: rfc8594
    resource: https://www.rfc-editor.org/rfc/rfc8594
    title: "RFC 8594: The Sunset HTTP Header Field"
  - id: rfc9111
    resource: https://www.rfc-editor.org/rfc/rfc9111.html
    title: "RFC 9111: HTTP Caching"
  - id: aip-185
    resource: https://google.aip.dev/185
    title: "AIP-185: API Versioning"
  - id: github-versions
    resource: https://docs.github.com/en/rest/about-the-rest-api/api-versions
    title: GitHub REST API - API Versions
  - id: stripe-versions
    resource: https://docs.stripe.com/api/versioning
    title: Stripe API - Versioning
  - id: probes
    resource: ~/azure-arm/ARCHITECTURE.md
    title: Owner's reverse-engineered ARM architecture from live probes, 2026-11-03 to 2026-11-05 (local, not published)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Summary

Azure versions every API with a date: `YYYY-MM-DD` for stable versions and `YYYY-MM-DD-preview`
for previews. The version travels as a required `api-version` query parameter.

- Stable versions never break.
- Previews may break, and live a short time.
- Several versions are served at once.
- Microsoft's current guidance makes the **service**, not each resource type, the unit that
  versions.

ARM itself only partly follows these rules, and the owner's probes measure the gap. How we
adopt the model is proposed in [ADR 0004](/decisions/0004-contract-first-toolchain.md).

Sources: [Microsoft Azure REST API Guidelines](https://github.com/microsoft/api-guidelines/blob/vNext/azure/Guidelines.md); [Azure Service Versioning Guide](https://github.com/Azure/azure-rest-api-specs/wiki/Azure-Service-Versioning-Guideline); Owner's reverse-engineered ARM architecture from live probes, 2026-11-03 to 2026-11-05 (local, not published) (`~/azure-arm/ARCHITECTURE.md`).

# Rules

- **Format:** `YYYY-MM-DD` or `YYYY-MM-DD-preview`.
  - Each new preview takes a later date.
  - A GA version never reuses a preview's date.
  - A new preview's date must be later than the latest GA.
- **Carrier:** a required `api-version` query parameter on every operation, including polling and
  next-page URLs. A version in the path is forbidden.
- **Errors:**
  - A missing version returns 400 `MissingApiVersionParameter`.
  - An unsupported version returns 400 `UnsupportedApiVersionValue`, listing the supported stable
    versions and the latest preview.
  - A retired version behaves like an unknown one.
- **Stable:**
  - No breaking changes, so a client moves to a newer stable version without code changes.
  - A published version, stable or preview, never changes behaviour. Changes go in a new version.
  - "Breaking" includes behaviour as well as shape: changed status or error codes, adding
    pagination later, new required fields, stricter rate limits, changed permissions.
- **Preview:**
  - A preview is a superset of the latest stable. It may break its own preview-only features from
    one preview to the next.
  - Azure asks for at least two previews before the first GA.
  - A feature stays in preview for at most one year, then goes GA or is removed.
  - Older previews are retired 90 days after a newer GA or preview ships.
- **Notice:**
  - After a breaking successor ships, old stable versions stay at least 3 years; for security
    issues, 1 year or less.
  - The Modern Lifecycle floor is 12 months' notice.
  - Previews can retire with 90 days' notice.
  - Azure signals retirement with a free-text `azure-deprecating` header.
  - The IETF standards are the `Deprecation` header (RFC 9745) and the `Sunset` header
    (RFC 8594).

Sources: [Microsoft Azure REST API Guidelines](https://github.com/microsoft/api-guidelines/blob/vNext/azure/Guidelines.md); [Azure Versioning Guidelines](https://github.com/microsoft/api-guidelines/blob/vNext/azure/VersioningGuidelines.md); [Versioning policy for Azure services, SDKs, and CLI tools](https://learn.microsoft.com/en-us/azure/developer/intro/azure-service-sdk-tool-versioning); [Considerations for Service Design](https://github.com/microsoft/api-guidelines/blob/vNext/azure/ConsiderationsForServiceDesign.md); [Azure Service Versioning Guide](https://github.com/Azure/azure-rest-api-specs/wiki/Azure-Service-Versioning-Guideline); [Modern Lifecycle Policy](https://learn.microsoft.com/en-us/lifecycle/policies/modern); [RFC 9745: The Deprecation HTTP Response Header Field](https://www.rfc-editor.org/rfc/rfc9745); [RFC 8594: The Sunset HTTP Header Field](https://www.rfc-editor.org/rfc/rfc8594).

# Ownership

- **Service, not resource type:** "An Azure service is a set of operations that version uniformly
  (in perpetuity)."
  - One service version is one contract, one set of docs and one SDK package.
  - Microsoft names versioning parts of a service separately as the main problem it is fixing.
- **Specs layout:** each service owns its contract in its own folder, under `preview/<date>-preview/`
  and `stable/<date>/`. Contracts are copied between services, never shared.
- **Operations list:** each resource provider serves `GET /providers/{namespace}/operations`.
  - It lists `{namespace}/{type}/{read|write|delete|action}`.
  - It doubles as the vocabulary for permissions.
- **Manifest:** ARM keeps every provider's manifest and checks the namespace, type and
  api-version before routing.
  - The order is: token, api-version present, RBAC, policy, locks, manifest.

Sources: [Azure Service Versioning Guide](https://github.com/Azure/azure-rest-api-specs/wiki/Azure-Service-Versioning-Guideline); [Azure Resource Manager resource provider contract (public fork, last updated 2021)](https://github.com/brrusino/azure-resource-manager-rpc); Owner's reverse-engineered ARM architecture from live probes, 2026-11-03 to 2026-11-05 (local, not published) (`~/azure-arm/ARCHITECTURE.md`).

# CI enforcement in azure-rest-api-specs

- **Checks:** pull requests run `oad` (Azure/openapi-diff) twice.
  - Same-version: an edit to a published version.
  - Cross-version: against the previous stable and the previous preview.
- **Stable vs preview:**
  - Breaking against the previous stable is an error that blocks the merge.
  - Breaking against the previous preview is only a warning.
- **Approval:** an edit to a published version, or an approved break, needs a label that only a
  named reviewer group may apply.

Sources: [azure-rest-api-specs CI checks](https://github.com/Azure/azure-rest-api-specs/blob/main/documentation/ci-fix.md); [azure-rest-api-specs openapi-diff runner (breaking-change detection and rule application)](https://github.com/Azure/azure-rest-api-specs/tree/main/eng/tools/openapi-diff-runner).

# Where ARM falls short

Counts are from the owner's probes of 4,813 resource types.

- **Previews outlive the one-year rule.**
  - 1,203 types (25%) have no stable version.
  - 438 of those have a newest preview older than 2024.
- **Defaults are stale.**
  - 873 types default to a preview.
  - 527 default to an older stable than their latest.
  - Microsoft.Network serves stable up to 2026-07-01, yet several of its types default to 2020-03-01.
- **Versions drift per type.**
  - 155 of 316 namespaces have types on different newest versions.
  - One type lists 98 versions.
- **Retirement is free text** in operation descriptions. There is no standard header.
- **The live error contradicts the guideline.** An unsupported version returns 404
  `NoRegisteredProviderFound`, not 400 `UnsupportedApiVersionValue`.

The lesson: rules held only in policy decay. The one-year preview limit, the defaults and
per-type drift need checks in CI, not guidance.

Sources: Owner's reverse-engineered ARM architecture from live probes, 2026-11-03 to 2026-11-05 (local, not published) (`~/azure-arm/ARCHITECTURE.md`); [Resource provider registration errors](https://learn.microsoft.com/en-us/azure/azure-resource-manager/troubleshooting/error-register-resource-provider); [Microsoft Azure REST API Guidelines](https://github.com/microsoft/api-guidelines/blob/vNext/azure/Guidelines.md).

# Where the version goes

| Carrier | Who uses it | For | Against |
|---------|-------------|-----|---------|
| Query parameter `api-version` | Azure ARM and Azure data planes | Part of the HTTP cache key by default, since the key is the method plus the full target URI. Polling and next-page URLs carry their own version. Visible in logs and easy to use from a browser or curl. | Every generated URL must carry it. Some CDNs drop query strings from the cache key unless configured. |
| Header (`X-GitHub-Api-Version`, `Stripe-Version`) | GitHub, Stripe | URLs stay identical across versions. | Caches must be told to vary on the header. Both services fall back to a default version when the header is missing, the drift ARM shows. |
| Path (`/v1/`) | Google | Simplest for caches and routing. | Major versions only. It does not fit date-based versions per service, and it puts the version into every resource's URL. Azure forbids it. |

Caching matters less than it looks for this platform: management responses are per caller and
authenticated, and a shared cache may not reuse a response to a request carrying `Authorization`
unless the response explicitly allows it.

Sources: [Microsoft Azure REST API Guidelines](https://github.com/microsoft/api-guidelines/blob/vNext/azure/Guidelines.md); [RFC 9111: HTTP Caching](https://www.rfc-editor.org/rfc/rfc9111.html); [GitHub REST API - API Versions](https://docs.github.com/en/rest/about-the-rest-api/api-versions); [Stripe API - Versioning](https://docs.stripe.com/api/versioning); [AIP-185: API Versioning](https://google.aip.dev/185).

# Unverified

- The current internal resource provider contract. The public copies are from 2021 or now return 404.
- Azure's internal breaking-change policy tables.
- ARM's exact 404 response body.
- The date of the Service Versioning Guide wiki page.
