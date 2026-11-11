---
type: Convention
title: Operations documentation conventions
description: How environments, deployment, observability, and SLOs are documented in operations/.
tags: [conventions, operations]
status: draft
sources:
  - id: okf-spec
    resource: /references/okf-spec.md
    title: Open Knowledge Format v0.2 specification
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T15:55:22Z }
---

# Scope

One concept per environment, such as local, staging or production. Other concepts cover the deployment pipeline (`type: Pipeline`) and service levels (`type: SLO`). Step-by-step procedures belong in [playbooks](/playbooks/maintain-knowledge-bundle.md).

# Concepts in this section

- `type: Environment`. Use one concept per file, with a kebab-case filename.
- Set `resource` to the environment's console or infrastructure-as-code path.
- Set `stale_after` on facts that drift, such as versions and capacity.
- Every concept also carries `title`, `description`, `tags`, `status` and `generated`, following the [maintenance playbook](/playbooks/maintain-knowledge-bundle.md).[^okf-spec]

# Skeleton

```markdown
---
type: Environment
title: <Title>
description: <One sentence; it is copied into index.md.>
tags: [operations]
status: draft
generated: { by: <actor>, at: <YYYY-MM-DDTHH:MM:SSZ> }
---

# Purpose

# Topology

# Deployment

# Observability

# Access
```

[^okf-spec]: Open Knowledge Format v0.2 specification
