# AGENTS.md

This file wires coding agents to the project. It adds no rules of its own.
`CLAUDE.md` only imports this file and the constitution.

**The binding rules are in the constitution,
[`.specify/memory/constitution.md`](.specify/memory/constitution.md).** It is the
top authority. If anything here or elsewhere disagrees with it, the constitution
wins: stop and raise the conflict.

`agentic-platform` makes AI-driven software work reliable and repeatable: the
model proposes, deterministic checks decide. It is designed on Docker Agent. Start with
[the platform overview](docs/overview/platform.md),
[the problem space](docs/overview/problem-space.md), and
[the architecture](docs/architecture/overview.md).

## Where things live

| What | Where |
|------|-------|
| Binding rules | `.specify/memory/constitution.md` |
| Decisions behind the rules | `docs/decisions/` |
| Feature work: spec, plan, research, tasks | `specs/<NNN-name>/` ([GitHub Spec Kit](https://github.com/github/spec-kit)) |
| Durable knowledge | `docs/` ([OKF v0.2](https://github.com/GoogleCloudPlatform/open-knowledge-format/blob/ad30107c31c06aec8a7d5636e0d1058118604e6f/SPEC.md) knowledge bundle) |
| Product code, contracts, agent definitions, evals, deployment | `src/` (layout in the constitution) |

## Before any task

1. Read the constitution.
2. Read `docs/index.md`, then open only the concepts the task needs.
3. For feature work, read `specs/<NNN-name>/`, the problem-space needs it
   addresses, and `docs/architecture/overview.md`.
4. Before proposing code for any capability, check `docs/components/` and
   `docs/references/`, starting with [Docker Agent](docs/references/docker-agent.md)
   and the [Agent Host Protocol](docs/references/agent-host-protocol.md).

## Spec Kit commands

In Claude Code the Spec Kit commands are skills in `.claude/skills/`. Other agents
install theirs with `specify integration install <agent>`. The constitution sets
which steps are required. What each step also updates in `docs/`:

| Step | Command | Also do in `docs/` |
|------|---------|--------------------|
| 1 | `/speckit-specify` | Use existing concepts for terms and constraints, and link to them from `spec.md`. Add any missing term to `overview/glossary.md`. |
| 2 | `/speckit-clarify` | Same as step 1. |
| 3 | `/speckit-plan` | Copy facts from the `research.md` Sources table that outlive the feature into `references/`, with `sources` and `stale_after`. Each adopted OSS project gets a `components/` concept. Each entry in the plan's Decisions table gets a `decisions/` concept that links back to the plan. Open decisions are not recorded. |
| 4 | `/speckit-checklist` | None. |
| 5 | `/speckit-tasks` | None. |
| 6 | `/speckit-analyze` | None. |
| 7 | `/speckit-implement` | Update any concept the shipped behavior contradicts, in the same change. |
| 8 | `/speckit-converge` | Same as step 7. |

No Spec Kit presets or extensions are installed.

## Knowledge bundle

Follow [docs/playbooks/maintain-knowledge-bundle.md](docs/playbooks/maintain-knowledge-bundle.md).
Each section's `conventions.md` gives the concept type, required fields and a skeleton.
The official OKF repository linked above is the only OKF source this project
follows.

Repository checks:

| Command | Checks |
|---------|--------|
| `python3 scripts/okf.py check` | The knowledge bundle in `docs/`. Needs PyYAML. |
| `python3 scripts/okf.py index` | Regenerates every `index.md`. |
