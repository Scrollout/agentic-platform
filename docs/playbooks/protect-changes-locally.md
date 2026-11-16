---
type: Playbook
title: Protect changes locally
description: Local git hooks run the quick static checks before a commit and require the owner's recorded approval for changes to protected paths, as constitution Principle XVI requires; CI on GitHub runs every check on each push.
tags: [playbooks, governance, git]
status: draft
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T18:00:00Z }
---

# Trigger

A fresh clone of the repository, or a change to the hooks.

# Steps

1. Point git at the repository's hooks: `git config core.hooksPath scripts/hooks`.
2. Before every commit, `scripts/hooks/pre-commit` runs only the quick static checks the staged
   files can affect: root layout, secret scan, knowledge bundle, markdown, contract lint,
   committed generated blocks, and `gofmt` and `go vet` of each changed Go module. No test suite
   runs there, so a commit takes at most about 2 minutes; it prints `pre-commit: done in <n> s`. It checks the staged snapshot, not the working tree: it exports the
   index into a temporary directory, runs every check there with the check scripts as staged,
   and removes the directory on exit. Untracked files and unstaged changes, such as another
   person's work in progress, cannot change the result, and a staged file whose working copy
   differs is checked as staged. It prints the snapshot, the staged files and each check it runs.
   The full suite, `scripts/check.sh`, runs in CI (GitHub Actions) on every push, and a task is
   done when that run is green ([ADR 0044](/decisions/0044-full-checks-run-on-github-hosted-runners.md)).
   Locally, run only targeted tests that take seconds to a few minutes; the Docker step, `ap test`
   and `e2e.sh` run in CI, and a fresh review judges that CI run
   ([ADR 0049](/decisions/0049-fresh-review-judges-the-ci-run.md)). The live evals run only when
   the owner starts them ([ADR 0048](/decisions/0048-evals-started-by-hand-and-gated-before-main.md)).
   The owner merges the feature branch into `main` only when the `evals-current` job on the
   feature branch's head names no pending suite; start the evals there first
   (`gh workflow run evals.yml --ref <branch>`).
3. The hook that judges a commit is the one in that commit. `core.hooksPath` points at the
   working tree, so git starts the working-tree copy of the hook, which may be half edited. The
   hook's first block reads the staged `scripts/hooks/pre-commit` from the index and, if it
   differs from the running file, copies it into a fresh `$TMPDIR/pre-commit-hook.*` folder and
   `exec`s it with the same arguments. `sh` reads a script one command at a time, so the rest of
   the working-tree file is never read. No variable decides the handover: a run is the staged
   hook exactly when its own file equals the staged one, so nothing a caller sets in the
   environment can skip it. Because `exec` keeps the process, git waits for the staged copy and
   a signal sent to the hook reaches it directly; there is no parent left to forward signals.
   The staged copy recognises its folder by name and removes it once it has compared itself.
   `scripts/hooks/commit-msg` does the same with the staged `scripts/hooks/commit-msg` (folder
   `$TMPDIR/commit-msg-hook.*`), keeping its interface: `commit-msg <message-file>`, exit 0 to
   accept, anything else to refuse. A commit that deletes a hook is judged by the running copy.
   Only an unstaged edit to that first block still takes effect, so keep the block short.
4. Staged names are passed NUL-separated or as git pathspecs, never split on white space, so a
   name with a space or a newline is still scanned for secrets and linted. Deleted paths count
   as staged: a commit that only deletes files still has its snapshot checked, for example the
   layout check, and the knowledge bundle check when a `docs/` file is deleted.
   The secret scan fails closed: it reads only regular files from the snapshot, scans a
   symlink's target text (what git stores) instead of following it, so a link to a file outside
   the repository is never read, and fails the hook when `xargs`, `grep` or `readlink` cannot
   run, exits with an error or is killed.
5. The hook checks Go in the pinned `golang` image, without network: for each Go module with a
   staged path, `gofmt -l` (testdata excluded) and `go vet`, after generating the code in the
   snapshot. `scripts/check.sh`, in CI, checks Go in two pinned images of the same Go release:
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
7. A commit that changes a protected path (the constitution, any file under `.github/`,
   `scripts/`, `src/deploy/`, `src/tests/`, test files, test data, Go module files, `.gitignore`)
   is refused by `scripts/hooks/commit-msg` unless its message carries an `Approved-by: <owner>`
   trailer. It reads the staged names NUL-separated and matches each whole name (`grep -z`), so neither a
   name git would quote, such as one with a non-ASCII letter, nor one with a newline can slip
   past a pattern. It fails closed: when `git diff`, `git interpret-trailers` or `grep` fails,
   the commit is refused with a message.
8. Only the project owner adds that trailer, or tells the agent to add it with their name and the
   date. Agents never add it on their own.
9. A commit that modifies, renames or deletes an existing test file (`*_test.go`, anything under
   a `testdata/` folder, `scripts/tests/` including fixtures, `src/tests/`, `test_*.sh`,
   `test_*.py`) is refused unless its final trailer block holds
   `Tests-approved-by: <owner name> (<YYYY-MM-DD>)`, as
   [ADR 0029](/decisions/0029-existing-tests-change-only-with-owner-approval.md) requires. A
   renamed test counts by its old path. The refusal lists every such file. `Approved-by` does not
   cover it. The trailer is written only after the owner approves that specific change, never
   under a standing approval, and a fresh agent's judgement does not replace it. A commit that
   only adds test files needs no `Tests-approved-by` trailer.
10. A commit that stages non-test Go code together with test files, even new ones, is refused
   without a `Tests-approved-by` trailer. A unit is committed once its separate author's tests and
   the code that makes them pass both pass, never its failing tests alone, and the push's CI run
   must then be green (the hook no longer runs `go test`, ADR 0044): one commit with the owner's
   `Tests-approved-by:` trailer, after the owner approves that commit (owner's choice,
   2026-11-13). Test-first stays shown by the order of work: the tests are written by a separate
   author and seen failing before the code exists, and the fresh review checks it.

# Verification

- `scripts/check.sh` ends with `all checks passed`.
- A test commit to `scripts/` without the trailer is refused.
- A commit that modifies an existing `*_test.go` with only `Approved-by` is refused and names
  the file; one that only adds a test file is accepted.
- `scripts/hooks/pre-commit` ends with `staged snapshot passed`; an untracked or unstaged file
  that would fail a check does not change that, and the same file staged does.
- An unstaged edit that breaks `scripts/hooks/pre-commit` below its first block does not change
  the result of a commit that does not stage it. The same edit staged decides that commit.
- A staged secret in a file whose name has a space or a newline is refused, and so is a
  deletion-only commit that breaks a check.
- A `grep` on `PATH` that exits 127 fails the hook; a staged symlink to `/etc/hostname` passes
  without its content in the output; a staged symlink whose target text holds a secret is
  refused.
- `AP_PRE_COMMIT_STAGED_HOOK=1 git commit` still hands over to a differing staged hook, and a
  staged `scripts/hooks/commit-msg` that refuses decides the commit even when the working copy
  is good.

# Limits

Hooks run on the developer's machine and can be skipped with `--no-verify`, so they record
approval rather than enforce it the way a hosted forge's branch protection does. The repository
is on GitHub without pull requests (ADR 0044): a commit is accepted before CI judges it, and a red
run does not block a push.

The handover lives in the running file: a working-tree hook whose first block is missing or
broken is not handed over, and a working copy without the block, such as `exit 0`, decides the
commit itself. A staged hook that lacks the block does not remove its `$TMPDIR/*-hook.*` folder,
which holds only that copy. A caller can make a hook that runs as
`$TMPDIR/pre-commit-hook.<name>/pre-commit` (or the `commit-msg` equivalent) remove that one
folder; a `<name>` holding `/` or `..` is never removed. A signal sent to the hook process
alone takes effect when the running check ends; a signal to the whole process group, as Ctrl-C, `timeout`
or a supervisor sends, stops the check at once. When git itself is killed it does not wait for
the hook, which removes its temporary folders a moment later.
