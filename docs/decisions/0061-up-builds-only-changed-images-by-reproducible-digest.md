---
type: Decision
title: "ADR 0061: ap up builds only the images that changed, judged by a reproducible digest"
description: ap up makes a reproducible candidate build of every image it starts and builds, signs and records only those whose candidate digest differs from the fingerprint recorded at their last signed build, asking for the signing password once and only when it must sign.
tags: [decisions, deployment, images, signing]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's request and answer, 2026-11-18
  - id: research
    resource: ../../specs/003-one-command-start/research.md
    title: Feature 003 research, R1 and R2
  - id: plan
    resource: ../../specs/003-one-command-start/plan.md
    title: Feature 003 plan, Decisions
  - id: buildkit
    resource: /references/buildkit-reproducible-builds.md
    title: BuildKit reproducible builds reference
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-18T12:00:00Z }
---

# Origin

The project owner found starting the MVP too complex ("Build and start is complex. Apply
abstraction principles", 2026-11-18): generate code, set a password, build and sign seven images
in turn, make a folder, then `ap up`. Feature 003's spec makes start one command, and its
[plan](../../specs/003-one-command-start/plan.md) records this decision as research R1 and R2. The
owner chose that the password is asked once per start that builds (spec, Clarifications).

Sources: [Project owner's request and answer, 2026-11-18](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 003 research, R1 and R2](../../specs/003-one-command-start/research.md).

# Context

- Constitution XVII: an operation is one command, safe to run again, with no order of steps to
  know; the tool's own mechanism comes first.
- Every Go image copies all of `src/`, so which files changed does not say which images changed.
- A build with `SOURCE_DATE_EPOCH=0` and `rewrite-timestamp=true`, without attestations, gave the
  same digest three times, cold, cached and after an unrelated change, in about a second when
  cached (experiment, 2026-11-18).
- ADR 0028 requires every started image to be signed and verified by digest.

Sources: [Feature 003 research, R1 and R2](../../specs/003-one-command-start/research.md), [BuildKit reproducible builds reference](/references/buildkit-reproducible-builds.md).

# Decision

- `ap up` calls `build.sh --changed` with every image it starts, before it verifies them.
- For each image, build.sh makes a candidate build. Its digest is the image's fingerprint.
- An image whose fingerprint equals the one recorded at its last signed build, and whose
  recorded digest still verifies, is left alone.
- Every other image is built with its SBOM and provenance, pushed, signed and recorded as
  ADR 0028 says, with its fingerprint in `built.env`.
- The password is asked once, without echo, only when something must be signed and
  `COSIGN_PASSWORD` is not set. It is passed through the environment only and never stored.

# Consequences

- A fresh clone starts with one command; a second start with nothing changed builds nothing.
- build.sh stays the one owner of how an image is built; a plain `build.sh <name>` keeps working
  for CI and development.
- A pin used only for the SBOM or signing does not trigger a rebuild by itself, and images built
  before this decision are built once more.
- The launcher's tests that stub Docker must answer candidate builds; that change needs the
  owner's approval.

Sources: [Feature 003 plan, Decisions](../../specs/003-one-command-start/plan.md).
