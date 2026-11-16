---
type: Decision
title: "ADR 0050: A failed eval job keeps its outputs for 7 days"
description: When a live eval job fails, evals.yml uploads its report folder (each trial's answer and events, report.json) as a workflow artifact kept 7 days, with actions/upload-artifact pinned by commit; a passing job uploads nothing.
tags: [decisions, ci, evals]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decision, 2026-11-17
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D46
  - id: artifacts
    resource: https://docs.github.com/en/actions/concepts/workflows-and-actions/workflow-artifacts
    title: GitHub Actions, workflow artifacts
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-17T01:00:00Z }
---

# Origin

The owner's decision on 2026-11-17, after an analyst eval case failed in CI but not on the host,
and no failing answer could be read.

Sources: [Feature 001 research, D46](../../specs/001-prose-to-accepted-slice/research.md).

# Context

The live eval jobs kept nothing: a failing trial's answer existed only on the runner, so a
failure that the host could not reproduce could not be diagnosed.

Sources: [Feature 001 research, D46](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- A failed eval job uploads its report folder as one artifact, kept 7 days.
- A passing job uploads nothing.
- The content is the model's answers to the suites' synthetic inputs, the public prompts and token
  use; no key and no user data, so a public artifact is acceptable.
- GitHub serves and stores artifacts encrypted; no tenant or user content can enter, so
  per-tenant erasure does not apply; the owner can delete one early.
- The folder is scanned for the secret patterns of `scripts/check.sh` before the upload, and
  nothing is uploaded when one matches.

# Consequences

- Every eval failure in CI can be read for a week.
- One more action, `actions/upload-artifact`, is pinned and reviewed like the others.

Sources: [GitHub Actions, workflow artifacts](https://docs.github.com/en/actions/concepts/workflows-and-actions/workflow-artifacts).
