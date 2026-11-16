---
type: Decision
title: "ADR 0049: A fresh review judges the CI run of the pushed commit"
description: Amends the constitution's Fresh review rule (2.2.0). The fresh reviewer runs the tests that judge the change in its own copy; the full test command's run is the CI run of the pushed commit, and the change is not done until it is green.
tags: [decisions, governance, review, ci]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decisions, 2026-11-16
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D45
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-16T23:45:00Z }
---

# Origin

The owner's decision on 2026-11-16, with [ADR 0048](/decisions/0048-evals-started-by-hand-and-gated-before-main.md).
Amends the Fresh review rule of [ADR 0025](/decisions/0025-engineering-principles.md).

Sources: [Feature 001 research, D45](../../specs/001-prose-to-accepted-slice/research.md).

# Context

The Fresh review rule required the reviewer to run the test command in its own copy. On the
project's small, shared host the full command takes 30 to 50 minutes and repeats what CI runs on
every push, in a fresh checkout, on larger runners ([ADR 0044](/decisions/0044-full-checks-run-on-github-hosted-runners.md)).

Sources: [Feature 001 research, D45](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- The fresh reviewer still works in its own copy, with the working tree's uncommitted and untracked
  changes, made read-only by tool permissions, and runs there the tests that judge the change.
- The full test command's run is the CI run of the pushed commit; the change is not done until it
  is green.

# Consequences

- Reviews take minutes instead of most of an hour, and the host runs no full suite.
- A defect only the full suite finds is found after the push and fixed by the next commit.
