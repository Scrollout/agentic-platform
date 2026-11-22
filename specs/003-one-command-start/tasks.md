# Tasks: Start the platform with one command, and reach its run page from another machine

**Input**: Design documents from `specs/003-one-command-start/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [terminal contract](../../src/cli/contract.md#starting-the-platform),
[quickstart.md](quickstart.md)

**Tests**: Required. Contract-first and Outside-in put the contract and failing tests before code;
every check proves it can fail.

**Organization**: by user story, in priority order (US1 P1; US2 and US4 P2; US3 P3); within a
story: failing tests, code, fresh review, converge. Revised on 2026-11-18 after
`/speckit-analyze` round 1: the start command is `ap start` and `ap up` stays unchanged (owner,
option B; ADR 0063).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2, US3 or US4 (spec.md)

## Rules for every task

- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; tests go in new files and are seen failing before the code. The code author
  never changes a test. No existing test, helper, fixture or stub changes in this feature
  (research R6); an agent that finds one must change refuses and reports it to the owner, who
  approves that change first (`Tests-approved-by:`).
- **Protected paths**: `src/deploy/`, `scripts/` and every test file need the owner's
  `Approved-by:`; a commit that modifies an existing test file would also need
  `Tests-approved-by:`.
- **Test time**: locally only the new test files and single cases, each run under 5 minutes; no
  `scripts/check.sh` full run, Docker lab, `ap test`, e2e or image build on the host. The full
  test command is the CI run of the pushed commit; the owner approves each push after being told
  which jobs it runs.
- **Fresh review**: each story ends with a fresh review by a read-only agent (Explore) on another
  model than the author's, in its own scratchpad worktree with the uncommitted and untracked
  files, judging the CI run; at most 3 rounds (Review converges). Then `/speckit-converge` for the
  story.
- **Gate**: `/speckit-analyze` (fresh reviewer) must report no CRITICAL or HIGH before T001, and
  again after any change to spec, plan, tasks or a design artifact. Passed on 2026-11-18 in round 3 (fresh Sonnet reviewer; round 1: 2 CRITICAL, 3 HIGH, fixed with the owner's option B; round 2: 3 LOW, fixed; round 3: 1 LOW, fixed). After US1 (2026-11-18): round 4: 1 HIGH, 3 MEDIUM, 3 LOW, fixed; round 5: clean. After US2 and US4 (2026-11-18): round 6: 2 MEDIUM, 4 LOW, fixed in the contract, data model, research and tasks (FR-018 and the spec's status line unchanged: the folder line is covered by "whether it would start"; the status waits for T024). After T018 and Phase 9 (2026-11-18): round 7: 2 MEDIUM, 4 LOW, fixed in tasks only (US4's Independent Test, the order of T027 and T028, T013's and T024's notes, T027's and T028's cases); no CRITICAL or HIGH; round 8 closed all six. After T023 and Phase 10 (2026-11-18): round 9: 4 MEDIUM, 3 LOW, no CRITICAL or HIGH; fixed in research R1 (order, which images, failure), the data model, the contract's wording (T032), T029's cases, the plan and the overview (ADR 0064 supersedes ADR 0061 in part). Round 10: all seven closed; 1 LOW (R1's Order wording too strong for an edit undone within one run), fixed.

- **Known gap** (owner, 2026-11-18): FR-001's "stopped" state is not fully met while a session
  file outlives its containers (after a reboot `start` says `already started; nothing changed` and
  `start --check` exits 0); it is backlog T166, after the MVP, from US1's fresh review. Closed 2026-11-22 by T166: `start` is started only when the session's engine is running.

---

## Phase 1: Setup (contract first)

- [x] T001 Move the reviewed draft `contracts/terminal-start.md` into the authoritative terminal contract `src/cli/contract.md`: add `start` to the launcher's own commands (options, defaults, its five steps, `--check`, its lines and exit codes), add `up`'s remote-access options, refusals and lines and `page`'s remote lines, keep every other paragraph and `up`'s behaviour without the new options word for word; then delete `specs/003-one-command-start/contracts/terminal-start.md` and point every link to it (in `plan.md`, `research.md`, `data-model.md`, `quickstart.md` and `tasks.md`) at `src/cli/contract.md`; `python3 scripts/okf.py check` and the markdown hook pass; commit alone, before any code (Contract-first)

---

## Phase 2: Foundational

- [X] T002 Code author: move build.sh's lock (`take_lock`, `release_lock`, the stale-lock rule and its messages) into a new file `src/deploy/lock.sh` in POSIX sh (with `local`, which dash and bash both support) that `src/deploy/build.sh` sources, with no change in behaviour (research R4). `scripts/tests/test_signing.py`'s lock tests (`test_a_stale_lock_of_a_dead_process_is_removed_and_the_build_succeeds`, `test_a_lock_without_a_pid_yet_is_waited_for_not_removed`, `test_a_lock_of_a_running_process_is_waited_for_then_the_build_proceeds`, `test_several_builds_behind_a_dead_owners_lock_remove_it_exactly_once`) prove it unchanged in CI's `py_tests` job; no test changes
- [X] T003 [P] Test author: new fixture `scripts/tests/fixtures/start/docker-stub`, a copy of the launcher's stub (`scripts/tests/fixtures/launcher/docker-stub`, which stays unchanged) that also answers: `docker info --format ...` with the runtimes in `STUB_RUNTIMES` (default `runc runsc`) and the server version in `STUB_DOCKER_VERSION`; `docker buildx build ... --metadata-file <f>` by writing `{"containerimage.digest": "sha256:<64 hex>"}` with `STUB_BUILD_DIGEST_<NAME>` or a fixed digest per image name, and failing when `STUB_BUILD_FAIL` names the image; `docker run` of the pinned `golang` image running `generate_cert.go` by writing `cert.pem` and `key.pem` into the mounted folder; `docker run` of the pinned `nginx` image running `mkpasswd` by printing a `$6$` line, and running the fingerprint command by printing 64 hex; it logs every call as the launcher's stub does

**Checkpoint**: contract, lock helper and test stub ready; stories may start.

---

## Phase 3: User Story 1 - One command takes a fresh clone to a started platform (Priority: P1) 🎯 MVP

**Goal**: `ap start` builds and signs only what changed, then runs `ap up` (FR-001 to FR-003,
FR-007, FR-008, FR-010, FR-011, FR-018; research R1, R2, R4, R7).

**Independent Test**: with the images built once, a second `ap start` builds nothing and asks
nothing, and `ap start --check` exits 0; after a change to the page's code, `ap start` builds
`page` only.

### Tests for User Story 1

- [X] T004 [P] [US1] Test author: new file `scripts/tests/test_build_changed.py` (real BuildKit and the local registry under a test `AP_REGISTRY_NAME`, a test signing key, `XDG_*` in a temporary folder; skipped with a reason when Docker is absent; runs in CI's `py_tests` job) for `src/deploy/build.sh --changed <name>...`: (a) after `build.sh cli`, `built.env` holds `AP_BUILT_CLI=sha256:<64 hex>` (mode 600) and `images.env` still holds only its `AP_IMAGE_*` lines; (b) `--changed cli` with nothing changed pushes, signs and records nothing, asks for no password, makes no key pair or signing configuration when none exists, and prints nothing; (c) after a change to a file under `src/cli/` that changes the binary, `--changed cli page` prints `build: building cli` before it builds and `build: built and signed cli` after, and records `cli` only; (d) after a change to a string in `src/page/`, `--changed page check` builds `page` only: the `check` image's candidate digest is unchanged (research R1, the check image's own stage); (e) an image with no `AP_BUILT_*` line is built; (f) two images to sign from a terminal (pty) with `COSIGN_PASSWORD` unset and a key pair present: one password prompt in all, never echoed, the password in no file under `XDG_*`; with no key pair yet: two prompts (enter and confirm), then the pair exists; (g) no terminal and no `COSIGN_PASSWORD` with an image to sign: exit 1 naming the images, nothing signed or recorded; (h) a failing build: exit 1 naming the image, the images built before it kept, and a second `--changed` builds only what is left; (i) `scripts/generate.sh --check` runs once per `--changed` run (observed through a wrapper on `PATH` or the run's output); (j) `--changed --check` prints `would build <names>` or `nothing to build`, exits 1 or 0, and changes no `images.env`, `built.env`, registry or signature; (k) with the network off for a first build that needs it: exit 1 naming the image and the network. Show each case failing against today's build.sh. Done 2026-11-18 (7ec64cf; helper fix a2418f2, bf55a4d, owner-approved): judged in CI's `py_tests` job
- [X] T005 [P] [US1] Test author: new file `scripts/tests/test_ap_start.py` (the new stub of T003, the launcher's harness pattern; `build.sh` observed through its docker calls) for `src/deploy/ap start`: it runs `build.sh --changed engine cli page agent check project lint` after the prerequisite checks and then `up` with the resolved options; with every image current it prints `start: already started; nothing changed` on a started session and `start: started` otherwise, then the page address; a failing build makes it exit 1 with no `compose up` call; two `ap start` at once: the second waits for the first's `start.lock` and then changes nothing; a lock left by a dead process is taken over; `--check` makes no `compose up`, no push, no sign and no session, prints the contract's `start: would ...` lines and exits 0 when nothing would change and 1 when something would; `ap up` without the new options makes exactly the docker calls it made before (compare with the launcher stub's log of the same command). Show each case failing against today's ap. Done 2026-11-18 (7ec64cf; helper fix f6b7f49, owner-approved)

### Implementation for User Story 1

- [X] T006 [US1] Code author: `src/deploy/Dockerfile.check`: a stage that copies only `catalogue/api-feature-go/template/` and makes the module cache, oapi-codegen and the warm base cache there; the stage that copies `src/` builds only the check program; the final image unchanged in content and layout (research R1). Judged by T004 (d) and by the existing `docker`, `us2`, `planted` and `e2e` jobs, unchanged
- [X] T007 [US1] Code author: `src/deploy/build.sh` builds every image with `--build-arg SOURCE_DATE_EPOCH=0`, and every candidate also with `rewrite-timestamp=true,unpack=false` (research S10: the pushed build cannot rewrite timestamps); records `AP_BUILT_<NAME>=<candidate digest>` in `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/built.env` under the `images.env.lock`, replacing only that name's line; and gains `--changed [--check] <name>...`: `generate.sh --check` once (under `--changed --check` it never regenerates, only reports stale generated code as a change); candidate builds (`--sbom=false --provenance=false`, output to the local image store); the images whose candidate digest differs from `AP_BUILT_<NAME>` or whose `AP_IMAGE_<NAME>` is missing; with `--check`, the contract's lines and nothing else; otherwise `build: building <names>`, the key pair and signing configuration made only then, one password prompt without echo when `COSIGN_PASSWORD` is unset (a second to confirm only when making the key pair), the password exported only to that run's cosign calls, then today's build, push, sign and record for each, and `build: built and signed <names>`; a plain `build.sh <name>` keeps its behaviour (research R1, R2; data model, Image fingerprint). Make T004 pass without changing it
- [X] T008 [US1] Code author: `src/deploy/ap start` (and its usage line): sources `lock.sh`, takes `start.lock` in the state folder, runs the prerequisite checks (US2 adds the defaults and the gVisor check), runs `build.sh --changed` with the seven images, then `cmd_up` with the resolved options, and prints the contract's `start:` lines; `--check` as the contract says (research R4, R7). `ap up` without the new options unchanged. Make T005 pass without changing it, and every existing launcher test still passes
- [X] T009 [US1] Push (after telling the owner which jobs it runs) and fix until the CI run is green; then a fresh read-only review of US1 (rules above) against the spec, research R1, R2, R4, R7 and the contract; findings fixed or recorded as tasks. Done 2026-11-18: CI runs 44214595497 and 44264572517 green; fresh review (Sonnet) round 1: 3 blocking, 2 fixed with `test_ap_start_review.py`, 1 filed as backlog T166 (owner); round 2 closed both; suggestions and untested cases in backlog T167
- [X] T010 [US1] `/speckit-converge` for US1 (appends tasks only). Done 2026-11-18: one finding, T025 and T026 (Phase 8); the kept-session gap is backlog T166 (owner)

**Checkpoint**: US1 works on its own: a second start builds nothing.

---

## Phase 4: User Story 2 - Missing prerequisites are named, with the fix (Priority: P2)

**Goal**: defaults for the key file and projects folder, and every prerequisite checked first
(FR-004 to FR-006; research R3).

**Independent Test**: remove each prerequisite in turn; `ap start` names it and its fix and
starts nothing.

### Tests for User Story 2

- [X] T011 [P] [US2] Test author: new file `scripts/tests/test_ap_start_defaults.py` (the new stub of T003) for `ap start`: with no options it uses `${XDG_CONFIG_HOME}/agentic-platform/anthropic_api_key` and `$HOME/projects`; a missing default projects folder is made with mode 755 and `start` goes on; a missing `--projects` folder is refused (exit 3) and not made; a projects folder with mode 700 (no `o+rx`) is refused, exit 3, naming the folder and `700`; a missing default key file: exit 3 naming the path it looked at and `--credentials`; a key file readable by group: exit 3, as `up` refuses it; `STUB_RUNTIMES=runc`: exit 3 naming gVisor and `docs/guides/install-gvisor.md`; `STUB_DOCKER_VERSION=27.5.0`: exit 3 as `up`; in every refusal no build, verify or `compose up` call was made and the key is in no output. Show each case failing against today's ap

### Implementation for User Story 2

- [X] T012 [US2] Code author: `src/deploy/ap start`: options optional with the contract's defaults; the checks in the contract's order before the build step, reusing `need_docker` and `check_credentials`, plus `runsc` in `docker info`'s runtimes and the projects folder (make the default with mode 755 when missing, refuse a folder without `o+rx` naming its mode); each failure exit 3 naming its fix (research R3). Make T011 pass without changing it. Close backlog T165 in `specs/backlog.md` as `closed: done by feature 003 (T012)`
- [X] T013 [US2] Push (owner told first), CI green, fresh read-only review of US2, findings fixed or recorded; then `/speckit-converge` for US2. Done 2026-11-18: CI runs 44273312536 and 44279658801 green; fresh review (Sonnet) round 1: 1 blocking (`start --check` made the default projects folder), fixed with `test_ap_start_check_review.py`; round 2 closed it; suggestions and untested cases in backlog T168; converge found nothing to append. Backlog T168's open question was decided by the owner: Phase 9 (T027, T028)

**Checkpoint**: US1 and US2 work; a fresh clone needs only `ap start`.

---

## Phase 5: User Story 4 - Reach the run page from another machine (Priority: P2)

**Goal**: `ap up --listen`, passed on by `ap start`, serves the run page to other machines over
TLS behind a sign-in secret, rate-limited, limited to the given names, recorded (FR-012 to
FR-017; research R5; ADR 0062).

**Independent Test**: on a VM, `ap start --listen 0.0.0.0`; from another machine 401 without the
secret, the page's own 404 at `/` and 200 at `/runs/<run>` with it, 400 over plain HTTP; after `down` and `start` the port no longer answers.

### Tests for User Story 4

- [X] T014 [P] [US4] Test author: new file `scripts/tests/test_page_remote.py` (CI; the pinned `nginx` image from `src/deploy/images.lock`, a certificate made as research R5 says, a stand-in page container on an internal network) for `src/deploy/page-proxy/nginx-remote.conf` with a `names.conf` for `127.0.0.1 localhost vm.test`: 200 with `ap:<secret>`, and the stand-in saw Host `127.0.0.1:<port>`; 401 without or with a wrong secret and no stand-in content; 400 for plain HTTP; 421 for Host `other.test`; after a burst of 20 wrong requests, 429; the forwarder's log names the client address for a failed sign-in and holds no secret tried; and for `src/deploy/compose.remote.yaml` merged after `compose.yaml` (`docker compose config`, with `AP_PAGE_ADDRESS` set to `0.0.0.0` and to `::`): only `page-proxy` changes, it publishes exactly one port, in the long syntax with `host_ip` equal to `AP_PAGE_ADDRESS` (no `127.0.0.1` port left: `!override`), runs as `AP_HOST_USER`, mounts `AP_PAGE_CERT_DIR` read-only at the path the config reads, and keeps read-only root, dropped capabilities and its networks. Show each failing before the files exist. Done 2026-11-18 (d2bf763): 9 cases, all failing on the missing files before T016; passed on the pinned nginx in CI run 44300846848
- [X] T015 [P] [US4] Test author: new file `scripts/tests/test_ap_up_remote.py` (the new stub of T003) for `ap up --listen` and `ap start --listen`: a `--listen` value that is not `0.0.0.0`, `::` or an address `hostname -I` lists (a stub `hostname` on `PATH`), a `--name` with any character outside the data model's pattern (for example `a;b`, `a b`, `{`), `--name` or `--certificate` without `--listen`, `--certificate` without `--key`, a certificate that is not PEM: exit 3, nothing started; with `--listen 0.0.0.0` Compose gets `compose.remote.yaml` after `compose.yaml` and `AP_PAGE_ADDRESS=0.0.0.0`; `session.env` gains `AP_PAGE_SECRET` (64 lowercase hex), `AP_PAGE_NAMES` and `AP_PAGE_CERT_DIR`; `page-remote/` holds `htpasswd` starting `ap:$6$`, `names.conf` with `server_name` and the given names only, `cert.pem`, `key.pem` and `cert.names`, all mode 600, and the secret reached `mkpasswd` on standard input, never in its argv; a second `up` keeps the secret and makes no new certificate; `up` after `down` makes a new secret; other `--name` values after `down` make a new certificate, and so does a `cert.names` time older than 358 days, each printing the new fingerprint; a port Compose cannot publish on the address (the stub's `compose up` failing with Docker's "address already in use"): exit 1 naming the address and port; `--certificate` and `--key` given: they are copied, no certificate is made, their fingerprint is printed; on a started session, `up --listen` after a local start, `up` without `--listen` after a remote start, and another `--listen` or `--name`: each exit 3 telling to run `down`; `access.log` gains one `on` line with address, port, names and `sha256=`, one `off` line at the first start without `--listen` after it, and never the secret; stdout and stderr show the secret only in the `sign in as ap with` line; `ap page` prints the https names, the secret and the fingerprint; without `--listen`, every docker call is the same as today's `up`. Show each failing against today's ap. Done 2026-11-18 (1195cb2): 32 cases, 30 failing before T017; the start stub's pin lookup fixed with the owner's approval (05523c3)

### Implementation for User Story 4

- [X] T016 [P] [US4] Code author: `src/deploy/page-proxy/nginx-remote.conf` (fixed: TLS 1.2/1.3 from `/etc/ap/remote/cert.pem` and `key.pem`; a `default_server` answering 421; the named server includes `/etc/ap/remote/names.conf`; `auth_basic` with `/etc/ap/remote/htpasswd`; `limit_req_zone $binary_remote_addr` 10 r/s, `burst=20 nodelay`, `limit_req_status 429`; proxies to the page with `Host` set from `names.conf`; temp paths in `/tmp`; no `return` in the protected location) and `src/deploy/compose.remote.yaml` (page-proxy only: `ports: !override` with one long-syntax entry, `host_ip: ${AP_PAGE_ADDRESS}`, `published: ${AP_PAGE_PORT}`, the TLS port as target; `user: ${AP_HOST_USER}`; the remote config and `${AP_PAGE_CERT_DIR}` mounted read-only). Make T014 pass without changing it
- [X] T017 [US4] Code author: `src/deploy/ap`: `up` parses and validates `--listen`, `--name`, `--certificate`, `--key` (the contract's refusals; a change on a started session refused in either direction) and `start` passes them on; default names (`hostname`, `hostname -I`, `127.0.0.1`, `localhost`, or the one address); `page-remote/` (mode 700): the certificate made with `generate_cert.go` in the pinned `golang` image (`--network none`, read-only root, an executable `/tmp`, ECDSA P-256, 8760h, the names as SAN) when missing, when `cert.names` differs or when older than 358 days, or the developer's copied; the SHA-256 fingerprint computed in the pinned nginx image; the secret from `rand`, `htpasswd` from the nginx image's `mkpasswd -m sha512` with the secret on standard input, `names.conf`; the session variables; `compose.remote.yaml` passed to every Compose call of the session; the `access.log` lines; the remote lines of `up` and `page` (contract). Without `--listen`, nothing of this runs. Make T015 pass without changing it
- [X] T018 [US4] Push (owner told first), CI green, fresh read-only review of US4 with the threat model, findings fixed or recorded; the owner runs quickstart section 3 on the VM (an agent never marks this as human-verified); then `/speckit-converge` for US4. Agent part done 2026-11-18: CI runs 44300846848 and 44308435407 green; fresh review (Sonnet) round 1: 1 blocking (a second `up` replaced the developer's certificate), fixed with `test_ap_up_remote_review.py`; round 2 closed it; suggestions and untested cases in backlog T169; converge found nothing to append. Open: the owner's quickstart section 3 on the VM. The owner's run on the VM (2026-11-18, branch at 9ea19e0, outputs pasted in the session): `ap start --listen 0.0.0.0` built, signed, verified and started (after a new signing key: the old key's password was lost, backlog T171); from another machine 401 without and with a wrong secret, the page's own 404 at `/` with the secret (the page has no index; quickstart corrected in d7c2f78), 400 over plain HTTP; `access.log` got the `on` line (address, port, names, fingerprint) and, after `ap down && ap start`, `off`; then `https://192.168.1.53:7878/` did not connect (000, run on the VM). The owner read the certificate's SHA-256 fingerprint in their browser, `868c4f36…b6efae79`, the same as the printed `86:8C:4F:…:AE:79`. Checked on the owner's run and answer, 2026-11-18

**Checkpoint**: the owner reaches the run page on the VM from their computer.

---

## Phase 6: User Story 3 - What the developer types says what, not how (Priority: P3)

**Goal**: the interface and the guides name no internal part (FR-009; Principle XVIII).

**Independent Test**: the quickstart's start is one command; `ap start`'s usage names only the
key, the projects and remote access.

- [X] T019 [P] [US3] Test author: new file `scripts/tests/test_ap_usage.py` (files `py_tests` judges only; nothing under `specs/`): `src/deploy/ap` with an unknown option prints a usage whose `start` line is the contract's and names none of `image`, `build.sh`, `generate`, `sign`, `cosign`, `registry`; `src/cli/contract.md`'s `start` section names no build, generator or signing step for the developer to run. Show it failing
- [X] T020 [US3] Code author: the usage text in `src/deploy/ap`; the start sections of `specs/001-prose-to-accepted-slice/quickstart.md` and `docs/operations/local.md` (one `ap start`; `build.sh` and `up` named only under development and CI; remote access and the Docker-bypasses-ufw note); `src/deploy/README.md` (`start`, the new variables, `compose.remote.yaml`, `lock.sh`, `built.env`, `page-remote/`, `access.log`); the stale header comment of `src/deploy/compose.yaml` on the page port. The root `README.md` changes only if its one-line "Try it" pointer becomes wrong. Make T019 pass without changing it. Done 2026-11-18: the usage lists `start`, `down`, `page` and the client commands, with `up`, `build-workers` and `test` under development and CI and `registry` left out (it is `build.sh`'s); the root README unchanged
- [X] T021 [US3] Push (owner told first), CI green, fresh read-only review of US3; then `/speckit-converge` for US3. Done 2026-11-18: CI run 44344279329 green; fresh review (Sonnet) round 1: 2 blocking (the usage's `--name` default untrue for one address; `--check` and `--listen` without a default), fixed with `test_ap_usage_review.py`; round 2 closed both; suggestions and untested cases in backlog T170; converge found nothing to append

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T022 Knowledge bundle: `docs/architecture/overview.md` (the `start` command; the forwarder's remote mode; ADR 0061, 0062, 0063), `docs/overview/problem-space.md` (N-005: spec 003, state), `docs/components/nginx.md` if the shipped behaviour differs from it, `docs/log.md`; `python3 scripts/okf.py index` and `check` report OK. Done 2026-11-18: overview section "Shipped by feature 003", network line and decisions list; N-005 → spec 003, in progress (hosted service still to do); nginx gains the forwarder's remote interface, files and test (it did not contradict the shipped behaviour, only lacked it); log entries for T018, T027/T028 and T022; `okf.py check` OK, markdownlint clean
- [X] T023 Measure on the owner's clean VM, the owner running it: the first `ap start` from a fresh clone (time, images built), a second `ap start` (under 30 s, SC-002), `ap down && ap start`, `ap start --check`; record the figures in `quickstart.md` and research R1, and whether the digests stayed stable on that machine's Docker. 2026-11-18: measured (research S11): first start 1 min 27 s with a warm build cache, second 38.6 s (SC-002 missed, Phase 10), `down` and `start` 1 min 41 s, `--check` exit 0, fingerprints stable; closed with T031's measurement (S13: second start 11.9 to 12.3 s; figures in quickstart.md); the owner asked the agent to run it over SSH
- [X] T024 `/speckit-converge` for the whole feature, then a final fresh read-only review of the feature against every FR and SC, judging the CI run of the last pushed commit (Review converges), including T027 and T028, with backlog T166 and T167 as known open items; every finding fixed or recorded; the owner decides the merge to `main`. Done 2026-11-18: converge for the whole feature found nothing to append (FR-001's stopped state stays backlog T166); fresh review (Sonnet, own worktree at 952c0e0, code identical to 178be0b) judged CI run 44391905252 green: every FR, SC, scenario and edge case met or covered by a known backlog item, no blocking finding; suggestions and untested cases in backlog T176. Open: the owner decides the merge to `main`

## Dependencies & Execution Order

- T001 → T002, T003 → every story. Stories: US1 (T004–T010) first, then its convergence tasks
  T025 and T026 before US2 (all three change `ap start`; T017 then reuses T026's one session
  comparison); US2 (T011–T013) after T008,
  since both change `ap start`; US4 (T014–T018) after T012 for the same reason, though T014 and
  T016 (nginx and Compose files only) may run alongside US2; US3 (T019–T021) after US4, since it
  documents every option; Phase 9 (T027 → T028) after T026 and T017, since T028 changes `ap
  start`'s checks, then pushed (owner told first), judged by its CI run and a fresh read-only
  review as a story is; Polish (T022–T024) last; Phase 10 (T029 → T030 → T031) after T023's
  first measurement and before T024, which judges it; T023 closes with T031's measurement.
- Within a story: tests (seen failing) → code → push and CI → fresh review → converge.
- One unit at a time on the host (small and shared): never two image builds or two Docker test
  files at once.

## Parallel Opportunities

- T002 and T003 (different files).
- T004 and T005 (different new test files) by one or two test authors; T006 alongside them.
- T014 and T015 (different new test files); T016 alongside T015 (different files).
- T019 alongside T022's text that does not depend on shipped behaviour.

## Implementation Strategy

- **MVP**: Phases 1–3 (US1): a second start builds nothing, the first builds everything with one
  prompt. Then US2 makes a fresh clone need only `ap start`; US4 gives the owner the VM page; US3
  cleans the interface and guides.
- Each story is pushed and judged by its CI run before the next starts.

## Phase 8: Convergence

- [X] T025 [US1] Test author: new file `scripts/tests/test_ap_start_session.py` (the start stub of T003): on a started session with another `--credentials` or `--projects`, `ap start` and `ap start --check` exit 3 telling to run `ap down` first, before any candidate build, push, sign or `compose up` call; show it failing against today's `ap`, per spec Edge Cases ("already started with another key file or projects folder: refuses, as today") and FR-004 (checks before it builds) (partial)
- [X] T026 [US1] Code author: move `up`'s comparison of a started session with the given key file and projects folder (and override) into one function that `cmd_up` and `cmd_start` both call, `start` calling it before `build.sh`; `up`'s behaviour and messages unchanged; make T025 pass without changing it. T166 will later change how "started" is decided in the same place, per spec Edge Cases and FR-004 (partial)

## Phase 9: Owner decision on T168 (2026-11-18)

The owner: "start name every failing prereq." FR-004 and the contract (step 2, "naming each failure and its fix") already say so; today `ap start` stops at the first failing prerequisite, so this is a defect against agreed intent.

- [X] T027 [US2] Test author: new file `scripts/tests/test_ap_start_all_prerequisites.py` (the start stub of T003): with several prerequisites failing at once (for example `STUB_RUNTIMES=runc`, a missing `--credentials` file, a `--projects` folder of mode 700 and `--name` without `--listen`), `ap start` and `ap start --check` exit 3 and name every one of them with its fix, in the contract's order, before any candidate build, push, sign or `compose up` call; and with a missing default projects folder and another failing prerequisite, `ap start` makes no folder and `ap start --check` prints each failure; show it failing against today's `ap`, per FR-004 and the contract's `start` step 2
- [X] T028 [US2] Code author: `src/deploy/ap start` runs every prerequisite check of step 2, names each failure with its fix, then exits 3 if any failed; a check whose input already failed (the key file's place relative to the projects folder, the started session's comparison) is skipped, and the default projects folder is made only when no check failed; `up`'s own checks and messages unchanged; make T027 pass without changing it, per FR-004. Done 2026-11-18 (21b4740); fresh review (Sonnet) round 1: 1 blocking (a bad `--name` was refused while parsing, before every other check), fixed with `test_ap_start_prerequisites_review.py` (e1a867b) and 8689db6; round 2 closed it; suggestions and untested cases in backlog T173; CI run 44362312267

## Phase 10: T023 found SC-002 missed (2026-11-18)

T023 on the owner's VM (research S11): a second `ap start` with nothing changed took 38 to 40 s, against SC-002's 30 s, because BuildKit does not cache the candidate's timestamp rewrite. A defect against agreed intent (SC-002) whose fix changes the design: research R1 revised, data model `cached.env`, ADR 0064 (owner's choice, 2026-11-18). Measured with a scratch copy on the VM: 12 to 14 s (S12).

- [X] T032 [US1] Contract (before T029): `src/cli/contract.md` "Starting the platform" cites ADR 0064, and `start --check` makes the builds that decide what changed, no image's reproducible build when its quick build settles it; lines and exit codes unchanged. Done 2026-11-18 with analyze round 9
- [X] T029 [US1] Test author: new file `scripts/tests/test_build_cached.py`. With the start stub (T003, unchanged) on `PATH` and `build.sh --changed` (and `ap start` where it shows the case): (a) a signed build writes `cached.env` (mode 600) with `AP_CACHED_<NAME>=<digest> <fingerprint>` for each image it signs, the fingerprint being its `AP_BUILT_<NAME>`; (b) with every image current and a matching pair, `--changed` and `--changed --check` make for each image one build without `rewrite-timestamp` and no build with it, and push, sign and record nothing; (c) a pair whose digest differs, or whose fingerprint is not `AP_BUILT_<NAME>`, or a missing or malformed line, or a recorded image missing from the store, falls back to the candidate (a build with `rewrite-timestamp`), which decides as before; (d) when that candidate finds the image current, `--changed` writes a new pair and `--changed --check` writes nothing; (e) `images.env` and `built.env` keep only their own lines; (f) order: every cached build before every candidate in a run, and the pair's digest is the cached build's of that run; with `--check` and no pair, the candidates alone (no cached build); (g) a failing cached build writes no pair and the candidate's failure is reported as before; (h) `ap down` keeps `cached.env`. With real BuildKit (CI's `py_tests`, as T004): after `build.sh cli`, two `--changed cli` runs build nothing and leave `cached.env` unchanged. Show it failing against today's `build.sh`, per SC-002, research R1 and the data model's `cached.env`. Done 2026-11-18 (a731fa5, Sonnet test author): 16 failures and 1 error against 8a1d5d6, three cases pin unchanged behaviour
- [X] T030 [US1] Code author: `src/deploy/build.sh` records and uses the cached digest as research R1 and the data model say; `src/deploy/README.md` and the header of `build.sh` describe it; make T029 pass without changing it, and keep every existing test passing unchanged (`test_build_changed.py`, `test_ap_start*.py`, `test_signing.py`), per ADR 0064. Done 2026-11-18; T029's stub cases and every `test_ap_start*.py` and `test_ap_usage*.py` pass locally; the real-BuildKit cases are judged in CI
- [X] T031 [US1] Push (owner told first), CI green, fresh read-only review of T029 and T030, findings fixed or recorded; then measure on the owner's VM again (T023's steps): a second `ap start` under 30 s (SC-002). Done 2026-11-18: CI run 44391905252 green (the real-BuildKit class ran, 109 s); fresh review (Sonnet) round 1: no blocking finding; suggestions and untested cases in backlog T175; VM from the pushed commit: 11.9 to 12.3 s
