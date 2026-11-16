---
type: Decision
title: "ADR 0039: A blocking review finding is confirmed by a new failing witness test the review writes"
description: A blocking review finding carries a new Go witness test for its rule in the review's own folder `review/r<n>/`; the check service runs only that test with coverage and confirms the finding when it fails having run the named line, per the coverage profile; the fix step must make it pass without changing it, and witnesses stay in the delivered project, unchangeable by every later step. Supersedes the confirmation part of ADR 0031.
tags: [decisions, engine, checks, review]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's answers in /speckit-clarify, 2026-11-14
  - id: spec
    resource: ../../specs/001-prose-to-accepted-slice/spec.md
    title: Feature 001 spec, Clarifications Session 2026-11-14, FR-017, US2.6
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D35 (with D29, D34)
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: adr-0031
    resource: /decisions/0031-review-findings-confirmed-by-rerunning-rule-tests.md
    title: "ADR 0031: Review findings confirmed by rerunning rule tests"
  - id: go-packages
    resource: https://pkg.go.dev/cmd/go#hdr-Package_lists_and_patterns
    title: "go command: package patterns"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-15T16:00:00Z }
---

# Origin

The T054f design (research D34) found that no review finding could block: the code step passes
every derived test before the review, and [ADR 0031](/decisions/0031-review-findings-confirmed-by-rerunning-rule-tests.md)
confirmed a finding only by rerunning those same tests. The owner answered in `/speckit-clarify`
on 2026-11-14 (FR-017, US2.6); research D35 holds the design.

Sources: [Feature 001 spec](../../specs/001-prose-to-accepted-slice/spec.md); [Feature 001 research, D34 and D35](../../specs/001-prose-to-accepted-slice/research.md).

# Context

- Every later step's checks recompute the hash of the tests step's files under `tests/**`.
- `go test ./...` runs every package of the project, so a test outside `tests/` still runs in
  AllTestsPass.
- Step containers mount only the step's writable folders writable.

Sources: [Feature 001 research, D29 and D30](../../specs/001-prose-to-accepted-slice/research.md); [go command: package patterns](https://pkg.go.dev/cmd/go#hdr-Package_lists_and_patterns).

# Decision

- The review step writes witness tests in `review/r<n>/` for rule `R-<n>`, its only writable
  folder. A blocking finding names its witness, a new test file there and a test function
  starting with `TestR<n>`.
- The check service runs only that witness test against the current code, with coverage. The
  finding blocks only when the test fails and its coverage profile shows the finding's line run,
  less a baseline run of the same package with no test (which removes lines package
  initialisation runs);
  printed text is never evidence, so a witness that only prints the location, or fails before
  reaching the line, confirms nothing. The finding's file must be one the fix step may change.
  Otherwise the finding is a suggestion.
- No step may change or remove a witness after its round; a later review may only add one. The
  fix step must make it pass, which AllTestsPass judges. Witnesses stay in the delivered project.
- Rejected: witnesses under `tests/` (they would change the derived tests' hash); removing them
  after their round; the reviewer's word or other checks' verdicts.

Sources: [Feature 001 research, D35](../../specs/001-prose-to-accepted-slice/research.md).

# Consequences

- A review round can block on evidence produced from the current code, so US2.6 and FR-016's
  round limit can be reached and tested.
- The check contract, the run event's finding, the pattern's review step, the launcher's review
  mount, the review agent and its evals change; the live step evals run again (about $1 to $2).
- A witness may test something else than its rule; the check proves only that it is new, in its
  rule's folder, and failed having run the named line. One that runs the line and then fails
  whatever the code does leaves the fix unable to pass, so the run is handed over, never accepted.
- Coverage in `set` mode marks a whole block once entered, so a line after an early exit in an
  entered block counts as run.
- Generate, the one earlier step that could write under `review/`, is stopped there by
  ScopeKept.
- Supersedes the confirmation part of ADR 0031; its evidence rule (a rule, a file and a line)
  stays.
- [ADR 0042](/decisions/0042-only-a-confirmed-finding-is-recorded-as-blocking.md) (owner,
  2026-11-15) amends what "blocking" means in the run record: only a finding NoBlockingFindingOpen
  confirmed is recorded as blocking; the confirmation rule here is unchanged.

Sources: [Feature 001 research, D35](../../specs/001-prose-to-accepted-slice/research.md); [ADR 0031](/decisions/0031-review-findings-confirmed-by-rerunning-rule-tests.md).
