---
type: Reference
title: GitHub Actions
description: What GitHub Actions gives a public repository on standard hosted runners (cost, machine size, concurrency, job time), how secrets reach workflows (forks, environments), two limits of job environments and artifact paths, and artifact and log retention, artifact listing and job output limits, as checked on 2026-11-16, 2026-11-22 and 2026-11-23.
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
  - id: contexts
    resource: https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#context-availability
    title: Contexts, context availability
  - id: artifact-run
    resource: https://github.com/Scrollout/agentic-platform/actions/runs/44888638411
    title: Evals run 44888638411, the upload step's error
  - id: retention
    resource: https://docs.github.com/en/organizations/managing-organization-settings/configuring-the-retention-period-for-github-actions-artifacts-and-logs-in-your-organization
    title: Configuring the retention period for artifacts and logs in an organization
  - id: artifacts-api
    resource: https://docs.github.com/en/rest/actions/artifacts
    title: REST API, artifacts
  - id: upload-artifact
    resource: https://github.com/actions/upload-artifact
    title: actions/upload-artifact README
  - id: download-artifact
    resource: https://github.com/actions/download-artifact
    title: actions/download-artifact README
  - id: workflow-syntax
    resource: https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions
    title: Workflow syntax, jobs.<job_id>.outputs
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

# Job environments and artifact paths

- A job-level `env` may use the `github`, `needs`, `strategy`, `matrix`, `vars`, `secrets` and
  `inputs` contexts, not `runner`; `runner.temp` is available only inside steps.
- `actions/upload-artifact` refuses a `path` holding a `.` or `..` segment ("Relative pathing '.'
  and '..' is not allowed"), as seen on 2026-11-22 with v7.0.2.

Sources: [Contexts, context availability](https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#context-availability); [Evals run 44888638411](https://github.com/Scrollout/agentic-platform/actions/runs/44888638411).

# Retention, artifacts and job outputs

- Artifacts and logs are kept 90 days by default; a public repository may set 1 to 90 days, and a
  repository's period cannot exceed its organization's or enterprise's limit. Since 2026-10-01 the
  same period also applies to checks, workflow runs and commit statuses. A change applies only to
  new objects.
- This repository's period was 1 day on 2026-11-23, and 1 day was the most its organization
  allowed (`actions/permissions/artifact-and-log-retention`), so an upload's `retention-days: 7`
  was kept 1 day. The owner raised both to 90 days the same day (feature 008 T017).
- `actions/upload-artifact` takes `retention-days` from 1 to 90; artifact names are unique in a
  run, and artifacts are immutable. Downloading another run's artifact needs a token with
  `actions:read` on the repository; a workflow's own `GITHUB_TOKEN` with `actions: read` did so
  (evals run 45170628365, 2026-11-23).
- The REST API lists a repository's artifacts (`per_page` at most 100) with `expired`,
  `created_at` and `workflow_run` (`id`, `head_branch`, `head_sha`); a download answers 302 with a
  zip URL that expires after 1 minute.
- A job's outputs may hold at most 1 MB, and a workflow run's at most 50 MB.

Sources: [Retention period in an organization](https://docs.github.com/en/organizations/managing-organization-settings/configuring-the-retention-period-for-github-actions-artifacts-and-logs-in-your-organization); [REST API, artifacts](https://docs.github.com/en/rest/actions/artifacts); [upload-artifact README](https://github.com/actions/upload-artifact); [download-artifact README](https://github.com/actions/download-artifact); [Workflow syntax](https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions).
