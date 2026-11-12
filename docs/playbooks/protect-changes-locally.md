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
3. The hook that judges a commit is the one in that commit. `core.hooksPath` points at the
   working tree, so git starts the working-tree copy of the hook, which may be half edited. The
   hook's first block reads the staged `scripts/hooks/pre-commit` from the index and, if it
   differs from the running file, runs it and returns its exit code. `sh` reads a script one
   command at a time, so the rest of the working-tree file is never read. The variable
   `AP_PRE_COMMIT_STAGED_HOOK` stops the staged hook from handing over again. A commit that
   deletes the hook is judged by the running copy. Only an unstaged edit to that first block
   still takes effect, so keep the block short.
4. Staged names are passed NUL-separated or as git pathspecs, never split on white space, so a
   name with a space or a newline is still scanned for secrets and linted. Deleted paths count
   as staged: a commit that only deletes files still has its snapshot checked, for example the
   layout check, the knowledge bundle check when a `docs/` file is deleted, and the check tests
   when a `scripts/` file is deleted. The repository built from the snapshot for the check tests
   ignores `.gitignore` and global excludes, so every tracked file is in it.
5. Go code is checked in two pinned images of the same Go release, without network:
   `go vet` and `go build` without cgo in the `golang` image, the configuration the static
   binaries ship in, then `go test -race` with cgo in the Debian-based `golang-race` image, since
   the race detector needs a C compiler. The tests run once, with the race detector.
   `scripts/check.sh` fails if the two images report different Go releases.
6. Generated Go code is not committed. After a clone, or a change to a contract, run
   `scripts/generate.sh`; `scripts/check.sh` regenerates it and fails if it differs.

   `scripts/check.sh` also runs `scripts/contract-diff.sh`, which compares each service's newest
   contract version under `src/<service>/api/{stable,preview}/` with the previous one using the
   pinned oasdiff image, without network, and fails on any breaking change (Principle XV). A
   service with one version prints `one version, nothing to compare` and passes.
7. A commit that changes a protected path (the constitution, `scripts/`, `src/deploy/`,
   `src/tests/`, test files, test data, Go module files, `.gitignore`) is refused by
   `scripts/hooks/commit-msg` unless its message carries an `Approved-by: <owner>` trailer.
8. Only the project owner adds that trailer, or tells the agent to add it with their name and the
   date. Agents never add it on their own.

# Verification

- `scripts/check.sh` ends with `all checks passed`.
- A test commit to `scripts/` without the trailer is refused.
- `scripts/hooks/pre-commit` ends with `staged snapshot passed`; an untracked or unstaged file
  that would fail a check does not change that, and the same file staged does.
- An unstaged edit that breaks `scripts/hooks/pre-commit` below its first block does not change
  the result of a commit that does not stage it. The same edit staged decides that commit.
- A staged secret in a file whose name has a space or a newline is refused, and so is a
  deletion-only commit that breaks a check.

# Limits

Hooks run on the developer's machine and can be skipped with `--no-verify`, so they record
approval rather than enforce it the way a hosted forge's branch protection does. When the
repository moves to a hosted forge, add branch protection and code owners there.
