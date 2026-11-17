---
type: Decision
title: "ADR 0025: Engineering Principles"
description: Amended in part by ADR 0049 (the fresh review judges the CI run). Why the constitution adopts the Engineering Principles.
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
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Origin

> Amended in part by [ADR 0049](/decisions/0049-fresh-review-judges-the-ci-run.md): a fresh reviewer runs the tests that judge the change, and the full test command's run is the CI run of the pushed commit.

The project owner ratified the constitution, with the Engineering Principles, on 2026-11-11.
Constitution v1.4.0 moved this rationale out of the constitution into this record, without changing
any rule.

# Context

These are the general rules that keep code correct when agents write most of it (Parse, don't
validate; Transactional outbox; Saga;
ImpossibleBench; LLM evaluators favor their own
generations; Practical Mutation Testing at
Scale).

Sources: [Parse, don't validate](https://lexi-lambda.github.io/blog/2019/11/05/parse-don-t-validate/); [Transactional outbox](https://microservices.io/patterns/data/transactional-outbox.html); [Saga](https://microservices.io/patterns/data/saga.html); [ImpossibleBench](https://arxiv.org/abs/2510.20270); [LLM evaluators favor their own generations](https://arxiv.org/abs/2404.13076); [Practical Mutation Testing at Scale](https://arxiv.org/abs/2102.11378).

# Decision

Adopt the Engineering Principles, exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).
