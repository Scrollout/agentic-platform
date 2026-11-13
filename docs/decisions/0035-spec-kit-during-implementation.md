---
type: Decision
title: "ADR 0035: Spec Kit during implementation"
description: During implementation a discovery is fixed at its source, a change of intent updates spec, plan and tasks in that order as its own reviewed change before code, analyze is re-run until clean after every artifact change, implement runs in validated stages behind the checklist gate, and converge runs after each user story and before a feature is done.
tags: [decisions, constitution, spec-kit]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's request, 2026-11-14
  - id: evolving-specs
    resource: https://github.com/github/spec-kit/blob/v1.4.0/docs/how-to/evolving-specs.md
    title: "Spec Kit v1.4.0: Evolving Specifications in an Existing Project"
  - id: agentic-sdd
    resource: https://github.com/github/spec-kit/blob/v1.4.0/docs/reference/agentic-sdd.md
    title: "Spec Kit v1.4.0: Agentic SDD reference"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-14T12:00:00Z }
---

# Origin

The project owner asked on 2026-11-14 to analyze how Spec Kit should be used during
implementation, from Spec Kit v1.4.0's guides on evolving specifications and on agentic SDD, and
to amend the constitution accordingly. The owner's request, and approval of the commit that
carries this record, is the approval of this record and of the constitution amendment (version
2.1.0).

Sources: [Project owner's request, 2026-11-14](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).

# Context

- The constitution named the Spec Kit commands and required analyze to pass before implement, but
  said nothing about what happens when implementation discovers something.
- In feature 001 implementation repeatedly found design gaps (the project's version history, the
  grants an engine learns after start, the guard for an existing folder). They were settled by
  editing `data-model.md`, `research.md` and `tasks.md` mid-unit, sometimes in the same working
  tree as the unit's code, and a contract change was once made with its code instead of before
  it. Analyze was re-run, but by habit, not by rule; converge has not run yet.
- Spec Kit v1.4.0 describes the loop: assess whether a discovery is a defect or a change of
  intent, with the implementation as evidence and not as a source of requirements; fix a problem
  at its source (requirements in specify or clarify, design in plan, tasks in the task list);
  make revised intent explicit in spec, plan and tasks before code, reviewably; run analyze, which
  only reports, and reconcile the artifacts by hand; implement in validated stages; run converge,
  which only appends tasks, and repeat implement and converge until nothing is left. Implement's
  checklist gate asks before running with unchecked checklist items, never changes a checklist
  marker, and never self-approves.

Sources: [Spec Kit v1.4.0: Evolving Specifications](https://github.com/github/spec-kit/blob/v1.4.0/docs/how-to/evolving-specs.md); [Spec Kit v1.4.0: Agentic SDD reference](https://github.com/github/spec-kit/blob/v1.4.0/docs/reference/agentic-sdd.md).

# Decision

- **Assess a discovery first**: decide whether it is a defect against agreed intent (fix the code
  and add a regression test) or a change of intent, design or task breakdown. Code and tests are
  evidence, never a replacement source of requirements.
- **Fix at the source**: a requirement gap goes to `/speckit-specify` or `/speckit-clarify`, a
  design gap to the plan and its design artifacts (`plan.md`, `research.md`, `data-model.md`,
  contracts), a task gap to `tasks.md`. A gap analyze or a checklist reports is fixed in the
  artifacts, never patched in code.
- **Intent before code**: a change of intent updates spec, then plan and design artifacts, then
  tasks, in that order, and is committed as its own reviewable change before the code that
  delivers it. A contract change is reviewed before its code. Rationale that a replaced artifact
  held is kept (moved to `research.md` or a decision) before it is replaced.
- **Analyze until clean**: `/speckit-analyze` is re-run after every change to `spec.md`,
  `plan.md`, `tasks.md` or a design artifact, and its findings are fixed at their source until it
  reports no CRITICAL or HIGH finding. Analyze only reports; the artifacts are reconciled by hand.
- **Implement in validated stages**: `/speckit-implement` runs one unit of tasks at a time, and
  each unit is validated (tests, review) before the next starts. Its checklist gate is honoured:
  with unchecked checklist items it asks the owner before continuing, and it never changes a
  checklist marker or marks its own work approved.
- **Converge**: `/speckit-converge` runs after the implementation of each user story and before a
  feature is done. It is append-only: a gap becomes a new task in `tasks.md`, never a direct code
  edit, and implement and converge repeat until converge finds nothing.

Sources: [Spec Kit v1.4.0: Evolving Specifications](https://github.com/github/spec-kit/blob/v1.4.0/docs/how-to/evolving-specs.md); [Spec Kit v1.4.0: Agentic SDD reference](https://github.com/github/spec-kit/blob/v1.4.0/docs/reference/agentic-sdd.md).

# Consequences

- A design question found during a unit stops that unit's code until the artifacts say the answer
  and analyze is clean; the artifact change lands as its own commit.
- Feature 001's Story 2 needs a `/speckit-converge` run once its units are implemented, and the
  feature is not done until converge finds nothing.
- The constitution's Development Workflow section carries these rules (version 2.1.0).

Sources: [Project owner's request, 2026-11-14](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).
