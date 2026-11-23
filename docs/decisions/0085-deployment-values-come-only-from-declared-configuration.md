---
type: Decision
title: "ADR 0085: Deployment values come only from declared configuration"
description: The delivered service's configuration is declared once in the pattern's template (gen/config.go, LISTEN_ADDRESS with its default), and the code step is rejected for an address, port, URL or credential literal and for reading the environment itself.
tags: [decisions, checks, catalogue, configuration]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/009-no-hard-coded-values/plan.md
    title: Feature 009 plan, Decisions
  - id: research
    resource: ../../specs/009-no-hard-coded-values/research.md
    title: Feature 009 research, E2, R3 and R4
  - id: spec
    resource: ../../specs/009-no-hard-coded-values/spec.md
    title: Feature 009 specification, Clarifications
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-23T20:00:00Z }
---

# Origin

The plan of [feature 009](../../specs/009-no-hard-coded-values/plan.md) (need N-059, owner
2026-11-23); the owner chose that the code step's code never reads the environment itself.

Sources: [Feature 009 plan, Decisions](../../specs/009-no-hard-coded-values/plan.md); [Feature 009 specification, Clarifications](../../specs/009-no-hard-coded-values/spec.md).

# Context

- Every code answer the platform has recorded listens on `":8080"` written into `main.go`.
- The constitution takes deploy configuration from environment variables.
- gosec's hard-coded credential rule found none of four plain credentials in an experiment.

Sources: [Feature 009 research, E2 and S8](../../specs/009-no-hard-coded-values/research.md); [gosec hard-coded credentials](/references/gosec-hardcoded-credentials.md).

# Decision

- The pattern's template holds `gen/config.go`: `ListenAddress()` reads `LISTEN_ADDRESS`,
  default `:8080`, and its doc comment documents every configuration value with its default.
- The code step's check DeploymentValuesConfigured rejects a string literal that is a URL, an IP
  address or a `host:port`, a credential literal by its call or name, and any call of
  `os.Getenv`, `os.LookupEnv`, `os.Environ` or `os.ExpandEnv` in the code step's code.

Sources: [Feature 009 research, R3 and R4](../../specs/009-no-hard-coded-values/research.md).

# Consequences

- A delivered service moves by setting `LISTEN_ADDRESS`, never by changing its code.
- A new configuration value is added to the template, by a pattern change, not by the agent.
- A port written as an integer, or a credential under another name or call, is not caught
  (stated limits).

Sources: [Feature 009 plan](../../specs/009-no-hard-coded-values/plan.md).
