---
type: Environment
title: Local environment
description: The platform on one developer's machine, started by the ap launcher with Docker Compose and gVisor.
tags: [operations, deployment, local]
status: draft
resource: /src/deploy/compose.yaml
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T23:00:00Z }
---

# Purpose

The only environment of feature 001: one developer, one machine, one active run. Nothing is
installed on the host beyond a container runtime with Compose and the gVisor runtime (`runsc`; see [Install the gVisor runtime for Docker](/guides/install-gvisor.md)).

# Topology

Ten services, declared once in `src/deploy/compose.yaml`, plus the local image registry
`ap-registry`, which `build.sh` and `ap up` start when it isn't running:

- The model gateway, the only container with egress and the only one that can read the provider key.
- The engine, which holds the session's three caller tokens and the tokens it calls the two
  services below, and the check service, with.
- The contract-lint service (Spectral) and the analyst (Docker Agent's `serve api`), under gVisor,
  each on its own internal network with the engine; the analyst's network also has the gateway, its
  only way out.
- The read-only run page, and its forwarder, which publishes the page's port on `127.0.0.1` only.
- The terminal client and the test harness, run per command.
- The step agent workers and the check service (the project folder read-only, the Go toolchain, the pinned oapi-codegen and the template's module cache in its image, so it runs offline), under gVisor with CPU, memory and process limits; the check service answers only the engine, with its own session token, and `ap up` starts it with the engine. The step workers are started per slice and project by `ap build-workers <slice> <project>` (run by `ap build` first): one container per step of the pattern from the `step-agent` or `procedure-worker` template, each mounting only its project, read-only except its step's writable folders (the whole root for generate) and with `.git` always read-only; each gets its own call token, its grant is a file in `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/grants` (owned by the engine's user, mounted only by the engine, read-only; research D31), and the launcher registers it with the engine. Each project has its own step containers, named `agentic-platform-worker-<slice>-<step>-<p>` (`<p>` the first 12 hex digits of the SHA-256 of the project name) and labelled with the slice, step and project, so building another project of the slice leaves the first one's running; `build-workers` refuses with exit 4 (`RunActive`) while a run of the slice has not ended. `ap resume <run>` registers the run's own running containers again and passes the command on; when they or the slice's tokens are gone it exits 3 naming `ap build-workers --resume <run>`. `ap replay <run> <project>` builds the run's slice into a new, empty project. Running `build-workers` again changes nothing, and `ap down` removes the workers, their grants and their tokens ([ADR 0036](/decisions/0036-step-containers-per-project-resume-reuses-them.md)).
- The project worker, the check program's third deployment, in its own small image with git (git pinned by exact Alpine package version), under gVisor and the only service with the projects folder writable: it keeps each new project's git version history and restores the project before each attempt, runs as the developer so they own the files and the repository, keeps its records on the volume `project-worker-state`, answers only the engine with its own session token, and `ap up` starts it with the check service.

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
only its fixed nginx configuration (one upstream, one path) limits what it asks for. The image
registry's publishing network has the same exposure. These three are the only containers that can
reach the host's services; the owner may close that with the optional host firewall rule under
Access. The check service shares a network with the engine only, so it cannot reach the gateway.

The step workers and the check service run under gVisor, which cannot use Docker's embedded DNS server: its own
network stack never sees the rule that carries `127.0.0.11` to Docker's resolver. The runtime flag
that would copy that rule (`--reproduce-nat`) belongs to the host's Docker configuration, so
instead the engine and the gateway have fixed addresses on the networks they share with the
workers, and the workers find them by name through their hosts file. All the platform's fixed
subnets lie in `10.231.0.0/16`, outside Docker's default address pools; they and the addresses are
declared once in `compose.yaml` (the registry's publishing network, `10.231.5.0/24`, in `ap`). A
subnet that overlaps another network on the machine stops `ap up` with Compose's `Pool overlaps`
message, and only one platform can run on a machine at a time.

The launcher needs Docker 28 or later, the first with `isolated` networks, and refuses an older
one. Networks made by an earlier version of the launcher lack `isolated`; the next `ap up` has
Compose recreate them, stopping and restarting the containers on them. If something Compose did
not start still uses one (a client command in flight), that `up` fails: run `ap down`, then
`ap up`.

Sources: [Docker: port publishing and gateway modes](https://docs.docker.com/engine/network/port-publishing/); [Docker: bridge network driver options](https://docs.docker.com/engine/network/drivers/bridge/); [Compose: extra_hosts and ipv4_address](https://docs.docker.com/reference/compose-file/services/); [Feature 001 tasks, T022a and T022c](../../specs/001-prose-to-accepted-slice/tasks.md) (tested on Docker 29.9.3, 2026-11-12: a container on a plain internal network reached a host service on the bridge address, one on an `isolated` internal network did not; `isolated` is refused on a non-internal network, and a port published from a network with `inhibit_ipv4` did not answer; under runsc release-20261109.0 a lookup through `127.0.0.11` was refused while runc resolved the same peer, `runsc flags` describes `--reproduce-nat` as "scrape the host netns NAT table and reproduce it in the sandbox", the annotation `dev.gvisor.flag.reproduce-nat` was refused without `--allow-flag-override`, and with the fixed addresses a gVisor worker resolved and reached the engine and gateway by name).

# Deployment

`src/deploy/ap up --credentials <file> --projects <folder>` checks its inputs and creates new
session tokens, or keeps those of a started session, so running it twice changes nothing. It passes the tokens and paths to Compose through environment variables only.
`ap down` stops everything and keeps run records and projects: the engine's store is the named
volume `engine-store`, which only the engine mounts and `down` does not remove. Until the run page
exists (T056), `ap up` without a built `page` image starts everything else and says the run page is
not built yet. The launcher's commands are in the
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
`ap up` starts contract-lint, the check service and the project worker, so build them first with
`src/deploy/build.sh lint`, `src/deploy/build.sh check` and `src/deploy/build.sh project` (the check image fetches the pinned
oapi-codegen and the template's modules while it is built, so that build needs the network).

# Observability

`docker compose logs`. The gateway logs nginx errors only, never headers or bodies.

# Access

The developer reaches the platform through the launcher and the run page on `127.0.0.1`. The
provider key file holds only the key, owned by the developer with mode 600. The gateway runs as
the developer's user so it can read the file, because Compose cannot change a file secret's owner
outside Swarm.

## Optional host firewall rule

The run page's forwarder, the gateway and the image registry are on bridges that keep the host's
addresses reachable, so a compromised one could reach a service listening on the host. The owner
may close that with one rule in the host's firewall. It needs root, `ap` never adds it, and nothing
in the platform depends on it. It drops every new connection from the platform's subnets to the
host itself, and keeps the replies to connections the host opens, which carry the run page and the
registry's published ports:

```sh
sudo iptables -I INPUT -s 10.231.0.0/16 -m conntrack ! --ctstate ESTABLISHED,RELATED -j DROP
```

Check it with the same rule and `-C` in place of `-I`; remove it with `-D`. It does not survive a
restart: keep it the way the distribution keeps firewall rules (for example `iptables-save`, or
the matching rule in `nftables.conf`, `ufw` or `firewalld`). It does not stop the gateway reaching
the local network or the internet, which goes through the host, not to it. Tested in a network
namespace of its own (iptables 1.8 with nf_tables, 2026-11-12): a new connection from a
`10.231.0.0/16` address to a host address was dropped, and a connection the host opened to that
address, and one from another address, went through; not tested on a running platform, since
adding it needs root.
