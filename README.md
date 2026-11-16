# agentic-platform

[![check](https://github.com/Scrollout/agentic-platform/actions/workflows/check.yml/badge.svg)](https://github.com/Scrollout/agentic-platform/actions/workflows/check.yml)
[![evals](https://github.com/Scrollout/agentic-platform/actions/workflows/evals.yml/badge.svg)](https://github.com/Scrollout/agentic-platform/actions/workflows/evals.yml)

AI-driven software work made reliable and repeatable: **the model proposes, deterministic checks
decide.** An agent's result is accepted only when checks the project fixed beforehand prove it,
and a proven solution is captured as a deterministic asset and reused instead of being generated
again ([platform overview](docs/overview/platform.md), [problem space](docs/overview/problem-space.md)).
It is built on [Docker Agent](docs/references/docker-agent.md).

## What works today

Feature 001, [from a description to accepted working software, one slice at a time](specs/001-prose-to-accepted-slice/spec.md),
runs on one developer's machine:

1. **Describe** a use case in your own words in the terminal. An analyst agent proposes a *slice*:
   goals, rules (shown as EARS sentences), Gherkin examples and an OpenAPI contract. Checks judge
   each part, you answer at most 10 questions, and you approve each part.
2. **Build** the approved slice into a new Go project. Fixed steps (contract check, generate, tests,
   code, review) run as Docker Agent step agents or fixed procedures, under gVisor, each allowed to
   change only its declared files. A step is accepted only when every check it declares passes:
   the tests are derived from the examples and seen failing before the code exists, and a review
   finding blocks only when its new witness test fails on the line it names. After three attempts
   or three review rounds the run is handed over to you.
3. **Inspect, replay and compare** runs in the terminal or on a read-only local page. A replay
   reuses every accepted result, and each result records its method, inputs, model and rule
   versions.

Status: Story 1 (describe and approve) and Story 2 (build) are implemented and converged; the
feature's last tasks are in [tasks.md](specs/001-prose-to-accepted-slice/tasks.md).

## Run it

You need Docker with Compose and gVisor ([install guide](docs/guides/install-gvisor.md)), and a
model key in a file only you can read. Build and sign the images, then start the platform and
describe a slice:

```sh
scripts/generate.sh
for i in engine page cli agent check project lint; do src/deploy/build.sh "$i"; done
src/deploy/ap up --credentials ~/.config/agentic-platform/anthropic_api_key --projects ~/projects
src/deploy/ap describe
```

The whole walk-through is the [quickstart](specs/001-prose-to-accepted-slice/quickstart.md); the
commands are in the [terminal contract](src/cli/contract.md), and running it locally is in
[local operations](docs/operations/local.md).

## Repository

| Where | What |
|-------|------|
| [`src/`](src/) | Engine, check service, terminal client, run page, agent definitions, catalogue pattern, contracts, tests, evals, deployment |
| [`specs/`](specs/) | Feature work with [GitHub Spec Kit](https://github.com/github/spec-kit): spec, plan, research, tasks |
| [`docs/`](docs/index.md) | Durable knowledge as an [OKF v0.2](docs/references/okf-spec.md) knowledge bundle, decisions in [`docs/decisions/`](docs/decisions/index.md) |
| [`scripts/`](scripts/) | Checks, code generation and git hooks |
| [`.specify/memory/constitution.md`](.specify/memory/constitution.md) | The binding rules |

## Develop

Every change follows the [constitution](.specify/memory/constitution.md); agents start at
[`AGENTS.md`](AGENTS.md). `scripts/check.sh` runs every check (`--list` names the steps,
`--only <step>` runs one), and the git hooks in `scripts/hooks/` guard each commit
([playbook](docs/playbooks/protect-changes-locally.md)). The knowledge bundle has its own checks:

```sh
python3 scripts/okf.py check   # validate docs/
python3 scripts/okf.py index   # regenerate every index.md
```

## License

[MIT](LICENSE).
