---
type: Decision
title: "ADR 0079: The method identity is a digest of the method's declared sources and pins"
description: The parts of the platform that produce and judge a step's result are declared once in src/engine/method/method.yaml; their identity is the SHA-256 of a manifest of those files and pins, derived by scripts/method-digest.py, checked by the test command and embedded in the engine.
tags: [decisions, engine, reuse, reproducibility]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/006-method-record/plan.md
    title: Feature 006 plan, Decisions
  - id: research
    resource: ../../specs/006-method-record/research.md
    title: Feature 006 research, R1, R2 and E1
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-22T15:00:00Z }
---

# Origin

The plan of [feature 006](../../specs/006-method-record/plan.md) (backlog T136 with T144; the
owner chose T136 on 2026-11-22 and the whole-method comparison the same day).

Sources: [Feature 006 plan, Decisions](../../specs/006-method-record/plan.md).

# Context

- Constitution Principle XX requires every accepted result to record the method, inputs, model
  and rule versions that produced it.
- The pattern's declared version stayed `1.0.0` through six changes to `pattern.yaml`, and nothing
  named the checks', procedures' or template's code, so the record could not say which method
  judged a result.
- A hand-set version needs a person to remember it; the images' fingerprints are known only after
  the images are built, and the engine cannot carry its own.

Sources: [Feature 006 research, R1](../../specs/006-method-record/research.md).

# Decision

- The method's parts are declared once, in `src/engine/method/method.yaml`: `include` and
  `exclude` globs over repository paths, and the pins of `src/deploy/images.lock` it runs on.
  The declaration lies inside what it declares.
- The method identity is the SHA-256 of a manifest: each declared file that Git lists as tracked
  or untracked and not ignored, as its SHA-256 and path in path order, then each pin's reference.
- `scripts/method-digest.py` derives it into a gitignored file the engine embeds; `--check` fails
  the test command when it is stale; `src/deploy/build.sh` derives it before every Go image build.
- The engine's code is taken whole, not package by package.

Sources: [Feature 006 research, R1 and R2](../../specs/006-method-record/research.md).

# Consequences

- The same source gives the same identity on every machine; documentation, the terminal client,
  the page and tests do not change it (research E1).
- Any change to the engine's non-test code, the catalogue, the agents, the check and lint images
  or their pins changes it, and the next run of every slice pays its model calls again.
- A new image or module that judges steps must be added to the declaration; until it is, a change
  to it is not seen.

Sources: [Feature 006 research, R1](../../specs/006-method-record/research.md).
