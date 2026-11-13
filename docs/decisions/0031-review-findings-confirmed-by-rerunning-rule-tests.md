---
type: Decision
title: "ADR 0031: A review finding blocks only when its rule's tests, rerun against the current code, fail at the file it names"
description: The check service confirms a blocking review finding by rerunning the tests of the rule it names and requiring a failure at the named file; this replaces passing the attempt's other check verdicts as confirmations.
tags: [decisions, engine, checks, review]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's choice of how a review finding is confirmed, 2026-11-13
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Constitution Check (XX) and Project Structure
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D11 and D29
  - id: contract
    resource: ../../src/check/api/preview/2026-11-13-preview/openapi.yaml
    title: Check service contract, 2026-11-13-preview (runCheck)
  - id: adr-0026
    resource: /decisions/0026-deterministic-workflow-engine.md
    title: "ADR 0026: Deterministic workflow engine with agents as state workers"
  - id: adr-0030
    resource: /decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md
    title: "ADR 0030: Agent steps run on Docker Agent's API server; checks and procedures run in a check service with its own contract"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T18:00:00Z }
---

# Origin

Feature 001's research (D11) says a review finding blocks only when it names a rule, a file and a
line and a rule test confirms it, as [ADR 0026](/decisions/0026-deterministic-workflow-engine.md)
requires. The check contract first passed the attempt's other check verdicts to
FindingsCarryEvidence and NoBlockingFindingOpen as `confirmations`, and left open which check
would confirm a finding. The project owner chose on 2026-11-13 to confirm a finding by rerunning
its rule's tests; the research records the choice in D29, closed by T054e.

Sources: [Project owner's choice, 2026-11-13](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np); [Feature 001 research, D11 and D29](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 plan](../../specs/001-prose-to-accepted-slice/plan.md).

# Context

- The review step runs after the code step, whose own checks all passed when it was accepted, so
  no earlier verdict in `confirmations` could ever be a failing one.
- A citation alone is a weak criterion: in the ADR 0026 spike a reviewer cited a rule while saying
  the code did not break it, and the finding still blocked.
- Each rule's examples are godog scenarios under a `Rule:` block tagged with the rule's ID
  (research D3), written by the tests step before the code step; the code and fix steps cannot
  change `tests/**`, which TestsUnchanged checks.

Sources: [Feature 001 research, D3, D11 and D29](../../specs/001-prose-to-accepted-slice/research.md); [ADR 0026](/decisions/0026-deterministic-workflow-engine.md).

# Decision

- A review finding blocks only when the tests of the rule it names fail at the file it names,
  rerun against the current code. The tests of rule `R-<n>` are the godog scenarios tagged
  `@R-<n>` and the Go tests whose name names the rule. The finding is confirmed when one of them
  fails and its output names the finding's file as a project path followed by `:` and a line.
- The check service does the rerun (`go test -json`, offline) in FindingsCarryEvidence and
  NoBlockingFindingOpen. FindingsCarryEvidence asks of each blocking finding a rule, a file the
  project holds and a line of at least 1, and fails only when that evidence is missing or the
  rerun cannot be judged, such as tests that do not build. NoBlockingFindingOpen fails on each
  confirmed finding. A blocking finding the rerun does not confirm counts as a suggestion.
- This replaces `confirmations`: the check request no longer carries other checks' verdicts, the
  `check` a finding names is not consulted, and the run event no longer requires it of a blocking
  finding.
- The exact matching rule is the check contract's `runCheck` description.

Sources: [Check service contract, runCheck](../../src/check/api/preview/2026-11-13-preview/openapi.yaml); [Feature 001 research, D29](../../specs/001-prose-to-accepted-slice/research.md).

# Consequences

- A finding blocks on evidence a deterministic check produces now, not on the reviewer's word or
  on verdicts from before the review.
- Accepted limit: the rerun trusts the project's tests, so a test written by an agent can fake a
  confirmation or avoid one. The code and fix steps cannot change the tests, which narrows but
  does not remove this.
- A review whose findings name no failing test cannot block, so a real fault no rule test covers
  becomes a suggestion.
- This refines [ADR 0030](/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md),
  which placed the checks in the check service without saying how a finding is confirmed, and
  makes ADR 0026's evidence rule concrete. It contradicts neither.

Sources: [Feature 001 research, D29 (known limits)](../../specs/001-prose-to-accepted-slice/research.md); [ADR 0030](/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md).
