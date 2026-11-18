---
type: Reference
title: BuildKit reproducible builds
description: How BuildKit makes an image digest reproducible with SOURCE_DATE_EPOCH and rewrite-timestamp, and what the platform measured on its own images on 2026-11-18.
resource: https://github.com/moby/buildkit/blob/master/docs/build-repro.md
tags: [references, docker, buildkit, images]
status: draft
stale_after: 2027-05-18T00:00:00Z
sources:
  - id: build-repro
    resource: https://github.com/moby/buildkit/blob/master/docs/build-repro.md
    title: "BuildKit: build reproducibility"
  - id: docker-repro
    resource: https://docs.docker.com/build/ci/github-actions/reproducible-builds/
    title: "Docker: reproducible builds"
  - id: research
    resource: ../../specs/003-one-command-start/research.md
    title: Feature 003 research, S2 and R1
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-18T12:00:00Z }
---

# Mechanism

- The build argument `SOURCE_DATE_EPOCH` sets the time BuildKit writes into the image
  configuration and its history.
- The image exporter's `rewrite-timestamp=true` also rewrites the file times in the layers to it,
  so files copied from a build stage no longer carry the time they were built.
- With the containerd image store, Docker's image exporter refuses `rewrite-timestamp=true` while
  it unpacks the image (`exporter option "rewrite-timestamp" conflicts with "unpack"`, seen on a
  GitHub-hosted runner on 2026-11-18); `unpack=false` stores the image without unpacking it and
  gives the same digest.
- SBOM and provenance attestations are separate manifests with their own times; a build without
  them (`--sbom=false --provenance=false`) reports the image's own digest.

Sources: ["BuildKit: build reproducibility"](https://github.com/moby/buildkit/blob/master/docs/build-repro.md), ["Docker: reproducible builds"](https://docs.docker.com/build/ci/github-actions/reproducible-builds/), [feature 003 research, S10](../../specs/003-one-command-start/research.md).

# Measured on the platform

- Docker 29.10.0, BuildKit v0.35.0, containerd image store, 2026-11-18.
- `Dockerfile.cli` was built with `SOURCE_DATE_EPOCH=0`, `rewrite-timestamp=true` and no
  attestations. It gave the same `containerimage.digest` three times: cold (5.7 s), unchanged
  (0.8 s), and after an unrelated file was added to `src/` (1.4 s).
- Go builds with `-trimpath`, `-buildvcs=false` and `-buildid=` give the same binary from the
  same sources, so the image changes only when the program does.

Sources: [Feature 003 research, S2 and R1](../../specs/003-one-command-start/research.md).
