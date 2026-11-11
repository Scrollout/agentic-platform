---
type: Decision
title: "ADR 0027: Contract-driven frontend"
description: The console is a shell with one UI module per service, modelled on the Azure portal, built against the API contracts and AHP in parallel with the backend; every known weakness of the stack is removed or mitigated.
tags: [decisions, frontend, ui, dashboards]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's question, 2026-11-11 (contract-first allows frontend development in parallel; what about frontend, UI and dashboards)
  - id: research
    resource: /references/frontend-toolchain.md
    title: Contract-driven frontend toolchain reference
  - id: adr-0004
    resource: /decisions/0004-contract-first-toolchain.md
    title: "ADR 0004: Contract-first toolchain, service-owned APIs and versioning"
  - id: portal
    resource: /references/azure-portal.md
    title: Azure portal architecture reference
  - id: adr-0003
    resource: /decisions/0003-ahp-client-session-protocol.md
    title: "ADR 0003: AHP is the client-facing session protocol"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T22:45:00Z }
---

# Origin

The project owner asked how the frontend, UI and dashboards fit contract-first development,
which lets frontend work run in parallel with the backend. The project owner
accepted this record on 2026-11-11, including starting the console as one app and one build.

Sources: [Project owner's question, 2026-11-11 (contract-first allows frontend development in parallel; what about frontend, UI and dashboards)](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).

# Context

- Each microservice owns an OpenAPI 3.1 contract, and a TypeScript client is generated from it.
- Interactive clients share live sessions through AHP, whose host owns the state and its reducers.
- Clients are a web console with product dashboards, a VS Code extension and a Go CLI. Mobile
  comes later.
- Principle VI allows only free, OSI-licensed open source. Principle XX requires that
  deterministic checks decide acceptance, including for UI work done by agents.

Sources: [ADR 0004: Contract-first toolchain, service-owned APIs and versioning](/decisions/0004-contract-first-toolchain.md); [ADR 0003: AHP is the client-facing session protocol](/decisions/0003-ahp-client-session-protocol.md).

# Decision

## Console structure, modelled on the Azure portal

The Azure portal is one app composed from UI owned by each resource provider's team, calling only
public ARM APIs. We adopt its model, without its 2013-era workarounds.

- **Shell plus one UI module per service.** The console is a shell (navigation, sign-in,
  notifications, search). Each microservice team owns one UI module for its namespace, the way each
  service owns its API (ADR 0004).
- **UI manifest in the registry.** Each module registers a UI manifest next to its OpenAPI
  contract in the control plane's provider registry. It declares the module's resource types,
  display names and icons, list columns, resource menu, the operation each command needs, any
  form overlay, and the routes it exports. The manifest has a published schema and is designed
  contract-first.
- **Typed navigation between modules.** Each module exports generated, typed route references.
  Routes are a public API: a route is never renamed or removed, and new parameters are optional.
  CI diffs the route types the way oasdiff diffs contracts.
- **Everything the UI does is a public API call.** No module has its own backend for the browser.
  Reads go through a gateway batch endpoint (at most 20 reads per batch). User dashboards and saved
  views are ordinary resources, so they can be exported and automated.
- **Lists come from a query service.** Every list and search reads a platform query service, like
  Azure Resource Graph, fed by change events plus a periodic full scan. It reads stable
  api-versions only and enforces read permissions. It is eventually consistent, and the UI shows
  when the data was last updated. Detail views call the owning service directly.
- **Actions follow permissions.** Every command maps to an entry in its service's
  `/providers/{namespace}/operations` list. The shell checks permissions in one batched call. A
  command the user lacks permission for is disabled, not hidden, and readers see everything except
  secrets. The backend always has the final say.
- **Long-running operations survive a reload.** The notification centre keeps operation ids on the
  server and follows them through AHP or backed-off polling.
- **Forms are generated from the contract.** Create and edit forms are generated from the
  operation's request schema (an enum becomes a drop-down, labels come from `x-` extensions). An
  optional overlay can add steps, visibility rules and validations, and CI lints the overlay
  against the contract.
- **"Show as API or CLI".** Any action can show the exact API request or CLI command it sends,
  instead of a lossy template export.
- **One app now, composition later.** Modules are packages in one repository and one build,
  loaded per route from the manifest. Run-time composition (independently deployed bundles) is
  deferred until teams really need separate release schedules; the manifest and typed routes keep
  that move cheap.
- **Isolation by trust.** First-party modules run in the console's own page. UI written by tenants,
  third parties or agents runs only in sandboxed, cross-origin iframes with a strict content
  security policy, using a small message-passing SDK for navigation, tokens and notifications.
- **Shared once.** React and the design system load once from the shell. Modules cannot bundle
  their own copies.
- **Versions are independent.** A UI module's version is independent of api-versions. Each module
  pins the api-versions its generated clients use, and preview api-versions appear only behind a
  feature flag in a preview console channel.
- **Release gates.** p95 module load under 2 s and view ready under 4 s. Each module ships as a
  versioned bundle with a staged rollout, a preview channel and one-step revert.
- **Portal weaknesses and how this design answers them:**
  - *The iframe workaround.* The portal copied view models into one shared frame because 2013
    browsers could not run many iframes, and is now walking that back. We never copy view models:
    first-party modules render directly in the console's page, and only untrusted UI runs in
    ordinary sandboxed iframes.
  - *Quality varies between extensions.* The portal reports quality weekly after release; here every
    module must pass the same conformance suite before it ships, so quality is a gate, not a report.
    The suite checks accessibility (axe-core plus keyboard navigation), the shell's shortcuts and
    themes including high contrast, the performance budgets, visual baselines, and that the module
    uses only design-system components. Custom markup or styling outside the design system needs a
    recorded exception.
  - *Exports lag the schemas.* The portal builds exports from published schemas, separately from
    the calls it makes, so they drift. Here "Show as API or CLI" prints the request the generated
    client actually sends, so it cannot lag. A resource's export is its declared state in the
    contract's own format, and CI proves the round trip: applying an export reproduces the same
    state.
  - *Lists hide preview properties.* The query service reads stable api-versions only, as Resource
    Graph does; preview properties appear only in detail views, behind the preview flag.
- **Not adopted from the portal:** copying view models into a shared frame, a custom page
  definition language, per-extension backends, free-form framed UIs without shared controls, and a
  closed SDK.

Sources: [Azure portal architecture reference](/references/azure-portal.md).

## How the frontend works in parallel

- **One generation step.** Orval generates, from each service's contract, the typed client, the
  TanStack Query hooks, the MSW mock handlers with seeded fake data, and Zod schemas. The UI calls
  services only through that client, and live sessions only through AHP's TypeScript SDK. No
  hand-written API calls.
- **Mocks from the contracts.** The generated MSW handlers drive local development, Storybook and
  tests, and every fixture is validated against the contract with Ajv. A recorded-session mock of
  the AHP host, validated against AHP's JSON Schema, covers live sessions. UI work starts as soon as
  a contract merges, before the backend exists.
- **The UI's real usage reaches the backend.** The console's Pact consumer tests call the
  generated client and record which operations, fields and status codes it relies on, with type
  matchers built from the same fixtures. Each Go service verifies them with pact-go, and a
  self-hosted Pact Broker blocks a deploy that would break the UI (`can-i-deploy`). Every operation
  the UI's tests use must have a Pact interaction. During end-to-end runs, Prism in proxy mode
  checks real responses against the contract.
- **Components in isolation.** Every component and screen is built in Storybook with
  contract-shaped data, and tested there for behaviour and accessibility.

## Stack

| Concern | Choice |
|---------|--------|
| Web console | React 19 + TypeScript single-page app on Vite (`assetsInlineLimit: 0`), TanStack Router with automatic code splitting and route loaders |
| Shared design system | A package used by the console and VS Code webviews: shadcn/ui on its React Aria base, Tailwind CSS v4, CSS variables mapped to `--vscode-*` in VS Code, no inline `<style>`. No packaged vendor design system such as Fluent UI. |
| Client, REST state, mocks | Orval: fetch client, TanStack Query v5 hooks, MSW v3 handlers, Zod schemas |
| Live-session state | AHP SDK reducers in Zustand; XState only for screens of a declared workflow (ADR 0026) |
| Workbench | Storybook 10 with the Vitest addon |
| Contract checks | Ajv fixture validation; Pact OSS (pact-js, pact-go, self-hosted broker); Prism proxy in end-to-end runs; Schemathesis and oasdiff on the services |
| Tests | Playwright in a digest-pinned image: screenshots, ARIA snapshots, keyboard order, reflow and forced colours; axe-core plus IBM Equal Access; a webview CSP gate |
| Forms and routes | Forms from the contract with react-jsonschema-form or JSON Forms; route reports diffed with API Extractor |
| Product dashboards | Built into the console; charts with Recharts through shadcn/ui's chart component, ECharts lazy-loaded for heavy charts |
| Operations dashboards | OpenTelemetry Collector into ClickHouse, with ClickStack as the operator UI; Jaeger v2 optional for traces |

## Determinism rules for the UI

- **State is rendered, not invented.** Live-session screens render AHP state and never keep a
  second copy of it.
- **Workflow screens are declared.** Approval and review screens show the workflow's current
  state and offer only the transitions it allows.
- **Screenshots are reproducible.** Baselines come only from Playwright's own image, pinned by
  digest, on one platform and Chromium only, with animations and the caret disabled, self-hosted
  fonts, a frozen clock and seeded fake data. Dynamic regions are masked.
- **Accessibility is checked, then recorded.** Automated checks run on every change. The rest of
  WCAG 2.2 AA is checked by hand per release and recorded in one machine-readable report (EARL, in
  the WCAG-EM format) with a row per criterion, the method, the assistive technology used and the
  build digest. CI fails if a criterion is missing, the digest differs, or no person has signed it.
- **The webview policy is tested.** A test serves the build under the exact VS Code webview content
  security policy and fails on any violation.
- **Agent UI work is guarded.** An agent may change only its component's files. It cannot edit the
  stories, tests, baselines or accessibility record that judge its work.

## Weaknesses: removed or mitigated

| Weakness | Outcome |
|----------|---------|
| openapi-typescript has not released since 2026-02-11 | Removed: Orval, released weekly by about 25 recent authors |
| The MSW generator had one maintainer | Removed: Orval generates the mocks too |
| Schema checks prove shape, not usage | Mostly removed: Pact OSS records the UI's real usage and each service verifies it. Pact interactions are a second artefact, built from the same fixtures |
| Screenshot tests are flaky | Mitigated: pinned image and the rules above. A small pixel tolerance remains, because rendering still varies slightly |
| axe-core finds only part of WCAG (57% of issues by volume, per Deque) | Mitigated: more automated checks plus a signed, machine-checked manual record. Some criteria can only be judged by a person |
| Storybook upgrades are heavy | Mitigated: official addons only, tests through Vitest, grouped updates, upgrade every minor. Storybook is still the best-maintained option |
| Perses is pre-1.0 | Removed: ClickStack (MIT) on ClickHouse is the operator UI. Perses stays a watch item for embedded panels |
| ECharts is large (205 KB gzip even modular) | Removed: Recharts (125 KB) with its accessibility layer on by default; ECharts only where needed |
| Routing, data and code splitting assembled by hand | Removed: TanStack Router's documented automatic code splitting and loaders. Framework SPA modes were rejected because they emit inline scripts |
| Radix is slowing | Removed: shadcn/ui's React Aria base, one primitive library instead of two |
| VS Code webviews forbid inline scripts and styles | Removed by configuration and the CSP gate. Our earlier claim that the stack emitted none was wrong: Base UI and shadcn's chart component inject `<style>` |

Sources: [Contract-driven frontend toolchain reference](/references/frontend-toolchain.md).

# Consequences

- Frontend work starts the day a contract merges, and a change that would break the UI is blocked
  on the service side before it deploys.
- The console and VS Code share one design system, tested under the webview policy.
- **Amends ADR 0004:** this record replaces ADR 0004's TypeScript client
  (openapi-typescript and openapi-fetch, with Hey API as fallback) by Orval. Hey API has stalled
  since June 2026 and depends on one maintainer.
- **Remaining watch items:** React Aria is shadcn/ui's newest base (July 2026); Pact adds a broker
  to run.
- **Licences:** every choice is MIT or Apache-2.0, except axe-core (MPL-2.0, used unmodified).
  SigNoz's enterprise directories and AGPL-3.0 tools (Grafana, OpenObserve, Uptrace) are not
  chosen; AGPL tools would have to offer their source to every hosted tenant if modified.
- **Not chosen:** Fluent UI and other packaged vendor design systems, Radix and Base UI as the
  base, server-rendering frameworks and framework SPA modes, PactFlow and pact-msw-adapter (built
  for PactFlow's paid feature), Specmatic, Lost Pixel (archived), Ladle and Histoire (stale),
  Dredd and swagger-mock-validator (archived), Microsoft's Webview UI Toolkit (archived).

Sources: [Contract-driven frontend toolchain reference](/references/frontend-toolchain.md).

# Follow-up

- Run OpenSSF Scorecard locally on each chosen tool; several have no published score.
- Verify that MSW's service worker can register inside VS Code webviews; otherwise mock at the
  extension's message layer.
- Verify ClickStack's single sign-on and role-based access in the open-source build.
- Choose the tool that measures the performance budgets.
- Define the UI manifest schema and the query service's API, contract-first.
- Build the shell, the shared design-system package and one service's UI module as a walking
  skeleton.
- Add a `docs/components/` concept for each tool when the first UI work adopts it.
