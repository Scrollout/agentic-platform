# Terminal contract

Two names, two jobs:

- **The launcher** `src/deploy/ap` runs on the host and only calls `docker compose` (research D6),
  except `test`, which builds a suite in the pinned Go container and runs its test binary on the
  host ([ADR 0037](../../docs/decisions/0037-terminal-run-commands-exit-codes-and-test-on-host.md)).
  Its own commands are `up --credentials <file> --projects <folder>`, `down`, `build-workers
  <slice> <project>`, `page`, `test <suite>` (runs a test suite, see "The test harness") and `registry`
  (starts the local image registry if it isn't running; `src/deploy/build.sh` calls it). It intercepts
  `build`, `resume` and `replay` (research D32):
  - `build <slice> <project>` runs `build-workers <slice> <project>` first, then passes the command on.
  - `replay <run> <project>` reads the run (`getRun`) for its slice, runs `build-workers <slice>
    <project>` unchanged, then passes the command on. A replay always builds into a new, empty
    project: a folder that holds a file, a symbolic link or a non-empty `.git` is refused by
    `build-workers` (exit 3) as for `build`. An unknown run is exit 4 (`NotFound`).
  - `resume <run>` never runs `build-workers`. It reads the run (`getRun`), which gives its slice and
    project; an unknown run is exit 4 (`NotFound`), and a run that is not `interrupted` is passed on
    unchanged for the engine to refuse (`RunNotInterrupted`). For an interrupted run, every step of
    the pattern must have its step container for that slice and project running (by the labels
    below and this session's tokens); the launcher then registers each with the engine again and
    passes the command on. When one is missing or stopped, or the slice's tokens are gone (after
    `ap down` or a host reboot), it changes nothing and exits 3 with `resume: the step workers of
    <slice> for the project <project> are not running; run ap build-workers --resume <run>, then ap
    resume <run>`.

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
  and registers them; running it again changes nothing. It exits 4 for an unknown run (`NotFound`)
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
  `AP_PAGE_PORT`, or the session's port, or 7878, and `page` prints its address. `down` works with
  a missing or broken session file. The session is kept, readable only by the developer,
  in `${XDG_STATE_HOME:-~/.local/state}/agentic-platform/session.env` (folder mode 700, file mode
  600) until `down`, also after an `up` that failed. A session file that is not a regular file
  owned by the developer with mode 600 is never read: every command but `down` refuses it (exit 1,
  telling the developer to run `down`), and `down` stops the platform without it. Every command
  but `up`, `down`, `registry` and `test` needs a started session (exit 3 without one). A command whose parts do not exist yet
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
| `build <slice> <project>` | Builds into the new folder `<project>` under the projects folder and follows the run | `startRun`, `getOperation`, `listRunEvents` |
| `resume <run>` | Continues an interrupted run and follows it | `resumeRun`, `getOperation`, `listRunEvents` |
| `replay <run> <project>` | Replays an ended run into the new folder `<project>` and follows it | `replayRun`, `getOperation`, `listRunEvents` |
| `take-over <run>` | Accepts the hand-over of a run waiting for the developer | `acceptHandOver`, `getOperation`, `getRun` |
| `compare <run> <other>` | Shows the differences between two runs | `compareRuns` |
| `runs` / `run <run>` | Lists runs, or shows one run's record and follows it while it runs | `listRuns`, `getRun`, `listRunEvents`, `getOperation` |
| `artifact <run> <hash>` | Writes an artifact's content to standard output | `getArtifact` |
| `slices` | Lists slices | `listSlices` |
| `cancel <run\|operation>` | Cancels a run or other running work | `getRun` (for a run), `cancelOperation`, `getOperation` |
| `delete <slice> [--yes]` | Erases a slice and its records after confirmation | `getSlice`, `listRuns`, `deleteSlice`, `getOperation` |

When a slice fails a pattern condition (a check result with `pattern` true; a result without
`pattern` counts as false), `describe` and `show` (without a part) also print one line: `The pattern does not fit this description: <condition>: <reason>; ...`, naming each failing
condition and its reason. A slice that fails no pattern condition prints no such line, also when
another check fails.

`delete` first shows what it erases: the slice's ID and title, its runs and the number of stored
artifacts, and that project folders are kept. It then asks the developer to type the slice ID, and
erases nothing on any other answer. `--yes` skips the question, for scripts.

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
mutating call (`startRun`, `resumeRun`, `replayRun` or `acceptHandOver`).

When that call gets no answer (a connection error or the client's 2-minute timeout), or answers
502, 503 or 504, the client sends the same request with the same key and body again, up to 3
more times, after 1, 2 and 4 seconds, each with up to half again of random jitter (research
D7); a 429 waits its `Retry-After` first. The engine returns the first result for a key it has
kept (24 hours, `x-limits.idempotency-key-retention-hours`), so a retry never starts a second
run. When every try is lost the client exits 1 with `engine: no answer to <operation> after 4
tries (Idempotency-Key <key>); it may have been recorded: see runs`. Any other answer is final:
a problem is a refusal (exit 4).

The first line on standard output after the 202, before any event is read, names the run and its
operation:

- `build`: `RUN-<n> started: building <slice> into <project> (operation OP-<m>)`
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
| `RunStarted` | `slice <slice>  pattern <pattern>` | `detail` |
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
| `interrupted` | `RUN-<n> is interrupted: <reason>; it expires <expires_at>. Resume it with resume RUN-<n>, or cancel it with cancel RUN-<n>.` |

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
| the engine lost or unreadable (retries lost, an answer that cannot be read) | 1 |
| Ctrl-C (detached; the run continues) | 130 |
| SIGTERM (detached; the run continues) | 143 |
| a usage error (a malformed run ID, a missing argument) | 3 |

`interrupted` is 2 because, like `waiting-for-developer`, the run is kept, not lost, and goes on
only when the developer acts; the end line names the command (`resume` or `take-over`), and `run`
shows the status, so no third code is needed. It is not 1: nothing failed, and the run's operation
is still running.

## Inspecting runs

`run <run>` prints one header line, then every event of the record, then the end line. It reads
every page of `listRunEvents` until a page comes back without `next_page_token`, or, for a run
that has not ended (whose last page keeps a token), until a page brings no new event; a run still
`running` is then followed as above. The header is:

```text
RUN-<n>  slice <slice>  pattern <pattern>  project <project>  operation OP-<m>  created <created_at>[  replay of RUN-<k>]
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

Another name exits 3 with `test: unknown suite <name>; the suites are planted, us2`. For `planted`,
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
`^TestUS2` for `us2`) and `-test.v=test2json`, and prints the lines
above from its events. The suites start their own labs with the container engine, so no container
holds the container engine's socket and no tests image is built. The binary uses the developer's
own Docker access on the host, an owner-approved exception to the constitution's test-security
rule (plan, Exceptions to Engineering Principles). `test` needs no started session: the suites
build the engine from source and start only `images.lock`'s pinned images, never the session's.
