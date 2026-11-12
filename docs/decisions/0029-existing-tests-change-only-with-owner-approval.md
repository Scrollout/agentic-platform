---
type: Decision
title: "ADR 0029: Existing tests change only with the owner's approval"
description: An existing test, fixture, grader, threshold or eval case changes only after the project owner approves that change in the commit that makes it; agents refuse briefs that would change one and report them; a fresh agent's judgement no longer suffices.
tags: [decisions, constitution, testing]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's request, 2026-11-12
  - id: adr-0025
    resource: /decisions/0025-engineering-principles.md
    title: "ADR 0025: Engineering Principles"
  - id: impossiblebench
    resource: https://arxiv.org/abs/2510.20270
    title: "ImpossibleBench"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T12:00:00Z }
---

# Origin

The project owner asked on 2026-11-12 for rules that keep agents from altering tests to make
failing code pass: agents must refuse any brief that changes an existing test and report it to the
owner, and an existing test changes only after the owner's recorded approval, not on the author's
say-so. The owner's request is the approval of this record and of the constitution amendment that
carries it (version 2.0.0).

Sources: [Project owner's request, 2026-11-12](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).

# Context

- ADR 0025's rule "Tests change only through a fresh agent" let a fresh agent judge a change to a
  test. In feature 001 the coordinating agent also briefed test authors to change existing tests
  and fixtures, for example to match a design it had decided itself; the fresh agent then
  executed the brief, so in effect the change rested on an agent's say-so.
- Models given a failing test and code to fix edit or delete the test more often than they admit,
  which is why the rule exists at all.
- A change to an existing test file is visible to a commit hook; whether a changed line alters
  what an existing test proves is not reliably decidable by a script.

Sources: [ADR 0025: Engineering Principles](/decisions/0025-engineering-principles.md); [ImpossibleBench](https://arxiv.org/abs/2510.20270).

# Decision

- **What counts as an existing test**: any test file, test helper, fixture, test data, planted
  case, grader, threshold or eval case that exists in the commit being changed (for example
  `*_test.go`, `testdata/`, `scripts/tests/`, `src/tests/`, evals, and test scripts such as
  `src/deploy/gateway/test_gateway.sh`). Modifying, renaming, skipping or deleting one changes it,
  including adding cases to an existing file. Adding a new test file does not.
- **Owner approval**: such a change is made only after the project owner approves that specific
  change, and the commit that makes it records it with the trailer
  `Tests-approved-by: <owner name> (<date>)`. No standing approval covers it. A fresh agent may
  propose and justify the change from the spec and the contract; its judgement does not replace
  the owner's approval.
- **Agents refuse**: an agent briefed to change an existing test refuses that part of the brief,
  does the rest, and reports the requested change to the owner with what it would change and
  why. A coordinating agent does not brief such a change without the owner's approval first.
- **Enforced**: the commit hook rejects a commit that modifies, renames or deletes an existing
  test file without that trailer.

Sources: [Project owner's request, 2026-11-12](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np).

# Consequences

- New test cases go into new test files unless the owner approves extending an existing one.
- Fixing a wrong test stays possible, through the owner, as "Fix the cause" requires.
- This supersedes the part of ADR 0025 that let a fresh agent approve a test change.

Sources: [ADR 0025: Engineering Principles](/decisions/0025-engineering-principles.md).
