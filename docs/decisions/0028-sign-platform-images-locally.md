---
type: Decision
title: "ADR 0028: Sign the platform's images locally with cosign"
description: Images the platform builds are pushed to a local registry with an SBOM and provenance, signed with cosign and a key pair kept outside the repository, and verified offline by the launcher before anything starts.
tags: [decisions, supply-chain, signing, deployment]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's acceptance of the signing recommendation, 2026-11-12
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D24 (recommendation, experiment and sources S15, S16)
  - id: cosign-sign
    resource: https://github.com/sigstore/cosign/blob/main/doc/cosign_sign.md
    title: cosign sign reference
  - id: cosign-verify
    resource: https://github.com/sigstore/cosign/blob/main/doc/cosign_verify.md
    title: cosign verify reference
  - id: dct
    resource: https://www.docker.com/blog/docker-content-trust-retirement-and-migration-guidance/
    title: Docker Content Trust retirement and migration guidance
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T09:00:00Z }
---

# Origin

Feature 001's plan needs the launcher to verify image signatures at start (Principle VIII). The
images are built on the developer's machine, and cosign signs images in a registry. The
recommendation in the feature's research (D24) was tested on 2026-11-12, and the project owner
accepted it the same day.

Sources: [Project owner's acceptance of the signing recommendation, 2026-11-12](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np); [Feature 001 research, D24](../../specs/001-prose-to-accepted-slice/research.md).

# Context

- Principle VIII requires every image we publish to be signed, carry an SBOM and build
  provenance, and be verified at deploy time. Principle XIII requires agent artifacts to have their
  signature or provenance verified before use.
- There is no hosted forge or CI yet, so images are built locally.
- Docker Content Trust is retired. Digest pinning proves content integrity but not publisher
  identity.
- The pinned Docker Agent and nginx images carry no cosign signatures or attestations.

Sources: [Docker Content Trust retirement and migration guidance](https://www.docker.com/blog/docker-content-trust-retirement-and-migration-guidance/); [Feature 001 research, D24](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- Each image the platform builds is pushed, with an SBOM and build provenance, to a local registry
  (the CNCF Distribution image) published on 127.0.0.1 only.
- Each pushed digest is signed with cosign, run from its pinned image, with a key pair made once
  outside the repository; the private key is encrypted and readable only by the developer.
- Signing uses a signing configuration with no transparency log, so no image name or digest
  leaves the machine. Verification therefore skips the transparency log and needs no network
  beyond the local registry.
- The launcher verifies every image digest with the public key before it starts anything, and
  starts images by digest. Any verification failure stops the start.
- Upstream images stay pinned by version and digest, which is all their publishers provide.

Sources: [cosign sign reference](https://github.com/sigstore/cosign/blob/main/doc/cosign_sign.md); [cosign verify reference](https://github.com/sigstore/cosign/blob/main/doc/cosign_verify.md).

# Consequences

- The launcher starts exactly the images the build produced; another image under the same name,
  or one signed with a different key, is refused.
- Signing on the same machine does not prove who built an image: a process running as the
  developer could read the key. Publisher identity needs hosted CI with keyless signing, which
  supersedes this record when it comes.
- The registry, cosign and the SBOM generator become pinned dependencies, and a registry container
  runs while images are built and started.

Sources: [Feature 001 research, D24](../../specs/001-prose-to-accepted-slice/research.md).
