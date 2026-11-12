# src/deploy

Packaging: the ap launcher, Compose file, Dockerfiles, the nginx gateway template, the run page
forwarder's nginx configuration (`page-proxy/nginx.conf`) and the image lock.

## Images

`build.sh <name> [docker build options]` builds, signs and records one image
([ADR 0028](../../docs/decisions/0028-sign-platform-images-locally.md)); `<name>` is any
`Dockerfile.<name>` here. Run `scripts/generate.sh` first: generated code in `api/gen/` is not
committed. It reads each base image from `images.lock` and passes it as a build arg
(`GOLANG_IMAGE`, `DOCKER_AGENT_IMAGE`), so no digest is copied into a Dockerfile. In order, it:

1. starts the local image registry if it isn't running (`ap registry`);
2. makes the signing material on first use, from cosign containers with no network, in `${XDG_CONFIG_HOME:-~/.config}/agentic-platform/signing/`
   (mode 700): an encrypted `cosign.key` (mode 600), `cosign.pub`, and `signing-config.json` with no
   transparency log, so no image name or digest leaves the machine;
3. builds with `src/` as the context, an SBOM (generator: the `sbom-scanner` pin) and build
   provenance (`mode=max`), and pushes `127.0.0.1:<port>/ap-<name>:dev`. The image also stays in
   the local image store (Docker's containerd image store), so Compose starts it by digest without
   pulling;
4. signs the pushed digest with cosign from its pin, reaching the registry as `ap-registry:5000`
   on the internal network `ap-signing`; the private key is mounted read-only into that container
   only;
5. records `AP_IMAGE_<NAME>=127.0.0.1:<port>/ap-<name>@sha256:<digest>` in
   `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/images.env` (mode 600, folder mode 700),
   replacing that name's line.

Builds may run at the same time, of the same or of different images: recording takes the lock
`images.env.lock` (a directory next to `images.env` holding the build's process ID), so no build
loses another's line; a build waits for it up to two minutes, then exits 1 naming it. A lock whose
build is no longer running is removed by the next build. When two builds of the same name overlap,
the one that records last wins, and an image whose tag another build has since moved is pulled
back into the local store by digest.

The key's password comes from `COSIGN_PASSWORD`, or cosign asks on the terminal; with neither,
`build.sh` stops before building. It exits 0 when the image is built, signed and recorded, 1 when a
step fails, and 3 for an unknown name.

The registry is the container `ap-registry` from the `registry` pin, on
`127.0.0.1:${AP_REGISTRY_PORT:-5005}` only, with its data in the volume `ap-registry-data`; see the
[registry concept](../../docs/components/distribution-registry.md) for its hardening and limits.
It publishes its port from its own network `ap-registry-host` (IP masquerading off, nothing else on
it), not Docker's default bridge, and cosign reaches it on the isolated internal network
`ap-signing`. Building an image again replaces the previous one under the same name in the local
store, so run `ap up` after a rebuild; an `up` whose recorded image is no longer in the local store
pulls it back by digest from the local registry on `127.0.0.1` before verifying it. Every build yields a new digest, even from unchanged source, because the
provenance and SBOM carry the build's time; so the next `ap up` recreates that image's containers.
A change to the registry's pin, port or run flags replaces its container on the next build or
`up`; its data volume is kept.
The ignore rule for the Go stack is `src/.dockerignore`; it leaves out `*_test.go`, `testdata/`,
`evals/`, `src/tests/` and `src/deploy/`. Every image runs as UID 65532, not root.

`images.lock` also pins `golang-race`, the Debian-based golang image of the same Go release as
`golang`. It has the C compiler that `go test -race` needs and is used only by `scripts/check.sh`
for the race run, never to build an image.

- `Dockerfile.agent`: the agent step image, with Docker Agent copied from its pinned image, the Go toolchain, and `TELEMETRY_ENABLED=false`.
- `Dockerfile.check`: the check container, with the Go toolchain only, to run checks on generated code.
- `Dockerfile.engine`: the workflow engine, a static Go binary on `scratch`; expects the main package `src/engine/cmd/engine`.
- `Dockerfile.cli`: the terminal client, a static Go binary on `scratch`; expects `src/cli/cmd/cli`.
- `Dockerfile.page`: the read-only run page, a static Go binary on `scratch`; expects `src/page/cmd/page`.

## Compose and the launcher

`ap` (POSIX sh, on the host) is the only way to start and stop the platform; its commands are in
the [terminal contract](../cli/contract.md). It calls only `docker` and passes every value to
`compose.yaml` through these environment variables, never on a command line:

| Variable | Holds |
|----------|-------|
| `AP_CREDENTIALS_FILE` | Absolute path of the file holding only the provider key; it reaches the gateway alone, as the Compose secret `anthropic_api_key` |
| `AP_PROJECTS_DIR` | Absolute path of the projects folder |
| `AP_PAGE_PORT` | Host port of the run page, published on `127.0.0.1` only (by `page-proxy`) |
| `AP_CLIENT_TOKEN`, `AP_PAGE_TOKEN`, `AP_WORKER_TOKEN` | This session's tokens, 64 hex characters each; `ap up` on a started session keeps them, and only `ap up` after `ap down` makes new ones |
| `AP_HOST_USER` | The developer's `uid:gid`: the gateway runs as it to read the key file (mode 600; Compose cannot change a file secret's owner outside Swarm), and step workers run as it to write projects |
| `AP_GATEWAY_IMAGE` | The `nginx` pin from `images.lock`, its one home; the gateway and `page-proxy` run it |
| `AP_IMAGE_ENGINE`, `AP_IMAGE_PAGE`, `AP_IMAGE_CLI`, `AP_IMAGE_AGENT`, `AP_IMAGE_CHECK`, `AP_IMAGE_TESTS` | Each image's digest reference from `images.env`, verified by `ap up` before anything starts and kept in the session; an image a profile `up` does not start (`agent`, `check`, `tests`) is verified when built and otherwise given the placeholder `ap-<name>:not-built`, so Compose can read the file |

`AP_REGISTRY_PORT` (default 5005) and `AP_REGISTRY_NAME` (default `ap-registry`) are read by
`build.sh` and `ap`, not by Compose. `AP_REGISTRY_NAME` is for running a second, independent
registry (for example in tests): the container is `$AP_REGISTRY_NAME`, its volume
`$AP_REGISTRY_NAME-data`, its internal network `$AP_REGISTRY_NAME-net` and its publishing network
`$AP_REGISTRY_NAME-host`; unset, they are `ap-registry`, `ap-registry-data`, `ap-signing` and
`ap-registry-host`. A network with other driver options (one made by an earlier `ap`) is replaced,
with the registry container; the data volume is kept. Images are signed and verified under the
container's name, so build and start with the same value.

`ap up` needs the `engine`, `page` and `cli` images and the public key, and exits 1 naming the
`build.sh <name>` to run when one is missing. It verifies every image it passes to Compose with
`cosign verify --key cosign.pub --insecure-ignore-tlog` from the `cosign` pin, on `ap-signing` only,
and exits 1 naming the image when one fails, before Compose starts anything. A recorded image no
longer in the local image store is first pulled back by digest from `127.0.0.1:<port>`; when the
registry no longer has it either, `up` exits 1 naming the `build.sh <name>` to run.

The session is `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/session.env`. `ap` makes that
folder mode 700 (also when it exists), writes the file mode 600, and reads it back with `.` only
when it is a regular file owned by the developer with mode 600; any other file stops every command
but `down` with exit 1 and `run: ap down`, and `down` stops the platform without reading it. The
file records the credentials file and projects folder; a second `up` with others exits 3 and says to
run `ap down` first. A failed `up` (Compose failed) keeps its session until `ap down`. Client
commands, `build` and `test` pass `-T` to `docker compose run` when standard input is not a
terminal.

A missing variable stops Compose with a message naming it. Every service runs non-root with a
read-only root file system, all capabilities dropped, `no-new-privileges`, CPU, memory and process
limits, and writable `tmpfs` only where it needs one; no service mounts a container-engine socket.

| Service | Started by | Networks | Notes |
|---------|------------|----------|-------|
| `gateway` | `ap up` | `agents`, `egress` | The only container with egress and the only one with the key |
| `engine` | `ap up` | `clients`, `agents`, `checks` | Holds the three tokens; no store volume yet (the engine does not open its store yet) |
| `page` | `ap up` | `clients`, `page` | Reached only through `page-proxy`; checks the Host header |
| `page-proxy` | `ap up` | `page`, `page-host` | The `nginx` pin with the fixed `page-proxy/nginx.conf`; publishes the page's port on `127.0.0.1` only and passes each request, Host header unchanged, to the page |
| `cli` | each client command (`run --rm`, profile `client`) | `clients` | Client token |
| `tests` | `ap test <suite>` (profile `tests`) | `clients` | Client token; no `Dockerfile.tests` yet |
| `step-agent` | `ap build-workers` (profile `workers`) | `agents` | gVisor, `/tmp` executable, projects read-only; the template for per-step workers |
| `check` | `ap build-workers` (profile `workers`) | `checks` | gVisor, `/tmp` executable, projects read-only; cannot reach the gateway |

All networks are internal except `egress` and `page-host`, and every internal one is `isolated`
(`com.docker.network.bridge.gateway_mode_ipv4`, Docker 28 and later): its bridge has no address on
the host, so a container on it reaches only the other containers on it, not the host's services
nor the outside. A plain internal network is not enough: tested on Docker 29, a container on one
reached a host service on the bridge address. Docker publishes no port from an internal network,
and no gateway mode keeps publishing on `127.0.0.1` while removing the host's address (`isolated`
is refused on a non-internal network; with `inhibit_ipv4` the published port stops answering;
`routed` and `nat-unprotected` keep the bridge address). So `page-proxy` is the only container on
`page-host`, a bridge with IP masquerading off. The page itself has no route to the host or out;
the forwarder can still reach the host's addresses and send packets out that get no reply, a limit
of this static, pinned component, not of the page. Publishing relies on Docker's userland proxy
(the default) to carry `127.0.0.1` to the container. The gateway's `egress` is a plain bridge
with NAT: besides the provider, the gateway can reach host services, the local network and the
whole internet, and only its fixed nginx configuration (one upstream, one path) limits what it
asks for.

`ap up` and `ap registry` need Docker 28 or later and exit 3 naming the version otherwise, because
an older engine may ignore the `isolated` option and leave the host reachable. Networks made by an
earlier `ap up` lack `isolated`; the next `ap up` replaces them on its own (tested with Compose
5.7: Compose stops the containers on such a network, recreates it with the new options and starts
them again). A container Compose did not start in that `up`, such as a client command still
running, keeps the network in use and the `up` fails; run `ap down`, then `ap up`. Our images
are started by digest and never pulled (`pull_policy: never`). The projects folder is not mounted into the gateway,
engine, page or client.

To read the resolved configuration without starting anything, set the variables and run
`docker compose -f src/deploy/compose.yaml --profile '*' config`.
