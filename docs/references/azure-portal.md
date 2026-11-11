---
type: Reference
title: Azure portal architecture
description: How the Azure portal is built against Azure Resource Manager (shell, per-provider extensions, public ARM calls, Resource Graph lists, permission-aware UI, declarative forms) and what our console should adopt or avoid.
tags: [references, frontend, ui, azure]
status: draft
stale_after: 2027-05-11T00:00:00Z
sources:
  - id: arch
    resource: https://web.archive.org/web/20221230070917/https://github.com/Azure/portaldocs/blob/main/portal-sdk/generated/top-extensions-architecture.md
    title: Azure Portal - A composed web application (Azure/portaldocs, archived 2025)
  - id: host-ev2
    resource: https://web.archive.org/web/20250122144736/https://github.com/Azure/portaldocs/blob/main/portal-sdk/generated/top-extensions-hosting-service-ev2.md
    title: Ev2 Integration with hosting service (Azure/portaldocs)
  - id: assets
    resource: https://web.archive.org/web/20231109074711/https://github.com/Azure/portaldocs/blob/main/portal-sdk/generated/declarative-assets.md
    title: Azure Portal assets (Azure/portaldocs)
  - id: fork
    resource: https://github.com/tonyxiuningji/portaldocs/tree/master/portal-sdk/generated
    title: Verbatim fork of the Azure portal SDK docs, 2020-11-05 (onboarding, hosting, ReactViews, permissions, batching, notifications, performance, accessibility)
  - id: ga
    resource: https://azure.microsoft.com/en-us/blog/announcing-azure-portal-general-availability/
    title: Announcing Azure Portal general availability (Azure blog, 2015-12-02)
  - id: design
    resource: https://microsoft.design/articles/designing-for-scale-and-complexity/
    title: Designing for scale and complexity (Microsoft Design, 2018)
  - id: arg
    resource: https://learn.microsoft.com/en-us/azure/governance/resource-graph/overview
    title: Overview of Azure Resource Graph (Microsoft Learn)
  - id: export
    resource: https://learn.microsoft.com/en-us/azure/azure-resource-manager/templates/export-template-portal
    title: Export template in Azure portal (Microsoft Learn)
  - id: cuid
    resource: https://learn.microsoft.com/en-us/azure/azure-resource-manager/managed-applications/create-uidefinition-overview
    title: CreateUiDefinition.json file for portal pane (Microsoft Learn)
  - id: forms
    resource: https://learn.microsoft.com/en-us/azure/azure-resource-manager/templates/template-specs-create-portal-forms
    title: Create portal forms for template spec (Microsoft Learn)
  - id: dash
    resource: https://learn.microsoft.com/en-us/azure/azure-portal/azure-portal-dashboards-structure
    title: The structure of Azure dashboards (Microsoft Learn)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T21:20:00Z }
---

# Summary

The Azure portal is one web app composed from UI built by hundreds of service teams. A shell
draws the chrome, and each resource provider's team owns an extension that it deploys on its own
schedule. Extensions call the same public ARM APIs as every other client, lists come from a query
service over ARM, and actions are shown according to the user's ARM permissions.

The portal's SDK docs were archived in January 2025 and moved to an internal Microsoft site, so
SDK-level facts here come from archived snapshots and a 2020 fork, and may be dated. How we adopt
the model is in [ADR 0027](/decisions/0027-contract-driven-frontend.md).

Sources: [Azure Portal - A composed web application (Azure/portaldocs, archived 2025)](https://web.archive.org/web/20221230070917/https://github.com/Azure/portaldocs/blob/main/portal-sdk/generated/top-extensions-architecture.md).

# Architecture

- **Shell and extensions.** The portal is "a UI composition system whose primary design goal is to
  enable the integration of UI built by hundreds of teams into a single, robust single-page web
  application." The shell renders the chrome and hosts pages (blades) and tiles (parts) that
  extensions provide.
- **Ownership.** Each service team builds and deploys its own extension "at a deployment cadence
  that suits their team", and a bug in one extension "has only a local impact". Most extensions
  map to one ARM resource provider.
- **Loading.** Extensions are static web apps loaded on demand from an endpoint registered
  centrally, and unloaded when unused.
- **Isolation.** Each extension runs in its own iframe on its own origin. The original design (2013)
  copied view models into the visible shell frame because browsers could not handle many iframes;
  since about 2018 the portal has moved to ordinary visible iframes.
- **Auth.** Each extension gets a token scoped to it and calls ARM or its provider directly. A
  per-extension backend is "less common, not recommended".
- **Deployment.** Teams upload a versioned bundle of static files to a hosting service that serves
  it in every datacenter. Rollouts are staged over 6 or 24 hours with revert to the last good
  version, and a preview portal can run a different extension version against production ARM.
- **Quality bars.** Extensions must meet production-readiness metrics before release, with p95
  targets such as extension load under 2 s and page ready under 4 s.

Sources: [Azure Portal - A composed web application (Azure/portaldocs, archived 2025)](https://web.archive.org/web/20221230070917/https://github.com/Azure/portaldocs/blob/main/portal-sdk/generated/top-extensions-architecture.md); [Ev2 Integration with hosting service (Azure/portaldocs)](https://web.archive.org/web/20250122144736/https://github.com/Azure/portaldocs/blob/main/portal-sdk/generated/top-extensions-hosting-service-ev2.md); [Verbatim fork of the Azure portal SDK docs, 2020-11-05](https://github.com/tonyxiuningji/portaldocs/tree/master/portal-sdk/generated); [Announcing Azure Portal general availability (Azure blog, 2015-12-02)](https://azure.microsoft.com/en-us/blog/announcing-azure-portal-general-availability/).

# Technology

- **Typed navigation.** Pages are opened through typed references generated from each extension's
  code, which other extensions consume as a package. A page's parameters are a public API: never
  rename a page, add only optional parameters, never remove one.
- **React views** are the recommended way to build new experiences. They run with full DOM access,
  use Redux for state and a small portal API for navigation and tokens, and load Fluent UI once
  from the portal so every extension shares it.
- **A definition language was dropped.** The portal's first XAML-like page language was "found to
  be developer-unfriendly" and replaced with TypeScript.
- **Accessibility.** Framework controls are accessible as shipped. Extensions own any custom markup,
  must keep portal shortcuts working and must support high-contrast mode. The design system has
  four themes and a contrast-checked colour set.

Sources: [Azure Portal - A composed web application (Azure/portaldocs, archived 2025)](https://web.archive.org/web/20221230070917/https://github.com/Azure/portaldocs/blob/main/portal-sdk/generated/top-extensions-architecture.md); [Verbatim fork of the Azure portal SDK docs, 2020-11-05](https://github.com/tonyxiuningji/portaldocs/tree/master/portal-sdk/generated); [Designing for scale and complexity (Microsoft Design, 2018)](https://microsoft.design/articles/designing-for-scale-and-complexity/).

# How the portal uses ARM

- **Public APIs.** Extensions call `management.azure.com` with an `api-version`, like any client.
  Each asset type declares the resource type and the api-version it uses. Whether the portal also
  uses private ARM APIs is unverified.
- **Batching.** "If you're calling ARM you should always batch your request": up to 20 reads per
  batch, merged automatically by the framework.
- **Lists from a query service.** Browse, search and "All resources" are powered by Azure Resource
  Graph, "a query-able caching layer over ARM". It is fed by ARM change notifications plus a
  periodic full scan, is not strongly consistent, reads each provider's latest stable API version
  and enforces read permissions.
- **Long-running operations.** A notification centre combines client and server events, and
  polling backs off over time. The docs advise storing the operation id so progress survives a
  page reload.
- **Permission-aware UI.** Each element declares the ARM action it needs, such as
  `Microsoft.Robotics/robots/restart/action`. Checks fail open, because the backend has the final
  say. Readers see everything except secrets, and controls are disabled rather than hidden.
- **Exports.** "Export template" generates Bicep or ARM JSON from published schemas. It can lag
  the schemas and "is not guaranteed to succeed". Dashboards are ARM resources themselves.

Sources: [Azure Portal assets (Azure/portaldocs)](https://web.archive.org/web/20231109074711/https://github.com/Azure/portaldocs/blob/main/portal-sdk/generated/declarative-assets.md); [Verbatim fork of the Azure portal SDK docs, 2020-11-05](https://github.com/tonyxiuningji/portaldocs/tree/master/portal-sdk/generated); [Overview of Azure Resource Graph (Microsoft Learn)](https://learn.microsoft.com/en-us/azure/governance/resource-graph/overview); [Export template in Azure portal (Microsoft Learn)](https://learn.microsoft.com/en-us/azure/azure-resource-manager/templates/export-template-portal); [The structure of Azure dashboards (Microsoft Learn)](https://learn.microsoft.com/en-us/azure/azure-portal/azure-portal-dashboards-structure).

# Declarative UI

- **Create forms.** `createUiDefinition.json` declares a form's steps, elements and outputs mapped
  to template parameters, with validations such as permission checks.
- **Generated defaults.** The portal can generate a default form straight from template
  parameters, for example a drop-down from `allowedValues`, which authors then refine.
- **Asset types in JSON** declare list queries and columns, bulk commands and the resource menu.
- No source shows the portal generating pages from providers' OpenAPI specs. Generation stops at
  template parameters.

Sources: [CreateUiDefinition.json file for portal pane (Microsoft Learn)](https://learn.microsoft.com/en-us/azure/azure-resource-manager/managed-applications/create-uidefinition-overview); [Create portal forms for template spec (Microsoft Learn)](https://learn.microsoft.com/en-us/azure/azure-resource-manager/templates/template-specs-create-portal-forms); [Azure Portal assets (Azure/portaldocs)](https://web.archive.org/web/20231109074711/https://github.com/Azure/portaldocs/blob/main/portal-sdk/generated/declarative-assets.md).

# Weaknesses

- The iframe-and-copied-view-model design was a workaround for 2013 browsers and is being walked
  back.
- Freer page models shift accessibility and consistency work onto each team, and React views still
  lack parity with the older controls.
- Generated outputs are weak: exports lag the schemas, and lists are eventually consistent and hide
  preview-only properties.
- Extension versions are independent of api-versions, so each asset pins its own api-version.
- The SDK docs went private in 2025, which makes the model hard to learn from outside.

Sources: [Azure Portal - A composed web application (Azure/portaldocs, archived 2025)](https://web.archive.org/web/20221230070917/https://github.com/Azure/portaldocs/blob/main/portal-sdk/generated/top-extensions-architecture.md); [Verbatim fork of the Azure portal SDK docs, 2020-11-05](https://github.com/tonyxiuningji/portaldocs/tree/master/portal-sdk/generated); [Export template in Azure portal (Microsoft Learn)](https://learn.microsoft.com/en-us/azure/azure-resource-manager/templates/export-template-portal); [Overview of Azure Resource Graph (Microsoft Learn)](https://learn.microsoft.com/en-us/azure/governance/resource-graph/overview).

# Unverified

- Whether the portal uses private ARM APIs.
- A public reference for ARM's batch endpoint.
- The current number of extensions; the latest wording is "hundreds of teams".
- SDK details after 2020, beyond the archived snapshots.
