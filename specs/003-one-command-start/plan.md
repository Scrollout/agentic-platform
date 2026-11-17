# Implementation Plan: Start the platform with one command, and reach its run page from another machine

**Branch**: `003-one-command-start` | **Date**: 2026-11-18 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/003-one-command-start/spec.md`

## Summary

A new launcher command, `ap start`, takes a machine from any state to a started platform: it
checks the prerequisites first, builds and signs only the images whose result changed (judged by a
reproducible candidate build, research R1), asks for the signing password once and only when it
must sign (R2), uses default paths for the key file and projects folder (R3), and then runs `ap
up`, which verifies and starts as feature 001 does. `ap start --check` shows what it would do and
changes nothing (R7). `ap up` itself is unchanged, for CI and the test suites (owner, option B,
after `/speckit-analyze` round 1). With `--listen`, which `up` gains and `start` passes on, the
run page's forwarder, the pinned nginx, also serves the page to other machines over TLS behind a
sign-in secret, rate-limited and limited to the given names, and the choice is recorded (R5).
Nothing else in the platform changes.

## Technical Context

**Language/Version**: POSIX sh (`src/deploy/ap`) and bash (`src/deploy/build.sh`), the stacks
these files already use; no Go change

**Primary Dependencies**: already adopted and pinned only: Docker 28+ with BuildKit and Compose,
nginx (`nginx` pin), the Go toolchain image (`golang` pin, for `crypto/tls/generate_cert.go`),
cosign (`cosign` pin)

**Storage**: the launcher's local state folder: `built.env`, `page-remote/`, `access.log`, and the
extended `session.env` ([data model](data-model.md))

**Testing**: Python `unittest` in `scripts/tests/` (the launcher's existing harness and docker
stub; real BuildKit and nginx for the build and remote cases, in CI's `py_tests` job); the
existing `e2e`, `docker`, `us2` and `planted` jobs prove nothing else changed

**Target Platform**: Linux with Docker 28+, Compose and gVisor; a local machine or a VM

**Project Type**: the local multi-container tool of feature 001; this feature changes its launcher
and the page forwarder's packaging

**Performance Goals**: a start with nothing changed in under 30 s (SC-002); the first start's time
is measured and recorded

**Constraints**: no new image or pin; the password never stored; no plain-HTTP or unauthenticated
path to run content from another machine; rate limit 10 r/s per address, burst 20

**Scale/Scope**: one developer, one machine, one sign-in secret

## Constitution Check

*Evaluated before research and again after design: PASS. Feature 001's approved exceptions (II,
V partial, XI partial, XII, XIII partial, XVI partial) still apply to the platform as a whole;
this feature adds none.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Needs before solutions | Pass | The spec serves N-005; no design is taken from another project |
| II. Two planes | Pass (feature 001's exception unchanged) | Only the launcher and the page forwarder change |
| III. API-first | Pass | No management path changes; the page stays a GET-only client of the engine API |
| IV. Asynchronous operations | Pass | No engine operation changes |
| V. Zero-trust identity | Pass | A remote viewer must present the sign-in secret, new per session; the forwarder accepts no request without it (R5) |
| VI. Adopt mature OSS | Pass | Only already adopted components (nginx, BuildKit, the Go toolchain, cosign); no custom code replaces an OSS capability (research, Principle VI evaluation) |
| VII. Pluggable providers | Pass | Unchanged |
| VIII. Containers | Pass | The remote forwarder runs non-root (the developer's user, as the gateway), read-only root, no capabilities; certificate and hash tools run in pinned containers with no network; images stay signed and verified |
| IX. Knowledge stays true | Pass | ADR 0061 and 0062, the nginx component, a BuildKit and nginx reference, the operations guide, the log |
| X. No silent loss | Pass | `up` keeps run records and the session's runs; builds happen before anything is stopped or started |
| XI. Agent data privacy | Pass | Every connection from another machine is TLS; the secret never reaches a log or the record; local traffic keeps feature 001's partial exception |
| XII. Tenant isolation | Not applicable | One developer |
| XIII. Secure supply chain | Pass | No new pin; images built from pinned sources, signed, with SBOM and provenance, as before |
| XIV. Safe defaults | Pass | Remote access off by default, an explicit and recorded choice when on; rate-limited; a failed check stops `up` before anything starts |
| XV. Compatible public APIs | Pass | The terminal contract grows additively ([terminal contract](../../src/cli/contract.md#starting-the-platform)); every existing command line keeps its meaning |
| XVI. Human control | Pass (feature 001's exception unchanged) | `src/deploy/`, `scripts/` and tests are protected paths needing the owner's `Approved-by:` |
| XVII. Declare, derive, converge | Pass | `start` converges images and the platform in one command, safe to repeat, with no order of steps to know, and `start --check` shows the difference without changing anything (R7); build.sh remains the one engine for an image, `lock.sh` the one lock rule |
| XVIII. Abstractions say what | Pass | `start`'s options are about the key, the projects and remote access; images, signing and generation leave the developer's interface; `up` stays as the lower command CI and tests use |
| XIX. Evals | Not affected | No agent, prompt, model or tool changes |
| XX. Deterministic acceptance | Not affected | No check of agent work changes |
| Contract-first | Pass | The terminal contract changes first (R6) |
| Existing tests change only with approval | Pass | No existing test changes (R6); new stub behaviour goes in a new fixture |
| Fresh review | Pass | A fresh read-only reviewer per story and before done, judging the CI run |
| Layout, Clean tree | Pass | New files under `src/deploy/` and `scripts/tests/`; no generated or local state committed |

## Decisions

Each decision that outlives this feature has a record in `docs/decisions/`, linked back here.

| Decision | Research | Record |
|----------|----------|--------|
| `ap up` builds, signs and records only the images whose reproducible candidate build (`SOURCE_DATE_EPOCH=0`, `rewrite-timestamp=true`) differs from the fingerprint recorded at their last signed build, asking for the signing password once and only when it must sign | R1, R2 | [ADR 0061](../../docs/decisions/0061-up-builds-only-changed-images-by-reproducible-digest.md) (superseded in part by ADR 0063) |
| The start command is a new `ap start` that checks, builds what changed and then runs `ap up` unchanged; `ap start --check` shows what it would do; `ap up` stays for CI and the tests (owner, option B, 2026-11-18) | R1, R4, R7 | [ADR 0063](../../docs/decisions/0063-ap-start-is-the-start-command-ap-up-unchanged.md) |
| The run page is reachable from other machines only with `ap up --listen`: the pinned nginx forwarder serves it over TLS (a certificate the platform makes with Go's `generate_cert.go`, or the developer's), behind HTTP Basic authentication with a secret new per session, rate-limited per address and limited to the given names; each change is recorded in `access.log` | R5 | [ADR 0062](../../docs/decisions/0062-run-page-reached-remotely-over-tls-behind-a-sign-in-secret.md) |

## Threat model

New and changed elements: the launcher's build step and password prompt; the signing key and its
password; the fingerprint record; the remote page forwarder (a new external interface and trust
boundary: other machines' networks); the certificate and key; the sign-in secret and its hash; the
access record. No model, tool, memory or agent context changes, so no new prompt-injection path.

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| Password prompt (build.sh `--changed`, run by `ap start`) | | | | ✓ | | | LLM02 sensitive information disclosure | Read without echo, passed to cosign only through the environment, never written or logged; asked only when signing is needed (R2) |
| Fingerprint record `built.env` | | ✓ | | | | ✓ | LLM03 supply chain | It only decides whether to rebuild; every started image is still verified against the public key by digest (ADR 0028), so a forged fingerprint can only skip a rebuild of a correctly signed image, never start an unsigned one; mode 600 in the private state folder |
| Remote page forwarder | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | LLM02 sensitive information disclosure, LLM10 unbounded consumption, ASI03 identity and privilege abuse | Off by default; TLS 1.2/1.3 only, plain HTTP gets no content; Basic authentication on every request; unknown Host 421; 10 r/s per address, burst 20; failed sign-ins logged with time and address, never the secret; fixed configuration with one data file whose names `ap` validates; non-root, read-only root, no capabilities; it reaches only the page on the internal network, the page is GET-only (S1, R5) |
| Certificate and key | ✓ | ✓ | | ✓ | | | ASI03 | Made with no network in the pinned Go image, mode 600, served only by the forwarder; the fingerprint printed for the developer to compare; renewed before it expires; or the developer's own |
| Sign-in secret | ✓ | | | ✓ | | | ASI03, LLM02 | 256 bits from `/dev/urandom`; shown only on the developer's terminal; kept in the session file (mode 600) and as a SHA-512 crypt hash; new after every `down`; a stated limit: whoever the developer gives it to can read the runs |
| Access record `access.log` | | ✓ | ✓ | | | | — | Append-only by `ap`, one line per change of remote access, never the secret; on the developer's own machine, so it records rather than proves (as feature 001's local hooks) |
| Published port on other addresses | | | | ✓ | ✓ | | LLM10 | Docker's published ports bypass host firewall rules such as ufw's (S7): stated in the operations guide; only the forwarder publishes, and only with `--listen` |

## Project Structure

### Documentation (this feature)

```text
specs/003-one-command-start/
├── plan.md              # this file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
src/
├── cli/contract.md                    # the terminal contract: up, page, down (authoritative)
└── deploy/
    ├── ap                             # start (new): lock, prerequisites, defaults, build step, --check, then up; up: remote options
    ├── build.sh                       # --changed [--check]: candidate builds, one password prompt, fingerprints
    ├── lock.sh                        # new: the one mkdir lock with its stale-lock rule, sourced by ap and build.sh
    ├── Dockerfile.check               # the template, module cache, oapi-codegen and warm cache in their own stage
    ├── compose.yaml                   # unchanged
    ├── compose.remote.yaml            # new: the page forwarder's remote settings (ports !override, files, user)
    ├── page-proxy/nginx.conf          # unchanged (local)
    ├── page-proxy/nginx-remote.conf   # new: TLS, Basic authentication, limit_req, server_name include
    └── README.md                      # variables and files
scripts/tests/
├── test_build_changed.py              # new: fingerprints, one prompt, nothing rebuilt (real BuildKit)
├── test_ap_start.py                   # new: start runs the build step then up, lock, messages, --check (new stub)
├── test_ap_start_defaults.py          # new: defaults and prerequisites (new stub)
├── fixtures/start/docker-stub         # new: the launcher stub's behaviour plus info, buildx and the remote tools
├── test_page_remote.py                # new: TLS, sign-in, names, rate limit (pinned nginx), the override
├── test_ap_up_remote.py               # new: --listen, secret, names, record (new stub)
└── test_ap_usage.py                   # new: usage and guides name no internal part
docs/operations/local.md               # start, remote access, firewall note
```

**Structure Decision**: the feature lives in the launcher's packaging under `src/deploy/` and its
tests under `scripts/tests/`, as feature 001's launcher does. No product code under `src/engine`,
`src/page` or `src/cli` changes except the contract text.

## Exceptions

None. The owner's approval of 2026-11-18 for changes to the launcher's docker stub and
`test_launcher.py` is not used: with `ap start` (owner, option B), `ap up` and its tests stay as
they are (research R6).

## Complexity Tracking

No Core Principle violation.
