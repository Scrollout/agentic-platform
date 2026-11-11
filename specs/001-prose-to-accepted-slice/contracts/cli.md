# Terminal contract

Two names, two jobs:

- **The launcher** `src/deploy/ap` runs on the host and only calls `docker compose` (research D6).
  Its own commands are `up --credentials <file> --projects <folder>`, `down`, `build-workers
  <slice>`, `page` and `test <suite>` (runs the test harness in its own container). It intercepts `build`: it runs `build-workers` for the slice first, then passes the command on.
  Every other command is passed unchanged to the terminal client, which never calls the launcher.
- **The terminal client** runs in its own container and is a client of the engine API
  ([engine-api.openapi.yaml](engine-api.openapi.yaml)). It adds no behaviour of its own.

Exit codes: `0` success, `1` a check or approval failed, `2` handed over to the developer,
`3` usage error, `4` refused by the engine (the problem's `code` is printed).

| Command | Does | API operations |
|---------|------|----------------|
| `describe` | Reads the description and proposes a slice | `createSlice`, `getOperation`, `getSlice` |
| `questions <slice>` | Shows open questions and records the answers | `getSlice`, `answerQuestions`, `getOperation` |
| `show <slice> [goals\|rules\|contract]` | Shows a part, its hash and its check results | `getSlice` |
| `edit <slice> <target>` | Opens a part in the editor and saves the change | `getSlice`, `changePart`, `getOperation` |
| `approve <slice> <part>` | Approves a part at the hash just shown | `getSlice`, `approvePart`, `getOperation` |
| `build <slice>` | Builds and follows the run, after the launcher has started the slice's workers | `startRun`, `getOperation`, `listRunEvents` |
| `resume <run>` / `replay <run>` | Continues an interrupted run, or replays a finished one | `resumeRun` or `replayRun`, `getOperation`, `listRunEvents` |
| `compare <run> <other>` | Shows the differences between two runs | `compareRuns` |
| `runs` / `run <run>` | Lists runs or shows one run's record | `listRuns`, `getRun`, `listRunEvents` |
| `slices` | Lists slices | `listSlices` |
| `cancel <operation>` | Cancels running work | `cancelOperation` |
| `delete <slice>` | Erases a slice and its records | `deleteSlice`, `getOperation` |

The planted-failure scenarios are test harness code in `src/tests/`, run with the launcher's
`test planted`, not client commands.
