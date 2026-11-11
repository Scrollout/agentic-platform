---
type: Convention
title: Guide documentation conventions
description: How developer how-tos are documented in guides/.
tags: [conventions, guides]
status: draft
sources:
  - id: okf-spec
    resource: /references/okf-spec.md
    title: Open Knowledge Format v0.2 specification
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Scope

One concept per task a contributor performs, such as setting up a development environment, running tests or adding an agent.

# Concepts in this section

- `type: Guide`. Use one concept per file, with a kebab-case filename.
- Link the [components](/components/conventions.md) or [agents](/agents/conventions.md) a guide touches.
- Every concept also carries `title`, `description`, `tags`, `status` and `generated`, following the [maintenance playbook](/playbooks/maintain-knowledge-bundle.md).

Sources: [Open Knowledge Format v0.2 specification](/references/okf-spec.md).

# Skeleton

```markdown
---
type: Guide
title: <Title>
description: <One sentence; it is copied into index.md.>
tags: [guides]
status: draft
generated: { by: <actor>, at: <YYYY-MM-DDTHH:MM:SSZ> }
---

# Goal

# Prerequisites

# Steps

1.

# Verification
```
