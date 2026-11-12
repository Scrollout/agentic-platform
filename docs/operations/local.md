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

Eight services, declared once in `src/deploy/compose.yaml`, plus the local image registry
`ap-registry`, which `build.sh` and `ap up` start when it isn't running:

- The model gateway, the only container with egress and the only one that can read the provider key.
- The engine, which holds the session's three tokens.
- The read-only run page, and its forwarder, which publishes the page's port on `127.0.0.1` only.
- The terminal client and the test harness, run per command.
- The step agent and check workers, under gVisor with CPU, memory and process limits.

Every service runs non-root, with a read-only root file system, all capabilities dropped and
`no-new-privileges`. No service mounts a container-engine socket. Every network is internal except
the gateway's egress network and the page's publishing network, and every internal network is
`isolated`: its bridge has no address on the host, so nothing on it reaches a host service or the
outside. Docker publishes no port from an internal network, so a forwarder (the pinned nginx with a
fixed configuration and no variables) is the only container on the publishing network, a bridge
with IP masquerading turned off, and passes each request, Host header unchanged, to the page on an
internal network; the page checks the Host header. The forwarder can still reach the host's
addresses and send packets out that get no reply; that limit is the forwarder's, a static pinned
component, and no longer the page's. The gateway's egress network is a plain bridge with NAT:
besides the provider it can reach host services, the local network and the whole internet, and
only its fixed nginx configuration (one upstream, one path) limits what it asks for. Check workers
share a network with the engine only, so they cannot reach the gateway.

The launcher needs Docker 28 or later, the first with `isolated` networks, and refuses an older
one. Networks made by an earlier version of the launcher lack `isolated`; the next `ap up` has
Compose recreate them, stopping and restarting the containers on them. If something Compose did
not start still uses one (a client command in flight), that `up` fails: run `ap down`, then
`ap up`.

Sources: [Docker: port publishing and gateway modes](https://docs.docker.com/engine/network/port-publishing/); [Docker: bridge network driver options](https://docs.docker.com/engine/network/drivers/bridge/); [Feature 001 tasks, T022a](../../specs/001-prose-to-accepted-slice/tasks.md) (tested on Docker 29.9.3, 2026-11-12: a container on a plain internal network reached a host service on the bridge address, one on an `isolated` internal network did not; `isolated` is refused on a non-internal network, and a port published from a network with `inhibit_ipv4` did not answer).

# Deployment

`src/deploy/ap up --credentials <file> --projects <folder>` checks its inputs and creates new
session tokens, or keeps those of a started session, so running it twice changes nothing. It passes the tokens and paths to Compose through environment variables only.
`ap down` stops everything and keeps run records and projects. The launcher's commands are in the
terminal contract, `src/cli/contract.md`. The variables and services are in `src/deploy/README.md`.

Images are built locally with `src/deploy/build.sh <name>`, which pushes each one with an SBOM and
build provenance to a local registry on `127.0.0.1`, signs its digest with a key pair kept in
`~/.config/agentic-platform/signing/`, and records the digest. `ap up` verifies every image's
signature before it starts anything and starts images by digest; Compose never pulls them, and an
image a rebuild dropped from the local store is taken back by digest from the local registry only
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
