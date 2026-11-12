# src/deploy

Packaging: the ap launcher, Compose file, Dockerfiles, the nginx gateway template and the image lock.

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
   `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/images.env` (mode 600), replacing that name's
   line.

The key's password comes from `COSIGN_PASSWORD`, or cosign asks on the terminal; with neither,
`build.sh` stops before building. It exits 0 when the image is built, signed and recorded, 1 when a
step fails, and 3 for an unknown name.

The registry is the container `ap-registry` from the `registry` pin, on
`127.0.0.1:${AP_REGISTRY_PORT:-5005}` only, with its data in the volume `ap-registry-data`; see the
[registry concept](../../docs/components/distribution-registry.md) for its hardening and limits.
Building an image again replaces the previous one under the same name in the local store, so run
`ap up` after a rebuild. Every build yields a new digest, even from unchanged source, because the
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
| `AP_PAGE_PORT` | Host port of the run page, published on `127.0.0.1` only |
| `AP_CLIENT_TOKEN`, `AP_PAGE_TOKEN`, `AP_WORKER_TOKEN` | This session's tokens, 64 hex characters each; `ap up` on a started session keeps them, and only `ap up` after `ap down` makes new ones |
| `AP_HOST_USER` | The developer's `uid:gid`: the gateway runs as it to read the key file (mode 600; Compose cannot change a file secret's owner outside Swarm), and step workers run as it to write projects |
| `AP_GATEWAY_IMAGE` | The `nginx` pin from `images.lock`, its one home |
| `AP_IMAGE_ENGINE`, `AP_IMAGE_PAGE`, `AP_IMAGE_CLI`, `AP_IMAGE_AGENT`, `AP_IMAGE_CHECK`, `AP_IMAGE_TESTS` | Each image's digest reference from `images.env`, verified by `ap up` before anything starts and kept in the session; an image a profile `up` does not start (`agent`, `check`, `tests`) is verified when built and otherwise given the placeholder `ap-<name>:not-built`, so Compose can read the file |

`AP_REGISTRY_PORT` (default 5005) and `AP_REGISTRY_NAME` (default `ap-registry`) are read by
`build.sh` and `ap`, not by Compose. `AP_REGISTRY_NAME` is for running a second, independent
registry (for example in tests): the container is `$AP_REGISTRY_NAME`, its volume
`$AP_REGISTRY_NAME-data` and its internal network `$AP_REGISTRY_NAME-net`; unset, they are
`ap-registry`, `ap-registry-data` and `ap-signing`. Images are signed and verified under the
container's name, so build and start with the same value.

`ap up` needs the `engine`, `page` and `cli` images and the public key, and exits 1 naming the
`build.sh <name>` to run when one is missing. It verifies every image it passes to Compose with
`cosign verify --key cosign.pub --insecure-ignore-tlog` from the `cosign` pin, on `ap-signing` only,
and exits 1 naming the image when one fails, before Compose starts anything.

A missing variable stops Compose with a message naming it. Every service runs non-root with a
read-only root file system, all capabilities dropped, `no-new-privileges`, CPU, memory and process
limits, and writable `tmpfs` only where it needs one; no service mounts a container-engine socket.

| Service | Started by | Networks | Notes |
|---------|------------|----------|-------|
| `gateway` | `ap up` | `agents`, `egress` | The only container with egress and the only one with the key |
| `engine` | `ap up` | `clients`, `agents`, `checks` | Holds the three tokens; no store volume yet (the engine does not open its store yet) |
| `page` | `ap up` | `clients`, `page-host` | Port published on `127.0.0.1` only |
| `cli` | each client command (`run --rm`, profile `client`) | `clients` | Client token |
| `tests` | `ap test <suite>` (profile `tests`) | `clients` | Client token; no `Dockerfile.tests` yet |
| `step-agent` | `ap build-workers` (profile `workers`) | `agents` | gVisor, `/tmp` executable, projects read-only; the template for per-step workers |
| `check` | `ap build-workers` (profile `workers`) | `checks` | gVisor, `/tmp` executable, projects read-only; cannot reach the gateway |

All networks are internal except `egress` and `page-host`. Docker publishes no port from an
internal network, so the page also joins `page-host`, a bridge with IP masquerading turned off. That
removes NAT, not isolation: the page can still reach host services on the bridge address and send
one-way packets out with its private source address; a known limit, tracked as T022a. Our images
are started by digest and never pulled (`pull_policy: never`). The projects folder is not mounted into the gateway,
engine, page or client.

To read the resolved configuration without starting anything, set the variables and run
`docker compose -f src/deploy/compose.yaml --profile '*' config`.
