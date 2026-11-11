---
type: Architecture
title: Architecture overview
description: System context, building blocks, and cross-cutting design of the platform.
tags: [architecture]
status: draft
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T15:55:22Z }
---

# Context

Not yet designed. The users, external systems, and platform boundary come
from the [platform purpose](/overview/platform.md).

# Building blocks

Each building block gets its own concept and is linked from here:

- Agents go in [agents/](/agents/conventions.md).
- Components go in [components/](/components/conventions.md).
- APIs go in [apis/](/apis/conventions.md).
- Data stores go in [data/](/data/conventions.md).

# Cross-cutting concerns

- Security: [security conventions](/security/conventions.md).
- Operations: [operations conventions](/operations/conventions.md).

# Decisions

- [ADR 0001: Adopt OKF for project knowledge](/decisions/0001-adopt-okf.md)
