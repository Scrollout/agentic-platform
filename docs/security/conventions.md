---
type: Convention
title: Security documentation conventions
description: How the threat model, access control, secrets, and compliance are documented in security/.
tags: [conventions, security]
status: draft
sources:
  - id: okf-spec
    resource: /references/okf-spec.md
    title: Open Knowledge Format v0.2 specification
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T15:55:22Z }
---

# Scope

One concept per policy (`type: Policy`) and one for the threat model (`type: Threat Model`). For an agentic platform this covers tool permissions, prompt-injection handling, data access by agents, and secret handling.

# Concepts in this section

- `type: Policy`. Use one concept per file, with a kebab-case filename.
- Policies that people must sign off on stay `draft` until they are `verified` by a `human:` actor.
- Every concept also carries `title`, `description`, `tags`, `status` and `generated`, following the [maintenance playbook](/playbooks/maintain-knowledge-bundle.md).[^okf-spec]

# Skeleton

```markdown
---
type: Policy
title: <Title>
description: <One sentence; it is copied into index.md.>
tags: [security]
status: draft
generated: { by: <actor>, at: <YYYY-MM-DDTHH:MM:SSZ> }
---

# Statement

# Applies to

# Controls

# Exceptions
```

[^okf-spec]: Open Knowledge Format v0.2 specification
