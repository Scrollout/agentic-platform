---
type: Component
title: git
description: The version control CLI the project worker runs to keep each new project's version history, one commit per accepted step.
tags: [components, projects, version-history]
status: draft
resource: https://github.com/git/git
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D30
  - id: adr-0032
    resource: /decisions/0032-project-version-history-kept-by-a-project-worker.md
    title: "ADR 0032: The new project's version history is a git repository kept by a project worker"
  - id: git-docs
    resource: https://git-scm.com/docs
    title: Git reference documentation
  - id: scorecard
    resource: https://api.securityscorecards.dev/projects/github.com/git/git
    title: OpenSSF Scorecard for github.com/git/git
stale_after: 2027-05-13T00:00:00Z
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-13T20:00:00Z }
---

# Responsibility

Keeps the new project's version history, so the developer receives a normal git repository with
one commit per accepted step (FR-025). Only the project worker writes the version history; the
engine runs no git, and step containers mount `.git` read-only. Licence GPL-2.0-only. It runs as a separate program and is not linked into
platform code. OpenSSF Scorecard 5.7 on 2026-11-09.

Sources: [ADR 0032](/decisions/0032-project-version-history-kept-by-a-project-worker.md), [OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/git/git).

# Interfaces

The project worker's `commitStep` and `restoreStep` operations in the
[check contract](../../src/check/api/preview/2026-11-13-preview/openapi.yaml). Requests carry
paths, hashes and stored artifacts, never git commands or options.

Sources: [Feature 001 research, D30](../../specs/001-prose-to-accepted-slice/research.md).

# Dependencies

The check image includes the git CLI because it is built on the Debian golang pin in
`images.lock`, `golang:1.26.11-bookworm`, which has git 2.39.5 (owner, 2026-11-13; T053c). Its
earlier Alpine base had no git.

Sources: [Feature 001 research, D30](../../specs/001-prose-to-accepted-slice/research.md).

# Configuration

Every option is fixed in the project worker's code:

- `GIT_NO_REPLACE_OBJECTS=1`, no system or global configuration, `core.fsmonitor=false`,
  `core.hooksPath=/dev/null`, `gc.auto=0` and no signing;
- plumbing only (`hash-object --no-filters`, `update-index --index-info`, `write-tree`,
  `commit-tree`, `update-ref`, `cat-file`), so no attribute, ignore or filter applies and nothing
  is added to the repository's `config` or `info/`;
- paths only on standard input or after `--`;
- `git init --template=`, so no template hooks or files enter the repository;
- files committed as non-executable regular files;
- author and committer `agentic-platform <noreply@localhost>`, dated from the run record;
- a history guard: `.git` is checked as files against the project worker's own record before
  any git command runs. Step containers mount `.git` read-only, so the guard is a second layer.

Sources: [Git reference documentation](https://git-scm.com/docs), [Feature 001 research, D30](../../specs/001-prose-to-accepted-slice/research.md).
