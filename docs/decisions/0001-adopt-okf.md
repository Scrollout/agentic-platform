---
type: Decision
title: "ADR 0001: Adopt OKF for project knowledge"
description: All project documentation is kept as an OKF v0.2 knowledge bundle in docs/.
tags: [decisions, documentation]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's approval, 2026-11-11
  - id: okf-spec
    resource: /references/okf-spec.md
    title: Open Knowledge Format v0.2 specification
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Origin

The project owner approved this decision on 2026-11-11.

Sources: [Project owner's approval, 2026-11-11](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).

# Context

Agents and people will both write and read this project's documentation. It has to:

- read without tooling,
- diff cleanly in git,
- show where agent-written content came from, who confirmed it, and whether it is still current.

# Decision

Keep all project knowledge as an Open Knowledge Format v0.2 knowledge bundle rooted at `docs/`.

- Every document is a concept with YAML frontmatter.
- `index.md` files are generated with `python3 scripts/okf.py index`.
- Changes are recorded in `docs/log.md`.

Sources: [Open Knowledge Format v0.2 specification](/references/okf-spec.md).

# Consequences

- Provenance (`sources`), authorship (`generated`), confirmation (`verified`) and lifecycle (`status`, `stale_after`) live in frontmatter, so they can be queried.
- Only people add `human:` entries to `verified`. Agent-written content stays at most machine-confirmed until a person reviews it.
- Any markdown tool can read the knowledge bundle, and so can OKF consumers such as the reference visualizer and Google Cloud Knowledge Catalog.
