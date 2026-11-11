---
type: Glossary
title: Glossary
description: Shared vocabulary for the platform and its knowledge bundle.
tags: [overview, glossary]
status: draft
sources:
  - id: okf-spec
    resource: /references/okf-spec.md
    title: Open Knowledge Format v0.2 specification
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T15:55:22Z }
---

# Knowledge bundle terms

These terms come from the OKF spec.[^okf-spec]

| Term | Meaning |
|------|---------|
| Bundle | This `docs/` directory: a self-contained tree of OKF concepts. |
| Concept | One markdown file with YAML frontmatter. Its id is its path without `.md`. |
| Actor | Who or what performed an action: `<producer>/<version>`, `human:<id>` or `process:<id>`. |
| Trust tier | Derived from `verified`: unverified, machine-confirmed, or human-reviewed. |
| Attested Computation | A concept carrying a sanctioned, checkable way to compute a value. |

# Platform terms

None defined yet. Add each term here when it first appears in another concept.

[^okf-spec]: Open Knowledge Format v0.2 specification
