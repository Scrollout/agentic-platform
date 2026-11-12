---
type: Playbook
title: Protect changes locally
description: Without a hosted forge, local git hooks run every check before a commit and require the owner's recorded approval for changes to protected paths, as constitution Principle XVI requires.
tags: [playbooks, governance, git]
status: draft
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T18:00:00Z }
---

# Trigger

A fresh clone of the repository, or a change to the hooks.

# Steps

1. Point git at the repository's hooks: `git config core.hooksPath scripts/hooks`.
2. Before every commit, `scripts/hooks/pre-commit` runs only the checks the staged files can
   affect, so it stays fast. It checks the staged snapshot, not the working tree: it exports the
   index into a temporary directory, runs every check there with the check scripts as staged,
   and removes the directory on exit. Untracked files and unstaged changes, such as another
   person's work in progress, cannot change the result, and a staged file whose working copy
   differs is checked as staged. It prints the snapshot, the staged files and each check it runs.
   Run the full suite, `scripts/check.sh`, before a story is reported done.
3. Go code is checked in two pinned images of the same Go release, without network:
   `go vet` and `go build` without cgo in the `golang` image, the configuration the static
   binaries ship in, then `go test -race` with cgo in the Debian-based `golang-race` image, since
   the race detector needs a C compiler. The tests run once, with the race detector.
   `scripts/check.sh` fails if the two images report different Go releases.
4. Generated Go code is not committed. After a clone, or a change to a contract, run
   `scripts/generate.sh`; `scripts/check.sh` regenerates it and fails if it differs.

   `scripts/check.sh` also runs `scripts/contract-diff.sh`, which compares each service's newest
   contract version under `src/<service>/api/{stable,preview}/` with the previous one using the
   pinned oasdiff image, without network, and fails on any breaking change (Principle XV). A
   service with one version prints `one version, nothing to compare` and passes.
5. A commit that changes a protected path (the constitution, `scripts/`, `src/deploy/`,
   `src/tests/`, test files, test data, Go module files, `.gitignore`) is refused by
   `scripts/hooks/commit-msg` unless its message carries an `Approved-by: <owner>` trailer.
6. Only the project owner adds that trailer, or tells the agent to add it with their name and the
   date. Agents never add it on their own.

# Verification

- `scripts/check.sh` ends with `all checks passed`.
- A test commit to `scripts/` without the trailer is refused.
- `scripts/hooks/pre-commit` ends with `staged snapshot passed`; an untracked or unstaged file
  that would fail a check does not change that, and the same file staged does.

# Limits

Hooks run on the developer's machine and can be skipped with `--no-verify`, so they record
approval rather than enforce it the way a hosted forge's branch protection does. When the
repository moves to a hosted forge, add branch protection and code owners there.
