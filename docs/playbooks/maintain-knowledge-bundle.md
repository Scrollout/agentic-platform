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
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T15:55:22Z }
---

# Trigger

You learned or decided something durable: a design, a component, an agent, an
interface, a dataset, a procedure, or a decision.

# Steps

1. **Place it.**
   - Pick the section and follow its conventions file, for example
     [components](/components/conventions.md) or [agents](/agents/conventions.md).
   - Filenames are kebab-case. Each path segment must match `[A-Za-z0-9_][A-Za-z0-9_.-]*`.
   - Decisions are numbered `NNNN-slug.md`.
2. **Write the frontmatter.**[^okf-spec]
   - `type` is required.
   - Always set `title`, `description` and `tags`.
   - Set `resource` when the concept describes a concrete asset, such as a repo path, URL or table.
3. **Record authorship.**
   - Set `generated: { by: <actor>, at: <ISO 8601 UTC> }` on every meaningful change.
   - The actor is `claude-code/<model>` for agents, `human:<id>` for people and `process:<id>` for automation.
4. **Record provenance.**
   - List what the concept was derived from in `sources`; each entry needs a `resource`.
   - Attribute specific claims with footnotes `[^id]` that match a `sources[].id`.
5. **Set lifecycle.**
   - New or unreviewed content is `status: draft`.
   - Use `stale_after` for facts that expire.
   - When a concept is replaced, set `status: deprecated` and keep the file so existing links still work.
6. **Link it.**
   - Use bundle-relative links, for example `[router](/components/router.md)`.
   - A link to a concept that does not exist yet is allowed. It marks knowledge that is still to be written.
7. **Regenerate the indexes.** Run `python3 scripts/okf.py index`.
8. **Log it.** Add a bullet to `docs/log.md` under today's `## YYYY-MM-DD` heading, newest date first.

# Verification

- `python3 scripts/okf.py check` reports `OK`.
- A person who confirms a concept adds `verified: { by: human:<id>, at: <ISO 8601 UTC> }`. Agents never add a `human:` entry.

[^okf-spec]: Open Knowledge Format v0.2 specification
