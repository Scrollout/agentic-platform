# agentic-platform

[![check](https://github.com/Scrollout/agentic-platform/actions/workflows/check.yml/badge.svg)](https://github.com/Scrollout/agentic-platform/actions/workflows/check.yml)
[![evals](https://github.com/Scrollout/agentic-platform/actions/workflows/evals.yml/badge.svg)](https://github.com/Scrollout/agentic-platform/actions/workflows/evals.yml)

AI coding agents are useful but unreliable: the same request gives different results, and an
agent can skip tests, edit the tests that judge it, or report work as done when it is not. This
platform makes their work reliable and repeatable: **the model proposes, deterministic checks
decide.** A result is accepted only when checks fixed beforehand prove it, and a proven result is
reused instead of being generated again.

## How it works

1. You describe a use case in your own words. An agent turns it into goals, rules, examples and an
   API contract; checks verify each part, and you approve them.
2. The platform builds the software in fixed steps, each run by an agent in its own sandbox that
   can change only its own files. Tests come from your examples, and a step is accepted only when
   its checks pass.
3. Every run is recorded, so you can inspect it, replay it with the same result, and compare runs.

It is built on [Docker Agent](docs/references/docker-agent.md) and runs in containers on your own
machine.

## Status

An early version for one developer on one machine. It builds one kind of software: an API feature
of a single Go service. See [what it does and its limits](specs/001-prose-to-accepted-slice/spec.md).

## Try it

You need Linux with Docker, Compose and [gVisor](docs/guides/install-gvisor.md), and an Anthropic
API key. Follow the [quickstart](specs/001-prose-to-accepted-slice/quickstart.md).

## Learn more

- [Platform overview](docs/overview/platform.md): the problem, the approach and what it will not do.
- [Architecture](docs/architecture/overview.md) and [decisions](docs/decisions/index.md).
- [Constitution](.specify/memory/constitution.md): the rules every change follows.

## Contributing

Changes go through [GitHub Spec Kit](https://github.com/github/spec-kit) features in
[`specs/`](specs/), and project knowledge lives in [`docs/`](docs/index.md). Start with
[`AGENTS.md`](AGENTS.md).

## License

[MIT](LICENSE).
