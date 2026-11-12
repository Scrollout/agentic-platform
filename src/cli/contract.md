# Terminal contract

Two names, two jobs:

- **The launcher** `src/deploy/ap` runs on the host and only calls `docker compose` (research D6).
  Its own commands are `up --credentials <file> --projects <folder>`, `down`, `build-workers
  <slice>`, `page`, `test <suite>` (runs the test harness in its own container) and `registry`
  (starts the local image registry if it isn't running; `src/deploy/build.sh` calls it). It intercepts
  `build`, `resume` and `replay`: it runs `build-workers` for the slice (for a run, the run's slice)
  first, then passes the command on. Every other command is passed unchanged to the terminal
  client, which never calls the launcher.

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
  but `up`, `down` and `registry` needs a started session (exit 3 without one). A command whose parts do not exist yet
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
- **The terminal client** runs in its own container and is a client of the engine API
  ([openapi.yaml](../engine/api/preview/2026-11-12-preview/openapi.yaml)) with the client token. It adds no behaviour
  of its own.

Exit codes: `0` success, `1` a check or approval failed (for `up`: an image is missing or failed
verification, or Compose failed; for `down` and `registry`: Docker failed), `2` the run waits for the developer or was
handed over, `3` usage error, `4` refused by the engine (the problem's `code` is printed), `5` the
run expired, `6` the run or operation was cancelled.

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
| `take-over <run>` | Accepts the hand-over of a run waiting for the developer | `acceptHandOver`, `getOperation` |
| `compare <run> <other>` | Shows the differences between two runs | `compareRuns` |
| `runs` / `run <run>` | Lists runs or shows one run's record | `listRuns`, `getRun`, `listRunEvents` |
| `artifact <run> <hash>` | Writes an artifact's content to standard output | `getArtifact` |
| `slices` | Lists slices | `listSlices` |
| `cancel <run\|operation>` | Cancels a run or other running work | `getRun` (for a run), `cancelOperation`, `getOperation` |
| `delete <slice> [--yes]` | Erases a slice and its records after confirmation | `getSlice`, `listRuns`, `deleteSlice`, `getOperation` |

When a slice fails a pattern condition (`ContractIsOpenApi31`, `OnlyHttpOperations`,
`ContractRefsLocal` or `RulesNameOneSystem`), `describe` and `show` (without a part) also print one
line: `The pattern does not fit this description: <condition>: <reason>; ...`, naming each failing
condition and its reason. A slice that fails no pattern condition prints no such line, also when
another check fails.

`delete` first shows what it erases: the slice's ID and title, its runs and the number of stored
artifacts, and that project folders are kept. It then asks the developer to type the slice ID, and
erases nothing on any other answer. `--yes` skips the question, for scripts.

The planted-failure scenarios are test harness code in `src/tests/`, run with the launcher's
`test planted`, not client commands.
