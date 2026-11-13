# Data Model: From a description to accepted working software, one slice at a time

The schemas live with their provider, the engine, in
[`src/engine/api/preview/2026-11-12-preview/`](../../src/engine/api/preview/2026-11-12-preview/)
(moved there by T011; [contracts/](contracts/) keeps only the review record).
All identifiers are stable once assigned and never reused.
A slice, run, operation or artifact that exists but is not for the calling token is reported as
not found (404), the same as one that does not exist; 403 means the token may not call the
operation at all.

## Slice

| Field | Meaning | Rules |
|-------|---------|-------|
| `id` | `S-<n>` | unique per installation; set by the platform |
| `description` | the developer's prose | non-empty; one use case |
| `goals[]` | Goal records | at least one |
| `terms[]` | Term records | every term a rule uses |
| `rules[]` | Rule records | every goal has at least one rule |
| `examples` | Gherkin text, one `Rule:` block per rule; each scenario tagged `@R-<n>` and `@E-<n>` | every rule has at least one scenario; `E-<n>` unique in the slice and never reused (check `ExampleTagged`) |
| `contract` | OpenAPI 3.1 document as YAML text | required; every operation has an `operationId` and `x-rules: [R-…]` |
| `rule_text` | each rule's EARS sentence, by rule ID | set by the platform; never edited |
| `questions[]` | Question records | at most 10 in total per slice (FR-006) |
| `parts` | current hash of `goals`, `rules` and `contract` | SHA-256 of the RFC 8785 canonical JSON of `{goals}`, `{rules, examples, terms}` and `{contract}` |
| `approvals[]` | Approval records | one each for goals, rules and contract |
| `checks[]` | latest check results | each names the part or item, the check, whether it passed, the reason, and `pattern`: whether it is a condition of the catalogue pattern (`PatternFit` when no pattern can be read) or one of the engine's own checks |
| `history[]` | every change in order: `proposed`, `revised`, `answered`, `changed`, `approved`, `withdrawn` | time and the IDs concerned; a proposal or revision names the model and agent definition digest, and `attempts`: how many times the analyst was asked before its output was usable (at most the pattern's `limits.attempts`; absent on entries recorded before it was kept) |
| `state` | see lifecycle | set by the platform |
| `latest_run` | the slice's latest run ID | the run owns its status and outcome |
| `title` | a short name | derived from the first goal |

**Lifecycle**: `proposed` → `questions-open` → `checked` → `approved`. Whether the slice is
building, done, handed over or expired is the status and outcome of its latest run. Any change to
an approved part returns the slice to `checked` and withdraws that part's approval (FR-007); any other part that the change makes fail a check, such as a rule that names a removed goal, loses its approval too, recorded as `withdrawn` with those parts as `refs`. A slice is never `approved` while a check fails. A slice the pattern does not fit stays
`checked` with a `PatternFit` check failed and cannot be built (FR-009).

The analyst's output is a **slice proposal**: goals, terms, rules, examples, contract and new
questions without IDs or answers; it has no `id`, `title`, `description`, `state`, `rule_text`,
`parts`, `approvals`, `checks` or `history` (see `slice-proposal.schema.json`). The parts of both
are defined once, in `slice-defs.schema.json`.

## Goal

`id` (`G-<n>`), `text`. Covered when at least one rule has `goal: G-<n>`.

## Term

`id` (`T-<n>`), `name`, `definition`. Defined once; referenced by ID.

## Rule

Structured EARS:

| Field | Meaning |
|-------|---------|
| `id` | `R-<n>` |
| `goal` | the `G-<n>` it serves |
| `pattern` | `ubiquitous`, `event`, `state`, `unwanted` or `option` |
| `precondition` | required for `state` |
| `trigger` | required for `event` and `unwanted` |
| `feature` | required for `option` |
| `system` | the system name |
| `response` | what the system shall do |
| `terms` | `T-<n>` IDs used in the fields above |

The EARS sentence is derived; the slice shows it in `rule_text`.

## Question

`id` (`Q-<n>`, set by the platform), `about` (a `G-`, `R-`, `T-`, `R-<n>/E-<n>` or
`contract/<operationId>` ID), `text`, `options[]` (at most 5), `answer` (`{option: 0-4}` or
`{text}` of at most 200 characters), `answered_at`.

## Approval

`part` (`goals`, `rules`, `contract`), `content_hash`, `at`. Holds only while `parts[part]` equals
`content_hash`.

## Pattern and catalogue

A pattern: `id`, `version`, `scenario`, `conditions[]` (named fit checks), `sources[]` (URL and
title), `steps[]`, `limits` (`attempts`, `review_rounds`), `budgets` (run and per step: tokens,
cost in USD, minutes), `status` (`approved`). A step: `name`, `worker` (`agent:<definition>` or
`procedure:<name>`), `agent_digest` (the agent definition's SHA-256, for an agent step),
`writable[]` (globs relative to the project folder), `dependencies` (`allowed[]`, `forbidden[]`), `checks[]`,
`inputs[]`, `outputs[]`. The catalogue is the set of approved patterns; this version holds one.

## Run, run record and accepted result

- **Run**: `id` (`RUN-<n>`), `slice`, `pattern` (`<id>@<version>`), `operation`, `project` (a
  new folder name under the projects folder), `replay_of` (a run ID, for a replay), `status`
  (`running`, `interrupted`, `waiting-for-developer`, `ended`), `outcome` once ended (`done`,
  `handed-over`, `expired`, `cancelled`), `reason`, `expires_at`. An interrupted run continues with
  resume. A run waiting for the developer ends `handed-over` when the developer accepts the
  hand-over, or `expired` 14 days after it stopped; cancelling its operation ends it `cancelled`.
  These are the terminal states Principle X requires: `done` is succeeded; `handed-over` is failed
  (the platform could not finish within its attempts, review rounds or budgets, and the developer
  took the work over); `expired` is timed out; `cancelled` is cancelled. A build refused before it
  is accepted as a run (`NotApproved`, `PatternDoesNotFit`, `RunActive`, `WorkersNotReady`) is the
  rejected-by-policy case: no run is created.
- **Run record**: append-only JSON Lines. Each event: `seq`, `at`, `run`, `slice` (on
  `RunStarted`), `kind` (see research D23), `step`, `attempt`, `round` (review), `worker`, `model`,
  `agent_digest`, `pattern`, `inputs[]` and `artifacts[]` (hashes), `reuse_key`, `usage` (`tokens`,
  `cost_usd`: what one attempt used, on `AttemptMade`, or on `StepRejected` for an attempt that made
  none; summed, the run's use, so its budgets hold across a restart; a negative amount counts as
  0), `changed_lines[]` (`file`, `line`: on `StepAccepted` or `ResultReused`, the lines a fix changed, which the next
  review round is given, also after a restart), `refs[]` (the `G-`, `R-`, `T-`,
  `R-<n>/E-<n>` or `contract/<operationId>` IDs it concerns), `check` (name, passed, reason),
  `finding` (rule, file, line, text, blocking, `check`: informational, no check reads it; a
  blocking finding is confirmed by rerunning its rule's tests, [ADR 0031](../../docs/decisions/0031-review-findings-confirmed-by-rerunning-rule-tests.md)), `detail`. Each kind requires the
  fields that explain it (see `run-event.schema.json`).
- **Accepted result**: `key` (the reuse key), `artifacts[]`, `run`. Found under its key, it is
  reused (FR-019).

## Run rules the machine follows

These settle what the spec leaves open; the machine tests (T043) and check tests (T042) pin them.

- **Active run**: a run is active until it has an outcome. A run waiting for the developer is
  active, so a second build is refused with `RunActive` (FR-021).
- **Budgets**: a step whose use takes the run or the step past a token, cost or time budget is not
  accepted, and the run is handed over with the budget it crossed as the reason (FR-016).
- **Review rounds**: between review rounds the code step runs again with the open blocking findings
  as its input; a later round sees only those findings and the lines the fix changed (FR-016,
  Review converges).
- **Refs**: every event names the refs it concerns: a check's own refs, otherwise the run's refs
  (the slice's goals, rules and examples) (SC-006).
- **Reuse key**: deterministic and different whenever any of its ingredients differs (pattern
  version, step, input hashes, agent definition digest, model id; research D8); its exact encoding
  belongs to the implementation.
- **A failing worker or model**: a worker that errors, including an unavailable model, makes a
  rejected attempt, retried within the step's attempts; the run can then be resumed (edge case).
- **The run as a process across components** (constitution, Reliable events and side effects;
  research D29, D30). A run spans the engine, the step workers, the check service and the project
  worker; the state-changing steps are an attempt's writes into the project and each accepted
  step's commit.
  - **Pivot**: the run's outcome recorded with the project handed over: `done`, or `handed-over`
    when the developer takes the run over. After it nothing is undone. `done` is recorded only
    after the last accepted step's commit is made, so a done project's working tree equals its
    last commit. A run handed over, expired or cancelled keeps its folder as the last attempt
    left it: the last commit holds the last accepted state, and a rejected last attempt's files,
    if any, are uncommitted changes the developer sees against it (nothing restores after the
    last attempt).
  - **Compensation**: the project worker's `restoreStep` (check contract), called by the engine
    before every attempt of every step, makes the project folder hold `before` (the files the
    last accepted step left) from the last commit: files the commit holds are written back, every
    other file outside `.git` is removed, no folder is removed. It undoes a rejected attempt's
    and an interrupted attempt's writes alike, and changes nothing otherwise. Accepted steps are
    not compensated: they are the state later steps and resume build on. Run-record events are
    not undone (D8).
  - **Commits**: after recording `StepAccepted` or `ResultReused`, the engine calls `commitStep`
    and calls nothing for the next step, and records no outcome, until it answers 200; on
    resume it calls it again for the last accepted step, which a repeat makes safe. One commit
    per accepted step, a step that changed no file included, on the branch `main`; its tree is
    exactly the accepted `project_files`, each file with its bytes as a non-executable regular
    file. For a reused result (`reused_from`), `commitStep` first writes the reused outputs'
    stored artifacts into the step's writable paths; this is how a replay's new project gets its
    files, and it lifts the guard that reused a result with files only in its own project.
  - **Who writes `.git`**: only the project worker. `ap build-workers` creates the project folder
    with an empty `.git` folder and binds it read-only into every step container, including the
    generate worker, whose project root is otherwise writable (a stated limit). The project
    worker creates the repository in it at the run's first `commitStep`, after contract-check is
    accepted and before generate runs.
  - **History guard** (a second layer): the project worker keeps, per project, on a volume only
    it mounts, the ID of the last commit it made, its parent ID, the trailers it names and the
    SHA-256 of the `.git/config` it wrote, replaced atomically (temporary file, fsync, rename)
    after making the commit and before moving `main` to it. Before any git command,
    `commitStep` and `restoreStep` check `.git` as files. With no record, `commitStep` empties the
    contents of `.git` (never the folder) and runs `git init --template=`, writing a provisional
    record (the config hash, no commit yet), so a crash during the first call never blocks the
    run; anything there can only be its own unfinished work. With a record, `.git` must hold no symbolic link and no `commondir`, `gitdir`,
    `shallow`, `info/grafts` or `objects/info/alternates`, `.git/HEAD` must be exactly `ref:
    refs/heads/main` and one line feed, the branch a loose ref with no `main` in `packed-refs`, `.git/config` must
    match, and the branch must be at the recorded commit (or its recorded parent, or
    missing for a first commit, only when the recorded commit is exactly the one the request
    makes). With a provisional record (after `init`, before the first commit) the branch must
    be missing, and with no record or a provisional one `restoreStep` requires `before` to be
    empty. `.git` is emptied only in `commitStep`'s commit step, after its `before` check; then the recorded commit must exist
    with its recorded parent and trailers. Otherwise 409 `HistoryChanged` and nothing changes:
    the run is interrupted with the reason.
  - **Restore clean-up**: `restoreStep` also removes entries the checks would not read where the
    step could have made them: below each `dir/**` folder, subfolders included, every symbolic
    link (never followed), every file and every folder whose name is not a ProjectPath; in the
    folder of an exact entry (the project root, for generate's `go.mod` and `go.sum`), only such
    an entry the step's writable entries match by name. Elsewhere such an entry is 422.
  - **Repeats**: a `commitStep` whose last commit has the same trailers is a repeat; it answers
    `created: false` when that commit is exactly the one the request makes, and 409
    `ProjectStateDiffers` when the payload differs.
  - **Commit identity and message**: author and committer `agentic-platform
    <noreply@localhost>`; both dates are the `at` of the `StepAccepted` or `ResultReused` event,
    in whole seconds, UTC. Message: the subject `Step <step> accepted in <run>` (`Step <step>
    reused in <run>` for a reused result), an empty line, then the trailers `Run: <run>`,
    `Step: <step>`, `Round: <n>` (review rounds and their fixes only), and `Attempt: <n>` or
    `Reused-from: <run>`, one per line in that order. A repeated `commitStep` finds the last
    commit's trailers equal and makes no second commit.
  - **Timeout**: each attempt within the step budget and the run within the run budget (D14:
    `budgets.step.minutes` 10, `budgets.run.minutes` 30); every call to a worker, the check
    service or the project worker is bounded by `time_limit_seconds`, at most 600 s, what is left
    of the step's budget (for `restoreStep` and `commitStep`, of the run's). When less than 1 s
    of the run's budget is left for a `restoreStep` or `commitStep`, the engine does not call it:
    the run is interrupted and can be resumed, and resume makes a pending commit first. A `restoreStep` or
    `commitStep` that fails or times out is retried with backoff (D7); if it still fails, the run
    is interrupted with the reason, not handed over, and resume repeats the call. A run waiting
    for the developer expires 14 days after it stopped.
  - **What a replay makes identical** (US2.8, SC-005): every step is reused, so the replay's
    artifacts are the original's, hash for hash; each step's `project_files`, and so each
    commit's tree and the final folder outside `.git`, are byte-identical to the original's.
    Commit IDs differ, since each commit names its own run and its own run's times.

## Step checks

- **Scope**: a step may change only paths its `writable` globs match; `dir/**` means everything
  below `dir`, any other entry one exact path.
- **Tests unchanged**: files the tests step wrote are byte-identical after every later step; the
  "unchanged" checks skip the current step's own writable paths (the seam `internal/app/app.go`,
  which generate writes and code replaces).
- **Dependencies**: imports in the step's own writable files (and `go.mod` when the step may
  write it) against the pattern's allowed and forbidden lists; forbidden wins over allowed.
- **Tests fail against the stub**: before code, the derived tests build and fail; a test that does
  not build does not count as failing.
- **Every example bound** (godog `--strict`): after tests; judged on binding, not results: it
  fails on a build failure, an undefined, pending or ambiguous step, or fewer scenarios run than
  `features/slice.feature` holds.
- **All tests pass**: after code; godog runs from the project's vendored
  dependencies inside the check container, with no network (D3, T050).
- **Generation drift**: generated code equals a fresh generation from the approved contract.
- **Contract hash**: the project's contract equals the approved contract's hash.
- **Evidence-backed finding**: a review finding blocks only when it names a rule ID, file and line
  and the rule test it names fails at that place; any other finding is a suggestion (FR-017, D11).
