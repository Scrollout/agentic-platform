---
type: Decision
title: "ADR 0044: The full checks run on GitHub-hosted runners, in a public repository"
description: Superseded in part by ADR 0048 (when the evals run). The repository moves to github.com/Scrollout/agentic-platform, public; GitHub Actions runs the full scripts/check.sh and the acceptance suites on standard hosted runners on every push, the live evals with the test key from the environment dev when an agent, prompt, model or Docker Agent changes, and the pre-commit hook keeps only the quick static checks. No pull requests, branch protection or code owners.
tags: [decisions, ci, supply-chain, security]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decisions, 2026-11-16
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D41
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions, Constitution Check and Complexity Tracking
  - id: tasks
    resource: ../../specs/001-prose-to-accepted-slice/tasks.md
    title: Feature 001 tasks, T106 to T109
  - id: reference
    resource: /references/github-actions.md
    title: GitHub Actions reference
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Constitution, Principles XIII and XVI
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-16T12:00:00Z }
---

# Origin

> Superseded in part by [ADR 0048](/decisions/0048-evals-started-by-hand-and-gated-before-main.md): the live evals run only when the owner starts them, and a keyless check job fails on `main` while a suite's evals are pending.

On 2026-11-12 the owner set GitHub aside, and `scripts/check.sh` with the git hooks became the
project's CI on the owner's host. That host is small and shared with other projects: the
pre-commit hook took 12 to 25 minutes, heavy runs had to go one at a time, and on 2026-11-15 a
hook run timed out under another project's load (T105). On 2026-11-16 the owner chose GitHub's
hosted runners in a public repository of the Scrollout organization, the live evals in CI with
the key as a repository secret in an environment, and a simple, fast process without pull
requests. This record replaces the "no forge" basis of the Principle XIII and XVI exceptions.

Sources: [Feature 001 research, D41](../../specs/001-prose-to-accepted-slice/research.md); [Feature 001 plan](../../specs/001-prose-to-accepted-slice/plan.md).

# Context

- Standard GitHub-hosted runners are free and unlimited in public repositories; a Free
  organization runs up to 20 such jobs at once, each up to 6 hours, on a 4-CPU, 16 GB machine
  with a 14 GB SSD by the runner reference.
- Secrets other than `GITHUB_TOKEN` are not given to workflows started by a fork's pull request,
  and an environment's secrets reach only the jobs that name it.
- The acceptance suites run steps under gVisor, which installs on Ubuntu from its apt repository.
- Self-hosted runners on the owner's PC (`Scrollout/platform-infra`) share the same hardware.

Sources: [GitHub Actions reference](/references/github-actions.md); [gVisor guide](/guides/install-gvisor.md).

# Decision

- **Repository**: `github.com/Scrollout/agentic-platform`, public, with its full history. The
  history is scanned for credentials before it is published (T106).
- **CI**: on every push, and when started by hand, GitHub Actions runs every step of
  `scripts/check.sh` on standard hosted Linux runners, split into parallel jobs, including the
  contract, Docker and acceptance suites with gVisor. Each job builds what it needs from source and
  signs it with a key made for the job; nothing is pushed outside the runner. Actions are pinned by
  commit hash and gVisor by release and SHA-512; `GITHUB_TOKEN` is read-only. The workflows in
  `.github/` are a protected path: a change needs the owner's `Approved-by:`.
- **Evals**: the live evals run in their own job when a prompt, agent definition, model pin or
  Docker Agent version changes, and when started by hand. The test-scoped key (its own workspace,
  $50 monthly limit) is a secret of the environment `dev`, without required approval, given only
  to that job.
- **No secret for a fork**: no workflow has a `pull_request` or `pull_request_target` trigger.
- **Hooks**: the pre-commit hook keeps only the quick static checks of what the commit changes,
  about 2 minutes at most. The commit-msg hook's `Approved-by:` and `Tests-approved-by:` rules stay.
- **Process**: no pull requests, required checks or code owners; a ruleset only blocks force-push
  and deletion of `main` and the feature branch. The owner commits and pushes; a red run is fixed
  by the next commit.

# Consequences

- The host runs no heavy suite for a commit. A full run takes the time of its longest job, and
  several pushes can be checked at once.
- A commit is accepted before CI judges it, and a red run does not block a push. The Principle
  XVI exception stays, now for a one-person project; the Principle XIII exception for automated
  update pull requests stays, with a monthly manual review, and a new one covers the hosted runner
  image, which is named by label and cannot be pinned.
- The code and its history are public, including the commit author's name and e-mail (owner,
  2026-11-16). The evals key is the only secret; it is test-scoped and capped.
- gVisor on a hosted runner is proven by the first run (T107); if it cannot run there, the
  acceptance suites stay on the host until a decision supersedes this one.
- The hook change is a change to a check: its tests change only with the owner's approval (T108).
- The work is T106 to T109.

Sources: [Feature 001 tasks, T106 to T109](../../specs/001-prose-to-accepted-slice/tasks.md); [Constitution](../../.specify/memory/constitution.md).
