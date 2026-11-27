# Terminal contract

Two names, two jobs:

- **The launcher** `src/deploy/ap` runs on the host and only calls `docker compose` (research D6),
  except `test`, which builds a suite in the pinned Go container and runs its test binary on the
  host ([ADR 0037](../../docs/decisions/0037-terminal-run-commands-exit-codes-and-test-on-host.md)).
  Its own commands are `start` (the developer's one start command, see "Starting the platform"),
  `up --credentials <file> --projects <folder>` (starts images already built; CI and the test
  suites use it), `down`, `build-workers
  <slice> <project>`, `page`, `test <suite>` (runs a test suite, see "The test harness") and `registry`
  (starts the local image registry if it isn't running; `src/deploy/build.sh` calls it). It intercepts
  `build`, `resume`, `replay`, `prove` (research D32; feature 011) and `trials` (feature 012):
  - `build <slice> <project>` runs `build-workers <slice> <project>` first, then passes the command on.
  - `replay <run> <project>` reads the run (`getRun`) for its slice, runs `build-workers <slice>
    <project>` unchanged, then passes the command on. A replay always builds into a new, empty
    project: a folder that holds a file, a symbolic link or a non-empty `.git` is refused by
    `build-workers` (exit 3) as for `build`. An unknown run is exit 4 (`NotFound`).
  - `prove <captured pattern> <project>` runs the client's `proof-slice <captured pattern>`,
    which prints only the proof slice's ID and a line feed on standard output, then runs as
    `build <proof slice> <project>` does (`build-workers` first). An unknown captured pattern is
    exit 4 (`NotFound`) and starts nothing. Running it again into another project builds again.
  - `trials <slice> <prefix> [--trials <k>]` runs `build-workers <slice> <prefix>-<i>` for each
    `<i>` from 1 to `<k>` (3 without `--trials`, the engine contract's `x-limits.trials-default`),
    in that order, then passes the command on. It stops at the first `build-workers` that fails,
    with that command's exit code, and starts no set; the projects already made stay, and running
    the command again with a new prefix is safe. `<k>` outside 1 to `x-limits.trials-max` is a
    usage error (exit 3) before anything changes. Each project gets its own step containers, so
    the trials run with the terminal closed.
  - `resume <run>` never runs `build-workers`. It reads the run (`getRun`), which gives its slice and
    project; an unknown run is exit 4 (`NotFound`), and a run that is not `interrupted` is passed on
    unchanged for the engine to refuse (`RunNotInterrupted`). For an interrupted run, every step of
    the pattern must have its step container for that slice and project running (by the labels
    below and this session's tokens); the launcher then registers each with the engine again and
    passes the command on. When one is missing or stopped, or the slice's tokens are gone (after
    `ap down` or a host reboot), it changes nothing and exits 3 with `resume: the step workers of
    <slice> for the project <project> are not running; run ap build-workers --resume <run>`.
    A run that a platform stop interrupted continues by itself once its step workers are
    registered, so `ap build-workers --resume <run>` alone lets it continue, with no `ap resume`.
    The engine keeps the registrations across its own restart, so when only the engine restarted
    and the step containers still run, the run continues with no command at all.

  `build-workers <slice> <project>` makes the
  project folder with an empty `.git` and the folders the steps write, starts one step container per
  step of the pattern (each with only its step's paths writable), writes each container's grant
  for the engine and registers it; running it again changes nothing. A step container is named
  `<compose project>-worker-<slice>-<step>-<p>` (`<compose project>` is `compose.yaml`'s `name`,
  `agentic-platform` in a real session; the slice in lowercase; `<p>` the first 12 hex
  digits of the SHA-256 of the project name, so the name fits one 63-character DNS label) and
  labelled `org.agentic-platform.slice`, `org.agentic-platform.step` and
  `org.agentic-platform.project` with the slice, step and project in full. Each project has its own
  containers: `build-workers` never removes or replaces a container whose project label names
  another project, and every step container stays up until `ap down`. It exits 3 for a slice or
  project name that is not one, for a project folder that already holds a file or symlink at any depth or a non-empty `.git` (naming it, and changing nothing), or without the agent and check images, 4 when the engine has no
  such slice (`NotFound`) or while a run of the slice has not ended (`build-workers: the run <run>
  of <slice> has not ended (RunActive); resume, take over or cancel it first`, found with
  `listRuns` and the session's client token, before anything changes), and 1 when Docker or the
  engine fails, when the engine's answer cannot be read, or when a container with the name it wants
  is labelled with another project.

  `build-workers --resume <run>` (T053j) rebuilds the step workers of an interrupted run after `ap
  down` or a host reboot. It reads the run (`getRun`) for its slice and project and accepts that
  run's existing project folder only, creating nothing in it; it makes the slice's tokens if they
  are gone, writes the grants, starts the project's step containers (same names, labels and mounts)
  and registers them, and the engine then continues the run by itself; running it again changes
  nothing. It exits 4 for an unknown run (`NotFound`)
  or one that is not `interrupted` (`RunNotInterrupted`), 3 when the project folder, its `.git` or
  a step's folder is missing or not a folder (naming it), and 1 as `build-workers` does.

  Every other command is passed unchanged to the terminal
  client, which never calls the launcher. The launcher's own
  lines (`ap: ...`) always go to standard error, so standard output holds only the client's output;
  it passes the client's exit code on unchanged, and Ctrl-C reaches the client. Ctrl-C while
  `build` or `replay` still runs `build-workers` exits 130 (143 for SIGTERM) before any run starts; running the
  command again is safe, since `build-workers` changes nothing the second time.

  `up` returns only when every service that declares a readiness check is ready (Compose's
  `--wait`; today `contract-lint`, through its `GET /health`), so a command right after `up` finds
  the platform answering; a service that does not become ready within its check's limit makes `up`
  exit 1, naming Compose's message, with the session kept until `down`.

  `up` refuses (exit 3) a credentials file that is missing, empty, not owned by the developer or
  readable or writable by anyone else, a projects folder that does not exist or is not writable,
  and a credentials file inside the projects folder. The file holds only the provider key. `up`
  after `down` starts a new session with new tokens; `up` on a started session keeps its tokens and
  port, so running it twice changes nothing, and refuses (exit 1, telling the developer to run
  `down`) when the session file is unreadable or incomplete. `up` on a started session with another
  credentials file or projects folder than the session's refuses (exit 3, telling the developer to
  run `down` first). The run page's port is
  `AP_PAGE_PORT`, or the session's port, or 7878, and `page` prints its address (with remote access,
see "Starting the platform"). `down` works with
  a missing or broken session file. The session is kept, readable only by the developer,
  in `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/session.env` (folder mode 700, file mode
  600) until `down`, also after an `up` that failed. A session file that is not a regular file
  owned by the developer with mode 600 is never read: every command but `down` refuses it (exit 1,
  telling the developer to run `down`), and `down` stops the platform without it. Every command
  but `start`, `up`, `down`, `registry` and `test` needs a started session (exit 3 without one). A command whose parts do not exist yet
  exits 3 with `not available yet: <task>`. Commands passed to the terminal client read standard
  input as it is, a terminal or not.

  `up` starts only images it has verified ([ADR 0028](../../docs/decisions/0028-sign-platform-images-locally.md)).
  It reads the images `src/deploy/build.sh <name>` recorded in
  `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/images.env` and exits 1, naming the
  `build.sh <name>` to run, when the `engine` or `cli` image or the public key
  `${XDG_CONFIG_HOME:-~/.config}/agentic-platform/signing/cosign.pub` is missing. Until the run
  page exists (T056) the `page` image is optional: without it `up` starts everything but the run
  page and its forwarder and says on stderr that the run page is not built yet. It verifies the
  signature of every image it passes to Compose, including a built `page`, `agent`, `check` or `tests`
  image, and exits 1 naming the first image that fails, before anything starts and without changing
  the session. A recorded image no longer in the local image store (a rebuild replaced it) is
  first taken back from the local image registry by digest; when the registry has lost it too,
  `up` exits 1 naming the `build.sh <name>` to run. Later commands start the images the session's `up` verified; after a rebuild, run
  `up` again. The local image registry listens on `127.0.0.1:${AP_REGISTRY_PORT:-5005}` only.
  `AP_REGISTRY_NAME` (default `ap-registry`) is for running a second, independent registry (for
  example in tests); its container, volume and network are named after it (`<name>`,
  `<name>-data`, `<name>-net`; by default `ap-registry`, `ap-registry-data`, `ap-signing`), and a
  name that is not `[a-z0-9][a-z0-9_.-]*` is a usage error (exit 3). Several `registry`, `up` and
  `build.sh` commands may start the registry at the same time: each one returns when the one
  registry answers, and none fails because another made it first. A registry that cannot publish
  its port (the port is taken) is removed again, its data kept, and `registry` exits 1.
- **The recorded model, for tests only.** When `AP_COMPOSE_OVERRIDE` names a file, `up` passes it
  to Compose after `compose.yaml` and keeps it in the session, so every later command (`build-workers`,
  the client commands, `down`) uses it. The file must be under `src/tests/e2e/` of the launcher's
  repository; another path, or one that is not a file, is a usage error (exit 3) and nothing starts.
  `up` then prints `up: the model is the recorded model (<file>)` on standard error. `scripts/e2e.sh`
  uses it to run the stack against the replay server (research D44); a developer never sets it.
- **The terminal client** runs in its own container and is a client of the engine API
  ([openapi.yaml](../engine/api/preview/2026-11-12-preview/openapi.yaml)) with the client token. It adds no behaviour
  of its own.

Exit codes: `0` success, `1` a check or approval failed, or the engine failed or could not be
reached (for `up`: an image is missing or failed verification, or Compose failed; for `down` and
`registry`: Docker failed), `2` the run waits for the developer, is interrupted or was handed over,
`3` usage error, `4` refused by the engine (the problem is printed, see "Problems"), `5` the run
expired, `6` the run or operation was cancelled, `7` the compared runs differ, `130` the developer
stopped following with Ctrl-C, `143` with SIGTERM (either way the work continues). How each run state maps to a code is in "Run
states and exit codes".

| Command | Does | API operations |
|---------|------|----------------|
| `describe` | Reads the description and proposes a slice | `createSlice`, `getOperation`, `getSlice` |
| `questions <slice>` | Shows open questions and records the answers | `getSlice`, `answerQuestions`, `getOperation` |
| `show <slice> [goals\|rules\|contract]` | Shows a part, its hash and its check results | `getSlice` |
| `edit <slice> goals\|rules\|contract` | Opens a part in the editor and saves it; `rules` holds the rules, examples and terms | `getSlice`, `replaceGoals`, `replaceRules` or `replaceContract`, `getOperation` |
| `approve <slice> <part>` | Approves a part at the hash just shown | `getSlice`, `approvePart`, `getOperation` |
| `build <slice> <project> [--pattern <id>]` | Builds into the new folder `<project>` under the projects folder and follows the run; `--pattern` names the pattern (the base pattern or a captured pattern), else the engine chooses | `startRun`, `getOperation`, `listRunEvents` |
| `resume <run>` | Continues an interrupted run and follows it | `resumeRun`, `getOperation`, `listRunEvents` |
| `replay <run> <project>` | Replays an ended run into the new folder `<project>` and follows it | `replayRun`, `getOperation`, `listRunEvents` |
| `take-over <run>` | Accepts the hand-over of a run waiting for the developer | `acceptHandOver`, `getOperation`, `getRun` |
| `compare <run> <other>` | Shows the differences between two runs | `compareRuns` |
| `runs` / `run <run>` | Lists runs, or shows one run's record and follows it while it runs | `listRuns`, `getRun`, `listRunEvents`, `getOperation` |
| `artifact <run> <hash>` | Writes an artifact's content to standard output | `getArtifact` |
| `slices` | Lists slices | `listSlices` |
| `cancel <run\|operation>` | Cancels a run or other running work | `getRun` (for a run), `cancelOperation`, `getOperation` |
| `delete <slice> [--yes]` | Erases a slice and its records after confirmation | `getSlice`, `listRuns`, `deleteSlice`, `getOperation` |
| `capture <run> [--name <name>]` | Captures a done run as a pattern and prints its parameters | `captureRun`, `getOperation`, `getCapturedPattern` |
| `patterns` | Lists captured patterns | `listCapturedPatterns` |
| `pattern <id>` | Shows a captured pattern: parameters and their places, proofs, state, usable | `getCapturedPattern` |
| `pattern <id> --drop <param>` / `--add [<name>=]<value>` | Removes or adds a parameter | `getCapturedPattern`, `replaceParameters`, `getOperation` |
| `proof-slice <id>` | Gives the captured pattern's proof slice and prints its ID | `makeProofSlice`, `getOperation` |
| `approve-pattern <id>` | Approves a proven captured pattern | `approveCapturedPattern`, `getOperation` |
| `withdraw-pattern <id>` | Stops a captured pattern's use by new runs | `withdrawCapturedPattern`, `getOperation` |
| `erase-pattern <id>` | Erases a captured pattern after confirmation | `getCapturedPattern`, `eraseCapturedPattern`, `getOperation` |
| `trials <slice> <prefix> [--trials <k>]` | Builds the slice `<k>` times (1 to 5, default 3), reusing nothing, into the new projects `<prefix>-1` to `<prefix>-<k>`, one after another, and follows the set: one line per trial as it ends, then the result | `startTrialSet`, `getOperation`, `getTrialSet` |
| `trial-sets <slice>` | Lists the slice's trial sets | `listTrialSets` |
| `trial-set <id>` | Shows a trial set's result; follows it while it runs | `getTrialSet`, `getOperation` |

When a slice fails a pattern condition (a check result with `pattern` true; a result without
`pattern` counts as false), `describe` and `show` (without a part) also print one line: `The pattern does not fit this description: <condition>: <reason>; ...`, naming each failing
condition and its reason. A slice that fails no pattern condition prints no such line, also when
another check fails.

Captured patterns (feature 011). `pattern <id>` with no parameter prints `No parameters: only a
slice identical to the source fits.`. `--add [<name>=]<value>` takes the name from the value when
none is given, and refuses (exit 3) a value that is not a valid name then; `--drop` and `--add`
send every other current parameter unchanged. `erase-pattern` first shows what it erases (the
captured pattern, the artifacts only it holds, its proof slice and that slice's runs) and asks the
developer to type its ID, erasing nothing on any other answer. `slices` gains a `PROOF OF` column
and `show` a `proof of <captured pattern>` line for a proof slice. A failed proof's run waits for
the developer like any handed-over run: cancel or take it over before changing the parameters or
proving again.

`delete` first shows what it erases: the slice's ID and title, its runs and the number of stored
artifacts, and that project folders are kept. It then asks the developer to type the slice ID, and
erases nothing on any other answer. `--yes` skips the question, for scripts.

## Starting the platform

Added by feature 003 ([ADR 0063](../../docs/decisions/0063-ap-start-is-the-start-command-ap-up-unchanged.md),
[ADR 0061](../../docs/decisions/0061-up-builds-only-changed-images-by-reproducible-digest.md),
[ADR 0062](../../docs/decisions/0062-run-page-reached-remotely-over-tls-behind-a-sign-in-secret.md),
[ADR 0064](../../docs/decisions/0064-a-cached-digest-spares-the-reproducible-rebuild.md)).
`up` without the options below behaves exactly as described above.

### `start`

```text
ap start [--check] [--credentials <file>] [--projects <folder>]
         [--listen <address> [--name <host>]... [--certificate <cert> --key <key>]]
```

Starts the platform from the current source and returns when it is ready. It is safe to run in
any state; with nothing changed it changes nothing.

| Option | Default | Meaning |
|--------|---------|---------|
| `--check` | off | Show what `start` would do and change nothing (below) |
| `--credentials <file>` | `${XDG_CONFIG_HOME:-~/.config}/agentic-platform/anthropic_api_key` | The file holding only the model provider's key |
| `--projects <folder>` | `~/projects`, made with mode 755 when missing | Where new projects are built; given explicitly, it must exist |
| `--listen`, `--name`, `--certificate`, `--key` | as `up` (below) | Passed on to `up` unchanged |

What `start` does, in this order:

1. Takes the start lock; a second `start` waits for the first, then finds nothing to do.
2. Checks the machine before anything else, and exits 3 naming each failure and its fix: Docker
   28 or later; the gVisor runtime `runsc` registered with Docker (naming
   `docs/guides/install-gvisor.md`); the key file (missing, empty, not the developer's, readable
   or writable by group or others, or inside the projects folder); the projects folder (missing
   when given with `--projects`, not writable, or not open to others, naming its mode); and the
   remote-access options as `up` checks them.
3. Builds the images whose result changed since their last signed build, and only those, through
   `src/deploy/build.sh --changed`. Its lines, on standard error, are `build: building <names>`
   before it builds and `build: built and signed <names>` after; with nothing to build it prints
   nothing. When it must sign and `COSIGN_PASSWORD` is not set, it asks once for the signing key's
   password on the terminal, without echo (a second time to confirm, only when it first makes the
   key pair). Without a terminal and without `COSIGN_PASSWORD` it exits 1 naming the images it
   would sign, and starts nothing. A failed build exits 1 naming the image, with the build's own
   error; images already built are kept.
4. Runs `up` with the resolved key file, projects folder and remote options, and exits with its
   code. `up` verifies every image and starts the platform, as before.
5. Prints, on standard error, `start: started` or `start: already started; nothing changed`,
   then the run page's address (or, with remote access, `up`'s remote lines). The platform is
   already started when it has a session and its engine is running; a session whose containers
   stopped (after a reboot, for example) is started again and prints `start: started`. When
   Docker cannot say whether the engine runs, `start` exits 1.

`start --check` does steps 1 and 2, makes the builds of step 3 that decide what changed (no image's
reproducible build when its quick build settles it, ADR 0064), and prints, changing no
recorded, signed or running image and no session, and making no projects folder: each failing
prerequisite; `start: would make the projects folder <folder>` when the default one is missing (a change:
exit 1);
`start: would build <names>` or `start: nothing to build`; and `start: would start` or `start:
already started`. It exits 0 when nothing would change, 1 when something would, and 3 for a
failed prerequisite.

Exit codes: 0 started (or, with `--check`, nothing would change); 1 a build, the signing, a
verification or Compose failed (or, with `--check`, something would change); 3 a usage error or a
failed prerequisite.

### `up`: remote access

```text
ap up --credentials <file> --projects <folder>
      [--listen <address> [--name <host>]... [--certificate <cert> --key <key>]]
```

Without `--listen`, `up` is unchanged in every respect.

| Option | Default | Meaning |
|--------|---------|---------|
| `--listen <address>` | none: the run page is reachable from this machine only | Let other machines reach the run page on `<address>`: `0.0.0.0`, `::`, or an address of this machine |
| `--name <host>` | with `0.0.0.0` or `::`: this machine's host name, the addresses `hostname -I` lists, `127.0.0.1` and `localhost`; with one address: that address | A name or address the run page answers to; repeatable |
| `--certificate <cert> --key <key>` | a certificate the platform makes for the names | A PEM certificate and its key to serve instead |

`up` exits 3, starting nothing, for: a `--listen` value that is not `0.0.0.0`, `::` or an address
`hostname -I` lists; a `--name` that is not a host name (labels of letters, digits and inner
hyphens, joined by dots) or an IP address; `--name`, `--certificate` or `--key` without
`--listen`; one of `--certificate` and `--key` without the other; a certificate or key that is not
readable PEM; and, on a started session, any change of remote access (turning it on or off, or
another address, names or certificate), telling to run `down` first. On a session started with
the developer's own certificate, a second `up` or `start` must give the same `--certificate` and
`--key` again (leaving them out is a change); on one with a certificate the platform made,
`--certificate` is a change. A port Compose cannot
publish on that address exits 1 naming it, as any Compose failure does.

With `--listen`, `up` also prints on standard error:

```text
up: the run page accepts connections from other machines on <address>:<port>
up: open https://<name>:<port>  (names: <name>, <name>, ...)
up: sign in as ap with <secret>
up: certificate sha256 <AB:CD:...>  (compare it once in your browser)
up: recorded in <state>/agentic-platform/access.log
```

The secret is new at each start after `down` and kept by a second `up`. `access.log` gets one `on`
line per start that turns remote access on, and one `off` line at the first start without it
after one with it; never the secret.

### `page` with remote access

Unchanged without remote access (`http://127.0.0.1:<port>`). With it: one `https://<name>:<port>`
line per name, then `sign in as ap with <secret>` and the certificate's fingerprint.

### With remote access, the run page

- Answers only over TLS (1.2 or 1.3) on `<address>:<port>`; a plain HTTP request gets 400 and no
  run content.
- Answers only the names given; any other Host gets 421.
- Asks for HTTP Basic authentication as user `ap` with the secret; without it, or with a wrong
  one, 401 and no run content; each failure is logged with its time and client address, never
  the secret.
- Answers at most 10 requests per second per client address, with a burst of 20; beyond that 429.
- Is the same read-only page as on the machine: nothing reachable through it can change a slice,
  a run or a project, or reach any other part.

### `down`

Unchanged: stops everything, keeps run records, projects, the image records and `access.log`, and
removes the session with its secret.

## Configuration

The client reads `ENGINE_URL` (the engine's base URL, `http://engine:8080` in a session) and
`ENGINE_TOKEN` (the client token) from its environment; the launcher sets both (`compose.yaml`,
service `cli`), and a test may set them itself. A command that calls the engine exits 3 when
either is empty, naming both; `help` and a usage error need neither. `EDITOR` is read by `edit`
only. The client writes no file, so it runs with a read-only root file system, and it reads
standard input as it is, a terminal or not.

## Problems

A refusal (exit 4) prints the problem on standard error, as the Story 1 client does:

```text
refused: <code>: <title>: <detail>
  failed  <check> (<part>): <reason>
```

`: <title>` is left out when the title is empty or equals the code, `: <detail>` when there is no
detail, and there is one `failed` line per entry of `failures`, in its order. A failed operation
prints its `error` the same way. Nothing goes to standard output for a refusal.

## Store text in the terminal

Text the client prints from the store (a reason, a finding's text, a detail, a compared value, a
title) can come from a model. Every control character in it, line breaks included, is printed
escaped as Go's `strconv.Quote` writes it, without the surrounding quotes (`\n`, `\x1b`), so such
text can neither move the cursor nor make a line that looks like the client's own. `artifact` is
the one exception: it writes the content unchanged, for a pipe or a file.

## Idempotency and the run ID

Each invocation of `build`, `resume`, `replay` and `take-over` makes one `Idempotency-Key`: 32
lowercase hex digits from the operating system's random source. It sends that key with its one
mutating call (`startRun`, `resumeRun`, `replayRun` or `acceptHandOver`). The captured-pattern
commands do the same: `capture` (`captureRun`), `pattern --drop` and `--add`
(`replaceParameters`), `proof-slice` (`makeProofSlice`) and `approve-pattern`
(`approveCapturedPattern`), each with the retry rule below; their lost-answer message ends `see
patterns` (`capture`) or `see pattern <id>` instead of `see runs`.

When that call gets no answer (a connection error or the client's 2-minute timeout), or answers
502, 503 or 504, the client sends the same request with the same key and body again, as the
engine contract's retry rule says (`x-limits`: `retry-attempts` more times, each after the wait
that `retry-first-wait-ms`, `retry-longest-wait-ms` and `retry-jitter-fraction` give; the client
reads them from the engine contract it was generated from); a 429 waits its `Retry-After`
first. The engine returns the first result for a key it has
kept (24 hours, `x-limits.idempotency-key-retention-hours`), so a retry never starts a second
run. When every try is lost the client exits 1 with `engine: no answer to <operation> after <n>
tries (Idempotency-Key <key>); it may have been recorded: see runs`, `<n>` being `retry-attempts`
plus the first try. Any other answer is final:
a problem is a refusal (exit 4).

The first line on standard output after the 202, before any event is read, names the run and its
operation:

- `build`: `RUN-<n> started: building <slice> into <project> with <pattern> (<pattern_reason>) (operation OP-<m>)`;
  the part `with <pattern> (<pattern_reason>)` is read from the run (`getRun`) and left out for a run
  that records no `pattern_reason`. Each entry of the run's `not_used` then prints one line `Not
  used: <captured pattern>: <reason>`, `<reason>` being the entry's `detail` (for
  `refused-value`, `<parameter>: <detail>`), escaped as store text.
- `replay`: `RUN-<n> started: replaying <run> into <project> (operation OP-<m>)`, `RUN-<n>` the
  operation's target, the new run
- `resume`: `RUN-<n> resumed (operation OP-<m>)`

## Following a run

`build`, `resume` and `replay` then follow the run, and so does `run <run>` while the run is
`running`. Following prints the run's record from its first event (also for `resume`, so the
record before the interruption is shown again), one line per event in `seq` order, on standard
output, then one end line, and returns when the run is no longer `running`.

It reads `listRunEvents` page by page, each time with the last `next_page_token`. A page with
no new event is followed by a wait, 100 ms at first and doubling to at most 2 seconds, reset by
a page with events (at most a few requests a second, well under
`x-limits.requests-per-second-per-token`). After a page with no new event it reads the run
(`getRun`). Once the run's status is not `running`, it reads the remaining events, reads the run's
operation (`getOperation`) and ends: an operation that `failed` prints its problem and exits 4;
otherwise it prints the end line and exits as "Run states and exit codes" says. A read with no
answer, or a 502, 503 or 504, is retried as above; when the retries are lost it exits 1 with
`engine: lost the engine while following RUN-<n> (<error>); the run goes on or is interrupted:
see run RUN-<n>`.

**Ctrl-C and SIGTERM detach** (owner, 2026-11-14): the first SIGINT (Ctrl-C) or SIGTERM stops
following, never the run. It exits 130 for SIGINT and 143 for SIGTERM (128 + the signal's number);
below, "Ctrl-C" stands for either signal and "130" for its code. The client prints on standard error `stopped following RUN-<n>; it continues.
Follow it with run RUN-<n>, cancel it with cancel RUN-<n>.` and exits 130. A signal that arrives after the command already has its result (an end line, a refusal, a comparison) does not replace it: the command keeps its own output and exit code. Ctrl-C after the
mutating call was sent and before its answer arrived prints `stopped; the request may have been
recorded: see runs` and exits 130. Ctrl-C while the client follows any other operation (a Story 1
command, `take-over`, `cancel`) also detaches and exits 130, naming the operation.

### Event lines

Each event is one line of fields separated by two spaces. A field the event does not carry is
left out; a list is comma-separated, without spaces; hashes are printed in full (64 hex digits).

```text
<seq>  <at>  <kind>[  <step>][  attempt <n>][  round <n>][  <fields of the kind>][  refs <ref>,...][  <free text>]
```

| Kind | Fields of the kind, in this order | Free text |
|------|-----------------------------------|-----------|
| `RunStarted` | `slice <slice>  pattern <pattern>[  reason <pattern_reason>][  proof][  source <source_run>][  parameters <name>=<value>,...]`, the parameters in byte order of their names (also on the run page) | `detail` |
| `AttemptMade` | `worker <worker>  model <model>  agent <agent_digest>  inputs <hash>,...  artifacts <hash>,...  reuse <reuse_key>  tokens <n>  cost $<usd>` | `detail` |
| `StepAccepted` | as `AttemptMade` without `tokens` and `cost`, then `changed <file>:<line>,...` | `detail` |
| `ResultReused` | `artifacts <hash>,...  reuse <reuse_key>[  open round]  changed <file>:<line>,...`; `open round` only with `open_round` | the check's reason, then `detail` |
| `CheckPassed`, `CheckFailed` | `<check name>` | the check's reason, then `detail` |
| `StepRejected` | `tokens <n>  cost $<usd>` | `detail` |
| `FindingRecorded` | `blocking` or `suggestion`, then `<rule>  <file>:<line>  check <check>` | the finding's text, then `detail` |
| every other kind | none | `detail` (for `RunHandedOver`, the reason) |

`<at>` is the event's time as the engine gives it (RFC 3339). `<usd>` has four decimals. Free
text comes last on its line, two parts separated by two spaces, and is escaped as "Store text in
the terminal" says. For example:

```text
14  2026-11-14T10:02:31Z  CheckPassed  tests  TestsFailAgainstStub  refs R-1,E-1  the tests fail against the stub
```

### End lines

| The run | End line on standard output |
|---------|-----------------------------|
| `ended`, outcome `done` | `RUN-<n> ended done` |
| `ended`, any other outcome | `RUN-<n> ended <outcome>: <reason>` (`: <reason>` left out without one) |
| `waiting-for-developer` | `RUN-<n> waits for the developer: <reason>; it expires <expires_at>. Take it over with take-over RUN-<n>, or cancel it with cancel RUN-<n>.` |
| `interrupted`, waiting for its step workers (the run's `waits_for` is `step-workers`) | `RUN-<n> is interrupted: <reason>; it expires <expires_at>. It continues by itself once its step workers run: start them with ap build-workers --resume RUN-<n>, or cancel it with cancel RUN-<n>.` |
| `interrupted`, any other (`waits_for` `resume` or absent) | `RUN-<n> is interrupted: <reason>; it expires <expires_at>. Resume it with resume RUN-<n>, or cancel it with cancel RUN-<n>.` |

## Trial sets

Feature 012. A trial set is `<k>` fresh builds of one approved slice, run one after another, each a
run of its own (engine contract, `startTrialSet`); `cancel <operation>` with the set's operation
cancels the running trial and starts no other. `trials` prints `TS-<n>  operation OP-<m>` once the
set is accepted, then follows it as `trial-set` does. While following, it prints one line per
trial as the trial ends, in trial order:

```text
trial <i>  RUN-<n>  <outcome>
```

When the set ends, `trial-set <id>` (and `trials`) prints, in this order:

1. `TS-<n>  slice <slice>  state <state>[  stopped <stopped_because>]  done <done>/<ended>[  unstable][  methods mixed]`;
2. a table of the trials, one row per started trial in trial order, with the columns `RUN`,
   `OUTCOME`, `ATTEMPTS`, `ROUNDS`, `SECONDS`, `TOKENS` and `COST` (the trial's totals);
3. a table of the steps, one row per step of the pattern in step order, with the columns `STEP`,
   `REACHED`, `ATTEMPTS`, `ROUNDS`, `SECONDS`, `TOKENS`, `COST` (each `<lowest>/<median>/<highest>`)
   and `FILES` (`same`, or the groups of trials with equal files as `1,2|3`);
4. for an `ended` or `stopped` set whose slice has a previous one (`previous`), `compared with
   TS-<m>: done <done>/<ended> -> <done>/<ended>[  less reliable][  methods differ]`.

A value the engine does not give (not available, or a trial not ended) is `-`. Tables are aligned
as `slices` aligns its table. Seconds are whole seconds, costs `$<usd>` with two decimals, and a
median that falls between two whole values keeps one decimal.

`trial-sets <slice>` reads every page of `listTrialSets` and prints one table, newest set first,
with the columns `SET`, `STATE`, `TRIALS`, `DONE` (`<done>/<ended>`), `UNSTABLE` (`unstable` or
`-`), `METHOD` (its first 12 hex digits) and `CREATED`. With no sets it prints `No trial sets yet.
Start one with trials <slice> <prefix>.` Either way it exits 0; an unknown slice is exit 4
`NotFound`.

`trials` and `trial-set` end with the code of the set's state when they return:

| How it ends | Exit |
|-------------|------|
| `ended` or `stopped`, at least one trial ended and every ended trial is `done` | 0 |
| `ended` or `stopped`, some ended trial is not `done`, or none ended | 7, as `compare`'s "differ": the builds do not agree with done |
| `cancelled` | 6 |
| refused (a problem answer), or the set's operation `failed` | 4, with the problem |
| the engine lost or unreadable | 1 |
| Ctrl-C (detached; the set continues), naming the operation | 130 |
| SIGTERM (detached; the set continues) | 143 |
| a usage error (a malformed slice or set ID, `<k>` outside 1 to 5) | 3 |

## Run states and exit codes

`build`, `resume`, `replay` and `run` end with the code of the state the run is in when they
return; an error before that has its own code.

| How it ends | Exit |
|-------------|------|
| `ended`, outcome `done` | 0 |
| `waiting-for-developer` | 2 |
| `interrupted` | 2 |
| `ended`, outcome `handed-over` | 2 |
| `ended`, outcome `expired` | 5 |
| `ended`, outcome `cancelled` | 6 |
| the run's operation `failed` | 4, with its problem |
| refused before a run starts or continues (a problem answer) | 4, with the problem |
| a proof's run (`proof` true) that ended `done` and passed | 0 |
| a proof's run that failed: handed over, expired, cancelled, or ended `done` with differing files | 1, over the run state's own code (2, 5, 6 or 0) |
| the engine lost or unreadable (retries lost, an answer that cannot be read) | 1 |
| Ctrl-C (detached; the run continues) | 130 |
| SIGTERM (detached; the run continues) | 143 |
| a usage error (a malformed run ID, a missing argument) | 3 |

After a proof's run (`build` of a proof slice, `prove`, `replay` of a proof run, or `run` of one), the
client reads the captured pattern (`getCapturedPattern`) and prints, after the end line, `Proof of
<id>: passed`, `Proof of <id>: failed: <failure>; ...` (each failure as `<step> <check>:
<reason>`, `<step> <path>: <reason>` or `<reason>`), or, while its run is interrupted, `Proof of
<id>: running`, which exits with the run state's code.

A run is `interrupted` only while it waits for the developer (the model was unavailable on every
attempt) or for its step workers (`ap build-workers --resume <run>` registers them); a platform
stop alone does not leave a run `interrupted`, because the run continues by itself once the
platform is up again. For a run that waits for its step workers, the launcher's `resume` exits 3
naming `ap build-workers --resume <run>` (see the launcher's `resume` paragraph).

`interrupted` is 2 because, like `waiting-for-developer`, the run is kept, not lost, and goes on
only when the developer acts; the end line names the command (`resume`, `take-over`, or `ap build-workers --resume` for a run whose `waits_for` is `step-workers`, which the client reads from the engine's run, never from the reason's wording), and `run`
shows the status, so no third code is needed. It is not 1: nothing failed, and the run's operation
is still running.

## Inspecting runs

`run <run>` prints one header line, then every event of the record, then the end line. It reads
every page of `listRunEvents` until a page comes back without `next_page_token`, or, for a run
that has not ended (whose last page keeps a token), until a page brings no new event; a run still
`running` is then followed as above. The header is:

```text
RUN-<n>  slice <slice>  pattern <pattern>[  reason <pattern_reason>][  proof][  source <source_run>]  project <project>  operation OP-<m>  created <created_at>[  replay of RUN-<k>]
```

So the record shows each step's name, the full hash of every artifact, the name of every check
and the goals, rules and examples each concerns (`refs`, such as `R-1`). It exits as "Run states
and exit codes" says (0 for a `done` run); an unknown run is exit 4 `NotFound`.

`runs` reads every page of `listRuns` and prints one table, newest run first (the engine's
order), with the columns `RUN`, `SLICE`, `STATUS`, `OUTCOME`, `PROJECT`, `CREATED` and `REPLAY OF`,
aligned as `slices` aligns its table; an empty cell is `-`. With no runs it prints `No runs yet.
Start one with build <slice> <project>.` Either way it exits 0. It takes no arguments.

`compare <run> <other>` reads every page of `compareRuns` (`with=<other>`). Runs that do not differ
print `<run> and <other> do not differ.` and exit 0. Runs that differ print `<run> and <other>
differ in <k> place(s):`, then a table with the columns `STEP`, `FIELD`, `<run>` and `<other>`,
one row per difference in the engine's order (step order), each value as the engine gives it
(`left` and `right`; `-` when absent, escaped as store text), and exit 7 (owner, 2026-11-14). An
unknown run on either side is exit 4 `NotFound`; a run compared with itself does not differ.

`artifact <run> <hash>` writes the artifact's content to standard output, byte for byte, with
nothing before or after it, and exits 0. `<hash>` must be 64 lowercase hex digits (else exit 3).
An unknown run or artifact, and one that has expired or was erased with its slice, is exit 4
`NotFound`, with the problem on standard error and nothing on standard output.

## Taking over

`take-over <run>` sends `acceptHandOver`, follows its operation until it ends, reads the run, and
prints `RUN-<n> ended handed-over: <reason>. The project <project> is yours to finish.` It exits
0: the developer asked for this, and nothing more is waiting. A run that is not
`waiting-for-developer` is refused by the engine (exit 4, `refused: RunNotWaiting: ...`), and an
unknown run is exit 4 `NotFound`; the client does not check the status first.

## The test harness

The planted-failure scenarios are test harness code in `src/tests/`, run with the launcher's
`test <suite>`, not client commands. The suites are:

| Suite | Runs |
|-------|------|
| `planted` | `TestPlanted` in `src/tests/acceptance` (SC-003): five scenarios, 3 trials each |
| `us2` | the `TestUS2` tests in `src/tests/acceptance` (spec US2.1 and US2.7 to US2.10; US2.2 to US2.6 are judged by `planted`) |

Another name exits 3 with `test: unknown suite <name>; the suites are planted, us2`. `test planted <scenario>` runs only that scenario's 3 trials, so CI can run the scenarios
on parallel runners (ADR 0074); its output is the same lines for that one scenario, and the total
counts it alone (`planted: 1 of 1 scenarios stopped in 3 out of 3 trials`). A scenario name that
is not words of lowercase letters and digits joined by single hyphens (`^[a-z0-9]+(-[a-z0-9]+)*$`), a second argument, or a scenario given to
`us2` exits 3; a well-formed name that is no scenario of the suite runs no test and exits 1 with
`test: no test of the planted suite ran`. For `planted`,
standard output has one line per trial and one per scenario, then a total, for example:

```text
PASS  edit-judging-tests  trial 1
PASS  edit-judging-tests  trial 2
PASS  edit-judging-tests  trial 3
PASS  edit-judging-tests  stopped in 3 out of 3 trials
...
planted: 5 of 5 scenarios stopped in 3 out of 3 trials
```

A trial that fails is `FAIL  <scenario>  trial <n>`, followed by its test's messages, each
indented by four spaces; its scenario is then `FAIL  <scenario>  stopped in <k> out of 3 trials`.
`us2` prints one `PASS` or `FAIL` line per test, with the failing test's messages below it. The
exit code is 0 when every test passes (for `planted`: every scenario stopped in 3 out of 3
trials), 1 when one fails or the suite does not build (naming the build's error), and 3 for a
usage error. A trial is `TestPlanted/<scenario>/trial-<n>`, a subtest of its scenario's subtest.

How the suites run (owner, 2026-11-14, research D33, ADR 0037): `test <suite>` builds the suite's
test binary in the pinned Go container (`images.lock`'s `golang`), offline (`--network none`,
`GOPROXY=off`, `GOFLAGS=-mod=readonly`), with the module cache in
`${XDG_CACHE_HOME:-~/.cache}/agentic-platform/go`, with `-tags docker`, as `scripts/check.sh` builds
its `docker` suites, then runs that binary on the host, in the suite's package folder of the
repository the launcher is in, with `-test.run` selecting the suite (`^TestPlanted$` for `planted`,
`^TestPlanted$/^<scenario>$` for `planted <scenario>`, `^TestUS2` for `us2`) and `-test.v=test2json`, and prints the lines
above from its events. The suites start their own labs with the container engine, so no container
holds the container engine's socket and no tests image is built. The binary uses the developer's
own Docker access on the host, an owner-approved exception to the constitution's test-security
rule (plan, Exceptions to Engineering Principles). `test` needs no started session: the suites
build the engine from source and start only `images.lock`'s pinned images, never the session's.
