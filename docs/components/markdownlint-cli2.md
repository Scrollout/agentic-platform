---
type: Component
title: markdownlint-cli2
description: "The Markdown linter scripts/check.sh and the commit hook run on docs, specs and source Markdown."
tags: [components, documentation, lint]
status: draft
resource: https://github.com/DavidAnson/markdownlint-cli2
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research
  - id: mdl-repo
    resource: https://github.com/DavidAnson/markdownlint-cli2
    title: markdownlint-cli2 repository
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-13T12:00:00Z }
---

# Responsibility

Keeps Markdown in `docs/`, `specs/`, `src/` and the root files consistent. Its licence and Scorecard are
not recorded in research D22; the check is open under T063.

Sources: [Feature 001 research](../../specs/001-prose-to-accepted-slice/research.md), [repository](https://github.com/DavidAnson/markdownlint-cli2).

# Interfaces

`scripts/npm-tool.sh markdownlint-cli2 --config scripts/repo.markdownlint-cli2.jsonc <files>`; run
by `scripts/check.sh` and the pre-commit hook.

# Dependencies

The pinned Node image.

# Configuration

`markdownlint-cli2` 0.18.1 in `scripts/tools/package.json` and the lock file; the config turns off
MD013, MD041 and MD025 and sets MD024 to `siblings_only`.

# Operations

Run offline from the lock file by `scripts/npm-tool.sh`.
