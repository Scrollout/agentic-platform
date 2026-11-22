# Contract draft: analyst answer rules

Draft of the additions this feature makes to the authoritative contracts. The authoritative
copies are `src/engine/README.md` (section Slices) and the `createSlice` and `answerQuestions`
descriptions in `src/engine/api/preview/2026-11-12-preview/openapi.yaml`; this file only links
to them once they are written (Engineering Principle "Contracts between components").

## Engine README, Slices (Go interface of the slices service)

- `ParseProposal(answer []byte) (Proposal, error)` turns an analyst's structured answer into a
  proposal. An answer whose objects hold a key twice, at any depth, gives an `*AnswerFault`
  naming the key and where; any other answer that is not one JSON object gives a plain error.
  Every `Analyst` implementation parses its answer with it.
- An `Analyst` may return an error wrapping `*AnswerFault`: `Create` and `Answer` treat it as an
  unusable proposal, ask again with its reason in `AnalystInput.Errors` and no `Previous`, up to
  the pattern's `limits.attempts`, then fail with `OperationFailed` naming it.
- `Answer`: a revision that lacks a goal, term, rule, example or operation ID of the current
  proposal, serves a kept rule from another goal, or puts a kept example under another rule, is
  unusable in the same way; its faults name the IDs. An item is removed only by changing its
  part (`ReplaceGoals`, `ReplaceRules`, `ReplaceContract`).

## OpenAPI descriptions

- `createSlice`: add "An analyst answer that repeats a key is asked for again; after the
  pattern's attempts the operation fails with OperationFailed."
- `answerQuestions`: add "The revision keeps every goal, term, rule, example and operation of
  the slice; a revision that drops or moves one, or an answer that repeats a key, is asked for
  again, and after the pattern's attempts the operation fails with OperationFailed and the slice
  is unchanged. To remove an item, change its part."
