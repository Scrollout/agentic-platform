---
type: Reference
title: Go mutation testing tools
description: What gremlins and go-mutesting (avito-tech's fork and zimmski's original) do, how healthy they are, and why none plants user-defined mutants, as read on 2026-11-21 for the step agents' evals.
resource: https://gremlins.dev/latest/usage/commands/unleash/
tags: [references, evals, mutation-testing]
status: draft
stale_after: 2027-05-21T00:00:00Z
sources:
  - id: gremlins
    resource: https://api.github.com/repos/go-gremlins/gremlins
    title: go-gremlins/gremlins repository
  - id: gremlins-docs
    resource: https://gremlins.dev/latest/usage/configuration/
    title: Gremlins configuration
  - id: avito
    resource: https://raw.githubusercontent.com/avito-tech/go-mutesting/master/README.md
    title: avito-tech/go-mutesting README
  - id: zimmski
    resource: https://api.securityscorecards.dev/projects/github.com/zimmski/go-mutesting
    title: Scorecard of zimmski/go-mutesting
  - id: concise
    resource: https://best.openssf.org/Concise-Guide-for-Evaluating-Open-Source-Software.html
    title: OpenSSF Concise Guide for Evaluating Open Source Software
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-21T16:00:00Z }
---

# The tools

| Tool | Licence | Latest release | Activity | Scorecard |
|------|---------|----------------|----------|-----------|
| go-gremlins/gremlins | Apache-2.0 | v0.6.0, 2025-12-06 | last commit 2026-03-30; 5 contributors in 12 months | no result |
| avito-tech/go-mutesting | MIT | v2.3.1, 2025-12-26 | last commit 2026-10-02 | no result |
| zimmski/go-mutesting | MIT | v1.2, 2021-06-10 | no commit since 2021 | 2.3 (2026-10-26) |

Sources: [gremlins repository](https://api.github.com/repos/go-gremlins/gremlins); [avito-tech README](https://raw.githubusercontent.com/avito-tech/go-mutesting/master/README.md); [Scorecard of zimmski/go-mutesting](https://api.securityscorecards.dev/projects/github.com/zimmski/go-mutesting).

# What they do

- All three apply generic operators (arithmetic, conditional boundaries and negation,
  increment and decrement, inverted logic) to every line they reach. None plants a mutant the
  user defines.
- gremlins chooses operators by flag, excludes files by regex, sets thresholds on efficacy and
  coverage, and ships binaries with a checksum file; no custom test command was found.
- avito-tech's go-mutesting runs a custom test script (`--exec`), selects functions by regex
  (`--match`), writes a JSON report of killed, escaped, timed-out and errored mutants, and
  excludes equivalent mutants only by a checksum list that changes whenever the source does.
- None detects equivalent mutants, so "every mutant killed" is not a reachable bar for generated
  mutants.

Sources: [Gremlins configuration](https://gremlins.dev/latest/usage/configuration/); [avito-tech README](https://raw.githubusercontent.com/avito-tech/go-mutesting/master/README.md).

# Use in this project

The step evals plant their mutants by hand ([ADR 0075](/decisions/0075-eval-mutants-planted-by-hand.md)).
A tool remains a candidate for measuring how much of a real project's code its tests check,
judged against the [OpenSSF Concise Guide](https://best.openssf.org/Concise-Guide-for-Evaluating-Open-Source-Software.html)
when that need arises.

Sources: [OpenSSF Concise Guide](https://best.openssf.org/Concise-Guide-for-Evaluating-Open-Source-Software.html).
