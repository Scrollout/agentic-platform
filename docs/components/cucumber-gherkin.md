---
type: Component
title: cucumber/gherkin and godog
description: "The Gherkin parser the slice model uses to trace examples now, and the godog runner that will execute them as tests in Story 2."
tags: [components, testing, gherkin]
status: draft
resource: https://github.com/cucumber/gherkin
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research
  - id: godog-repo
    resource: https://github.com/cucumber/godog
    title: cucumber/godog repository
  - id: gherkin-repo
    resource: https://github.com/cucumber/gherkin
    title: cucumber/gherkin repository
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-13T12:00:00Z }
---

# Responsibility

A rule's examples are Gherkin scenarios under a `Rule:` block (research D3). **gherkin** parses them
(shipped); **godog** will run them with `--strict` so an undefined step fails (planned for Story 2,
US2; no Go module requires it yet). Licences MIT; OpenSSF Scorecards: gherkin 7.2, messages 7.2,
godog 6.2, google/uuid 6.9 (research D22).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [cucumber/gherkin](https://github.com/cucumber/gherkin), [cucumber/godog](https://github.com/cucumber/godog).

# Interfaces

`slice.Trace` and `slice.ExampleTags` in `src/slice` call `gherkin.ParseGherkinDocument` and check that
every rule has an example, every scenario carries its `@R-<n>` and its own `@E-<n>`. The Story 2
godog check is planned in `src/engine/checks`.

# Dependencies

`cucumber/messages` (required by gherkin; owner-approved 2026-11-12) and `google/uuid` v1.6.0 (indirect).

# Configuration

`github.com/cucumber/gherkin/go/v42 v42.0.1` and `github.com/cucumber/messages/go/v34 v34.2.0` in
`src/slice/go.mod`. godog is pinned as `github.com/cucumber/godog/cmd/godog@v0.16.0` in `images.lock`
for the check image.

# Operations

Not yet used: the godog step check.
