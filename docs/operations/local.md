---
type: Environment
title: Local environment
description: The platform on one developer's machine, started by the ap launcher with Docker Compose and gVisor.
tags: [operations, deployment, local]
status: draft
resource: /src/deploy/compose.yaml
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T10:00:00Z }
---

# Purpose

The only environment of feature 001: one developer, one machine, one active run. Nothing is
installed on the host beyond a container runtime with Compose and the gVisor runtime (`runsc`; see [Install the gVisor runtime for Docker](/guides/install-gvisor.md)).

# Topology

Seven services, declared once in `src/deploy/compose.yaml`, plus the local image registry
`ap-registry`, which `build.sh` and `ap up` start when it isn't running:

- The model gateway, the only container with egress and the only one that can read the provider key.
- The engine, which holds the session's three tokens.
- The read-only run page, published on `127.0.0.1` only.
- The terminal client and the test harness, run per command.
- The step agent and check workers, under gVisor with CPU, memory and process limits.

Every service runs non-root, with a read-only root file system, all capabilities dropped and
`no-new-privileges`. No service mounts a container-engine socket. Every network is internal except
the gateway's egress network and the page's publishing network. Docker publishes no port from an
internal network, so the page's network is a bridge with IP masquerading turned off. That removes
NAT, not isolation: a compromised page can still reach services on the host's bridge address and
send packets out with its private source address (so replies cannot return). This is a known limit
until a host firewall rule or an isolating gateway mode closes it (task T022a). Check workers share a network with the engine only, so they cannot reach the
gateway.

# Deployment

`src/deploy/ap up --credentials <file> --projects <folder>` checks its inputs and creates new
session tokens, or keeps those of a started session, so running it twice changes nothing. It passes the tokens and paths to Compose through environment variables only.
`ap down` stops everything and keeps run records and projects. The launcher's commands are in the
terminal contract, `src/cli/contract.md`. The variables and services are in `src/deploy/README.md`.

Images are built locally with `src/deploy/build.sh <name>`, which pushes each one with an SBOM and
build provenance to a local registry on `127.0.0.1`, signs its digest with a key pair kept in
`~/.config/agentic-platform/signing/`, and records the digest. `ap up` verifies every image's
signature before it starts anything and starts images by digest, never pulling them
([ADR 0028](/decisions/0028-sign-platform-images-locally.md); [cosign](/components/cosign.md);
[Distribution registry](/components/distribution-registry.md)). The signature proves that `up`
starts exactly what `build.sh` built, not who built it, because the key is on the same machine.
Upstream images (nginx, Docker Agent, Go) are pinned by digest only; their publishers sign none.

# Observability

`docker compose logs`. The gateway logs nginx errors only, never headers or bodies.

# Access

The developer reaches the platform through the launcher and the run page on `127.0.0.1`. The
provider key file holds only the key, owned by the developer with mode 600. The gateway runs as
the developer's user so it can read the file, because Compose cannot change a file secret's owner
outside Swarm.
