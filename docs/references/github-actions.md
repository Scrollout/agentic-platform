---
type: Reference
title: GitHub Actions
description: What GitHub Actions gives a public repository on standard hosted runners (cost, machine size, concurrency, job time) and how secrets reach workflows (forks, environments), as checked on 2026-11-16.
resource: https://docs.github.com/en/actions
tags: [references, ci, github, security]
status: draft
stale_after: 2027-05-16T00:00:00Z
sources:
  - id: billing
    resource: https://docs.github.com/en/actions/concepts/billing-and-usage
    title: GitHub Actions billing and usage
  - id: limits
    resource: https://docs.github.com/en/actions/reference/limits
    title: Actions limits
  - id: runners
    resource: https://docs.github.com/en/actions/reference/runners/github-hosted-runners
    title: GitHub-hosted runners reference
  - id: pricing
    resource: https://docs.github.com/en/billing/reference/actions-runner-pricing
    title: Actions runner pricing
  - id: runner-images
    resource: https://github.com/actions/runner-images
    title: GitHub Actions runner images
  - id: ubuntu-2604-preview
    resource: https://github.com/actions/runner-images/issues/14226
    title: Ubuntu 26.04 runner image public preview
  - id: secrets
    resource: https://docs.github.com/actions/security-guides/encrypted-secrets
    title: Using secrets in GitHub Actions
  - id: secret-types
    resource: https://docs.github.com/en/code-security/reference/secret-security/secret-types
    title: Understanding GitHub secret types
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-16T12:00:00Z }
---

# Hosted runners

- Standard GitHub-hosted runners, and self-hosted runners, are free in public repositories.
  Larger runners are billed in public repositories too. A private repository gets a monthly
  minute quota.
- The Linux runner behind `ubuntu-latest`, `ubuntu-26.04`, `ubuntu-24.04` and `ubuntu-22.04` has 4 CPUs, 16 GB
  of memory and a 14 GB SSD, by the runner reference. The pricing page still lists a 2-core Linux
  runner; the reference is the newer page.
- `ubuntu-26.04` is a public preview: its image 20261111.196.1 has Python 3.14.4 and Docker 29.4.2.
  `ubuntu-latest` still means `ubuntu-24.04`.
- Concurrent standard-runner jobs: 20 on Free, 40 on Pro, 60 on Team, 500 on Enterprise. A job
  runs for at most 6 hours. GitHub says these limits may change.
- Each job gets a new virtual machine; nothing installed by one job is there for the next.

Sources: [Billing and usage](https://docs.github.com/en/actions/concepts/billing-and-usage); [Actions limits](https://docs.github.com/en/actions/reference/limits); [GitHub-hosted runners reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners); [Actions runner pricing](https://docs.github.com/en/billing/reference/actions-runner-pricing); [runner-images README](https://github.com/actions/runner-images); [Ubuntu 26.04 image readme](https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2604-Readme.md); [Ubuntu 26.04 public preview](https://github.com/actions/runner-images/issues/14226).

# Secrets

- Except `GITHUB_TOKEN`, secrets are not passed to a workflow started by a pull request from a
  fork, and that token is read-only there. A private repository's admin can change this.
- An environment's secrets are available only to jobs that reference that environment.
  Environments can require reviewers before a job that uses them runs.

Sources: [Using secrets in GitHub Actions](https://docs.github.com/actions/security-guides/encrypted-secrets); [Secret types](https://docs.github.com/en/code-security/reference/secret-security/secret-types).
