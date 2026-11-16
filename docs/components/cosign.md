---
type: Component
title: cosign
description: Sigstore's signing tool; signs every image the platform builds with a local key and verifies the signature before the launcher starts it.
tags: [components, supply-chain, signing, deployment]
status: draft
resource: https://github.com/sigstore/cosign
sources:
  - id: cosign-sign
    resource: https://github.com/sigstore/cosign/blob/main/doc/cosign_sign.md
    title: cosign sign reference
  - id: cosign-verify
    resource: https://github.com/sigstore/cosign/blob/main/doc/cosign_verify.md
    title: cosign verify reference
  - id: adr-0028
    resource: /decisions/0028-sign-platform-images-locally.md
    title: "ADR 0028: Sign the platform's images locally with cosign"
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D24 and sources S15, S16
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T10:00:00Z }
---

# Responsibility

Signs each image digest the platform builds and checks that signature before the image starts, as
[ADR 0028](/decisions/0028-sign-platform-images-locally.md) decides. It is the Sigstore project's
command-line tool, licensed Apache-2.0, adopted as-is (Principle VI).

Sources: [ADR 0028](/decisions/0028-sign-platform-images-locally.md); [cosign sign reference](https://github.com/sigstore/cosign/blob/main/doc/cosign_sign.md).

# Interfaces

- `generate-key-pair`: makes `cosign.key` (encrypted with a password) and `cosign.pub`.
- `signing-config create --out <file>` with no service flags: a signing configuration that names
  no transparency log, certificate authority or timestamp authority. cosign v3 rejects
  `--tlog-upload=false`; this configuration replaces it.
- `sign --yes --key <key> --signing-config <file> --allow-http-registry <repo>@<digest>`: pushes
  the signature for that digest to the same registry.
- `verify --key <pub> --insecure-ignore-tlog --allow-http-registry <repo>@<digest>`: exit 0 only
  when a signature made with the matching private key exists for that digest. Without
  `--insecure-ignore-tlog` it tries to reach Sigstore's public TUF server.

Sources: [cosign verify reference](https://github.com/sigstore/cosign/blob/main/doc/cosign_verify.md); [Feature 001 research, D24](../../specs/001-prose-to-accepted-slice/research.md).

# Dependencies

- The [local image registry](/components/distribution-registry.md), which stores images and their
  signatures. cosign reaches it as `ap-registry:5000` on the internal network `ap-signing`.

Sources: [Feature 001 research, D24](../../specs/001-prose-to-accepted-slice/research.md).

# Configuration

- Image: the `cosign` pin in `src/deploy/images.lock` (v3.1.4, by digest). It never runs on the host.
- Signing material: `${XDG_CONFIG_HOME:-~/.config}/agentic-platform/signing/` (mode 700) with
  `cosign.key` (mode 600, encrypted), `cosign.pub` and `signing-config.json`, made on first use by
  `src/deploy/build.sh`. The password comes from `COSIGN_PASSWORD` or a terminal prompt.
- Every cosign container runs as the developer's user, with a read-only root, no capabilities and
  `no-new-privileges`. Key generation has no network; signing and verification join `ap-signing`
  only. The private key is mounted read-only into the signing container alone; the launcher mounts
  only `cosign.pub`.

Sources: [ADR 0028](/decisions/0028-sign-platform-images-locally.md).

# Operations

- `src/deploy/build.sh <name>` signs each image it pushes; `src/deploy/ap up` verifies each image
  it passes to Compose and starts nothing if one fails.
- Limit: the key is on the machine that builds and runs the images, so a signature proves that
  `ap up` starts exactly what `build.sh` built, not who built it. Losing the key means rebuilding
  every image with a new pair.

Sources: [ADR 0028](/decisions/0028-sign-platform-images-locally.md); [Feature 001 research, D24](../../specs/001-prose-to-accepted-slice/research.md).
