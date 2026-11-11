---
type: Playbook
title: Maintain the knowledge bundle
description: How people and agents add, change, verify, and retire concepts in docs/.
tags: [playbooks, documentation]
status: draft
sources:
  - id: okf-spec
    resource: /references/okf-spec.md
    title: Open Knowledge Format v0.2 specification
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Trigger

You learned or decided something durable: a design, a component, an agent, an
interface, a dataset, a procedure, or a decision.

# Rules

- Every `.md` file in `docs/` other than `index.md` and `log.md` is a concept and needs YAML frontmatter with a non-empty `type`.
- Agents never write a `human:` entry in `verified`. Only a person confirms content.
- New content is `status: draft`. Deprecate a concept instead of deleting it.
- Never hand-edit an `index.md`; regenerate it. The only exception is a section description in `docs/index.md`.
- Before finishing a change, `python3 scripts/okf.py check` must report `OK`.

# Steps

1. **Place it.**
   - Pick the section and follow its conventions file, for example
     [components](/components/conventions.md) or [agents](/agents/conventions.md).
   - Filenames are kebab-case. Each path segment must match `[A-Za-z0-9_][A-Za-z0-9_.-]*`.
   - Decisions are numbered `NNNN-slug.md`.
2. **Write the frontmatter.**
   - `type` is required.
   - Always set `title`, `description`, `tags` and `status`.
   - Set `resource` when the concept describes a concrete asset, such as a repo path, URL or table.
3. **Record authorship.**
   - Set `generated: { by: <actor>, at: <ISO 8601 UTC> }` on every meaningful change.
   - The actor is `claude-code/<model>` for agents, `human:<id>` for people and `process:<id>` for automation.
4. **Record provenance.**
   - List what the concept was derived from in `sources`; each entry needs a `resource`.
   - Attribute claims with one `Sources:` line at the end of each section, linking the `sources` entries that section relies on.
   - Do not use inline footnote markers (`[^id]`). OKF allows them for per-claim attribution, but they make the text hard to read, so this knowledge bundle attributes by section.
5. **Set lifecycle.**
   - New or unreviewed content is `status: draft`.
   - Use `stale_after` for facts that expire.
   - When a concept is replaced, set `status: deprecated` and keep the file so existing links still work.
6. **Link it.**
   - Use links relative to the knowledge bundle root (`docs/`), for example `[router](/components/router.md)`.
   - A link to a concept that does not exist yet is allowed. It marks knowledge that is still to be written.
7. **Regenerate the indexes.** Run `python3 scripts/okf.py index`. It needs PyYAML.
8. **Log it.** Add a bullet to `docs/log.md` under today's `## YYYY-MM-DD` heading, newest date first.

Sources: [Open Knowledge Format v0.2 specification](/references/okf-spec.md).

# Verification

- `python3 scripts/okf.py check` reports `OK`. It needs PyYAML.
- A person who confirms a concept adds `verified: { by: human:<id>, at: <ISO 8601 UTC> }`. Agents never add a `human:` entry.
