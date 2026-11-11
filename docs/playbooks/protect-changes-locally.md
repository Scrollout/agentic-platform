---
type: Playbook
title: Protect changes locally
description: Without a hosted forge, local git hooks run every check before a commit and require the owner's recorded approval for changes to protected paths, as constitution Principle XVI requires.
tags: [playbooks, governance, git]
status: draft
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T02:30:00Z }
---

# Trigger

A fresh clone of the repository, or a change to the hooks.

# Steps

1. Point git at the repository's hooks: `git config core.hooksPath scripts/hooks`.
2. Before every commit, `scripts/hooks/pre-commit` runs only the checks the staged files can
   affect, so it stays fast. Run the full suite, `scripts/check.sh`, before a story is reported
   done.
3. Generated Go code is not committed. After a clone, or a change to a contract, run
   `scripts/generate.sh`; `scripts/check.sh` regenerates it and fails if it differs.
4. A commit that changes a protected path (the constitution, `scripts/`, `src/deploy/`,
   `src/tests/`, test files, test data, Go module files, `.gitignore`) is refused by
   `scripts/hooks/commit-msg` unless its message carries an `Approved-by: <owner>` trailer.
5. Only the project owner adds that trailer, or tells the agent to add it with their name and the
   date. Agents never add it on their own.

# Verification

- `scripts/check.sh` ends with `all checks passed`.
- A test commit to `scripts/` without the trailer is refused.

# Limits

Hooks run on the developer's machine and can be skipped with `--no-verify`, so they record
approval rather than enforce it the way a hosted forge's branch protection does. When the
repository moves to a hosted forge, add branch protection and code owners there.
