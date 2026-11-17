# Tasks: Start the platform with one command, and reach its run page from another machine

**Input**: Design documents from `specs/003-one-command-start/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/terminal-up.md](contracts/terminal-up.md),
[quickstart.md](quickstart.md)

**Tests**: Required. Contract-first and Outside-in put the contract and failing tests before code;
every check proves it can fail.

**Organization**: by user story, in priority order (US1 P1; US2 and US4 P2; US3 P3); within a
story: failing tests, code, fresh review, converge.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2, US3 or US4 (spec.md)

## Rules for every task

- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; tests go in new files and are seen failing before the code. The code author
  never changes a test. An existing test, helper, fixture or stub changes only with the owner's
  approval of that change, recorded with `Tests-approved-by:`; the only approved ones are in T002.
  Every agent refuses and reports any other.
- **Protected paths**: `src/deploy/`, `scripts/` and every test file need the owner's
  `Approved-by:`; new test files need `Tests-approved-by:` too.
- **Test time**: locally only the new test files and single cases, each run under 5 minutes; no
  `scripts/check.sh` full run, Docker lab, `ap test` or e2e on the host. The full test command is
  the CI run of the pushed commit; the owner approves each push after being told which jobs it
  runs.
- **Fresh review**: each story ends with a fresh review by a read-only agent (Explore) on another
  model than the author's, in its own scratchpad worktree with the uncommitted and untracked
  files, judging the CI run; at most 3 rounds (Review converges). Then `/speckit-converge` for the
  story.
- **Gate**: `/speckit-analyze` (fresh reviewer) must report no CRITICAL or HIGH before T001, and
  again after any change to spec, plan, tasks or a design artifact.

---

## Phase 1: Setup (contract first)

- [ ] T001 Move the reviewed draft [contracts/terminal-up.md](contracts/terminal-up.md) into the authoritative terminal contract `src/cli/contract.md`: replace its `up` and `page` paragraphs (defaults, the five steps of `up`, `--listen`, `--name`, `--certificate`/`--key`, the remote lines, one `up` at a time, `down` unchanged), keep every other paragraph, then delete `specs/003-one-command-start/contracts/terminal-up.md` and point the plan's and research's links at `src/cli/contract.md`; commit alone, before any code (Contract-first)

---

## Phase 2: Foundational (owner-approved test changes)

- [ ] T002 A separate test author makes exactly the owner-approved changes (2026-11-18, plan Exceptions): `scripts/tests/fixtures/launcher/docker-stub` answers `docker buildx build ... --metadata-file <f>` by writing `<f>` as JSON `{"containerimage.digest": "sha256:<64 hex>"}` with the digest of `STUB_BUILD_DIGEST_<NAME>` when set, else a fixed digest per image name, and logs the call as any other; `scripts/tests/test_launcher.py`'s `setUp` also writes `built.env` (`AP_BUILT_<NAME>=sha256:...`, mode 600) with the stub's fixed digests, so `ap up` finds every image current; its `["up"]` case in `test_unknown_own_option_is_usage_error` moves to a case where the default key file is missing (still exit 3, no `compose up` call). Every existing test in `scripts/tests/` still passes against today's `src/deploy/ap` (run `test_launcher.py` and the files that use the stub); commit with `Approved-by:` and `Tests-approved-by:`

**Checkpoint**: contract and test harness ready; stories may start.

---

## Phase 3: User Story 1 - One command takes a fresh clone to a started platform (Priority: P1) 🎯 MVP

**Goal**: `ap up` builds and signs only what changed, then verifies and starts (FR-001 to FR-003,
FR-007, FR-008, FR-010, FR-011; research R1, R2, R4).

**Independent Test**: on a machine with the images built once, a second `ap up` builds nothing and
asks nothing; after a change to the page's code, `ap up` builds `page` only.

### Tests for User Story 1

- [ ] T003 [P] [US1] Test author: new file `scripts/tests/test_build_changed.py` (real BuildKit and the local registry under a test `AP_REGISTRY_NAME`, a test signing key, `XDG_*` in a temporary folder; skipped with a reason when Docker is absent; runs in CI's `py_tests` job) for `src/deploy/build.sh --changed <name>...`: (a) after `build.sh cli`, `built.env` holds `AP_BUILT_CLI=sha256:<64 hex>` (mode 600) and `images.env` still holds only its `AP_IMAGE_*` lines; (b) `--changed cli` with nothing changed pushes, signs and records nothing, asks for no password and prints nothing about building; (c) after a change to a file under `src/cli/` that changes the binary, `--changed cli page` prints `building cli` before it builds, then builds, signs and records `cli` only; (d) an image with no `AP_BUILT_*` line is built; (e) two images to sign from a terminal (pty) with `COSIGN_PASSWORD` unset: one password prompt in all, never echoed, and the password in no file under `XDG_*`; (f) no terminal and no `COSIGN_PASSWORD` with an image to sign: exit 1 naming the images it would sign, nothing signed or recorded; (g) a failing build: exit 1 naming the image, the images built before it kept. Show each case failing against today's build.sh
- [ ] T004 [P] [US1] Test author: new file `scripts/tests/test_ap_up_builds.py` (the launcher's docker stub and harness, as changed by T002; `build.sh` replaced by a recording stub through a test-only seam the test documents, or observed through its docker calls) for `src/deploy/ap up`: it runs `build.sh --changed` with every image it starts (`engine cli page agent check project lint`, as recorded) before the first verify call; with every image current it prints `up: already started; nothing changed` on a started session and `up: started` otherwise, with the run page's address; a failing `build.sh --changed` makes `up` exit 1 with no `compose up` call; two `ap up` at once: the second waits for the first's lock and then changes nothing; a lock left by a dead process is taken over. Show each case failing against today's ap

### Implementation for User Story 1

- [ ] T005 [US1] Code author: `src/deploy/build.sh` builds every image with `--build-arg SOURCE_DATE_EPOCH=0` and `rewrite-timestamp=true`; records `AP_BUILT_<NAME>=<candidate digest>` in `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/built.env` under the `images.env.lock`, replacing only that name's line; and gains `--changed <name>...`: candidate builds first (`--sbom=false --provenance=false`, output to the local image store), the list of images whose candidate digest differs from `AP_BUILT_<NAME>` or whose `AP_IMAGE_<NAME>` is missing, `building <names>` on stderr, one password prompt without echo when that list is not empty and `COSIGN_PASSWORD` is unset (twice when it makes the key pair), the password exported only to that run's cosign calls, then today's build, push, sign and record for each, in order; a plain `build.sh <name>` keeps its behaviour (research R1, R2; data model, Image fingerprint). Make T003 pass without changing it
- [ ] T006 [US1] Code author: `src/deploy/ap up` takes the start lock (a `mkdir` lock under the state folder holding its PID, the stale-lock rule of build.sh), runs `build.sh --changed` with every image it starts after the prerequisite checks and before verification, and prints `up: started` / `up: already started; nothing changed` and the page address (contract; research R4). Make T004 and T002's tests pass without changing them
- [ ] T007 [US1] Push (after telling the owner which jobs it runs) and fix until the CI run is green; then a fresh read-only review of US1 (rules above) against the spec, research R1, R2, R4 and the contract; findings fixed or recorded as tasks
- [ ] T008 [US1] `/speckit-converge` for US1 (appends tasks only)

**Checkpoint**: US1 works on its own: a second start builds nothing.

---

## Phase 4: User Story 2 - Missing prerequisites are named, with the fix (Priority: P2)

**Goal**: defaults for the key file and projects folder, and every prerequisite checked first
(FR-004 to FR-006; research R3).

**Independent Test**: remove each prerequisite in turn; `ap up` names it and its fix and starts
nothing.

### Tests for User Story 2

- [ ] T009 [P] [US2] Test author: new file `scripts/tests/test_ap_up_defaults.py` (the launcher's docker stub and harness) for `ap up`: with no options it uses `${XDG_CONFIG_HOME}/agentic-platform/anthropic_api_key` and `$HOME/projects`; a missing default projects folder is made with mode 755 and `up` goes on; a missing `--projects` folder is refused (exit 3) and not made; a projects folder with mode 700 (no `o+rx`) is refused, exit 3, the message naming the folder and `700`; a missing default key file: exit 3 naming the path it looked at and `--credentials`; a Docker without `runsc` in `docker info`'s runtimes: exit 3 naming gVisor and `docs/guides/install-gvisor.md`; Docker 27: exit 3 as today; in every refusal no build, verify or `compose up` call was made and the key is in no output. Show each case failing against today's ap

### Implementation for User Story 2

- [ ] T010 [US2] Code author: `src/deploy/ap up`: options optional with the contract's defaults; the checks in the contract's order before the lock's build step (Docker version, `runsc` registered, key file, projects folder: make the default with mode 755 when missing, refuse a folder without `o+rx` naming its mode); each failure exit 3 naming its fix (research R3). Make T009 pass without changing it
- [ ] T011 [US2] Push (owner told first), CI green, fresh read-only review of US2, findings fixed or recorded; then `/speckit-converge` for US2

**Checkpoint**: US1 and US2 work; a fresh clone needs only `ap up`.

---

## Phase 5: User Story 4 - Reach the run page from another machine (Priority: P2)

**Goal**: `ap up --listen` serves the run page to other machines over TLS behind a sign-in secret,
rate-limited, limited to the given names, recorded (FR-012 to FR-017; research R5; ADR 0062).

**Independent Test**: on a VM, `ap up --listen 0.0.0.0`; from another machine 401 without the
secret, 200 with it, 400 over plain HTTP; after `down` and `up` the port no longer answers.

### Tests for User Story 4

- [ ] T012 [P] [US4] Test author: new file `scripts/tests/test_page_remote.py` (CI; the pinned `nginx` image from `src/deploy/images.lock`, a certificate made as research R5 says, a stand-in page container on an internal network) for `src/deploy/page-proxy/nginx-remote.conf` with a `names.conf` for `127.0.0.1 localhost vm.test`: 200 with `ap:<secret>`, and the stand-in saw Host `127.0.0.1:<port>`; 401 without or with a wrong secret and no stand-in content; 400 for plain HTTP; 421 for Host `other.test`; after a burst of 20 wrong requests, 429; the forwarder's log names the client address for a failed sign-in and holds no secret tried; and for `src/deploy/compose.remote.yaml` merged after `compose.yaml` (`docker compose config`): only `page-proxy` changes, it publishes one port on `AP_PAGE_ADDRESS` (no `127.0.0.1` port left: `!override`), runs as `AP_HOST_USER`, mounts `AP_PAGE_CERT_DIR` read-only at the path the config reads, and keeps read-only root, dropped capabilities and its networks. Show each failing before the files exist
- [ ] T013 [P] [US4] Test author: new file `scripts/tests/test_ap_up_remote.py` (the launcher's docker stub; the certificate and hash containers answered by the stub) for `ap up --listen`: an address that is not an IPv4 or IPv6 address, a `--name` with any character outside the data model's pattern (for example `a;b`, `a b`, `{`), `--name` or `--certificate` without `--listen`, `--certificate` without `--key`: exit 3, nothing started; with `--listen 0.0.0.0` Compose gets `compose.remote.yaml` after `compose.yaml` and `AP_PAGE_ADDRESS=0.0.0.0`; `session.env` gains `AP_PAGE_SECRET` (64 lowercase hex), `AP_PAGE_NAMES` and `AP_PAGE_CERT_DIR`; `page-remote/` holds `htpasswd` starting `ap:$6$` and `names.conf` with `server_name` and the given names only, all mode 600; a second `up` keeps the secret; `up` after `down` makes a new one; a running remote session and `up` with another `--listen` or `--name`: exit 3 telling to run `down`; `access.log` gains one `on` line with address, port, names and `sha256=`, and one `off` line at the first start without `--listen` after it, and never the secret; stdout and stderr show the secret only in the `sign in as ap with` line; `ap page` prints the https names, the secret and the fingerprint; without `--listen` nothing of this happens and the page is published on `127.0.0.1` as today. Show each failing against today's ap

### Implementation for User Story 4

- [ ] T014 [P] [US4] Code author: `src/deploy/page-proxy/nginx-remote.conf` (fixed: TLS 1.2/1.3 from `/etc/ap/remote/cert.pem` and `key.pem`; a `default_server` answering 421; the named server includes `/etc/ap/remote/names.conf`; `auth_basic` with `/etc/ap/remote/htpasswd`; `limit_req_zone $binary_remote_addr` 10 r/s, `burst=20 nodelay`, `limit_req_status 429`; proxies to the page with `Host` set from `names.conf`; temp paths in `/tmp`; no `return` in the protected location) and `src/deploy/compose.remote.yaml` (page-proxy only: `ports: !override` on `${AP_PAGE_ADDRESS}:${AP_PAGE_PORT}` to the TLS port, `user: ${AP_HOST_USER}`, the remote config and `${AP_PAGE_CERT_DIR}` mounted read-only). Make T012 pass without changing it
- [ ] T015 [US4] Code author: `src/deploy/ap`: parse and validate `--listen`, `--name`, `--certificate`, `--key` (data model's patterns; refuse a change on a running session); default names (`hostname`, `hostname -I`, `127.0.0.1`, `localhost`, or the one address); `page-remote/` (mode 700): the certificate made with `generate_cert.go` in the pinned `golang` image (`--network none`, read-only root, an executable `/tmp`, ECDSA P-256, 8760h, the names as SAN) when missing, when `cert.names` differs or when older than 358 days, or the developer's copied; the SHA-256 fingerprint computed in the pinned nginx image; the secret from `rand`, `htpasswd` from the nginx image's `mkpasswd -m sha512` (the secret on its standard input, never an argument), `names.conf`; the session variables; `compose.remote.yaml` passed to every Compose call of the session; the `access.log` lines; the remote lines of `up` and `page` (contract). Make T013 pass without changing it
- [ ] T016 [US4] Push (owner told first), CI green, fresh read-only review of US4 with the threat model, findings fixed or recorded; the owner runs quickstart section 3 on the VM (an agent never marks this as human-verified); then `/speckit-converge` for US4

**Checkpoint**: the owner reaches the run page on the VM from their computer.

---

## Phase 6: User Story 3 - What the developer types says what, not how (Priority: P3)

**Goal**: the interface and the guides name no internal part (FR-009; Principle XVIII).

**Independent Test**: the quickstart's start is one command; `ap up`'s usage names only the key,
the projects and remote access.

- [ ] T017 [P] [US3] Test author: new file `scripts/tests/test_ap_usage.py`: `src/deploy/ap` with an unknown option prints a usage whose `up` line is the contract's and names none of `image`, `build.sh`, `generate`, `sign`, `cosign`, `registry`; `README.md`'s "Run it" and `specs/001-prose-to-accepted-slice/quickstart.md`'s "Start the platform" each hold one start command and none of `build.sh`, `generate.sh`, `COSIGN_PASSWORD`, `mkdir`, `chmod`. Show it failing
- [ ] T018 [US3] Code author: the usage text in `src/deploy/ap`; the start sections of `README.md`, `specs/001-prose-to-accepted-slice/quickstart.md` and `docs/operations/local.md` (one `ap up`; `build.sh` kept only under development and CI; remote access and the Docker-bypasses-ufw note); `src/deploy/README.md` (the new variables, `compose.remote.yaml`, `built.env`, `page-remote/`, `access.log`). Make T017 pass without changing it
- [ ] T019 [US3] Push (owner told first), CI green, fresh read-only review of US3; then `/speckit-converge` for US3

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T020 Knowledge bundle: `docs/architecture/overview.md` (the one-command start; the forwarder's remote mode, ADR 0061, 0062), `docs/overview/problem-space.md` (N-005: spec 003, state), `docs/components/nginx.md` if the shipped behaviour differs from it, `docs/log.md`; `python3 scripts/okf.py index` and `check` report OK
- [ ] T021 Measure on the owner's clean VM, the owner running it: the first `ap up` from a fresh clone (time, images built), a second `ap up` (under 30 s, SC-002), `ap down && ap up`; record the figures in `quickstart.md` and research R1
- [ ] T022 `/speckit-converge` for the whole feature, then a final fresh read-only review of the feature against every FR and SC, judging the CI run of the last pushed commit (Review converges); every finding fixed or recorded; the owner decides the merge to `main`

## Dependencies & Execution Order

- T001 → T002 → every story. Stories: US1 (T003–T008) first; US2 (T009–T011) after T006, since
  both change `ap up`; US4 (T012–T016) after T010 for the same reason, though T012 and T014 (nginx
  and Compose files only) may run alongside US2; US3 (T017–T019) after US4, since it documents
  every option; Polish last.
- Within a story: tests (seen failing) → code → push and CI → fresh review → converge.
- One unit at a time on the host (small and shared): never two image builds or two Docker test
  files at once.

## Parallel Opportunities

- T003 and T004 (different new test files) by one or two test authors.
- T012 and T013 (different new test files); T014 alongside T013 (different files).
- T017 alongside T020's text that does not depend on shipped behaviour.

## Implementation Strategy

- **MVP**: Phases 1–3 (US1): a second start builds nothing, the first builds everything with one
  prompt. Then US2 makes a fresh clone need only `ap up`; US4 gives the owner the VM page; US3
  cleans the interface and guides.
- Each story is pushed and judged by its CI run before the next starts.
