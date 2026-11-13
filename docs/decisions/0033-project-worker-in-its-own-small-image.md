---
type: Decision
title: "ADR 0033: The project worker runs in its own small image"
description: The project worker runs the static check binary in its own image, ap-project, on the Alpine pin with git pinned by exact package version; the check image keeps its Alpine base without git.
tags: [decisions, deploy, projects, git, images]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decision on the project worker's image, 2026-11-13
  - id: adr-0032
    resource: /decisions/0032-project-version-history-kept-by-a-project-worker.md
    title: "ADR 0032: The new project's version history is a git repository kept by a project worker"
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D30
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions and Complexity Tracking
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T23:45:00Z }
---

# Origin

[ADR 0032](/decisions/0032-project-version-history-kept-by-a-project-worker.md) put git in the
check image by building it on the Debian golang pin. Built in T053c, the check image grew from
365 MB to 1.29 GB. The host is short on resources, so the project owner changed the decision on
2026-11-13. The feature's research records it in D30, and the plan lists it in its Decisions
table.

Sources: [Project owner's decision, 2026-11-13](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np); [Feature 001 research, D30](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 plan](../../specs/001-prose-to-accepted-slice/plan.md).

# Context

- The project worker needs the git CLI. It runs no check and no procedure, so it needs neither
  the Go toolchain nor the module template.
- The Alpine golang pin the check image is built on has no git.
- Alpine packages have no digest, so `apk add` can pin a package only by exact version.

Sources: [Feature 001 research, D30](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- The project worker runs the static check binary in its own image, `ap-project`
  (`src/deploy/Dockerfile.project`, `AP_IMAGE_PROJECT`), built, signed and verified like the
  other images.
- Its base is the `alpine` pin by digest. git is added with `apk add git=2.49.1-r0`, pinned by
  exact package version in `images.lock` (`alpine-git`). The plan's Complexity Tracking table records this
  version-only pin as an owner-approved exception to Principle XIII.
- The image is about 45 MB. The check image keeps its Alpine base, without git.
- Rejected: the check image on the Debian golang pin (1.29 GB, too large for the host).

Sources: [Feature 001 plan, Complexity Tracking](../../specs/001-prose-to-accepted-slice/plan.md).

# Consequences

- The check image is unchanged at 365 MB. The project worker's image is small.
- A new git package release in Alpine makes the image's build fail until the pin moves.
- The project worker's tests pass with git 2.49.1, as with the host's git.
- This supersedes ADR 0032 in part: only where git comes from. The rest of ADR 0032 stands.

Sources: [ADR 0032](/decisions/0032-project-version-history-kept-by-a-project-worker.md).
