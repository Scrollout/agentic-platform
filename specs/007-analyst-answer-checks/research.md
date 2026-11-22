# Research: The platform refuses a revision that loses the slice's items

Phase 0 of the [plan](plan.md) for the [spec](spec.md). Each decision names its deciding source:
the code on `main` (`e4348bf`), a reproduced experiment, or a cited document. Decisions marked
*owner* were put to the project owner as direct questions.

## Sources

| ID | Source |
|----|--------|
| S1 | `src/engine/analyst/http.go` `Propose`: `json.Unmarshal([]byte(ans.Content), &p)`; any error is returned as "analyst: the answer is not a proposal", which `slices.Service.propose` returns at once as "the analyst failed", never retried |
| S2 | `src/engine/slices/service.go` `propose` and `unusable`: an unusable proposal (schema, Gherkin or YAML syntax, unreadable contract) is sent back with its faults (`AnalystInput.Previous`, `Errors`) up to the pattern's `limits.attempts` (3, `src/catalogue/api-feature-go/pattern.yaml`), then `OperationFailed`; broken links are retried too but stored after the last attempt. `answers.go` `Answer` calls `propose` with `AnalystInput.Current` set |
| S3 | Go 1.26.11 source, `src/encoding/json/v2/doc.go` lines 5, 14, 203-207: `encoding/json/v2` exists only with `GOEXPERIMENT=jsonv2`; "By default, v1 allows for the presence of duplicate names, while v2 rejects duplicate names" |
| S4 | [go-json-experiment/json README](https://raw.githubusercontent.com/go-json-experiment/json/master/README.md) and [repository](https://api.github.com/repos/go-json-experiment/json), read 2026-11-22: BSD-3-Clause, not archived; it "currently hosts a mirror of `encoding/json/v2`", the prototype "formally adopted into the Go standard library as an experiment in Go 1.25 and then later as a stable API in Go 1.27"; it states no stability promise of its own |
| S5 | `src/deploy/images.lock`: `golang:1.26-alpine` pin; `src/engine/go.mod` `go 1.26.0`, `src/tests/go.mod` `go 1.26` |
| S6 | Experiment E1 below |
| S7 | [RFC 8259 section 4](https://www.rfc-editor.org/rfc/rfc8259#section-4): names within an object SHOULD be unique; with duplicates "the behavior of software that receives such an object is unpredictable". [RFC 7493 (I-JSON) section 2.3](https://www.rfc-editor.org/rfc/rfc7493#section-2.3): objects MUST NOT have members with duplicate names |
| S8 | `src/tests/evals/analyst/revision_evals_test.go` `runRevision`: one analyst call per trial with the message `RevisionInput` captured, `Attempts: 1`, no retry; `retry_evals_test.go` `analystWithRetry`: the description cases run through `slices.Service.Create` with `dockerAnalyst`, which parses each answer with `Extract` and `json.Unmarshal` |
| S9 | `src/tests/evals/analyst/revision.go` `KeepsIDs` / `itemIDs`: an earlier ID is kept when the revision has it, a rule with the same goal and an example (`@E-<n>`) under the same rule (`R-<n>/E-<n>` pairs); operations by `operationId` |
| S10 | Evals runs 45047239704 (T195) and 45051980598 (T196), backlog `specs/backlog.md` on branch `006-method-record` |
| S11 | Constitution: Principle XIV ("Defences MUST assume prompt injection will succeed and limit what it can reach through authorization, not through filters alone"), XIX ("Every fixed agent failure MUST add an eval case"), VI (custom code needs a written justification), Engineering Principles "Values that check themselves" ("Model output ... MUST be parsed against their schema, never trusted"), "Single owner for rules" |

## E1. What Go does with a repeated key today

Reproduced with Go 1.26.11 (scratch module, not committed):

- `json.Unmarshal` of `{"examples":"Feature: A\n  Rule: R-1","examples":"Feature: Notes\n"}` into
  a struct returns no error and `examples = "Feature: Notes\n"`: the last value wins, silently;
  this is T195's answer (S10).
- `json.Decoder.Token` walks the same text token by token (object keys come as strings in key
  position), so a set of keys per open object finds a repeat in one pass over the answer.

## R1. Where and how a repeated key is refused

- **Decision**: one function in `slices`, `ParseProposal(answer []byte) (Proposal, error)`,
  parses the analyst's structured answer. It first walks the answer with `encoding/json`'s
  `Decoder.Token`, keeping the keys of each open object, and refuses a key that repeats with
  `*AnswerFault` (an error type of `slices`) whose reason names the key and the JSON Pointer of
  its object (`the answer holds the key "examples" twice at /`). Then it decodes as today. Every
  analyst client calls it: `analyst.HTTP.Propose` and the evals' `dockerAnalyst`. An `Analyst`
  may return an error wrapping `*AnswerFault`; `Service.propose` treats it as an unusable answer:
  its reason is a fault, the next attempt is asked with `Previous` unset and `Errors` holding the
  reason, and after the last attempt the request fails with `OperationFailed` naming it, as for
  any unusable proposal (S2). An answer that is not JSON stays an analyst error, as today (the
  spec covers repeated keys only).
- **Analyze round 1**: keys are compared without regard to letter case, since `json.Unmarshal`
  matches struct fields so and `examples`/`Examples` would resolve silently too; the key and
  pointer in a fault are quoted (`%q`) and cut to 64 characters, since they are model text; the
  analyst's message with `Previous` nil already reads "Previous proposal: null" then the errors
  (`message.go`, pinned by `TestAnalystMessageRetryWithErrorsOnlyPinsNullPrevious`), which says
  truly that no proposal could be read, so `message.go` does not change.
- **Rationale**: model output is parsed, never trusted (S11); RFC 7493 forbids repeated names and
  RFC 8259 calls their handling unpredictable (S7). Refusing in the one parse function gives the
  rule one owner, used by production and by the evals alike. Sending no `Previous` avoids
  showing the analyst one arbitrary reading of an ambiguous answer.
- **Custom code justification (Principle VI)**: the capability, rejecting duplicate names, is
  covered by `encoding/json/v2`, stable only from Go 1.27 (S3, S4); the platform is pinned to Go
  1.26 (S5), where v2 needs `GOEXPERIMENT=jsonv2` and is not a stable API. `go-json-experiment/json`
  is a mirror of that code with no release or stability promise of its own (S4), a new dependency
  for one check. The walk over the standard library's own tokenizer is about 30 lines with
  tests. When the platform moves to Go 1.27, `ParseProposal` uses `encoding/json/v2`, which
  rejects duplicate names by default, and the walk goes (a backlog task this feature adds
  records it).
- **Alternatives**:
  - *A schema keyword*: JSON Schema validators read already-decoded values, in which the repeat
    is gone.
  - *Parsing through YAML* (`yaml.v3` refuses a repeated mapping key): JSON is not exactly YAML
    (escapes, numbers), so it would read some valid answers wrongly.
  - *Refusing in `analyst.HTTP` only*: the evals' client would accept what production refuses,
    and the rule would have two owners.

## R2. How a revision keeps the current proposal's IDs

- **Decision**: `Service.propose`, when its input has a current proposal (a revision), adds one
  fault per item the built slice loses, beside the faults of `unusable`: a goal, term, rule,
  example or operation ID of the current proposal that the revision lacks ("the revision drops
  R-1 of the current proposal"), a kept rule that serves another goal ("R-2 served G-1 and now
  serves G-2"), and a kept example under another rule. These are faults, not links: after the
  last attempt the answer fails with `OperationFailed` naming them and the slice is unchanged
  (FR-004). IDs come from the parts as `slice.Trace` reads them: goals, terms and rules from
  their lists, examples from the `@E-<n>` tags under `Rule:` blocks (`slice.ExampleRules`), operations from the contract's `operationId`s. When the current examples or contract
  do not parse (a developer may store such a part, FR-007 of feature 001), their IDs are not
  known and are not checked. A kept rule whose goal the current proposal lacks (the developer
  removed the goal directly) keeps its ID but not that goal, so the analyst may repair the
  broken link (analyze round 1). When the revision's own examples or contract do not parse, that
  part's IDs are not compared either: the attempt already has a syntax fault and is sent back
  (T010). Example IDs with their rules are read by a new `slice.ExampleRules(text)` beside
  `ExampleScenarios`: the `slice` module owns parsing the Gherkin examples, and the engine's
  `slices` package may not import the Gherkin parser (arch lint, CI run 45067768028; T021-T023). The rule is the service's alone; the eval grader `KeepsIDs` (S9)
  judges the analyst independently and stays as it is.
- **Spec alignment**: S9 keeps an example under its rule, and FR-003 says "re-homes"; the spec's
  FR-003 now names a kept example under the same rule explicitly (plan finding, recorded in the
  spec's Clarifications).
- **Rationale**: the owner chose "never" (*owner*, FR-005); the platform, not the analyst's
  instruction, enforces it (S11, Principle XIV). Faults name IDs only, never the answer text
  (FR-010). Create has no current proposal, so nothing changes there.
- **Alternatives**: *a stored failed check* (like broken links): the developer would get a slice
  without its items and have to restore them by hand, the harm T196 shows; refused.

## R3. What the revision evals grade

- **Decision** (*owner*, FR-006): each revision trial goes through `slices.Service.Answer` with
  the real catalogue pattern: the service is created from the fixed earlier proposal by an
  analyst that replays it on `Create` and hands every `Answer` call to a `dockerAnalyst` (a new
  helper beside `replayAnalyst` in `revision.go`; `RevisionInput` stays for the message file), so an unusable revision is
  sent back as in production. What is graded is the last attempt's answer, with `GradeRevision`
  against the input of the first attempt; the trial fails when `Answer` fails or a grader fails;
  `Trial.Attempts` is the number of analyst calls of `Answer`. `dockerAnalyst` parses with
  `slices.ParseProposal` (R1). The message file written per case stays the first attempt's.
  The stub test gains a planted answer with a repeated key; it and the planted revision that
  drops rules each fail every trial with `pattern limits.attempts` (3) attempts, which proves the
  platform refused and asked again; the good revision passes with 1 attempt.
- **Eval cases (FR-007, Principle XIX)**: the live cases `revise-inject-drop-rules` and
  `revise-inject-approvals-field` stay and now judge the platform's defence; the planted stub
  fixtures `bad-revised-repeated-key.json` (new) and `bad-revised-dropped-rules.json` (existing)
  are the deterministic cases of the two failures. A live case cannot provoke a repeated key on
  demand.
- **Cost**: a trial whose first revision is usable makes one call, as today; a refused one up
  to three. The stub test's two bad fixtures make 9 container runs each instead of 3 (measured in
  the implementation, research S12 then).
- **Existing tests changed** (owner approval per change, `Tests-approved-by:`):
  `revision_evals_test.go` (`runRevision`, the stub test's fixtures and attempts),
  `retry_evals_test.go` (`dockerAnalyst` parses with `ParseProposal`), `revision.go` (its header
  says "no retry"), the suite's `README.md`.

## R4. The T195 quarantine

- **Decision**: the quarantine (`Quarantine: "T195"`, commit f757838) exists only on branch
  `006-method-record`; on `main` and on this branch the case counts. This feature's live analyst
  run is the evidence (SC-003). After this feature merges, 006 drops the quarantine (a reverting
  commit with the owner's `Tests-approved-by:`), is rebased on `main` and runs its eval dispatch;
  T195 and T196 close in this feature's last task.
- **Rationale**: a change to 006's files from this branch would mix the features; the owner
  ordered both fixes first, then both merges (2026-11-22).

## R5. Existing tests the change affects

- **Finding** (a read-only sweep of every `Answer` call in `src/engine` and `src/tests`, and a
  script over the recorded revision): one existing engine test changes outcome.
  `src/engine/slices/slices_crosspart_test.go` `TestAnAnswerThatRevisesAPartWithdrawsApprovalsOfPartsItNowBreaks`
  answers with a revision that drops G-2 and expects the rules' approval withdrawn. Under FR-005
  that revision is refused: the answer fails with `OperationFailed` naming G-2. Since a revision
  now keeps every ID, it can no longer break another part's links; the test's scenario becomes
  the refusal. Every other revision in the engine's tests is the Create fixture with changed
  text or questions; `slices_retry2_test.go` and `slices_retry_links_test.go` send a bad revision
  first that is already unusable, and assert `attempts == 2` and that the faults name R-2, which
  still hold when the ID faults follow the existing faults. The recorded analyst revision
  (`src/tests/recorded/recordings/mappings/analyst-notes-answered.json`) keeps every ID of its
  current proposal (checked by a script, 2026-11-22). No `analyst/http_test.go` case repeats a key.
- **Decision**: the ID faults are added after `unusable`'s faults, so the order of existing
  faults is unchanged. The cross-part test's change (expect the refusal, the slice unchanged) is
  put to the owner before its brief (Engineering Principle "Existing tests change only with the
  owner's approval"), as are the eval files of R3.
- **Owner answers (T002, 2026-11-22)**: (a) the cross-part test expects the refusal: approved;
  (b) `runRevision` through `Service.Answer` and the stub test's fixtures and attempts: approved;
  (c) `dockerAnalyst` parses with `ParseProposal` and (d) `revision.go`'s helper analyst and
  header and the suite's README: approved. Each commit carries `Tests-approved-by:`.

## CI runs

| Task | Commit | Run | Result |
|------|--------|-----|--------|
| T007 (US1) | 79731d4 | 45065044569 | green |
| T011 (US2) | 92556c8 | 45067768028 | go-arch-lint failed (`kept.go` imported the Gherkin parser); cancelled when superseded |
| T011 (US2 fix, T012) | 988be8f | 45068904668 | green |

US1 fresh review (Sonnet, read-only worktree at 79731d4, judging run 45065044569): no blocking
finding; suggestions and untested cases in tasks T019 (open) and T020 (closed). `/speckit-converge`
for US1: FR-001 and FR-002 are built (`proposal.go`, `propose`, `http.go`) and tested; nothing
appended beyond T019.

T012 stub run (`run.sh stub`, owner go-ahead, host, 2026-11-22): 16 s in all (before T012: not
measured); `TestHarnessStubRevision`: `good-revised` passes with 1 attempt per trial,
`bad-revised-dropped-rules` and `bad-revised-repeated-key` fail every trial with 3 attempts and
`OperationFailed` naming the lost IDs and the repeated key (4.5 s each). The first stub run had
failed the old "agent container failed" check, which the refusal now trips; the owner-approved
test change requires the refusal there instead.

US2 fresh review (Sonnet, read-only worktree at 988be8f, judging run 45068904668): no blocking
finding; tasks T024 (open) and T025 (closed). `/speckit-converge` for US2: FR-003, FR-004,
FR-005 and FR-010 are built (`kept.go`, `propose`, `slice.ExampleRules`) and tested; nothing
appended beyond T024.

## Live evals (T013)

Evals run 45071292368 (dispatched at 988be8f, owner go-ahead, 2026-11-22; the branch added to
environment `dev`): all four suites green (analyst, steps code, steps tests, steps review; the
selector chose the steps suites too, since their inputs include the changed engine code). Analyst:
all 21 cases pass^3, none quarantined (SC-003); every trial of every case, description and
revision, took 1 attempt, so no live answer repeated a key or lost an ID, and an answer usable on
the first attempt made one analyst call (SC-004, T016). `revise-inject-drop-rules` and
`revise-inject-approvals-field` passed 3 of 3. The cost is in the jobs' summaries on the run page,
which the API does not return (owner reads it).

US3 fresh review (Sonnet, read-only worktree at 988be8f, judging CI run 45068904668, the stub run
and evals run 45071292368): no blocking finding; tasks T026 (open) and T027 (closed).
`/speckit-converge` for US3: FR-006, FR-007 and SC-003 are met; FR-008's quarantine exists only on
branch 006 (R4, T018); nothing appended beyond T026.

Final fresh review (Sonnet, read-only worktree at a42e22e, judging CI run 45068904668, whose commit
988be8f holds all the code, and evals run 45071292368): no blocking finding and no new suggestion;
FR-009 diff empty; every changed existing test carries `Tests-approved-by:`; `okf.py check` OK.
`/speckit-converge` for the feature: nothing appended (open: T019, T024, T026, suggestions).
