# Research: Start the platform with one command, and reach its run page from another machine

Phase 0 of the [plan](plan.md) for the [spec](spec.md). Each decision names its deciding source:
a cited primary source or an experiment reproduced on the owner's host (constitution, Evidence
before action).

## Sources

| ID | Source |
|----|--------|
| S1 | Experiment, 2026-11-18, in the session scratchpad: the pinned `nginx:1.29-alpine` (images.lock `nginx`) serving TLS with a certificate from Go's `crypto/tls/generate_cert.go` (run in the pinned `golang:1.26-alpine` with `--network none`, 5.3 s), HTTP Basic authentication against a SHA-512 crypt line made by the image's own `mkpasswd -m sha512`, and `limit_req` at 10 r/s with a burst of 20. Results: with the secret 200; no secret 401; a wrong one 401; plain HTTP to the TLS port 400; an unknown Host 421 from a default server; an untrusted certificate refused by the client; 40 wrong sign-ins in a row gave 401 for the first 20, then 429; the error log names the user, client address and "password mismatch" and never the secret tried. A `return` in the location answered before authentication ran, so the protected location must proxy or serve, never `return` |
| S2 | Experiment, 2026-11-18: `docker buildx build` (Docker 29.10.0, BuildKit v0.35.0, containerd image store) of `Dockerfile.cli` with `--build-arg SOURCE_DATE_EPOCH=0`, `--provenance=false --sbom=false` and `--output type=image,rewrite-timestamp=true`, three times: cold (5.7 s), unchanged (0.8 s) and after adding an unrelated file to `src/` (1.4 s). All three reported the same `containerimage.digest`, so the image is reproducible and a rebuild of unchanged sources is cheap |
| S3 | [BuildKit: build reproducibility (`SOURCE_DATE_EPOCH`, `rewrite-timestamp`)](https://github.com/moby/buildkit/blob/master/docs/build-repro.md); [Docker: reproducible builds](https://docs.docker.com/build/ci/github-actions/reproducible-builds/) |
| S4 | [nginx `ngx_http_auth_basic_module`](https://nginx.org/en/docs/http/ngx_http_auth_basic_module.html), [`ngx_http_limit_req_module`](https://nginx.org/en/docs/http/ngx_http_limit_req_module.html), [`ngx_http_ssl_module`](https://nginx.org/en/docs/http/ngx_http_ssl_module.html) |
| S5 | [Go source: `crypto/tls/generate_cert.go`](https://go.dev/src/crypto/tls/generate_cert.go) (BSD-3-Clause, part of the Go distribution in the pinned `golang` image) |
| S6 | [Compose file: merge rules, `!override` and `!reset`](https://docs.docker.com/reference/compose-file/merge/) |
| S7 | [Docker: packet filtering and firewalls](https://docs.docker.com/engine/network/packet-filtering-firewalls/) (published ports bypass rules such as ufw's) |
| S8 | Feature 001: [research D22](../001-prose-to-accepted-slice/research.md) (nginx: BSD-2, Scorecard 7.9), [ADR 0028](../../docs/decisions/0028-sign-platform-images-locally.md) (local signing), `src/deploy/ap`, `src/deploy/build.sh`, `src/deploy/compose.yaml`, `src/deploy/page-proxy/nginx.conf`, `scripts/tests/test_launcher.py`, `scripts/tests/test_signing.py`, `scripts/tests/test_compose.py` |

## Decisions

### R1. `ap up` builds what changed, judged by a reproducible image digest

- **Decision**: before it verifies and starts the images, `ap up` runs `src/deploy/build.sh
  --changed <every image it starts>`. For each image, build.sh first makes a *candidate*: the
  image built as it always is, with `SOURCE_DATE_EPOCH=0` and `rewrite-timestamp=true`, without
  SBOM, provenance or push, into the local image store. The candidate's digest is the image's
  *fingerprint*. An image whose fingerprint equals the one recorded at its last signed build,
  and whose recorded digest is still verified, is current and left alone. Every other image is
  built, pushed with its SBOM and provenance, signed and recorded as today (ADR 0028), and its
  fingerprint recorded with it. build.sh says which images it will build before it starts and
  what it did at the end. A plain `build.sh <name>` keeps its behaviour and also records the
  fingerprint.
- **Why**: S2 shows the candidate digest stays the same for unchanged inputs and costs about a
  second when the build cache holds it, so asking BuildKit is both exact and cheap. The Go images
  all `COPY . .` from `src/`, so only the built result, not the file list, tells whether an image
  changed; Go builds are reproducible (`-trimpath`, `-buildvcs=false`, `-buildid=`) and
  `rewrite-timestamp` removes file times from the layers (S3). This uses the tool's own mechanism
  (BuildKit's cache and digests, Principle XVII) and leaves build.sh the single owner of how an
  image is built.
- **Rejected**: a hash of the input files kept by `ap` (a second home for what each Dockerfile
  reads, and every `src/` change would rebuild every Go image); `docker compose up --build`
  (Compose cannot attach the SBOM and provenance, push to the local registry or sign, so ADR 0028's
  verification by digest would break); Docker Bake (declares the build arguments again but solves
  neither the change test nor signing).
- **Stated limits**: a change to a pin used only for the SBOM or signing (`sbom-scanner`,
  `cosign`) does not change a fingerprint, so it does not rebuild by itself. An image built by
  feature 001's build.sh has no recorded fingerprint and is built again once. Each start makes
  every candidate; the time this takes with nothing changed is measured against SC-002 in the
  quickstart.

### R2. The signing password is asked once, only when something must be signed

- **Decision**: build.sh `--changed` makes every candidate first, which needs no password. Only
  when at least one image must be signed, and `COSIGN_PASSWORD` is not set, does it ask for the
  password once on the terminal, without echo (twice on the first start, when it makes the key
  pair), and passes it to every cosign call of that run through the environment only. The password
  is never written to a file. Without a terminal and without `COSIGN_PASSWORD`, it stops before
  signing anything, naming the images it would have signed.
- **Why**: FR-007 (owner, 2026-11-18). cosign already reads `COSIGN_PASSWORD` (S8, build.sh), so
  one prompt in build.sh replaces one prompt per image.

### R3. Defaults and prerequisite checks

- **Decision**: `ap up` with no options uses the key file
  `${XDG_CONFIG_HOME:-~/.config}/agentic-platform/anthropic_api_key` and the projects folder
  `~/projects`; `--credentials` and `--projects` still override them. Before it builds or starts
  anything it checks, and names each failure with its fix: Docker 28 or later (as today), the
  `runsc` runtime registered with Docker (`docker info`; the fix names the
  [install guide](../../docs/guides/install-gvisor.md)), the key file (as today: present, not
  empty, mode 600 and owned by the developer, outside the projects folder), and the projects
  folder. A missing *default* projects folder is made with mode 755; a folder given with
  `--projects` must exist, as today. A folder others cannot open and search (mode without
  `o+rx`) is refused, naming its mode (closes backlog T165).
- **Why**: FR-004 to FR-006; the default paths are the ones the quickstart already uses. Making
  only the default folder keeps a mistyped `--projects` path from silently creating a folder.

### R4. One start at a time

- **Decision**: `ap up` holds a lock (a directory made with `mkdir`, holding its process ID, with
  the stale-lock rule build.sh already uses) from its prerequisite checks until Compose has
  started; a second `ap up` waits for it, then finds nothing to do.
- **Why**: Edge Cases (two starts at once); the same mechanism build.sh has proven (S8,
  `test_signing.py` lock tests).

### R5. Remote access: the page forwarder terminates TLS and asks for a sign-in secret

- **Decision**: `ap up --listen <address>` (`0.0.0.0`, `::`, or one address of the machine), with
  `--name <host>` repeatable, turns remote access on. Without `--listen` nothing changes. With
  it, `ap up` adds the Compose file `src/deploy/compose.remote.yaml`, which changes only the page
  forwarder: it publishes the page's port on the given address (`ports: !override`, S6), serves
  TLS from a certificate and key, asks for HTTP Basic authentication against a password file,
  rate-limits by source address, answers only the names given, and runs as the developer so it
  can read those files (as the gateway does). Its nginx configuration is a second fixed file,
  `src/deploy/page-proxy/nginx-remote.conf`, which includes one data file `ap` writes: the
  `server_name` list and the page's own Host. Every name and address is checked by `ap` against
  a hostname or IP pattern before it is written, so nothing from the command line becomes nginx
  syntax.
  - **Certificate**: made by `ap` when missing, when the names change or when it is older than
    358 days, with Go's `generate_cert.go` (S5) in the pinned `golang` image, `--network none`,
    ECDSA P-256, valid 365 days, its SAN the names and addresses; or the developer's own
    (`--certificate <cert> --key <key>`). `ap` prints its SHA-256 fingerprint, computed in the
    pinned nginx image (busybox `base64` and `sha256sum`).
  - **Sign-in**: the user `ap` and a random secret of 64 hexadecimal characters (`ap`'s token generator, 256 bits from `/dev/urandom`) made at each new session (not
    by a second `up`), shown by `ap up` and `ap page`, kept only in the session file (mode 600)
    and, as a SHA-512 crypt line made by the nginx image's `mkpasswd`, in the password file.
  - **Default names**: with `--listen 0.0.0.0` or `::` and no `--name`, the machine's host name
    and the addresses `hostname -I` lists, plus `127.0.0.1` and `localhost`; with one address,
    that address. Any other Host gets 421 from a default server (S1).
  - **Rate limit**: 10 requests per second per source address with a burst of 20, answered 429
    beyond it, declared once in `nginx-remote.conf` (S1, S4).
  - **Record**: `ap up` appends one line to
    `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/access.log` (time, address, port, names,
    certificate fingerprint, and whether access was turned on or off), prints the same, and
    never writes the secret there. Failed sign-ins are in the forwarder's log (`docker compose
    logs page-proxy`) with time and client address, never the secret (S1).
- **Why**: FR-012 to FR-017; Principle XI (encrypted to every client), V (authenticated),
  XIV (explicit, audited, rate-limited). S1 proves every promise on the pinned images. It reuses
  only components already adopted and pinned (nginx, the Go toolchain image), so it adds no new
  dependency (Principle VI, Stack: reuse an existing stack that fits).
- **Rejected**: binding the existing forwarder to `0.0.0.0` (plain HTTP and no sign-in: run
  content readable by anyone on the network); Caddy (automatic TLS and Basic authentication, but
  a new component and pin for what the pinned nginx already does); an OAuth or OIDC proxy (needs
  an identity provider, beyond one developer); a VPN or Tailscale (outside the platform; the SSH
  tunnel stays available); the page checking several Hosts itself (a Go change for what nginx's
  `server_name` already does; in remote mode the forwarder passes the page its expected Host after
  checking the name, since TLS and the secret are the guard against DNS rebinding there).
- **Stated limits**: the secret is shared by whoever the developer gives it to; there are no
  accounts. A browser warns about the platform's own certificate until the developer accepts it
  after comparing the fingerprint. Docker's published ports bypass host firewall rules such as
  ufw's (S7), so the port is open on every address the developer chose; a cloud firewall is the
  developer's. Locally the platform's own containers still talk plain HTTP on internal networks
  (feature 001's approved partial XI exception).

### R6. Contracts and tests

- **Decision**: the terminal contract `src/cli/contract.md` (the authoritative copy) gains the
  defaults, `--listen`, `--name`, `--certificate`, `--key`, the remote lines of `ap up` and `ap
  page`, and the build lines; the draft is [contracts/terminal-up.md](contracts/terminal-up.md),
  merged into `src/cli/contract.md` before any code. New tests go in new files by a separate
  author: `scripts/tests/test_ap_up_defaults.py` (defaults and prerequisites, docker stub),
  `scripts/tests/test_build_changed.py` (fingerprints, one prompt, nothing rebuilt; real
  BuildKit, CI), `scripts/tests/test_page_remote.py` (the forwarder's TLS, sign-in, names and
  rate limit on the pinned nginx, as in S1, CI).
- **Existing tests that change, each only with the owner's approval first**: the launcher's
  docker stub (`scripts/tests/fixtures/launcher/docker-stub`) and `test_launcher.py`'s set-up
  must answer the new candidate builds as unchanged, so that `ap up` goes on to verify and start
  as these tests expect; `test_launcher.py`'s `["up"]` usage case becomes a missing-key case
  (still exit 3, nothing started). No other existing test is expected to change; any other that
  must is reported to the owner first.
- **Why**: Contract-first; Existing tests change only with the owner's approval.

## Principle VI evaluation

| Need | Project | Licence | Scorecard | Status |
|------|---------|---------|-----------|--------|
| TLS, Basic authentication, rate limit, name check | nginx (already adopted, pinned) | BSD-2 | 7.9 (D22, feature 001) | reused |
| Self-signed certificate | Go `crypto/tls/generate_cert.go` in the pinned Go image | BSD-3-Clause | — (the platform's stack, ADR 0057) | reused |
| Password hash | busybox `mkpasswd` in the pinned nginx image | GPL-2.0, run as a tool, not linked | — | reused |
| Change detection | BuildKit (Docker's builder, already required) | Apache-2.0 | — | reused |
| Alternatives | Caddy (Apache-2.0), oauth2-proxy (MIT), Docker Bake | — | not evaluated further: each adds a component the reused ones make unnecessary | rejected |

No custom code replaces an open-source capability: `ap` and build.sh only call these tools.
