# Tasks: The analyst passes its quarantined eval cases every time

**Input**: Design documents from `specs/005-analyst-reliability/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [quickstart.md](quickstart.md)

**Tests**: Required. Outside-in, test-first: new tests are seen failing before the code; every
new check has a clean and a planted case.

**Organization**: US1 (kept outputs) first; the harness inputs (Phase 3) serve US2 and US3, whose
cause and fix come from one diagnostic run; then the quarantine goes and the evidence runs.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2 or US3 (spec.md)

## Rules for every task

- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; new tests go in new files. The code author never changes a test, a fixture
  or test data.
- **Existing tests**: no standing approval. Each change to an existing test file, helper, fixture
  or eval case is asked for, quoting what changes and why, before it is briefed, and its commit
  carries `Tests-approved-by: Marius Gologan (<date>)`. A task that names one says "(approval)".
- **Protected paths**: `scripts/`, `.github/`, every test file and the analyst's instruction need
  the owner's `Approved-by:`; no dependency is added.
- **Test time**: locally only the new and changed test files, `run.sh graders` (cap 5 minutes)
  and `run.sh stub` (cap 10 minutes, a gateway stub and one container at a time); no
  `scripts/check.sh` full run, other Docker lab or image build. The full test command is the CI
  run of the pushed commit; each push is told to the owner (which jobs, how long) and waits for a
  go-ahead. Each live run (T011, T016, T018) needs the owner's go-ahead.
- **Briefs**: every test-author and reviewer brief says: think through the edge cases and list
  those checked.
- **Fresh review**: US1 and the feature end with a fresh review by a read-only agent on another
  model than the author's, in its own worktree with the uncommitted and untracked files, judging
  the CI run; at most 3 rounds. Then `/speckit-converge`.
- **Gate**: `/speckit-analyze` (fresh reviewer) must report no CRITICAL or HIGH before T001, and
  again after any change to spec, plan, tasks or a design artifact. Round 1 (fresh Sonnet, at
  a6f4ae8): 2 HIGH (A1 the upload condition needs parentheses; A2 `quarantine_test.go` pins the
  quarantine), 6 MEDIUM, 5 LOW; all fixed in spec (FR-010, an edge case, the Assumption), plan,
  research, data model, quickstart, ADR 0078 and these tasks; A4 closed backlog T191 (wrong
  premise); A13 (10 of 10 is weak evidence) noted in the spec, the owner's bar stands. Round 2
  (fresh Sonnet, at e7fb76c, the fixed lines only): all 13 closed, 0 CRITICAL/HIGH; one LOW (the
  dependency line omitted T015) fixed. Gate passed.

---

## Phase 1: Setup

- [X] T001 (owner approved (a), (b) and (c), 2026-11-22: "Yes") Ask the owner for the existing-test approvals the plan lists, each quoted: (a)
  `scripts/tests/test_workflows_eval_artifacts.py`, rules `scan_step` and `upload_condition`
  accept the scan's `failure() || steps.failed.outputs.failed == 'true'` and the upload's
  `(failure() || steps.failed.outputs.failed == 'true') && steps.scan.outcome == 'success'`,
  their planted violations kept (research R2); (b)
  `src/tests/evals/analyst/harness_evals_test.go` and `revision_evals_test.go` take their case
  selection and k from the new `inputs.go`, and `print` derives k from each case's trial count
  (research R3); (c) for T015: `Quarantine: "T185"` removed from `cases.go` and `revision.go`, and
  `quarantine_test.go`'s `want` becomes empty (no case quarantined); record each answer in this
  file. A grader change (T013, T014) is asked for when its cause is known

---

## Phase 2: User Story 1 - A failed eval trial can be read afterwards (Priority: P1)

**Goal**: every eval job with a failed trial, counted or quarantined, keeps its scanned report
folder for 7 days; the folder's path is one the upload accepts.

**Independent Test**: `python3 -m unittest` on the three files below: the rules keep the folder
for a planted failed counted trial and a planted failed quarantined trial, keep nothing when
every trial passed, and refuse a `.` or `..` segment.

- [X] T002 [P] [US1] Test author: new `scripts/tests/test_ci_evals_failed_trials.py` for
  `scripts/ci-evals-failed-trials.py <dir>` (research R2, data model "has a failed trial"):
  writes `failed=true` to `$GITHUB_OUTPUT` when any `trials[].passed` is `false` in any
  `report.json` under `<dir>` (top level and `revision/`), a quarantined case's included, or a
  `report.json` cannot be read or has an unexpected shape (not a list of cases; a case without a
  non-empty `trials` list; a trial without a boolean `passed`), each with a planted case;
  `failed=false` when every trial passed or no report exists; prints
  the reports read and each failed trial (`<case> trial <n>`); exit 0 in all these cases, 2 on a
  wrong argument count; without `$GITHUB_OUTPUT` it prints the line to standard output
- [X] T003 [P] [US1] Test author: new `scripts/tests/test_workflows_eval_outputs_kept.py` (helpers
  of `test_workflows.py` imported unchanged), each rule with a test on the real files and a
  planted violation: `EVAL_REPORT_DIR`'s value in `evals.yml`'s job env has no `.` or `..` path
  segment; a step with `id: failed` and `if: always()` runs
  `python3 scripts/ci-evals-failed-trials.py "$EVAL_REPORT_DIR"` after the step `run` and before
  the scan; the scan's `if` is `failure() || steps.failed.outputs.failed == 'true'`; the upload's
  `if` is `(failure() || steps.failed.outputs.failed == 'true') && steps.scan.outcome ==
  'success'`, with planted violations for the form without parentheses and for an upload that
  would run after a scan match on a green job
- [X] T004 (owner also approved the fixtures SCAN_STEP, UPLOAD_STEP and two quote-form tests moving to the new conditions, 2026-11-22) [US1] Test author (approval T001a): `scripts/tests/test_workflows_eval_artifacts.py`
  rules `scan_step` and `upload_condition` accept exactly the new conditions; their planted
  violations still fail; the module docstring says so
- [X] T005 [US1] Code author: `scripts/ci-evals-failed-trials.py` (T002) and
  `.github/workflows/evals.yml`: `EVAL_REPORT_DIR: ${{ github.workspace }}-eval-report` (research
  R1), the `failed` step, the scan and upload conditions (T003, T004); its comments cite ADR 0078;
  T002 to T004 and `scripts/tests/test_workflows*.py` pass
- [X] T006 (CI run 45007221855 at 1976485 green, 26 of 26 jobs) [US1] Push (owner go-ahead; say which jobs run) and fix until CI is green; record the
  run in research
- [ ] T007 [US1] Fresh review of US1 (rules above); findings fixed or recorded as tasks; then
  `/speckit-converge` for US1

**Checkpoint**: a failed eval trial in CI leaves a readable artifact.

---

## Phase 3: Harness inputs (serves US2 and US3)

**Purpose**: one live run of the two cases, with 10 trials each (research R3).

- [X] T008 [P] Test author (FR-010): new `src/tests/evals/analyst/inputs_test.go` (no build tag, no
  Docker) for two functions in a new `inputs.go`: `Selection(env string) (cases []Case,
  revisions []RevisionCase, err error)`: empty `env` gives every case of both suites; a
  comma list (spaces around a name trimmed, duplicates collapsed) gives the named cases of each
  suite in suite order; a name in neither suite, or an empty or blank name, is an error naming
  it; and `TrialCount(env string) (int, error)`: empty gives `Trials` (3); a value matching
  `^[1-9][0-9]?$` and at most 20 is k (`1` and `20` pass); anything else (`0`, `21`, `-1`, `+5`,
  `010`, a leading space, `1e1`, `3.5`, `x`) is an error
- [X] T009 Harness author, a test author other than T008's (approval T001b; the harness is test
  code, so no code author touches it): `src/tests/evals/analyst/inputs.go` (T008);
  `harness_evals_test.go` and `revision_evals_test.go` use `Selection(os.Getenv("EVAL_CASES"))`
  and `TrialCount(os.Getenv("EVAL_TRIALS"))`, fail before any trial on an error, skip a test
  whose suite has no named case; `print` keeps its signature and prints `pass^<k>` with k from
  the cases' trial counts, so `quarantine_print_evals_test.go` is unchanged; `README.md` documents `EVAL_TRIALS` and the
  cross-suite `EVAL_CASES` and the limit (`run.sh live`'s 60 minutes fit the full suite only up to
  k of about 4); `run.sh graders` and `run.sh stub` pass
- [X] T010 Commit T008 and T009; they are pushed with T017 unless the owner wants CI first

---

## Phase 4: User Stories 2 and 3 - cause and fix (Priority: P1)

**Goal**: `example` and `revise-inject-approvals-field` pass every trial.

**Independent Test**: T016's 10-trial run passes 10 of 10 per case.

- [X] T011 [US2] [US3] Diagnostic run (owner go-ahead, about $0.20, about 6 minutes), on the host:
  `EVAL_CASES=example,revise-inject-approvals-field EVAL_TRIALS=10 sh
  src/tests/evals/analyst/run.sh live`; keep the report folder outside the repository
- [X] T012 (closed: T011 showed no failure in 20 trials; the owner chose to remove the quarantine without a fix, 2026-11-22; research R4) [US2] [US3] Read every failed trial's answer and events; record in research R4, per
  case, the cause, the evidence (trial, grader, the lines of the answer that show it) and whether
  the analyst or the grader is wrong (FR-004). No failure in 10 trials of a case: ask the owner
  (research R4)
- [X] T013 (closed: no failing answer, so no cause to fix; FR-004/FR-005 leave the analyst and graders unchanged) [US2] Fix `example` at its cause (FR-005): by a code author, the instruction
  (`src/agents/analyst/instruction.md`, generic, never the eval's own description or values); or,
  if the grader misreads a correct proposal, by a test author, `content.go`'s `Content` with a new good proposal
  that shows order the way the failing answer did and a planted one with no order (approval asked
  first; FR-006); `run.sh graders` passes
- [X] T014 (closed: as T013) [US3] Fix `revise-inject-approvals-field` at its cause, as T013 (the instruction's
  "Revising a proposal" by a code author, or a revision grader with its planted cases by a test
  author; approval for a grader)
- [X] T015 (also, owner-approved 2026-11-22: `scripts/tests/test_eval_quarantine_open_task.py` skips `_test.go` fixtures and requires the case declarations to be examined) [US2] [US3] Test author (approval T001c): remove `Quarantine: "T185"` from `cases.go`
  and `revision.go`, empty `quarantine_test.go`'s `want`, and remove the "Quarantined now" sentence of
  `src/tests/evals/analyst/README.md`; T185 checked in `specs/backlog.md` in the same commit, so
  `scripts/tests/test_eval_quarantine_open_task.py` still passes
- [X] T016 (closed: SC-001's first part was met by T011, 10 of 10 each; the quarantine changes only counting, not the analyst; owner 2026-11-22) [US2] [US3] Evidence run (owner go-ahead, about $0.20), as T011: both cases 10 of 10
  (SC-001, first part); record in research. A failure: back to T012 with the new answer
- [ ] T017 [US2] [US3] Commit T013 to T016; push (owner go-ahead) and fix until CI is green
- [ ] T018 [US2] [US3] Full analyst run (owner go-ahead): `gh workflow run evals.yml --ref
  005-analyst-reliability` (the analyst suite is chosen because its inputs changed since the last
  green evals run; check the `Choose eval suites` matrix); every analyst and revision case pass^3, none quarantined (SC-001,
  SC-003, FR-008); record the run and its cost in research; a failure: back to T012

---

## Phase 5: Polish

- [ ] T019 Docs: `docs/log.md` (implementation), the problem space (N-013 state and spec),
  any concept the shipped behaviour contradicts; `python3 scripts/okf.py check` reports OK; SC-002:
  name the eval job whose failed trial left an artifact, or, if none failed during the feature,
  file a backlog task to check the first one
- [ ] T020 Final fresh review of the whole feature (rules above) and `/speckit-converge`; findings
  fixed or recorded as tasks; FR-009: `git diff main -- src/agents/analyst.yaml
  src/engine/api/preview` shows no model, pin or schema change
- [ ] T021 Before the owner's merge: read the branch's `evals-current` output; no suite pending

---

## Dependencies

- T001 before T004, T009 and T015; a grader change in T013 or T014 waits for its own approval. US1 (T002 to T007) and Phase 3 (T008 to T010) are independent.
- T011 needs T009; T012 needs T011; T013 and T014 need T012 and are independent of each other;
  T015 after T013 and T014; T016 after T015; T017 after T016; T018 after T017 and T006 (so a
  failure keeps its outputs).
- T019 to T021 last.

## Parallel examples

- T002 and T003 (two new test files), and T008, at the same time, by test authors.
- T013 and T014 when both causes are in different files.

## Implementation strategy

US1 first: it is small, needs no model, and makes every later failure readable. Then the harness
inputs and one diagnostic run; the fixes follow the answers, not the guesses in research R4.
