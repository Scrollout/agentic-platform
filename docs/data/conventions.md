---
type: Convention
title: Data documentation conventions
description: How datasets, stores, and schemas are documented in data/.
tags: [conventions, data]
status: draft
sources:
  - id: okf-spec
    resource: /references/okf-spec.md
    title: Open Knowledge Format v0.2 specification
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T15:55:22Z }
---

# Scope

One concept per dataset, table, index, or store. A number that must be computed the sanctioned way becomes an `Attested Computation` concept (OKF §10), linked from the dataset or metric that uses it.

# Concepts in this section

- `type: Dataset`. Use one concept per file, with a kebab-case filename.
- Set `resource` to the canonical URI of the store or table.
- Every concept also carries `title`, `description`, `tags`, `status` and `generated`, following the [maintenance playbook](/playbooks/maintain-knowledge-bundle.md).[^okf-spec]

# Skeleton

```markdown
---
type: Dataset
title: <Title>
description: <One sentence; it is copied into index.md.>
tags: [data]
status: draft
generated: { by: <actor>, at: <YYYY-MM-DDTHH:MM:SSZ> }
---

# Schema

| Field | Type | Description |
|-------|------|-------------|

# Lineage

# Retention and access
```

[^okf-spec]: Open Knowledge Format v0.2 specification
