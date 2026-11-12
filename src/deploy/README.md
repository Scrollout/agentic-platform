# src/deploy

Packaging: the ap launcher, Compose file, Dockerfiles, the nginx gateway template and the image lock.

## Images

`build.sh <name>` builds `ap-<name>:dev` with `src/` as the context. It reads each base image from
`images.lock` and passes it as a build arg (`GOLANG_IMAGE`, `DOCKER_AGENT_IMAGE`), so no digest is
copied into a Dockerfile. Run `scripts/generate.sh` first: generated code in `api/gen/` is not committed.
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

A missing variable stops Compose with a message naming it. Every service runs non-root with a
read-only root file system, all capabilities dropped, `no-new-privileges`, CPU, memory and process
limits, and writable `tmpfs` only where it needs one; no service mounts a container-engine socket.

| Service | Started by | Networks | Notes |
|---------|------------|----------|-------|
| `gateway` | `ap up` | `agents`, `egress` | The only container with egress and the only one with the key |
| `engine` | `ap up` | `clients`, `agents`, `checks` | Holds the three tokens; no store volume yet (the engine does not open its store yet) |
| `page` | `ap up` | `clients`, `page-host` | Port published on `127.0.0.1` only |
| `cli` | each client command (`run --rm`, profile `client`) | `clients` | Client token |
| `tests` | `ap test <suite>` (profile `tests`) | `clients` | Client token; image `ap-tests:dev` not written yet |
| `step-agent` | `ap build-workers` (profile `workers`) | `agents` | gVisor, `/tmp` executable, projects read-only; the template for per-step workers |
| `check` | `ap build-workers` (profile `workers`) | `checks` | gVisor, `/tmp` executable, projects read-only; cannot reach the gateway |

All networks are internal except `egress` and `page-host`. Docker publishes no port from an
internal network, so the page also joins `page-host`, a bridge with IP masquerading turned off. That
removes NAT, not isolation: the page can still reach host services on the bridge address and send
one-way packets out with its private source address; a known limit, tracked as T022a. Images named `ap-*:dev`
are never pulled (`pull_policy: never`). The projects folder is not mounted into the gateway,
engine, page or client.

To read the resolved configuration without starting anything, set the variables and run
`docker compose -f src/deploy/compose.yaml --profile '*' config`.
