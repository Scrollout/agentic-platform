---
type: Convention
title: Agent documentation conventions
description: How each agent of the platform is documented in agents/.
tags: [conventions, agents]
status: draft
sources:
  - id: okf-spec
    resource: /references/okf-spec.md
    title: Open Knowledge Format v0.2 specification
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Scope

One concept per agent the platform runs: its role, the model behind it, the tools it may call, and the guardrails around it.

# Concepts in this section

- `type: Agent`. Use one concept per file, with a kebab-case filename.
- Set `resource` to the agent's code or configuration path.
- Link the [APIs](/apis/conventions.md) and [data](/data/conventions.md) it touches.
- Every concept also carries `title`, `description`, `tags`, `status` and `generated`, following the [maintenance playbook](/playbooks/maintain-knowledge-bundle.md).

Sources: [Open Knowledge Format v0.2 specification](/references/okf-spec.md).

# Skeleton

```markdown
---
type: Agent
title: <Title>
description: <One sentence; it is copied into index.md.>
tags: [agents]
status: draft
generated: { by: <actor>, at: <YYYY-MM-DDTHH:MM:SSZ> }
---

# Role

# Model

# Tools

| Tool | Purpose | Side effects |
|------|---------|--------------|

# Prompt

# Guardrails

# Evaluation
```
