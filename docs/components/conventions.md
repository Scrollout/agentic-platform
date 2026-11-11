---
type: Convention
title: Component documentation conventions
description: How services, libraries, and modules are documented in components/.
tags: [conventions, components]
status: draft
sources:
  - id: okf-spec
    resource: /references/okf-spec.md
    title: Open Knowledge Format v0.2 specification
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Scope

One concept per deployable service, library, or module.

# Concepts in this section

- `type: Component`. Use one concept per file, with a kebab-case filename.
- Set `resource` to the component's source path or repository URL.
- Link its dependencies and the [APIs](/apis/conventions.md) it exposes or consumes.
- Every concept also carries `title`, `description`, `tags`, `status` and `generated`, following the [maintenance playbook](/playbooks/maintain-knowledge-bundle.md).

Sources: [Open Knowledge Format v0.2 specification](/references/okf-spec.md).

# Skeleton

```markdown
---
type: Component
title: <Title>
description: <One sentence; it is copied into index.md.>
tags: [components]
status: draft
generated: { by: <actor>, at: <YYYY-MM-DDTHH:MM:SSZ> }
---

# Responsibility

# Interfaces

# Dependencies

# Configuration

# Operations
```
