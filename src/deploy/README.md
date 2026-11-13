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
back into the local store by digest. Builds that start together on a machine with no registry
running and no key pair yet are safe too: `ap registry` lets each of them find the one registry
the first made (see below), and only one makes the key pair, under the lock `keys.lock` in the
signing folder, while the others wait for it and then sign with it.

The key's password comes from `COSIGN_PASSWORD`, or cosign asks on the terminal; with neither,
`build.sh` stops before building. It exits 0 when the image is built, signed and recorded, 1 when a
step fails, and 3 for an unknown name.

The registry is the container `ap-registry` from the `registry` pin, on
`127.0.0.1:${AP_REGISTRY_PORT:-5005}` only, with its data in the volume `ap-registry-data`; see the
[registry concept](../../docs/components/distribution-registry.md) for its hardening and limits.
It publishes its port from its own network `ap-registry-host` (IP masquerading off, nothing else on
it, subnet `10.231.5.0/24`), not Docker's default bridge, and cosign reaches it on the isolated
internal network `ap-signing`. `ap registry` may run several times at once (two builds starting
together): a step that fails because another run changed the registry in between (a name already
taken, a removal already in progress) fails that pass, which runs again, up to five passes, and
then finds what the other made. A registry whose port is taken is left running without it by
Docker 29; `ap registry` removes such a container (its data is kept) and exits 1 after the last
pass. Building an image again replaces the previous one under the same name in the local
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

`images.lock` also pins `wiremock`, the WireMock image that serves as the recorded-model replay
server and its recorder in tests (`src/tests/recorded/`, research D26). It stands in for the
gateway on test networks only; no image is built from it and Compose does not start it.

- `Dockerfile.agent`: the agent step image, with Docker Agent copied from its pinned image, the Go toolchain, and `TELEMETRY_ENABLED=false`.
- `Dockerfile.check`: the check service (`/check`, research D29) with the Go toolchain, the pinned oapi-codegen (`images.lock`, passed by `build.sh` as `OAPI_CODEGEN`), the catalogue's module template at `/opt/template` and the module cache of its `go.mod` at `/opt/gomodcache` (read-only), so checks and the generate procedure run with no network and a read-only root.
- `Dockerfile.engine`: the workflow engine, a static Go binary on `scratch`; expects the main package `src/engine/cmd/engine`.
- `Dockerfile.cli`: the terminal client, a static Go binary on `scratch`; expects `src/cli/cmd/cli`.
- `Dockerfile.page`: the read-only run page, a static Go binary on `scratch`; expects `src/page/cmd/page`.
- `Dockerfile.lint`: the contract-lint service (T038e, research D28), built as `build.sh lint` and
  recorded as `AP_IMAGE_LINT`. It is the pinned `node` image with `/tools/node_modules` installed by
  `npm ci` from `scripts/tools/package-lock.json`, which verifies every package's integrity, and
  `lint/server.mjs` at `/app/server.mjs`. Both come from named build contexts (`npm-tools`,
  `lint-service`), because the lockfile lies outside `src/` and `src/deploy/` is left out of the
  context. The server is Spectral's library behind `node:http`. `POST /lint` takes the contract as
  its body, at most 1 MiB, with `Authorization: Bearer $LINT_TOKEN`, and answers
  `[{level,code,text,path}]` under `spectral:oas` (recommended), within 30 s. Its `$ref` resolver
  has no file, http or https resolvers, so an external reference is an error finding and nothing is
  read or fetched. `GET /health` is open. The engine's client is `src/engine/contracts/lint.go`. Its
  tests are `src/tests/lint/` (Docker, run by `scripts/check.sh`), which run the server from the
  `node` pin with the npm-tool cache mounted at `/tools`. Compose starts it as `contract-lint`.

## Compose and the launcher

`ap` (POSIX sh, on the host) is the only way to start and stop the platform; its commands are in
the [terminal contract](../cli/contract.md). It calls only `docker` and passes every value to
`compose.yaml` through these environment variables, never on a command line:

| Variable | Holds |
|----------|-------|
| `AP_CREDENTIALS_FILE` | Absolute path of the file holding only the provider key; it reaches the gateway alone, as the Compose secret `anthropic_api_key` |
| `AP_PROJECTS_DIR` | Absolute path of the projects folder |
| `AP_PAGE_PORT` | Host port of the run page, published on `127.0.0.1` only (by `page-proxy`) |
| `AP_CLIENT_TOKEN`, `AP_PAGE_TOKEN`, `AP_WORKER_TOKEN`, `AP_LINT_TOKEN`, `AP_ANALYST_TOKEN`, `AP_CHECK_TOKEN` | This session's tokens, 64 hex characters each and pairwise distinct; `ap up` on a started session keeps them, and only `ap up` after `ap down` makes new ones. The last three are what the engine calls `contract-lint`, `analyst` and `check` with; each reaches only the engine and its own service |
| `AP_HOST_USER` | The developer's `uid:gid`: the gateway runs as it to read the key file (mode 600; Compose cannot change a file secret's owner outside Swarm), and step workers run as it to write projects |
| `AP_GATEWAY_IMAGE` | The `nginx` pin from `images.lock`, its one home; the gateway and `page-proxy` run it |
| `AP_IMAGE_ENGINE`, `AP_IMAGE_PAGE`, `AP_IMAGE_CLI`, `AP_IMAGE_AGENT`, `AP_IMAGE_CHECK`, `AP_IMAGE_TESTS`, `AP_IMAGE_LINT` | Each image's digest reference from `images.env`, verified by `ap up` before anything starts and kept in the session; an image a profile `up` does not start (`agent`, `tests`), `page` until the run page exists (T056, `up` then starts neither `page` nor `page-proxy`), and `lint` and `check` until their first signed build (T038g, T054c), is verified when built and otherwise given the placeholder `ap-<name>:not-built`, so Compose can read the file. `up` does start `contract-lint` and `check`, so without `build.sh lint` and `build.sh check` its `docker compose up` fails with `No such image: ap-<name>:not-built` |

`AP_REGISTRY_PORT` (default 5005) and `AP_REGISTRY_NAME` (default `ap-registry`) are read by
`build.sh` and `ap`, not by Compose. `AP_REGISTRY_NAME` is for running a second, independent
registry (for example in tests): the container is `$AP_REGISTRY_NAME`, its volume
`$AP_REGISTRY_NAME-data`, its internal network `$AP_REGISTRY_NAME-net` and its publishing network
`$AP_REGISTRY_NAME-host`; unset, they are `ap-registry`, `ap-registry-data`, `ap-signing` and
`ap-registry-host`; only the default publishing network has the fixed subnet, a second registry's
is chosen by Docker. A network with other driver options or another subnet (one made by an earlier
`ap`) is replaced, with the registry container; the data volume is kept. Images are signed and verified under the
container's name, so build and start with the same value.

`ap up` needs the `engine` and `cli` images and the public key, and exits 1 naming the
`build.sh <name>` to run when one is missing. The `page` image is optional until the run page
exists (T056): without one recorded, `up` passes the placeholder `ap-page:not-built`, does not
verify it, starts the rest with `--scale page=0 --scale page-proxy=0` and says on stderr that the
run page is not built yet; with one recorded it is verified and started like the others. It verifies every image it passes to Compose with
`cosign verify --key cosign.pub --insecure-ignore-tlog` from the `cosign` pin, on `ap-signing` only,
and exits 1 naming the image when one fails, before Compose starts anything. A recorded image no
longer in the local image store is first pulled back by digest from `127.0.0.1:<port>`; when the
registry no longer has it either, `up` exits 1 naming the `build.sh <name>` to run.

The session is `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/session.env`. `ap` makes that
folder mode 700 (also when it exists), writes the file mode 600, and reads it back with `.` only
when it is a regular file owned by the developer with mode 600; any other file stops every command
but `down` with exit 1 and `run: ap down`, and `down` stops the platform without reading it. The
file records the credentials file and projects folder; a second `up` with others exits 3 and says to
run `ap down` first. A failed `up` (Compose failed) keeps its session until `ap down`. A session
started before the `contract-lint` and `analyst` services were added lacks their tokens, so `up`
says it is incomplete: run `ap down` once, then `ap up`. Client
commands, `build` and `test` pass `-T` to `docker compose run` when standard input is not a
terminal.

A missing variable stops Compose with a message naming it. Every service runs non-root with a
read-only root file system, all capabilities dropped, `no-new-privileges`, CPU, memory and process
limits, and writable `tmpfs` only where it needs one; no service mounts a container-engine socket.

| Service | Started by | Networks | Notes |
|---------|------------|----------|-------|
| `gateway` | `ap up` | `agents`, `analyst`, `egress` | The only container with egress and the only one with the key |
| `engine` | `ap up` | `clients`, `agents`, `checks`, `lint`, `analyst` | Holds the three caller tokens, and `AP_LINT_URL` (`http://contract-lint:8080`), `AP_LINT_TOKEN`, `AP_ANALYST_URL` (`http://analyst:8080`), `AP_ANALYST_TOKEN`, `AP_CHECK_URL` (`http://check:8080`) and `AP_CHECK_TOKEN`, which it reads from T038g and T054c; its store is the named volume `engine-store` at `/var/lib/engine` (`ENGINE_STORE_DIR`), the only writable path besides `/tmp` and mounted by no other service; the image makes that directory owned by 65532, which Docker gives a new volume; `ap down` keeps the volume |
| `contract-lint` | `ap up` | `lint` | `AP_IMAGE_LINT`; gVisor; its token as `LINT_TOKEN`; reachable by the engine only; finds `engine` through `extra_hosts` (the gVisor rule; it calls no one) |
| `analyst` | `ap up` | `analyst` | The `docker-agent` pin by digest (an upstream image like the gateway's, so no `AP_IMAGE_` variable; written out in `compose.yaml`, and a test fails when it differs from `images.lock`); gVisor; `/docker-agent serve api /agents/analyst.yaml` on port 8080 with `--session-db /tmp/session.db` on a tmpfs owned by UID 65532; `src/agents` mounted read-only at `/agents`; the token from its environment through `sh -c`, so not on its command line; `MODEL_GATEWAY_URL=http://gateway:8080`, a placeholder `ANTHROPIC_API_KEY` and `TELEMETRY_ENABLED=false`; finds `engine` and `gateway` through `extra_hosts`; its only way out is the gateway |
| `page` | `ap up`, when its image is recorded (T056) | `clients`, `page` | Reached only through `page-proxy`; checks the Host header |
| `page-proxy` | `ap up`, with `page` | `page`, `page-host` | The `nginx` pin with the fixed `page-proxy/nginx.conf`; publishes the page's port on `127.0.0.1` only and passes each request, Host header unchanged, to the page |
| `cli` | each client command (`run --rm`, profile `client`) | `clients` | Client token |
| `tests` | `ap test <suite>` (profile `tests`) | `clients` | Client token; no `Dockerfile.tests` yet |
| `step-agent` | `ap build-workers` (profile `workers`) | `agents` | gVisor, `/tmp` executable, projects read-only; finds `engine` and `gateway` through `extra_hosts`; the template for per-step workers |
| `check` | `ap up`, like `contract-lint` (no profile; its image stays optional until built) | `checks` | The check service (`CHECK_DEPLOYMENT=check-service`): runs `/check`, answers only the engine's `AP_CHECK_TOKEN` (`CHECK_TOKEN`), holds no worker token; gVisor, `/tmp` executable, projects read-only; finds `engine` through `extra_hosts`; cannot reach the gateway |

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
asks for. The owner may close the host to the forwarder, the gateway and the registry with one
host firewall rule, an optional step that needs root and that `ap` never runs; see
[the local environment](../../docs/operations/local.md#optional-host-firewall-rule).

Names under gVisor: a container under `runtime: runsc` cannot use Docker's embedded DNS server
(`127.0.0.11`; tested with runsc release-20261109.0 on Docker 29.9.3: the lookup is refused, while
the same container under runc resolves the peer and a gVisor container reaches it by address).
gVisor's own network stack never sees the NAT rule in the container's namespace that carries
`127.0.0.11` to Docker's resolver; runsc's `--reproduce-nat` flag copies it, but that is a flag of
the host's Docker configuration (a per-container annotation is refused without
`--allow-flag-override`). So `compose.yaml` gives the networks shared with the workers fixed
subnets and the engine and gateway fixed addresses on them, and the workers find those two names
through `/etc/hosts` (`extra_hosts`); so do `contract-lint` and `analyst`, also under gVisor. Each subnet and address is declared once, under
`x-addresses`:

| Network | Subnet | Fixed addresses | Who uses them |
|---------|--------|-----------------|---------------|
| `agents` | `10.231.1.0/24` (others from `10.231.1.128/25`) | `engine` `10.231.1.2`, `gateway` `10.231.1.3` | `step-agent` |
| `checks` | `10.231.2.0/24` (others from `10.231.2.128/25`) | `engine` `10.231.2.2` | `check` |
| `lint` | `10.231.6.0/24` (others from `10.231.6.128/25`) | `engine` `10.231.6.2` | `contract-lint` |
| `analyst` | `10.231.7.0/24` (others from `10.231.7.128/25`) | `engine` `10.231.7.2`, `gateway` `10.231.7.3` | `analyst` |
| `egress` | `10.231.3.0/24` | none | the host firewall rule |
| `page-host` | `10.231.4.0/24` | none | the host firewall rule |

Every container under runc keeps using Docker's DNS. The subnets lie outside Docker's default
address pools; one that overlaps another network on the machine stops `ap up` with Compose's
`Pool overlaps` message, and only one Compose project from this file can run at a time.

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
