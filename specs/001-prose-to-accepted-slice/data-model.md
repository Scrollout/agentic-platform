# Data Model: From a description to accepted working software, one slice at a time

The schemas in [contracts/](contracts/) are drafts of the contract. The authoritative copies live
with their provider, under `src/engine/api/preview/2026-11-12-preview/`, once task T011 moves them.
All identifiers are stable once assigned and never reused.

## Slice

| Field | Meaning | Rules |
|-------|---------|-------|
| `id` | `S-<n>` | unique per installation; set by the platform |
| `description` | the developer's prose | non-empty; one use case |
| `goals[]` | Goal records | at least one |
| `terms[]` | Term records | every term a rule uses |
| `rules[]` | Rule records | every goal has at least one rule |
| `examples` | Gherkin text, one `Rule:` block per rule, tagged `@R-<n>` | every rule has at least one scenario |
| `contract` | OpenAPI 3.1 document | required; every operation has `x-rules: [R-…]` |
| `questions[]` | Question records | at most 10 in total per slice (FR-006) |
| `parts` | current hash of `goals`, `rules` and `contract` | SHA-256 of RFC 8785 canonical JSON; `rules` covers rules, examples and terms |
| `approvals[]` | Approval records | one each for goals, rules and contract |
| `checks[]` | latest check results | each names the part, the check and the reason |
| `state` | see lifecycle | set by the platform |
| `title` | a short name | derived from the first goal |

**Lifecycle**: `proposed` → `questions-open` → `checked` → `approved`. A run moves it to
`building`, then `done`, `handed-over` or `expired`. Any change to an approved part returns the slice
to `checked` and withdraws that part's approval (FR-007). A slice the pattern does not fit stays
`checked` with a `PatternFit` check failed and cannot be built (FR-009).

The analyst's output is a **slice proposal**: the same fields without `id`, `state`, `parts`,
`approvals`, `checks` and derived rule `text` (see `slice-proposal.schema.json`).

## Goal

`id` (`G-<n>`), `text`. Covered when at least one rule has `goal: G-<n>`.

## Term

`id` (`T-<n>`), `name`, `definition`. Defined once; referenced by ID.

## Rule

Structured EARS:

| Field | Meaning |
|-------|---------|
| `id` | `R-<n>` |
| `goal` | the `G-<n>` it serves |
| `pattern` | `ubiquitous`, `event`, `state`, `unwanted` or `option` |
| `precondition` | required for `state` |
| `trigger` | required for `event` and `unwanted` |
| `feature` | required for `option` |
| `system` | the system name |
| `response` | what the system shall do |
| `terms` | `T-<n>` IDs used in the fields above |
| `text` | derived EARS sentence; never edited by hand |

## Question

`id` (`Q-<n>`), `about` (a `G-`, `R-`, `T-` or operation ID), `text`, `options[]` (at most 5),
`answer` (at most 200 characters for a free answer), `answered_at`.

## Approval

`part` (`goals`, `rules`, `contract`), `content_hash`, `at`. Holds only while `parts[part]` equals
`content_hash`.

## Pattern and catalogue

A pattern: `id`, `version`, `scenario`, `conditions[]` (named fit checks), `sources[]` (URL and
title), `steps[]`, `limits` (`attempts`, `review_rounds`), `budgets` (run and per step: tokens,
cost in USD, minutes), `status` (`approved`). A step: `name`, `worker` (`agent:<definition>` or
`procedure:<name>`), `writable[]`, `dependencies` (`allowed[]`, `forbidden[]`), `checks[]`,
`inputs[]`, `outputs[]`. The catalogue is the set of approved patterns; this version holds one.

## Run, run record and accepted result

- **Run**: `id` (`RUN-<n>`), `slice`, `pattern@version`, `operation`, `project` (the folder),
  `replay_of` (a run ID, for a replay), `outcome` (`done`, `handed-over`, `expired`, or none while
  running), `reason`.
- **Run record**: append-only JSON Lines. Each event: `seq`, `at`, `run`, `kind` (see research
  D23), `step`, `attempt`, `model`, `agent_digest`, `pattern`, `inputs[]` and `artifacts[]`
  (hashes), `reuse_key`, `refs[]` (the `G-`, `R-` or `T-` IDs it concerns, or `R-<n>/E-<n>` for an example), `check`
  (name, passed, reason), `finding` (rule, file, line, text, blocking), `detail`.
- **Accepted result**: `key` (the reuse key), `artifacts[]`, `run`. Found under its key, it is
  reused (FR-019).
