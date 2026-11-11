---
type: Reference
title: From prose to software through checked layers
description: Prior art for turning a developer's plain description into working software through layers of checked, traceable artifacts, what each layer's checks can and cannot prove, and the resulting pipeline for the platform.
tags: [references, requirements, specs, traceability]
status: draft
stale_after: 2027-05-12T00:00:00Z
sources:
  - id: uc20
    resource: https://www.ivarjacobson.com/files/use-case_2_0_e-book_2023_0.pdf
    title: "USE-CASE 2.0: The Definitive Guide (Ivar Jacobson International, 2023)"
  - id: ddd-ref
    resource: https://www.domainlanguage.com/ddd/reference/
    title: Domain-Driven Design Reference (Eric Evans, 2015)
  - id: eventstorming
    resource: https://leanpub.com/introducing_eventstorming
    title: Introducing EventStorming (Alberto Brandolini)
  - id: context-mapper
    resource: https://contextmapper.org/
    title: Context Mapper
  - id: gherkin
    resource: https://cucumber.io/docs/gherkin/reference/
    title: Gherkin Reference (Cucumber)
  - id: better-gherkin
    resource: https://cucumber.io/docs/bdd/better-gherkin/
    title: Writing better Gherkin (Cucumber)
  - id: gherkin-parser
    resource: https://github.com/cucumber/gherkin
    title: cucumber/gherkin parser
  - id: iso-29148
    resource: https://standards.ieee.org/ieee/29148/6937/
    title: ISO/IEC/IEEE 29148:2018 Requirements engineering
  - id: sebok-trace
    resource: https://sebokwiki.org/wiki/System_Requirements_Definition
    title: System Requirements Definition (SEBoK)
  - id: ears
    resource: https://alistairmavin.com/ears/
    title: "EARS: Easy Approach to Requirements Syntax, Official Guide (Alistair Mavin)"
  - id: fret
    resource: https://github.com/NASA-SW-VnV/fret
    title: FRET (NASA Formal Requirements Elicitation Tool)
  - id: spec-kit
    resource: https://github.com/github/spec-kit/blob/main/spec-driven.md
    title: Spec-Driven Development (GitHub Spec Kit)
  - id: openspec
    resource: https://github.com/Fission-AI/OpenSpec
    title: OpenSpec
  - id: kiro-specs
    resource: https://kiro.dev/docs/specs/feature-specs/
    title: Feature Specs (Kiro)
  - id: kiro-analysis
    resource: https://kiro.dev/blog/deep-spec-analysis/
    title: Requirements analysis (Kiro blog, 2026-05-12)
  - id: bockeler
    resource: https://martinfowler.com/articles/exploring-gen-ai/sdd-3-tools.html
    title: "Understanding Spec-Driven-Development: Kiro, spec-kit, and Tessl (Birgitta Böckeler, 2025)"
  - id: tw-sdd
    resource: https://www.thoughtworks.com/radar/techniques/spec-driven-development
    title: Spec-driven development (Thoughtworks Technology Radar)
  - id: slr-re
    resource: https://arxiv.org/abs/2509.11446
    title: "LLMs for Requirements Engineering: A Systematic Literature Review (2025)"
  - id: ease-rules
    resource: https://arxiv.org/abs/2505.18019
    title: LLM assisted web application functional requirements generation (EASE 2025)
  - id: gherkin-llm
    resource: https://arxiv.org/abs/2607.01980
    title: Epic-Organized vs. Requirement-Aligned Gherkin (2026)
  - id: inspection
    resource: https://arxiv.org/abs/2608.21298
    title: "Human-AI Collaboration in RE: Evidence of the Negative Effect of LLMs on Requirements Inspection (2026)"
  - id: lissa
    resource: https://fuchss.org/conferences/icse25/
    title: LiSSA trace-link recovery (ICSE 2025)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T00:30:00Z }
---

# Summary

Turning prose into software through layers of checked artifacts is established practice, and
current AI tools automate parts of it. Two findings shape the platform's version:

- **Traceability proves coverage, not meaning.** Links written when an artifact is created can be
  checked mechanically: every goal has a rule, every rule an example, every example a test. They
  cannot prove that a rule means what the developer intended. Only the developer can judge intent.
- **Today's spec tools mostly leave checking to the model.** Spec Kit checks only that its files
  exist, and its analysis step is itself a prompt. OpenSpec and Kiro add real deterministic checks.

The pipeline this leads to is under "Resulting pipeline".

Sources: [Requirements analysis (Kiro blog, 2026-05-12)](https://kiro.dev/blog/deep-spec-analysis/); [Spec-Driven Development (GitHub Spec Kit)](https://github.com/github/spec-kit/blob/main/spec-driven.md); [OpenSpec](https://github.com/Fission-AI/OpenSpec).

# Prior art

- **Use-Case 2.0.** A use-case slice is "one or more stories selected from a use case to form a
  work item that is of clear value to the customer", and it evolves through design,
  implementation and test. "It is the test cases that define what it means to successfully
  implement the use case." Slices move through Scoped, Prepared, Analyzed, Implemented and
  Verified, so the slice, not the whole system, is the unit that flows through the pipeline.
- **Domain-Driven Design and EventStorming.** A change in the shared language "is a change to the
  model", and aggregate invariants should become automated tests. Both produce prose or workshop
  results rather than checkable formats. Context Mapper adds a checkable language for bounded
  contexts and aggregates.
- **Behaviour-driven development.** Gherkin's `Rule` keyword represents one business rule, with
  examples illustrating it, so a rule and its examples belong together. Gherkin has a generated
  parser, so syntax checks are deterministic, and steps should describe behaviour, not
  implementation.
- **Requirements standards.** ISO/IEC/IEEE 29148:2018 sets criteria for individual requirements,
  for sets of them and for their language. Traceability ensures requirements "trace to their
  needs, and that they are correct and complete", and supports change-impact analysis.
- **EARS.** A small fixed grammar for requirements ("While …, when …, the … shall …") with five
  patterns, small enough to parse deterministically. NASA's FRET goes further, turning restricted
  English into logic and checking consistency.

Sources: [USE-CASE 2.0: The Definitive Guide (Ivar Jacobson International, 2023)](https://www.ivarjacobson.com/files/use-case_2_0_e-book_2023_0.pdf); [Domain-Driven Design Reference (Eric Evans, 2015)](https://www.domainlanguage.com/ddd/reference/); [Introducing EventStorming (Alberto Brandolini)](https://leanpub.com/introducing_eventstorming); [Context Mapper](https://contextmapper.org/); [Gherkin Reference (Cucumber)](https://cucumber.io/docs/gherkin/reference/); [Writing better Gherkin (Cucumber)](https://cucumber.io/docs/bdd/better-gherkin/); [cucumber/gherkin parser](https://github.com/cucumber/gherkin); [ISO/IEC/IEEE 29148:2018 Requirements engineering](https://standards.ieee.org/ieee/29148/6937/); [System Requirements Definition (SEBoK)](https://sebokwiki.org/wiki/System_Requirements_Definition); [EARS: Easy Approach to Requirements Syntax, Official Guide (Alistair Mavin)](https://alistairmavin.com/ears/); [FRET (NASA Formal Requirements Elicitation Tool)](https://github.com/NASA-SW-VnV/fret).

# Current spec-driven tools

| Tool | Checked deterministically | Left to the model |
|------|---------------------------|-------------------|
| GitHub Spec Kit | Only that its files exist and are not empty | Checklists, cross-artifact analysis, clarification markers |
| OpenSpec | Every requirement contains SHALL or MUST and has at least one scenario | Content |
| Kiro | Requirements in EARS; a solver checks consistency and gaps; property-based tests traced to requirements | Turning prose into logic, which can be unfaithful |
| Tessl, BMAD | Nothing described | Everything |

- Kiro's own analysis says "the user remains the only judge of intent".
- Criticism: one review found Kiro turning a small bug into 16 acceptance criteria, agents
  ignoring specs, and regenerated code differing from the same spec. Thoughtworks rates
  spec-driven development "Assess" and warns of long spec files that are hard to review.

Sources: [Spec-Driven Development (GitHub Spec Kit)](https://github.com/github/spec-kit/blob/main/spec-driven.md); [OpenSpec](https://github.com/Fission-AI/OpenSpec); [Feature Specs (Kiro)](https://kiro.dev/docs/specs/feature-specs/); [Requirements analysis (Kiro blog, 2026-05-12)](https://kiro.dev/blog/deep-spec-analysis/); [Understanding Spec-Driven-Development: Kiro, spec-kit, and Tessl (Birgitta Böckeler, 2025)](https://martinfowler.com/articles/exploring-gen-ai/sdd-3-tools.html); [Spec-driven development (Thoughtworks Technology Radar)](https://www.thoughtworks.com/radar/techniques/spec-driven-development).

# What research says about models doing this work

- Most studies of LLMs in requirements work are evaluated in controlled settings.
- Models struggle most with business rules: all four models in one study "struggled to generate
  relevant Business Rules".
- Syntax is easy, meaning is not: constrained output reached 100% valid Gherkin but about 93% of
  the intended coverage.
- LLM help can make human review worse: in a 34-person study, LLM support reduced how well people
  found problems in requirements.
- Recovering trace links after the fact is not yet reliable enough for practice, so links must be
  written when artifacts are created.

Sources: [LLMs for Requirements Engineering: A Systematic Literature Review (2025)](https://arxiv.org/abs/2509.11446); [LLM assisted web application functional requirements generation (EASE 2025)](https://arxiv.org/abs/2505.18019); [Epic-Organized vs. Requirement-Aligned Gherkin (2026)](https://arxiv.org/abs/2607.01980); [Human-AI Collaboration in RE: Evidence of the Negative Effect of LLMs on Requirements Inspection (2026)](https://arxiv.org/abs/2608.21298); [LiSSA trace-link recovery (ICSE 2025)](https://fuchss.org/conferences/icse25/).

# Resulting pipeline

The first sketch had five layers. The evidence changes three things: rules and their examples
merge into one layer; the domain language runs across all layers instead of being one; and design
splits into a contract, which a person approves, and a build plan, which checks approve. The whole
pipeline runs per use-case slice.

| Layer | Artifact and format | Deterministic checks | Person |
|-------|---------------------|----------------------|--------|
| Intent | Goals, users, non-goals, constraints, each with an ID | Matches its schema; no open clarification markers | Approves |
| Behaviour | Business rules in EARS, each with Gherkin examples under a `Rule` tagged with the rule's ID | Every rule parses as EARS and has at least one example; vague words flagged; every term is in the glossary; every rule traces to a goal and every goal has a rule | Approves the rules, answering targeted questions instead of reading long documents |
| Contract | OpenAPI or AsyncAPI with JSON Schema | Lint and compatibility checks pass; every example's action maps to an operation or event | Approves (Contract-first) |
| Build plan | A pattern from the catalogue | Matches the catalogue's rules for the scenario | Optional |
| Build | Tests, then code | Every example's steps are bound to code; tests fail before the code and pass after; no drift from the contract; scope and read-only guards | — |

- The glossary (domain terms) is shared by every layer.
- Checks approve conformance at every layer. People approve meaning: intent, rules and public
  contracts.

Sources: [USE-CASE 2.0: The Definitive Guide (Ivar Jacobson International, 2023)](https://www.ivarjacobson.com/files/use-case_2_0_e-book_2023_0.pdf); [Gherkin Reference (Cucumber)](https://cucumber.io/docs/gherkin/reference/); [EARS: Easy Approach to Requirements Syntax, Official Guide (Alistair Mavin)](https://alistairmavin.com/ears/); [OpenSpec](https://github.com/Fission-AI/OpenSpec); [Requirements analysis (Kiro blog, 2026-05-12)](https://kiro.dev/blog/deep-spec-analysis/).

# Unverified

- The exact list of characteristics in ISO/IEC/IEEE 29148 (the standard's text was not reachable).
- The rule count in INCOSE's Guide to Writing Requirements v4.
- The names of the seven process patterns in Specification by Example.
