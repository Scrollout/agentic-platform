---
type: Decision
title: "ADR 0064: A cached digest spares the reproducible candidate build when nothing changed"
description: Each image's signed build also records the digest of a quick build without the timestamp rewrite, paired with its fingerprint; a start makes the quick build first and the slow reproducible candidate only when that digest differs. Supersedes ADR 0061 in part (how an image is judged current).
tags: [decisions, deployment, images, performance]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's choice, 2026-11-18
  - id: research
    resource: ../../specs/003-one-command-start/research.md
    title: Feature 003 research, R1, S11 and S12
  - id: plan
    resource: ../../specs/003-one-command-start/plan.md
    title: Feature 003 plan, Decisions
  - id: buildkit
    resource: /references/buildkit-reproducible-builds.md
    title: BuildKit reproducible builds reference
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-18T22:00:00Z }
---

# Origin

T023 of feature 003 measured `ap start` on the owner's clean VM (4 CPUs, 7 GB, Docker 29.10): a
second start with nothing changed took 38 to 40 s, against SC-002's 30 s. The project owner chose
the fix recommended from those measurements (2026-11-18), recorded in the
[plan](../../specs/003-one-command-start/plan.md)'s Decisions.

Sources: [Project owner's choice, 2026-11-18](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 003 research, R1, S11 and S12](../../specs/003-one-command-start/research.md).

# Context

- [ADR 0061](/decisions/0061-up-builds-only-changed-images-by-reproducible-digest.md) judges each
  image by its candidate build, exported with `rewrite-timestamp=true`. BuildKit does not cache
  that rewrite: every start rewrites every layer again, 24 s for the check image and 45 to 60 s
  for all seven on the VM, though every build step is a cache hit.
- The same build exported without the rewrite took 0.8 to 0.9 s per image there and gave the same
  digest on every repeat while the build cache held it. It does not survive a pruned cache or a
  step that runs again, so it cannot replace the fingerprint.
- Equal digests mean equal layers, and the rewrite is a function of the layers, so an equal
  quick digest implies an equal fingerprint.

Sources: [Feature 003 research, R1, S11 and S12](../../specs/003-one-command-start/research.md), [BuildKit reproducible builds reference](/references/buildkit-reproducible-builds.md).

# Decision

- A signed build also makes a *cached build*: the candidate's build without `rewrite-timestamp`.
  It records the cached build's digest paired with the image's fingerprint, under the lock that
  records the fingerprint.
- An image with such a pair is current when its cached build has that digest, the pair's
  fingerprint is the recorded one and the recorded image is still in the local store. No
  reproducible candidate is made for it.
- Otherwise the image is judged by its reproducible candidate, as ADR 0061 says. When the
  candidate finds it current, a start (not `--check`) records a new pair, so the next start is
  quick again.
- The fingerprint, the signing and every other part of ADR 0061 and ADR 0063 stand.

# Consequences

- A second start with nothing changed measured 12 to 14 s on the VM, from 38 to 40 s.
- A pruned build cache, a first start after this change, or an edit that runs a step again gives
  one slow start, never an extra rebuild or an unverified image.
- One more record file in the state folder; a forged pair can at most skip a rebuild of an image
  that `ap up` still verifies by digest.

Sources: [Feature 003 plan, Decisions](../../specs/003-one-command-start/plan.md).
