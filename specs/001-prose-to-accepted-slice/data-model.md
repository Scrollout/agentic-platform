# Data Model: From a description to accepted working software, one slice at a time

The schemas live with their provider, the engine, in
[`src/engine/api/preview/2026-11-12-preview/`](../../src/engine/api/preview/2026-11-12-preview/)
(moved there by T011; [contracts/](contracts/) keeps only the review record).
All identifiers are stable once assigned and never reused.
A slice, run, operation or artifact that exists but is not for the calling token is reported as
not found (404), the same as one that does not exist; 403 means the token may not call the
operation at all.

## Slice

| Field | Meaning | Rules |
|-------|---------|-------|
| `id` | `S-<n>` | unique per installation; set by the platform |
| `description` | the developer's prose | non-empty; one use case |
| `goals[]` | Goal records | at least one |
| `terms[]` | Term records | every term a rule uses |
| `rules[]` | Rule records | every goal has at least one rule |
| `examples` | Gherkin text, one `Rule:` block per rule; each scenario tagged `@R-<n>` and `@E-<n>` | every rule has at least one scenario; `E-<n>` unique in the slice and never reused (check `ExampleTagged`) |
| `contract` | OpenAPI 3.1 document as YAML text | required; every operation has an `operationId` and `x-rules: [R-…]` |
| `rule_text` | each rule's EARS sentence, by rule ID | set by the platform; never edited |
| `questions[]` | Question records | at most 10 in total per slice (FR-006) |
| `parts` | current hash of `goals`, `rules` and `contract` | SHA-256 of the RFC 8785 canonical JSON of `{goals}`, `{rules, examples, terms}` and `{contract}` |
| `approvals[]` | Approval records | one each for goals, rules and contract |
| `checks[]` | latest check results | each names the part or item, the check, whether it passed and the reason |
| `history[]` | every change in order: `proposed`, `revised`, `answered`, `changed`, `approved` | time and the IDs concerned; a proposal or revision names the model and agent definition digest |
| `state` | see lifecycle | set by the platform |
| `latest_run` | the slice's latest run ID | the run owns its status and outcome |
| `title` | a short name | derived from the first goal |

**Lifecycle**: `proposed` → `questions-open` → `checked` → `approved`. Whether the slice is
building, done, handed over or expired is the status and outcome of its latest run. Any change to
an approved part returns the slice to `checked` and withdraws that part's approval (FR-007). A slice the pattern does not fit stays
`checked` with a `PatternFit` check failed and cannot be built (FR-009).

The analyst's output is a **slice proposal**: goals, terms, rules, examples, contract and new
questions without IDs or answers; it has no `id`, `title`, `description`, `state`, `rule_text`,
`parts`, `approvals`, `checks` or `history` (see `slice-proposal.schema.json`). The parts of both
are defined once, in `slice-defs.schema.json`.

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

The EARS sentence is derived; the slice shows it in `rule_text`.

## Question

`id` (`Q-<n>`, set by the platform), `about` (a `G-`, `R-`, `T-`, `R-<n>/E-<n>` or
`contract/<operationId>` ID), `text`, `options[]` (at most 5), `answer` (`{option: 0-4}` or
`{text}` of at most 200 characters), `answered_at`.

## Approval

`part` (`goals`, `rules`, `contract`), `content_hash`, `at`. Holds only while `parts[part]` equals
`content_hash`.

## Pattern and catalogue

A pattern: `id`, `version`, `scenario`, `conditions[]` (named fit checks), `sources[]` (URL and
title), `steps[]`, `limits` (`attempts`, `review_rounds`), `budgets` (run and per step: tokens,
cost in USD, minutes), `status` (`approved`). A step: `name`, `worker` (`agent:<definition>` or
`procedure:<name>`), `agent_digest` (the agent definition's SHA-256, for an agent step),
`writable[]` (globs relative to the project folder), `dependencies` (`allowed[]`, `forbidden[]`), `checks[]`,
`inputs[]`, `outputs[]`. The catalogue is the set of approved patterns; this version holds one.

## Run, run record and accepted result

- **Run**: `id` (`RUN-<n>`), `slice`, `pattern` (`<id>@<version>`), `operation`, `project` (a
  new folder name under the projects folder), `replay_of` (a run ID, for a replay), `status`
  (`running`, `interrupted`, `waiting-for-developer`, `ended`), `outcome` once ended (`done`,
  `handed-over`, `expired`, `cancelled`), `reason`, `expires_at`. An interrupted run continues with
  resume. A run waiting for the developer ends `handed-over` when the developer accepts the
  hand-over, or `expired` 14 days after it stopped; cancelling its operation ends it `cancelled`.
- **Run record**: append-only JSON Lines. Each event: `seq`, `at`, `run`, `kind` (see research
  D23), `step`, `attempt`, `round` (review), `worker`, `model`, `agent_digest`, `pattern`,
  `inputs[]` and `artifacts[]` (hashes), `reuse_key`, `refs[]` (the `G-`, `R-`, `T-`,
  `R-<n>/E-<n>` or `contract/<operationId>` IDs it concerns), `check` (name, passed, reason),
  `finding` (rule, file, line, text, blocking, confirming check), `detail`. Each kind requires the
  fields that explain it (see `run-event.schema.json`).
- **Accepted result**: `key` (the reuse key), `artifacts[]`, `run`. Found under its key, it is
  reused (FR-019).
