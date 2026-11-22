# Contract: analyst answer rules

The contract this feature adds lives with its provider (Engineering Principle "Contracts between
components"); this file only links to it:

- [`src/engine/README.md`](../../../src/engine/README.md), section Slices: `ParseProposal`,
  `AnswerFault`, the retry of an unusable answer, and the IDs a revision through `Answer` keeps.
- [`openapi.yaml`](../../../src/engine/api/preview/2026-11-12-preview/openapi.yaml): the
  `createSlice` and `answerQuestions` descriptions.

The draft's wording was moved there in T001; the faults' shapes are in the
[data model](../data-model.md).
