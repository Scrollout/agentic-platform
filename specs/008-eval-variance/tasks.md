# Tasks: Eval suites show how much their trials vary

**Input**: Design documents from `specs/008-eval-variance/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Required. Outside-in, test-first: new tests are seen failing before the code; every
new rule and script has a clean and a planted case.

**Organization**: the contract (Phase 1), then US1 (passed trials and unstable cases), US2 (the
spread of effort), US3 (kept measurements and the comparison). Each story's code extends the
same scripts, so the stories run one after another.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2 or US3 (spec.md)

## Rules for every task

- **Contract first**: T001 puts the three scripts' contracts in their docstrings and is committed
  alone, before any test or code.
- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; new tests go in new files under `scripts/tests/`, `unittest`, standard
  library only, run by the `py_tests` job on `ci-python`; the scripts also run on the runner's
  `python3` (contract "Python"). A planted credential is built at run time (`"sk-" + "ant-" +
  ...`), never written as a literal, so the `secrets` job's scan of tracked files stays clean. The code author never changes a
  test, a fixture or test data.
- **Existing tests**: only the two files research R2 names change (T011), as the owner approved
  on 2026-11-23; that commit carries `Tests-approved-by: Marius Gologan (<date>)`. Any other
  existing test that turns out to need a change is stopped and asked for, quoting what changes
  and why. `scripts/ci-evals-summary.py` and its tests do not change (FR-009, T192 stays open).
- **Protected paths**: for a change to `.github/workflows/evals.yml` the steps are, in order:
  show the owner the diff with the push question, get the owner's confirmation, commit with
  `Approved-by:` naming the date of that confirmation, then push. No commit carries the trailer
  before the confirmation. New and changed `scripts/ci-evals-*` files, also protected, carry
  `Approved-by:` under the owner's standing approval (owner, 2026-11-23). No path an eval
  suite reads (`src/tests/evals/suites.yaml`) changes, so no push selects a paid suite.
- **No dependency** is added; the scripts use the standard library and `gh`.
- **Never a gate** (FR-007): every new workflow step carries `continue-on-error: true`; every
  new script catches every error past its argument check, says what went wrong and exits 0; exit
  2 only on a wrong number of arguments (contract "What every script promises").
- **Test time**: locally only the new test files and the files they touch
  (`python3 -m unittest scripts/tests/test_<name>.py`, each capped at 5 minutes); no full
  `scripts/check.sh`, Docker lab or image build. The full test command is the CI run of the
  pushed commit; a push changes `.github/**` and `scripts/ci-*`, so every check job runs: each
  push is told to the owner and waits for a go-ahead. A live eval run is paid and needs the
  owner's go-ahead.
- **Briefs**: every test-author and reviewer brief says: think through the edge cases and list
  those checked; it gives a time limit and a cap on full runs.
- **Fresh review**: each story and the feature end with a fresh review by a read-only agent on
  another model than the author's, in its own worktree checked out at the branch commit with the
  uncommitted and untracked files, judging the CI run; at most 3 rounds. Then `/speckit-converge`.
- **Gate**: `/speckit-analyze` (fresh reviewer) must report no CRITICAL or HIGH before T001, and
  again after any change to spec, plan, tasks or a design artifact. Round 1 (fresh Sonnet, at 29e6f1e):
  0 CRITICAL, 4 HIGH (I1 tokens shown as median only; F1 the measurement scan could fail the job;
  F2 the config step could fail the job; F3 uncaught errors and a 0/0 share), 8 MEDIUM, 9 LOW,
  fixed at their source (spec FR-001, FR-002, FR-004, FR-006, FR-007 and edge cases; research R2
  to R4; data model fallbacks; contract; these rules and T003-T018). Round 2 (fresh Sonnet, at 04ca0f1):
  all 4 HIGH fixed; 0 CRITICAL, 0 HIGH; N1, N2 MEDIUM (the scan's warning not in the summary; who
  creates the measurement folder) and N3-N6 LOW fixed in wording (research R2, contract, quickstart,
  these rules, T003, T013, T014). Round 3 (fresh Sonnet, at 9f9020b): all six fixed, no
  regression, no finding. Gate passed.

---

## Phase 1: Setup (contract first)

- [X] T001 Contract (committed alone): new `scripts/ci-evals-measure.py`,
  `scripts/ci-evals-earlier.py` and `scripts/ci-evals-trials.py`, each holding only its docstring
  from [contracts/eval-trials-scripts.md](contracts/eval-trials-scripts.md) (usage, inputs,
  outputs, the section's text, exit codes, the promises every script makes; the measurement's
  fields and their fallbacks as [data-model.md](data-model.md) lists them, "exactly these fields;
  any other is a defect") and a
  body that prints `not implemented` and exits 1, executable like the other `scripts/ci-*`; the
  draft then links to the authoritative copies
- [X] T002 Owner approvals recorded (research R2, 2026-11-23): changes (a) to
  `scripts/tests/test_workflows_eval_artifacts.py` and (b) to
  `scripts/tests/test_workflows_eval_outputs_kept.py`; storage, baseline rule, T192 left out

---

## Phase 2: User Story 1 - Each case shows how often and how evenly its trials passed (Priority: P1)

**Goal**: every eval job keeps a measurement of its report and shows, after the unchanged
summary, each case's passed trials out of k, pass^k with k from the report, and whether it is
unstable (FR-001, FR-002, FR-004's fields, FR-008, FR-009; research R4, R5).

**Independent Test**: T003-T005 pass; quickstart Q1 and Q2 on a planted report with cases at 3/3,
2/3 and 0/3 mark only the 2/3 case unstable, and the measurement holds no other field.

- [X] T003 [P] [US1] Test author: new `scripts/tests/test_ci_evals_measure.py` for
  `ci-evals-measure.py`: the measurement's keys are exactly data-model.md's at each level
  (`version` `1`, `suite`, `cases`, `key` `<suite>` or `<suite>-<cases>`, `commit`, `branch`,
  `run_id`, `run_attempt`, `measured_cases`; per case `case`, `agent`, `quarantined_by` only when
  the report has it, `trials`; per trial `passed`, `seconds`, `attempts`, `input_tokens`,
  `output_tokens`, `cost_usd`, each absent when the report lacks it), and each value breaking its
  rule takes the data model's fallback: `commit` "40 lowercase hex characters (`""`)", `run_id` and
  `run_attempt` "a positive integer (`0`)" (unset, `abc`, `-1`), `suite`, `cases` and `agent`
  "letters, digits and `-` only (`"?"`)" (`cases` "may be empty", as the analyst's is), `case` "at most 200 characters (`"?"`)",
  `quarantined_by` "`^T[0-9]+$` (`"?"`, still quarantined)", `branch` "at most 255 characters, no
  control character (`""`)", tokens and attempts "an integer at least 0", `cost_usd` "a finite
  number at least 0" (`NaN`, `-1`, `"0.1"`, `true` give absent); a case or trial that is not an
  object, and `trials` that is not a list, are handled as the data model says; planted `stderr`, `exit_error`, `results[].reason`,
  `changed_files` and `attack` holding `sk-ant-` followed by 30 letters and answer text never
  appear in the file; "a missing or non-boolean value is `false`"; Go durations `850ms`, `12s`,
  `1m2.5s`, `1h0m0s`, `1.234s` parse to seconds, an unparsable one leaves `seconds` absent; an
  output file's folder that does not exist is created; a report that is not JSON, not a list, or missing, and an output path that cannot be written,
  write no file, print why and exit 0; wrong arguments exit 2; order of cases and trials kept; it
  runs with `PATH` holding no `gh` or `git` (it starts no process)
- [X] T004 [P] [US1] Test author: new `scripts/tests/test_ci_evals_trials.py` for
  `ci-evals-trials.py`'s first table (contract "The section"): `Passed` is `p/n`, with
  `⚠️ unstable` exactly when some trial passed and some failed; `pass^k` names k, the largest trial
  count (3 when no case has a trial), in the heading and column; a case with no trials is `0/0`,
  fails, not unstable; a quarantined case's `Passed` cell ends with `⚠️ quarantined (<task>)`
  (after `⚠️ unstable` when both apply), its trials count in the suite line and it is not counted
  among the unstable counted cases; the suite line `## Trials: <p> of <n> passed, <u> unstable
  case(s)`; a `|`, line break or control character in a case or agent name is written as the
  contract says; a measurement with a wrongly typed field (a string `trials`, a non-boolean
  `passed`, `NaN` seconds) is "could not be read" and exits 0, never a traceback; an unwritable
  `$GITHUB_STEP_SUMMARY` prints the section instead and exits 0; it runs with `PATH` holding no
  `gh` or `git`;
  the text is the same for the same input twice; it is appended to `$GITHUB_STEP_SUMMARY` when
  set, printed otherwise; an unreadable measurement prints that it could not be read and exits 0;
  wrong arguments exit 2; with no `EARLIER` the second heading is `### No earlier run to compare
  with: <reason>`
- [X] T005 [P] [US1] Test author: new `scripts/tests/test_workflows_eval_trials.py` (clean tree
  and planted violations, as `test_workflows_eval_artifacts.py` builds them): the evals job's
  `env` has `EVAL_MEASUREMENT_DIR: ${{ github.workspace }}-eval-measurement` (no `.` or `..`
  segment; the text `eval-report` nowhere in the new steps, which `judge_evals_report_dir`
  forbids); right after the step `Summary of the evals` a step with `if: always()` and
  `continue-on-error: true` runs `scripts/ci-evals-measure.py "$EVAL_REPORT_DIR/report.json"
  "$EVAL_MEASUREMENT_DIR/measurement.json"`, then `scripts/ci-evals-trials.py` on that file, with
  `env` `SUITE: ${{ matrix.suite }}` and `EVAL_CASES: ${{ matrix.cases }}` and no secret; the step
  `Summary of the evals` is unchanged; every existing `test_workflows*.py` stays green
- [X] T006 [US1] Code author: `scripts/ci-evals-measure.py` (allowlist, research R5),
  `scripts/ci-evals-trials.py` (first table and suite line; the comparison heading for no
  `EARLIER`), and the evals job's `EVAL_MEASUREMENT_DIR` and measurement step in
  `.github/workflows/evals.yml` as T005 states (`Approved-by:` after the owner confirms the diff). T003-T005 pass; `test_workflows*.py` and `test_ci_evals_summary*.py` stay
  green
- [X] T007 [US1] (CI 45148887025 green 26/26 at 7c82140; review: no blocking, T023; converge US1 clean) Commit (tests and code in separate commits); push (owner go-ahead; every check
  job runs) and fix until CI is green; record the run in research; fresh review of US1;
  `/speckit-converge` for US1

- [ ] T023 [US1] Review findings US1-F1, F2 and untested cases (fresh Sonnet review, CI run
  45148887025 green 26/26; suggestions, not blocking): a duration or `cost_usd` too large for a
  float (`1` followed by 400 zeros) raises `OverflowError` in `scripts/ci-evals-measure.py`
  (`seconds()`, `amount()`), caught, so no measurement is written instead of the field's
  fallback (absent); untested: a backslash before `|`, backticks, Markdown or HTML and bidi
  format characters (Unicode Cf) in names; tests in new files by a test author

---

## Phase 3: User Story 2 - Each case shows how much effort its trials took and how that spread (Priority: P1)

**Goal**: the trials table shows, per case, the lowest, median and highest seconds, cost, input
tokens, output tokens and attempts, over the trials that carry them (FR-003, FR-009).

**Independent Test**: T008 passes; quickstart Q1's fixture with trials of 10 s, 20 s and 60 s
shows `10.0, 20.0, 60.0`.

- [X] T008 [US2] Test author: new `scripts/tests/test_ci_evals_trials_spread.py`: lowest, median
  and highest of seconds (one decimal), cost (four decimals), input tokens, output tokens and
  attempts (integers), as the contract's columns; the median of an even count is
  "the mean of the two middle values"; a measure some trials lack shows `(<n> of <m> trials)`; one
  that no trial carries shows `n/a`; a steps measurement (no attempts) shows `n/a` for attempts;
  one trial gives the same value three times; zero cost and zero tokens are numbers, not `n/a`
- [X] T009 [US2] Code author: the spread columns in `scripts/ci-evals-trials.py`. T008 passes,
  T003-T005 stay green
- [X] T010 [US2] (CI 45153547320 green 26/26 at f8b5416; review: no blocking, T025; converge US2 clean) Commit; push (owner go-ahead) and fix until CI is green; record the run; fresh
  review of US2; `/speckit-converge` for US2

- [ ] T025 [US2] Review findings US2-R1 to R4 (fresh Sonnet review at f8b5416; suggestions, not
  blocking): R1 an integer too large for a float in a measurement (`"seconds": 10**400`) makes
  `math.isfinite` raise in `scripts/ci-evals-trials.py` `read_trial`, so only a stderr line is
  written, not "could not be read" (with T023's same case in `ci-evals-measure.py`); R2 the
  large-integer spread tests stay below 2^53 (add `[10**30, 10**30+1]`); R3 nothing pins an exact
  median against a float mean; R4 `-0.0` seconds or cost is written `0.0`, not
  `format(-0.0, ".1f")` (`-0.0`): state it in the contract or test it; tests by a test author

---

## Phase 4: User Story 3 - Each run is compared with the earlier runs of the same cases (Priority: P2)

**Goal**: every eval job keeps its measurement 90 days, and its trials section compares each case
with the latest earlier measurement whose commit is in this commit's history (FR-004 to FR-007;
research R1 to R3).

**Independent Test**: T011-T014 pass; quickstart Q3 picks the ancestor's measurement and leaves
out an unmerged branch's, this run's and an expired one; Q5 shows the comparison live.

- [X] T011 [US3] Test author (owner-approved changes T002 (a), (b); commit with
  `Tests-approved-by:`): in `scripts/tests/test_workflows_eval_artifacts.py`, `the_upload` and
  `judge_only_evals_uploads` find the failed-outputs upload by its name (`evals-…`) and allow
  exactly one more upload step, in the evals job, whose name starts `eval-measurements-`; the
  planted "second upload in evals" case becomes "a second failed-outputs upload, or an upload with
  any other name, fails"; in `scripts/tests/test_workflows_eval_outputs_kept.py`,
  `judge_upload_condition_failed_trial` finds that upload by name. Nothing else in either file
  changes; both stay green on today's workflow
- [X] T012 [P] [US3] Test author: new `scripts/tests/test_ci_evals_earlier.py` for
  `ci-evals-earlier.py <artifacts.jsonl>`, in a temporary git repository with a stub `gh` on
  `PATH` that serves zips: for each matrix key the newest (by `created_at`) unexpired artifact
  named `eval-measurements-<key>-<digits>` of another run whose `head_sha` is `SHA` or an ancestor
  of it is chosen; an artifact of this `RUN_ID`, an expired one, one of a commit not here, one of
  an unmerged branch's commit, one of another key (`steps-code` against `steps-tests`, `analyst`
  against `analyst-x`), one without `run_id` or `head_sha`, and a line that is not a JSON object
  are left out; a listing of two pages' lines is read whole; an entry is `{"run_id", "commit",
  "measurement"}` or `{"none": "<reason>"}`; a failed download, a zip without `measurement.json`,
  a measurement that is not JSON, and a missing listing file give `none` naming it; `MATRIX` unset
  or not JSON gives `earlier={}`; one line `earlier=<JSON>` in `$GITHUB_OUTPUT`, printed when it is
  unset; it starts no process but `git` and `gh`; exit 0 in all these cases, 2 on arguments
- [X] T013 [P] [US3] Test author: new `scripts/tests/test_ci_evals_trials_compare.py`: with
  `EARLIER` holding the measurement's key, the heading `### Since run <run_id> (<7-character
  commit>)`; per case in both, by `case`, `<p>/<n> → <p>/<n>`, median seconds and median cost
  `before → now`; `📉 less reliable` exactly when the current share of passed trials is lower
  (`2/3 → 5/5` is not; `3/3 → 4/5` is); a case only now says `nothing to compare with`; a case
  only in the baseline is not shown; a case with no trials on either side has nothing to compare
  with (no division); the first case of a repeated name is the one compared; a median one side
  lacks is `n/a` (`n/a → 14.0`); an entry `none` gives `### No earlier run to compare with:
  <reason>`, the reason written as the contract says for text from data; `EARLIER` absent, empty,
  not JSON or without the key gives "no earlier run could be read"; a baseline whose `run_id` is
  not a positive integer, whose `commit` is not 40 lowercase hex characters (a planted
  `<script>` or `|` in it), or whose measurement breaks the data model's rules is "could not be
  read", never a crash, while a baseline holding the data model's fallbacks (`"?"`, `""`, `0`) is
  read; the same inputs give the same text; exit 0
- [X] T014 [P] [US3] Test author: new `scripts/tests/test_workflows_eval_measurements.py` (clean
  and planted trees): the evals job's measurement upload uses the upload action's pinned digest,
  `retention-days: 90`, `path` the measurement folder, a name `eval-measurements-…` built from
  `matrix.suite`, `matrix.cases` and `github.run_attempt`, `if-no-files-found: ignore`,
  `continue-on-error: true`; a step with an `id`, `if: always()` and `continue-on-error: true`
  runs `scripts/secret-scan.sh "$EVAL_MEASUREMENT_DIR"`, after the failed-outputs scan; the
  upload's `if` is `always() && steps.<that id>.outcome == 'success'`; a step after it with
  `if: always() && steps.<that id>.outcome != 'success'` prints a `::warning::` that the
  measurement was not kept and appends that line to `$GITHUB_STEP_SUMMARY`; all come before the failed-outputs upload, which stays last; the
  config job's step, after `select`, with `continue-on-error: true`, writes the listing to a file
  with `gh api --paginate` and the contract's `--jq` followed by `|| true`, then runs
  `scripts/ci-evals-earlier.py` on that file with `GH_TOKEN: ${{ github.token }}`, `MATRIX` from
  `steps.select.outputs.matrix`, `SHA`, `RUN_ID: ${{ github.run_id }}`; the job declares the
  output `earlier`; the trials step's `env` has `EARLIER: ${{ needs.config.outputs.earlier }}`; no
  job's permissions change; each rule has a planted violation
- [X] T015 [US3] Code author: `scripts/ci-evals-earlier.py` (importing `scripts/ci_select.py`'s
  `git` and `is_commit` and calling `git("merge-base", "--is-ancestor", head, sha)` itself, since
  the ancestor test is inline in `base_from_runs`; `ci_select.py` does not change), the comparison
  in `scripts/ci-evals-trials.py`, and in `.github/workflows/evals.yml` (`Approved-by:` after the
  owner confirms the diff) the config step and output, the
  measurement scan and upload, the trials step's `EARLIER`. T011-T014 pass; every
  `test_workflows*.py` and the earlier stories' tests stay green
- [ ] T024 [US3] T015's open points (code author, not blocking): the "not kept" warning covers
  only a scan that did not pass, not a failed upload (the action's log only); several earlier
  measurements together could pass the 1 MB limit on the config job's output (each is capped at
  1 MB; today's are a few kilobytes); the comment above the failed-outputs scan now sits three
  steps from its upload
- [X] T016 [US3] (CI 45156838929 green 26/26 at 086720d) Commit; push (owner go-ahead) and fix until CI is green; record the run
- [X] T017 [US3] (done 2026-11-23: the API answers `{"days":90,"maximum_allowed_days":90}`) Owner: raise the Scrollout organization's and this repository's artifact and log
  retention to 90 days (backlog T198); check `gh api
  repos/Scrollout/agentic-platform/actions/permissions/artifact-and-log-retention` answers
  `"days":90`, then close T198
- [ ] T018 [US3] Quickstart Q5 (paid; owner go-ahead; add the branch to environment `dev`'s
  branch policies first): dispatch `evals.yml` with `-f all=true` twice; the request quotes the
  cost of the last all-suites run from its summary, twice; record in research both runs, that each
  job uploaded `eval-measurements-…` (90 days), that the second compared with the first (research
  R3's limit: the config job's token downloaded it), the new steps' seconds per job, which must be
  under 60 (SC-004; over it is a finding), and the trials' cost
- [ ] T019 [US3] Fresh review of US3; `/speckit-converge` for US3

---

## Phase 5: Polish & cross-cutting concerns

- [ ] T020 Docs: `docs/operations/local.md` (the evals paragraph: the trials section, the kept
  measurements and where to read them, the 90-day retention); `docs/overview/problem-space.md`
  N-054 `in progress` (the developer's runs stay for a later feature) with a line on what 008
  covers; `docs/log.md`; `python3 scripts/okf.py check` reports OK
- [ ] T021 Final fresh review of the feature (judging the CI run of the last commit);
  `/speckit-converge`; every finding fixed or recorded as a task
- [ ] T022 Merge steps: `evals-current` names no pending suite on the branch's last check run;
  the owner merges by fast-forward; remove the branch from environment `dev`; delete the branch
  (ask)

---

## Dependencies

- T001 before every test task; tests (T003-T005, T008, T011-T014) are seen failing before their
  code task (T006, T009, T015).
- US1 (T003-T007) → US2 (T008-T010) → US3 (T011-T019): each extends `ci-evals-trials.py` and the
  same workflow job.
- T017 can happen at any time; T018 needs T016 but not T017: two runs on the same day compare
  even with a 1-day retention; only FR-005's 90 days needs T017.
- Polish (T020-T022) after T019.

## Parallel opportunities

- US1: T003, T004, T005 (three new test files).
- US3: T012, T013, T014 in parallel; T011 alone (existing files, approval trailer), and before
  T015, since today's rules take the first upload as the failed-outputs one.

## Implementation strategy

- MVP: US1, which alone makes a flaky case visible in every run.
- Then US2 (effort spread, same table), then US3 (history), whose live proof (T018) is the only
  paid step.
