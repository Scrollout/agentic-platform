---
type: Component
title: godog
description: "The Cucumber test runner for Go, v0.16.0, which runs a slice's Gherkin examples with --strict so that an undefined step fails and every example is proven bound to a test."
tags: [components, go, testing, gherkin, checks]
status: draft
resource: https://github.com/cucumber/godog
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, S6, S7, D3 and D22
  - id: images-lock
    resource: ../../src/deploy/images.lock
    title: src/deploy/images.lock
  - id: dockerfile
    resource: ../../src/deploy/Dockerfile.check
    title: src/deploy/Dockerfile.check
  - id: template
    resource: ../../src/catalogue/api-feature-go/template/go.mod
    title: The api-feature-go template go.mod
  - id: steps
    resource: ../../src/engine/checks/steps.go
    title: src/engine/checks/steps.go, GodogStrict
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/cucumber/godog
    title: OpenSSF Scorecard for cucumber/godog
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-17T22:00:00Z }
---

# Responsibility

It runs the Gherkin examples of a slice against the generated project's tests. The platform uses
`--strict` (the `Strict` option of godog's test suite), which fails the suite on pending,
undefined or ambiguous steps. That proves every example is bound to a test (research D3, S7). The
engine's check `GodogStrict`, which implements the slice check `EveryExampleBound`, runs
`go test -json -count=1 ./tests/...`, reads godog's own summary and fails when a scenario is
unbound or the counts do not match the feature file. Licence MIT (research S6, D22).

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [steps.go](../../src/engine/checks/steps.go).

# Interfaces

- Library: the generated project's tests import `github.com/cucumber/godog` and its `colors`
  package. The template's `vendortools` package imports both so the later steps build offline.
- Command: none in the platform's images; the `godog` pin in `images.lock` names the version the
  template's tests are vendored at.
- Related: [cucumber-gherkin](/components/cucumber-gherkin.md), which parses the feature files.

Sources: [api-feature-go template go.mod](../../src/catalogue/api-feature-go/template/go.mod), [Dockerfile.check](../../src/deploy/Dockerfile.check).

# Dependencies

The generated project requires `github.com/cucumber/godog v0.16.0` (an indirect requirement in the
template's `go.mod`). Its modules are vendored for offline builds.

Sources: [api-feature-go template go.mod](../../src/catalogue/api-feature-go/template/go.mod).

# Configuration

Pin: `godog  github.com/cucumber/godog/cmd/godog@v0.16.0` in `src/deploy/images.lock`. The
template's `go.mod` requires the same version.

Sources: [src/deploy/images.lock](../../src/deploy/images.lock).

# Operations

Updated through the monthly dependency review. Keep the pin and the template's requirement at one
version.

# Supply chain

OpenSSF Scorecard 6.2 (2026-11-09; research D22, confirmed on the Scorecard API on 2026-11-17).
Licence MIT.

Sources: [Feature 001 research, D22](../../specs/001-prose-to-accepted-slice/research.md), [OpenSSF Scorecard for cucumber/godog](https://api.securityscorecards.dev/projects/github.com/cucumber/godog).
