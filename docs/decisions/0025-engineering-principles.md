---
type: Decision
title: "ADR 0025: Engineering Principles"
description: Why the constitution adopts the Engineering Principles.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: parse-don-t
    resource: https://lexi-lambda.github.io/blog/2019/11/05/parse-don-t-validate/
    title: "Parse, don't validate"
  - id: transactional-outbox
    resource: https://microservices.io/patterns/data/transactional-outbox.html
    title: "Transactional outbox"
  - id: saga
    resource: https://microservices.io/patterns/data/saga.html
    title: "Saga"
  - id: impossiblebench
    resource: https://arxiv.org/abs/2510.20270
    title: "ImpossibleBench"
  - id: llm-evaluators-favor
    resource: https://arxiv.org/abs/2404.13076
    title: "LLM evaluators favor their own generations"
  - id: practical-mutation-testing
    resource: https://arxiv.org/abs/2102.11378
    title: "Practical Mutation Testing at Scale"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with the Engineering Principles, on 2026-11-11.
Constitution v1.4.0 moved this rationale out of the constitution into this record, without changing
any rule.

# Context

These are the general rules that keep code correct when agents write most of it (Parse, don't
validate[^parse-don-t]; Transactional outbox[^transactional-outbox]; Saga[^saga];
ImpossibleBench[^impossiblebench]; LLM evaluators favor their own
generations[^llm-evaluators-favor]; Practical Mutation Testing at
Scale[^practical-mutation-testing]).

# Decision

Adopt the Engineering Principles, exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^parse-don-t]: Parse, don't validate
[^transactional-outbox]: Transactional outbox
[^saga]: Saga
[^impossiblebench]: ImpossibleBench
[^llm-evaluators-favor]: LLM evaluators favor their own generations
[^practical-mutation-testing]: Practical Mutation Testing at Scale
