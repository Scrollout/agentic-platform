---
type: Reference
title: Open Knowledge Format v0.2 specification
description: The format this bundle follows, pinned to the upstream commit it was written against.
resource: https://github.com/GoogleCloudPlatform/open-knowledge-format/blob/ad30107c31c06aec8a7d5636e0d1058118604e6f/SPEC.md
tags: [references, okf]
status: stable
sources:
  - id: okf-spec
    resource: https://github.com/GoogleCloudPlatform/open-knowledge-format/blob/ad30107c31c06aec8a7d5636e0d1058118604e6f/SPEC.md
    title: Open Knowledge Format (OKF) v0.2
    last_modified: 2026-08-21T20:08:36Z
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T15:55:22Z }
---

# Summary

OKF represents knowledge as a directory tree of markdown files with YAML frontmatter.[^okf-spec]

- **Conformance (§11):**
  - Every non-reserved `.md` file has parseable frontmatter with a non-empty `type`.
  - `index.md` (§8) and `log.md` (§9) follow their structure.
- **Frontmatter (§4.1):** `type` is required. `title`, `description`, `resource` and `tags` are recommended. Extra keys are allowed.
- **Provenance (§5.1):**
  - `sources[]` entries take `resource` plus optional `id`, `title`, `author`, `usage_count` and `last_modified`.
  - Claims are attributed with footnotes keyed by `id`.
- **Trust (§5.2, §5.3):**
  - `generated: { by, at }` records who wrote the content.
  - `verified` records who confirmed it.
  - A `human:` verifier makes a concept human-reviewed.
- **Lifecycle (§5.4, §5.5):** `status` is `draft`, `stable` or `deprecated` (absent means `stable`). `stale_after` is an absolute instant.
- **Links (§6):** use standard markdown links, preferably bundle-relative (`/path.md`). Broken links are allowed.
- **Actors (§7):** `<producer>/<version>`, `human:<id>` or `process:<id>`.
- **Attested Computation (§10):** a concept type with `runtime`, `parameters`, `executor` and `attester` for values that must be computed the sanctioned way.
- **Timestamps:** ISO 8601 with an explicit UTC offset.

# Upstream status

The upstream repository now carries a notice (branch `deprecate-repo`, 2026-11-06)
that development moved to `open-knowledge-format/open-knowledge-format` with spec v0.3.
That repository was not publicly reachable on 2026-11-11, so this bundle targets v0.2.

[^okf-spec]: Open Knowledge Format (OKF) v0.2
