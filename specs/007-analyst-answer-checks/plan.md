# Implementation Plan: The platform refuses a revision that loses the slice's items

**Branch**: `007-analyst-answer-checks` | **Date**: 2026-11-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/007-analyst-answer-checks/spec.md` (backlog T195, T196)

## Summary

The analyst's answer is parsed by one function, `slices.ParseProposal`, which refuses an answer
that repeats an object key (research R1); the service treats that refusal, and a revision that
drops or re-homes an ID of the current proposal (R2), as an unusable proposal: it asks the
analyst again within the pattern's attempts, then fails with `OperationFailed` and changes
nothing. The revision evals run each trial through the platform's `Answer` with that retry and
report its attempts, with a planted repeated-key answer beside the planted dropped-rules one (R3).
The analyst's prompt, model and schema do not change.

## Technical Context

**Language/Version**: Go 1.26 (the engine and the evals harness, as feature 001)

**Primary Dependencies**: none added; `encoding/json` (`Decoder.Token`), `go.yaml.in/yaml/v3` and
the Gherkin parser already used by `slice`

**Storage**: none changed; a refused revision stores nothing

**Testing**: Go tests in `src/engine/slices` and `src/engine/analyst` (new files by separate
authors); the analyst evals' stub run (`run.sh stub`) and one live analyst run; CI's check run of
the pushed commit as the full test command

**Target Platform**: the local platform (Compose), as feature 001

**Project Type**: the engine's slices service and its analyst client; the analyst evals harness

**Performance Goals**: SC-004: an answer usable on the first attempt makes no extra model call;
the key walk is one pass over an answer of at most 32K tokens

**Constraints**: FR-009 (no prompt, model or schema change); the faults name keys and IDs only
(FR-010); existing faults keep their order (R5); changed existing tests carry the owner's
`Tests-approved-by:`

**Scale/Scope**: one parse function and error type, one fault source in `propose`, two analyst
clients, the revision eval runner and its stub fixtures, the engine README and the
`answerQuestions`/`createSlice` descriptions

## Constitution Check

*Evaluated before research and again after design: PASS.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Needs before solutions | Pass | Serves N-051, N-013 |
| II. Two planes | Not affected | No component or flow between planes changes |
| III. API-first | Pass | No new operation; the `createSlice` and `answerQuestions` descriptions state the refusal first (contract-first) |
| IV. Asynchronous operations | Pass | The refusal ends the existing operation `failed` with `OperationFailed`, already declared |
| V, XII. Identity, tenants | Not affected | No call, token or tenant path changes |
| VI. Adopt mature OSS | Pass with justification | About 30 lines over the standard library's tokenizer instead of `encoding/json/v2` (stable only from Go 1.27) or its mirror module (R1); a backlog task replaces it on the Go 1.27 move |
| VII. Providers | Not affected | No provider kind changes |
| VIII. Containers | Not affected | No image changes beyond the engine's code |
| IX. Knowledge stays true | Pass | ADR 0081, ADR 0082, reference `go-json-duplicate-names`; engine README, architecture overview and log at implementation |
| X. No silent loss | Pass | A refused revision changes nothing and is reported as `OperationFailed`; the developer's answers are not recorded, so they can answer again |
| XI. Privacy | Pass | Faults name keys and IDs, never answer text or content (FR-010); nothing new is logged |
| XIII. Supply chain | Pass | No dependency added; the analyst's prompt is unchanged |
| XIV. Safe defaults | Pass | The point of the feature: the platform, not the prompt, refuses a revision an injected answer shaped; an ambiguous answer is refused (fail closed) |
| XV. Compatible APIs | Pass | No schema change; a revision that was accepted before may now fail with an existing error code, stated in the descriptions (preview version) |
| XVI. Human control | Pass | Changed existing tests and eval files carry the owner's `Tests-approved-by:` |
| XVII. Declare, derive, converge | Not affected | No declaration added; the attempts limit stays the pattern's |
| XVIII. Abstractions say what | Pass | `ParseProposal` and `AnswerFault` are named for what they give; no new layer: both analyst clients call the one function |
| XIX. Evals | Pass | Revision evals grade the platform's accepted revision (owner), a planted case per fixed failure, one live analyst run pass^3 before merge |
| XX. Deterministic acceptance | Pass | A revision is accepted only when deterministic checks hold that its items are kept; the model's claim is not a check |
| Values that check themselves | Pass | The answer is parsed at the trust boundary into a `Proposal` by the one function; nothing else checks repeated keys |
| Single owner for rules | Pass | Repeated keys: `ParseProposal`; kept IDs: `Service.propose`; the eval grader `KeepsIDs` is an independent judge |
| Checks prove they can fail | Pass | Planted answers with a repeated key (top level, nested, same value) and planted revisions that drop or re-home IDs fail; clean ones pass |
| Existing tests change only with approval | Pass | R5 and R3 list them; each is put to the owner before its brief |
| Fresh review | Pass | A fresh read-only reviewer per story and before done, judging the CI run |
| Layout | Pass | Changes in `src/engine/` and `src/tests/evals/analyst/` |

## Decisions

Each decision that outlives the feature has a record in `docs/decisions/`.

| Decision | Research | Record |
|----------|----------|--------|
| The platform refuses an analyst answer that repeats an object key, and a revision that drops or re-homes an ID of the current proposal, as unusable: asked again within the pattern's attempts, then `OperationFailed`; an item is removed only by changing its part directly | R1, R2 | [ADR 0081](../../docs/decisions/0081-platform-refuses-ambiguous-answers-and-revisions-that-lose-items.md) |
| The analyst's revision evals grade the revision the platform accepts through its retry, and report each trial's attempts | R3 | [ADR 0082](../../docs/decisions/0082-revision-evals-grade-the-platforms-revision.md) |

## Threat model

No external interface, tool, model, memory or access to protected content is added. The
analyst's answer and the developer's answers are existing trust boundaries (feature 001's threat
model, rows "Description and answers into the model" and "Model output (analyst, step agents)"); this feature adds a mitigation on
each.

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| Developer's answers in the analyst's context | | ✓ | | | | ✓ | LLM01 prompt injection; ASI01 agent goal hijack | A revision that loses an item is refused by the platform (R2), whatever the answer said; faults name IDs only, so the answer text is never echoed back (FR-010) |
| Analyst answer (model output) | | ✓ | | | | | LLM05 improper output handling | One parse function refuses repeated keys before decoding (R1), so no reading of an ambiguous answer is chosen silently |
| The retry loop | | | | | ✓ | | LLM10 unbounded consumption | Bounded by the pattern's `limits.attempts` and the analyst's budget, as today |

## Project Structure

### Documentation (this feature)

```text
specs/007-analyst-answer-checks/
├── plan.md              # this file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── analyst-answer-rules.md   # draft of the engine README and description additions
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
src/engine/slices/proposal.go        # new: ParseProposal, AnswerFault (R1)
src/engine/slices/kept.go            # new: the IDs a revision must keep (R2)
src/slice/tags.go, src/slice/README.md   # ExampleRules: each example ID with its rule (R2)
src/engine/slices/service.go         # propose: AnswerFault and lost IDs as faults
src/engine/analyst/http.go           # Propose parses with slices.ParseProposal
src/engine/README.md                 # Slices: the two refusals (contract)
src/engine/api/preview/2026-11-12-preview/openapi.yaml   # createSlice, answerQuestions descriptions
src/tests/evals/analyst/revision_evals_test.go   # runRevision through Service.Answer (approval)
src/tests/evals/analyst/retry_evals_test.go      # dockerAnalyst parses with ParseProposal (approval)
src/tests/evals/analyst/revision.go              # header: retry; analyst replaying Create, docker on Answer (approval)
src/tests/evals/analyst/testdata/revision/bad-revised-repeated-key.json   # new planted answer
src/tests/evals/analyst/README.md                # the revision cases run through the platform
src/engine/slices/slices_crosspart_test.go       # the refusal replaces the withdrawal (approval, R5)
```

**Structure Decision**: both rules belong to the slices service, which owns what a proposal is
and when it is usable; the analyst client and the evals harness only call its parse function.

**Existing tests expected to change** (each put to the owner first): `slices_crosspart_test.go`
(R5); `revision_evals_test.go`, `retry_evals_test.go`, `revision.go` (a helper analyst and its
header) and the suite's README (R3).

## Exceptions

None.

## Complexity Tracking

No violation.
