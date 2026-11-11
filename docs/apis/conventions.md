---
type: Convention
title: API documentation conventions
description: How interfaces the platform exposes or consumes are documented in apis/.
tags: [conventions, apis]
status: draft
sources:
  - id: okf-spec
    resource: /references/okf-spec.md
    title: Open Knowledge Format v0.2 specification
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Scope

One concept per API, internal or external. OKF references the machine-readable schema (OpenAPI, Protobuf); it does not replace it.

# Concepts in this section

- `type: API`. Use one concept per file, with a kebab-case filename.
- Set `resource` to the schema file or the provider's documentation URL.
- Every concept also carries `title`, `description`, `tags`, `status` and `generated`, following the [maintenance playbook](/playbooks/maintain-knowledge-bundle.md).

Sources: [Open Knowledge Format v0.2 specification](/references/okf-spec.md).

# Skeleton

```markdown
---
type: API
title: <Title>
description: <One sentence; it is copied into index.md.>
tags: [apis]
status: draft
generated: { by: <actor>, at: <YYYY-MM-DDTHH:MM:SSZ> }
---

# Purpose

# Endpoints

| Method | Path | Description |
|--------|------|-------------|

# Authentication

# Examples
```
