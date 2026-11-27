# src/deploy

Packaging: the ap launcher, Compose files, Dockerfiles, the nginx gateway template, the run page
forwarder's nginx configurations (`page-proxy/nginx.conf`, and `page-proxy/nginx-remote.conf` for
remote access), the lock helper `lock.sh` and the image lock.

The developer starts the platform with `ap start` alone (below); `build.sh` and `ap up` are the
lower commands it uses, which the platform's own development and CI call directly.

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

Each signed build also records the image's fingerprint as `AP_BUILT_<NAME>` in `built.env`, next
to `images.env`: the digest of a candidate build of the same sources (`SOURCE_DATE_EPOCH=0`,
`rewrite-timestamp=true`, no SBOM, provenance or push), which stays the same while its inputs do.
`build.sh --changed <name>...` builds, signs and records only the named images whose fingerprint
changed or that are not recorded, asking for the signing key's password once and only when it must
sign; `--changed --check` only says `would build <names>` (exit 1) or `nothing to build` (exit 0).
`ap start` runs it before `up` (feature 003, ADR 0061, ADR 0063).

BuildKit does not cache the candidate's timestamp rewrite (24 s for the check image on a 4-CPU
machine), so each signed build also records `AP_CACHED_<NAME>=<digest> <fingerprint>` in
`cached.env` (mode 600): the digest of the same build without `rewrite-timestamp`, which takes
about a second while the build cache holds the image's layers. `--changed` makes these cached
builds first and needs an image's candidate only when its cached digest, fingerprint or stored
image no longer matches; a candidate that finds the image current renews the pair (not with
`--check`). A failed cached build counts as no pair (feature 003, ADR 0064).

Builds may run at the same time, of the same or of different images: recording takes the lock
`images.env.lock` (a directory next to `images.env` holding the build's process ID), so no build
loses another's line; a build waits for it up to two minutes, then exits 1 naming it. A lock whose
build is no longer running is removed by the next build. When two builds of the same name overlap,
the one that records last wins, and an image whose tag another build has since moved is pulled
back into the local store by digest. Builds that start together on a machine with no registry
running and no key pair yet are safe too: `ap registry` lets each of them find the one registry
the first made (see below), and only one makes the key pair, under the lock `keys.lock` in the
signing folder, while the others wait for it and then sign with it. Both locks follow one rule,
kept in `lock.sh`, which `build.sh` sources.

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

`images.lock` also pins `alpine-git`, git's exact apk package version in the `alpine` pin's
release, which `build.sh` passes as `ALPINE_GIT` to `Dockerfile.project`. An Alpine package has no
digest, so it is pinned by version only (an owner-approved exception: ADR 0033 and the plan's Complexity Tracking); a new package
release makes that build fail until the pin moves.

`images.lock` also pins `wiremock`, the WireMock image that serves as the recorded-model replay
server and its recorder in tests (`src/tests/recorded/`, research D26). It stands in for the
gateway on test networks only; no image is built from it and Compose does not start it.

- `Dockerfile.agent`: the agent step image, with Docker Agent copied from its pinned image, the Go toolchain, and `TELEMETRY_ENABLED=false`.
- `Dockerfile.check`: the check service (`/check`, research D29) with the Go toolchain, the pinned oapi-codegen (`images.lock`, passed by `build.sh` as `OAPI_CODEGEN`), the catalogue's module template at `/opt/template` and the module cache of its `go.mod` at `/opt/gomodcache` (owned by root, read-only, shared by every call), so checks and the generate procedure run with no network and a read-only root. It sets no `GOCACHE` or `GOPATH`: each call of the check program makes its own folder under the `/tmp` tmpfs for `GOCACHE`, `GOTMPDIR`, `TMPDIR`, `HOME` and `GOPATH` and removes it when the call ends (research D37, ADR 0041); `HOME=/tmp/home` is the service process's own. Its runtime stage is `FROM scratch` with the golang image's files copied in, since that image's `ENV` sets `GOPATH=/go`. A template stage, which copies only the catalogue's module template, makes oapi-codegen and the module cache, so a change elsewhere in `src/` leaves the image's digest unchanged (feature 003, research R1); it also compiles a warm base Go cache from the pinned sources only (the standard library dependencies of the template's vendored tools and of a derived test, through `go test`, `-cover` and `go test -c` with the service's settings) into `/opt/gocache-base` (root-owned, read-only, about 117 MB; the image grew from 418 MB to 569 MB), and `CHECK_GOCACHE_BASE=/opt/gocache-base` makes each call start from a copy of it.
- `Dockerfile.project`: the project worker (research D30, ADRs 0032 and 0033), built as `build.sh project` (`ap-project`, `AP_IMAGE_PROJECT`): the static check binary from the golang pin on the `alpine` pin, with `git=<alpine-git>` from apk and a folder `/var/lib/project-worker` of mode 1777, which a new `project-worker-state` volume takes its mode from; no Go toolchain (about 45 MB).
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
the [terminal contract](../cli/contract.md). `ap registry` (starts the local image registry) is
called by `build.sh` and is left out of `ap`'s usage, which lists only what the developer runs.

`ap start` (feature 003, ADR 0063) holds the lock `start.lock` in the state folder (`lock.sh`'s
rule, the one `build.sh` uses for `images.env.lock` and `keys.lock`), checks the machine (Docker 28
or later, `runsc` in `docker info`'s runtimes, the key file, by default
`${XDG_CONFIG_HOME:-~/.config}/agentic-platform/anthropic_api_key`, and the projects folder, by
default `~/projects`, made with mode 755 when missing), refuses a started session with another key
file, projects folder or remote access, runs `build.sh --changed engine cli page agent check
project lint`, then `up` with the resolved options. `ap start --check` makes no change.

With `--listen` (on `up`, passed on by `start`), `ap` adds `compose.remote.yaml` after
`compose.yaml` to every Compose call of the session: it changes `page-proxy` alone, which then runs
as `AP_HOST_USER`, reads `page-proxy/nginx-remote.conf` (TLS 1.2/1.3, HTTP Basic authentication,
10 r/s per address with a burst of 20, a 421 default server) and the folder `AP_PAGE_CERT_DIR`, and
publishes its TLS port on `AP_PAGE_ADDRESS`. That folder, `page-remote/` in the state folder (mode
700), holds `cert.pem` and `key.pem` (made with Go's `generate_cert.go` in the `golang` pin with no
network, or the developer's own), `cert.names`, `htpasswd` (the secret's SHA-512 crypt line from
the nginx pin's `mkpasswd`) and `names.conf`, all mode 600 (data model of feature 003). Each change
of remote access is appended to `access.log` in the state folder, never with the secret; `down`
keeps it.

It calls only `docker` and passes every value to `compose.yaml` through these environment
variables, never on a command line:

| Variable | Holds |
|----------|-------|
| `AP_CREDENTIALS_FILE` | Absolute path of the file holding only the provider key; it reaches the gateway alone, as the Compose secret `anthropic_api_key` |
| `AP_PROJECTS_DIR` | Absolute path of the projects folder |
| `AP_PAGE_PORT` | Host port of the run page, published on `127.0.0.1` only (by `page-proxy`), or on `AP_PAGE_ADDRESS` with remote access |
| `AP_PAGE_ADDRESS` | With remote access only: the `--listen` address the page's port is published on (`compose.remote.yaml`) |
| `AP_PAGE_CERT_DIR` | With remote access only: the `page-remote/` folder, mounted read-only into `page-proxy` at `/etc/ap/remote` |
| `AP_CLIENT_TOKEN`, `AP_PAGE_TOKEN`, `AP_WORKER_TOKEN`, `AP_LINT_TOKEN`, `AP_ANALYST_TOKEN`, `AP_CHECK_TOKEN`, `AP_PROJECT_TOKEN` | This session's tokens, 64 hex characters each and pairwise distinct; `ap up` on a started session keeps them, and only `ap up` after `ap down` makes new ones. The last four are what the engine calls `contract-lint`, `analyst`, `check` and `project-worker` with; each reaches only the engine and its own service (the engine holds `AP_PROJECT_TOKEN` too, since T053e). A session started before `AP_PROJECT_TOKEN` existed is refused as incomplete: run `ap down` |
| `AP_GRANTS_DIR` | The step worker grants folder, `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/grants` (research D31). `ap up` makes it and hands it to the engine's user (65532) with mode 700, from a one-off container with only the CHOWN capability; only the engine mounts it, read-only, at `ENGINE_GRANTS_DIR`. `build-workers` writes one file per grant into it (named the lowercase hex SHA-256 of the worker's token, holding `{"slice","step","call_token"}`, mode 600, from a one-off container running as the engine's user, the tokens on standard input), and `down` empties it. Unset, Compose names a folder that does not exist, so the engine does not start |
| `AP_HOST_USER` | The developer's `uid:gid`: the gateway runs as it to read the key file (mode 600; Compose cannot change a file secret's owner outside Swarm), and step workers run as it to write projects |
| `AP_GATEWAY_IMAGE` | The `nginx` pin from `images.lock`, its one home; the gateway and `page-proxy` run it |
| `AP_IMAGE_ENGINE`, `AP_IMAGE_PAGE`, `AP_IMAGE_CLI`, `AP_IMAGE_AGENT`, `AP_IMAGE_CHECK`, `AP_IMAGE_PROJECT`, `AP_IMAGE_TESTS`, `AP_IMAGE_LINT` | Each image's digest reference from `images.env`, verified by `ap up` before anything starts and kept in the session; an image a profile `up` does not start (`agent`, `tests`), `page` until the run page exists (T056, `up` then starts neither `page` nor `page-proxy`), and `lint`, `check` and `project` until their first signed build (T038g, T054c, T053c), is verified when built and otherwise given the placeholder `ap-<name>:not-built`, so Compose can read the file. `up` does start `contract-lint`, `check` and `project-worker`, so without `build.sh lint`, `build.sh check` and `build.sh project` its `docker compose up` fails with `No such image: ap-<name>:not-built` |

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
file records the credentials file and projects folder, and with remote access also
`AP_PAGE_ADDRESS`, `AP_PAGE_NAMES`, `AP_PAGE_SECRET` (the sign-in secret, new after each `down`) and
`AP_PAGE_CERT_DIR`; a second `up` with others, or with another remote access, exits 3 and says to
run `ap down` first. A failed `up` (Compose failed) keeps its session until `ap down`. A session
started before the `contract-lint` and `analyst` services were added lacks their tokens, so `up`
says it is incomplete: run `ap down` once, then `ap up`. Client
commands and `build` pass `-T` to `docker compose run` when standard input is not a terminal.
They run `docker compose run` in the background with `ap`'s standard input, pass SIGINT and SIGTERM
on to it, and exit with the client's code (130 or 143 when it detaches).

`ap test <suite>` (`planted`, `us2`) needs no session and calls no Compose (research D33,
[ADR 0037](../../docs/decisions/0037-terminal-run-commands-exit-codes-and-test-on-host.md)): it
builds `src/tests/acceptance` with `go test -c -tags docker` in `images.lock`'s `golang`
container (`--network none`, `GOPROXY=off`, `GOFLAGS=-mod=readonly`, `CGO_ENABLED=0`, `src/`
mounted, the module cache `${XDG_CACHE_HOME:-~/.cache}/agentic-platform/go` at `/gopath`), runs
the binary on the host in `src/tests/acceptance` with `-test.run` (`^TestPlanted$` or `^TestUS2`)
and `-test.v=test2json`, converts its output with `go tool test2json` in the same pinned container
(`--network none`), and prints the contract's lines. Exit 0 when every test passes, 1 for a failing
trial or test or a failed build, 3 for an unknown suite. The binary is removed afterwards.
`ap test planted <scenario>` runs only that scenario (`^TestPlanted$/^<scenario>$`; ADR 0074):
exit 3 for a name not made of lowercase letters, digits and single hyphens, a second argument or a
scenario for `us2`, and 1 with `test: no test of the planted suite ran` for a name no scenario has.

A missing variable stops Compose with a message naming it. Every service runs non-root with a
read-only root file system, all capabilities dropped, `no-new-privileges`, CPU, memory and process
limits, and writable `tmpfs` only where it needs one; no service mounts a container-engine socket.

| Service | Started by | Networks | Notes |
|---------|------------|----------|-------|
| `gateway` | `ap up` | `agents`, `analyst`, `egress` | The only container with egress and the only one with the key |
| `engine` | `ap up` | `clients`, `agents`, `checks`, `lint`, `analyst` | Holds the three caller tokens, and `AP_LINT_URL` (`http://contract-lint:8080`), `AP_LINT_TOKEN`, `AP_ANALYST_URL` (`http://analyst:8080`), `AP_ANALYST_TOKEN`, `AP_CHECK_URL` (`http://check:8080`), `AP_CHECK_TOKEN`, `AP_PROJECT_URL` (`http://project-worker:8080`) and `AP_PROJECT_TOKEN`, all required, which it reads from T038g, T054c and T053e; its store is the named volume `engine-store` at `/var/lib/engine` (`ENGINE_STORE_DIR`), the only writable path besides `/tmp` and mounted by no other service; the image makes that directory owned by 65532, which Docker gives a new volume; `ap down` keeps the volume |
| `contract-lint` | `ap up` | `lint` | `AP_IMAGE_LINT`; gVisor; its token as `LINT_TOKEN`; reachable by the engine only; finds `engine` through `extra_hosts` (the gVisor rule; it calls no one) |
| `analyst` | `ap up` | `analyst` | The `docker-agent` pin by digest (an upstream image like the gateway's, so no `AP_IMAGE_` variable; written out in `compose.yaml`, and a test fails when it differs from `images.lock`); gVisor; `/docker-agent serve api /agents/analyst.yaml` on port 8080 with `--session-db /tmp/session.db` on a tmpfs owned by UID 65532; `src/agents` mounted read-only at `/agents`; the token from its environment through `sh -c`, so not on its command line; `MODEL_GATEWAY_URL=http://gateway:8080`, a placeholder `ANTHROPIC_API_KEY` and `TELEMETRY_ENABLED=false`; finds `engine` and `gateway` through `extra_hosts`; its only way out is the gateway |
| `durable-store` | `ap up` | `durable-store` | The `postgres` pin as user 70:70; the volume `durable-store` holds the data and `ap down` keeps it; its password is the file `durable-store-password` that `ap up` makes once in the folder `durable/` of the state folder (folder mode 700, file mode 644 because Compose mounts a file secret with the host's owner and mode; never printed) and Compose gives it as a secret; the image is pulled by digest when missing, like the gateway's; tmpfs `/tmp` and `/var/run/postgresql`; 256 MiB |
| `durable-schema` | `ap up`, before `durable` | `durable-store` | The `temporal-admin-tools` pin running `durable/schema.sh` once: `temporal-sql-tool` creates the `temporal` and `temporal_visibility` databases and updates their schemas; safe to run again; exits 0. It does not register the namespace `ap`, which needs the running server: the engine does, when it connects (ADR 0070) |
| `durable` | `ap up` | `durable`, `durable-store` | The `temporal-server` pin, every service in one process, plugin `postgres12`, as the image's user 1000; dynamic config `durable/dynamicconfig.yaml` (version check off); the engine is the only other member of `durable`; the engine starts after it is healthy; 256 MiB |
| `page` | `ap up`, when its image is recorded (T056) | `clients`, `page` | Reached only through `page-proxy`; checks the Host header |
| `page-proxy` | `ap up`, with `page` | `page`, `page-host` | The `nginx` pin with the fixed `page-proxy/nginx.conf`; publishes the page's port on `127.0.0.1` only and passes each request, Host header unchanged, to the page; with remote access, `compose.remote.yaml` (above) |
| `cli` | each client command (`run --rm`, profile `client`) | `clients` | Client token |
| `tests` | nothing (profile `tests`) | `clients` | Unused since `ap test` runs on the host (ADR 0037); its removal waits for the owner's approval |
| `step-agent` | `ap build-workers` (profile `workers`) | `agents` | The template of each agent step's container: Docker Agent `serve api /agents/$STEP_AGENT.yaml` with `--auth-token "$STEP_TOKEN"` through `sh -c` (it refuses to start without either; the token is then on docker-agent's command line inside the sandbox, a known limit); `ENGINE_TOKEN` empty, so the template holds no session token; gVisor, `/tmp` executable, `src/agents` read-only at `/agents`, a placeholder `ANTHROPIC_API_KEY`; finds `engine` and `gateway` through `extra_hosts` |
| `procedure-worker` | `ap build-workers` (profile `workers`) | `checks` | The template of each procedure step's container: `/check` from the check image as `CHECK_DEPLOYMENT=procedure-worker`, run as `AP_HOST_USER`; the template holds no token (`CHECK_TOKEN` empty, so it does not start alone), and `build-workers` sets `CHECK_PROCEDURE`, `CHECK_STEP` and `CHECK_TOKEN`, the container's call token; gVisor; finds `engine` through `extra_hosts`; cannot reach the gateway |
| `check` | `ap up`, like `contract-lint` (no profile; its image stays optional until built) | `checks` | The check service (`CHECK_DEPLOYMENT=check-service`): runs `/check`, answers only the engine's `AP_CHECK_TOKEN` (`CHECK_TOKEN`), holds no worker token; gVisor, `/tmp` executable, projects read-only; finds `engine` through `extra_hosts`; cannot reach the gateway |
| `project-worker` | `ap up`, like `check` | `checks` | The project worker (`CHECK_DEPLOYMENT=project-worker`, research D30): runs `/check` from its own image (`AP_IMAGE_PROJECT`, `Dockerfile.project`) as `AP_HOST_USER`, answers only `AP_PROJECT_TOKEN` (`CHECK_TOKEN`); gVisor, `/tmp` executable, the projects folder read-write (the only service with it writable), its records on the named volume `project-worker-state` at `/var/lib/project-worker` (`CHECK_HISTORY_DIR`), mounted by no other service and kept by `ap down`; finds `engine` through `extra_hosts`; cannot reach the gateway |

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
| `checks` | `10.231.2.0/24` (others from `10.231.2.128/25`) | `engine` `10.231.2.2` | `check`, `project-worker` |
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

`ap build-workers <slice> <project>` (research D29, D30, D31) reads the steps and their writable
entries from `src/catalogue/api-feature-go/pattern.yaml` and the Compose project name from
`compose.yaml`, then: refuses a taken project folder (below) before it asks the engine anything;
reads every page of `listRuns` with the session's client token and refuses with exit 4
(`RunActive`) while a run of the slice has not ended, changing nothing (an answer that is missing,
not 200 or cannot be read is exit 1; research D32); refuses with exit 1 a container of a wanted name
labelled with another project; writes the slice's grants and registers each step's endpoint
`http://<compose project>-worker-<slice>-<step>-<p>:8080` with the engine (`<p>` the first 12 hex
digits of the SHA-256 of the project name, so the name fits one 63-character DNS label;
`registerWorker`, sent by a one-off busybox `nc` container on the `agents` network with the request
on standard input, so no token is on a command line, as the engine reads are; a 404 stops it with exit 4 before any folder or container is made; the grant files are already written by then); makes the
project folder, an empty `.git`, `review/` and the folders of each step's entries (`dir/**` its
folder, an exact path its folder); and starts one container per step with `docker compose run -d` from
`step-agent` or `procedure-worker`. Each container mounts only its project, at
`/projects/<project>`: read-only, with only its step's folders writable (contract-check `api/` and
`features/`, tests `tests/`, code `internal/` and `cmd/`, review `review/`, for its witness tests
(research D35)), except generate, whose whole project root is writable because its `go.mod` and
`go.sum` are in it (a stated limit), with `review/` bound read-only on top (owner, 2026-11-14);
`.git` is bound read-only on top in every container, so only the review container writes
`review/`. The container holds only its call token; its worker
token, used only by the launcher to register it, stays in
`${XDG_STATE_HOME:-~/.local/state}/agentic-platform/workers/<slice>` (mode 600) for the session.
A container carries a label of what it was started with, so one that is running with the same
settings is left alone and any other of the same project is replaced: running it twice changes
nothing. It is also labelled `org.agentic-platform.slice`, `org.agentic-platform.step` and
`org.agentic-platform.project` with the slice, step and project in full. Each project has its own
containers ([ADR 0036](../../docs/decisions/0036-step-containers-per-project-resume-reuses-them.md)):
building another project of the slice leaves them running, and they stay up until `ap down`. An existing
project folder that holds a file or symbolic link at any depth, or anything in `.git`, is refused
with exit 3 before anything is granted, registered, made or started (the engine's `ProjectExists`
rule); one of empty folders only, such as the skeleton it made, is accepted. `ap down` removes the workers, their grants and their tokens. It needs built
agent and check images that `ap up` verified.

`ap resume <run>` reads the run with `getRun` (client token) and never runs `build-workers`. For an
interrupted run, every step's container of the run's slice and project must be running with the
session's tokens; it registers each again and passes the command on. When one is missing or
stopped, or the slice's tokens file is gone, it exits 3 naming `ap build-workers --resume <run>`
and changes nothing. A run in another state is passed on for the engine to
refuse. `ap replay <run> <project>` reads the run's slice with `getRun`, runs `build-workers` for it
into the new project, then passes the command on. For both, an unknown run is exit 4 (`NotFound`),
and an answer that is missing, not 200 or cannot be read, or lacks the slice ID (for resume also the
project name or status), is exit 1.

`ap trials <slice> <prefix> [--trials <k>]` (feature 012) runs `build-workers <slice>
<prefix>-<i>` for each `<i>` from 1 to `<k>`, in that order, then passes the command on to the
client unchanged. `<k>` defaults to the engine contract's `x-limits.trials-default` and may be 1 to
`x-limits.trials-max`, both read from this repository's copy of the contract; a `<k>` outside them,
a malformed slice ID or a prefix that does not make a project name for every trial (so at most 61
characters) is exit 3 before anything changes. The first `build-workers` that fails ends it with
that exit code and starts no set; the projects already made stay.

`ap prove <captured pattern> <project>` (feature 011) proves a captured pattern: it runs the
client's `proof-slice <captured pattern>`, which prints only the proof slice's ID (made from the
captured pattern's copy, or restored to it), then does what `ap build <proof slice> <project>`
does: `build-workers` for the proof slice into the new project, then the client's `build`, which
prints the proof's outcome after the run's end line (`Proof of <id>: passed`, `failed: ...` or
`running`) and exits 0 for a passed proof and 1 for a failed one. A project that is not a project
name is exit 3, and a refusal of `proof-slice` (an unknown captured pattern is exit 4, `NotFound`)
ends it with that code before anything is granted, made or started. A `fill` step's worker
container is started as for any step but never called: the engine fills that step itself.

`ap build-workers --resume <run>` (T053j, FR-020) rebuilds an interrupted run's step workers after
`ap down` or a host reboot. It reads the run with `getRun`; the slice and project come only from the
run, never the command line. A run that is not `interrupted` is exit 4 (`RunNotInterrupted`), an
unknown run exit 4 (`NotFound`), and another run of the slice that has not ended exit 4
(`RunActive`). It accepts only that run's existing folder, which must hold `.git` and every step's
folder, each a folder and no symbolic link (else exit 3, naming it), and creates, changes or
removes nothing in it. It then makes the slice's tokens if they are gone, writes the grants, starts
the project's step containers with the same names, labels and mounts as `build-workers` and
registers them; `.git` stays read-only in every container, so the project worker's history guard
still sees a change made while the platform was down. Running it again changes nothing. An engine
answer that is missing, not 200 or cannot be read is exit 1, changing nothing.

To read the resolved configuration without starting anything, set the variables and run
`docker compose -f src/deploy/compose.yaml --profile '*' config`.
