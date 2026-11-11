# Terminal contract

Two names, two jobs:

- **The launcher** `src/deploy/ap` runs on the host and only calls `docker compose` (research D6).
  Its own commands are `up --credentials <file> --projects <folder>`, `down`, `build-workers
  <slice>`, `page` and `test <suite>` (runs the test harness in its own container). It intercepts
  `build`, `resume` and `replay`: it runs `build-workers` for the slice (for a run, the run's slice)
  first, then passes the command on. Every other command is passed unchanged to the terminal
  client, which never calls the launcher.
- **The terminal client** runs in its own container and is a client of the engine API
  ([openapi.yaml](../engine/api/preview/2026-11-12-preview/openapi.yaml)) with the client token. It adds no behaviour
  of its own.

Exit codes: `0` success, `1` a check or approval failed, `2` the run waits for the developer or was
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

`delete` first shows what it erases: the slice's ID and title, its runs and the number of stored
artifacts, and that project folders are kept. It then asks the developer to type the slice ID, and
erases nothing on any other answer. `--yes` skips the question, for scripts.

The planted-failure scenarios are test harness code in `src/tests/`, run with the launcher's
`test planted`, not client commands.
