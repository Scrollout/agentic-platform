---
type: Decision
title: "ADR 0032: The new project's version history is a git repository kept by a project worker"
description: A project worker, a third deployment of the check program with the projects folder writable, commits each accepted step, restores the project to its last commit before every attempt, and writes a replay's reused step files; the engine keeps no git and no project mount.
tags: [decisions, engine, checks, projects, git]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decision on the project's version history, 2026-11-13
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D30 (with D5, D8, D14, D29)
  - id: data-model
    resource: ../../specs/001-prose-to-accepted-slice/data-model.md
    title: Feature 001 data model, run rules
  - id: contract
    resource: ../../src/check/api/preview/2026-11-13-preview/openapi.yaml
    title: Check service contract, 2026-11-13-preview (commitStep, restoreStep)
  - id: git-commit
    resource: https://git-scm.com/docs/git-commit
    title: git-commit documentation
  - id: adr-0030
    resource: /decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md
    title: "ADR 0030: Agent steps run on Docker Agent's API server; checks and procedures run in a check service with its own contract"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T21:00:00Z }
---

# Origin

Feature 001 builds each slice into a new project that the developer receives with its own version
history (FR-025). A run spans several components and writes project files, so it needs a pivot, a
compensation and a timeout. A replay also has to write reused step files into its new project.
[ADR 0030](/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md) left open
how the version history is kept and which component restores and writes files. The project owner
decided on 2026-11-13. The feature's research records it as D30, and the plan lists it in its
Decisions table.

Sources: [Project owner's decision, 2026-11-13](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np); [Feature 001 research, D29 and D30](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 plan](../../specs/001-prose-to-accepted-slice/plan.md).

# Context

- The engine has no project mount and no git. The check service mounts the projects folder
  read-only. Each procedure step worker can write only its own step's paths.
- The engine stores every accepted output by hash. Every attempt and check names the project's
  files as `before`, a list of paths and hashes.
- An agent step may write anything in its writable paths, so a rejected attempt's files must be
  undone before the next attempt.

Sources: [Feature 001 research, D5, D8 and D29](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- The project's version history is a git repository in the project folder, with one commit per
  accepted step on the branch `main`. The developer receives a plain folder holding a normal git
  repository.
- A project worker keeps it. The worker is a third deployment of the check program, under gVisor,
  with the projects folder writable and its own engine-issued token. It serves two operations of
  the check contract:
  - `commitStep` commits an accepted step's files. With `reused_from` and the stored artifacts, it
    first writes a reused step's files into the project. A repeated call finds its own commit and
    makes no second one.
  - `restoreStep` makes the folder hold the last accepted state from the last commit. The engine
    calls it before every attempt. This is the run's compensation.
- Requests carry paths, hashes and stored artifacts, never commands. Commits are deterministic:
  the author and committer are `agentic-platform <noreply@localhost>`, dated from the run record,
  and the message trailers name the run, step, round and attempt.
- The pivot is the run's outcome recorded with the project handed over (`done` or
  `handed-over`). After it nothing is undone.
- The version history is not part of the project. No other operation reads, lists, judges or
  writes `.git`.
- The check image is built on the Debian golang pin already in `images.lock`, which has git. The
  larger image is accepted. `apk add git` was rejected because the package is not pinned by digest.
- Only the generate procedure worker gets the project root writable, since its files `go.mod`
  and `go.sum` are in the root. Every other step's container gets only its own writable paths.
- No step container can write `.git`. `ap build-workers` creates an empty `.git` folder in the
  new project and binds it read-only into every step container, on top of the generate worker's
  writable root. The project worker creates the repository in it at the run's first
  `commitStep`, after contract-check and before generate.
- A history guard is a second layer. The project worker keeps a record per project on a volume
  only it mounts: the last commit ID, its parent, its trailers and the hash of the `.git/config`
  it wrote, replaced atomically. Before any git command it checks `.git` against the record, and
  refuses symbolic links, `commondir`, `gitdir`, alternates, `shallow`, grafts and a packed
  `main`. On a difference it answers 409 `HistoryChanged`.
- Git runs only through filter-free plumbing, with replace objects, fsmonitor, hooks and
  automatic gc off. Paths reach it only on standard input or after `--`.
- The exact rules are the check contract's and the data model's run rules.

Sources: [Check service contract](../../src/check/api/preview/2026-11-13-preview/openapi.yaml); [Feature 001 data model, run rules](../../specs/001-prose-to-accepted-slice/data-model.md); [git-commit](https://git-scm.com/docs/git-commit).

# Consequences

- A replay gives byte-identical artifacts and, step by step, byte-identical files and commit
  trees. Commit IDs differ, because each commit names its own run.
- The engine still runs no git and reads no project file. Only one component writes the version
  history.
- Rejected and interrupted attempts are undone the same way, so resume needs no separate
  clean-up.
- Accepted limit: the project worker can write any project under the projects folder. Only the
  engine can call it, and the engine names only its run's new project.
- Accepted limit: the generate worker can write the whole project except `.git`. It is fixed
  code, not an agent, and ScopeKept judges its files.
- What remains for the guard: the developer, host processes and the project worker itself can
  still change `.git`. The guard then stops the run instead of trusting the history.
- The check image is larger.
- This adds to ADR 0030 and closes what it left open. It contradicts nothing in it.

Sources: [Feature 001 research, D30](../../specs/001-prose-to-accepted-slice/research.md); [ADR 0030](/decisions/0030-agent-steps-on-docker-agent-checks-in-a-check-service.md).
