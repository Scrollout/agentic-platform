---
type: Reference
title: Contract-driven frontend toolchain
description: Research on building the web console, VS Code views and dashboards against the API contracts, in parallel with the backend; one recommendation per concern with licence, maintenance and risk, as of 2026-11-11.
tags: [references, frontend, ui, dashboards, testing]
status: draft
stale_after: 2027-05-11T00:00:00Z
sources:
  - id: react-scratch
    resource: https://react.dev/learn/build-a-react-app-from-scratch
    title: Build a React app from Scratch (react.dev)
  - id: tanstack-router
    resource: https://tanstack.com/router/latest/docs/framework/react/overview
    title: "TanStack Router: Type-Safe Routing with Search Param Validation"
  - id: vscode-webview
    resource: https://code.visualstudio.com/api/extension-guides/webview
    title: Webview API (Visual Studio Code Extension API)
  - id: shadcn
    resource: https://ui.shadcn.com/docs
    title: Introduction - shadcn/ui
  - id: radix-a11y
    resource: https://www.radix-ui.com/primitives/docs/overview/accessibility
    title: Accessibility - Radix Primitives
  - id: shadcn-base-ui
    resource: https://ui.shadcn.com/docs/changelog/2026-07-base-ui-default
    title: July 2026 - Base UI as the Default (shadcn/ui changelog)
  - id: react-aria-quality
    resource: https://react-aria.adobe.com/quality
    title: Quality - React Aria
  - id: openapi-react-query
    resource: https://openapi-ts.dev/openapi-react-query/
    title: openapi-react-query (OpenAPI TypeScript)
  - id: storybook-vitest
    resource: https://storybook.js.org/docs/writing-tests/integrations/vitest-addon
    title: Vitest addon (Storybook docs)
  - id: msw
    resource: https://mswjs.io/docs/
    title: Introduction - Mock Service Worker
  - id: openapi-msw
    resource: https://github.com/christoph-fricke/openapi-msw
    title: openapi-msw
  - id: prism
    resource: https://github.com/stoplightio/prism
    title: Prism
  - id: pactflow-bdct
    resource: https://pactflow.io/bi-directional-contract-testing/
    title: Bi-Directional Contract Testing (PactFlow)
  - id: specmatic-pricing
    resource: https://specmatic.io/pricing/
    title: Pricing - Specmatic
  - id: playwright-snapshots
    resource: https://playwright.dev/docs/test-snapshots
    title: Visual comparisons (Playwright)
  - id: vitest-browser
    resource: https://vitest.dev/guide/browser/
    title: Browser Mode (Vitest)
  - id: perses-cncf
    resource: https://www.cncf.io/projects/perses/
    title: Perses (CNCF)
  - id: perses
    resource: https://perses.dev/
    title: Perses
  - id: agpl
    resource: https://www.gnu.org/licenses/agpl-3.0.txt
    title: GNU Affero General Public License v3.0
  - id: grafana-licensing
    resource: https://grafana.com/blog/2021/04/20/qa-with-our-ceo-on-relicensing/
    title: Q&A with Grafana Labs CEO Raj Dutt about our licensing changes
  - id: echarts-aria
    resource: https://echarts.apache.org/handbook/en/best-practices/aria/
    title: Aria - Best Practices (Apache ECharts Handbook)
  - id: orval-query
    resource: https://orval.dev/docs/guides/react-query
    title: React Query (Orval docs)
  - id: orval-msw
    resource: https://orval.dev/docs/guides/msw
    title: MSW (Orval docs)
  - id: pact-matching
    resource: https://docs.pact.io/getting_started/matching
    title: Matching (Pact docs)
  - id: pact-broker
    resource: https://docs.pact.io/pact_broker
    title: Introduction (Pact Broker docs)
  - id: playwright-docker
    resource: https://playwright.dev/docs/docker
    title: Docker (Playwright)
  - id: playwright-aria
    resource: https://playwright.dev/docs/aria-snapshots
    title: Snapshot testing (Playwright)
  - id: deque-study
    resource: https://www.deque.com/blog/automated-testing-study-identifies-57-percent-of-digital-accessibility-issues
    title: Automated testing study identifies 57 percent of digital accessibility issues (Deque)
  - id: storybook-upgrade
    resource: https://storybook.js.org/docs/releases/upgrading
    title: Upgrading Storybook
  - id: tanstack-splitting
    resource: https://tanstack.com/router/latest/docs/framework/react/guide/automatic-code-splitting
    title: Automatic Code Splitting (TanStack Router)
  - id: base-ui-csp
    resource: https://base-ui.com/react/utils/csp-provider
    title: CSP Provider (Base UI)
  - id: clickstack
    resource: https://github.com/ClickHouse/ClickStack
    title: ClickStack
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T22:30:00Z }
---

# Summary

Contract-first lets the frontend be built at the same time as the backend. The UI uses clients
generated from each service's contract and AHP's SDK, and is developed and tested against mocks
generated from the same contracts. Licence, latest release and recent activity were checked on
2026-11-11 for every tool below. The choices are proposed in
[ADR 0027](/decisions/0027-contract-driven-frontend.md).

# Recommendations

These are the choices after a second research pass that set out to remove every weakness of the
first pass. The first-pass evidence follows; the second pass is under "Removing the weaknesses".

| Concern | Choice | Licence | Remaining risk |
|---------|--------|---------|----------------|
| Web console | React 19 + TypeScript single-page app on Vite, TanStack Router with automatic code splitting and loaders | MIT | — |
| Components | shadcn/ui (code copied into the repo) on its React Aria base, Tailwind CSS v4 | MIT, Apache-2.0 | The React Aria base is new (July 2026); WCAG 2.2 conformance is still ours to test |
| Client, REST state, mocks | Orval: fetch client, TanStack Query v5 hooks, MSW v3 handlers, Zod schemas | MIT | — |
| Live-session state | AHP SDK reducers held in Zustand; XState only for declared workflow screens | MIT | — |
| Component workbench | Storybook 10 with its Vitest addon | MIT | Upgrades, mitigated |
| UI-to-API contract | Ajv fixture validation, Pact OSS with a self-hosted broker, Prism proxy in end-to-end runs, Schemathesis and oasdiff | MIT, Apache-2.0 | Pact interactions are a second artefact |
| End-to-end, visual, accessibility | Playwright in a pinned image (screenshots, ARIA snapshots, keyboard, reflow, forced colours), axe-core, IBM Equal Access, a webview CSP gate, a signed manual record | Apache-2.0, MPL-2.0 | Small pixel tolerance; some WCAG criteria need a person |
| Operations dashboards | OpenTelemetry Collector into ClickHouse, ClickStack as the UI, Jaeger v2 optional | MIT, Apache-2.0 | ClickStack single sign-on and roles unverified |
| Product charts | Recharts through shadcn/ui's chart component; ECharts lazy-loaded where needed | MIT, Apache-2.0 | Patterns beyond colour must be added by hand |

# Evidence

- **Single-page app, not server rendering.** An authenticated console needs no server rendering.
  React's own guide says build tools "start off with a client-only, single-page app (SPA)". A
  single-page app suits a long-lived WebSocket with reducer state, and the same components run
  in VS Code webviews.
  - Webviews forbid inline scripts and styles by default and are themed through `--vscode-*` CSS
    variables, so the build must emit external files and the design system must use CSS
    variables.
  - Microsoft's Webview UI Toolkit is archived and must not be used.

Sources: [Build a React app from Scratch (react.dev)](https://react.dev/learn/build-a-react-app-from-scratch); [TanStack Router: Type-Safe Routing with Search Param Validation](https://tanstack.com/router/latest/docs/framework/react/overview); [Webview API (Visual Studio Code Extension API)](https://code.visualstudio.com/api/extension-guides/webview).

- **Components we own.** shadcn/ui is "not a component library… a way to build your own": the
  code lives in our repo, with no runtime vendor lock.
- **Modern primitives, not a vendor design system.** Fluent UI (Microsoft's design system, used by
  the Azure portal) and similar packaged libraries bring their maker's look and a heavy runtime.
  Headless primitives give behaviour and accessibility without styling, and we style them.
  - **Base UI** (MIT, by the MUI team; v1.9.0, 2026-10-09) became shadcn/ui's default in July 2026.
    shadcn/ui still supports Radix and ships every component for both.
  - **Radix** (MIT) still works, but its repository has no recent releases and activity is slowing.
    It follows WAI-ARIA authoring practices but makes no WCAG claim.
  - **React Aria Components** (Apache-2.0, by Adobe; v1.22.0, 2026-10-09) are tested with
    VoiceOver, JAWS, NVDA and TalkBack across Safari, Chrome and Firefox, include localised strings
    for 30+ languages and support right-to-left interaction.
  - **Ark UI** (MIT, by the Chakra UI team) builds every component on Zag state machines, which
    matches the platform's state-machine approach, but its community is smaller.
  - WCAG 2.2 conformance remains ours to test and record, whatever the base.

Sources: [Introduction - shadcn/ui](https://ui.shadcn.com/docs); [July 2026 - Base UI as the Default (shadcn/ui changelog)](https://ui.shadcn.com/docs/changelog/2026-07-base-ui-default); [Accessibility - Radix Primitives](https://www.radix-ui.com/primitives/docs/overview/accessibility); [Quality - React Aria](https://react-aria.adobe.com/quality).

- **State.** openapi-react-query is a small typed wrapper that connects the generated
  openapi-fetch client to TanStack Query. AHP's host owns the reducers, so the console keeps
  their state in a small store (Zustand) and adds no reducer framework of its own.

Sources: [openapi-react-query (OpenAPI TypeScript)](https://openapi-ts.dev/openapi-react-query/).

- **One fixture set.** MSW handlers work in the browser, Node, Storybook and Vitest, so stories,
  tests and local development share the same mock data. openapi-msw types the handlers from the
  generated contract types, so a fixture that drifts from the contract fails to compile. A test
  also checks every fixture against the schema at run time. Prism can proxy end-to-end traffic and
  validate real responses against the contract.
  - Storybook's Vitest addon runs stories as component tests in a real browser, with accessibility
    checks.

Sources: [Introduction - Mock Service Worker](https://mswjs.io/docs/); [openapi-msw](https://github.com/christoph-fricke/openapi-msw); [Prism](https://github.com/stoplightio/prism); [Vitest addon (Storybook docs)](https://storybook.js.org/docs/writing-tests/integrations/vitest-addon).

- **No paid contract-testing tier.** PactFlow's bi-directional contract testing "is exclusive to
  PactFlow", so it fails Principle VI. Open-source Pact with a self-hosted broker would work, but
  duplicates the OpenAPI contract. Specmatic's open-source tier is MIT, but advanced OpenAPI
  features are paid. Schema-based checks avoid both.

Sources: [Bi-Directional Contract Testing (PactFlow)](https://pactflow.io/bi-directional-contract-testing/); [Pricing - Specmatic](https://specmatic.io/pricing/).

- **Deterministic UI checks.** Playwright's screenshot baselines differ by browser, platform and
  hardware, so baselines are generated only inside one pinned container image (Principle XX).
  Vitest browser mode runs component tests in a real browser.

Sources: [Visual comparisons (Playwright)](https://playwright.dev/docs/test-snapshots); [Browser Mode (Vitest)](https://vitest.dev/guide/browser/).

- **Operations dashboards and licences.** Perses is a CNCF Sandbox project under Apache-2.0, with
  dashboards as code and panels that can be embedded in our own UI. Grafana is AGPL-3.0: running
  it unmodified is fine, but a modified version must offer its source to every remote user,
  including every hosted tenant.

Sources: [Perses (CNCF)](https://www.cncf.io/projects/perses/); [Perses](https://perses.dev/); [GNU Affero General Public License v3.0](https://www.gnu.org/licenses/agpl-3.0.txt); [Q&A with Grafana Labs CEO Raj Dutt about our licensing changes](https://grafana.com/blog/2021/04/20/qa-with-our-ceo-on-relicensing/).

- **Accessible charts.** ECharts generates ARIA labels and decal patterns, so series are not told
  apart by colour alone. Both are off by default and must be turned on.

Sources: [Aria - Best Practices (Apache ECharts Handbook)](https://echarts.apache.org/handbook/en/best-practices/aria/).

# Removing the weaknesses

- **One maintained generator.** openapi-typescript and its query and mock companions had no
  release since 2026-02-11, and Hey API (ADR 0004's fallback) has had no stable release since June
  2026 and one maintainer. Orval (MIT) releases weekly, had about 25 authors in the last three
  months, and generates the client, TanStack Query v5 hooks, MSW handlers with fake data, and Zod
  schemas in one step. Its OpenAPI 3.1 support is under active work rather than complete.

Sources: [React Query (Orval docs)](https://orval.dev/docs/guides/react-query); [MSW (Orval docs)](https://orval.dev/docs/guides/msw).

- **Real usage, not just shape.** Pact verification checks only the fields the consumer declares
  and, with type matchers, checks types rather than values. The open-source Pact libraries for
  TypeScript and Go and the self-hosted broker are MIT, and the broker's `can-i-deploy` blocks a
  deploy that would break a consumer. pact-msw-adapter was rejected: it writes literal bodies with
  no matching rules, which only suits PactFlow's paid feature.

Sources: [Matching (Pact docs)](https://docs.pact.io/getting_started/matching); [Introduction (Pact Broker docs)](https://docs.pact.io/pact_broker).

- **Reproducible screenshots.** Playwright says to "always pin your Docker image"; animations and
  the caret are disabled by default, and dynamic regions can be masked. Lost Pixel is archived, so
  Playwright alone is used.

Sources: [Docker (Playwright)](https://playwright.dev/docs/docker); [Visual comparisons (Playwright)](https://playwright.dev/docs/test-snapshots).

- **More accessibility automation.** Deque's own study found automated axe tests catch 57% of
  issues by volume. Playwright's ARIA snapshots record roles, names and states; keyboard order,
  reflow and forced colours can be tested too; IBM Equal Access is a second rule engine. The rest
  needs a person and a recorded result.

Sources: [Automated testing study identifies 57 percent of digital accessibility issues (Deque)](https://www.deque.com/blog/automated-testing-study-identifies-57-percent-of-digital-accessibility-issues); [Snapshot testing (Playwright)](https://playwright.dev/docs/aria-snapshots).

- **Storybook stays.** Its alternatives are stale, and its `upgrade` command runs automigrations
  and checks. Upgrading every minor version keeps each step small.

Sources: [Upgrading Storybook](https://storybook.js.org/docs/releases/upgrading).

- **Code splitting without a framework.** TanStack Router splits code per route automatically.
  React Router's SPA mode emits inline scripts, which VS Code webviews forbid.

Sources: [Automatic Code Splitting (TanStack Router)](https://tanstack.com/router/latest/docs/framework/react/guide/automatic-code-splitting).

- **Inline styles found and fixed.** Base UI's ScrollArea and Select inject `<style>` elements
  unless told not to, and shadcn's chart component does too. A test under the exact webview policy
  catches any that remain.

Sources: [CSP Provider (Base UI)](https://base-ui.com/react/utils/csp-provider); [Webview API (Visual Studio Code Extension API)](https://code.visualstudio.com/api/extension-guides/webview).

- **Operations UI.** ClickStack is MIT, OpenTelemetry-native, and stores logs, traces and metrics in
  ClickHouse. SigNoz's enterprise directories need a subscription in production, and Grafana,
  OpenObserve and Uptrace are AGPL-3.0, so none of them is the default.

Sources: [ClickStack](https://github.com/ClickHouse/ClickStack); [Perses](https://perses.dev/).

- **Lighter charts.** Measured with the same chart set: ECharts 205 KB gzip even with modular
  imports, Recharts 125 KB with its accessibility layer on by default.

# Unverified

- Whether MSW's service worker registers inside VS Code webviews.
- ClickStack's single sign-on and role-based access in the open-source build.
- OpenSSF Scorecard results for tools with no published score.
- That AI coding agents produce more correct code in React and TypeScript than in other frameworks.
