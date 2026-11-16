---
type: Component
title: Distribution registry
description: The CNCF Distribution image registry, run on 127.0.0.1 to hold the platform's built images, their SBOM and provenance attestations, and their signatures.
tags: [components, supply-chain, registry, deployment]
status: draft
resource: https://github.com/distribution/distribution
sources:
  - id: deploying
    resource: https://distribution.github.io/distribution/about/deploying/
    title: "CNCF Distribution: deploying a registry"
  - id: adr-0028
    resource: /decisions/0028-sign-platform-images-locally.md
    title: "ADR 0028: Sign the platform's images locally with cosign"
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D24 and sources S15, S16
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T10:00:00Z }
---

# Responsibility

Holds each image `src/deploy/build.sh` builds, with its attestations and its
[cosign](/components/cosign.md) signature, so the launcher can verify the image by digest before
start ([ADR 0028](/decisions/0028-sign-platform-images-locally.md)). It is the CNCF Distribution
project's official `registry` image, licensed Apache-2.0, adopted as-is (Principle VI).

Sources: [ADR 0028](/decisions/0028-sign-platform-images-locally.md); [CNCF Distribution: deploying a registry](https://distribution.github.io/distribution/about/deploying/).

# Interfaces

- The OCI distribution HTTP API, without TLS or authentication, on two addresses:
  `127.0.0.1:${AP_REGISTRY_PORT:-5005}` for the container engine's pushes and pulls, and
  `ap-registry:5000` on the internal network `ap-signing` for cosign. A signature made through one address verifies
  through the other.

Sources: [Feature 001 research, D24](../../specs/001-prose-to-accepted-slice/research.md).

# Dependencies

None beyond the container engine. Data lives in the volume `ap-registry-data`.

# Configuration

- Image: the `registry` pin in `src/deploy/images.lock` (`registry:3`, by digest).
- The launcher owns how it runs (`src/deploy/ap registry`; `build.sh` and `ap up` call it): container
  `ap-registry`, user 65532, read-only root with a `tmpfs` `/tmp`, all capabilities dropped,
  `no-new-privileges`, memory, CPU and process limits, log level `info`. A new data volume is handed
  to user 65532 once by a container with `CHOWN` as its only capability and no network.
- A container created with another pin, port or run flags is replaced (a label holds the pin, the
  port and a checksum of the flags); the volume is kept.
- `AP_REGISTRY_NAME` runs a second, independent registry, for example in tests: container
  `<name>`, volume `<name>-data`, internal network `<name>-net`, publishing network `<name>-host`.

Sources: [ADR 0028](/decisions/0028-sign-platform-images-locally.md).

# Operations

- Started when needed and left running; `docker stop ap-registry` stops it, and the next build or
  `ap up` starts it again.
- Its port is published from its own network `ap-registry-host`, a bridge with IP masquerading off
  and no other container, so it has no working route out; cosign reaches it on `ap-signing`, an
  internal network in the `isolated` gateway mode, whose bridge has no host address. A network
  made with other options (by an earlier launcher) is replaced, with the container; the volume is
  kept.
- An `ap up` whose recorded digest is no longer in the local image store pulls it back from here,
  by digest, on `127.0.0.1`.
- Limits: it has no authentication, so any local process can push to it; the launcher's signature
  check is what refuses an image pushed that way. Docker publishes no port from an internal
  network, so its publishing network keeps an address on the host: the registry can reach host
  services there. Its debug and metrics listener (`:5001`) is reachable from `ap-signing`, not from
  the host.

Sources: [Feature 001 research, D24](../../specs/001-prose-to-accepted-slice/research.md).
