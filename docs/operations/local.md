---
type: Environment
title: Local environment
description: The platform on one developer's machine, started by the ap launcher with Docker Compose and gVisor.
tags: [operations, deployment, local]
status: draft
resource: /src/deploy/compose.yaml
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-15T12:00:00Z }
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
- The terminal client, run per command. The test harness is no service (see Deployment).
- The step agent workers and the check service (the project folder read-only, the Go toolchain, the pinned oapi-codegen and the template's module cache in its image, so it runs offline), under gVisor with CPU, memory and process limits; the check service answers only the engine, with its own session token, and `ap up` starts it with the engine. The step workers are started per slice and project by `ap build-workers <slice> <project>` (run by `ap build` first): one container per step of the pattern from the `step-agent` or `procedure-worker` template, each mounting only its project, read-only except its step's writable folders (the whole root for generate) and with `.git` always read-only; each gets its own call token, its grant is a file in `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/grants` (owned by the engine's user, mounted only by the engine, read-only; research D31), and the launcher registers it with the engine. Each project has its own step containers, named `agentic-platform-worker-<slice>-<step>-<p>` (`<p>` the first 12 hex digits of the SHA-256 of the project name) and labelled with the slice, step and project, so building another project of the slice leaves the first one's running; `build-workers` refuses with exit 4 (`RunActive`) while a run of the slice has not ended. `ap resume <run>` registers the run's own running containers again and passes the command on; when they or the slice's tokens are gone (after `ap down` or a host reboot) it exits 3 naming `ap build-workers --resume <run>`, which rebuilds that interrupted run's containers in its existing folder, creating nothing in it; then `ap resume <run>` continues the run. `ap replay <run> <project>` builds the run's slice into a new, empty project. Running `build-workers` again changes nothing, and `ap down` removes the workers, their grants and their tokens ([ADR 0036](/decisions/0036-step-containers-per-project-resume-reuses-them.md)).
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

`src/deploy/ap test <suite>` (`planted` or `us2`) runs a T046 acceptance suite without a session,
Compose or a tests image ([ADR 0037](/decisions/0037-terminal-run-commands-exit-codes-and-test-on-host.md)).
It builds the suite's test binary in the pinned `golang` container with `--network none`,
`-tags docker`, `GOPROXY=off` and `-mod=readonly`, the module cache in
`${XDG_CACHE_HOME:-~/.cache}/agentic-platform/go` (shared with `scripts/check.sh`), then runs the
binary on the host in `src/tests/acceptance` with `-test.run`, `-test.timeout` (60 minutes for
`planted`, 75 for `us2`: a test binary has no time limit of its own) and `-test.v=test2json`. The suites
start their own labs with the developer's own Docker access, an owner-approved exception to the
test-security rule; no container holds the container engine's socket. `go tool test2json`, in the
same pinned container, converts the binary's output, and `ap` prints one line per trial, per
scenario and a total (`planted`), or one per test (`us2`), with a failing test's messages indented
below it. It exits 0 when everything passes, 1 for a failing trial or test or a suite that does not
build (the build's error on standard error), and 3 for an unknown suite.

The suites' step agents answer from synthesised recordings, the step recording sets in
`src/tests/recorded/steps/` (`happy`, `resume`, `planted/<scenario>`; research D34,
[ADR 0038](/decisions/0038-lab-runs-the-build-path-with-synthesised-step-recordings.md)). A
changed step agent definition, instruction, engine message or Docker Agent version makes them
miss; make them again, free and without a key, from their reviewed answer scripts
(`answers.json`), one suite at a time:

```sh
rm -rf src/tests/recorded/steps/<set>/mappings src/tests/recorded/steps/<set>/__files  # each set whose answers changed
sh src/tests/recorded/synth-steps.sh '^TestPlanted$'
sh src/tests/recorded/synth-steps.sh '^(TestUS2|TestUS2_7_.*|TestUS2_10_.*)$'
```

The us2 pattern names only the tests that use a step set: a lab with the scripted model and no
set fails by design. Review the new mappings against their scripts, then run `ap test planted`
and `ap test us2`, which replay them with no miss (`src/tests/recorded/README.md`).

The run commands go to the terminal client through `ap` (T055, `src/cli/contract.md`). `ap build
<slice> <project>`, `ap resume <run>` and `ap replay <run> <project>` print the run ID line, then
one line per event of the run's record from its first event, then an end line, all on standard
output, and exit by the run's state: done 0; waiting for the developer, interrupted or handed over
2; expired 5; cancelled 6; a failed run operation or a refusal 4, with the problem on standard
error; the engine lost 1. `ap run <run>` prints the record the same way and follows it while it
runs, `ap runs` lists the runs, `ap compare <run> <other>` exits 7 with a table when the runs
differ, `ap artifact <run> <hash>` writes the artifact's bytes unchanged, and `ap take-over <run>`
accepts a hand-over. Each mutating call carries one `Idempotency-Key` per invocation and is sent
again unchanged after a lost answer, at most 4 tries, so a retry never starts a second run. The
first Ctrl-C or SIGTERM while a command follows a run or an operation detaches: the work goes on,
the client says how to follow it (`run <run>`) or cancel it (`cancel <run>`) and exits 130 or 143.
`ap` passes SIGINT and SIGTERM on to the client, also when only `ap` gets them, and exits with the
client's code.
A signal after the command has its result keeps that result. Text from the store is printed with
control characters escaped.

## End-to-end build and replay

The owner's live end-to-end flow (feature 001, T053a): one slice described, approved, built and
replayed through `ap`, then the two runs and their projects compared. Run each command from the
repository root, one image build at a time. The provider key is passed only as a file path; the
signing key's password goes from its file into the environment and is never printed.

```sh
COSIGN_PASSWORD=$(cat ~/.config/agentic-platform/cosign.pass) src/deploy/build.sh <engine|cli|project|check|agent|lint>   # one at a time
src/deploy/ap up --credentials ~/.config/agentic-platform/anthropic_api_key --projects <projects folder>
printf '%s' "An in-memory notes service. Listing returns all notes in creation order. Creating a note needs non-empty text and returns it with an id starting at 1." | src/deploy/ap describe
printf '0\n1\n' | src/deploy/ap questions S-1   # the analyst's questions may differ on a live run
src/deploy/ap approve S-1 goals; src/deploy/ap approve S-1 rules; src/deploy/ap approve S-1 contract
src/deploy/ap build-workers S-1 notes
src/deploy/ap build S-1 notes </dev/null
src/deploy/ap replay RUN-1 notes-replay </dev/null
src/deploy/ap compare RUN-1 RUN-2   # exit 0 = identical, 7 = differs
git -C <projects>/notes log --format='%H %T %s'; git -C <projects>/notes-replay log --format='%H %T %s'
src/deploy/ap down
```

The replay is identical when `compare` exits 0 and the two logs list equal trees (`%T`), commit
by commit; commit IDs differ, since each commit names its own run and times. Until T088, a
replay of a run with more than one review round differs (research D36); the measured run
below found it (compare exit 7). Measured on
2026-11-15: the images, cached, 93 s; `up` 10 s; the slice 33 s; `build-workers` 8 s; `build`
419 s; `replay` 229 s; `compare` 1 s; `down` 3 s. With Haiku 5.5 the whole flow cost about
$0.01 (RUN-1 used 115,595 tokens, $0.0084).

On the host, `git status` can show a vendored `build.cmd` (from
`github.com/apapsch/go-jsonmerge/v2`) as modified. That module's own `.gitattributes` sets
`*.cmd text eol=crlf`, so the host's git compares the file with converted line endings. It is not
a defect.

Sources: [Feature 001 tasks, T053a](../../specs/001-prose-to-accepted-slice/tasks.md) (the run of 2026-11-15).

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
